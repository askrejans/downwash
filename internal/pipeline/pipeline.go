// Package pipeline orchestrates the full downwash processing workflow for a
// single DJI video file: telemetry extraction → chart generation → GPX export
// → report generation → optional video transcode.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/askrejans/downwash/internal/chart"
	"github.com/askrejans/downwash/internal/ffmpeg"
	"github.com/askrejans/downwash/internal/gpx"
	"github.com/askrejans/downwash/internal/report"
	"github.com/askrejans/downwash/internal/telemetry"
)

// StepName identifies a pipeline processing step.
type StepName string

const (
	StepTelemetry  StepName = "Extracting telemetry"
	StepCodec      StepName = "Probing codec"
	StepGPX        StepName = "Writing GPX track"
	StepAltChart   StepName = "Rendering altitude chart"
	StepTrackChart StepName = "Rendering track chart"
	StepMarkdown   StepName = "Writing Markdown report"
	StepMetadata   StepName = "Exporting metadata JSON"
	StepPDF        StepName = "Generating PDF briefing"
	StepZip        StepName = "Creating ZIP package"
	StepTranscode  StepName = "Transcoding video"
)

// AllSteps returns the ordered list of pipeline steps. If transcode is false,
// the transcode step is omitted. Steps disabled by skip flags are excluded.
func AllSteps(transcode bool) []StepName {
	return FilteredSteps(Options{Transcode: transcode})
}

// FilteredSteps returns steps based on the full options, excluding skipped ones.
func FilteredSteps(opts Options) []StepName {
	var steps []StepName
	if !opts.SkipTelemetry {
		steps = append(steps, StepTelemetry)
	}
	steps = append(steps, StepCodec)
	if !opts.SkipGPX {
		steps = append(steps, StepGPX)
	}
	if !opts.SkipCharts {
		steps = append(steps, StepAltChart, StepTrackChart)
	}
	if !opts.SkipMarkdown {
		steps = append(steps, StepMarkdown)
	}
	if !opts.SkipMetadata {
		steps = append(steps, StepMetadata)
	}
	if !opts.SkipPDF {
		steps = append(steps, StepPDF)
	}
	if opts.ZipOutput {
		steps = append(steps, StepZip)
	}
	if opts.Transcode {
		steps = append(steps, StepTranscode)
	}
	return steps
}

// StepStatus reports the current state of a pipeline step.
type StepStatus int

const (
	StepPending StepStatus = iota
	StepRunning
	StepDone
	StepSkipped
	StepFailed
)

// ProgressFunc is called by the pipeline to report step transitions.
type ProgressFunc func(step StepName, status StepStatus, msg string)

// Options configures a single-video pipeline run.
type Options struct {
	// InputPath is the source DJI MP4 video file.
	InputPath string
	// OutputDir is the directory where all output artefacts are written.
	// It is created if it does not exist.
	OutputDir string
	// Transcode controls whether the video is re-encoded. Default: false.
	Transcode bool
	// TranscodeCodec is "h264" or "h265". Default: "h264".
	TranscodeCodec string
	// TranscodeBitrate is a target bitrate string, e.g. "15M". Default: "15M".
	TranscodeBitrate string
	// TranscodePreset is the ffmpeg encode-speed/quality preset. Default: "medium".
	TranscodePreset string
	// SkipTelemetry skips exiftool extraction when set (useful for codec-only
	// test runs or when exiftool is not installed).
	SkipTelemetry bool
	// SkipGPX skips GPX track file generation.
	SkipGPX bool
	// SkipCharts skips altitude and track chart PNG generation.
	SkipCharts bool
	// SkipMarkdown skips Markdown report generation.
	SkipMarkdown bool
	// SkipPDF skips PDF briefing generation.
	SkipPDF bool
	// SkipMetadata skips metadata JSON export.
	SkipMetadata bool
	// ZipOutput bundles all output artefacts into a single ZIP file.
	// Individual files are removed after successful ZIP creation.
	ZipOutput bool
	// StartOffsetMS trims frames from the beginning of the video. Frames
	// with SampleTime < StartOffsetMS are excluded from all analysis.
	StartOffsetMS int
	// EndTrimMS trims frames from the end of the video. Frames within the
	// last EndTrimMS milliseconds of the flight are excluded from all analysis.
	EndTrimMS int
	// Logger is used for all pipeline log output. If nil, slog.Default() is used.
	Logger *slog.Logger
	// OnProgress is an optional callback invoked when pipeline steps start or
	// complete. If nil, no callbacks are made.
	OnProgress ProgressFunc
}

