package telemetry

import (
	"math"
	"strings"
	"unicode/utf8"
)

// Additional documented DJI fields retain their native meaning and units.
// Paths come from ExifTool's public DJI tag reference; unknown fields are ignored.
func djiAdditional(data []byte, protocol string) (map[string]any, error) {
	values := map[string]any{}
	addNumber := func(name string, path ...uint64) error {
		v, err := protoAt(data, path...)
		if err != nil {
			return err
		}
		n := v.number()
		if v.present && !math.IsNaN(n) && !math.IsInf(n, 0) {
			values[name] = n
		}
		return nil
	}
	addString := func(name string, path ...uint64) error {
		v, err := protoAt(data, path...)
		if err != nil {
			return err
		}
		if v.present && len(v.b) <= 256 && utf8.Valid(v.b) {
			s := strings.TrimSpace(string(v.b))
			if s != "" {
				values[name] = s
			}
		}
		return nil
	}
	if err := addString("serial_number", 1, 1, 5); err != nil {
		return nil, err
	}
	if protocol != "dvtm_Air3.proto" && protocol != "dvtm_Air3s.proto" {
		if err := addString("model", 1, 1, 10); err != nil {
			return nil, err
		}
	}
	if protocol != "dvtm_Mini5Pro.proto" {
		if err := addNumber("frame_number", 3, 1, 1); err != nil {
			return nil, err
		}
		info := uint64(3)
		if protocol == "dvtm_wm265e.proto" || protocol == "dvtm_pm320.proto" || protocol == "dvtm_wa345e.proto" {
			info = 2
		}
		for i, key := range []string{"frame_width_px", "frame_height_px", "frame_rate_fps"} {
			if err := addNumber(key, 2, info, uint64(i+1)); err != nil {
				return nil, err
			}
		}
	}
	if djiLayouts[protocol].gimbal {
		if err := addNumber("gimbal_roll_deg", 3, 4, 3, 2); err != nil {
			return nil, err
		}
		if value, ok := values["gimbal_roll_deg"].(float64); ok {
			values["gimbal_roll_deg"] = value / 10
		}
	}
	if protocol == "dvtm_wm265e.proto" || protocol == "dvtm_pm320.proto" {
		if err := addNumber("digital_zoom", 3, 2, 6, 1); err != nil {
			return nil, err
		}
	}
	if protocol == "dvtm_Mini4_Pro.proto" || protocol == "dvtm_Mavic4.proto" || protocol == "dvtm_Mini5Pro.proto" {
		if err := addNumber("sensor_temperature", 3, 2, 37, 1); err != nil {
			return nil, err
		}
	}
	switch protocol {
	case "dvtm_Air3.proto", "dvtm_Air3s.proto", "dvtm_dji_neo.proto", "dvtm_PP-101.proto", "dvtm_wm261.proto", "dvtm_wa345e.proto", "dvtm_Mavic4.proto":
		if err := addNumber("device_timestamp_s", 3, 1, 2); err != nil {
			return nil, err
		}
		if value, ok := values["device_timestamp_s"].(float64); ok {
			values["device_timestamp_s"] = value / 1e6
		}
	}
	return values, nil
}
