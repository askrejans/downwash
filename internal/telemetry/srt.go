package telemetry

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	srtTimeRE   = regexp.MustCompile(`^(\d{2,}):(\d{2}):(\d{2})[,.](\d{3})\s*-->`)
	srtKeyRE    = regexp.MustCompile(`(?i)\b(latitude|longitude|longtitude|rel_alt|abs_alt|drone_roll|drone_pitch|drone_yaw|gb_pitch|gb_yaw|gimbal_pitch|gimbal_yaw|iso|fnum|ct|shutter)\s*:\s*([^\]\s,]+)`)
	srtExtraRE  = regexp.MustCompile(`(?i)\b([a-z][a-z0-9_]*)\s*:\s*([^\]\s,<>]+)`)
	srtGPSRE    = regexp.MustCompile(`(?i)\bGPS\s*\(\s*([-+\d.]+)\s*,\s*([-+\d.]+)\s*,\s*([-+\d.]+)[A-Za-z]*\s*\)`)
	srtBaroRE   = regexp.MustCompile(`(?i)\bBAROMETER\s*[:(]\s*([-+\d.]+)`)
	srtLegacyRE = regexp.MustCompile(`(?i)(?:^|[,\s])(F/|SS|ISO|EV|H\.S|V\.S|H|D)\s*([-+]?\d+(?:\.\d+)?(?:/\d+)?)`)
	srtAnglesRE = regexp.MustCompile(`(?i)\b([FG])\.PRY\s*\(\s*([-+\d.]+)[°\s]*,\s*([-+\d.]+)[°\s]*,\s*([-+\d.]+)[°\s]*\)`)
)

