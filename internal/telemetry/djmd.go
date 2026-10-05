package telemetry

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DJI field paths are documented by ExifTool's DJI tag reference. This decoder
// implements the protobuf wire format independently and reads only known fields.
// https://exiftool.org/TagNames/DJI.html
type protoValue struct {
	wire    uint64
	n       uint64
	b       []byte
	present bool
}

func protoFields(data []byte) (map[uint64]protoValue, error) {
	return protoFieldsContext(context.Background(), data)
}

func protoFieldsContext(ctx context.Context, data []byte) (map[uint64]protoValue, error) {
	fields := make(map[uint64]protoValue)
	for len(data) > 0 {
		if err := checkTableCount(ctx, uint64(len(fields)+1)); err != nil {
			return nil, err
		}
		tag, n := binary.Uvarint(data)
		if n <= 0 || tag>>3 == 0 {
			return nil, fmt.Errorf("invalid protobuf tag")
		}
		data = data[n:]
		v := protoValue{wire: tag & 7, present: true}
		switch v.wire {
		case 0:
			v.n, n = binary.Uvarint(data)
			if n <= 0 {
				return nil, fmt.Errorf("truncated protobuf integer")
			}
			data = data[n:]
		case 1, 5:
			size := 8
			if v.wire == 5 {
				size = 4
			}
			if len(data) < size {
				return nil, fmt.Errorf("truncated protobuf number")
			}
			v.b, data = data[:size], data[size:]
		case 2:
			size, n := binary.Uvarint(data)
			if n <= 0 || size > uint64(len(data)-n) {
				return nil, fmt.Errorf("truncated protobuf message")
			}
			data = data[n:]
			v.b, data = data[:size], data[size:]
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type %d", v.wire)
		}
		fields[tag>>3] = v
	}
	return fields, nil
}

func protoAt(data []byte, path ...uint64) (protoValue, error) {
	return protoAtContext(context.Background(), data, path...)
}

func protoAtContext(ctx context.Context, data []byte, path ...uint64) (protoValue, error) {
	var value protoValue
	for _, field := range path {
		fields, err := protoFieldsContext(ctx, data)
		if err != nil {
			return protoValue{}, err
		}
		var ok bool
		value, ok = fields[field]
		if !ok {
			return protoValue{}, nil
		}
		data = value.b
	}
	return value, nil
}

func (v protoValue) number() float64 {
	switch v.wire {
	case 0:
		return float64(int64(v.n))
	case 1:
		if len(v.b) == 8 {
			return math.Float64frombits(binary.LittleEndian.Uint64(v.b))
		}
	case 5:
		if len(v.b) == 4 {
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(v.b)))
		}
	case 2:
		if len(v.b) == 4 {
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(v.b)))
		}
		if len(v.b) == 8 {
			return math.Float64frombits(binary.LittleEndian.Uint64(v.b))
		}
	}
	return 0
}

