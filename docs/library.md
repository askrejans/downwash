# Offline telemetry library

Import `github.com/askrejans/downwash` to analyse telemetry and produce the same
GPX, KML/KMZ, CSV, PNG charts, Markdown, metadata JSON and PDF briefing as the command-line
tool. The library has no subprocess or network dependencies, remains pure Go,
and renders track charts on an offline background.

```go
response, err := downwash.Analyze(ctx, "/flights/DJI_0001.MP4")
response, err = downwash.Process(ctx, downwash.Request{
    InputPath: "/flights/DJI_0001.SRT",
    OutputDir: "/flights/reports",
    ZipOutput: true,
})
```

`AnalyzeJSON(inputPath string) string` and `ProcessJSON(requestJSON string) string`
provide a string-only integration boundary. A caller can wrap these functions
in the platform's preferred binding mechanism. The core requires no CGO.

## Source coverage

MP4, MOV and LRF use the ISO BMFF sample tables and DJI `djmd` protobuf stream.
The parser reads the movie metadata and telemetry samples by random access;
it skips encoded video data. Standard and extended atom sizes, `stco` and `co64`
offsets, variable and fixed sample sizes, and multiple timing/chunk-table entries
are supported. Unknown protocols, fragmented containers without ordinary sample
tables, invalid sample boundaries and malformed protobuf data fail explicitly.

