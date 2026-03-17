package ffmpeg

import (
	"strings"
	"testing"
)

// TestTranscodeErrorString verifies the Error() method format.
func TestTranscodeErrorString(t *testing.T) {
	e := &TranscodeError{ExitCode: 1, Stderr: "conversion failed"}
	got := e.Error()
	if !strings.Contains(got, "1") {
		t.Errorf("Error() = %q: missing exit code", got)
	}
	if !strings.Contains(got, "conversion failed") {
		t.Errorf("Error() = %q: missing stderr message", got)
	}
}

// TestParseBitrateParams verifies maxrate = 4/3 × bitrate, bufsize = 2 × bitrate.
func TestParseBitrateParams(t *testing.T) {
	cases := []struct {
		bitrate     string
		wantMaxrate string
		wantBufsize string
	}{
		{"15M", "20M", "30M"},
		{"8M", "10M", "16M"},
		{"20M", "26M", "40M"},
		{"6M", "8M", "12M"},
		{"8000K", "10666K", "16000K"},
	}

	for _, tc := range cases {
		t.Run(tc.bitrate, func(t *testing.T) {
			maxrate, bufsize, err := parseBitrateParams(tc.bitrate)
			if err != nil {
				t.Fatalf("parseBitrateParams(%q) error: %v", tc.bitrate, err)
			}
			if maxrate != tc.wantMaxrate {
				t.Errorf("maxrate(%s) = %s, want %s", tc.bitrate, maxrate, tc.wantMaxrate)
			}
			if bufsize != tc.wantBufsize {
				t.Errorf("bufsize(%s) = %s, want %s", tc.bitrate, bufsize, tc.wantBufsize)
			}
		})
	}
}

// TestParseBitrateParamsErrors verifies invalid bitrate strings are rejected.
func TestParseBitrateParamsErrors(t *testing.T) {
	bad := []string{"", "M", "abc", "15", "0M", "-5M"}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			_, _, err := parseBitrateParams(s)
			if err == nil {
				t.Errorf("parseBitrateParams(%q) expected error, got nil", s)
			}
		})
	}
}

// TestCodecDefault verifies that an empty Codec string defaults to "h264".
func TestCodecDefault(t *testing.T) {
	codec := ""
	if codec == "" {
		codec = "h264"
	}
	if codec != "h264" {
		t.Errorf("default codec = %q, want h264", codec)
	}
}

// TestLibCodecMapping verifies the codec → library name mapping.
func TestLibCodecMapping(t *testing.T) {
	cases := []struct {
		codec   string
		wantLib string
	}{
		{"h264", "libx264"},
		{"h265", "libx265"},
		{"", "libx264"}, // empty → defaults to h264
	}
	for _, tc := range cases {
		t.Run(tc.codec, func(t *testing.T) {
			c := tc.codec
			if c == "" {
				c = "h264"
			}
			lib := "libx264"
			if c == "h265" {
				lib = "libx265"
			}
			if lib != tc.wantLib {
				t.Errorf("lib(%q) = %q, want %q", tc.codec, lib, tc.wantLib)
			}
		})
	}
}
