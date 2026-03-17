// Package telemetry extracts and parses per-frame telemetry from DJI drone
// video files by invoking exiftool as a subprocess and streaming its output.
package telemetry

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Frame holds per-video-frame telemetry decoded from the DJI djmd protobuf
// stream embedded in an MP4 file.
type Frame struct {
	SampleTime       time.Duration
	GPSTime          time.Time
	Lat              float64 // decimal degrees, positive = North
	Lon              float64 // decimal degrees, positive = East
	AltAbsolute      float64 // metres above sea level (ASL)
	AltRelative      float64 // metres above ground level / takeoff point (AGL)
	Roll             float64 // degrees
	Pitch            float64 // degrees
	Yaw              float64 // degrees
	GimbalPitch      float64 // degrees
	GimbalYaw        float64 // degrees
	ISO              int
	ShutterSpeed     string  // raw string, e.g. "1/500"
	FNumber          float64 // e.g. 1.7
	ColorTemperature int     // Kelvin
}

// FlightStats summarises a completed flight derived from a Frame slice.
type FlightStats struct {
	Duration      time.Duration
	MaxAltASL     float64
	MinAltASL     float64
	MaxAltAGL     float64
	MinAltAGL     float64
	MaxSpeedMS    float64 // m/s, derived from GPS deltas
	AvgSpeedMS    float64
	DistanceM     float64
	FrameCount    int
	GPSPointCount int
	StartTime     time.Time
	EndTime       time.Time
	StartLat      float64
	StartLon      float64
	EndLat        float64
	EndLon        float64
	// Camera settings from the first valid frame.
	ISO          int
	ShutterSpeed string
	FNumber      float64
	ColorTemp    int
}

// exiftoolArgs is the -p format string passed to exiftool -ee.
const exiftoolArgs = "$SampleTime,$GPSDateTime,$GPSLatitude,$GPSLongitude," +
	"$AbsoluteAltitude,$RelativeAltitude,$DroneRoll,$DronePitch,$DroneYaw," +
	"$GimbalPitch,$GimbalYaw,$ISO,$ShutterSpeed,$FNumber,$ColorTemperature"

// Extract runs exiftool against videoPath and returns all parsed telemetry
// frames. Each frame corresponds to one video frame (~1/30 s for 29.97 fps).
// The returned slice is ordered by ascending SampleTime.
func Extract(ctx context.Context, videoPath string, logger *slog.Logger) ([]Frame, error) {
	cmd := exec.CommandContext(ctx, "exiftool", "-ee", "-p", exiftoolArgs, videoPath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("telemetry: pipe stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("telemetry: pipe stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("telemetry: start exiftool: %w", err)
	}

	// Drain stderr in a goroutine so it doesn't block the process.
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			logger.Debug("exiftool stderr", "line", sc.Text())
		}
	}()

	var frames []Frame
	sc := bufio.NewScanner(stdout)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := sc.Text()
		f, err := parseCSVLine(line)
		if err != nil {
			logger.Debug("telemetry: skip unparseable line",
				"line", lineNum, "err", err)
			continue
		}
		frames = append(frames, f)
	}

	if err := cmd.Wait(); err != nil {
		if len(frames) == 0 {
			return nil, fmt.Errorf("telemetry: exiftool failed: %w", err)
		}
		// exiftool returns non-zero for warnings but still produces output.
		logger.Debug("exiftool non-zero exit (ignored, frames extracted)",
			"err", err, "frames", len(frames))
	}

	logger.Info("telemetry extracted", "frames", len(frames), "video", videoPath)
	return frames, nil
}

// parseCSVLine parses one comma-separated exiftool output line into a Frame.
func parseCSVLine(line string) (Frame, error) {
	parts := strings.Split(line, ",")
	if len(parts) < 15 {
		return Frame{}, fmt.Errorf("expected 15 fields, got %d", len(parts))
	}

	sampleTime, err := ParseSampleTime(parts[0])
	if err != nil {
		return Frame{}, fmt.Errorf("sample time: %w", err)
	}

	lat, err := ParseDMSCoord(parts[2])
	if err != nil {
		return Frame{}, fmt.Errorf("latitude: %w", err)
	}
	lon, err := ParseDMSCoord(parts[3])
	if err != nil {
		return Frame{}, fmt.Errorf("longitude: %w", err)
	}

	altASL, _ := strconv.ParseFloat(parts[4], 64)
	altAGL, _ := strconv.ParseFloat(parts[5], 64)
	roll, _ := strconv.ParseFloat(parts[6], 64)
	pitch, _ := strconv.ParseFloat(parts[7], 64)
	yaw, _ := strconv.ParseFloat(parts[8], 64)
	gimbalPitch, _ := strconv.ParseFloat(parts[9], 64)
	gimbalYaw, _ := strconv.ParseFloat(parts[10], 64)
	iso, _ := strconv.Atoi(parts[11])
	fnum, _ := strconv.ParseFloat(parts[13], 64)
	colorTemp, _ := strconv.Atoi(parts[14])

	return Frame{
		SampleTime:       sampleTime,
		Lat:              lat,
		Lon:              lon,
		AltAbsolute:      altASL,
		AltRelative:      altAGL,
		Roll:             roll,
		Pitch:            pitch,
		Yaw:              yaw,
		GimbalPitch:      gimbalPitch,
		GimbalYaw:        gimbalYaw,
		ISO:              iso,
		ShutterSpeed:     strings.TrimSpace(parts[12]),
		FNumber:          fnum,
		ColorTemperature: colorTemp,
	}, nil
}

