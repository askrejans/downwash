package telemetry

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func subtitleMP4(kind string, samples ...[]byte) []byte {
	var sizes []uint32
	for _, sample := range samples {
		sizes = append(sizes, uint32(len(sample)))
	}
	stsz := append(uints(0, 0, uint32(len(samples))), uints(sizes...)...)
	stbl := atom("stbl", atom("stsd", uints(0, 1), atom(kind, make([]byte, 8))), atom("stsz", stsz), atom("stsc", uints(0, 1, 1, uint32(len(samples)), 1)), atom("stco", uints(0, 1, 8)), atom("stts", uints(0, 1, uint32(len(samples)), 1000)))
	return append(atom("mdat", samples...), atom("moov", atom("trak", atom("mdia", atom("mdhd", uints(0, 0, 0, 1000, uint32(len(samples)*1000))), atom("minf", stbl))))...)
}

func prefixedText(text string) []byte {
	return append(binary.BigEndian.AppendUint16(nil, uint16(len(text))), []byte(text)...)
}

func TestDocumentedLegacyDJISubtitle(t *testing.T) {
	text := "F/3.5, SS 1000, ISO 100, EV -1/3, GPS (24.0, 57.0, 72.5), D 24.26m, H 0.00m, H.S 2.10m/s, V.S 0.00m/s, F.PRY (2.7°, -7.0°, 110.1°), G.PRY (-51.1°, 0.0°, -58.9°)"
	frames, err := ParseSRT(strings.NewReader("1\n00:00:00,000 --> 00:00:01,000\n" + text))
	if err != nil || len(frames) != 1 {
		t.Fatalf("frames=%d error=%v", len(frames), err)
	}
	f := frames[0]
	if f.Lat != 57 || f.Lon != 24 || f.AltAbsolute != 72.5 || f.AltRelative != 0 || !f.Available.AltRelative || f.FNumber != 3.5 || f.ShutterSpeed != "1/1000" || f.Pitch != 2.7 || f.Roll != -7 || f.GimbalPitch != -51.1 || f.Additional["gimbal_roll_deg"] != float64(0) || f.Additional["horizontal_speed_ms"] != 2.1 || f.Additional["distance_home_m"] != 24.26 {
		t.Fatalf("unexpected telemetry: %+v", f)
	}
	frames, err = ParseSRT(strings.NewReader("1\n00:00:00,000 --> 00:00:01,000\nF/2.8, ISO 100, GPS (0.0, 0.0, 0), H 0m"))
	if err != nil || len(frames) != 1 || frames[0].Available.GPS || !frames[0].Available.Camera || !frames[0].Available.AltRelative {
		t.Fatalf("zero fix lost camera/height: %+v %v", frames, err)
	}
}

func TestEmbeddedDJITimedText(t *testing.T) {
	for _, kind := range []string{"text", "tx3g"} {
		t.Run(kind, func(t *testing.T) {
			data := subtitleMP4(kind, prefixedText("Ordinary caption"), prefixedText("[latitude: 57] [longitude: 24] [rel_alt: 0] [iso: 100]"), prefixedText("[latitude: 57.00001] [longitude: 24] [rel_alt: 1]"))
			path := filepath.Join(t.TempDir(), "text.mp4")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			frames, info, err := ExtractMP4(context.Background(), path)
			if err != nil || len(frames) != 2 || info.Protocol != "dji_text" || frames[0].SampleTime != time.Second || frames[1].SampleTime != 2*time.Second || !frames[0].Available.AltRelative || frames[0].AltRelative != 0 {
				t.Fatalf("frames=%+v info=%+v error=%v", frames, info, err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "malformed.mp4")
	os.WriteFile(path, subtitleMP4("tx3g", []byte{0, 40, 'x'}), 0600)
	if _, _, err := ExtractMP4(context.Background(), path); err == nil || errors.Is(err, ErrNoTelemetry) {
		t.Fatalf("malformed text accepted: %v", err)
	}
}

func TestVideoMetadataWithoutTelemetry(t *testing.T) {
	description := make([]byte, 78)
	binary.BigEndian.PutUint16(description[24:], 3840)
	binary.BigEndian.PutUint16(description[26:], 2160)
	stbl := atom("stbl", atom("stsd", uints(0, 1), atom("hvc1", description)), atom("stts", uints(0, 1, 300, 1000)))
	data := atom("moov", atom("trak", atom("mdia", atom("mdhd", uints(0, 0, 0, 30000, 300000)), atom("minf", stbl))))
	path := filepath.Join(t.TempDir(), "video.mp4")
	os.WriteFile(path, data, 0600)
	frames, info, err := ExtractMP4(context.Background(), path)
	if !errors.Is(err, ErrNoTelemetry) || len(frames) != 0 || info.DurationS != 10 || info.Codec != "h265" || info.Width != 3840 || info.Height != 2160 || info.FrameRate != 30 {
		t.Fatalf("frames=%d info=%+v error=%v", len(frames), info, err)
	}
	// A malformed video remains an error, even when telemetry is absent.
	data[len(data)-4] = 255
	os.WriteFile(path, data[:len(data)-1], 0600)
	if _, _, err := ExtractMP4(context.Background(), path); err == nil || errors.Is(err, ErrNoTelemetry) {
		t.Fatalf("corrupt video accepted: %v", err)
	}
}
