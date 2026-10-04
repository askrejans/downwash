package telemetry

import (
	"strings"
	"testing"
)

func TestSRTCameraOnlyAndAdditionalFields(t *testing.T) {
	frames, err := ParseSRT(strings.NewReader("1\n00:00:01,000 --> 00:00:02,000\n[iso: 100] [shutter: 1/500] [ev: -0.3] [speed: 0]\n"))
	if err != nil {
		t.Fatal(err)
	}
	frame := frames[0]
	if frame.Available.GPS || frame.Available.AltASL || !frame.Available.Camera {
		t.Fatalf("invented measurement availability: %+v", frame.Available)
	}
	if frame.Additional["srt_ev"] != -0.3 || frame.Additional["srt_speed"] != float64(0) {
		t.Fatalf("missing subtitle fields: %+v", frame.Additional)
	}
	if _, err := ParseSRT(strings.NewReader("1\n00:00:01,000 --> 00:00:02,000\n[hello: world]\n")); err == nil {
		t.Fatal("ordinary subtitles accepted")
	}
}
