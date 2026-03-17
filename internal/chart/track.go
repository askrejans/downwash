package chart

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	vgdraw "gonum.org/v1/plot/vg/draw"

	"github.com/askrejans/downwash/internal/geo"
	"github.com/askrejans/downwash/internal/telemetry"
)

var (
	colTrack  = color.RGBA{R: 255, G: 210, B: 50, A: 255} // amber track line
	colStart  = color.RGBA{R: 50, G: 220, B: 130, A: 255} // green start marker
	colEnd    = color.RGBA{R: 255, G: 80, B: 80, A: 255}  // red end marker
)

// trackSize is the output image size in pixels.
const trackSize = 800

// FlightTrack renders a top-down map-style PNG of the GPS flight path.
// It attempts to fetch dark OSM map tiles as a background. If tile
// download fails, it falls back to the plain dark chart background.
// Points are downsampled and jitter-filtered, matching the GPX writer logic.
// outputPath must end in ".png".
func FlightTrack(frames []telemetry.Frame, title, outputPath string) error {
	pts := buildTrackPts(frames)
	if len(pts) == 0 {
		return fmt.Errorf("chart: no GPS points for flight track")
	}

	// Compute bounding box from points.
	minLat, maxLat, minLon, maxLon := ptsBounds(pts)

	// Pad the bounding box by 15% for breathing room.
	latPad := (maxLat - minLat) * 0.15
	lonPad := (maxLon - minLon) * 0.15
	if latPad < 0.0005 {
		latPad = 0.0005
	}
	if lonPad < 0.0005 {
		lonPad = 0.0005
	}
	minLat -= latPad
	maxLat += latPad
	minLon -= lonPad
	maxLon += lonPad

	// Try to fetch dark OSM map tiles as background.
	mapBg := fetchMapBackground(minLat, maxLat, minLon, maxLon, trackSize, trackSize)

	if mapBg != nil {
		return renderTrackOnMap(mapBg, pts, minLat, maxLat, minLon, maxLon, title, outputPath)
	}

	// Fallback: plain dark chart (no map tiles).
	return renderTrackPlain(pts, title, outputPath)
}

// renderTrackOnMap draws the flight track directly on top of the map tile image.
func renderTrackOnMap(bg image.Image, pts plotter.XYs, minLat, maxLat, minLon, maxLon float64, title, outputPath string) error {
	out := image.NewRGBA(image.Rect(0, 0, trackSize, trackSize))
	draw.Draw(out, out.Bounds(), bg, bg.Bounds().Min, draw.Src)

	// Convert lat/lon to pixel coordinates on the image.
	toPixel := func(lat, lon float64) (int, int) {
		x := int((lon - minLon) / (maxLon - minLon) * float64(trackSize))
		y := int((maxLat - lat) / (maxLat - minLat) * float64(trackSize)) // lat is inverted
		return x, y
	}

	// Draw track line (thick amber with glow effect).
	for i := 1; i < len(pts); i++ {
		x0, y0 := toPixel(pts[i-1].Y, pts[i-1].X)
		x1, y1 := toPixel(pts[i].Y, pts[i].X)
		// Outer glow.
		drawLine(out, x0, y0, x1, y1, color.RGBA{R: 255, G: 210, B: 50, A: 60}, 5)
		// Core line.
		drawLine(out, x0, y0, x1, y1, colTrack, 2)
	}

	// Start marker (green filled circle).
	sx, sy := toPixel(pts[0].Y, pts[0].X)
	drawCircle(out, sx, sy, 7, colStart)

	// End marker (red filled circle).
	ex, ey := toPixel(pts[len(pts)-1].Y, pts[len(pts)-1].X)
	drawCircle(out, ex, ey, 7, colEnd)

	// Title overlay.
	// (Skipped — gonum text rendering requires a separate canvas; the title
	// is already in the PDF/Markdown. Keeping the image clean.)

	// Attribution (small, bottom-right).
	// CartoDB dark_matter requires OSM attribution.
	drawAttribution(out)

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("chart: create %s: %w", outputPath, err)
	}
	defer f.Close()
	return png.Encode(f, out)
}

