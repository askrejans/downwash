package chart

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/askrejans/downwash/internal/telemetry"
)

// latPerM is the rough degree-per-metre conversion at the equator.
const testLatPerM = 1.0 / 111_195.0

// syntheticFrames returns a small slice of frames suitable for chart tests.
func syntheticFrames(n int) []telemetry.Frame {
	frames := make([]telemetry.Frame, n)
	for i := range frames {
		t := float64(i) / 30.0 // ~30 fps
		frames[i] = telemetry.Frame{
			SampleTime:  time.Duration(t * float64(time.Second)),
			Lat:         testLatPerM * float64(i) * 5,
			Lon:         testLatPerM * float64(i) * 2,
			AltAbsolute: 50 + float64(i)*0.5,
			AltRelative: 10 + float64(i)*0.3,
		}
	}
	return frames
}

func TestAltitudeProfileCreatesFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "alt.png")

	frames := syntheticFrames(90) // 3 seconds at 30 fps
	if err := AltitudeProfile(frames, "Test Flight", out); err != nil {
		t.Fatalf("AltitudeProfile: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output PNG is empty")
	}
}

func TestAltitudeProfileEmptyFrames(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "empty.png")

	err := AltitudeProfile(nil, "Empty", out)
	if err == nil {
		t.Error("expected error for empty frame slice, got nil")
	}
}

func TestFlightTrackCreatesFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "track.png")

	frames := syntheticFrames(60)
	if err := FlightTrack(frames, "Test Flight", out); err != nil {
		t.Fatalf("FlightTrack: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("output not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("output PNG is empty")
	}
}

func TestFlightTrackNoGPSPoints(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "nogps.png")

	// All frames have zero GPS.
	frames := []telemetry.Frame{
		{SampleTime: 0, Lat: 0, Lon: 0},
		{SampleTime: time.Second, Lat: 0, Lon: 0},
	}

	err := FlightTrack(frames, "No GPS", out)
	if err == nil {
		t.Error("expected error when no GPS points available, got nil")
	}
}

func TestBuildAltPtsDownsample(t *testing.T) {
	// 30 frames within 0.2 s → all fall in the same bucket → 1 point.
	var frames []telemetry.Frame
	for i := 0; i < 30; i++ {
		frames = append(frames, telemetry.Frame{
			SampleTime:  time.Duration(i) * (time.Second / 150), // ~0.007s each
			AltAbsolute: float64(50 + i),
			AltRelative: float64(10 + i),
		})
	}
	asl, agl := buildAltPts(frames)
	if len(asl) != 1 {
		t.Errorf("expected 1 downsampled ASL point, got %d", len(asl))
	}
	if len(agl) != 1 {
		t.Errorf("expected 1 downsampled AGL point, got %d", len(agl))
	}
}

func TestFillPolygonEmpty(t *testing.T) {
	result := fillPolygon(nil)
	if result != nil {
		t.Errorf("fillPolygon(nil) = %v, want nil", result)
	}
}

func TestHaversineM(t *testing.T) {
	// 1 degree of latitude ≈ 111 195 m.
	got := haversineM(0, 0, 1, 0)
	want := 111_195.0
	eps := 200.0
	if got < want-eps || got > want+eps {
		t.Errorf("haversineM(1 deg lat) = %.0f, want %.0f±%.0f", got, want, eps)
	}
}
