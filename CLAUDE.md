# CLAUDE.md — downwash developer guide

This file documents the project conventions, architecture, and development
workflow for Claude Code and human contributors.

## Project overview

`downwash` is a Go CLI tool that processes DJI drone MP4 videos and produces
post-flight analysis packages: GPX tracks, PNG charts, Markdown reports, and
PDF briefings.

Module: `github.com/askrejans/downwash`
Go version: 1.21+
Entry point: `cmd/downwash/main.go`

## Package structure

| Package | Responsibility |
|---|---|
| `internal/telemetry` | exiftool invocation, CSV line parsing, `Frame` / `FlightStats` types |
| `internal/ffmpeg` | `ffmpeg` transcode, `ffprobe` codec detection, typed `TranscodeError` |
| `internal/geo` | Shared geodesy: `HaversineM`, coordinate rounding, `MaxGPSJitterM` constant |
| `internal/gpx` | GPX 1.1 XML writer, ~1 Hz downsampling, jitter filter |
| `internal/chart` | gonum/plot PNG charts: altitude profile, flight-track map with OSM tile background |
| `internal/report` | Markdown + gofpdf three-page PDF briefing |
| `internal/pipeline` | Orchestrates all steps for a single video file; progress callback API |
| `internal/tui` | Bubble Tea interactive terminal UI: file picker, progress view, result display |
| `cmd/downwash` | cobra CLI: `process`, `batch`, `version` sub-commands; TUI launcher |
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

// pipeline.StepName / StepStatus / ProgressFunc — progress callback API
type StepName string   // e.g. StepTelemetry, StepCodec, StepGPX, …
type StepStatus string // StepRunning, StepDone, StepFailed, StepSkipped
type ProgressFunc func(step StepName, status StepStatus, msg string)

// tui.Config — parameters passed from CLI to the TUI
type Config struct {
    Version      string
    FilePath     string           // skip file picker if set
    PipelineOpts pipeline.Options
}
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

### Sample artefacts

`samples/generate.go` (build-tag `ignore`) produces a full set of output
artefacts from a synthetic figure-8 flight (no real locations). Run with:

```bash
go run ./samples/generate.go
```

The generated files (`samples/sample_flight_*`) are committed to the repo and
can be used to visually verify chart rendering, markdown formatting, and PDF
layout without needing a real DJI video or exiftool/ffmpeg installed. When
changing chart colours, report templates, or PDF layout, regenerate the samples
and inspect the diff to catch regressions.

## Code conventions

- **Errors** — always wrap with `fmt.Errorf("package: operation: %w", err)`.
- **Logging** — use `log/slog` throughout; pass `*slog.Logger` in all structs
  that produce output. Never call `log.Printf` directly.
- **Context** — propagate `context.Context` through all I/O operations.
- **CGO** — disabled (`CGO_ENABLED=0`) for all builds. Do not add CGO
  dependencies.
- **Comments** — exported symbols must have doc comments. Unexported helpers
  that are non-obvious should also be documented.
- **Shared geo helpers** — haversine distance and GPS jitter threshold live in
  `internal/geo`. Do not duplicate these in other packages.
- **TUI** — interactive mode uses Bubble Tea (`charmbracelet/bubbletea`) with
  `bubbles` components and `lipgloss` styling. The `--no-tui` flag or a non-TTY
  environment falls back to plain CLI output.

## GPS filtering

Two filters clean up raw DJI GPS telemetry in `ComputeStats` and the GPX writer:

1. **Distance jitter filter** (`geo.MaxGPSJitterM = 50 m`) — frame pairs where
   the haversine distance exceeds 50 m are dropped. This excludes multi-kilometre
   teleportation spikes common at the start/end of DJI flights.

2. **Speed plausibility filter** (`geo.MaxPlausibleSpeedMS = 50 m/s / 180 km/h`)
   — frame pairs that produce an instantaneous speed above 50 m/s are excluded
   from distance and max-speed statistics. This catches GPS acquisition noise
   where small position errors at high sample rates (~30 Hz) produce absurd
   speeds (e.g. 2 m error in 33 ms = 60 m/s). The 50 m/s threshold covers
   even the fastest DJI FPV drones (~39 m/s in manual mode).

## TUI architecture

The interactive terminal UI lives in `internal/tui` and uses
[Bubble Tea](https://github.com/charmbracelet/bubbletea). Key design points:

- **State machine**: `stateFilePicker` → `stateOptions` → `stateProcessing` → `stateDone`.
- **Progress bridge**: `pipeline.ProgressFunc` callback sends `stepUpdateMsg`
  messages over a buffered channel to the Bubble Tea event loop.
- **Options menu**: `optionsModel` in `tui/options.go` lets the user toggle
  output artefacts (GPX, charts, markdown, PDF), enable video transcoding,
  and configure codec/bitrate/preset before processing starts.
- **Batch mode**: pressing `s` in the file picker selects the current directory.
  The TUI processes all MP4s sequentially, showing `[1/N] filename` progress.
- **Skip flags**: `pipeline.Options` has `SkipGPX`, `SkipCharts`, `SkipMarkdown`,
  `SkipPDF` booleans. `FilteredSteps(opts)` returns only the active steps.
- **File picker**: wraps `bubbles/filepicker` filtered to `.MP4`/`.mp4` files.
  Defaults to user home directory. Press `s` to select folder for batch mode.
- **Colour palette**: cyberpunk theme (neon cyan `#00FFDD`, violet `#BF40FF`,
  green-blue `#00CC99`, pink `#FF2E97`) defined in `tui/styles.go`.
- **Fallback**: when `--no-tui` is passed or stdin is not a TTY, the CLI runs
  in plain mode with text output — no Bubble Tea model is created.

### Additional TUI dependencies

| Module | Purpose |
|---|---|
| `github.com/charmbracelet/bubbletea` | Terminal UI framework |
| `github.com/charmbracelet/bubbles` | Spinner, filepicker components |
| `github.com/charmbracelet/lipgloss` | Styled terminal rendering |
| `golang.org/x/term` | TTY detection (`term.IsTerminal`) |

## OSM map tiles

The flight track chart (`internal/chart/track.go`) attempts to render the GPS
path over a dark map background using CartoDB dark_matter tiles:

- **Tile source**: `https://basemaps.cartocdn.com/dark_all/{z}/{x}/{y}.png`
  (no API key required, CC BY 3.0 license).
- **Fallback**: if any tile download fails or the bounding box is too large,
  the chart falls back to a plain dark background with grid lines.
- **Implementation**: `internal/chart/tiles.go` handles slippy-map tile math,
  HTTP fetching (10 s timeout), and nearest-neighbour image scaling.
- **Limits**: max 6×6 tile grid; zoom auto-selected to fit the flight bounding
  box with 15% padding.

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
