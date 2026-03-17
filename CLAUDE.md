# CLAUDE.md — downwash developer guide

This file documents the project conventions, architecture, and development
workflow for Claude Code and human contributors.

## Project overview

`downwash` is a Go CLI tool that processes DJI drone MP4 videos and produces
post-flight analysis packages: GPX tracks, PNG charts, Markdown reports, and
PDF briefings.

Module: `github.com/arvis/downwash`
Go version: 1.21+
Entry point: `cmd/downwash/main.go`

## Package structure

| Package | Responsibility |
|---|---|
| `internal/telemetry` | exiftool invocation, CSV line parsing, `Frame` / `FlightStats` types, GPS helpers |
| `internal/ffmpeg` | `ffmpeg` transcode, `ffprobe` codec detection, typed `TranscodeError` |
| `internal/gpx` | GPX 1.1 XML writer, ~1 Hz downsampling, jitter filter |
| `internal/chart` | gonum/plot PNG charts: altitude profile (two-panel) and flight-track map |
| `internal/report` | Markdown + gofpdf three-page PDF briefing |
| `internal/pipeline` | Orchestrates all steps for a single video file |
| `cmd/downwash` | cobra CLI: `process`, `batch`, `version` sub-commands |
| `samples` | `//go:build ignore` generator producing synthetic sample artefacts |

## Key types

```go
// telemetry.Frame — one video frame of DJI telemetry (~1/30 s)
type Frame struct {
    SampleTime   time.Duration
    GPSTime      time.Time
    Lat, Lon     float64
    AltAbsolute  float64 // ASL metres
    AltRelative  float64 // AGL metres
    Roll, Pitch, Yaw, GimbalPitch, GimbalYaw float64
    ISO          int
    ShutterSpeed string
    FNumber      float64
    ColorTemperature int
}

// telemetry.FlightStats — aggregate flight statistics
type FlightStats struct { ... }

// ffmpeg.Options — transcode configuration
type Options struct {
    InputPath, OutputPath, Codec, Bitrate, Preset string
    Logger *slog.Logger
}

// pipeline.Options — full pipeline configuration
type Options struct { ... }
```

## External tool dependencies

- **exiftool** — extracts the djmd protobuf telemetry stream from DJI MP4 files. Invoked with `-ee -p <csv-format>`. A non-zero exit code is tolerated when frames have been extracted (exiftool returns warnings as errors).
- **ffmpeg** — video transcoding. Invoked with `-map 0:v:0` to select only the primary video stream (DJI embeds data streams that have no codec tag and would otherwise cause errors).
- **ffprobe** — codec detection. Invoked with `-select_streams v:0 -show_entries stream=codec_name`.

## Testing

Tests live in `_test.go` files in the same package. Run with:

```bash
go test ./...          # all unit tests (no external tools required)
go test -v ./...       # verbose
```

Integration tests (requiring exiftool + ffmpeg) use the `//go:build integration`
tag and are run via `make test-integration`.

All public parsing functions have table-driven unit tests covering normal cases,
edge cases, and expected error conditions.

## Code conventions

- **Errors** — always wrap with `fmt.Errorf("package: operation: %w", err)`.
- **Logging** — use `log/slog` throughout; pass `*slog.Logger` in all structs
  that produce output. Never call `log.Printf` directly.
- **Context** — propagate `context.Context` through all I/O operations.
- **CGO** — disabled (`CGO_ENABLED=0`) for all builds. Do not add CGO
  dependencies.
- **Comments** — exported symbols must have doc comments. Unexported helpers
  that are non-obvious should also be documented.

## GPS jitter filtering

GPS points where the haversine distance from the previous accepted point
exceeds **50 metres** are silently dropped from distance/speed calculations
and from the GPX track. This threshold is chosen to tolerate drone speeds
up to ~150 km/h between ~30 Hz samples (≈ 1.4 m/frame), while excluding
the multi-kilometre teleportation spikes common in DJI raw telemetry at the
start/end of a flight.

## Chart palette

Dark aviation theme (all files use the same palette defined in `chart/altitude.go`):

| Name | RGB | Use |
|---|---|---|
| `colBackground` | 18, 18, 28 | Plot background |
| `colGrid` | 50, 50, 70 | Grid lines |
| `colASL` | 77, 166, 255 | ASL altitude line (sky blue) |
| `colAGL` | 50, 220, 130 | AGL altitude line (green) |
| `colTrack` | 255, 210, 50 | GPS track line (amber) |
| `colStart` | 50, 220, 130 | Start marker |
| `colEnd` | 255, 80, 80 | End marker (red) |

## Adding a new output format

1. Create `internal/<format>/writer.go` with a `Write(frames, stats, outputPath)` function.
2. Add the step to `internal/pipeline/pipeline.go` in the appropriate position.
3. Add the output path to `pipeline.Result`.
4. Add the corresponding flag (if configurable) to `cmd/downwash/main.go`.
5. Update `README.md` and `samples/generate.go`.

## Release

```bash
git tag v0.x.0
make release          # requires GITHUB_TOKEN
```

goreleaser builds all platform binaries, creates .deb and .rpm via nfpm, and
publishes to GitHub Releases automatically.
