package telemetry

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fieldVar(id uint64, value uint64) []byte {
	data := binary.AppendUvarint(nil, id<<3)
	return binary.AppendUvarint(data, value)
}

func fieldBytes(id uint64, value []byte) []byte {
	data := binary.AppendUvarint(nil, id<<3|2)
	data = binary.AppendUvarint(data, uint64(len(value)))
	return append(data, value...)
}

func fieldDouble(id uint64, value float64) []byte {
	data := binary.AppendUvarint(nil, id<<3|1)
	return binary.LittleEndian.AppendUint64(data, math.Float64bits(value))
}

func miniSample(lat, lon float64, protocol bool) []byte {
	gps := append(fieldDouble(2, lat*math.Pi/180), fieldDouble(3, lon*math.Pi/180)...)
	location := fieldBytes(4, append(fieldBytes(1, gps), fieldVar(2, 72500)...))
	alt := binary.AppendUvarint(nil, 1<<3|5)
	alt = binary.LittleEndian.AppendUint32(alt, math.Float32bits(12300))
	location = append(location, fieldBytes(5, alt)...)
	negative := int64(-125)
	location = append(location, fieldBytes(3, append(fieldVar(1, uint64(negative)), fieldVar(3, 900)...))...)
	frame := fieldBytes(3, fieldBytes(3, location))
	if protocol {
		frame = append(fieldBytes(1, fieldBytes(1, fieldBytes(1, []byte("dvtm_Mini4_Pro.proto")))), frame...)
	}
	return frame
}

func atom(kind string, parts ...[]byte) []byte {
	var content []byte
	for _, part := range parts {
		content = append(content, part...)
	}
	data := binary.BigEndian.AppendUint32(nil, uint32(len(content)+8))
	data = append(data, []byte(kind)...)
	return append(data, content...)
}

func uints(values ...uint32) []byte {
	var data []byte
	for _, value := range values {
		data = binary.BigEndian.AppendUint32(data, value)
	}
	return data
}

// syntheticMP4 is a minimal ISO BMFF fixture with two DJI samples in one chunk.
// It deliberately contains no encoded video and no personal location data.
func syntheticMP4() []byte {
	return syntheticMP4Tables(false, false)
}

func syntheticMP4Tables(wideOffsets, constantSize bool) []byte {
	first, second := miniSample(57, 24, true), miniSample(57.00001, 24.00001, false)
	if constantSize {
		second = miniSample(57.00001, 24.00001, true)
	}
	mdat := atom("mdat", first, second)
	stsz := atom("stsz", uints(0, 0, 2, uint32(len(first)), uint32(len(second))))
	if constantSize {
		stsz = atom("stsz", uints(0, uint32(len(first)), 2))
	}
	offsets := atom("stco", uints(0, 1, 8))
	if wideOffsets {
		offsets = atom("co64", uints(0, 1, 0, 8))
	}
	stbl := atom("stbl",
		atom("stsd", uints(0, 1), atom("djmd", make([]byte, 8))),
		stsz,
		atom("stsc", uints(0, 1, 1, 2, 1)),
		offsets,
		atom("stts", uints(0, 1, 2, 1000)))
	mdhd := atom("mdhd", uints(0, 0, 0, 1000, 2000))
	return append(mdat, atom("moov", atom("trak", atom("mdia", mdhd, atom("minf", stbl))))...)
}

func TestNativeMP4TablesAndExtendedAtoms(t *testing.T) {
	for _, wide := range []bool{false, true} {
		for _, constant := range []bool{false, true} {
			data := syntheticMP4Tables(wide, constant)
			mdatSize := int(binary.BigEndian.Uint32(data))
			moov := data[mdatSize:]
			extended := uints(1)
			extended = append(extended, []byte("moov")...)
			extended = binary.BigEndian.AppendUint64(extended, uint64(len(moov)+8))
			extended = append(extended, moov[8:]...)
			data = append(data[:mdatSize], extended...)
			path := filepath.Join(t.TempDir(), "extended.mp4")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			frames, _, err := ExtractMP4(context.Background(), path)
			if err != nil || len(frames) != 2 {
				t.Fatalf("wide=%v constant=%v frames=%d error=%v", wide, constant, len(frames), err)
			}
		}
	}
}

func nestedValue(path []uint64, value []byte) []byte {
	for i := len(path) - 1; i >= 0; i-- {
		value = fieldBytes(path[i], value)
	}
	return value
}

