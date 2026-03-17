package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/askrejans/downwash/internal/telemetry"
)

func TestMarkdownCreatesFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "report.md")

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

	if err := Markdown(stats, "DJI_0001", "hevc", out); err != nil {
		t.Fatalf("Markdown() failed: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	content := string(data)
	for _, want := range []string{
		"# Post-Flight Briefing",
		"DJI_0001",
		"HEVC",
		"3m 12s",
		"57.165742",
		"1/500",
		"f/1.7",
		"5500 K",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("markdown missing %q", want)
		}
	}
	// Ensure no HTML tags or garbled encoding.
	for _, bad := range []string{"<table>", "<tr>", "<td>", "<sub>", "&deg;", "&amp;", "&copy;", "&mdash;", "\u00c2"} {
		if strings.Contains(content, bad) {
			t.Errorf("markdown contains unwanted HTML/entity: %q", bad)
		}
	}
}

func TestMarkdownEmptyStats(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "empty.md")

	err := Markdown(telemetry.FlightStats{}, "empty_flight", "", out)
	if err != nil {
		t.Fatalf("Markdown() with empty stats failed: %v", err)
	}

	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), "# Post-Flight Briefing") {
		t.Error("empty stats markdown is missing header")
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m 30s"},
		{3*time.Minute + 12*time.Second, "3m 12s"},
		{1*time.Hour + 5*time.Minute + 3*time.Second, "1h 5m 3s"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got := formatDuration(tc.d)
			if got != tc.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
			}
		})
	}
}
