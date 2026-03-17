//go:build ignore
// +build ignore

// generate.go produces sample output artefacts committed to the samples/
// directory. It uses entirely synthetic GPS data (a figure-8 path centred on
// 0°N 0°E) so no real flight locations are exposed.
//
// Run with:
//
//	go run ./samples/generate.go
package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"time"

	"github.com/askrejans/downwash/internal/chart"
	"github.com/askrejans/downwash/internal/gpx"
	"github.com/askrejans/downwash/internal/report"
	"github.com/askrejans/downwash/internal/telemetry"
)

func main() {
	outDir := "samples"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		log.Fatalf("create samples dir: %v", err)
	}

	frames := syntheticFlight()
	stats := telemetry.ComputeStats(frames)

	const name = "sample_flight"
	const codec = "hevc"

	// GPX track.
	gpxPath := outDir + "/" + name + "_track.gpx"
	if err := gpx.Write(frames, name, gpxPath); err != nil {
		log.Fatalf("gpx: %v", err)
	}
	fmt.Println("wrote", gpxPath)

	// Altitude chart.
	altPath := outDir + "/" + name + "_altitude.png"
	if err := chart.AltitudeProfile(frames, "Sample DJI Flight", altPath); err != nil {
		log.Fatalf("altitude chart: %v", err)
	}
	fmt.Println("wrote", altPath)

	// Track chart.
	trackPath := outDir + "/" + name + "_track.png"
	if err := chart.FlightTrack(frames, "Sample DJI Flight", trackPath); err != nil {
		log.Fatalf("track chart: %v", err)
	}
	fmt.Println("wrote", trackPath)

	// Markdown report.
	mdPath := outDir + "/" + name + "_report.md"
	if err := report.Markdown(stats, name, codec, mdPath); err != nil {
		log.Fatalf("markdown: %v", err)
	}
	fmt.Println("wrote", mdPath)

	// PDF briefing.
	pdfPath := outDir + "/" + name + "_briefing.pdf"
	if err := report.PDF(stats, name, codec, altPath, trackPath, pdfPath); err != nil {
		log.Fatalf("pdf: %v", err)
	}
	fmt.Println("wrote", pdfPath)

	fmt.Println("\nAll sample artefacts generated in", outDir+"/")
}

// syntheticFlight returns ~192 seconds of synthetic telemetry at ~30 fps,
// flying a figure-8 path centred at 0°N, 0°E at 120 m AGL, reaching up to
// 150 m AGL at the crossing point.
func syntheticFlight() []telemetry.Frame {
	const fps = 29.97
	const durationSec = 192 // 3m 12s
	const radius = 0.003    // ~330 m at equator
	const baseASL = 35.0
	const baseAGL = 0.0
	const maxAGL = 120.0

	// Synthetic GPS origin — generic, far from any real city.
	const originLat = 0.0
	const originLon = 0.0

	startTime := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)

	totalFrames := int(durationSec * fps)
	frames := make([]telemetry.Frame, 0, totalFrames)

	for i := 0; i < totalFrames; i++ {
		t := float64(i) / fps
		frac := t / durationSec // 0→1 over the flight

		// Figure-8 Lissajous path (a=1, b=2).
		theta := frac * 2 * math.Pi
		lat := originLat + radius*math.Sin(theta)
		lon := originLon + radius*math.Sin(2*theta)

		// Altitude: ramp up over first 10s, hold, descend last 10s.
		agl := altitudeRamp(t, durationSec, baseAGL, maxAGL)
		asl := baseASL + agl

		// Synthetic attitude.
		roll := 5.0 * math.Sin(theta)
		pitch := -3.0 * math.Cos(theta*0.7)
		yaw := math.Mod(t*18, 360) - 180 // slowly rotating

		frames = append(frames, telemetry.Frame{
			SampleTime:       time.Duration(float64(time.Second) * t),
			GPSTime:          startTime.Add(time.Duration(float64(time.Second) * t)),
			Lat:              lat,
			Lon:              lon,
			AltAbsolute:      asl,
			AltRelative:      agl,
			Roll:             roll,
			Pitch:            pitch,
			Yaw:              yaw,
			GimbalPitch:      -30.0,
			GimbalYaw:        0,
			ISO:              100,
			ShutterSpeed:     "1/500",
			FNumber:          1.7,
			ColorTemperature: 5500,
		})
	}
	return frames
}

// altitudeRamp produces a smooth altitude profile: climb → cruise → descend.
func altitudeRamp(t, total, base, peak float64) float64 {
	climbDur := 10.0
	descentStart := total - 10.0
	if t < climbDur {
		return base + (peak-base)*(t/climbDur)
	}
	if t > descentStart {
		prog := (t - descentStart) / (total - descentStart)
		return peak - (peak-base)*prog
	}
	// Gentle sine oscillation during cruise.
	cruise := (t - climbDur) / (descentStart - climbDur)
	return peak - 20*math.Sin(cruise*4*math.Pi)
}