func TestDocumentedDJIProtocolLayouts(t *testing.T) {
	cases := []struct {
		protocol           string
		location, gps, iso uint64
		degrees            bool
	}{
		{"dvtm_Mini4_Pro.proto", 3, 4, 7, false}, {"dvtm_Air3.proto", 3, 4, 7, false}, {"dvtm_Air3s.proto", 3, 4, 7, false},
		{"dvtm_wm265e.proto", 3, 4, 2, false}, {"dvtm_pm320.proto", 3, 4, 2, false}, {"dvtm_wm261.proto", 3, 4, 9, false}, {"dvtm_wa345e.proto", 3, 4, 0, false},
		{"dvtm_AVATA2.proto", 4, 4, 2, false}, {"dvtm_dji_neo.proto", 4, 4, 2, false}, {"dvtm_Mavic4.proto", 3, 4, 9, true}, {"dvtm_Mini5Pro.proto", 3, 4, 0, true},
		{"dvtm_ac203.proto", 4, 2, 3, false}, {"dvtm_ac204.proto", 4, 2, 3, false}, {"dvtm_ac206.proto", 4, 2, 3, false}, {"dvtm_oq101.proto", 4, 2, 3, false}, {"dvtm_PP-101.proto", 0, 0, 9, false},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			lat, lon := 57.0, 24.0
			if !tc.degrees {
				lat, lon = lat*math.Pi/180, lon*math.Pi/180
			}
			gpsData := append(fieldBytes(1, append(fieldDouble(2, lat), fieldDouble(3, lon)...)), fieldVar(2, 72500)...)
			var frame []byte
			if tc.location != 0 {
				frame = fieldBytes(tc.location, fieldBytes(tc.gps, gpsData))
			}
			if tc.iso != 0 {
				iso := binary.AppendUvarint(nil, 1<<3|5)
				iso = binary.LittleEndian.AppendUint32(iso, math.Float32bits(200))
				frame = append(frame, nestedValue([]uint64{2, tc.iso}, iso)...)
			}
			data := append(nestedValue([]uint64{1, 1, 1}, []byte(tc.protocol)), fieldBytes(3, frame)...)
			decoded, _, hasFrame, err := decodeDJI(data, "")
			if err != nil || !hasFrame {
				t.Fatalf("frame=%v error=%v", hasFrame, err)
			}
			if tc.location != 0 && (math.Abs(decoded.Lat-57) > 1e-8 || math.Abs(decoded.Lon-24) > 1e-8 || decoded.AltAbsolute != 72.5) {
				t.Fatalf("decoded=%+v", decoded)
			}
			if tc.iso != 0 && decoded.ISO != 200 {
				t.Fatalf("ISO=%d", decoded.ISO)
			}
		})
	}
}

func TestSRTLegacyAndMalformedTimes(t *testing.T) {
	for _, text := range []string{"GPS(120.5,57.0,72.5) BAROMETER:12.5", "GPS(120.5,57.0,72.5M) BAROMETER(12.5)"} {
		frames, err := ParseSRT(strings.NewReader("1\n00:00:00,000 --> 00:00:01,000\n" + text))
		if err != nil || frames[0].Lat != 57 || frames[0].Lon != 120.5 || frames[0].AltRelative != 12.5 {
			t.Fatalf("frames=%+v error=%v", frames, err)
		}
	}
	for _, text := range []string{"00:00:00,000 --> 00:00:01,000\nGPS(200,95,70)", "00:60:00,000 --> 00:00:01,000\n[latitude: 57] [longitude: 24]", "99999999999999999999999:00:00,000 --> 00:00:01,000\n[latitude: 57] [longitude: 24]"} {
		if _, err := ParseSRT(strings.NewReader(text)); err == nil {
			t.Fatalf("accepted invalid source %q", text)
		}
	}
}

func TestSparseGPSAndLongDistanceHome(t *testing.T) {
	var frames []Frame
	for i := 0; i <= 120; i++ {
		frames = append(frames, Frame{SampleTime: time.Duration(i) * time.Second, Lat: 57 + float64(i)*0.0001, Lon: 24})
	}
	stats := ComputeStats(frames)
	if stats.MaxHomeDist < 1300 || stats.DistanceM < 1300 {
		t.Fatalf("long flight clipped: %+v", stats)
	}
	sparse := ComputeStats([]Frame{frames[0], frames[10]})
	if sparse.DistanceM < 100 || sparse.MaxSpeedMS < 10 {
		t.Fatalf("sparse GPS discarded: %+v", sparse)
	}
}

