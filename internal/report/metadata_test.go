package report

import (
	"encoding/json"
	"github.com/askrejans/downwash/internal/telemetry"
	"testing"
)

func TestMetadataRetainsAdditionalFieldsAndAvailability(t *testing.T) {
	frames := []telemetry.Frame{{Additional: map[string]any{"model": "test", "srt_ev": 0.0}, ISO: 100, Available: &telemetry.Availability{Camera: true}}}
	data, err := MetadataData(frames, telemetry.ComputeStats(frames), "flight", "h264")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	frame := doc["frames"].([]any)[0].(map[string]any)
	available := frame["available"].(map[string]any)
	if available["gps"] != false || available["camera"] != true {
		t.Fatal("incorrect frame availability")
	}
	if frame["additional"].(map[string]any)["model"] != "test" {
		t.Fatal("additional fields dropped")
	}
}
