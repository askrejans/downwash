package telemetry

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMP4RejectsOversizedMoovBeforeAllocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.mp4")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(append(binary.BigEndian.AppendUint32(nil, 8+(5<<20)), []byte("moov")...))
	if err == nil {
		err = f.Truncate(8 + (5 << 20))
	}
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithAnalysisLimits(context.Background(), AnalysisLimits{MaxFrames: 6000, MaxMetadataBytes: 4 << 20})
	if _, _, err := ExtractMP4(ctx, path); !errors.Is(err, ErrAnalysisLimit) {
		t.Fatalf("oversized moov: %v", err)
	}
}

func TestMP4ExactMetadataAndFrameLimitsPreserveAllMeasurements(t *testing.T) {
	data := syntheticMP4()
	path := filepath.Join(t.TempDir(), "flight.mp4")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := WithAnalysisLimits(context.Background(), AnalysisLimits{MaxFrames: 2, MaxMetadataBytes: int64(len(data) - 16)})
	frames, _, err := ExtractMP4(ctx, path)
	if err != nil || len(frames) != 2 || frames[1].Lat == 0 {
		t.Fatalf("exact boundary lost measurements: frames=%d err=%v", len(frames), err)
	}
	ctx = WithAnalysisLimits(context.Background(), AnalysisLimits{MaxFrames: 1, MaxMetadataBytes: int64(len(data) - 16)})
	if _, _, err := ExtractMP4(ctx, path); !errors.Is(err, ErrAnalysisLimit) {
		t.Fatalf("frame limit=%v", err)
	}
	ctx = WithAnalysisLimits(context.Background(), AnalysisLimits{MaxFrames: 2, MaxMetadataBytes: int64(len(data) - 17)})
	if _, _, err := ExtractMP4(ctx, path); !errors.Is(err, ErrAnalysisLimit) {
		t.Fatalf("byte limit=%v", err)
	}
}

func TestTimedTextCountsSampleBytesOnceAndPreservesFrameLimit(t *testing.T) {
	data := subtitleMP4("tx3g", prefixedText("[latitude: 57 longitude: 24 rel_alt: 10]"), prefixedText("[latitude: 57.001 longitude: 24 rel_alt: 11]"))
	path := filepath.Join(t.TempDir(), "text.mp4")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := WithAnalysisLimits(context.Background(), AnalysisLimits{MaxFrames: 2, MaxMetadataBytes: int64(len(data) - 16)})
	frames, info, err := ExtractMP4(ctx, path)
	if err != nil || len(frames) != 2 || info.Protocol != "dji_text" {
		t.Fatalf("timed text limit=%v frames=%d", err, len(frames))
	}
	ctx = WithAnalysisLimits(context.Background(), AnalysisLimits{MaxFrames: 1, MaxMetadataBytes: int64(len(data) - 16)})
	if _, _, err := ExtractMP4(ctx, path); !errors.Is(err, ErrAnalysisLimit) {
		t.Fatalf("timed text frame limit=%v", err)
	}
}

type cancelReadAt struct {
	source *bytes.Reader
	cancel context.CancelFunc
	reads  int
}

func (r *cancelReadAt) ReadAt(data []byte, offset int64) (int, error) {
	r.reads++
	n, err := r.source.ReadAt(data, offset)
	if r.cancel != nil {
		r.cancel()
	}
	return n, err
}

func syntheticTrack(t *testing.T, data []byte) []byte {
	t.Helper()
	mdatSize := int(binary.BigEndian.Uint32(data))
	tracks, err := mp4Boxes(context.Background(), data[mdatSize+8:])
	if err != nil || len(tracks) != 1 {
		t.Fatalf("track fixture: %v", err)
	}
	return tracks[0].data
}

