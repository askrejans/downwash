package pipeline

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
