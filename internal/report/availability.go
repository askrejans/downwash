package report

import (
	"github.com/askrejans/downwash/internal/telemetry"
	"strings"
)

func measurementPresent(stats telemetry.FlightStats, label string) bool {
	if stats.Available == nil {
		return true
	}
	key := strings.ToUpper(label)
	switch {
	case strings.Contains(key, "SHUTTER"):
		return stats.ShutterSpeed != ""
	case strings.Contains(key, "ISO"):
		return stats.ISO > 0
	case strings.Contains(key, "F-NUMBER"), strings.Contains(key, "APERTURE"):
		return stats.FNumber > 0
	case strings.Contains(key, "COLOR TEMP"):
		return stats.ColorTemp > 0
	case strings.Contains(key, "ASL"):
		return stats.Available.AltASL
	case strings.Contains(key, "AGL"), strings.Contains(key, "TAKEOFF"), strings.Contains(key, "CLIMB"), strings.Contains(key, "DESCENT"):
		return stats.Available.AltRelative
	case strings.Contains(key, "ROLL"), strings.Contains(key, "PITCH"), strings.Contains(key, "YAW"):
		return stats.Available.Attitude
	case strings.Contains(key, "SPEED"):
		return stats.Available.GPS && stats.GPSPointCount > 1
	case strings.Contains(key, "DISTANCE"), strings.Contains(key, "FROM HOME"), strings.Contains(key, "POSITION"), strings.Contains(key, "LAT/LON"):
		return stats.Available.GPS
	default:
		return true
	}
}
func measuredValue(stats telemetry.FlightStats, label, value string) string {
	if !measurementPresent(stats, label) {
		return "N/A"
	}
	return value
}
func measuredRows(stats telemetry.FlightStats, rows []tableRow) []tableRow {
	result := make([]tableRow, len(rows))
	copy(result, rows)
	for i := range result {
		result[i].v = measuredValue(stats, result[i].k, result[i].v)
	}
	return result
}
