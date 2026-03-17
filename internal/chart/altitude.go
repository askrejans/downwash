// Package chart renders flight-telemetry charts as PNG images using
// gonum/plot with a dark aviation-style theme.
package chart

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	vgdraw "gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"

	"github.com/askrejans/downwash/internal/telemetry"
)

// ---------- palette ---------------------------------------------------------

var (
	colBackground = color.RGBA{R: 18, G: 18, B: 28, A: 255}   // near-black
	colGrid       = color.RGBA{R: 50, G: 50, B: 70, A: 255}   // dim grid lines
	colASL        = color.RGBA{R: 77, G: 166, B: 255, A: 255} // sky blue
	colAGL        = color.RGBA{R: 50, G: 220, B: 130, A: 255} // green
	colAxes       = color.RGBA{R: 200, G: 200, B: 220, A: 255}
	colTitle      = color.RGBA{R: 230, G: 230, B: 255, A: 255}
)

// ---------- AltitudeProfile -------------------------------------------------

// AltitudeProfile renders a two-panel PNG chart (ASL on top, AGL below)
// showing altitude over flight time. outputPath must end in ".png".
func AltitudeProfile(frames []telemetry.Frame, title, outputPath string) error {
	if len(frames) == 0 {
		return fmt.Errorf("chart: no frames to plot")
	}

	aslPts, aglPts := buildAltPts(frames)

	pASL, err := newDarkPlot(title+" — Altitude ASL", "Time (s)", "Altitude ASL (m)")
	if err != nil {
		return fmt.Errorf("chart: create ASL plot: %w", err)
	}
	if err := addLine(pASL, aslPts, colASL); err != nil {
		return fmt.Errorf("chart: ASL line: %w", err)
	}

	pAGL, err := newDarkPlot("Altitude AGL", "Time (s)", "Altitude AGL (m)")
	if err != nil {
		return fmt.Errorf("chart: create AGL plot: %w", err)
	}
	if err := addLine(pAGL, aglPts, colAGL); err != nil {
		return fmt.Errorf("chart: AGL line: %w", err)
	}

	return saveTwoPanel(pASL, pAGL, 1200, 600, outputPath)
}

// ---------- helpers ---------------------------------------------------------

// buildAltPts downsamples to ~5 Hz (every 0.2 s bucket) for readability.
func buildAltPts(frames []telemetry.Frame) (asl, agl plotter.XYs) {
	const bucketSec = 0.2
	lastBucket := -1

	for _, f := range frames {
		bucket := int(f.SampleTime.Seconds() / bucketSec)
		if bucket == lastBucket {
			continue
		}
		lastBucket = bucket
		t := f.SampleTime.Seconds()
		asl = append(asl, plotter.XY{X: t, Y: f.AltAbsolute})
		agl = append(agl, plotter.XY{X: t, Y: f.AltRelative})
	}
	return
}

// newDarkPlot creates a gonum plot styled with the dark aviation palette.
// Grid lines are added as a plotter.Grid so the axis structs are not set
// directly (the GridLineStyle field was removed in gonum/plot v0.14+).
func newDarkPlot(title, xLabel, yLabel string) (*plot.Plot, error) {
	p := plot.New()

	p.Title.Text = title
	p.Title.TextStyle.Color = colTitle
	p.Title.TextStyle.Font.Size = vg.Points(12)

	p.X.Label.Text = xLabel
	p.Y.Label.Text = yLabel
	for _, ax := range []*plot.Axis{&p.X, &p.Y} {
		ax.Label.TextStyle.Color = colAxes
		ax.LineStyle.Color = colAxes
		ax.Tick.LineStyle.Color = colAxes
		ax.Tick.Label.Color = colAxes
	}

	p.BackgroundColor = colBackground

	// Add grid as a separate plotter.
	g := plotter.NewGrid()
	g.Vertical = vgdraw.LineStyle{Color: colGrid, Width: vg.Points(0.5)}
	g.Horizontal = vgdraw.LineStyle{Color: colGrid, Width: vg.Points(0.5)}
	p.Add(g)

	return p, nil
}

// addLine draws a coloured line and a semi-transparent fill beneath it.
func addLine(p *plot.Plot, pts plotter.XYs, c color.RGBA) error {
	line, err := plotter.NewLine(pts)
	if err != nil {
		return err
	}
	line.LineStyle.Color = c
	line.LineStyle.Width = vg.Points(1.5)
	p.Add(line)

	// Closed polygon for the fill area.
	fill, err := plotter.NewPolygon(fillPolygon(pts))
	if err != nil {
		return err
	}
	fill.Color = color.RGBA{R: c.R, G: c.G, B: c.B, A: 35}
	fill.LineStyle.Width = 0
	p.Add(fill)

	return nil
}

// fillPolygon builds a closed plotter.XYs tracing the line and the zero baseline.
func fillPolygon(pts plotter.XYs) plotter.XYs {
	if len(pts) == 0 {
		return nil
	}
	out := make(plotter.XYs, 0, len(pts)+2)
	out = append(out, pts...)
	out = append(out, plotter.XY{X: pts[len(pts)-1].X, Y: 0})
	out = append(out, plotter.XY{X: pts[0].X, Y: 0})
	return out
}

// saveTwoPanel renders top and bottom plots stacked vertically into a single PNG.
func saveTwoPanel(top, bot *plot.Plot, widthPx, heightPx int, outputPath string) error {
	w := vg.Length(widthPx) * vg.Inch / 96
	h := vg.Length(heightPx/2) * vg.Inch / 96

	imgTop, err := plotToImage(top, w, h)
	if err != nil {
		return fmt.Errorf("chart: render top panel: %w", err)
	}
	imgBot, err := plotToImage(bot, w, h)
	if err != nil {
		return fmt.Errorf("chart: render bottom panel: %w", err)
	}

	combined := image.NewRGBA(image.Rect(0, 0, widthPx, heightPx))
	draw.Draw(combined, combined.Bounds(), &image.Uniform{colBackground}, image.Point{}, draw.Src)

	// Scale top panel to top half.
	topBounds := imgTop.Bounds()
	for y := 0; y < heightPx/2 && y < topBounds.Max.Y; y++ {
		for x := 0; x < widthPx && x < topBounds.Max.X; x++ {
			combined.Set(x, y, imgTop.At(x, y))
		}
	}
	// Scale bottom panel to bottom half.
	botBounds := imgBot.Bounds()
	for y := 0; y < heightPx/2 && y < botBounds.Max.Y; y++ {
		for x := 0; x < widthPx && x < botBounds.Max.X; x++ {
			combined.Set(x, y+heightPx/2, imgBot.At(x, y))
		}
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("chart: create %s: %w", outputPath, err)
	}
	defer f.Close()
	return png.Encode(f, combined)
}

// plotToImage renders a plot into an in-memory image.Image using vgimg.
func plotToImage(p *plot.Plot, w, h vg.Length) (image.Image, error) {
	c := vgimg.PngCanvas{Canvas: vgimg.New(w, h)}
	dc := vgdraw.New(c)
	p.Draw(dc)

	var buf bytes.Buffer
	if _, err := c.WriteTo(&buf); err != nil {
		return nil, fmt.Errorf("chart: encode PNG: %w", err)
	}
	return png.Decode(&buf)
}
