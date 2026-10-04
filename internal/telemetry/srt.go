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
	srtTimeRE = regexp.MustCompile(`^(\d{2,}):(\d{2}):(\d{2})[,.](\d{3})\s*-->`)
	srtKeyRE  = regexp.MustCompile(`(?i)\b(latitude|longitude|longtitude|rel_alt|abs_alt|drone_roll|drone_pitch|drone_yaw|gb_pitch|gb_yaw|gimbal_pitch|gimbal_yaw|iso|fnum|ct|shutter)\s*:\s*([^\]\s,]+)`)
	srtGPSRE  = regexp.MustCompile(`(?i)\bGPS\s*\(\s*([-+\d.]+)\s*,\s*([-+\d.]+)\s*,\s*([-+\d.]+)[A-Za-z]*\s*\)`)
	srtBaroRE = regexp.MustCompile(`(?i)\bBAROMETER\s*[:(]\s*([-+\d.]+)`)
)

// ParseSRT reads DJI bracketed telemetry subtitles, including DJI's documented
// "longtitude" spelling. Legacy GPS tuples are supported only when coordinate
// bounds resolve their order unambiguously. Ordinary subtitles are rejected.
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
		lonText := values["longitude"]
		if lonText == "" {
			lonText = values["longtitude"]
		}
		lat, latErr := strconv.ParseFloat(values["latitude"], 64)
		lon, lonErr := strconv.ParseFloat(lonText, 64)
		if latErr != nil || lonErr != nil {
			match := srtGPSRE.FindStringSubmatch(text.String())
			if match == nil {
				return
			}
			first, firstErr := strconv.ParseFloat(match[1], 64)
			second, secondErr := strconv.ParseFloat(match[2], 64)
			if firstErr != nil || secondErr != nil {
				return
			}
			switch {
			case math.Abs(first) <= 90 && math.Abs(second) > 90 && math.Abs(second) <= 180:
				lat, lon = first, second
			case math.Abs(second) <= 90 && math.Abs(first) > 90 && math.Abs(first) <= 180:
				lat, lon = second, first
			default:
				subtitleErr = fmt.Errorf("telemetry: ambiguous or invalid legacy GPS coordinate order; use labeled latitude/longitude subtitles")
				return
			}
			values["abs_alt"] = match[3]
			if baro := srtBaroRE.FindStringSubmatch(text.String()); baro != nil {
				values["rel_alt"] = baro[1]
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