// ParseSRT reads DJI bracketed telemetry subtitles, including DJI's documented
// "longtitude" spelling and documented longitude/latitude legacy GPS tuples.
// Ordinary subtitles are rejected.
func ParseSRT(r io.Reader) ([]Frame, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var frames []Frame
	var stamp time.Duration
	var text strings.Builder
	hasStamp := false
	var subtitleErr error
	flush := func() {
		if !hasStamp {
			return
		}
		values := make(map[string]string)
		for _, match := range srtKeyRE.FindAllStringSubmatch(text.String(), -1) {
			values[strings.ToLower(match[1])] = match[2]
		}
		additional := map[string]any{}
		for _, match := range srtLegacyRE.FindAllStringSubmatch(text.String(), -1) {
			key, value := strings.ToUpper(match[1]), match[2]
			switch key {
			case "F/":
				values["fnum"] = value
			case "SS":
				if parseFloat(value) > 0 {
					values["shutter"] = "1/" + value
				}
			case "ISO":
				values["iso"] = value
			case "H":
				values["rel_alt"] = value
			case "D":
				additional["distance_home_m"] = parseFloat(value)
			case "H.S":
				additional["horizontal_speed_ms"] = parseFloat(value)
			case "V.S":
				additional["vertical_speed_ms"] = parseFloat(value)
			case "EV":
				parts := strings.Split(value, "/")
				exposure := parseFloat(parts[0])
				if len(parts) == 2 && parseFloat(parts[1]) != 0 {
					exposure /= parseFloat(parts[1])
				}
				additional["exposure_compensation_ev"] = exposure
			}
		}
		for _, match := range srtAnglesRE.FindAllStringSubmatch(text.String(), -1) {
			if strings.ToUpper(match[1]) == "F" {
				values["drone_pitch"], values["drone_roll"], values["drone_yaw"] = match[2], match[3], match[4]
			} else {
				values["gimbal_pitch"], values["gimbal_yaw"] = match[2], match[4]
				additional["gimbal_roll_deg"] = parseFloat(match[3])
			}
		}
		lonText := values["longitude"]
		if lonText == "" {
			lonText = values["longtitude"]
		}
		lat, latErr := strconv.ParseFloat(values["latitude"], 64)
		lon, lonErr := strconv.ParseFloat(lonText, 64)
		if latErr != nil || lonErr != nil {
			match := srtGPSRE.FindStringSubmatch(text.String())
			if match == nil {
				if len(values) == 0 {
					return
				}
				lat, lon = 0, 0
			} else {
				first, firstErr := strconv.ParseFloat(match[1], 64)
				second, secondErr := strconv.ParseFloat(match[2], 64)
				if firstErr != nil || secondErr != nil {
					return
				}
				// DJI documents the tuple as longitude, latitude, altitude.
				lon, lat = first, second
				if math.Abs(lat) > 90 || math.Abs(lon) > 180 {
					subtitleErr = fmt.Errorf("telemetry: invalid legacy GPS coordinates")
					return
				}
				values["abs_alt"] = match[3]
				if baro := srtBaroRE.FindStringSubmatch(text.String()); baro != nil {
					values["rel_alt"] = baro[1]
				}
			}
		}
		if !ValidGPS(lat, lon) {
			lat, lon = 0, 0
		}
		f := Frame{SampleTime: stamp, Lat: lat, Lon: lon,
			AltAbsolute: parseFloat(values["abs_alt"]), AltRelative: parseFloat(values["rel_alt"]),
			Roll: parseFloat(values["drone_roll"]), Pitch: parseFloat(values["drone_pitch"]), Yaw: parseFloat(values["drone_yaw"]),
			GimbalPitch: parseFloat(values["gb_pitch"]), GimbalYaw: parseFloat(values["gb_yaw"]),
			ISO: int(parseFloat(values["iso"])), ShutterSpeed: values["shutter"],
			FNumber: parseFloat(values["fnum"]), ColorTemperature: int(parseFloat(values["ct"])),
			Additional: additional,
		}
		hasNumber := func(keys ...string) bool {
			for _, key := range keys {
				v, err := strconv.ParseFloat(values[key], 64)
				if err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
					return true
				}
			}
			return false
		}
		f.Available = &Availability{GPS: ValidGPS(lat, lon), AltASL: hasNumber("abs_alt"), AltRelative: hasNumber("rel_alt"), Attitude: hasNumber("drone_roll", "drone_pitch", "drone_yaw"), Gimbal: hasNumber("gb_pitch", "gb_yaw", "gimbal_pitch", "gimbal_yaw"), Camera: f.ISO > 0 || f.ShutterSpeed != "" || f.FNumber > 0 || f.ColorTemperature > 0}
		if value, ok := values["gimbal_pitch"]; ok {
			f.GimbalPitch = parseFloat(value)
		}
		if value, ok := values["gimbal_yaw"]; ok {
			f.GimbalYaw = parseFloat(value)
		}
		if f.FNumber >= 100 {
			f.FNumber /= 100 // older DJI subtitles encode f/1.7 as 170
		}
		for _, match := range srtExtraRE.FindAllStringSubmatch(text.String(), -1) {
			key := strings.ToLower(match[1])
			if _, known := values[key]; known {
				continue
			}
			if f.Additional == nil {
				f.Additional = make(map[string]any)
			}
			value := any(match[2])
			if number, err := strconv.ParseFloat(match[2], 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
				value = number
			}
			f.Additional["srt_"+key] = value
		}
		frames = append(frames, f)
	}
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if match := srtTimeRE.FindStringSubmatch(line); match != nil {
			flush()
			text.Reset()
			h, hourErr := strconv.Atoi(match[1])
			m, _ := strconv.Atoi(match[2])
			s, _ := strconv.Atoi(match[3])
			ms, _ := strconv.Atoi(match[4])
			if hourErr != nil || h > 2562047 || m > 59 || s > 59 {
				return nil, fmt.Errorf("telemetry: invalid SRT timestamp %q", line)
			}
			totalMS := (uint64(h)*3600+uint64(m)*60+uint64(s))*1000 + uint64(ms)
			if totalMS > uint64(time.Duration(1<<63-1)/time.Millisecond) {
				return nil, fmt.Errorf("telemetry: SRT timestamp exceeds supported duration")
			}
			stamp = time.Duration(totalMS) * time.Millisecond
			hasStamp = true
		} else {
			text.WriteString(line)
			text.WriteByte(' ')
		}
	}
	flush()
	if subtitleErr != nil {
		return nil, subtitleErr
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("telemetry: read SRT: %w", err)
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("telemetry: no supported DJI bracketed telemetry in SRT file")
	}
	sort.SliceStable(frames, func(i, j int) bool { return frames[i].SampleTime < frames[j].SampleTime })
	return frames, nil
}
