package chart

import (
	"image"
	"image/color"
	"testing"

	"gonum.org/v1/plot/plotter"
)

func TestLatLonToTile(t *testing.T) {
	// Known value: lat 57.17, lon 24.82 at zoom 14.
	x, y := latLonToTile(57.17, 24.82, 14)
	if x < 0 || y < 0 {
		t.Errorf("latLonToTile returned negative: x=%d y=%d", x, y)
	}
	// At zoom 14, tile x for lon ~24.82 should be ~9300-9400.
	if x < 9000 || x > 10000 {
		t.Errorf("latLonToTile x=%d, expected ~9300", x)
	}
}

func TestLatLonToPixel(t *testing.T) {
	px, py := latLonToPixel(0, 0, 1)
	// At zoom 1, (0,0) should be at the center of the 2-tile world = 256px.
	if px < 200 || px > 300 {
		t.Errorf("latLonToPixel(0,0,1) px=%.0f, expected ~256", px)
	}
	if py < 200 || py > 300 {
		t.Errorf("latLonToPixel(0,0,1) py=%.0f, expected ~256", py)
	}
}

func TestPickZoom(t *testing.T) {
	// Small area should get high zoom.
	z := pickZoom(57.16, 57.17, 24.82, 24.83, 800)
	if z < 13 || z > 18 {
		t.Errorf("pickZoom for small area = %d, expected 13-18", z)
	}

	// Large area should get low zoom.
	z = pickZoom(40.0, 60.0, -10.0, 30.0, 800)
	if z > 6 {
		t.Errorf("pickZoom for large area = %d, expected <= 6", z)
	}
}

func TestScaleImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			src.Set(x, y, color.RGBA{R: 128, G: 64, B: 32, A: 255})
		}
	}

	dst := scaleImage(src, 50, 50)
	if dst == nil {
		t.Fatal("scaleImage returned nil")
	}
	if dst.Bounds().Dx() != 50 || dst.Bounds().Dy() != 50 {
		t.Errorf("scaleImage size = %dx%d, want 50x50", dst.Bounds().Dx(), dst.Bounds().Dy())
	}
}

func TestScaleImageEmpty(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 0, 0))
	dst := scaleImage(src, 50, 50)
	if dst != nil {
		t.Error("scaleImage with empty source should return nil")
	}
}

func TestPtsBounds(t *testing.T) {
	pts := syntheticTrackPts()
	minLat, maxLat, minLon, maxLon := ptsBounds(pts)
	if minLat >= maxLat {
		t.Errorf("minLat=%f >= maxLat=%f", minLat, maxLat)
	}
	if minLon >= maxLon {
		t.Errorf("minLon=%f >= maxLon=%f", minLon, maxLon)
	}
}

func TestDrawLineNoPanic(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	// Should not panic with any coordinates.
	drawLine(img, 10, 10, 90, 90, color.RGBA{R: 255, A: 255}, 2)
	drawLine(img, -5, -5, 105, 105, color.RGBA{R: 255, A: 255}, 1) // out of bounds
}

func TestDrawCircleNoPanic(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	drawCircle(img, 50, 50, 10, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	drawCircle(img, 0, 0, 20, color.RGBA{R: 255, A: 255}) // edge case
}

func TestBlendPixel(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	img.Set(5, 5, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	blendPixel(img, 5, 5, color.RGBA{R: 255, G: 0, B: 0, A: 128})

	got := img.RGBAAt(5, 5)
	// Should be a blend of red onto grey.
	if got.R < 150 || got.G > 80 {
		t.Errorf("blendPixel result = %v, expected reddish blend", got)
	}
}

func TestFetchMapBackgroundNoPanic(t *testing.T) {
	// Should not panic for any input — result depends on network availability.
	_ = fetchMapBackground(-60, 60, -120, 120, 800, 800)
	_ = fetchMapBackground(57.16, 57.17, 24.82, 24.83, 100, 100)
}

func TestRenderTrackPlainFallback(t *testing.T) {
	pts := syntheticTrackPts()
	dir := t.TempDir()
	err := renderTrackPlain(pts, "Test", dir+"/test_track.png")
	if err != nil {
		t.Fatalf("renderTrackPlain: %v", err)
	}
}

func TestAbs(t *testing.T) {
	if abs(-5) != 5 {
		t.Error("abs(-5) != 5")
	}
	if abs(3) != 3 {
		t.Error("abs(3) != 3")
	}
	if abs(0) != 0 {
		t.Error("abs(0) != 0")
	}
}

// syntheticTrackPts creates a small set of test points.
func syntheticTrackPts() plotter.XYs {
	return plotter.XYs{
		{X: 24.820, Y: 57.160},
		{X: 24.821, Y: 57.161},
		{X: 24.822, Y: 57.162},
		{X: 24.823, Y: 57.161},
		{X: 24.824, Y: 57.160},
	}
}