Protocol field mappings follow the [ExifTool DJI tag reference](https://exiftool.org/TagNames/DJI.html)
and its [maintainer's source](https://github.com/exiftool/exiftool/blob/master/lib/Image/ExifTool/DJI.pm).
These are reverse-engineered protocols, rather than a promise that every firmware
revision writes the same fields. Tests exercise synthetic protocol samples and
container fixtures; hardware coverage still requires representative source files.

| DJI protocol | Associated model | Documented fields decoded |
| --- | --- | --- |
| `dvtm_Mini4_Pro` | Mini 4 Pro | GPS, absolute/relative altitude, aircraft/gimbal attitude, camera |
| `dvtm_Air3`, `dvtm_Air3s` | Air 3 / Air 3S | GPS, absolute/relative altitude, aircraft/gimbal attitude, camera |
| `dvtm_wm265e` | Mavic 3 | GPS, absolute/relative altitude, aircraft/gimbal attitude, ISO/shutter |
| `dvtm_wm261` | Mavic 3 Pro | GPS, absolute/relative altitude, aircraft/gimbal attitude, ISO/shutter/aperture |
| `dvtm_Mavic4` | Mavic 4 Pro | GPS, absolute/relative altitude, aircraft attitude, ISO/shutter/color temperature |
| `dvtm_Mini5Pro` | Mini 5 Pro | GPS and absolute altitude |
| `dvtm_pm320` | Matrice 30 | GPS, absolute/relative altitude, aircraft/gimbal attitude, ISO/shutter/aperture |
| `dvtm_wa345e` | Matrice 4E | GPS, absolute/relative altitude, aircraft/gimbal attitude |
| `dvtm_AVATA2` | Avata 2 | GPS, absolute/relative altitude, aircraft attitude, camera |
| `dvtm_dji_neo` | Neo | GPS, absolute altitude, aircraft attitude, camera |
| `dvtm_ac203`, `dvtm_ac204`, `dvtm_ac206` | Osmo Action 4 / 5 / 6 | GPS when recorded, GPS altitude, ISO/shutter/color temperature |
| `dvtm_oq101` | Osmo 360 | GPS when recorded, absolute altitude, ISO/shutter/aperture/color temperature |
| `dvtm_PP-101` | Osmo Pocket 3 | ISO/shutter/color temperature; no documented timed GPS fields |

Missing numerical protobuf fields use the protocol's zero default. Fields not
documented for a protocol are not guessed from another model. The response warns
when relative altitude or attitude is unavailable. GPS needs an actual recorded
fix; a device's GPS capability does not guarantee GPS is present in every file.

DJI SRT subtitles support labeled latitude/longitude, DJI's `longtitude` spelling,
relative/absolute altitude, camera settings, aircraft and gimbal attitude, HTML
wrappers, CRLF, comma/dot millisecond timestamps, and older aperture scaling.
Field names and units follow [DJI's subtitle documentation](https://repair.dji.com/help/content?customId=01700006657&lang=en&paperDocType=ARTICLE&re=US&spaceId=17).
Different sources may omit fields. Date text without an explicit timezone is not
converted into an invented GPS UTC timestamp.

Legacy `GPS(a,b,altitude)` tuples are accepted when coordinate bounds establish
one possible order: one coordinate is outside ±90° but within ±180°. Both
latitude-first and longitude-first variants and `BAROMETER(...)`/`BAROMETER:value`
are supported in that case. Both-in-range tuples are rejected as ambiguous.

Downwash metadata JSON version `1.0` can be reimported and reanalysed. Arbitrary
flight-log CSV, encrypted DJI Fly/GO TXT/DAT, GoPro GPMF, Autel logs, Remote ID,
DJI PPK/RINEX and live SDK telemetry are separate formats and are not accepted as
DJI video telemetry. The CLI keeps its exiftool fallback for other metadata that
ExifTool can extract.

## JSON contract

`ProcessJSON` accepts:

```json
{
  "input_path": "/flights/DJI_0001.MP4",
  "output_dir": "/flights/reports",
  "skip_gpx": false,
  "skip_csv": false,
  "skip_kml": false,
  "skip_kmz": false,
  "skip_charts": false,
  "skip_markdown": false,
  "skip_metadata": false,
  "skip_pdf": false,
  "zip_output": false,
  "start_offset_ms": 0,
  "end_trim_ms": 0
}
```

All formats are enabled by default. Offsets are nonnegative milliseconds on the
original video timeline, and an empty trim window fails before exports. PDF
briefings retain their charts even if standalone PNG export is disabled. ZIP
creation retains the individual library exports. Video transcoding remains the
CLI's optional ffmpeg operation and is not part of this offline library API.

Both bridges return:

```json
{
  "source": "DJI_0001.MP4",
  "format": "dji_djmd",
  "protocol": "dvtm_Mini4_Pro.proto",
  "stats": {},
  "frames": [],
  "artifacts": {},
  "available": {
    "gps": true,
    "alt_asl": true,
    "alt_relative": true,
    "attitude": true,
    "gimbal": true,
    "camera": true
  },
  "warnings": []
}
```

`format` is `dji_djmd`, `dji_srt` or `downwash_json`. `AnalyzeJSON` generates no
artifacts. `ProcessJSON` artifact keys are `gpx`, `csv`, `kml`, `kmz`, `altitude_png`, `track_png`,
`markdown`, `metadata`, `pdf` and `zip`; each contains the generated file path.
Disabled or failed outputs have no artifact key. Export failures appear in
`warnings` while successful files remain available. Fatal errors add a nonempty
`error` field. On an early fatal error, `stats` is `{}` and `frames` is `[]`.
`available` flags identify measurements recorded in at least one retained frame.
Each frame also includes its own `available` object. Use those flags when
displaying values or charting gaps; an absent measurement is not a measured zero.
Metadata reimport preserves flags, and older JSON without flags infers them from
the keys actually present. Missing altitude does not pollute statistics or charts.

`stats` uses the existing metadata JSON schema:

- `duration_s`, `distance_m`, `max_speed_ms`, `avg_speed_ms`
- `max_alt_asl_m`, `min_alt_asl_m`, `max_alt_agl_m`, `min_alt_agl_m`
- `alt_gain_m`, `alt_loss_m`, `max_climb_ms`, `max_descent_ms`
- `max_roll_deg`, `max_pitch_deg`, `max_yaw_rate_deg_s`, `max_home_dist_m`
- `frame_count`, `gps_point_count`, `start_lat`, `start_lon`, `end_lat`, `end_lon`
- Optional `start_time`, `end_time`, `iso`, `shutter_speed`, `f_number`,
  `color_temp_k` and `codec`

Each `frames` record contains `time_s`, `lat`, `lon`, `alt_asl_m`, `alt_agl_m`,
`roll_deg`, `pitch_deg`, `yaw_deg`, `gimbal_pitch_deg`, `gimbal_yaw_deg`; optional
fields are `gps_time`, `iso`, `shutter_speed`, `f_number`, `color_temp_k` and
`additional`. The last object preserves extra documented measurements such as
model/serial, frame dimensions/rate, digital zoom, gimbal roll and device-clock
readings when recorded. Raw sensor temperature has no assumed temperature unit.
Additional named SRT values are retained with an `srt_` prefix, without guessing
their meaning or units. Camera-only subtitles are accepted.

CSV includes every retained sample, explicit units in known column names,
additional field columns and empty cells for unavailable measurements. Text is
escaped against spreadsheet formula interpretation. KML uses WGS84 longitude,
latitude and recorded ASL altitude with `absolute` mode; if ASL is incomplete it
uses `clampToGround`. Relative altitude is never described as terrain height.
KMZ packages `doc.kml` without external dependencies. Track gaps and implausible
GPS jumps create separate line segments. PDF and Markdown show unavailable
measurements as N/A rather than fabricated zeroes.

Coordinates use WGS84 decimal degrees, altitude uses metres, speed uses metres
per second, and timestamps use RFC3339 UTC when source GPS time is available.
Invalid or missing coordinates become `(0, 0)` and are excluded from tracks and
distance. Genuine fixes on the equator or prime meridian are accepted. GPS
statistics are calculated near 1 Hz, with distance thresholds scaled for sparse
samples and speed plausibility filtering. These are source-derived estimates.

## Verification

Run `go test ./...`, `go test -race ./...`, `go vet ./...` and
`go test ./internal/telemetry -fuzz=FuzzProtoFields -fuzztime=5s`.
The CLI builds with `go build ./cmd/downwash` or `make build`.
