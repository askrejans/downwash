// Package downwash provides offline telemetry analysis and export without a
// command-line process or external executable dependencies.
package downwash

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/askrejans/downwash/internal/chart"
	"github.com/askrejans/downwash/internal/gpx"
	"github.com/askrejans/downwash/internal/kml"
	"github.com/askrejans/downwash/internal/report"
	"github.com/askrejans/downwash/internal/telemetry"
)

// Frame is one timed telemetry measurement.
type Frame = telemetry.Frame

// FlightStats contains aggregate statistics computed with GPS jitter filtering.
type FlightStats = telemetry.FlightStats

// Availability identifies measurements recorded in a telemetry frame.
type Availability = telemetry.Availability

// Request configures offline exports. All formats are enabled by default.
type Request struct {
	InputPath     string `json:"input_path"`
	OutputDir     string `json:"output_dir"`
	SkipCSV       bool   `json:"skip_csv"`
	SkipKML       bool   `json:"skip_kml"`
	SkipKMZ       bool   `json:"skip_kmz"`
	SkipGPX       bool   `json:"skip_gpx"`
	SkipCharts    bool   `json:"skip_charts"`
	SkipMarkdown  bool   `json:"skip_markdown"`
	SkipMetadata  bool   `json:"skip_metadata"`
	SkipPDF       bool   `json:"skip_pdf"`
	ZipOutput     bool   `json:"zip_output"`
	StartOffsetMS int    `json:"start_offset_ms"`
	EndTrimMS     int    `json:"end_trim_ms"`
}

// Response contains the existing metadata schema plus generated file paths.
// Stats and Frames use the snake_case keys documented in docs/library.md.
type Response struct {
	Source    string            `json:"source"`
	Format    string            `json:"format"`
	Protocol  string            `json:"protocol,omitempty"`
	Stats     json.RawMessage   `json:"stats"`
	Frames    json.RawMessage   `json:"frames"`
	Artifacts map[string]string `json:"artifacts"`
	Warnings  []string          `json:"warnings"`
	Available map[string]bool   `json:"available"`
	Error     string            `json:"error,omitempty"`
}

type analysis struct {
	frames   []Frame
	stats    FlightStats
	codec    string
	response Response
}

// Analyze extracts supported MP4, DJI SRT, or Downwash metadata JSON telemetry.
// It performs no output writes or network requests.
func Analyze(ctx context.Context, inputPath string) (Response, error) {
	a, err := readAnalysis(ctx, Request{InputPath: inputPath})
	return a.response, err
}

