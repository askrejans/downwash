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

	"github.com/askrejans/downwash/internal/telemetry"
)

var (
	colTrack  = color.RGBA{R: 255, G: 210, B: 50, A: 255} // amber track line
	colStart  = color.RGBA{R: 50, G: 220, B: 130, A: 255} // green start marker
	colEnd    = color.RGBA{R: 255, G: 80, B: 80, A: 255}  // red end marker
)

// FlightTrack renders a top-down map-style PNG of the GPS flight path.
// Points are downsampled and jitter-filtered, matching the GPX writer logic.
// outputPath must end in ".png".
func FlightTrack(frames []telemetry.Frame, title, outputPath string) error {
	pts := buildTrackPts(frames)
	if len(pts) == 0 {
		return fmt.Errorf("chart: no GPS points for flight track")
	}

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

	return saveSquarePNG(p, 800, outputPath)
}

// ---------- helpers ---------------------------------------------------------

// buildTrackPts downsamples to ~1 Hz and filters GPS jitter spikes.
func buildTrackPts(frames []telemetry.Frame) plotter.XYs {
	const bucketSec = 1.0
	const maxJitter = 50.0 // metres

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
			d := haversineM(prev.Y, prev.X, f.Lat, f.Lon) // Y=lat, X=lon
			if d > maxJitter {
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

// haversineM returns the great-circle distance in metres between two WGS-84 points.
func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6_371_000.0
	p := math.Pi / 180.0
	a := math.Sin((lat2-lat1)*p/2)*math.Sin((lat2-lat1)*p/2) +
		math.Cos(lat1*p)*math.Cos(lat2*p)*
			math.Sin((lon2-lon1)*p/2)*math.Sin((lon2-lon1)*p/2)
	return 2 * R * math.Asin(math.Sqrt(a))
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
