// Package gpx writes GPS Exchange Format (GPX 1.1) track files from
// extracted drone telemetry frames.
package gpx

import (
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/askrejans/downwash/internal/telemetry"
)

// maxGPSJitterM is the maximum per-segment distance (metres) before a GPS
// point is considered a jitter spike and dropped from the track.
const maxGPSJitterM = 50.0

// targetHz is the approximate trackpoint density to write (1 Hz = one point
// per second). Drone telemetry is captured at ~30 Hz; we downsample to avoid
// bloated GPX files without losing meaningful spatial resolution.
const targetHz = 1.0

// gpx11 is the GPX 1.1 namespace URI.
const gpx11 = "http://www.topografix.com/GPX/1/1"

// xmlHeader is written before the encoded XML.
const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

// ---------- XML schema types ------------------------------------------------

type gpxDoc struct {
	XMLName  xml.Name  `xml:"gpx"`
	Version  string    `xml:"version,attr"`
	Creator  string    `xml:"creator,attr"`
	Xmlns    string    `xml:"xmlns,attr"`
	Metadata *metadata `xml:"metadata,omitempty"`
	Trk      track     `xml:"trk"`
}

type metadata struct {
	Name string    `xml:"name,omitempty"`
	Desc string    `xml:"desc,omitempty"`
	Time time.Time `xml:"time,omitempty"`
}

type track struct {
	Name string    `xml:"name,omitempty"`
	Seg  trackSeg  `xml:"trkseg"`
}

type trackSeg struct {
	Points []trackPoint `xml:"trkpt"`
}

type trackPoint struct {
	Lat  float64   `xml:"lat,attr"`
	Lon  float64   `xml:"lon,attr"`
	Ele  float64   `xml:"ele,omitempty"`
	Time time.Time `xml:"time,omitempty"`
	Desc string    `xml:"desc,omitempty"`
}

// ---------- public API -------------------------------------------------------

// Write downsamples frames to ~1 Hz, filters GPS jitter spikes, and writes a
// GPX 1.1 track file to outputPath. trackName is embedded in the <trk><name>
// element and is typically the source video filename.
func Write(frames []telemetry.Frame, trackName, outputPath string) error {
	pts := buildPoints(frames)
	if len(pts) == 0 {
		return fmt.Errorf("gpx: no valid GPS points in %d frames", len(frames))
	}

	doc := gpxDoc{
		Version: "1.1",
		Creator: "downwash",
		Xmlns:   gpx11,
		Metadata: &metadata{
			Name: trackName,
			Desc: fmt.Sprintf("DJI drone flight track — %d trackpoints", len(pts)),
		},
		Trk: track{
			Name: trackName,
			Seg:  trackSeg{Points: pts},
		},
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("gpx: create %s: %w", outputPath, err)
	}
	defer f.Close()

	if _, err := f.WriteString(xmlHeader); err != nil {
		return fmt.Errorf("gpx: write header: %w", err)
	}

	enc := xml.NewEncoder(f)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("gpx: encode: %w", err)
	}
	return enc.Flush()
}

// ---------- helpers ----------------------------------------------------------

// buildPoints converts a raw frame slice into GPX trackpoints by:
//  1. Downsampling to ~targetHz (picking the first frame of each 1-second bucket).
//  2. Skipping frames where both Lat and Lon are zero (no GPS fix).
//  3. Dropping segments whose great-circle distance exceeds maxGPSJitterM.
func buildPoints(frames []telemetry.Frame) []trackPoint {
	if len(frames) == 0 {
		return nil
	}

	// Downsample: keep the first frame for each integer-second bucket.
	var sampled []telemetry.Frame
	lastBucket := -1
	for _, f := range frames {
		bucket := int(f.SampleTime.Seconds() / (1.0 / targetHz))
		if bucket != lastBucket {
			lastBucket = bucket
			sampled = append(sampled, f)
		}
	}

	var pts []trackPoint
	for i, f := range sampled {
		if f.Lat == 0 && f.Lon == 0 {
			continue // no GPS fix
		}

		// Jitter filter: skip if jump from the previous accepted point is huge.
		if len(pts) > 0 {
			prev := pts[len(pts)-1]
			d := haversineM(prev.Lat, prev.Lon, f.Lat, f.Lon)
			if d > maxGPSJitterM {
				continue
			}
		}
		_ = i

		pt := trackPoint{
			Lat: round6(f.Lat),
			Lon: round6(f.Lon),
			Ele: round2(f.AltAbsolute),
			Desc: fmt.Sprintf("AGL %.1fm | Roll %.1f° Pitch %.1f° Yaw %.1f°",
				f.AltRelative, f.Roll, f.Pitch, f.Yaw),
		}
		if !f.GPSTime.IsZero() {
			pt.Time = f.GPSTime.UTC()
		}
		pts = append(pts, pt)
	}
	return pts
}

func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6_371_000.0
	p := math.Pi / 180.0
	a := math.Sin((lat2-lat1)*p/2)*math.Sin((lat2-lat1)*p/2) +
		math.Cos(lat1*p)*math.Cos(lat2*p)*
			math.Sin((lon2-lon1)*p/2)*math.Sin((lon2-lon1)*p/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
func round2(v float64) float64 { return math.Round(v*100) / 100 }
