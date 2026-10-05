package downwash

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalysisLimitsPreserveUnrestrictedResultsAndExactBoundaries(t *testing.T) {
	path := testSource(t)
	info, _ := os.Stat(path)
	ordinary, err := Analyze(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, limits := range []AnalysisLimits{{}, {MaxFrames: 3, MaxMetadataBytes: info.Size()}} {
		result, err := AnalyzeWithLimits(context.Background(), path, limits)
		if err != nil || string(result.Frames) != string(ordinary.Frames) || string(result.Stats) != string(ordinary.Stats) {
			t.Fatalf("limited analysis differs: %v", err)
		}
	}
	for _, limits := range []AnalysisLimits{{MaxFrames: 2}, {MaxMetadataBytes: info.Size() - 1}} {
		if _, err := AnalyzeWithLimits(context.Background(), path, limits); !errors.Is(err, ErrAnalysisLimit) {
			t.Fatalf("expected resource limit, got %v", err)
		}
		var result Response
		if err := json.Unmarshal([]byte(AnalyzeWithLimitsJSON(context.Background(), path, limits)), &result); err != nil {
			t.Fatal(err)
		}
		if result.ErrorCode != "resource_limit" || string(result.Frames) != "[]" {
			t.Fatalf("unexpected limited response: %+v", result)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var result Response
	if err := json.Unmarshal([]byte(AnalyzeWithLimitsJSON(ctx, path, AnalysisLimits{MaxFrames: 3})), &result); err != nil {
		t.Fatal(err)
	}
	if result.ErrorCode != "cancelled" {
		t.Fatalf("cancelled code=%q", result.ErrorCode)
	}
}

func TestJSONFrameLimitStopsBeforeDecodingExcessFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "limited.json")
	// The excessive frame is invalid: a streaming limit must win before its decode.
	data := `{"version":"1.0","frames":[{"time_s":0},{"time_s":1},{"time_s":"invalid"}]}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := AnalyzeWithLimits(context.Background(), path, AnalysisLimits{MaxFrames: 2, MaxMetadataBytes: int64(len(data))}); !errors.Is(err, ErrAnalysisLimit) {
		t.Fatalf("excess frame was decoded: %v", err)
	}
	data = `{"Frames":[{"time_s":0},{"time_s":1}],"Version":"1.0"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := AnalyzeWithLimits(context.Background(), path, AnalysisLimits{MaxFrames: 2, MaxMetadataBytes: int64(len(data))})
	if err != nil || !strings.Contains(string(result.Stats), `"frame_count": 2`) {
		t.Fatalf("valid metadata failed: %v stats=%s", err, result.Stats)
	}
	if _, err := AnalyzeWithLimits(context.Background(), path, AnalysisLimits{MaxFrames: 2, MaxMetadataBytes: int64(len(data)) - 1}); !errors.Is(err, ErrAnalysisLimit) {
		t.Fatalf("metadata bytes unbounded: %v", err)
	}
}

func TestJSONMapAndNestingLimitsApplyBeforeStructuredDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fields.json")
	for _, additional := range []string{
		`{` + strings.Repeat(`"field":1,`, 70) + `"last":1}`,
		strings.Repeat(`{"nested":`, 70) + `1` + strings.Repeat(`}`, 70),
	} {
		data := `{"version":"1.0","frames":[{"time_s":0,"additional":` + additional + `}]}`
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := AnalyzeWithLimits(context.Background(), path, AnalysisLimits{MaxFrames: 1, MaxMetadataBytes: int64(len(data))}); !errors.Is(err, ErrAnalysisLimit) {
			t.Fatalf("structured metadata unbounded: %v", err)
		}
	}
}