// Process analyses a source and writes the requested reports, charts and tracks.
// Export failures are recorded as warnings while successful artifacts remain
// available. Source/validation failures return an error before writing outputs.
func Process(ctx context.Context, request Request) (Response, error) {
	a, err := readAnalysis(ctx, request)
	if err != nil {
		return a.response, err
	}
	if request.OutputDir == "" {
		return a.response, fmt.Errorf("downwash: output_dir is required")
	}
	if err := os.MkdirAll(request.OutputDir, 0o755); err != nil {
		return a.response, fmt.Errorf("downwash: create output directory: %w", err)
	}
	base := strings.TrimSuffix(filepath.Base(request.InputPath), filepath.Ext(request.InputPath))
	out := func(suffix string) string { return filepath.Join(request.OutputDir, base+suffix) }
	write := func(key, suffix string, enabled bool, generate func(string) error) {
		if !enabled {
			return
		}
		if err := ctx.Err(); err != nil {
			a.response.Warnings = append(a.response.Warnings, key+": "+err.Error())
			return
		}
		path := out(suffix)
		if err := generate(path); err != nil {
			a.response.Warnings = append(a.response.Warnings, key+": "+err.Error())
			os.Remove(path)
		} else {
			a.response.Artifacts[key] = path
		}
	}
	write("gpx", "_track.gpx", !request.SkipGPX, func(path string) error { return gpx.Write(a.frames, base, path) })
	write("csv", "_telemetry.csv", !request.SkipCSV, func(path string) error { return report.CSV(a.frames, path) })
	write("kml", "_track.kml", !request.SkipKML, func(path string) error { return kml.Write(a.frames, base, path) })
	write("kmz", "_track.kmz", !request.SkipKMZ, func(path string) error { return kml.WriteKMZ(a.frames, base, path) })
	// PDFs include the charts even when standalone chart export is disabled.
	altPath, trackPath := out("_altitude.png"), out("_track.png")
	if request.SkipCharts && !request.SkipPDF {
		tempDir, err := os.MkdirTemp(request.OutputDir, ".downwash-charts-")
		if err != nil {
			return a.response, fmt.Errorf("downwash: create PDF chart directory: %w", err)
		}
		defer os.RemoveAll(tempDir)
		altPath, trackPath = filepath.Join(tempDir, "altitude.png"), filepath.Join(tempDir, "track.png")
		if err := chart.AltitudeProfile(a.frames, base, altPath); err != nil {
			a.response.Warnings = append(a.response.Warnings, "pdf altitude chart: "+err.Error())
			altPath = ""
		}
		if err := chart.FlightTrackOffline(a.frames, base, trackPath); err != nil {
			a.response.Warnings = append(a.response.Warnings, "pdf track chart: "+err.Error())
			trackPath = ""
		}
	} else {
		write("altitude_png", "_altitude.png", !request.SkipCharts, func(path string) error { return chart.AltitudeProfile(a.frames, base, path) })
		write("track_png", "_track.png", !request.SkipCharts, func(path string) error { return chart.FlightTrackOffline(a.frames, base, path) })
		altPath, trackPath = a.response.Artifacts["altitude_png"], a.response.Artifacts["track_png"]
	}
	write("markdown", "_report.md", !request.SkipMarkdown, func(path string) error { return report.Markdown(a.stats, base, a.codec, path) })
	write("metadata", "_metadata.json", !request.SkipMetadata, func(path string) error { return report.MetadataJSON(a.frames, a.stats, base, a.codec, path) })
	write("pdf", "_briefing.pdf", !request.SkipPDF, func(path string) error { return report.PDF(a.stats, base, a.codec, altPath, trackPath, path) })
	if request.ZipOutput {
		files := make([]string, 0, len(a.response.Artifacts))
		for _, key := range []string{"csv", "kml", "kmz", "gpx", "altitude_png", "track_png", "markdown", "metadata", "pdf"} {
			if path := a.response.Artifacts[key]; path != "" {
				files = append(files, path)
			}
		}
		write("zip", "_package.zip", true, func(path string) error { return writeZIP(path, files) })
	}
	return a.response, nil
}

// AnalyzeJSON is a string-only bridge suitable for gomobile or a small FFI
// adapter. Errors are returned in the response's error field.
func AnalyzeJSON(inputPath string) string {
	r, err := Analyze(context.Background(), inputPath)
	return encodeResponse(r, err)
}

// ProcessJSON accepts a JSON Request and returns a JSON Response.
func ProcessJSON(requestJSON string) string {
	var request Request
	if err := json.Unmarshal([]byte(requestJSON), &request); err != nil {
		return encodeResponse(Response{}, fmt.Errorf("downwash: decode request: %w", err))
	}
	r, err := Process(context.Background(), request)
	return encodeResponse(r, err)
}

func encodeResponse(r Response, err error) string {
	if r.Artifacts == nil {
		r.Artifacts = map[string]string{}
	}
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	if r.Available == nil {
		r.Available = map[string]bool{}
	}
	if r.Stats == nil {
		r.Stats = json.RawMessage(`{}`)
	}
	if r.Frames == nil {
		r.Frames = json.RawMessage(`[]`)
	}
	if err != nil {
		r.Error = err.Error()
	}
	data, marshalErr := json.Marshal(r)
	if marshalErr != nil {
		data, _ = json.Marshal(map[string]string{"error": "downwash: encode response: " + marshalErr.Error()})
	}
	return string(data)
}

