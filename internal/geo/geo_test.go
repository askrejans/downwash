package geo

import (
	"math"
	"testing"
)

func TestHaversineM(t *testing.T) {
	cases := []struct {
		name            string
		lat1, lon1      float64
		lat2, lon2      float64
		wantM, epsilonM float64
	}{
		{"1deg lon at equator", 0, 0, 0, 1, 111_195, 200},
		{"1deg lat", 0, 0, 1, 0, 111_195, 200},
		{"same point", 57.165742, 24.824664, 57.165742, 24.824664, 0, 0.001},
		{"short distance ~20m", 0, 0, 1.0 / 111_195.0 * 20, 0, 20, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := HaversineM(tc.lat1, tc.lon1, tc.lat2, tc.lon2)
			if math.Abs(got-tc.wantM) > tc.epsilonM {
				t.Errorf("HaversineM(%.6f,%.6f → %.6f,%.6f) = %.2fm, want %.2f±%.2f",
					tc.lat1, tc.lon1, tc.lat2, tc.lon2, got, tc.wantM, tc.epsilonM)
			}
		})
	}
}

func TestRound6(t *testing.T) {
	got := Round6(57.1657423456)
	want := 57.165742
	if math.Abs(got-want) > 1e-7 {
		t.Errorf("Round6 = %v, want %v", got, want)
	}
}

func TestMaxPlausibleSpeedMS(t *testing.T) {
	// Sanity check: the constant should be 40 m/s (144 km/h).
	if MaxPlausibleSpeedMS != 40.0 {
		t.Errorf("MaxPlausibleSpeedMS = %v, want 40.0", MaxPlausibleSpeedMS)
	}
	// 40 m/s should be faster than any consumer drone (DJI FPV max ~39 m/s).
	if MaxPlausibleSpeedMS < 39 {
		t.Error("MaxPlausibleSpeedMS too low for fast FPV drones")
	}
}

func TestRound2(t *testing.T) {
	got := Round2(123.456)
	want := 123.46
	if math.Abs(got-want) > 1e-3 {
		t.Errorf("Round2 = %v, want %v", got, want)
	}
}
