package pipeline

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/askrejans/downwash/internal/telemetry"
)

func TestStem(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/videos/DJI_0001.MP4", "DJI_0001"},
		{"flight.mp4", "flight"},
		{"/a/b/c.tar.gz", "c.tar"},
		{"noext", "noext"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := stem(tc.path)
			if got != tc.want {
				t.Errorf("stem(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestRunInputNotFound(t *testing.T) {
	_, err := Run(context.Background(), Options{
		InputPath: "/nonexistent/video.mp4",
		OutputDir: t.TempDir(),
		Logger:    slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
	})
	if err == nil {
		t.Fatal("expected error for missing input, got nil")
	}
}

func TestRunCreatesOutputDir(t *testing.T) {
	// Create a dummy file to act as input (telemetry will fail, but that's non-fatal).
	dir := t.TempDir()
	input := filepath.Join(dir, "test.mp4")
	if err := os.WriteFile(input, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "output", "nested")
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// SkipTelemetry so we don't need exiftool.
	// This will still fail at codec probe (no ffprobe), but that's a warning, not fatal.
	result, err := Run(context.Background(), Options{
		InputPath:     input,
		OutputDir:     outDir,
		SkipTelemetry: true,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	// Output dir should exist.
	if _, err := os.Stat(outDir); err != nil {
		t.Errorf("output dir not created: %v", err)
	}

	// Markdown report should always be generated (even without telemetry).
	if result.MarkdownPath == "" {
		t.Error("expected MarkdownPath to be set")
	}
	if _, err := os.Stat(result.MarkdownPath); err != nil {
		t.Errorf("markdown file not found: %v", err)
	}

	// PDF should also be generated.
	if result.PDFPath == "" {
		t.Error("expected PDFPath to be set")
	}

	// GPX, charts should be empty (no telemetry).
	if result.GPXPath != "" {
		t.Errorf("expected empty GPXPath without telemetry, got %q", result.GPXPath)
	}
	if result.AltPNGPath != "" {
		t.Errorf("expected empty AltPNGPath without telemetry, got %q", result.AltPNGPath)
	}
}

func TestRunNilLogger(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "test.mp4")
	if err := os.WriteFile(input, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Logger is nil — should use slog.Default() without panicking.
	_, err := Run(context.Background(), Options{
		InputPath:     input,
		OutputDir:     dir,
		SkipTelemetry: true,
	})
	if err != nil {
		t.Fatalf("Run() with nil logger: %v", err)
	}
}

func TestRunProducesMarkdownAndPDF(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "DJI_0042.mp4")
	if err := os.WriteFile(input, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	result, err := Run(context.Background(), Options{
		InputPath:     input,
		OutputDir:     dir,
		SkipTelemetry: true,
		Logger:        logger,
	})
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// Check markdown contains the stem.
	if result.MarkdownPath == "" {
		t.Fatal("MarkdownPath is empty")
	}
	data, err := os.ReadFile(result.MarkdownPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "DJI_0042") {
		t.Error("markdown should contain the file stem")
	}

	// PDF should exist and be non-empty.
	info, err := os.Stat(result.PDFPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Error("PDF file is empty")
	}
}

func TestStemEdgeCases(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"test", "test"},
		{"test.tar.gz", "test.tar"},
		{"a/b/file.mp4", "file"},
	}
	for _, tc := range cases {
		got := stem(tc.path)
		if got != tc.want {
			t.Errorf("stem(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestFilteredStepsSkipMetadata(t *testing.T) {
	opts := Options{SkipMetadata: true}
	steps := FilteredSteps(opts)
	for _, s := range steps {
		if s == StepMetadata {
			t.Error("FilteredSteps should not include StepMetadata when SkipMetadata is true")
		}
	}
}

func TestFilteredStepsZip(t *testing.T) {
	opts := Options{ZipOutput: true}
	steps := FilteredSteps(opts)
	hasZip := false
	for _, s := range steps {
		if s == StepZip {
			hasZip = true
		}
	}
	if !hasZip {
		t.Error("FilteredSteps should include StepZip when ZipOutput is true")
	}
}

func TestCollectOutputFiles(t *testing.T) {
	r := Result{
		GPXPath:      "/tmp/track.gpx",
		AltPNGPath:   "/tmp/alt.png",
		MarkdownPath: "/tmp/report.md",
	}
	files := collectOutputFiles(r)
	if len(files) != 3 {
		t.Errorf("collectOutputFiles returned %d files, want 3", len(files))
	}
}

func TestCollectOutputFilesEmpty(t *testing.T) {
	files := collectOutputFiles(Result{})
	if len(files) != 0 {
		t.Errorf("collectOutputFiles(empty) returned %d files, want 0", len(files))
	}
}

func TestCreateZip(t *testing.T) {
	dir := t.TempDir()

	// Create test files.
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")
	os.WriteFile(f1, []byte("hello"), 0o644)
	os.WriteFile(f2, []byte("world"), 0o644)

	zipPath := filepath.Join(dir, "out.zip")
	err := createZip(zipPath, []string{f1, f2})
	if err != nil {
		t.Fatalf("createZip failed: %v", err)
	}

	info, err := os.Stat(zipPath)
	if err != nil {
		t.Fatalf("zip file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("zip file is empty")
	}
}

func TestTrimFrames(t *testing.T) {
	frames := []telemetry.Frame{
		{SampleTime: 0},
		{SampleTime: 1 * time.Second},
		{SampleTime: 2 * time.Second},
		{SampleTime: 3 * time.Second},
		{SampleTime: 4 * time.Second},
	}

	// Trim 1s from start.
	trimmed := trimFrames(frames, 1000, 0)
	if len(trimmed) != 4 {
		t.Errorf("start trim: got %d frames, want 4", len(trimmed))
	}
	if trimmed[0].SampleTime != 1*time.Second {
		t.Errorf("first frame should be at 1s, got %v", trimmed[0].SampleTime)
	}

	// Trim 1s from end.
	trimmed = trimFrames(frames, 0, 1000)
	if len(trimmed) != 4 {
		t.Errorf("end trim: got %d frames, want 4", len(trimmed))
	}
	if trimmed[len(trimmed)-1].SampleTime != 3*time.Second {
		t.Errorf("last frame should be at 3s, got %v", trimmed[len(trimmed)-1].SampleTime)
	}

	// Trim both.
	trimmed = trimFrames(frames, 1000, 1000)
	if len(trimmed) != 3 {
		t.Errorf("both trim: got %d frames, want 3", len(trimmed))
	}

	// Trim everything (overlap).
	trimmed = trimFrames(frames, 3000, 3000)
	if trimmed != nil {
		t.Errorf("overlap trim: got %d frames, want nil", len(trimmed))
	}

	// Empty input.
	trimmed = trimFrames(nil, 1000, 1000)
	if len(trimmed) != 0 {
		t.Errorf("nil trim: got %d frames, want 0", len(trimmed))
	}
}

func TestTrimFramesZeroOffsets(t *testing.T) {
	frames := []telemetry.Frame{
		{SampleTime: 0},
		{SampleTime: time.Second},
	}
	trimmed := trimFrames(frames, 0, 0)
	if len(trimmed) != 2 {
		t.Errorf("zero offsets: got %d frames, want 2", len(trimmed))
	}
}

func TestRunContextCancelled(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "test.mp4")
	if err := os.WriteFile(input, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// Should complete without panicking even with cancelled context.
	// Telemetry extraction will fail (non-fatal), rest proceeds.
	_, _ = Run(ctx, Options{
		InputPath: input,
		OutputDir: dir,
		Logger:    logger,
	})
}
