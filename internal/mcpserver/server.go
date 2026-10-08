// Package mcpserver exposes the actual offline library through MCP. HTTP accepts
// explicit uploads only; it never reads caller-supplied filesystem paths or URLs.
package mcpserver

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	downwash "github.com/askrejans/downwash"
	"github.com/askrejans/downwash/internal/ffmpeg"
	"github.com/askrejans/downwash/internal/telemetry"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxUpload = 16 << 20

var formats = []string{"gpx", "csv", "kml", "kmz", "charts", "markdown", "metadata", "pdf", "zip"}

type source struct {
	Path string `json:"source_path,omitempty" jsonschema:"Absolute source path; available only over local stdio"`
	Name string `json:"filename,omitempty" jsonschema:"Uploaded basename with .srt, .json, .mp4, .mov or .lrf extension"`
	Data string `json:"data_base64,omitempty" jsonschema:"Explicit uploaded recording encoded in base64; at most 16 MiB decoded"`
}
type analyzeArgs struct {
	source
	Offset int `json:"offset,omitempty" jsonschema:"Telemetry sample offset; nonnegative"`
	Limit  int `json:"limit,omitempty" jsonschema:"Samples per page; 1 to 1000; default 100"`
}
type exportArgs struct {
	source
	Output  string   `json:"output_directory,omitempty" jsonschema:"A new absolute output folder; stdio only; omit for embedded artifacts"`
	Formats []string `json:"formats,omitempty" jsonschema:"Select gpx,csv,kml,kmz,charts,markdown,metadata,pdf,zip; defaults to all"`
	Start   int      `json:"start_offset_ms,omitempty" jsonschema:"Milliseconds trimmed from the beginning"`
	End     int      `json:"end_trim_ms,omitempty" jsonschema:"Milliseconds trimmed from the end"`
}
type scanArgs struct {
	Path      string `json:"directory" jsonschema:"Absolute local directory"`
	Recursive bool   `json:"recursive,omitempty"`
}
type transcodeArgs struct {
	Path     string `json:"source_path"`
	Output   string `json:"output_path"`
	Codec    string `json:"codec,omitempty" jsonschema:"h264 or h265; default h264"`
	Bitrate  string `json:"bitrate,omitempty" jsonschema:"Target bitrate such as 15M"`
	Preset   string `json:"preset,omitempty" jsonschema:"Encoder preset such as medium"`
	Start    int    `json:"start_offset_ms,omitempty"`
	End      int    `json:"end_trim_ms,omitempty"`
	Duration int    `json:"duration_ms,omitempty" jsonschema:"Required for end trimming"`
}
type batchArgs struct {
	Paths   []string `json:"source_paths"`
	Output  string   `json:"output_directory"`
	Formats []string `json:"formats,omitempty"`
	Start   int      `json:"start_offset_ms,omitempty"`
	End     int      `json:"end_trim_ms,omitempty"`
}