func TestMP4ChecksAggregateSampleBytesBeforeAnySampleRead(t *testing.T) {
	data := syntheticMP4()
	r := &cancelReadAt{source: bytes.NewReader(data)}
	ctx := WithAnalysisLimits(context.Background(), AnalysisLimits{MaxFrames: 6000, MaxMetadataBytes: 8})
	_, _, err := readDJITrack(ctx, r, int64(len(data)), syntheticTrack(t, data), "djmd")
	if !errors.Is(err, ErrAnalysisLimit) || r.reads != 0 {
		t.Fatalf("limit=%v reads=%d", err, r.reads)
	}
}

func TestMP4CancellationStopsInsideOneChunk(t *testing.T) {
	data := syntheticMP4()
	ctx, cancel := context.WithCancel(context.Background())
	r := &cancelReadAt{source: bytes.NewReader(data), cancel: cancel}
	_, _, err := readDJITrack(ctx, r, int64(len(data)), syntheticTrack(t, data), "djmd")
	if !errors.Is(err, context.Canceled) || r.reads != 1 {
		t.Fatalf("cancel=%v reads=%d", err, r.reads)
	}
}

func TestMetadataTablesAndProtoMapsAreBounded(t *testing.T) {
	var boxes, fields []byte
	for i := 1; i <= 66; i++ {
		boxes = append(boxes, atom("free")...)
		fields = append(fields, fieldVar(uint64(i), 1)...)
	}
	ctx := WithAnalysisLimits(context.Background(), AnalysisLimits{MaxFrames: 1, MaxMetadataBytes: 4096})
	if _, err := mp4Boxes(ctx, boxes); !errors.Is(err, ErrAnalysisLimit) {
		t.Fatalf("atom entries unbounded: %v", err)
	}
	if _, err := protoFieldsContext(ctx, fields); !errors.Is(err, ErrAnalysisLimit) {
		t.Fatalf("protobuf entries unbounded: %v", err)
	}
}

type cancelReader struct {
	cancel context.CancelFunc
	reads  int
}

func (r *cancelReader) Read(data []byte) (int, error) {
	r.reads++
	r.cancel()
	return copy(data, "1\n00:00:00,000 --> 00:00:01,000\n[latitude: 57 longitude: 24]\n"), nil
}

func TestSRTCancellationAndSubtitleFieldLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &cancelReader{cancel: cancel}
	if _, err := ParseSRTContext(ctx, r); !errors.Is(err, context.Canceled) || r.reads != 1 {
		t.Fatalf("cancel=%v reads=%d", err, r.reads)
	}
	ctx = WithAnalysisLimits(context.Background(), AnalysisLimits{MaxFrames: 1, MaxMetadataBytes: 4096})
	text := "1\n00:00:00,000 --> 00:00:01,000\n[latitude: 57 longitude: 24] " + strings.Repeat("[field: 1] ", 70)
	if _, err := ParseSRTContext(ctx, strings.NewReader(text)); !errors.Is(err, ErrAnalysisLimit) {
		t.Fatalf("subtitle fields unbounded: %v", err)
	}
}

func TestMetadataReaderExactLimitAllowsEOFButRejectsNextByte(t *testing.T) {
	for _, source := range []string{"1234", "12345"} {
		ctx := WithAnalysisLimits(context.Background(), AnalysisLimits{MaxMetadataBytes: 4})
		_, err := io.ReadAll(AnalysisReader(ctx, strings.NewReader(source)))
		if source == "1234" && err != nil {
			t.Fatal(err)
		}
		if source == "12345" && !errors.Is(err, ErrAnalysisLimit) {
			t.Fatalf("extra byte=%v", err)
		}
	}
	ctx := WithAnalysisLimits(context.Background(), AnalysisLimits{MaxMetadataBytes: math.MaxInt64})
	if data, err := io.ReadAll(AnalysisReader(ctx, strings.NewReader("1234"))); err != nil || string(data) != "1234" {
		t.Fatalf("large positive byte limit failed: %q %v", data, err)
	}
}
