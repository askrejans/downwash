// Package ffmpeg wraps the ffmpeg and ffprobe command-line tools for video
// transcoding and codec inspection.
package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
)

// Options configures a transcode operation.
type Options struct {
	InputPath  string
	OutputPath string
	// Codec is "h264" (→ libx264) or "h265" (→ libx265). Default: "h264".
	Codec string
	// Bitrate is a target video bitrate string, e.g. "15M". Default: "15M".
	Bitrate string
	// Preset controls encode speed/quality trade-off. Default: "medium".
	Preset string
	Logger *slog.Logger
}

// TranscodeError wraps an ffmpeg subprocess failure.
type TranscodeError struct {
	ExitCode int
	Stderr   string
}

func (e *TranscodeError) Error() string {
	return fmt.Sprintf("ffmpeg exited %d: %s", e.ExitCode, e.Stderr)
}

// Transcode re-encodes the input video using the supplied options.
// It streams ffmpeg stderr to slog at DEBUG level so live progress is visible
// when the caller enables debug logging.
func Transcode(ctx context.Context, opts Options) error {
	codec := opts.Codec
	if codec == "" {
		codec = "h264"
	}
	bitrate := opts.Bitrate
	if bitrate == "" {
		bitrate = "15M"
	}
	preset := opts.Preset
	if preset == "" {
		preset = "medium"
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	libCodec := "libx264"
	if codec == "h265" {
		libCodec = "libx265"
	}

	// Parse bitrate number for maxrate/bufsize calculation.
	bitrateNum, _ := strconv.Atoi(strings.TrimRight(bitrate, "MmKk"))
	unit := strings.ToUpper(string(bitrate[len(bitrate)-1]))
	maxrate := fmt.Sprintf("%d%s", bitrateNum*4/3, unit)
	bufsize := fmt.Sprintf("%d%s", bitrateNum*2, unit)

	args := []string{
		"-y",
		"-i", opts.InputPath,
		"-map", "0:v:0",
		"-c:v", libCodec,
		"-b:v", bitrate,
		"-maxrate", maxrate,
		"-bufsize", bufsize,
		"-preset", preset,
		"-profile:v", "high",
		"-level:v", "5.1",
		"-pix_fmt", "yuv420p",
		"-color_range", "tv",
		"-colorspace", "bt709",
		"-color_trc", "bt709",
		"-color_primaries", "bt709",
		"-movflags", "+faststart",
		opts.OutputPath,
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	var stderrBuf bytes.Buffer
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg: pipe stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ffmpeg: start: %w", err)
	}

	// Stream stderr to both the logger and a buffer for error reporting.
	sc := bufio.NewScanner(stderrPipe)
	for sc.Scan() {
		line := sc.Text()
		stderrBuf.WriteString(line + "\n")
		logger.Debug("ffmpeg", "line", line)
	}

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &TranscodeError{
				ExitCode: exitErr.ExitCode(),
				Stderr:   stderrBuf.String(),
			}
		}
		return fmt.Errorf("ffmpeg: wait: %w", err)
	}
	return nil
}

// ProbeCodec returns the codec name of the first video stream in videoPath
// using ffprobe. Returns an empty string if no video stream is found.
func ProbeCodec(ctx context.Context, videoPath string) (string, error) {
	out, err := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=nw=1:nk=1",
		videoPath,
	).Output()
	if err != nil {
		return "", fmt.Errorf("ffprobe: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
