package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/askrejans/downwash/internal/telemetry"
)

// CSV writes every timed measurement, leaving unavailable fields empty.
func CSV(frames []telemetry.Frame, path string) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if e := file.Close(); err == nil {
			err = e
		}
	}()
	w := csv.NewWriter(file)
	header := []string{"time_s", "gps_time", "latitude_deg", "longitude_deg", "altitude_asl_m", "altitude_takeoff_relative_m", "roll_deg", "pitch_deg", "yaw_deg", "gimbal_pitch_deg", "gimbal_yaw_deg", "iso", "shutter_speed", "f_number", "color_temperature_k"}
	extraKeys := map[string]bool{}
	for _, f := range frames {
		for key := range f.Additional {
			extraKeys[key] = true
		}
	}
	extras := make([]string, 0, len(extraKeys))
	for key := range extraKeys {
		extras = append(extras, key)
	}
	sort.Strings(extras)
	safe := func(value string) string {
		if strings.ContainsAny(value[:min(len(value), 1)], "=+-@\t\r") {
			return "'" + value
		}
		return value
	}
	for _, key := range extras {
		header = append(header, safe(key))
	}
	if err = w.Write(header); err != nil {
		return err
	}
	number := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	for _, f := range frames {
		row := make([]string, len(header))
		row[0] = number(f.SampleTime.Seconds())
		if !f.GPSTime.IsZero() {
			row[1] = f.GPSTime.UTC().Format(time.RFC3339Nano)
		}
		has := func(flag bool) bool { return f.Available == nil || flag }
		if telemetry.ValidGPS(f.Lat, f.Lon) && (f.Available == nil || f.Available.GPS) {
			row[2] = number(f.Lat)
			row[3] = number(f.Lon)
		}
		a := telemetry.Availability{}
		if f.Available != nil {
			a = *f.Available
		}
		if has(a.AltASL) {
			row[4] = number(f.AltAbsolute)
		}
		if has(a.AltRelative) {
			row[5] = number(f.AltRelative)
		}
		if has(a.Attitude) {
			row[6] = number(f.Roll)
			row[7] = number(f.Pitch)
			row[8] = number(f.Yaw)
		}
		if has(a.Gimbal) {
			row[9] = number(f.GimbalPitch)
			row[10] = number(f.GimbalYaw)
		}
		if has(a.Camera) {
			if f.ISO > 0 {
				row[11] = strconv.Itoa(f.ISO)
			}
			row[12] = f.ShutterSpeed
			if f.FNumber > 0 {
				row[13] = number(f.FNumber)
			}
			if f.ColorTemperature > 0 {
				row[14] = strconv.Itoa(f.ColorTemperature)
			}
		}
		for i, key := range extras {
			if value, ok := f.Additional[key]; ok {
				if text, ok := value.(string); ok {
					row[15+i] = safe(text)
				} else {
					data, err := json.Marshal(value)
					if err != nil {
						return err
					}
					row[15+i] = string(data)
				}
			}
		}
		row[12] = safe(row[12])
		if err = w.Write(row); err != nil {
			return fmt.Errorf("report: write CSV: %w", err)
		}
	}
	w.Flush()
	return w.Error()
}
