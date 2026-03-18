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

	"github.com/askrejans/downwash/internal/geo"
	"github.com/askrejans/downwash/internal/telemetry"
)

// ---------- palette ---------------------------------------------------------

var (
	colBackground = color.RGBA{R: 14, G: 17, B: 23, A: 255}   // rich dark
	colGrid       = color.RGBA{R: 36, G: 41, B: 52, A: 255}   // subtle grid
	colASL        = color.RGBA{R: 99, G: 155, B: 255, A: 255}  // soft steel blue
	colAGL        = color.RGBA{R: 56, G: 203, B: 137, A: 255}  // muted emerald
	colSpeed      = color.RGBA{R: 255, G: 180, B: 50, A: 255}  // warm amber
	colAxes       = color.RGBA{R: 140, G: 150, B: 175, A: 255} // soft grey-blue
	colTitle      = color.RGBA{R: 200, G: 210, B: 235, A: 255} // subtle light
)

// ---------- AltitudeProfile -------------------------------------------------

// AltitudeProfile renders a three-panel PNG chart (ASL top, AGL middle,
// Speed bottom) showing altitude and speed over flight time.
// outputPath must end in ".png".
func AltitudeProfile(frames []telemetry.Frame, title, outputPath string) error {
	if len(frames) == 0 {
		return fmt.Errorf("chart: no frames to plot")
	}

	aslPts, aglPts := buildAltPts(frames)
	spdPts := buildSpeedPts(frames)

	pASL, err := newDarkPlot(title+" \u2014 Altitude ASL", "Time (s)", "Altitude ASL (m)")
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

	pSpd, err := newDarkPlot("Ground Speed", "Time (s)", "Speed (km/h)")
	if err != nil {
		return fmt.Errorf("chart: create speed plot: %w", err)
	}
	if err := addLine(pSpd, spdPts, colSpeed); err != nil {
		return fmt.Errorf("chart: speed line: %w", err)
	}

	return saveThreePanel(pASL, pAGL, pSpd, 1600, 1200, outputPath)
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

// buildSpeedPts computes instantaneous ground speed (km/h) from GPS deltas,
// downsampled to ~1 Hz buckets for a smooth curve.
func buildSpeedPts(frames []telemetry.Frame) plotter.XYs {
	const bucketSec = 1.0
	var pts plotter.XYs

	lastBucket := -1
	var bucketFrame telemetry.Frame
	hasBucket := false

	for _, f := range frames {
		if f.Lat == 0 && f.Lon == 0 {
			continue
		}
		bucket := int(f.SampleTime.Seconds() / bucketSec)
		if bucket == lastBucket {
			continue
		}

		if hasBucket {
			dt := f.SampleTime.Seconds() - bucketFrame.SampleTime.Seconds()
			if dt > 0 {
				d := geo.HaversineM(bucketFrame.Lat, bucketFrame.Lon, f.Lat, f.Lon)
				if d < geo.MaxGPSJitterM {
					spd := d / dt // m/s
					if spd <= geo.MaxPlausibleSpeedMS {
						pts = append(pts, plotter.XY{X: f.SampleTime.Seconds(), Y: spd * 3.6}) // km/h
					}
				}
			}
		}

		bucketFrame = f
		lastBucket = bucket
		hasBucket = true
	}
	return pts
}

// newDarkPlot creates a gonum plot styled with the dark aviation palette.
func newDarkPlot(title, xLabel, yLabel string) (*plot.Plot, error) {
	p := plot.New()

	p.Title.Text = title
	p.Title.TextStyle.Color = colTitle
	p.Title.TextStyle.Font.Size = vg.Points(14)
	p.Title.Padding = vg.Points(8)

	p.X.Label.Text = xLabel
	p.Y.Label.Text = yLabel
	for _, ax := range []*plot.Axis{&p.X, &p.Y} {
		ax.Label.TextStyle.Color = colAxes
		ax.Label.TextStyle.Font.Size = vg.Points(11)
		ax.LineStyle.Color = colAxes
		ax.LineStyle.Width = vg.Points(0.8)
		ax.Tick.LineStyle.Color = colAxes
		ax.Tick.LineStyle.Width = vg.Points(0.5)
		ax.Tick.Label.Color = colAxes
		ax.Tick.Label.Font.Size = vg.Points(10)
	}

	p.BackgroundColor = colBackground

	// Subtle grid.
	g := plotter.NewGrid()
	g.Vertical = vgdraw.LineStyle{Color: colGrid, Width: vg.Points(0.5)}
	g.Horizontal = vgdraw.LineStyle{Color: colGrid, Width: vg.Points(0.5)}
	p.Add(g)

	return p, nil
}

// addLine draws a coloured line and a subtle fill beneath it.
// The fill is a dark, pre-mixed colour (not alpha-blended) to avoid
// gonum/plot's unreliable alpha compositing.
func addLine(p *plot.Plot, pts plotter.XYs, c color.RGBA) error {
	// Pre-mix fill colour: blend 12% of the line colour into the background.
	const mix = 0.12
	bg := colBackground
	fillCol := color.RGBA{
		R: uint8(float64(c.R)*mix + float64(bg.R)*(1-mix)),
		G: uint8(float64(c.G)*mix + float64(bg.G)*(1-mix)),
		B: uint8(float64(c.B)*mix + float64(bg.B)*(1-mix)),
		A: 255,
	}

	// Fill area first (behind the line).
	fill, err := plotter.NewPolygon(fillPolygon(pts))
	if err != nil {
		return err
	}
	fill.Color = fillCol
	fill.LineStyle.Width = 0
	p.Add(fill)

	// Main line on top.
	line, err := plotter.NewLine(pts)
	if err != nil {
		return err
	}
	line.LineStyle.Color = c
	line.LineStyle.Width = vg.Points(2.5)
	p.Add(line)

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

// saveThreePanel renders three plots stacked vertically into a single PNG.
func saveThreePanel(top, mid, bot *plot.Plot, widthPx, heightPx int, outputPath string) error {
	panelH := heightPx / 3
	w := vg.Length(widthPx) * vg.Inch / 96
	h := vg.Length(panelH) * vg.Inch / 96

	imgTop, err := plotToImage(top, w, h)
	if err != nil {
		return fmt.Errorf("chart: render top panel: %w", err)
	}
	imgMid, err := plotToImage(mid, w, h)
	if err != nil {
		return fmt.Errorf("chart: render middle panel: %w", err)
	}
	imgBot, err := plotToImage(bot, w, h)
	if err != nil {
		return fmt.Errorf("chart: render bottom panel: %w", err)
	}

	combined := image.NewRGBA(image.Rect(0, 0, widthPx, heightPx))
	draw.Draw(combined, combined.Bounds(), &image.Uniform{colBackground}, image.Point{}, draw.Src)

	panels := []struct {
		img    image.Image
		yStart int
	}{
		{imgTop, 0},
		{imgMid, panelH},
		{imgBot, panelH * 2},
	}
	for _, p := range panels {
		b := p.img.Bounds()
		for y := 0; y < panelH && y < b.Max.Y; y++ {
			for x := 0; x < widthPx && x < b.Max.X; x++ {
				combined.Set(x, y+p.yStart, p.img.At(x, y))
			}
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
