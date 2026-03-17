package chart

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"

	"github.com/askrejans/downwash/internal/telemetry"
)

func TestBuildTrackPts(t *testing.T) {
	frames := syntheticFrames(60)
	pts := buildTrackPts(frames)
	if len(pts) == 0 {
		t.Fatal("buildTrackPts returned empty")
	}
	// At 30fps, 60 frames = 2 seconds, should get ~2 points at 1Hz.
	if len(pts) > 3 {
		t.Errorf("expected ~2 points after downsampling, got %d", len(pts))
	}
}

func TestBuildTrackPtsFiltersZeroGPS(t *testing.T) {
	frames := []telemetry.Frame{
		{SampleTime: 0, Lat: 0, Lon: 0},
		{SampleTime: time.Second, Lat: 0, Lon: 0},
	}
	pts := buildTrackPts(frames)
	if len(pts) != 0 {
		t.Errorf("expected 0 points for zero GPS, got %d", len(pts))
	}
}

func TestBuildTrackPtsJitterFilter(t *testing.T) {
	const latPerM = 1.0 / 111_195.0
	frames := []telemetry.Frame{
		{SampleTime: 0, Lat: latPerM * 5, Lon: 0},
		{SampleTime: time.Second, Lat: 10, Lon: 10},                      // jitter spike
		{SampleTime: 2 * time.Second, Lat: latPerM * 10, Lon: 0},         // close to start
	}
	pts := buildTrackPts(frames)
	for _, pt := range pts {
		if pt.Y > 1 || pt.X > 1 {
			t.Errorf("jitter spike not filtered: lat=%.4f lon=%.4f", pt.Y, pt.X)
		}
	}
}

func TestSetEqualAspect(t *testing.T) {
	p := plot.New()
	pts := plotter.XYs{
		{X: 24.82, Y: 57.16},
		{X: 24.83, Y: 57.17},
	}
	// Should not panic.
	setEqualAspect(p, pts)

	if p.X.Min >= p.X.Max {
		t.Error("X axis range is invalid after setEqualAspect")
	}
	if p.Y.Min >= p.Y.Max {
		t.Error("Y axis range is invalid after setEqualAspect")
	}
}

func TestSetEqualAspectWideTrack(t *testing.T) {
	p := plot.New()
	// Wide track (more longitude span than latitude).
	pts := plotter.XYs{
		{X: 24.80, Y: 57.165},
		{X: 24.85, Y: 57.166},
	}
	setEqualAspect(p, pts)
	if p.Y.Min >= p.Y.Max {
		t.Error("Y axis should be padded for wide track")
	}
}

func TestLatTicks(t *testing.T) {
	pts := plotter.XYs{
		{X: 24.82, Y: 57.160},
		{X: 24.83, Y: 57.165},
	}
	ticks := latTicks(pts)
	if len(ticks) == 0 {
		t.Error("latTicks returned empty")
	}
}

func TestLonTicks(t *testing.T) {
	pts := plotter.XYs{
		{X: 24.820, Y: 57.16},
		{X: 24.825, Y: 57.17},
	}
	ticks := lonTicks(pts)
	if len(ticks) == 0 {
		t.Error("lonTicks returned empty")
	}
}

func TestDegTicks(t *testing.T) {
	ticks := degTicks(57.160, 57.165)
	if len(ticks) == 0 {
		t.Error("degTicks returned empty")
	}
	for _, tick := range ticks {
		if tick.Label == "" {
			t.Error("tick should have a label")
		}
	}
}

func TestSaveSquarePNG(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "test.png")

	p := plot.New()
	p.Title.Text = "Test"
	err := saveSquarePNG(p, 200, out)
	if err != nil {
		t.Fatalf("saveSquarePNG: %v", err)
	}

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("PNG file is empty")
	}
}
