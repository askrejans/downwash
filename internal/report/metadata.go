package report

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/askrejans/downwash/internal/telemetry"
)

// MetadataJSON writes a structured JSON file containing the full flight
// statistics and per-frame telemetry data. This provides a machine-readable
// export of all extracted metadata for integration with other tools.
func MetadataJSON(
	frames []telemetry.Frame,
	stats telemetry.FlightStats,
	videoName, codec string,
	outputPath string,
) error {
	type frameRecord struct {
		TimeSec          float64 `json:"time_s"`
		GPSTime          string  `json:"gps_time,omitempty"`
		Lat              float64 `json:"lat"`
		Lon              float64 `json:"lon"`
		AltASL           float64 `json:"alt_asl_m"`
		AltAGL           float64 `json:"alt_agl_m"`
		Roll             float64 `json:"roll_deg"`
		Pitch            float64 `json:"pitch_deg"`
		Yaw              float64 `json:"yaw_deg"`
		GimbalPitch      float64 `json:"gimbal_pitch_deg"`
		GimbalYaw        float64 `json:"gimbal_yaw_deg"`
		ISO              int     `json:"iso,omitempty"`
		ShutterSpeed     string  `json:"shutter_speed,omitempty"`
		FNumber          float64 `json:"f_number,omitempty"`
		ColorTemperature int     `json:"color_temp_k,omitempty"`
	}

	type statsRecord struct {
		DurationSec    float64 `json:"duration_s"`
		DistanceM      float64 `json:"distance_m"`
		MaxSpeedMS     float64 `json:"max_speed_ms"`
		AvgSpeedMS     float64 `json:"avg_speed_ms"`
		MaxAltASL      float64 `json:"max_alt_asl_m"`
		MinAltASL      float64 `json:"min_alt_asl_m"`
		MaxAltAGL      float64 `json:"max_alt_agl_m"`
		MinAltAGL      float64 `json:"min_alt_agl_m"`
		AltGainM       float64 `json:"alt_gain_m"`
		AltLossM       float64 `json:"alt_loss_m"`
		MaxClimbMS     float64 `json:"max_climb_ms"`
		MaxDescentMS   float64 `json:"max_descent_ms"`
		MaxRoll        float64 `json:"max_roll_deg"`
		MaxPitch       float64 `json:"max_pitch_deg"`
		MaxYawRate     float64 `json:"max_yaw_rate_deg_s"`
		MaxHomeDist    float64 `json:"max_home_dist_m"`
		FrameCount     int     `json:"frame_count"`
		GPSPointCount  int     `json:"gps_point_count"`
		StartTime      string  `json:"start_time,omitempty"`
		EndTime        string  `json:"end_time,omitempty"`
		StartLat       float64 `json:"start_lat"`
		StartLon       float64 `json:"start_lon"`
		EndLat         float64 `json:"end_lat"`
		EndLon         float64 `json:"end_lon"`
		ISO            int     `json:"iso,omitempty"`
		ShutterSpeed   string  `json:"shutter_speed,omitempty"`
		FNumber        float64 `json:"f_number,omitempty"`
		ColorTemp      int     `json:"color_temp_k,omitempty"`
		Codec          string  `json:"codec,omitempty"`
	}

	type metadata struct {
		Version   string        `json:"version"`
		Source    string        `json:"source"`
		Generated string       `json:"generated"`
		Stats     statsRecord   `json:"stats"`
		Frames    []frameRecord `json:"frames"`
	}

	sr := statsRecord{
		DurationSec:   stats.Duration.Seconds(),
		DistanceM:     stats.DistanceM,
		MaxSpeedMS:    stats.MaxSpeedMS,
		AvgSpeedMS:    stats.AvgSpeedMS,
		MaxAltASL:     stats.MaxAltASL,
		MinAltASL:     stats.MinAltASL,
		MaxAltAGL:     stats.MaxAltAGL,
		MinAltAGL:     stats.MinAltAGL,
		AltGainM:      stats.AltGainM,
		AltLossM:      stats.AltLossM,
		MaxClimbMS:    stats.MaxClimbMS,
		MaxDescentMS:  stats.MaxDescentMS,
		MaxRoll:       stats.MaxRoll,
		MaxPitch:      stats.MaxPitch,
		MaxYawRate:    stats.MaxYawRate,
		MaxHomeDist:   stats.MaxHomeDist,
		FrameCount:    stats.FrameCount,
		GPSPointCount: stats.GPSPointCount,
		StartLat:      stats.StartLat,
		StartLon:      stats.StartLon,
		EndLat:        stats.EndLat,
		EndLon:        stats.EndLon,
		ISO:           stats.ISO,
		ShutterSpeed:  stats.ShutterSpeed,
		FNumber:       stats.FNumber,
		ColorTemp:     stats.ColorTemp,
		Codec:         codec,
	}
	if !stats.StartTime.IsZero() {
		sr.StartTime = stats.StartTime.UTC().Format(time.RFC3339)
	}
	if !stats.EndTime.IsZero() {
		sr.EndTime = stats.EndTime.UTC().Format(time.RFC3339)
	}

	fr := make([]frameRecord, 0, len(frames))
	for _, f := range frames {
		rec := frameRecord{
			TimeSec:          f.SampleTime.Seconds(),
			Lat:              f.Lat,
			Lon:              f.Lon,
			AltASL:           f.AltAbsolute,
			AltAGL:           f.AltRelative,
			Roll:             f.Roll,
			Pitch:            f.Pitch,
			Yaw:              f.Yaw,
			GimbalPitch:      f.GimbalPitch,
			GimbalYaw:        f.GimbalYaw,
			ISO:              f.ISO,
			ShutterSpeed:     f.ShutterSpeed,
			FNumber:          f.FNumber,
			ColorTemperature: f.ColorTemperature,
		}
		if !f.GPSTime.IsZero() {
			rec.GPSTime = f.GPSTime.UTC().Format(time.RFC3339)
		}
		fr = append(fr, rec)
	}

	doc := metadata{
		Version:   "1.0",
		Source:    videoName,
		Generated: time.Now().UTC().Format(time.RFC3339),
		Stats:    sr,
		Frames:   fr,
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("report: marshal metadata JSON: %w", err)
	}
	return os.WriteFile(outputPath, data, 0o644)
}
