package telemetry

import (
	"math"
	"testing"
)

func TestParseFloat(t *testing.T) {
	cases := []struct {
		input string
		want  float64
	}{
		{"123.45", 123.45},
		{" 123.45 ", 123.45},
		{"", 0},
		{"abc", 0},
		{"-10.5", -10.5},
		{"0", 0},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := parseFloat(tc.input)
			if math.Abs(got-tc.want) > 0.001 {
				t.Errorf("parseFloat(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseCSVLineValid(t *testing.T) {
	// A realistic exiftool CSV line with 15 fields.
	line := `0.03 s,2025:06:15 10:00:01Z,57 deg 9' 56.67" N,24 deg 49' 28.79" E,155.0,120.3,1.5,-0.8,180.0,5.2,3.1,100,1/500,1.7,5500`

	f, err := parseCSVLine(line)
	if err != nil {
		t.Fatalf("parseCSVLine() error: %v", err)
	}

	if math.Abs(f.SampleTime.Seconds()-0.03) > 0.001 {
		t.Errorf("SampleTime = %v, want 0.03s", f.SampleTime)
	}
	if math.Abs(f.Lat-57.165742) > 0.001 {
		t.Errorf("Lat = %v, want ~57.165742", f.Lat)
	}
	if math.Abs(f.Lon-24.824664) > 0.001 {
		t.Errorf("Lon = %v, want ~24.824664", f.Lon)
	}
	if math.Abs(f.AltAbsolute-155.0) > 0.01 {
		t.Errorf("AltAbsolute = %v, want 155.0", f.AltAbsolute)
	}
	if math.Abs(f.AltRelative-120.3) > 0.01 {
		t.Errorf("AltRelative = %v, want 120.3", f.AltRelative)
	}
	if f.ISO != 100 {
		t.Errorf("ISO = %d, want 100", f.ISO)
	}
	if f.ShutterSpeed != "1/500" {
		t.Errorf("ShutterSpeed = %q, want 1/500", f.ShutterSpeed)
	}
	if math.Abs(f.FNumber-1.7) > 0.01 {
		t.Errorf("FNumber = %v, want 1.7", f.FNumber)
	}
	if f.ColorTemperature != 5500 {
		t.Errorf("ColorTemperature = %d, want 5500", f.ColorTemperature)
	}
}

func TestParseCSVLineTooFewFields(t *testing.T) {
	_, err := parseCSVLine("a,b,c")
	if err == nil {
		t.Error("expected error for too few fields, got nil")
	}
}

func TestParseCSVLineBadSampleTime(t *testing.T) {
	line := `bad_time,2025:06:15 10:00:01Z,57 deg 9' 56.67" N,24 deg 49' 28.79" E,155.0,120.3,1.5,-0.8,180.0,5.2,3.1,100,1/500,1.7,5500`
	_, err := parseCSVLine(line)
	if err == nil {
		t.Error("expected error for bad sample time, got nil")
	}
}

func TestParseCSVLineBadCoord(t *testing.T) {
	line := `0.03 s,2025:06:15 10:00:01Z,bad_lat,24 deg 49' 28.79" E,155.0,120.3,1.5,-0.8,180.0,5.2,3.1,100,1/500,1.7,5500`
	_, err := parseCSVLine(line)
	if err == nil {
		t.Error("expected error for bad latitude, got nil")
	}
}

func TestParseCSVLineBlankNumericFields(t *testing.T) {
	// DJI sometimes writes blank numeric fields — parseFloat should return 0.
	line := `0.03 s,2025:06:15 10:00:01Z,57 deg 9' 56.67" N,24 deg 49' 28.79" E, , , , , , , , , ,1.7, `

	f, err := parseCSVLine(line)
	if err != nil {
		t.Fatalf("parseCSVLine() with blanks error: %v", err)
	}
	if f.AltAbsolute != 0 {
		t.Errorf("AltAbsolute = %v, want 0 for blank field", f.AltAbsolute)
	}
	if f.ISO != 0 {
		t.Errorf("ISO = %d, want 0 for blank field", f.ISO)
	}
}