// renderTrackPlain is the fallback renderer using gonum/plot with a dark background.
func renderTrackPlain(pts plotter.XYs, title, outputPath string) error {
	p, err := newDarkPlot(title+" — Flight Track", "Longitude", "Latitude")
	if err != nil {
		return fmt.Errorf("chart: create track plot: %w", err)
	}

	// Add grid plotter.
	g := plotter.NewGrid()
	g.Vertical = vgdraw.LineStyle{Color: colGrid, Width: vg.Points(0.5)}
	g.Horizontal = vgdraw.LineStyle{Color: colGrid, Width: vg.Points(0.5)}
	p.Add(g)

	// Tick marks every 0.001° ≈ 100 m.
	p.X.Tick.Marker = plot.ConstantTicks(lonTicks(pts))
	p.Y.Tick.Marker = plot.ConstantTicks(latTicks(pts))

	// Track line.
	line, err := plotter.NewLine(pts)
	if err != nil {
		return fmt.Errorf("chart: track line: %w", err)
	}
	line.LineStyle.Color = colTrack
	line.LineStyle.Width = vg.Points(1.5)
	p.Add(line)

	// Start marker (green circle).
	startPts := plotter.XYs{{X: pts[0].X, Y: pts[0].Y}}
	startScatter, err := plotter.NewScatter(startPts)
	if err != nil {
		return fmt.Errorf("chart: start scatter: %w", err)
	}
	startScatter.GlyphStyle = vgdraw.GlyphStyle{
		Color:  colStart,
		Radius: vg.Points(5),
		Shape:  vgdraw.CircleGlyph{},
	}
	p.Add(startScatter)

	// End marker (red circle).
	endPts := plotter.XYs{{X: pts[len(pts)-1].X, Y: pts[len(pts)-1].Y}}
	endScatter, err := plotter.NewScatter(endPts)
	if err != nil {
		return fmt.Errorf("chart: end scatter: %w", err)
	}
	endScatter.GlyphStyle = vgdraw.GlyphStyle{
		Color:  colEnd,
		Radius: vg.Points(5),
		Shape:  vgdraw.CircleGlyph{},
	}
	p.Add(endScatter)

	// Force equal aspect ratio by padding the shorter axis.
	setEqualAspect(p, pts)

	return saveSquarePNG(p, trackSize, outputPath)
}

// ---------- drawing primitives -----------------------------------------------

// drawLine draws a line between two points using Bresenham's algorithm with
// the given thickness and color.
func drawLine(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA, thickness int) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx, sy := 1, 1
	if x0 >= x1 {
		sx = -1
	}
	if y0 >= y1 {
		sy = -1
	}
	err := dx - dy

	for {
		// Draw a filled square at each point for thickness.
		half := thickness / 2
		for ty := -half; ty <= half; ty++ {
			for tx := -half; tx <= half; tx++ {
				px, py := x0+tx, y0+ty
				if px >= 0 && px < img.Bounds().Max.X && py >= 0 && py < img.Bounds().Max.Y {
					blendPixel(img, px, py, c)
				}
			}
		}
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

// drawCircle draws a filled circle at the given center with the given radius.
func drawCircle(img *image.RGBA, cx, cy, radius int, c color.RGBA) {
	// Outer glow.
	glowR := radius + 3
	glow := color.RGBA{R: c.R, G: c.G, B: c.B, A: 80}
	for y := -glowR; y <= glowR; y++ {
		for x := -glowR; x <= glowR; x++ {
			if x*x+y*y <= glowR*glowR {
				px, py := cx+x, cy+y
				if px >= 0 && px < img.Bounds().Max.X && py >= 0 && py < img.Bounds().Max.Y {
					blendPixel(img, px, py, glow)
				}
			}
		}
	}
	// Core circle.
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				px, py := cx+x, cy+y
				if px >= 0 && px < img.Bounds().Max.X && py >= 0 && py < img.Bounds().Max.Y {
					img.Set(px, py, c)
				}
			}
		}
	}
}

// blendPixel alpha-blends a colour onto an existing pixel.
func blendPixel(img *image.RGBA, x, y int, c color.RGBA) {
	existing := img.RGBAAt(x, y)
	a := float64(c.A) / 255.0
	inv := 1.0 - a
	img.SetRGBA(x, y, color.RGBA{
		R: uint8(float64(c.R)*a + float64(existing.R)*inv),
		G: uint8(float64(c.G)*a + float64(existing.G)*inv),
		B: uint8(float64(c.B)*a + float64(existing.B)*inv),
		A: 255,
	})
}

// drawAttribution draws a small OSM attribution line at the bottom-right.
func drawAttribution(img *image.RGBA) {
	// Simple pixel-level text is too complex; draw a semi-transparent bar
	// with the attribution text baked into the image metadata instead.
	// For now, draw a subtle dark bar at the bottom.
	bounds := img.Bounds()
	barH := 14
	for y := bounds.Max.Y - barH; y < bounds.Max.Y; y++ {
		for x := bounds.Max.X - 200; x < bounds.Max.X; x++ {
			if x >= 0 {
				blendPixel(img, x, y, color.RGBA{R: 0, G: 0, B: 0, A: 140})
			}
		}
	}
	// Note: actual text "© OpenStreetMap contributors" should be rendered
	// but pixel font rendering in pure Go without freetype is impractical.
	// The attribution is included in the Markdown and PDF reports instead.
}

