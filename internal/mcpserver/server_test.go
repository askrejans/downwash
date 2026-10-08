package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func session(t *testing.T, local bool) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ss, err := New("test", local).Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "integration-test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close(); _ = ss.Close() })
	return cs
}
func call(t *testing.T, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestRealMCPAnalyzeAndAllReportFormats(t *testing.T) {
	cs := session(t, true)
	sample, err := filepath.Abs("../../samples/sample_flight_metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	result := call(t, cs, "downwash_analyze", map[string]any{"source_path": sample, "offset": 1, "limit": 2})
	if result.IsError {
		t.Fatal(result.GetError())
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	analysis := value["analysis"].(map[string]any)
	if len(analysis["frames"].([]any)) != 2 || value["offset"] != float64(1) {
		t.Fatalf("pagination: %s", encoded)
	}
	folder := filepath.Join(t.TempDir(), "new-exports")
	result = call(t, cs, "downwash_export", map[string]any{"source_path": sample, "output_directory": folder, "formats": formats})
	if result.IsError {
		t.Fatal(result.GetError())
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Fatalf("expected ten real artifacts, got %d", len(entries))
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Size() == 0 {
			t.Fatalf("missing artifact %s", entry.Name())
		}
	}
	if !call(t, cs, "downwash_export", map[string]any{"source_path": sample, "output_directory": folder}).IsError {
		t.Fatal("existing output was accepted")
	}
	if !call(t, cs, "downwash_analyze", map[string]any{"source_path": sample, "limit": 1001}).IsError {
		t.Fatal("oversized page accepted")
	}
}
func TestHTTPUploadBoundaryAndReturnedArtifactBytes(t *testing.T) {
	cs := session(t, false)
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 3 {
		t.Fatalf("HTTP exposed %d tools", len(tools.Tools))
	}
	if !call(t, cs, "downwash_analyze", map[string]any{"source_path": "/etc/passwd"}).IsError {
		t.Fatal("remote local read accepted")
	}
	if !call(t, cs, "downwash_analyze", map[string]any{"filename": "../flight.srt", "data_base64": "YQ=="}).IsError {
		t.Fatal("uploaded traversal accepted")
	}
	sample, err := os.ReadFile("../../samples/sample_flight_metadata.json")
	if err != nil {
		t.Fatal(err)
	}
	result := call(t, cs, "downwash_export", map[string]any{"filename": "flight.json", "data_base64": base64.StdEncoding.EncodeToString(sample), "formats": []string{"gpx", "csv", "metadata", "zip"}})
	if result.IsError {
		t.Fatal(result.GetError())
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	var value map[string]any
	_ = json.Unmarshal(encoded, &value)
	for format, artifact := range value["artifacts"].(map[string]any) {
		file := artifact.(map[string]any)
		data, err := base64.StdEncoding.DecodeString(file["data_base64"].(string))
		if err != nil || len(data) == 0 {
			t.Fatalf("invalid %s content", format)
		}
		if strings.Contains(string(encoded), "downwash-mcp-output-") {
			t.Fatal("remote response leaks temporary filesystem path")
		}
	}
}
func TestHTTPAuthenticationOriginsAndDiscovery(t *testing.T) {
	if _, err := Handler("test", "", false); err == nil {
		t.Fatal("unauthorized public listener accepted")
	}
	handler, err := Handler("test", strings.Repeat("x", 32), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		token, origin string
		status        int
	}{{"", "", 401}, {strings.Repeat("x", 32), "https://other.example", 403}} {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`))
		if test.token != "" {
			request.Header.Set("Authorization", "Bearer "+test.token)
		}
		if test.origin != "" {
			request.Header.Set("Origin", test.origin)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.status {
			t.Fatalf("got %d want %d", response.Code, test.status)
		}
	}
	public, err := Handler("test", "", true)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(public)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "http-test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if call(t, cs, "downwash_capabilities", map[string]any{}).IsError {
		t.Fatal("real HTTP MCP capabilities failed")
	}
}

func TestPublicRateLimitCannotBeBypassedByClientIPHeader(t *testing.T) {
	t.Setenv("DOWNWASH_MCP_TRUST_PROXY", "false")
	handler, err := Handler("test", "", true)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 31; index++ {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/mcp", strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("X-Real-IP", fmt.Sprintf("203.0.113.%d", index+1))
		request.RemoteAddr = "192.0.2.1:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if index == 30 && (response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60") {
			t.Fatalf("missing public rate limit: status %d", response.Code)
		}
	}
}