func annotation(readOnly bool) *mcp.ToolAnnotations {
	no := false
	return &mcp.ToolAnnotations{ReadOnlyHint: readOnly, DestructiveHint: &no, OpenWorldHint: &no}
}
func input(args source, local bool) (string, func(), error) {
	if args.Path != "" {
		if !local || args.Data != "" || args.Name != "" || !filepath.IsAbs(args.Path) {
			return "", nil, fmt.Errorf("mcp: local paths require stdio and cannot be combined with an upload")
		}
		return args.Path, func() {}, nil
	}
	if args.Name == "" || filepath.Base(args.Name) != args.Name || strings.ContainsAny(args.Name, "/\\\x00") || args.Data == "" {
		return "", nil, fmt.Errorf("mcp: provide an uploaded basename and data_base64")
	}
	switch strings.ToLower(filepath.Ext(args.Name)) {
	case ".srt", ".json", ".mp4", ".mov", ".lrf":
	default:
		return "", nil, fmt.Errorf("mcp: unsupported source extension")
	}
	if len(args.Data) > base64.StdEncoding.EncodedLen(maxUpload) {
		return "", nil, fmt.Errorf("mcp: upload exceeds 16 MiB")
	}
	data, err := base64.StdEncoding.Strict().DecodeString(args.Data)
	if err != nil || len(data) > maxUpload {
		return "", nil, fmt.Errorf("mcp: invalid or oversized base64 upload")
	}
	dir, err := os.MkdirTemp("", "downwash-mcp-input-")
	if err != nil {
		return "", nil, fmt.Errorf("mcp: create input: %w", err)
	}
	clean := func() { _ = os.RemoveAll(dir) }
	path := filepath.Join(dir, args.Name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		clean()
		return "", nil, fmt.Errorf("mcp: write input: %w", err)
	}
	return path, clean, nil
}
func limited(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	return telemetry.WithAnalysisLimits(ctx, downwash.AnalysisLimits{MaxFrames: 100000, MaxMetadataBytes: maxUpload}), cancel
}
func analyze(ctx context.Context, args analyzeArgs, local bool) (any, error) {
	if args.Offset < 0 || args.Limit < 0 || args.Limit > 1000 {
		return nil, fmt.Errorf("mcp: invalid telemetry page")
	}
	if args.Limit == 0 {
		args.Limit = 100
	}
	path, clean, err := input(args.source, local)
	if err != nil {
		return nil, err
	}
	defer clean()
	ctx, cancel := limited(ctx)
	defer cancel()
	result, err := downwash.Analyze(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("mcp: analyze: %w", err)
	}
	var frames []json.RawMessage
	if err := json.Unmarshal(result.Frames, &frames); err != nil {
		return nil, fmt.Errorf("mcp: decode frames: %w", err)
	}
	start := min(args.Offset, len(frames))
	end := min(start+args.Limit, len(frames))
	var next *int
	if end < len(frames) {
		next = &end
	}
	result.Frames, _ = json.Marshal(frames[start:end])
	return map[string]any{"analysis": result, "total_samples": len(frames), "offset": start, "next_offset": next}, nil
}
func request(path, out string, args exportArgs) (downwash.Request, error) {
	selected := map[string]bool{}
	if len(args.Formats) == 0 {
		args.Formats = formats
	}
	for _, format := range args.Formats {
		known := false
		for _, value := range formats {
			if format == value {
				known = true
				break
			}
		}
		if !known {
			return downwash.Request{}, fmt.Errorf("mcp: unknown export format %q", format)
		}
		selected[format] = true
	}
	if args.Start < 0 || args.End < 0 {
		return downwash.Request{}, fmt.Errorf("mcp: trims must be nonnegative")
	}
	return downwash.Request{InputPath: path, OutputDir: out, SkipGPX: !selected["gpx"], SkipCSV: !selected["csv"], SkipKML: !selected["kml"], SkipKMZ: !selected["kmz"], SkipCharts: !selected["charts"], SkipMarkdown: !selected["markdown"], SkipMetadata: !selected["metadata"], SkipPDF: !selected["pdf"], ZipOutput: selected["zip"], StartOffsetMS: args.Start, EndTrimMS: args.End}, nil
}
func export(ctx context.Context, args exportArgs, local bool) (any, error) {
	path, clean, err := input(args.source, local)
	if err != nil {
		return nil, err
	}
	defer clean()
	out := args.Output
	if out != "" {
		if !local || !filepath.IsAbs(out) {
			return nil, fmt.Errorf("mcp: output_directory is an absolute local stdio path")
		}
		if err := os.Mkdir(out, 0o700); err != nil {
			return nil, fmt.Errorf("mcp: output_directory must be new: %w", err)
		}
	} else {
		out, err = os.MkdirTemp("", "downwash-mcp-output-")
		if err != nil {
			return nil, fmt.Errorf("mcp: create output: %w", err)
		}
		defer os.RemoveAll(out)
	}
	opts, err := request(path, out, args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := limited(ctx)
	defer cancel()
	result, err := downwash.Process(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("mcp: export: %w", err)
	}
	// Full telemetry is available through analyze; keep report responses bounded.
	if len(result.Artifacts) == 0 {
		return nil, fmt.Errorf("mcp: no requested artifacts were generated: %s", strings.Join(result.Warnings, "; "))
	}
	result.Frames = json.RawMessage(`[]`)
	artifacts := map[string]any{}
	total := int64(0)
	for format, path := range result.Artifacts {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			return nil, fmt.Errorf("mcp: generated artifact is missing")
		}
		if args.Output != "" {
			artifacts[format] = map[string]any{"path": path, "size": info.Size()}
			continue
		}
		total += info.Size()
		if total > maxUpload {
			return nil, fmt.Errorf("mcp: artifacts exceed 16 MiB; select fewer formats or use a local output directory")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("mcp: read artifact: %w", err)
		}
		artifacts[format] = map[string]any{"name": filepath.Base(path), "size": len(data), "data_base64": base64.StdEncoding.EncodeToString(data)}
	}
	result.Artifacts = nil
	return map[string]any{"analysis": result, "artifacts": artifacts}, nil
}

