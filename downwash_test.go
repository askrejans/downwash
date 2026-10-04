package downwash

import (
	"archive/zip"
	"context"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testSource(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "synthetic.SRT")
	text := "1\n00:00:00,000 --> 00:00:01,000\n[latitude: 57] [longitude: 24] [rel_alt: 10 abs_alt: 50]\n\n2\n00:00:01,000 --> 00:00:02,000\n[latitude: 57.00001] [longitude: 24.00001] [rel_alt: 11 abs_alt: 51]\n\n3\n00:00:02,000 --> 00:00:03,000\n[latitude: 57.00002] [longitude: 24.00002] [rel_alt: 12 abs_alt: 52]\n"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOfflineLibraryExports(t *testing.T) {
	response, err := Process(context.Background(), Request{InputPath: testSource(t), OutputDir: t.TempDir(), ZipOutput: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Artifacts) != 7 {
		t.Fatalf("artifacts=%+v warnings=%v", response.Artifacts, response.Warnings)
	}
	for key, path := range response.Artifacts {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("%s output missing or empty: %v", key, err)
		}
		if strings.HasSuffix(key, "png") {
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = png.Decode(f)
			f.Close()
			if err != nil {
				t.Fatalf("%s invalid PNG: %v", key, err)
			}
		}
	}
	pdf, err := os.ReadFile(response.Artifacts["pdf"])
	if err != nil || !strings.HasPrefix(string(pdf), "%PDF-") {
		t.Fatalf("invalid PDF: %v", err)
	}
	pack, err := zip.OpenReader(response.Artifacts["zip"])
	if err != nil {
		t.Fatal(err)
	}
	defer pack.Close()
	if len(pack.File) != 6 {
		t.Fatalf("ZIP files=%d", len(pack.File))
	}
	gpx, err := os.ReadFile(response.Artifacts["gpx"])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gpx), "0001-") {
		t.Fatal("GPX invented timestamp for undated source")
	}
	imported, err := Analyze(context.Background(), response.Artifacts["metadata"])
	if err != nil {
		t.Fatal(err)
	}
	if string(imported.Frames) != string(response.Frames) {
		t.Fatal("metadata roundtrip changed frame data")
	}
}

func TestLibraryTrimSkipAndErrors(t *testing.T) {
	request := Request{InputPath: testSource(t), OutputDir: t.TempDir(), StartOffsetMS: 1000, EndTrimMS: 500, SkipCharts: true, SkipPDF: true, SkipGPX: true, SkipMarkdown: true, SkipMetadata: true}
	r, err := Process(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	var stats map[string]float64
	if err := json.Unmarshal(r.Stats, &stats); err != nil {
		t.Fatal(err)
	}
	if stats["frame_count"] != 1 || len(r.Artifacts) != 0 {
		t.Fatalf("stats=%v artifacts=%v", stats, r.Artifacts)
	}
	request.StartOffsetMS = 10000
	if _, err := Process(context.Background(), request); err == nil {
		t.Fatal("accepted empty trim")
	}
	request.StartOffsetMS = -1
	if _, err := Process(context.Background(), request); err == nil {
		t.Fatal("accepted negative trim")
	}
	for _, response := range []string{ProcessJSON(`{`), AnalyzeJSON("/missing.mp4"), ProcessJSON(`{}`)} {
		var result Response
		if err := json.Unmarshal([]byte(response), &result); err != nil {
			t.Fatal(err)
		}
		if result.Error == "" {
			t.Fatal("bridge omitted error")
		}
		if string(result.Frames) != "[]" || string(result.Stats) != "{}" {
			t.Fatalf("invalid error shape: %s", response)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Analyze(cancelled, request.InputPath); err == nil {
		t.Fatal("cancelled context accepted")
	}
}

func TestSourceAvailabilityAndMissingAltitude(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.srt")
	text := "1\n00:00:00,000 --> 00:00:01,000\n[latitude: 57] [longitude: 24] [rel_alt: 0]\n\n2\n00:00:01,000 --> 00:00:02,000\n[latitude: 57.00001] [longitude: 24.00001] [rel_alt: 1 abs_alt: 51]"
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := Analyze(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Available["gps"] || !r.Available["alt_relative"] || r.Available["attitude"] || r.Available["camera"] {
		t.Fatalf("available=%v", r.Available)
	}
	var stats map[string]float64
	if err := json.Unmarshal(r.Stats, &stats); err != nil {
		t.Fatal(err)
	}
	if stats["min_alt_asl_m"] != 51 || stats["min_alt_agl_m"] != 0 {
		t.Fatalf("missing altitude polluted range: %v", stats)
	}
	metadata := filepath.Join(t.TempDir(), "partial.json")
	if err := os.WriteFile(metadata, []byte(`{"version":"1.0","frames":[{"time_s":0,"iso":200}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	r, err = Analyze(context.Background(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	if r.Available["gps"] || r.Available["alt_asl"] || r.Available["alt_relative"] || !r.Available["camera"] {
		t.Fatalf("metadata invented fields: %v", r.Available)
	}
	if err := os.WriteFile(metadata, []byte(`{"version":"1.0","frames":[{"time_s":0}]} garbage`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(context.Background(), metadata); err == nil {
		t.Fatal("accepted trailing malformed JSON")
	}
}
