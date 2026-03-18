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
	baseLat := 57.0
	frames := []Frame{
		{SampleTime: 0, Lat: baseLat, Lon: 24.0, AltAbsolute: 100, AltRelative: 10},
		{SampleTime: 1 * time.Second, Lat: baseLat + latPerM*20, Lon: 24.0, AltAbsolute: 120, AltRelative: 30},
		{SampleTime: 2 * time.Second, Lat: baseLat + latPerM*40, Lon: 24.0, AltAbsolute: 110, AltRelative: 20},
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

func TestComputeStatsSingleFrame(t *testing.T) {
	frames := []Frame{
		{SampleTime: 0, Lat: 57.0, Lon: 24.0, AltAbsolute: 100, AltRelative: 50,
			ISO: 200, ShutterSpeed: "1/1000", FNumber: 2.8, ColorTemperature: 6000},
	}

	s := ComputeStats(frames)
	if s.FrameCount != 1 {
		t.Errorf("FrameCount = %d, want 1", s.FrameCount)
	}
	if s.Duration != 0 {
		t.Errorf("Duration = %v, want 0", s.Duration)
	}
	if s.DistanceM != 0 {
		t.Errorf("DistanceM = %.2f, want 0", s.DistanceM)
	}
	if s.ISO != 200 {
		t.Errorf("ISO = %d, want 200", s.ISO)
	}
	if s.ShutterSpeed != "1/1000" {
		t.Errorf("ShutterSpeed = %q, want 1/1000", s.ShutterSpeed)
	}
	if s.FNumber != 2.8 {
		t.Errorf("FNumber = %v, want 2.8", s.FNumber)
	}
	if s.ColorTemp != 6000 {
		t.Errorf("ColorTemp = %d, want 6000", s.ColorTemp)
	}
	if s.GPSPointCount != 1 {
		t.Errorf("GPSPointCount = %d, want 1", s.GPSPointCount)
	}
	if s.StartLat != 57.0 || s.EndLat != 57.0 {
		t.Errorf("StartLat/EndLat wrong")
	}
}

func TestComputeStatsZeroDt(t *testing.T) {
	// Two frames with same SampleTime — should not panic or divide by zero.
	frames := []Frame{
		{SampleTime: time.Second, Lat: 0.001, Lon: 0},
		{SampleTime: time.Second, Lat: 0.001, Lon: 0},
		{SampleTime: 2 * time.Second, Lat: 0.0011, Lon: 0},
	}
	s := ComputeStats(frames)
	if s.FrameCount != 3 {
		t.Errorf("FrameCount = %d, want 3", s.FrameCount)
	}
}

func TestComputeStatsGPSCount(t *testing.T) {
	frames := []Frame{
		{SampleTime: 0, Lat: 0, Lon: 0},     // zero GPS — should not count
		{SampleTime: time.Second, Lat: 1, Lon: 0}, // has GPS
		{SampleTime: 2 * time.Second, Lat: 0, Lon: 1}, // has GPS (lon only)
	}
	s := ComputeStats(frames)
	if s.GPSPointCount != 2 {
		t.Errorf("GPSPointCount = %d, want 2", s.GPSPointCount)
	}
}

func TestParseSampleTimeMMSSWithFraction(t *testing.T) {
	got, err := ParseSampleTime("1:30.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(got.Seconds()-90.5) > 0.01 {
		t.Errorf("got %.2fs, want 90.5s", got.Seconds())
	}
}

func TestParseSampleTimeLeadingWhitespace(t *testing.T) {
	got, err := ParseSampleTime("  0.5 s  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(got.Seconds()-0.5) > 0.01 {
		t.Errorf("got %.2fs, want 0.5s", got.Seconds())
	}
}

func TestParseDMSCoordLeadingWhitespace(t *testing.T) {
	got, err := ParseDMSCoord(`  57 deg 9' 56.67" N  `)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(got-57.165742) > 0.001 {
		t.Errorf("got %.6f, want ~57.165742", got)
	}
}

func TestComputeStatsGPSJitterFiltered(t *testing.T) {
	// A teleportation spike (>50 m) should be excluded from distance.
	frames := []Frame{
		{SampleTime: 0, Lat: 57.0, Lon: 24.0, AltAbsolute: 50, AltRelative: 10},
		{SampleTime: time.Second, Lat: 10, Lon: 10, // ~5500 km spike — should be ignored
			AltAbsolute: 50, AltRelative: 10},
		{SampleTime: 2 * time.Second, Lat: 57.0001, Lon: 24.0, // ~11 m — should count
			AltAbsolute: 50, AltRelative: 10},
	}

	s := ComputeStats(frames)
	// Only the last segment (~11 m) should contribute.
	if s.DistanceM > 100 {
		t.Errorf("GPS spike not filtered: DistanceM = %.2f, want < 100", s.DistanceM)
	}
}

func TestComputeStatsNewFields(t *testing.T) {
	// Synthetic flight: climb 20m, descend 10m, with roll/pitch.
	latPerM := 1.0 / 111_195.0
	baseLat := 57.0
	frames := []Frame{
		{SampleTime: 0, Lat: baseLat, Lon: 24.0, AltAbsolute: 100, AltRelative: 10,
			Roll: 5, Pitch: -3, Yaw: 0},
		{SampleTime: 1 * time.Second, Lat: baseLat + latPerM*20, Lon: 24.0, AltAbsolute: 120, AltRelative: 30,
			Roll: -15, Pitch: 10, Yaw: 45},
		{SampleTime: 2 * time.Second, Lat: baseLat + latPerM*40, Lon: 24.0, AltAbsolute: 110, AltRelative: 20,
			Roll: 25, Pitch: -8, Yaw: 90},
	}

	s := ComputeStats(frames)

	// Altitude gain: 20m (10→30), loss: 10m (30→20).
	if math.Abs(s.AltGainM-20) > 0.1 {
		t.Errorf("AltGainM = %.2f, want ~20", s.AltGainM)
	}
	if math.Abs(s.AltLossM-10) > 0.1 {
		t.Errorf("AltLossM = %.2f, want ~10", s.AltLossM)
	}

	// Max climb rate: 20 m/s (20m in 1s).
	if s.MaxClimbMS < 15 {
		t.Errorf("MaxClimbMS = %.2f, want >= 15", s.MaxClimbMS)
	}

	// Max descent rate: 10 m/s (10m in 1s).
	if s.MaxDescentMS < 5 {
		t.Errorf("MaxDescentMS = %.2f, want >= 5", s.MaxDescentMS)
	}

	// Max roll: 25 (absolute).
	if math.Abs(s.MaxRoll-25) > 0.1 {
		t.Errorf("MaxRoll = %.2f, want 25", s.MaxRoll)
	}

	// Max pitch: 10 (absolute).
	if math.Abs(s.MaxPitch-10) > 0.1 {
		t.Errorf("MaxPitch = %.2f, want 10", s.MaxPitch)
	}

	// Max home distance: ~40m (latPerM * 40).
	if s.MaxHomeDist < 30 || s.MaxHomeDist > 50 {
		t.Errorf("MaxHomeDist = %.2f, want ~40", s.MaxHomeDist)
	}

	// Yaw rate should be nonzero (45 deg/s).
	if s.MaxYawRate < 40 {
		t.Errorf("MaxYawRate = %.2f, want >= 40", s.MaxYawRate)
	}
}

func TestComputeStatsSpeedFilter(t *testing.T) {
	// With 1 Hz downsampling, GPS noise within a single second bucket is
	// naturally filtered out. Test that the jitter filter still catches
	// implausible inter-bucket speeds.
	latPerM := 1.0 / 111_195.0
	baseLat := 57.0
	frames := []Frame{
		{SampleTime: 0, Lat: baseLat, Lon: 24.0, AltAbsolute: 50, AltRelative: 10},
		// Normal: 10 m in 1 s = 10 m/s.
		{SampleTime: 1 * time.Second, Lat: baseLat + latPerM*10, Lon: 24.0,
			AltAbsolute: 50, AltRelative: 10},
		// Another 10 m in 1 s.
		{SampleTime: 2 * time.Second, Lat: baseLat + latPerM*20, Lon: 24.0,
			AltAbsolute: 50, AltRelative: 10},
	}

	s := ComputeStats(frames)
	// Max speed should be ~10 m/s.
	if s.MaxSpeedMS > 15 {
		t.Errorf("speed filter failed: MaxSpeedMS = %.2f, want < 15", s.MaxSpeedMS)
	}
	// Distance should be ~20 m.
	if math.Abs(s.DistanceM-20) > 3 {
		t.Errorf("DistanceM = %.2f, want ~20", s.DistanceM)
	}
}
