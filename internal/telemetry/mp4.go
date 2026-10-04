package telemetry

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// MP4Info describes the source of native timed telemetry extraction.
type MP4Info struct {
	Protocol string
	Codec    string
	Warnings []string
}

type mp4Box struct {
	kind string
	data []byte
}

// mp4Boxes validates every atom boundary before exposing its payload.
func mp4Boxes(data []byte) ([]mp4Box, error) {
	var boxes []mp4Box
	for len(data) > 0 {
		if len(data) < 8 {
			return nil, fmt.Errorf("telemetry: truncated MP4 atom")
		}
		size, header := uint64(binary.BigEndian.Uint32(data)), uint64(8)
		if size == 1 {
			if len(data) < 16 {
				return nil, fmt.Errorf("telemetry: truncated extended MP4 atom")
			}
			size, header = binary.BigEndian.Uint64(data[8:]), 16
		} else if size == 0 {
			size = uint64(len(data))
		}
		if size < header || size > uint64(len(data)) {
			return nil, fmt.Errorf("telemetry: invalid MP4 atom size")
		}
		boxes = append(boxes, mp4Box{string(data[4:8]), data[header:size]})
		data = data[size:]
	}
	return boxes, nil
}

func childBox(data []byte, path ...string) ([]byte, error) {
	for _, kind := range path {
		boxes, err := mp4Boxes(data)
		if err != nil {
			return nil, err
		}
		data = nil
		for _, box := range boxes {
			if box.kind == kind {
				data = box.data
				break
			}
		}
		if data == nil {
			return nil, fmt.Errorf("telemetry: missing MP4 %s atom", kind)
		}
	}
	return data, nil
}

// ExtractMP4 reads DJI djmd samples directly with bounded random-access reads.
// It does not read video media into memory or invoke external programs.
// Fragmented MP4 files and unknown DJI protocols are rejected explicitly.
func ExtractMP4(ctx context.Context, path string) ([]Frame, MP4Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, MP4Info{}, fmt.Errorf("telemetry: open MP4: %w", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, MP4Info{}, fmt.Errorf("telemetry: stat MP4: %w", err)
	}
	var moov []byte
	for pos := int64(0); pos < stat.Size(); {
		var header [16]byte
		if _, err := f.ReadAt(header[:8], pos); err != nil {
			return nil, MP4Info{}, fmt.Errorf("telemetry: read MP4 atom: %w", err)
		}
		size, skip := uint64(binary.BigEndian.Uint32(header[:])), uint64(8)
		if size == 1 {
			if _, err := f.ReadAt(header[8:], pos+8); err != nil {
				return nil, MP4Info{}, fmt.Errorf("telemetry: read extended atom: %w", err)
			}
			size, skip = binary.BigEndian.Uint64(header[8:]), 16
		} else if size == 0 {
			size = uint64(stat.Size() - pos)
		}
		if size < skip || size > uint64(stat.Size()-pos) {
			return nil, MP4Info{}, fmt.Errorf("telemetry: invalid MP4 atom size")
		}
		if string(header[4:8]) == "moov" {
			if size-skip > 64<<20 {
				return nil, MP4Info{}, fmt.Errorf("telemetry: MP4 metadata exceeds 64 MiB")
			}
			moov = make([]byte, size-skip)
			if _, err := f.ReadAt(moov, pos+int64(skip)); err != nil {
				return nil, MP4Info{}, fmt.Errorf("telemetry: read moov: %w", err)
			}
			break
		}
		pos += int64(size)
	}
	if moov == nil {
		return nil, MP4Info{}, fmt.Errorf("telemetry: MP4 has no movie metadata")
	}
	tracks, err := mp4Boxes(moov)
	if err != nil {
		return nil, MP4Info{}, err
	}
	var info MP4Info
	var telemetryTrack []byte
	for _, track := range tracks {
		if track.kind != "trak" {
			continue
		}
		stsd, err := childBox(track.data, "mdia", "minf", "stbl", "stsd")
		if err != nil || len(stsd) < 16 {
			continue
		}
		if bytes.Contains(stsd, []byte("djmd")) {
			telemetryTrack = track.data
		}
		switch string(stsd[12:16]) {
		case "avc1", "avc3":
			info.Codec = "h264"
		case "hvc1", "hev1":
			info.Codec = "h265"
		}
	}
	if telemetryTrack == nil {
		return nil, info, fmt.Errorf("telemetry: no embedded DJI djmd track; import the matching SRT subtitle file")
	}
	frames, protocol, err := readDJITrack(ctx, f, stat.Size(), telemetryTrack)
	info.Protocol = protocol
	if layout, ok := djiLayouts[protocol]; ok {
		var absent []string
		if !layout.relative {
			absent = append(absent, "relative altitude")
		}
		if !layout.attitude {
			absent = append(absent, "aircraft attitude")
		}
		if !layout.gimbal {
			absent = append(absent, "gimbal attitude")
		}
		if len(absent) > 0 {
			info.Warnings = append(info.Warnings, "This DJI protocol has no documented "+strings.Join(absent, ", ")+" fields. Unavailable fields are represented as zero.")
		}
	}
	return frames, info, err
}

type sampleChunk struct{ first, count uint32 }
type sampleTiming struct{ count, delta uint32 }

