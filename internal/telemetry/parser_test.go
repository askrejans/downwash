package telemetry

import (
	"math"
	"testing"
	"time"
)

// ── ParseSampleTime ──────────────────────────────────────────────────────────

func TestParseSampleTime(t *testing.T) {
	cases := []struct {
		input   string
		wantSec float64
	}{
		{"0 s", 0},
		{"0.03 s", 0.03},
		{"0.067 s", 0.067},
		{"1:30", 90},
		{"0:03:12", 192},
		{"1:00:00", 3600},
		{"0:00:00", 0},
		{"2:34:56", 9296},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseSampleTime(tc.input)
			if err != nil {
				t.Fatalf("ParseSampleTime(%q) unexpected error: %v", tc.input, err)
			}
			gotSec := got.Seconds()
			if math.Abs(gotSec-tc.wantSec) > 0.001 {
				t.Errorf("ParseSampleTime(%q) = %.4fs, want %.4fs",
					tc.input, gotSec, tc.wantSec)
			}
		})
	}
}

func TestParseSampleTimeErrors(t *testing.T) {
	bad := []string{"", "abc", "1:2:3:4", "x s"}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseSampleTime(s); err == nil {
				t.Errorf("ParseSampleTime(%q) expected error, got nil", s)
			}
		})
	}
}

// ── ParseDMSCoord ────────────────────────────────────────────────────────────

func TestParseDMSCoord(t *testing.T) {
	cases := []struct {
		input   string
		want    float64
		epsilon float64
	}{
		{`57 deg 9' 56.67" N`, 57.165742, 0.00001},
		{`24 deg 49' 28.79" E`, 24.824664, 0.00001},
		{`0 deg 0' 0.00" S`, 0.0, 0.00001},
		{`90 deg 0' 0.00" S`, -90.0, 0.00001},
		{`180 deg 0' 0.00" W`, -180.0, 0.00001},
		{`51 deg 30' 26.46" N`, 51.507350, 0.001},  // London
		{`0 deg 7' 39.96" W`, -0.127767, 0.0001},   // London (slight west)
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseDMSCoord(tc.input)
			if err != nil {
				t.Fatalf("ParseDMSCoord(%q) unexpected error: %v", tc.input, err)
			}
			if math.Abs(got-tc.want) > tc.epsilon {
				t.Errorf("ParseDMSCoord(%q) = %.7f, want %.7f (ε=%.6f)",
					tc.input, got, tc.want, tc.epsilon)
			}
		})
	}
}

func TestParseDMSCoordErrors(t *testing.T) {
	bad := []string{"", "not a coord", "57 9' 56.67\" N", `57 deg 9' 56.67" X`}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			if _, err := ParseDMSCoord(s); err == nil {
				t.Errorf("ParseDMSCoord(%q) expected error, got nil", s)
			}
		})
	}
}

// ── haversineM ───────────────────────────────────────────────────────────────

func TestHaversineM(t *testing.T) {
	cases := []struct {
		name             string
		lat1, lon1       float64
		lat2, lon2       float64
		wantM, epsilonM  float64
	}{
		// 1 degree of longitude at equator ≈ 111 195 m
		{"1deg lon at equator", 0, 0, 0, 1, 111_195, 200},
		// 1 degree of latitude ≈ 111 195 m everywhere
		{"1deg lat", 0, 0, 1, 0, 111_195, 200},
		// Same point → 0
		{"same point", 57.165742, 24.824664, 57.165742, 24.824664, 0, 0.001},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := haversineM(tc.lat1, tc.lon1, tc.lat2, tc.lon2)
			if math.Abs(got-tc.wantM) > tc.epsilonM {
				t.Errorf("haversineM(%.6f,%.6f → %.6f,%.6f) = %.2fm, want %.2f±%.2f",
					tc.lat1, tc.lon1, tc.lat2, tc.lon2, got, tc.wantM, tc.epsilonM)
			}
		})
	}
}

// ── ComputeStats ─────────────────────────────────────────────────────────────

func TestComputeStatsEmpty(t *testing.T) {
	s := ComputeStats(nil)
	if s.FrameCount != 0 {
		t.Errorf("empty slice: FrameCount = %d, want 0", s.FrameCount)
	}
}

func TestComputeStats(t *testing.T) {
	// Synthetic 3-frame flight: straight north 20 m, then another 20 m.
	// Steps are 20 m — below the 50 m jitter filter threshold.
	latPerM := 1.0 / 111_195.0 // degrees per metre at equator
	frames := []Frame{
		{SampleTime: 0, Lat: 0, Lon: 0, AltAbsolute: 100, AltRelative: 10},
		{SampleTime: 1 * time.Second, Lat: latPerM * 20, Lon: 0, AltAbsolute: 120, AltRelative: 30},
		{SampleTime: 2 * time.Second, Lat: latPerM * 40, Lon: 0, AltAbsolute: 110, AltRelative: 20},
	}

	s := ComputeStats(frames)

	if s.FrameCount != 3 {
		t.Errorf("FrameCount = %d, want 3", s.FrameCount)
	}
	if math.Abs(s.Duration.Seconds()-2) > 0.01 {
		t.Errorf("Duration = %v, want 2s", s.Duration)
	}
	if math.Abs(s.MaxAltASL-120) > 0.01 {
		t.Errorf("MaxAltASL = %.2f, want 120", s.MaxAltASL)
	}
	if math.Abs(s.MinAltASL-100) > 0.01 {
		t.Errorf("MinAltASL = %.2f, want 100", s.MinAltASL)
	}
	if math.Abs(s.MaxAltAGL-30) > 0.01 {
		t.Errorf("MaxAltAGL = %.2f, want 30", s.MaxAltAGL)
	}
	// Distance should be approximately 40 m (two 20 m steps).
	if math.Abs(s.DistanceM-40) > 3 {
		t.Errorf("DistanceM = %.2f, want ~40", s.DistanceM)
	}
	// AvgSpeed ≈ 20 m/s (40 m / 2 s)
	if math.Abs(s.AvgSpeedMS-20) > 3 {
		t.Errorf("AvgSpeedMS = %.2f, want ~20", s.AvgSpeedMS)
	}
}

func TestComputeStatsGPSJitterFiltered(t *testing.T) {
	// A teleportation spike (>50 m) should be excluded from distance.
	frames := []Frame{
		{SampleTime: 0, Lat: 0, Lon: 0, AltAbsolute: 50, AltRelative: 10},
		{SampleTime: time.Second, Lat: 10, Lon: 10, // 1570 km spike — should be ignored
			AltAbsolute: 50, AltRelative: 10},
		{SampleTime: 2 * time.Second, Lat: 0.0001, Lon: 0, // ~11 m — should count
			AltAbsolute: 50, AltRelative: 10},
	}

	s := ComputeStats(frames)
	// Only the last segment (~11 m) should contribute.
	if s.DistanceM > 100 {
		t.Errorf("GPS spike not filtered: DistanceM = %.2f, want < 100", s.DistanceM)
	}
}