// New returns a complete local server, or an upload-only HTTP server. No tool
// contacts a store, charges customers, or changes an application's entitlement.
func New(version string, local bool) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "downwash", Version: version}, &mcp.ServerOptions{Instructions: "Call downwash_capabilities first. Source files remain unchanged. Use stdio for local files, batch exports and video conversion; HTTP accepts explicit base64 uploads only. Telemetry is paginated. Exports either use a new local output directory or return embedded base64 files. This is the free open-source engine."})
	mcp.AddTool(server, &mcp.Tool{Name: "downwash_capabilities", Description: "Discover actual supported formats, transports and limits.", Annotations: annotation(true)}, func(ctx context.Context, req *mcp.CallToolRequest, args struct{}) (*mcp.CallToolResult, any, error) {
		return nil, map[string]any{"formats": formats, "local_files": local, "batch": local, "video_conversion": local, "video_conversion_requires": "ffmpeg on PATH", "http_upload_limit_bytes": maxUpload, "maximum_retained_samples": 100000, "source_formats": []string{"mp4", "mov", "lrf", "srt", "json"}, "price": "free open-source engine"}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "downwash_analyze", Description: "Analyze supported DJI telemetry and recording details. Return full stats, availability, warnings and a page of telemetry samples.", Annotations: annotation(true)}, func(ctx context.Context, req *mcp.CallToolRequest, args analyzeArgs) (*mcp.CallToolResult, any, error) {
		value, err := analyze(ctx, args, local)
		return nil, value, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "downwash_export", Description: "Produce PDF, CSV, JSON, GPX, KML, KMZ, charts, Markdown and ZIP with optional time trimming. A local output folder must be new. Uploaded files and temporary outputs are removed after the response.", Annotations: annotation(false)}, func(ctx context.Context, req *mcp.CallToolRequest, args exportArgs) (*mcp.CallToolResult, any, error) {
		value, err := export(ctx, args, local)
		return nil, value, err
	})
	if local {
		mcp.AddTool(server, &mcp.Tool{Name: "downwash_scan", Description: "Find supported local recording files without following directory symlinks.", Annotations: annotation(true)}, func(ctx context.Context, req *mcp.CallToolRequest, args scanArgs) (*mcp.CallToolResult, any, error) {
			if !filepath.IsAbs(args.Path) {
				return nil, nil, fmt.Errorf("mcp: an absolute directory is required")
			}
			files := []string{}
			err := filepath.WalkDir(args.Path, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if entry.IsDir() {
					if path != args.Path && !args.Recursive {
						return filepath.SkipDir
					}
					return nil
				}
				if entry.Type()&os.ModeSymlink != 0 {
					return nil
				}
				switch strings.ToLower(filepath.Ext(path)) {
				case ".mp4", ".mov", ".lrf", ".srt", ".json":
					files = append(files, path)
				}
				if len(files) > 10000 {
					return fmt.Errorf("mcp: scan a smaller folder")
				}
				return nil
			})
			return nil, map[string]any{"paths": files}, err
		})
		mcp.AddTool(server, &mcp.Tool{Name: "downwash_batch", Description: "Export up to 100 local sources to separate new folders. Includes every report format and synchronized telemetry trimming.", Annotations: annotation(false)}, func(ctx context.Context, req *mcp.CallToolRequest, args batchArgs) (*mcp.CallToolResult, any, error) {
			if len(args.Paths) == 0 || len(args.Paths) > 100 || !filepath.IsAbs(args.Output) {
				return nil, nil, fmt.Errorf("mcp: provide 1-100 absolute sources and a new output directory")
			}
			if err := os.Mkdir(args.Output, 0o700); err != nil {
				return nil, nil, fmt.Errorf("mcp: batch output must be new: %w", err)
			}
			results := []any{}
			for index, path := range args.Paths {
				if err := ctx.Err(); err != nil {
					return nil, nil, err
				}
				value, err := export(ctx, exportArgs{source: source{Path: path}, Output: filepath.Join(args.Output, fmt.Sprintf("%03d", index+1)), Formats: args.Formats, Start: args.Start, End: args.End}, true)
				if err != nil {
					return nil, nil, err
				}
				results = append(results, value)
			}
			return nil, map[string]any{"results": results}, nil
		})
		mcp.AddTool(server, &mcp.Tool{Name: "downwash_transcode", Description: "Trim/compress a local video using ffmpeg, preserving the source. Requires a new output path; codecs h264/h265 and bitrate/preset are configurable.", Annotations: annotation(false)}, func(ctx context.Context, req *mcp.CallToolRequest, args transcodeArgs) (*mcp.CallToolResult, any, error) {
			if !filepath.IsAbs(args.Path) || !filepath.IsAbs(args.Output) || filepath.Clean(args.Path) == filepath.Clean(args.Output) {
				return nil, nil, fmt.Errorf("mcp: distinct absolute input/output paths are required")
			}
			if _, err := os.Lstat(args.Output); !os.IsNotExist(err) {
				return nil, nil, fmt.Errorf("mcp: output already exists or is inaccessible")
			}
			err := ffmpeg.Transcode(ctx, ffmpeg.Options{NoOverwrite: true, InputPath: args.Path, OutputPath: args.Output, Codec: args.Codec, Bitrate: args.Bitrate, Preset: args.Preset, StartOffsetMS: args.Start, EndTrimMS: args.End, DurationMS: args.Duration, Logger: slog.New(slog.NewTextHandler(os.Stderr, nil))})
			if err != nil {
				return nil, nil, err
			}
			info, err := os.Stat(args.Output)
			if err != nil || info.Size() == 0 {
				return nil, nil, fmt.Errorf("mcp: no converted video was produced")
			}
			return nil, map[string]any{"path": args.Output, "size": info.Size()}, nil
		})
	}
	return server
}

