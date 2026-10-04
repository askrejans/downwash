// Package kml writes KML2.2 flight tracks and KMZ containers without map requests.
package kml

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/askrejans/downwash/internal/geo"
	"github.com/askrejans/downwash/internal/telemetry"
)

type document struct {
	XMLName  xml.Name `xml:"kml"`
	NS       string   `xml:"xmlns,attr"`
	Document folder   `xml:"Document"`
}
type folder struct {
	Name   string      `xml:"name"`
	Style  style       `xml:"Style"`
	Tracks []placemark `xml:"Placemark"`
}
type style struct {
	ID    string `xml:"id,attr"`
	Color string `xml:"LineStyle>color"`
	Width int    `xml:"LineStyle>width"`
}
type placemark struct {
	Name  string `xml:"name"`
	Style string `xml:"styleUrl"`
	Line  line   `xml:"LineString"`
}
type line struct {
	Mode        string `xml:"altitudeMode"`
	Coordinates string `xml:"coordinates"`
}

// Data encodes longitude,latitude[,ASL altitude] in the KML2.2 coordinate order.
// Takeoff-relative heights are never mislabelled as terrain-relative heights.
func Data(frames []telemetry.Frame, name string) ([]byte, error) {
	var segments [][]telemetry.Frame
	var segment []telemetry.Frame
	bucket := -1
	flush := func() {
		if len(segment) > 1 {
			segments = append(segments, segment)
		}
		segment = nil
		bucket = -1
	}
	for _, f := range frames {
		if !telemetry.ValidGPS(f.Lat, f.Lon) || (f.Available != nil && !f.Available.GPS) {
			flush()
			continue
		}
		b := int(f.SampleTime.Seconds())
		if b == bucket {
			continue
		}
		if len(segment) > 0 {
			previous := segment[len(segment)-1]
			seconds := (f.SampleTime - previous.SampleTime).Seconds()
			if seconds <= 0 || seconds > 5 || geo.HaversineM(previous.Lat, previous.Lon, f.Lat, f.Lon) > 40*seconds {
				flush()
			}
		}
		bucket = b
		segment = append(segment, f)
	}
	flush()
	if len(segments) == 0 {
		return nil, fmt.Errorf("kml: no continuous valid GPS track")
	}
	d := document{NS: "http://www.opengis.net/kml/2.2", Document: folder{Name: name, Style: style{ID: "flight", Color: "ffffb581", Width: 3}}}
	number := func(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
	for i, points := range segments {
		mode := "absolute"
		for _, f := range points {
			if f.Available != nil && !f.Available.AltASL {
				mode = "clampToGround"
				break
			}
		}
		var coords []string
		for _, f := range points {
			coordinate := number(f.Lon) + "," + number(f.Lat)
			if mode == "absolute" {
				coordinate += "," + number(f.AltAbsolute)
			}
			coords = append(coords, coordinate)
		}
		d.Document.Tracks = append(d.Document.Tracks, placemark{Name: fmt.Sprintf("%s · track %d", name, i+1), Style: "#flight", Line: line{Mode: mode, Coordinates: strings.Join(coords, " ")}})
	}
	data, err := xml.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), data...), nil
}
func Write(frames []telemetry.Frame, name, path string) error {
	data, err := Data(frames, name)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// WriteKMZ packages the same KML as doc.kml, without external resources.
func WriteKMZ(frames []telemetry.Frame, name, path string) (err error) {
	data, err := Data(frames, name)
	if err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if e := file.Close(); err == nil {
			err = e
		}
	}()
	pack := zip.NewWriter(file)
	entry, err := pack.Create("doc.kml")
	if err != nil {
		pack.Close()
		return err
	}
	if _, err = entry.Write(data); err != nil {
		pack.Close()
		return err
	}
	return pack.Close()
}