func (v protoValue) rational() float64 {
	num, n := binary.Uvarint(v.b)
	if n <= 0 {
		return 0
	}
	den, m := binary.Uvarint(v.b[n:])
	if m <= 0 || n+m != len(v.b) || den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

var djiProtocolRE = regexp.MustCompile(`dvtm_[A-Za-z0-9_\-]+\.proto`)

type djiLayout struct {
	location, iso, shutter, aperture, color uint64
	degrees                                 bool
	relative, attitude, gimbal              bool
}

var djiLayouts = map[string]djiLayout{
	"dvtm_Mini4_Pro.proto": {3, 7, 10, 11, 32, false, true, true, true},
	"dvtm_Air3.proto":      {3, 7, 10, 11, 32, false, true, true, true},
	"dvtm_Air3s.proto":     {3, 7, 10, 11, 32, false, true, true, true},
	"dvtm_wm265e.proto":    {3, 2, 3, 0, 0, false, true, true, true},
	"dvtm_pm320.proto":     {3, 2, 3, 4, 0, false, true, true, true},
	"dvtm_wm261.proto":     {3, 9, 10, 11, 0, false, true, true, true},
	"dvtm_wa345e.proto":    {3, 0, 0, 0, 0, false, true, true, true},
	"dvtm_AVATA2.proto":    {4, 2, 4, 10, 6, false, true, true, false},
	"dvtm_dji_neo.proto":   {4, 2, 4, 10, 6, false, false, true, false},
	"dvtm_Mavic4.proto":    {3, 9, 10, 0, 24, true, true, true, false},
	"dvtm_Mini5Pro.proto":  {3, 0, 0, 0, 0, true, false, false, false},
	"dvtm_ac203.proto":     {4, 3, 4, 0, 6, false, false, false, false},
	"dvtm_ac204.proto":     {4, 3, 4, 0, 6, false, false, false, false},
	"dvtm_ac206.proto":     {4, 3, 4, 0, 6, false, false, false, false},
	"dvtm_oq101.proto":     {4, 3, 4, 0, 6, false, false, false, false},
	"dvtm_PP-101.proto":    {0, 9, 10, 0, 24, false, false, false, false},
}

func decodeDJI(data []byte, previousProtocol string) (Frame, string, bool, error) {
	return decodeDJIContext(context.Background(), data, previousProtocol)
}

func decodeDJIContext(ctx context.Context, data []byte, previousProtocol string) (Frame, string, bool, error) {
	fields, err := protoFieldsContext(ctx, data)
	if err != nil {
		return Frame{}, previousProtocol, false, err
	}
	protocol := previousProtocol
	if match := djiProtocolRE.Find(data); match != nil {
		protocol = string(match)
	}
	layout, ok := djiLayouts[protocol]
	if !ok {
		return Frame{}, protocol, false, fmt.Errorf("unsupported DJI protocol %q; import the matching SRT file", protocol)
	}
	if len(fields[3].b) == 0 {
		return Frame{}, protocol, false, nil // protocol header, without a frame
	}
	loc := layout.location
	var pathErr error
	get := func(path ...uint64) protoValue {
		value, err := protoAtContext(ctx, data, path...)
		if err != nil && pathErr == nil {
			pathErr = err
		}
		return value
	}
	gps := uint64(4)
	actionCamera := strings.HasPrefix(protocol, "dvtm_ac") || protocol == "dvtm_oq101.proto"
	if actionCamera {
		gps = 2
	}
	lat := get(3, loc, gps, 1, 2).number()
	lon := get(3, loc, gps, 1, 3).number()
	if !layout.degrees && get(3, loc, gps, 1, 1).number() == 0 {
		lat, lon = lat*180/math.Pi, lon*180/math.Pi
	}
	if !ValidGPS(lat, lon) {
		lat, lon = 0, 0
	}
	f := Frame{
		Lat: lat, Lon: lon,
		AltAbsolute: get(3, loc, gps, 2).number() / 1000,
	}
	f.Available = &Availability{GPS: ValidGPS(lat, lon), AltASL: get(3, loc, gps, 2).present}
	if layout.relative {
		f.AltRelative = get(3, loc, 5, 1).number() / 1000
		f.Available.AltRelative = get(3, loc, 5, 1).present
	}
	if layout.attitude {
		f.Roll, f.Pitch, f.Yaw = get(3, loc, 3, 1).number()/10, get(3, loc, 3, 2).number()/10, get(3, loc, 3, 3).number()/10
		f.Available.Attitude = get(3, loc, 3).present
	}
	if layout.gimbal {
		f.GimbalPitch, f.GimbalYaw = get(3, 4, 3, 1).number()/10, get(3, 4, 3, 3).number()/10
		f.Available.Gimbal = get(3, 4, 3).present
	}
	if protocol == "dvtm_oq101.proto" {
		f.FNumber = get(1, 14, 1).rational()
	}
	if layout.iso != 0 {
		f.ISO = int(get(3, 2, layout.iso, 1).number())
	}
	if layout.shutter != 0 {
		if exposure := get(3, 2, layout.shutter, 1).rational(); exposure > 0 {
			if exposure < 1 {
				f.ShutterSpeed = "1/" + strconv.FormatFloat(1/exposure, 'f', -1, 64)
			} else {
				f.ShutterSpeed = strconv.FormatFloat(exposure, 'f', -1, 64)
			}
		}
	}
	if layout.aperture != 0 {
		f.FNumber = get(3, 2, layout.aperture, 1).rational()
	}
	if layout.color != 0 {
		f.ColorTemperature = int(get(3, 2, layout.color, 1).number())
	}
	f.Available.Camera = f.ISO > 0 || f.ShutterSpeed != "" || f.FNumber > 0 || f.ColorTemperature > 0
	date := strings.TrimSpace(string(get(3, loc, gps, 6, 1).b))
	for _, format := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999", "2006:01:02 15:04:05.999"} {
		if parsed, err := time.Parse(format, date); err == nil {
			f.GPSTime = parsed
			break
		}
	}
	if pathErr != nil {
		return Frame{}, protocol, false, fmt.Errorf("invalid nested DJI message: %w", pathErr)
	}
	return f, protocol, true, nil
}

// ValidGPS checks whether coordinates are finite, within WGS84 bounds, and
// different from DJI's (0, 0) no-fix sentinel. Equator/prime-meridian fixes work.
func ValidGPS(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) &&
		lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 && (lat != 0 || lon != 0)
}