// ParseSampleTime parses the SampleTime field from exiftool output.
// Supported formats:
//   - "0 s"        → 0
//   - "0.03 s"     → 30ms
//   - "1:30"       → 90s  (MM:SS)
//   - "0:03:12"    → 192s (H:MM:SS)
func ParseSampleTime(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)

	if strings.HasSuffix(s, " s") {
		sec, err := strconv.ParseFloat(strings.TrimSuffix(s, " s"), 64)
		if err != nil {
			return 0, fmt.Errorf("parse %q: %w", s, err)
		}
		return time.Duration(sec * float64(time.Second)), nil
	}

	parts := strings.Split(s, ":")
	switch len(parts) {
	case 2: // MM:SS
		m, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("parse %q minutes: %w", s, err)
		}
		sec, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0, fmt.Errorf("parse %q seconds: %w", s, err)
		}
		return time.Duration(float64(m)*60*float64(time.Second) +
			sec*float64(time.Second)), nil

	case 3: // H:MM:SS
		h, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("parse %q hours: %w", s, err)
		}
		m, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, fmt.Errorf("parse %q minutes: %w", s, err)
		}
		sec, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return 0, fmt.Errorf("parse %q seconds: %w", s, err)
		}
		return time.Duration(float64(h)*3600*float64(time.Second)+
			float64(m)*60*float64(time.Second)+
			sec*float64(time.Second)), nil
	}

	return 0, fmt.Errorf("unrecognised SampleTime format: %q", s)
}

// dmsRe matches strings like: 57 deg 9' 56.67" N
var dmsRe = regexp.MustCompile(`^(\d+) deg (\d+)' ([\d.]+)" ([NSEW])$`)

// ParseDMSCoord parses an exiftool DMS coordinate string into decimal degrees.
// "57 deg 9' 56.67\" N" → 57.165742
// "24 deg 49' 28.79\" E" → 24.824664
func ParseDMSCoord(s string) (float64, error) {
	s = strings.TrimSpace(s)
	m := dmsRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid DMS coordinate: %q", s)
	}

	deg, _ := strconv.ParseFloat(m[1], 64)
	min, _ := strconv.ParseFloat(m[2], 64)
	sec, _ := strconv.ParseFloat(m[3], 64)

	decimal := deg + min/60.0 + sec/3600.0
	if m[4] == "S" || m[4] == "W" {
		decimal = -decimal
	}
	return decimal, nil
}

// haversineM returns the great-circle distance in metres between two WGS-84 points.
func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6_371_000.0
	p := math.Pi / 180.0
	a := math.Sin((lat2-lat1)*p/2)*math.Sin((lat2-lat1)*p/2) +
		math.Cos(lat1*p)*math.Cos(lat2*p)*
			math.Sin((lon2-lon1)*p/2)*math.Sin((lon2-lon1)*p/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}

// ComputeStats derives aggregate FlightStats from a parsed frame slice.
// GPS jitter spikes (>50 m between consecutive frames) are excluded from
// distance and speed calculations.
func ComputeStats(frames []Frame) FlightStats {
	if len(frames) == 0 {
		return FlightStats{}
	}

	s := FlightStats{
		FrameCount: len(frames),
		StartTime:  frames[0].GPSTime,
		EndTime:    frames[len(frames)-1].GPSTime,
		StartLat:   frames[0].Lat,
		StartLon:   frames[0].Lon,
		EndLat:     frames[len(frames)-1].Lat,
		EndLon:     frames[len(frames)-1].Lon,
		MaxAltASL:  -math.MaxFloat64,
		MinAltASL:  math.MaxFloat64,
		MaxAltAGL:  -math.MaxFloat64,
		MinAltAGL:  math.MaxFloat64,
	}

	// Camera info from first valid frame.
	s.ISO = frames[0].ISO
	s.ShutterSpeed = frames[0].ShutterSpeed
	s.FNumber = frames[0].FNumber
	s.ColorTemp = frames[0].ColorTemperature

	var gpsCount int
	var prevDt float64

	for i, f := range frames {
		if f.Lat != 0 || f.Lon != 0 {
			gpsCount++
		}

		if f.AltAbsolute > s.MaxAltASL {
			s.MaxAltASL = f.AltAbsolute
		}
		if f.AltAbsolute < s.MinAltASL {
			s.MinAltASL = f.AltAbsolute
		}
		if f.AltRelative > s.MaxAltAGL {
			s.MaxAltAGL = f.AltRelative
		}
		if f.AltRelative < s.MinAltAGL {
			s.MinAltAGL = f.AltRelative
		}

		if i > 0 {
			prev := frames[i-1]
			dt := f.SampleTime.Seconds() - prev.SampleTime.Seconds()
			if dt <= 0 {
				dt = prevDt
			}
			d := haversineM(prev.Lat, prev.Lon, f.Lat, f.Lon)
			if d < 50 { // ignore GPS teleportation spikes
				s.DistanceM += d
				if dt > 0 {
					spd := d / dt
					if spd > s.MaxSpeedMS {
						s.MaxSpeedMS = spd
					}
				}
			}
			prevDt = dt
		}
	}

	s.GPSPointCount = gpsCount
	s.Duration = frames[len(frames)-1].SampleTime - frames[0].SampleTime
	if s.Duration.Seconds() > 0 {
		s.AvgSpeedMS = s.DistanceM / s.Duration.Seconds()
	}
	return s
}
