package downwash

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func videoAtom(kind string, parts ...[]byte) []byte {
	var body []byte
	for _, part := range parts {
		body = append(body, part...)
	}
	return append(append(binary.BigEndian.AppendUint32(nil, uint32(len(body)+8)), []byte(kind)...), body...)
}
func videoInts(values ...uint32) []byte {
	var data []byte
	for _, v := range values {
		data = binary.BigEndian.AppendUint32(data, v)
	}
	return data
}

func TestAnalyzeVideoWithoutInventingTelemetry(t *testing.T) {
	entry := make([]byte, 78)
	binary.BigEndian.PutUint16(entry[24:], 1920)
	binary.BigEndian.PutUint16(entry[26:], 1080)
	stbl := videoAtom("stbl", videoAtom("stsd", videoInts(0, 1), videoAtom("avc1", entry)), videoAtom("stts", videoInts(0, 1, 300, 1000)))
	data := videoAtom("moov", videoAtom("trak", videoAtom("mdia", videoAtom("mdhd", videoInts(0, 0, 0, 30000, 300000)), videoAtom("minf", stbl))))
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Analyze(context.Background(), path)
	if err != nil || r.Format != "video" || r.Video == nil || r.Video.DurationS != 10 || r.Video.FrameRate != 30 || r.Video.Width != 1920 || string(r.Frames) != "[]" || len(r.Warnings) == 0 {
		t.Fatalf("response=%+v error=%v", r, err)
	}
	for key, available := range r.Available {
		if available {
			t.Fatalf("invented %s telemetry", key)
		}
	}
	var stats map[string]any
	if err := json.Unmarshal(r.Stats, &stats); err != nil {
		t.Fatal(err)
	}
	if stats["duration_s"] != float64(10) || stats["frame_count"] != float64(0) || stats["gps_point_count"] != float64(0) {
		t.Fatalf("stats=%+v", stats)
	}
	var bridge Response
	if err := json.Unmarshal([]byte(AnalyzeJSON(path)), &bridge); err != nil || bridge.Error != "" || bridge.Video == nil {
		t.Fatalf("bridge=%+v error=%v", bridge, err)
	}
	output := filepath.Join(t.TempDir(), "outputs")
	if _, err := Process(context.Background(), Request{InputPath: path, OutputDir: output}); err == nil {
		t.Fatal("telemetry reports accepted absent measurements")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed telemetry export wrote output directory")
	}
}
