// Package geo provides shared geodesy helpers used across the downwash
// pipeline: haversine distance, coordinate rounding, and the GPS jitter
// threshold constant.
package geo

import "math"

// MaxGPSJitterM is the maximum great-circle distance (metres) between
// consecutive GPS samples before the point is considered a jitter spike and
// dropped. The value tolerates drone speeds up to ~150 km/h at ~30 Hz
// (~1.4 m/frame) while excluding multi-kilometre teleportation artefacts
// common at the start/end of DJI flights.
const MaxGPSJitterM = 50.0

// MaxPlausibleSpeedMS is the maximum instantaneous speed (m/s) considered
// physically plausible for a consumer/prosumer drone. Frame pairs that
// produce a higher speed are treated as GPS acquisition noise and excluded
// from distance and speed statistics. 40 m/s ≈ 144 km/h covers the fastest
// DJI FPV drone (~39 m/s in full manual mode) while filtering GPS noise.
const MaxPlausibleSpeedMS = 40.0

// HaversineM returns the great-circle distance in metres between two
// WGS-84 points specified in decimal degrees.
func HaversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6_371_000.0
	p := math.Pi / 180.0
	a := math.Sin((lat2-lat1)*p/2)*math.Sin((lat2-lat1)*p/2) +
		math.Cos(lat1*p)*math.Cos(lat2*p)*
			math.Sin((lon2-lon1)*p/2)*math.Sin((lon2-lon1)*p/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}

// Round6 rounds a float64 to 6 decimal places (~0.11 m precision for
// latitude/longitude in degrees).
func Round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// Round2 rounds a float64 to 2 decimal places.
func Round2(v float64) float64 { return math.Round(v*100) / 100 }