func readAnalysis(ctx context.Context, request Request) (analysis, error) {
	a := analysis{response: Response{Source: filepath.Base(request.InputPath), Artifacts: map[string]string{}, Warnings: []string{}}}
	if request.InputPath == "" {
		return a, fmt.Errorf("downwash: input_path is required")
	}
	if request.StartOffsetMS < 0 || request.EndTrimMS < 0 {
		return a, fmt.Errorf("downwash: trim offsets must be nonnegative")
	}
	if err := ctx.Err(); err != nil {
		return a, fmt.Errorf("downwash: analysis cancelled: %w", err)
	}
	var err error
	switch strings.ToLower(filepath.Ext(request.InputPath)) {
	case ".mp4", ".mov", ".lrf":
		var info telemetry.MP4Info
		a.frames, info, err = telemetry.ExtractMP4(ctx, request.InputPath)
		a.response.Format, a.response.Protocol, a.codec = "dji_djmd", info.Protocol, info.Codec
		a.response.Warnings = append(a.response.Warnings, info.Warnings...)
	case ".srt":
		var f *os.File
		f, err = os.Open(request.InputPath)
		if err == nil {
			defer f.Close()
			a.frames, err = telemetry.ParseSRT(f)
		}
		a.response.Format = "dji_srt"
		a.response.Warnings = append(a.response.Warnings, "SRT may omit attitude, gimbal, camera settings, or absolute altitude. Only data present in the source can be extracted.")
	case ".json":
		a.frames, err = readMetadata(request.InputPath)
		a.response.Format = "downwash_json"
	default:
		err = fmt.Errorf("supported sources are DJI MP4/MOV/LRF, bracketed DJI SRT, and Downwash metadata JSON")
	}
	if err != nil {
		return a, fmt.Errorf("downwash: read telemetry: %w", err)
	}
	if len(a.frames) == 0 {
		return a, fmt.Errorf("downwash: source contains no telemetry")
	}
	for i := range a.frames {
		f := &a.frames[i]
		if !telemetry.ValidGPS(f.Lat, f.Lon) || (f.Available != nil && !f.Available.GPS) {
			f.Lat, f.Lon = 0, 0
			if f.Available != nil {
				f.Available.GPS = false
			}
		}
		if f.ISO < 0 || f.ColorTemperature < 0 || f.FNumber < 0 {
			return a, fmt.Errorf("downwash: frame %d contains invalid camera settings", i)
		}
		for _, v := range []float64{f.AltAbsolute, f.AltRelative, f.Roll, f.Pitch, f.Yaw, f.GimbalPitch, f.GimbalYaw, f.FNumber} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return a, fmt.Errorf("downwash: frame %d contains nonfinite telemetry", i)
			}
		}
		if f.SampleTime < 0 || (i > 0 && f.SampleTime < a.frames[i-1].SampleTime) {
			return a, fmt.Errorf("downwash: frame %d has invalid sample time", i)
		}
	}
	if request.StartOffsetMS > 0 || request.EndTrimMS > 0 {
		start, end := time.Duration(request.StartOffsetMS)*time.Millisecond, a.frames[len(a.frames)-1].SampleTime-time.Duration(request.EndTrimMS)*time.Millisecond
		var trimmed []Frame
		for _, f := range a.frames {
			if f.SampleTime >= start && f.SampleTime <= end {
				trimmed = append(trimmed, f)
			}
		}
		if len(trimmed) == 0 {
			return a, fmt.Errorf("downwash: trim window contains no telemetry")
		}
		a.frames = trimmed
	}
	a.stats = telemetry.ComputeStats(a.frames)
	a.response.Available = map[string]bool{"gps": false, "alt_asl": false, "alt_relative": false, "attitude": false, "gimbal": false, "camera": false}
	for _, frame := range a.frames {
		available := frame.Available
		if available == nil {
			available = &telemetry.Availability{GPS: telemetry.ValidGPS(frame.Lat, frame.Lon), AltASL: true, AltRelative: true, Attitude: true, Gimbal: true, Camera: frame.ISO > 0 || frame.ShutterSpeed != "" || frame.FNumber > 0 || frame.ColorTemperature > 0}
		}
		for key, value := range map[string]bool{"gps": available.GPS, "alt_asl": available.AltASL, "alt_relative": available.AltRelative, "attitude": available.Attitude, "gimbal": available.Gimbal, "camera": available.Camera} {
			a.response.Available[key] = a.response.Available[key] || value
		}
	}
	if a.stats.GPSPointCount == 0 {
		a.response.Warnings = append(a.response.Warnings, "No valid GPS fixes; track and distance are unavailable.")
	}
	data, err := report.MetadataData(a.frames, a.stats, a.response.Source, a.codec)
	if err != nil {
		return a, err
	}
	var document struct {
		Stats  json.RawMessage `json:"stats"`
		Frames json.RawMessage `json:"frames"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return a, fmt.Errorf("downwash: metadata response: %w", err)
	}
	a.response.Stats, a.response.Frames = document.Stats, document.Frames
	return a, nil
}

func readMetadata(path string) ([]Frame, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > 128<<20 {
		return nil, fmt.Errorf("metadata JSON exceeds 128 MiB")
	}
	var document struct {
		Version string `json:"version"`
		Frames  []struct {
			Additional  map[string]any          `json:"additional"`
			TimeSec     *float64                `json:"time_s"`
			GPSTime     string                  `json:"gps_time"`
			Lat         float64                 `json:"lat"`
			Lon         float64                 `json:"lon"`
			AltASL      *float64                `json:"alt_asl_m"`
			AltAGL      *float64                `json:"alt_agl_m"`
			Roll        *float64                `json:"roll_deg"`
			Pitch       *float64                `json:"pitch_deg"`
			Yaw         *float64                `json:"yaw_deg"`
			GimbalPitch *float64                `json:"gimbal_pitch_deg"`
			GimbalYaw   *float64                `json:"gimbal_yaw_deg"`
			ISO         int                     `json:"iso"`
			Shutter     string                  `json:"shutter_speed"`
			FNumber     float64                 `json:"f_number"`
			Color       int                     `json:"color_temp_k"`
			Available   *telemetry.Availability `json:"available"`
		} `json:"frames"`
	}
	decoder := json.NewDecoder(io.LimitReader(f, 128<<20))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode metadata JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("metadata JSON contains trailing data")
	}
	if document.Version != "1.0" || len(document.Frames) == 0 {
		return nil, fmt.Errorf("expected Downwash metadata version 1.0 with frames")
	}
	frames := make([]Frame, 0, len(document.Frames))
	value := func(p *float64) float64 {
		if p == nil {
			return 0
		}
		return *p
	}
	for _, rec := range document.Frames {
		if rec.TimeSec == nil {
			return nil, fmt.Errorf("metadata frame is missing time_s")
		}
		f := Frame{SampleTime: time.Duration(value(rec.TimeSec) * float64(time.Second)), Lat: rec.Lat, Lon: rec.Lon, AltAbsolute: value(rec.AltASL), AltRelative: value(rec.AltAGL), Roll: value(rec.Roll), Pitch: value(rec.Pitch), Yaw: value(rec.Yaw), GimbalPitch: value(rec.GimbalPitch), GimbalYaw: value(rec.GimbalYaw), ISO: rec.ISO, ShutterSpeed: rec.Shutter, FNumber: rec.FNumber, ColorTemperature: rec.Color}
		f.Additional = rec.Additional
		f.Available = rec.Available
		if f.Available == nil {
			f.Available = &telemetry.Availability{GPS: telemetry.ValidGPS(f.Lat, f.Lon), AltASL: rec.AltASL != nil, AltRelative: rec.AltAGL != nil, Attitude: rec.Roll != nil || rec.Pitch != nil || rec.Yaw != nil, Gimbal: rec.GimbalPitch != nil || rec.GimbalYaw != nil, Camera: f.ISO > 0 || f.ShutterSpeed != "" || f.FNumber > 0 || f.ColorTemperature > 0}
		}
		if rec.GPSTime != "" {
			var err error
			f.GPSTime, err = time.Parse(time.RFC3339Nano, rec.GPSTime)
			if err != nil {
				return nil, fmt.Errorf("invalid gps_time: %w", err)
			}
		}
		frames = append(frames, f)
	}
	return frames, nil
}

func writeZIP(path string, files []string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()
	zw := zip.NewWriter(f)
	for _, source := range files {
		if err := func() error {
			src, err := os.Open(source)
			if err != nil {
				return err
			}
			defer src.Close()
			entry, err := zw.Create(filepath.Base(source))
			if err != nil {
				return err
			}
			_, err = io.Copy(entry, src)
			return err
		}(); err != nil {
			zw.Close()
			return fmt.Errorf("downwash: package artifact: %w", err)
		}
	}
	return zw.Close()
}