func TestExtractNativeMP4(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic.mp4")
	if err := os.WriteFile(path, syntheticMP4(), 0o600); err != nil {
		t.Fatal(err)
	}
	frames, info, err := ExtractMP4(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || info.Protocol != "dvtm_Mini4_Pro.proto" {
		t.Fatalf("frames=%d info=%+v", len(frames), info)
	}
	if frames[1].SampleTime != time.Second || math.Abs(frames[0].Lat-57) > 1e-8 || math.Abs(frames[0].Lon-24) > 1e-8 {
		t.Fatalf("frame=%+v", frames[0])
	}
	if frames[0].AltAbsolute != 72.5 || math.Abs(frames[0].AltRelative-12.3) > 1e-5 || frames[0].Roll != -12.5 {
		t.Fatalf("values=%+v", frames[0])
	}
}

func TestNativeRejectsMalformedSources(t *testing.T) {
	for name, data := range map[string][]byte{"short": {1, 2}, "oversized": {255, 255, 255, 255, 'm', 'o', 'o', 'v'}, "empty": atom("moov")} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name+".mp4")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ExtractMP4(context.Background(), path); err == nil {
				t.Fatal("expected malformed source error")
			}
		})
	}
	for _, data := range [][]byte{{0}, {10, 255}, {13, 1}, {8, 128}} {
		if _, err := protoFields(data); err == nil {
			t.Fatalf("accepted malformed protobuf %x", data)
		}
	}
	if _, _, _, err := decodeDJI(miniSample(57, 24, false), "dvtm_unknown.proto"); err == nil {
		t.Fatal("unknown protocol accepted")
	}
}

func TestSRTDJIFields(t *testing.T) {
	source := "1\r\n00:00:00,000 --> 00:00:00,033\r\n<font>2024-06-01 13:00:00 [iso : 100] [shutter : 1/500] [fnum : 170] [latitude: 57.0] [longtitude: 24.0] [rel_alt: 12.5 abs_alt: 72.5] [drone_roll: -2] [gb_pitch: -90]</font>\r\n\r\n2\r\n00:00:01,000 --> 00:00:01,033\r\n[latitude: 57.00001] [longitude: 24.00001] [fnum: 1.7]\r\n"
	frames, err := ParseSRT(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[0].AltRelative != 12.5 || frames[0].FNumber != 1.7 || frames[1].FNumber != 1.7 || frames[0].GimbalPitch != -90 || frames[0].ISO != 100 {
		t.Fatalf("frames=%+v", frames)
	}
	if !frames[0].GPSTime.IsZero() {
		t.Fatal("SRT date without timezone must not invent GPS UTC time")
	}
	if _, err := ParseSRT(strings.NewReader("1\n00:00:00,000 --> 00:00:01,000\nHello world\n")); err == nil {
		t.Fatal("ordinary subtitle accepted")
	}
}

func TestValidGPSAndStatsEndpoints(t *testing.T) {
	for _, pair := range [][2]float64{{0, 0}, {91, 24}, {57, 181}, {math.NaN(), 24}, {57, math.Inf(1)}} {
		if ValidGPS(pair[0], pair[1]) {
			t.Fatalf("accepted %v", pair)
		}
	}
	if !ValidGPS(0, 24) || !ValidGPS(57, 0) {
		t.Fatal("valid equator/prime-meridian point rejected")
	}
	stats := ComputeStats([]Frame{{}, {SampleTime: time.Second, Lat: 57, Lon: 24}, {SampleTime: 2 * time.Second, Lat: 57.00001, Lon: 24.00001}, {SampleTime: 3 * time.Second, Lat: 91, Lon: 24}})
	if stats.GPSPointCount != 2 || stats.StartLat != 57 || stats.EndLat != 57.00001 {
		t.Fatalf("stats=%+v", stats)
	}
}

func FuzzProtoFields(f *testing.F) {
	f.Add(miniSample(57, 24, true))
	f.Add([]byte{10, 255})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = protoFields(data) })
}

func TestAdditionalDJIMeasurements(t *testing.T) {
	width := append(fieldVar(1, 3840), fieldVar(2, 2160)...)
	width = append(width, fieldDouble(3, 29.97)...)
	header := fieldBytes(1, append(fieldBytes(5, []byte("ABC-123")), fieldBytes(10, []byte("Mavic 3"))...))
	frame := fieldBytes(1, fieldVar(1, 42))
	frame = append(frame, fieldBytes(2, fieldBytes(6, fieldBytes(1, binary.LittleEndian.AppendUint32(nil, math.Float32bits(2)))))...)
	data := append(fieldBytes(1, header), fieldBytes(2, fieldBytes(2, width))...)
	data = append(data, fieldBytes(3, frame)...)
	values, err := djiAdditional(data, "dvtm_wm265e.proto")
	if err != nil {
		t.Fatal(err)
	}
	if values["model"] != "Mavic 3" || values["serial_number"] != "ABC-123" || values["frame_width_px"] != float64(3840) || values["frame_number"] != float64(42) || values["digital_zoom"] != float64(2) {
		t.Fatal(values)
	}
	if _, ok := values["sensor_temperature"]; ok {
		t.Fatal("invented temperature")
	}
}
