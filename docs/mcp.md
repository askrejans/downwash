# MCP automation

`downwash mcp` starts the official Go MCP SDK's stdio transport. It implements
all free open-source processing features: source discovery, analysis and full
telemetry pagination, every report format, synchronized telemetry trimming,
batch processing and optional video conversion with the existing ffmpeg engine.
Original files are preserved. Export folders must be new; video conversion
refuses an existing output. `ffmpeg` must be installed for video conversion.
Analysis and reports use the offline Go library without external executables.

```json
{"mcpServers":{"downwash":{"command":"/absolute/path/to/downwash","args":["mcp"]}}}
```

Tool names:

- `downwash_capabilities`: supported inputs, formats, transports and limits.
- `downwash_scan`: discover supported local sources without following symlinks.
- `downwash_analyze`: stats, availability, warnings and paginated full samples.
- `downwash_export`: PDF, CSV, metadata JSON, GPX, KML, KMZ, PNG charts,
  Markdown and ZIP, with optional start/end trimming.
- `downwash_batch`: up to 100 sources with separate output directories.
- `downwash_transcode`: H.264/H.265, bitrate, encoding preset and video trimming.

`source_path` is an absolute local file. Alternatively, provide `filename` and
`data_base64` to upload a file explicitly. `downwash_analyze` accepts `offset`
and `limit` (default 100, maximum 1000). It reports `total_samples` and
`next_offset`. Analysis is limited to 100,000 retained samples and 16 MiB of
metadata, with a 60-second request deadline. These budgets fail explicitly;
they do not silently truncate the source. Reanalyzing another page reads the
source again and does not persist media on a service.

`downwash_export` accepts `formats`, `start_offset_ms`, `end_trim_ms` and an
optional new `output_directory`. Without a local destination it returns
`artifacts` containing each file's basename, size and base64 bytes. Temporary
uploads and exports are removed after constructing the response. Embedded
artifacts together have a 16 MiB budget; select fewer formats for a large report.
Warnings disclose formats unavailable in the source. No generated artifacts is
an error, and a warning is not a claim that every requested format succeeded.

## Streamable HTTP

```sh
# Private upload-only HTTP. Inject a random token through deployment secrets.
DOWNWASH_MCP_TOKEN=YOUR_RANDOM_TOKEN_AT_LEAST_32_CHARACTERS downwash mcp --listen 127.0.0.1:8080
# Deliberately public upload-only HTTP, for a separately operated free service.
downwash mcp --listen 127.0.0.1:8080 --public
```

The endpoint is `/mcp`; `/health` reports readiness. Serve it behind HTTPS.
The default listener requires `Authorization: Bearer <token>` and a token of
at least 32 characters. `--public` deliberately enables anonymous uploaded-data
processing. Authentication tokens never belong in source control or a registry
record. Cross-origin browser requests are rejected, request bodies are capped
at 24 MiB, and a maximum of two operations run concurrently. Public HTTP accepts at most
30 POST requests per minute from each client IP and caps its rate-limit map at
10,000 IPs. Set `DOWNWASH_MCP_TRUST_PROXY=true` only behind a controlled proxy
that overwrites `X-Real-IP`; otherwise the TCP peer address is used.

HTTP exposes only capabilities, analysis and report exports. It rejects local
paths, output paths, URLs and local scan, batch and video conversion tools.
Explicit uploaded basenames cannot contain traversal. This prevents an HTTP
caller from accessing a server's private filesystem or fetching internal URLs.
Uploads are limited to 16 MiB decoded. Large video processing and video conversion
use local stdio. Public-service operators must deploy and verify their own HTTPS
route before advertising a remote endpoint. The included `Dockerfile.mcp` builds
the upload-only service; it contains no payment credentials.

This server is the open-source engine. It does not grant paid entitlements in any
commercial app, purchase products, or run store administration.