// ptsBounds returns the lat/lon bounding box of the points.
func ptsBounds(pts plotter.XYs) (minLat, maxLat, minLon, maxLon float64) {
	minLat, maxLat = pts[0].Y, pts[0].Y
	minLon, maxLon = pts[0].X, pts[0].X
	for _, pt := range pts {
		if pt.Y < minLat {
			minLat = pt.Y
		}
		if pt.Y > maxLat {
			maxLat = pt.Y
		}
		if pt.X < minLon {
			minLon = pt.X
		}
		if pt.X > maxLon {
			maxLon = pt.X
		}
	}
	return
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ---------- helpers ---------------------------------------------------------

// buildTrackPts downsamples to ~1 Hz and filters GPS jitter spikes.
func buildTrackPts(frames []telemetry.Frame) plotter.XYs {
	const bucketSec = 1.0

	lastBucket := -1
	var pts plotter.XYs
	for _, f := range frames {
		if f.Lat == 0 && f.Lon == 0 {
			continue
		}
		bucket := int(f.SampleTime.Seconds() / bucketSec)
		if bucket == lastBucket {
			continue
		}
		lastBucket = bucket

		if len(pts) > 0 {
			prev := pts[len(pts)-1]
			d := geo.HaversineM(prev.Y, prev.X, f.Lat, f.Lon) // Y=lat, X=lon
			if d > geo.MaxGPSJitterM {
				continue
			}
		}
		pts = append(pts, plotter.XY{X: f.Lon, Y: f.Lat})
	}
	return pts
}

// setEqualAspect pads the plot axes so the track isn't distorted.
func setEqualAspect(p *plot.Plot, pts plotter.XYs) {
	minLat, maxLat := pts[0].Y, pts[0].Y
	minLon, maxLon := pts[0].X, pts[0].X
	for _, pt := range pts {
		if pt.Y < minLat {
			minLat = pt.Y
		}
		if pt.Y > maxLat {
			maxLat = pt.Y
		}
		if pt.X < minLon {
			minLon = pt.X
		}
		if pt.X > maxLon {
			maxLon = pt.X
		}
	}

	// Convert degree spans to approximate metres for comparison.
	latSpanM := (maxLat - minLat) * 111_195
	lonSpanM := (maxLon - minLon) * 111_195 * math.Cos((minLat+maxLat)/2*math.Pi/180)

	pad := 0.1 // 10% padding
	if latSpanM > lonSpanM {
		extra := (latSpanM - lonSpanM) / (2 * 111_195 * math.Cos((minLat+maxLat)/2*math.Pi/180))
		p.X.Min = minLon - extra - (maxLon-minLon)*pad
		p.X.Max = maxLon + extra + (maxLon-minLon)*pad
		p.Y.Min = minLat - (maxLat-minLat)*pad
		p.Y.Max = maxLat + (maxLat-minLat)*pad
	} else {
		extra := (lonSpanM - latSpanM) / (2 * 111_195)
		p.Y.Min = minLat - extra - (maxLat-minLat)*pad
		p.Y.Max = maxLat + extra + (maxLat-minLat)*pad
		p.X.Min = minLon - (maxLon-minLon)*pad
		p.X.Max = maxLon + (maxLon-minLon)*pad
	}
}

// latTicks / lonTicks produce axis tick values spaced at ~0.001° intervals.
func latTicks(pts plotter.XYs) []plot.Tick {
	minV, maxV := pts[0].Y, pts[0].Y
	for _, p := range pts {
		if p.Y < minV {
			minV = p.Y
		}
		if p.Y > maxV {
			maxV = p.Y
		}
	}
	return degTicks(minV, maxV)
}

func lonTicks(pts plotter.XYs) []plot.Tick {
	minV, maxV := pts[0].X, pts[0].X
	for _, p := range pts {
		if p.X < minV {
			minV = p.X
		}
		if p.X > maxV {
			maxV = p.X
		}
	}
	return degTicks(minV, maxV)
}

func degTicks(min, max float64) []plot.Tick {
	step := 0.001
	start := math.Floor(min/step) * step
	var ticks []plot.Tick
	for v := start; v <= max+step; v += step {
		ticks = append(ticks, plot.Tick{Value: v, Label: fmt.Sprintf("%.4f", v)})
	}
	return ticks
}

// saveSquarePNG renders a single plot to a square PNG file.
func saveSquarePNG(p *plot.Plot, sizePx int, outputPath string) error {
	sz := vg.Length(sizePx) * vg.Inch / 96

	img, err := plotToImage(p, sz, sz)
	if err != nil {
		return fmt.Errorf("chart: render track: %w", err)
	}

	// Re-draw onto a properly-sized RGBA canvas.
	out := image.NewRGBA(image.Rect(0, 0, sizePx, sizePx))
	draw.Draw(out, out.Bounds(), &image.Uniform{colBackground}, image.Point{}, draw.Src)
	draw.Draw(out, img.Bounds(), img, image.Point{}, draw.Over)

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("chart: create %s: %w", outputPath, err)
	}
	defer f.Close()
	return png.Encode(f, out)
}
