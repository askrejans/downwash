package telemetry

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// MP4Info describes the source of native timed telemetry extraction.
type MP4Info struct {
	Protocol      string
	Codec         string
	DurationS     float64
	Width, Height int
	FrameRate     float64
	Warnings      []string
}

// ErrNoTelemetry distinguishes a valid video without supported telemetry from
// malformed media or an unsupported DJI binary protocol.
var ErrNoTelemetry = errors.New("telemetry: this video has no embedded DJI telemetry track; use the original recording from the SD card or its matching SRT file")

type mp4Box struct {
	kind string
	data []byte
}

// mp4Boxes validates every atom boundary before exposing its payload.
func mp4Boxes(ctx context.Context, data []byte) ([]mp4Box, error) {
	var boxes []mp4Box
	for len(data) > 0 {
		if err := checkTableCount(ctx, uint64(len(boxes)+1)); err != nil {
			return nil, err
		}
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

func childBox(ctx context.Context, data []byte, path ...string) ([]byte, error) {
	for _, kind := range path {
		boxes, err := mp4Boxes(ctx, data)
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
		if err := ctx.Err(); err != nil {
			return nil, MP4Info{}, err
		}
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
			if err := MetadataAllocation(ctx, int64(size-skip)); err != nil {
				return nil, MP4Info{}, err
			}
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
	tracks, err := mp4Boxes(ctx, moov)
	if err != nil {
		return nil, MP4Info{}, err
	}
	var info MP4Info
	var telemetryTracks [][]byte
	var textTracks []mp4Box
	for _, track := range tracks {
		if err := ctx.Err(); err != nil {
			return nil, info, err
		}
		if track.kind != "trak" {
			continue
		}
		stsd, err := childBox(ctx, track.data, "mdia", "minf", "stbl", "stsd")
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, info, ctxErr
		}
		if errors.Is(err, ErrAnalysisLimit) {
			return nil, info, err
		}
		if err != nil || len(stsd) < 16 {
			continue
		}
		descriptions, err := mp4Boxes(ctx, stsd[8:])
		if err != nil {
			return nil, info, fmt.Errorf("telemetry: invalid sample descriptions: %w", err)
		}
		if uint32(len(descriptions)) != binary.BigEndian.Uint32(stsd[4:]) {
			return nil, info, fmt.Errorf("telemetry: sample description count mismatch")
		}
		for _, description := range descriptions {
			switch description.kind {
			case "djmd":
				telemetryTracks = append(telemetryTracks, track.data)
			case "text", "tx3g":
				textTracks = append(textTracks, mp4Box{description.kind, track.data})
			case "avc1", "avc3":
				info.Codec = "h264"
			case "hvc1", "hev1":
				info.Codec = "h265"
			}
			if description.kind == "avc1" || description.kind == "avc3" || description.kind == "hvc1" || description.kind == "hev1" {
				if err := readVideoInfo(ctx, track.data, description.data, &info); err != nil {
					return nil, info, err
				}
			}
		}
	}
	if len(telemetryTracks) == 0 {
		for _, track := range textTracks {
			frames, _, err := readDJITrack(ctx, f, stat.Size(), track.data, track.kind)
			if err != nil {
				return nil, info, err
			}
			if len(frames) > 0 {
				info.Protocol = "dji_text"
				return frames, info, nil
			}
		}
		if info.Codec != "" && info.DurationS > 0 {
			if err := ctx.Err(); err != nil {
				return nil, info, err
			}
			return nil, info, ErrNoTelemetry
		}
		return nil, info, fmt.Errorf("telemetry: no supported video or DJI telemetry track")
	}
	frames, protocol, err := readDJITrack(ctx, f, stat.Size(), telemetryTracks[0], "djmd")
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

func readVideoInfo(ctx context.Context, track, description []byte, info *MP4Info) error {
	mdhd, err := childBox(ctx, track, "mdia", "mdhd")
	if err != nil {
		return err
	}
	offset, durationWidth := 12, 4
	if len(mdhd) > 0 && mdhd[0] == 1 {
		offset, durationWidth = 20, 8
	}
	if len(mdhd) < offset+4+durationWidth || mdhd[0] > 1 {
		return fmt.Errorf("telemetry: truncated or unsupported video media header")
	}
	scale := binary.BigEndian.Uint32(mdhd[offset:])
	if scale == 0 {
		return fmt.Errorf("telemetry: invalid video timescale")
	}
	duration := uint64(binary.BigEndian.Uint32(mdhd[offset+4:]))
	if durationWidth == 8 {
		duration = binary.BigEndian.Uint64(mdhd[offset+4:])
	}
	if duration == uint64(^uint32(0)) && durationWidth == 4 || duration == ^uint64(0) {
		return fmt.Errorf("telemetry: unknown video duration")
	}
	info.DurationS = float64(duration) / float64(scale)
	if len(description) >= 28 {
		info.Width, info.Height = int(binary.BigEndian.Uint16(description[24:])), int(binary.BigEndian.Uint16(description[26:]))
	}
	stts, err := childBox(ctx, track, "mdia", "minf", "stbl", "stts")
	if stopped := analysisStopped(ctx, err); stopped != nil {
		return stopped
	}
	if err != nil || len(stts) < 8 {
		return fmt.Errorf("telemetry: missing video sample timing")
	}
	count := binary.BigEndian.Uint32(stts[4:])
	if err := checkTableCount(ctx, uint64(count)); err != nil {
		return err
	}
	if uint64(count)*8 > uint64(len(stts)-8) {
		return fmt.Errorf("telemetry: truncated video sample timing")
	}
	var samples, ticks uint64
	for i := uint32(0); i < count; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := stts[8+int(i)*8:]
		n, d := uint64(binary.BigEndian.Uint32(p)), uint64(binary.BigEndian.Uint32(p[4:]))
		if n*d > ^uint64(0)-ticks {
			return fmt.Errorf("telemetry: video sample timing overflow")
		}
		samples += n
		ticks += n * d
	}
	if ticks > 0 {
		info.FrameRate = float64(samples) * float64(scale) / float64(ticks)
	}
	return nil
}

func readDJITrack(ctx context.Context, r io.ReaderAt, fileSize int64, track []byte, kind string) ([]Frame, string, error) {
	text := kind != "djmd"
	mdhd, err := childBox(ctx, track, "mdia", "mdhd")
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
	stbl, err := childBox(ctx, track, "mdia", "minf", "stbl")
	if err != nil {
		return nil, "", err
	}
	sizesData, err := childBox(ctx, stbl, "stsz")
	if stopped := analysisStopped(ctx, err); stopped != nil {
		return nil, "", stopped
	}
	if err != nil || len(sizesData) < 12 {
		return nil, "", fmt.Errorf("telemetry: missing or truncated sample sizes")
	}
	constant, count := binary.BigEndian.Uint32(sizesData[4:]), binary.BigEndian.Uint32(sizesData[8:])
	if count == 0 || count > 2_000_000 || (constant == 0 && uint64(count)*4 > uint64(len(sizesData)-12)) {
		return nil, "", fmt.Errorf("telemetry: invalid sample count")
	}
	if err := checkTableCount(ctx, uint64(count)); err != nil {
		return nil, "", err
	}
	metadataSize := uint64(constant) * uint64(count)
	if constant == 0 {
		for i := uint32(0); i < count; i++ {
			if err := ctx.Err(); err != nil {
				return nil, "", err
			}
			metadataSize += uint64(binary.BigEndian.Uint32(sizesData[12+int(i)*4:]))
		}
	}
	if err := CheckMetadataSize(ctx, int64(metadataSize)); err != nil {
		return nil, "", err
	}
	offsets, err := childBox(ctx, stbl, "stco")
	if stopped := analysisStopped(ctx, err); stopped != nil {
		return nil, "", stopped
	}
	width := 4
	if err != nil {
		offsets, err = childBox(ctx, stbl, "co64")
		if stopped := analysisStopped(ctx, err); stopped != nil {
			return nil, "", stopped
		}
		width = 8
	}
	if err != nil || len(offsets) < 8 {
		return nil, "", fmt.Errorf("telemetry: missing chunk offsets")
	}
	chunkCount := binary.BigEndian.Uint32(offsets[4:])
	if uint64(chunkCount)*uint64(width) > uint64(len(offsets)-8) {
		return nil, "", fmt.Errorf("telemetry: truncated chunk offsets")
	}
	chunksData, err := childBox(ctx, stbl, "stsc")
	if stopped := analysisStopped(ctx, err); stopped != nil {
		return nil, "", stopped
	}
	if err != nil || len(chunksData) < 8 {
		return nil, "", fmt.Errorf("telemetry: missing sample-to-chunk table")
	}
	entryCount := binary.BigEndian.Uint32(chunksData[4:])
	if entryCount == 0 || uint64(entryCount)*12 > uint64(len(chunksData)-8) {
		return nil, "", fmt.Errorf("telemetry: invalid sample-to-chunk table")
	}
	for _, entries := range []uint32{count, chunkCount, entryCount} {
		if err := checkTableCount(ctx, uint64(entries)); err != nil {
			return nil, "", err
		}
	}
	chunks := make([]sampleChunk, entryCount)
	for i := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		p := chunksData[8+i*12:]
		chunks[i] = sampleChunk{binary.BigEndian.Uint32(p), binary.BigEndian.Uint32(p[4:])}
		if chunks[i].count == 0 || (i == 0 && chunks[i].first != 1) || (i > 0 && chunks[i].first <= chunks[i-1].first) {
			return nil, "", fmt.Errorf("telemetry: invalid chunk layout")
		}
	}
	timesData, err := childBox(ctx, stbl, "stts")
	if stopped := analysisStopped(ctx, err); stopped != nil {
		return nil, "", stopped
	}
	if err != nil || len(timesData) < 8 {
		return nil, "", fmt.Errorf("telemetry: missing sample timing")
	}
	timeCount := binary.BigEndian.Uint32(timesData[4:])
	if timeCount == 0 || uint64(timeCount)*8 > uint64(len(timesData)-8) {
		return nil, "", fmt.Errorf("telemetry: invalid sample timing")
	}
	if err := checkTableCount(ctx, uint64(timeCount)); err != nil {
		return nil, "", err
	}
	times := make([]sampleTiming, timeCount)
	var timingSamples uint64
	for i := range times {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		p := timesData[8+i*8:]
		times[i] = sampleTiming{binary.BigEndian.Uint32(p), binary.BigEndian.Uint32(p[4:])}
		if times[i].count == 0 {
			return nil, "", fmt.Errorf("telemetry: empty sample timing entry")
		}
		timingSamples += uint64(times[i].count)
	}
	if timingSamples != uint64(count) {
		return nil, "", fmt.Errorf("telemetry: sample timing count mismatch")
	}
	var frames []Frame
	var protocol string
	headerFields := map[string]any{}
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
			if err := ctx.Err(); err != nil {
				return nil, protocol, err
			}
			size := constant
			if size == 0 {
				size = binary.BigEndian.Uint32(sizesData[12+int(sample)*4:])
			}
			if size == 0 || size > 4<<20 || offset > uint64(fileSize) || uint64(size) > uint64(fileSize)-offset {
				return nil, protocol, fmt.Errorf("telemetry: invalid DJI sample boundary")
			}
			if err := MetadataAllocation(ctx, int64(size)); err != nil {
				return nil, protocol, err
			}
			data := make([]byte, size)
			if _, err := r.ReadAt(data, int64(offset)); err != nil {
				return nil, protocol, fmt.Errorf("telemetry: read DJI sample: %w", err)
			}
			var frame Frame
			var nextProtocol string
			var hasFrame bool
			var err error
			if text {
				if len(data) < 2 {
					return nil, protocol, fmt.Errorf("telemetry: truncated timed text sample")
				}
				length := int(binary.BigEndian.Uint16(data))
				var payload []byte
				if length <= len(data)-2 {
					payload = data[2 : 2+length]
				} else if kind == "text" && data[0] >= 32 && data[0] <= 126 {
					payload = data
				} else {
					return nil, protocol, fmt.Errorf("telemetry: invalid timed text length")
				}
				parsed, parseErr := parseSRT(ctx, strings.NewReader("1\n00:00:00,000 --> 00:00:01,000\n"+string(payload)), false)
				if stopped := analysisStopped(ctx, parseErr); stopped != nil {
					return nil, protocol, stopped
				}
				if parseErr != nil && (srtGPSRE.Match(payload) || srtKeyRE.Match(payload)) {
					return nil, protocol, fmt.Errorf("telemetry: timed DJI subtitle: %w", parseErr)
				}
				if parseErr == nil && len(parsed) > 0 {
					frame, hasFrame, nextProtocol = parsed[0], true, "dji_text"
				}
			} else {
				frame, nextProtocol, hasFrame, err = decodeDJIContext(ctx, data, protocol)
				if err != nil {
					return nil, protocol, fmt.Errorf("telemetry: DJI sample %d: %w", sample, err)
				}
				protocol = nextProtocol
				additional, err := djiAdditionalContext(ctx, data, protocol)
				if err != nil {
					return nil, protocol, fmt.Errorf("telemetry: additional fields: %w", err)
				}
				for _, key := range []string{"model", "serial_number", "frame_width_px", "frame_height_px", "frame_rate_fps"} {
					if value, ok := additional[key]; ok {
						headerFields[key] = value
					}
				}
				for key, value := range headerFields {
					additional[key] = value
				}
				if len(additional) > 0 {
					frame.Additional = additional
				}
			}
			if hasFrame {
				if err := CheckFrameCount(ctx, len(frames)+1); err != nil {
					return nil, protocol, err
				}
				nanoseconds := float64(ticks) / float64(scale) * float64(time.Second)
				if nanoseconds >= float64(int64(^uint64(0)>>1)) {
					return nil, protocol, fmt.Errorf("telemetry: sample time exceeds supported duration")
				}
				frame.SampleTime = time.Duration(nanoseconds)
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
	if len(frames) == 0 && !text {
		return nil, protocol, fmt.Errorf("telemetry: DJI track contains no telemetry frames")
	}
	return frames, protocol, nil
}

func analysisStopped(ctx context.Context, err error) error {
	if stopped := ctx.Err(); stopped != nil {
		return stopped
	}
	if errors.Is(err, ErrAnalysisLimit) {
		return err
	}
	return nil
}
