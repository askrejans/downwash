package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jung-kurt/gofpdf"

	"github.com/askrejans/downwash/internal/telemetry"
)

func TestPDFCreatesFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "briefing.pdf")

	stats := telemetry.FlightStats{
		Duration:      3*time.Minute + 12*time.Second,
		FrameCount:    5760,
		GPSPointCount: 192,
		MaxAltASL:     155.0,
		MinAltASL:     35.0,
		MaxAltAGL:     120.3,
		MinAltAGL:     0.0,
		MaxSpeedMS:    18.5,
		AvgSpeedMS:    8.2,
		DistanceM:     1580.0,
		StartLat:      57.165742,
		StartLon:      24.824664,
		EndLat:        57.165001,
		EndLon:        24.823500,
		ISO:           100,
		ShutterSpeed:  "1/500",
		FNumber:       1.7,
		ColorTemp:     5500,
	}

	err := PDF(stats, "DJI_0001", "hevc", "", "", out)
	if err != nil {
		t.Fatalf("PDF() failed: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output PDF is empty")
	}
}

func TestPDFEmptyStats(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "empty.pdf")

	err := PDF(telemetry.FlightStats{}, "empty_flight", "", "", "", out)
	if err != nil {
		t.Fatalf("PDF() with empty stats failed: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("empty stats PDF is empty")
	}
}

func TestPDFMissingChartImages(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "no_charts.pdf")

	stats := telemetry.FlightStats{
		Duration:   time.Minute,
		FrameCount: 100,
	}

	// Pass non-existent image paths — should not cause an error.
	err := PDF(stats, "test", "h264",
		"/nonexistent/altitude.png", "/nonexistent/track.png", out)
	if err != nil {
		t.Fatalf("PDF() with missing charts failed: %v", err)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"a very long string", 10, "…ng string"},
	}
	for _, tc := range cases {
		t.Run(tc.s, func(t *testing.T) {
			got := truncate(tc.s, tc.n)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.s, tc.n, got, tc.want)
			}
		})
	}
}

func TestCoordStr(t *testing.T) {
	cases := []struct {
		lat, lon float64
		wantN    string // substring that must be present
		wantDir  string
	}{
		{57.165742, 24.824664, "57.165742", "N"},
		{57.165742, -24.824664, "24.824664", "W"},
		{-33.8688, 151.2093, "33.868800", "S"},
		{-33.8688, 151.2093, "151.209300", "E"},
		{0, 0, "0.000000", "N"},
	}
	for _, tc := range cases {
		got := coordStr(tc.lat, tc.lon)
		if got == "" {
			t.Error("coordStr returned empty")
		}
		if !strings.Contains(got, tc.wantN) {
			t.Errorf("coordStr(%.6f, %.6f) = %q, missing %q", tc.lat, tc.lon, got, tc.wantN)
		}
		if !strings.Contains(got, tc.wantDir) {
			t.Errorf("coordStr(%.6f, %.6f) = %q, missing direction %q", tc.lat, tc.lon, got, tc.wantDir)
		}
	}
}

func TestEmbedImageMissingFile(t *testing.T) {
	// Should not panic with a missing file.
	pdf := gofpdf.New("L", "mm", "A4", "")
	pdf.AddPage()
	embedImage(pdf, "/nonexistent/image.png", 10, 10, 100, 100)
	// No error expected — just silently skipped.
}