// Result describes the artefacts produced by a successful pipeline run.
type Result struct {
	GPXPath      string
	AltPNGPath   string
	TrackPNGPath string
	MarkdownPath string
	MetadataPath string
	PDFPath      string
	ZipPath      string // non-empty only if ZipOutput was enabled
	VideoPath    string // non-empty only if Transcode was performed
}

// Run executes the full processing pipeline for opts.InputPath.
//
// Processing steps (in order):
//  1. Validate input file and create output directory.
//  2. Extract telemetry frames with exiftool (unless SkipTelemetry).
//  3. Compute flight statistics.
//  4. Write GPX track file.
//  5. Render altitude profile PNG.
//  6. Render flight-track PNG.
//  7. Write Markdown post-flight report.
//  8. Generate aviation-themed PDF briefing.
//  9. Transcode video with ffmpeg (if Transcode is true).
//
// All output file names are derived from the input file's base name using
// standard suffixes (e.g. "_track.gpx", "_altitude.png", "_briefing.pdf").
func Run(ctx context.Context, opts Options) (Result, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	notify := func(step StepName, status StepStatus, msg string) {
		if opts.OnProgress != nil {
			opts.OnProgress(step, status, msg)
		}
	}

	// ── 1. Setup ─────────────────────────────────────────────────────────────
	if _, err := os.Stat(opts.InputPath); err != nil {
		return Result{}, fmt.Errorf("pipeline: input not found: %w", err)
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("pipeline: create output dir: %w", err)
	}

	base := stem(opts.InputPath)
	out := func(suffix string) string {
		return filepath.Join(opts.OutputDir, base+suffix)
	}

	var res Result

	// ── 2. Telemetry extraction ───────────────────────────────────────────────
	var frames []telemetry.Frame
	var stats telemetry.FlightStats
	var codec string

	if !opts.SkipTelemetry {
		notify(StepTelemetry, StepRunning, "")
		logger.Info("extracting telemetry", "file", opts.InputPath)
		var err error
		frames, err = telemetry.Extract(ctx, opts.InputPath, logger)
		if err != nil {
			logger.Warn("telemetry extraction failed (skipping)", "err", err)
			notify(StepTelemetry, StepFailed, err.Error())
		} else {
			notify(StepTelemetry, StepDone, fmt.Sprintf("%d frames", len(frames)))
		}
	} else {
		notify(StepTelemetry, StepSkipped, "")
	}

	// Capture original video duration (from telemetry) for ffmpeg trim sync.
	var originalDurationMS int
	if len(frames) > 0 {
		originalDurationMS = int(frames[len(frames)-1].SampleTime.Milliseconds())
	}

	// Apply time trimming if configured.
	if len(frames) > 0 && (opts.StartOffsetMS > 0 || opts.EndTrimMS > 0) {
		before := len(frames)
		frames = trimFrames(frames, opts.StartOffsetMS, opts.EndTrimMS)
		logger.Info("time trim applied",
			"before", before, "after", len(frames),
			"start_offset_ms", opts.StartOffsetMS,
			"end_trim_ms", opts.EndTrimMS)
	}

	if len(frames) > 0 {
		stats = telemetry.ComputeStats(frames)
		logger.Info("telemetry computed",
			"frames", stats.FrameCount,
			"duration", stats.Duration,
			"distance_m", fmt.Sprintf("%.0f", stats.DistanceM))
	}

	// Probe codec from the source file.
	notify(StepCodec, StepRunning, "")
	detectedCodec, err := ffmpeg.ProbeCodec(ctx, opts.InputPath)
	if err != nil {
		logger.Warn("codec probe failed", "err", err)
		notify(StepCodec, StepFailed, err.Error())
	} else {
		codec = detectedCodec
		notify(StepCodec, StepDone, codec)
	}

	// ── 3. GPX ───────────────────────────────────────────────────────────────
	if opts.SkipGPX {
		notify(StepGPX, StepSkipped, "disabled")
	} else if len(frames) > 0 {
		notify(StepGPX, StepRunning, "")
		gpxPath := out("_track.gpx")
		logger.Info("writing GPX", "path", gpxPath)
		if err := gpx.Write(frames, base, gpxPath); err != nil {
			logger.Warn("GPX write failed", "err", err)
			notify(StepGPX, StepFailed, err.Error())
		} else {
			res.GPXPath = gpxPath
			notify(StepGPX, StepDone, gpxPath)
		}
	} else {
		notify(StepGPX, StepSkipped, "no frames")
	}

	// ── 4. Altitude profile chart ────────────────────────────────────────────
	// When charts are skipped but PDF is enabled, render to temp files so the
	// PDF can embed them; the temp files are cleaned up after PDF generation.
	needChartsForPDF := opts.SkipCharts && !opts.SkipPDF && len(frames) > 0
	var tempAltPath, tempTrackPath string

	if opts.SkipCharts && !needChartsForPDF {
		notify(StepAltChart, StepSkipped, "disabled")
		notify(StepTrackChart, StepSkipped, "disabled")
	} else if len(frames) > 0 {
		notify(StepAltChart, StepRunning, "")
		altPath := out("_altitude.png")
		if needChartsForPDF {
			altPath = filepath.Join(os.TempDir(), base+"_altitude_tmp.png")
			tempAltPath = altPath
		}
		logger.Info("rendering altitude chart", "path", altPath)
		if err := chart.AltitudeProfile(frames, base, altPath); err != nil {
			logger.Warn("altitude chart failed", "err", err)
			notify(StepAltChart, StepFailed, err.Error())
		} else {
			if !needChartsForPDF {
				res.AltPNGPath = altPath
			}
			notify(StepAltChart, StepDone, altPath)
		}

		// ── 5. Flight track chart ────────────────────────────────────────────
		notify(StepTrackChart, StepRunning, "")
		trackPath := out("_track.png")
		if needChartsForPDF {
			trackPath = filepath.Join(os.TempDir(), base+"_track_tmp.png")
			tempTrackPath = trackPath
		}
		logger.Info("rendering track chart", "path", trackPath)
		if err := chart.FlightTrack(frames, base, trackPath); err != nil {
			logger.Warn("track chart failed", "err", err)
			notify(StepTrackChart, StepFailed, err.Error())
		} else {
			if !needChartsForPDF {
				res.TrackPNGPath = trackPath
			}
			notify(StepTrackChart, StepDone, trackPath)
		}
	} else {
		notify(StepAltChart, StepSkipped, "no frames")
		notify(StepTrackChart, StepSkipped, "no frames")
	}

	// ── 6. Markdown report ───────────────────────────────────────────────────
	if opts.SkipMarkdown {
		notify(StepMarkdown, StepSkipped, "disabled")
	} else {
		notify(StepMarkdown, StepRunning, "")
		mdPath := out("_report.md")
		logger.Info("writing markdown report", "path", mdPath)
		if err := report.Markdown(stats, base, codec, mdPath); err != nil {
			logger.Warn("markdown report failed", "err", err)
			notify(StepMarkdown, StepFailed, err.Error())
		} else {
			res.MarkdownPath = mdPath
			notify(StepMarkdown, StepDone, mdPath)
		}
	}

	// ── 7. Metadata JSON ────────────────────────────────────────────────────
	if opts.SkipMetadata {
		notify(StepMetadata, StepSkipped, "disabled")
	} else if len(frames) > 0 {
		notify(StepMetadata, StepRunning, "")
		metaPath := out("_metadata.json")
		logger.Info("exporting metadata JSON", "path", metaPath)
		if err := report.MetadataJSON(frames, stats, base, codec, metaPath); err != nil {
			logger.Warn("metadata JSON export failed", "err", err)
			notify(StepMetadata, StepFailed, err.Error())
		} else {
			res.MetadataPath = metaPath
			notify(StepMetadata, StepDone, metaPath)
		}
	} else {
		notify(StepMetadata, StepSkipped, "no frames")
	}

	// ── 8. PDF briefing ──────────────────────────────────────────────────────
	if opts.SkipPDF {
		notify(StepPDF, StepSkipped, "disabled")
	} else {
		notify(StepPDF, StepRunning, "")
		pdfPath := out("_briefing.pdf")
		logger.Info("generating PDF briefing", "path", pdfPath)

		// Use temp chart paths when charts were rendered only for the PDF.
		pdfAltPath := res.AltPNGPath
		pdfTrackPath := res.TrackPNGPath
		if tempAltPath != "" {
			pdfAltPath = tempAltPath
		}
		if tempTrackPath != "" {
			pdfTrackPath = tempTrackPath
		}

		if err := report.PDF(stats, base, codec, pdfAltPath, pdfTrackPath, pdfPath); err != nil {
			logger.Warn("PDF briefing failed", "err", err)
			notify(StepPDF, StepFailed, err.Error())
		} else {
			res.PDFPath = pdfPath
			notify(StepPDF, StepDone, pdfPath)
		}

		// Clean up temp chart files rendered only for the PDF.
		if tempAltPath != "" {
			os.Remove(tempAltPath)
		}
		if tempTrackPath != "" {
			os.Remove(tempTrackPath)
		}
	}

	// ── 9. ZIP package ──────────────────────────────────────────────────────
	if opts.ZipOutput {
		notify(StepZip, StepRunning, "")
		zipPath := out("_package.zip")
		logger.Info("creating ZIP package", "path", zipPath)

		filesToZip := collectOutputFiles(res)
		if err := createZip(zipPath, filesToZip); err != nil {
			logger.Warn("ZIP creation failed", "err", err)
			notify(StepZip, StepFailed, err.Error())
		} else {
			// Remove individual files after successful ZIP.
			for _, f := range filesToZip {
				os.Remove(f)
			}
			res.ZipPath = zipPath
			// Clear individual paths since files are now in the ZIP.
			res.GPXPath = ""
			res.AltPNGPath = ""
			res.TrackPNGPath = ""
			res.MarkdownPath = ""
			res.MetadataPath = ""
			res.PDFPath = ""
			notify(StepZip, StepDone, zipPath)
		}
	}

	// ── 10. Video transcode ──────────────────────────────────────────────────
	if opts.Transcode {
		notify(StepTranscode, StepRunning, "")
		videoCodec := opts.TranscodeCodec
		if videoCodec == "" {
			videoCodec = "h264"
		}
		ext := ".mp4"
		transPath := out("_" + strings.ToLower(videoCodec) + ext)
		logger.Info("transcoding video",
			"codec", videoCodec,
			"bitrate", opts.TranscodeBitrate,
			"output", transPath)

		err := ffmpeg.Transcode(ctx, ffmpeg.Options{
			InputPath:     opts.InputPath,
			OutputPath:    transPath,
			Codec:         videoCodec,
			Bitrate:       opts.TranscodeBitrate,
			Preset:        opts.TranscodePreset,
			StartOffsetMS: opts.StartOffsetMS,
			EndTrimMS:     opts.EndTrimMS,
			DurationMS:    originalDurationMS,
			Logger:        logger,
		})
		if err != nil {
			notify(StepTranscode, StepFailed, err.Error())
			return res, fmt.Errorf("pipeline: transcode: %w", err)
		}
		res.VideoPath = transPath
		notify(StepTranscode, StepDone, transPath)
	}

	return res, nil
}

