// Package pipeline orchestrates the full downwash processing workflow for a
// single DJI video file: telemetry extraction → chart generation → GPX export
// → report generation → optional video transcode.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
	StepPDF        StepName = "Generating PDF briefing"
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
	if !opts.SkipPDF {
		steps = append(steps, StepPDF)
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
	PDFPath      string
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
	if opts.SkipCharts {
		notify(StepAltChart, StepSkipped, "disabled")
		notify(StepTrackChart, StepSkipped, "disabled")
	} else if len(frames) > 0 {
		notify(StepAltChart, StepRunning, "")
		altPath := out("_altitude.png")
		logger.Info("rendering altitude chart", "path", altPath)
		if err := chart.AltitudeProfile(frames, base, altPath); err != nil {
			logger.Warn("altitude chart failed", "err", err)
			notify(StepAltChart, StepFailed, err.Error())
		} else {
			res.AltPNGPath = altPath
			notify(StepAltChart, StepDone, altPath)
		}

		// ── 5. Flight track chart ────────────────────────────────────────────
		notify(StepTrackChart, StepRunning, "")
		trackPath := out("_track.png")
		logger.Info("rendering track chart", "path", trackPath)
		if err := chart.FlightTrack(frames, base, trackPath); err != nil {
			logger.Warn("track chart failed", "err", err)
			notify(StepTrackChart, StepFailed, err.Error())
		} else {
			res.TrackPNGPath = trackPath
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

	// ── 7. PDF briefing ──────────────────────────────────────────────────────
	if opts.SkipPDF {
		notify(StepPDF, StepSkipped, "disabled")
	} else {
		notify(StepPDF, StepRunning, "")
		pdfPath := out("_briefing.pdf")
		logger.Info("generating PDF briefing", "path", pdfPath)
		if err := report.PDF(stats, base, codec, res.AltPNGPath, res.TrackPNGPath, pdfPath); err != nil {
			logger.Warn("PDF briefing failed", "err", err)
			notify(StepPDF, StepFailed, err.Error())
		} else {
			res.PDFPath = pdfPath
			notify(StepPDF, StepDone, pdfPath)
		}
	}

	// ── 8. Video transcode ───────────────────────────────────────────────────
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
			InputPath:  opts.InputPath,
			OutputPath: transPath,
			Codec:      videoCodec,
			Bitrate:    opts.TranscodeBitrate,
			Preset:     opts.TranscodePreset,
			Logger:     logger,
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
