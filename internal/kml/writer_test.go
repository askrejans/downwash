package kml

import (
	"archive/zip"
	"encoding/xml"
	"github.com/askrejans/downwash/internal/telemetry"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCoordinateOrderAltitudeAndKMZ(t *testing.T) {
	frames := []telemetry.Frame{{Lat: 57, Lon: 24, AltAbsolute: 75}, {Lat: 57.00001, Lon: 24.00001, AltAbsolute: 76, SampleTime: time.Second}}
	data, err := Data(frames, "flight <&>")
	if err != nil {
		t.Fatal(err)
	}
	var doc document
	if err = xml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Document.Tracks[0].Line.Coordinates != "24,57,75 24.00001,57.00001,76" || doc.Document.Tracks[0].Line.Mode != "absolute" {
		t.Fatal(string(data))
	}
	path := filepath.Join(t.TempDir(), "flight.kmz")
	if err = WriteKMZ(frames, "flight <&>", path); err != nil {
		t.Fatal(err)
	}
	pack, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pack.Close()
	if len(pack.File) != 1 || pack.File[0].Name != "doc.kml" {
		t.Fatal("invalid KMZ")
	}
}
func TestMissingASLAndSignalLoss(t *testing.T) {
	a := &telemetry.Availability{GPS: true, AltRelative: true}
	frames := []telemetry.Frame{{Lat: 57, Lon: 24, AltRelative: 10, Available: a}, {Lat: 57.00001, Lon: 24.00001, SampleTime: time.Second, Available: a}, {SampleTime: 2 * time.Second}, {Lat: 57.00002, Lon: 24.00002, SampleTime: 3 * time.Second, Available: a}, {Lat: 57.00003, Lon: 24.00003, SampleTime: 4 * time.Second, Available: a}}
	data, err := Data(frames, "flight")
	if err != nil {
		t.Fatal(err)
	}
	var doc document
	xml.Unmarshal(data, &doc)
	if len(doc.Document.Tracks) != 2 {
		t.Fatal(string(data))
	}
	for _, track := range doc.Document.Tracks {
		if track.Line.Mode != "clampToGround" {
			t.Fatal("invented terrain-relative height")
		}
		if strings.Contains(track.Line.Coordinates, ",10") {
			t.Fatal("relative altitude used as ASL")
		}
	}
	if err = Write([]telemetry.Frame{{Lat: 900, Lon: 24}}, "bad", filepath.Join(t.TempDir(), "bad.kml")); err == nil {
		t.Fatal("accepted invalid GPS")
	}
}