// stem returns the filename without its directory and extension.
// e.g. "/videos/DJI_0001.MP4" → "DJI_0001"
func stem(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// collectOutputFiles returns all non-empty output file paths from a Result.
func collectOutputFiles(r Result) []string {
	var files []string
	for _, p := range []string{
		r.GPXPath, r.AltPNGPath, r.TrackPNGPath,
		r.MarkdownPath, r.MetadataPath, r.PDFPath,
	} {
		if p != "" {
			files = append(files, p)
		}
	}
	return files
}

// createZip bundles the listed files into a ZIP archive at zipPath.
// Each file is stored with its base name only (no directory structure).
func createZip(zipPath string, files []string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("pipeline: create zip: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for _, src := range files {
		if err := addFileToZip(zw, src); err != nil {
			return fmt.Errorf("pipeline: zip add %s: %w", filepath.Base(src), err)
		}
	}
	return nil
}

// addFileToZip copies a single file into the ZIP writer using its base name.
func addFileToZip(zw *zip.Writer, filePath string) error {
	src, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.Base(filePath)
	header.Method = zip.Deflate

	w, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, src)
	return err
}

// trimFrames returns the subset of frames within the time window defined by
// startOffsetMS (milliseconds from the beginning) and endTrimMS (milliseconds
// from the end). Both values are relative to the original video timeline.
func trimFrames(frames []telemetry.Frame, startOffsetMS, endTrimMS int) []telemetry.Frame {
	if len(frames) == 0 {
		return frames
	}

	startCut := time.Duration(startOffsetMS) * time.Millisecond
	totalDur := frames[len(frames)-1].SampleTime
	endCut := totalDur - time.Duration(endTrimMS)*time.Millisecond

	if endCut <= startCut {
		return nil
	}

	var trimmed []telemetry.Frame
	for _, f := range frames {
		if f.SampleTime >= startCut && f.SampleTime <= endCut {
			trimmed = append(trimmed, f)
		}
	}
	return trimmed
}