// Handler restricts HTTP to uploaded data and bounds concurrent processing. The
// operator must select authenticated access or deliberately public upload tools.
func Handler(version, token string, public bool) (http.Handler, error) {
	if !public && len(token) < 32 {
		return nil, fmt.Errorf("mcp: set DOWNWASH_MCP_TOKEN to at least 32 characters, or explicitly use --public")
	}
	server := New(version, false)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 24 << 20, PropagateRequestCancellation: true})
	slots := make(chan struct{}, 2)
	type window struct {
		start time.Time
		count int
	}
	var rateMutex sync.Mutex
	rates := map[string]window{}
	trustProxy := os.Getenv("DOWNWASH_MCP_TRUST_PROXY") == "true"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/health" && r.Method == "GET" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ready","service":"downwash-mcp"}`))
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browser origins are not supported", http.StatusForbidden)
			return
		}
		if !public && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="downwash-mcp"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if public && r.Method == http.MethodPost {
			address, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				address = r.RemoteAddr
			}
			if trustProxy {
				if ip := net.ParseIP(r.Header.Get("X-Real-IP")); ip != nil {
					address = ip.String()
				}
			}
			now := time.Now()
			rateMutex.Lock()
			for key, value := range rates {
				if now.Sub(value.start) >= time.Minute {
					delete(rates, key)
				}
			}
			value, exists := rates[address]
			saturated := !exists && len(rates) >= 10000
			if !exists {
				value.start = now
			}
			value.count++
			if !saturated {
				rates[address] = value
			}
			rateMutex.Unlock()
			if saturated || value.count > 30 {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "upload request limit reached", http.StatusTooManyRequests)
				return
			}
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, "processor busy", http.StatusTooManyRequests)
			return
		}
		handler.ServeHTTP(w, r)
	}), nil
}
