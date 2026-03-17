package gpx

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/askrejans/downwash/internal/telemetry"
)

// latPerM is the rough degree-per-metre conversion at the equator.
const latPerM = 1.0 / 111_195.0

func makeFrames() []telemetry.Frame {
	base := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	return []telemetry.Frame{
		{SampleTime: 0, GPSTime: base, Lat: 0, Lon: 0, AltAbsolute: 50, AltRelative: 10},
		{SampleTime: time.Second, GPSTime: base.Add(time.Second), Lat: latPerM * 20, Lon: 0, AltAbsolute: 60, AltRelative: 20},
		{SampleTime: 2 * time.Second, GPSTime: base.Add(2 * time.Second), Lat: latPerM * 40, Lon: 0, AltAbsolute: 70, AltRelative: 30},
		// Jitter spike — 10 degrees away, should be excluded.
		{SampleTime: 3 * time.Second, GPSTime: base.Add(3 * time.Second), Lat: 10, Lon: 10, AltAbsolute: 70, AltRelative: 30},
		// Resumes near original position — jitter filter skips because prev accepted is ~40m from here.
		{SampleTime: 4 * time.Second, GPSTime: base.Add(4 * time.Second), Lat: latPerM * 50, Lon: 0, AltAbsolute: 65, AltRelative: 25},
	}
}

func TestWriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "track.gpx")

	if err := Write(makeFrames(), "test_flight", out); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output file is empty")
	}
}

func TestWriteValidXML(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "track.gpx")
	if err := Write(makeFrames(), "test_flight", out); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var doc gpxDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("GPX is not valid XML: %v", err)
	}
	if doc.Version != "1.1" {
		t.Errorf("version = %q, want 1.1", doc.Version)
	}
}

func TestWriteJitterFilter(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "track.gpx")

	frames := []telemetry.Frame{
		// Valid starting point — becomes the previous accepted point.
		{SampleTime: 0, Lat: latPerM * 5, Lon: 0, AltAbsolute: 50},
		// Teleportation spike (>50 m) — must be filtered.
		{SampleTime: time.Second, Lat: 10, Lon: 10, AltAbsolute: 50},
		// Normal next point close to the starting position — accepted.
		{SampleTime: 2 * time.Second, Lat: latPerM * 10, Lon: 0, AltAbsolute: 50},
	}
	if err := Write(frames, "jitter", out); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, _ := os.ReadFile(out)
	var doc gpxDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid XML: %v", err)
	}

	for _, pt := range doc.Trk.Seg.Points {
		if pt.Lat > 1 || pt.Lon > 1 {
			t.Errorf("jitter spike not filtered: lat=%.4f lon=%.4f", pt.Lat, pt.Lon)
		}
	}
}

func TestWriteEmptyFrames(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "empty.gpx")
	err := Write(nil, "empty", out)
	if err == nil {
		t.Error("expected error for empty frame slice, got nil")
	}
}

func TestBuildPointsDownsample(t *testing.T) {
	// At 30 fps, 30 frames within the same 1-second bucket should collapse to 1 point.
	// Use i+1 so Lat is never zero (zero-coord frames are skipped as "no GPS fix").
	var frames []telemetry.Frame
	for i := 0; i < 30; i++ {
		frames = append(frames, telemetry.Frame{
			SampleTime:  time.Duration(i) * (time.Second / 30),
			Lat:         latPerM * float64(i+1) * 5, // non-zero
			Lon:         latPerM * float64(i+1),
			AltAbsolute: 50,
		})
	}
	pts := buildPoints(frames)
	if len(pts) != 1 {
		t.Errorf("expected 1 downsampled point, got %d", len(pts))
	}
}