func readDJITrack(ctx context.Context, r io.ReaderAt, fileSize int64, track []byte) ([]Frame, string, error) {
	mdhd, err := childBox(track, "mdia", "mdhd")
	if err != nil {
		return nil, "", err
	}
	timeOffset := 12
	if len(mdhd) > 0 && mdhd[0] == 1 {
		timeOffset = 20
	}
	if len(mdhd) < timeOffset+4 {
		return nil, "", fmt.Errorf("telemetry: truncated media header")
	}
	scale := binary.BigEndian.Uint32(mdhd[timeOffset:])
	if scale == 0 {
		return nil, "", fmt.Errorf("telemetry: invalid media timescale")
	}
	stbl, err := childBox(track, "mdia", "minf", "stbl")
	if err != nil {
		return nil, "", err
	}
	sizesData, err := childBox(stbl, "stsz")
	if err != nil || len(sizesData) < 12 {
		return nil, "", fmt.Errorf("telemetry: missing or truncated sample sizes")
	}
	constant, count := binary.BigEndian.Uint32(sizesData[4:]), binary.BigEndian.Uint32(sizesData[8:])
	if count == 0 || count > 2_000_000 || (constant == 0 && uint64(count)*4 > uint64(len(sizesData)-12)) {
		return nil, "", fmt.Errorf("telemetry: invalid sample count")
	}
	offsets, err := childBox(stbl, "stco")
	width := 4
	if err != nil {
		offsets, err = childBox(stbl, "co64")
		width = 8
	}
	if err != nil || len(offsets) < 8 {
		return nil, "", fmt.Errorf("telemetry: missing chunk offsets")
	}
	chunkCount := binary.BigEndian.Uint32(offsets[4:])
	if uint64(chunkCount)*uint64(width) > uint64(len(offsets)-8) {
		return nil, "", fmt.Errorf("telemetry: truncated chunk offsets")
	}
	chunksData, err := childBox(stbl, "stsc")
	if err != nil || len(chunksData) < 8 {
		return nil, "", fmt.Errorf("telemetry: missing sample-to-chunk table")
	}
	entryCount := binary.BigEndian.Uint32(chunksData[4:])
	if entryCount == 0 || uint64(entryCount)*12 > uint64(len(chunksData)-8) {
		return nil, "", fmt.Errorf("telemetry: invalid sample-to-chunk table")
	}
	chunks := make([]sampleChunk, entryCount)
	for i := range chunks {
		p := chunksData[8+i*12:]
		chunks[i] = sampleChunk{binary.BigEndian.Uint32(p), binary.BigEndian.Uint32(p[4:])}
		if chunks[i].count == 0 || (i == 0 && chunks[i].first != 1) || (i > 0 && chunks[i].first <= chunks[i-1].first) {
			return nil, "", fmt.Errorf("telemetry: invalid chunk layout")
		}
	}
	timesData, err := childBox(stbl, "stts")
	if err != nil || len(timesData) < 8 {
		return nil, "", fmt.Errorf("telemetry: missing sample timing")
	}
	timeCount := binary.BigEndian.Uint32(timesData[4:])
	if timeCount == 0 || uint64(timeCount)*8 > uint64(len(timesData)-8) {
		return nil, "", fmt.Errorf("telemetry: invalid sample timing")
	}
	times := make([]sampleTiming, timeCount)
	var timingSamples uint64
	for i := range times {
		p := timesData[8+i*8:]
		times[i] = sampleTiming{binary.BigEndian.Uint32(p), binary.BigEndian.Uint32(p[4:])}
		timingSamples += uint64(times[i].count)
	}
	if timingSamples != uint64(count) {
		return nil, "", fmt.Errorf("telemetry: sample timing count mismatch")
	}
	var frames []Frame
	var protocol string
	var sample, timeUsed uint32
	var ticks uint64
	chunkEntry, timeEntry := 0, 0
	for chunk := uint32(1); chunk <= chunkCount && sample < count; chunk++ {
		if err := ctx.Err(); err != nil {
			return nil, protocol, fmt.Errorf("telemetry: extraction cancelled: %w", err)
		}
		for chunkEntry+1 < len(chunks) && chunk >= chunks[chunkEntry+1].first {
			chunkEntry++
		}
		p := offsets[8+int(chunk-1)*width:]
		offset := uint64(binary.BigEndian.Uint32(p))
		if width == 8 {
			offset = binary.BigEndian.Uint64(p)
		}
		for j := uint32(0); j < chunks[chunkEntry].count && sample < count; j++ {
			size := constant
			if size == 0 {
				size = binary.BigEndian.Uint32(sizesData[12+int(sample)*4:])
			}
			if size == 0 || size > 4<<20 || offset > uint64(fileSize) || uint64(size) > uint64(fileSize)-offset {
				return nil, protocol, fmt.Errorf("telemetry: invalid DJI sample boundary")
			}
			data := make([]byte, size)
			if _, err := r.ReadAt(data, int64(offset)); err != nil {
				return nil, protocol, fmt.Errorf("telemetry: read DJI sample: %w", err)
			}
			frame, nextProtocol, hasFrame, err := decodeDJI(data, protocol)
			if err != nil {
				return nil, protocol, fmt.Errorf("telemetry: DJI sample %d: %w", sample, err)
			}
			protocol = nextProtocol
			if hasFrame {
				frame.SampleTime = time.Duration(float64(ticks) / float64(scale) * float64(time.Second))
				frames = append(frames, frame)
			}
			ticks += uint64(times[timeEntry].delta)
			timeUsed++
			if timeUsed == times[timeEntry].count && timeEntry+1 < len(times) {
				timeEntry++
				timeUsed = 0
			}
			offset += uint64(size)
			sample++
		}
	}
	if sample != count {
		return nil, protocol, fmt.Errorf("telemetry: incomplete DJI chunk table")
	}
	if len(frames) == 0 {
		return nil, protocol, fmt.Errorf("telemetry: DJI track contains no telemetry frames")
	}
	return frames, protocol, nil
}
