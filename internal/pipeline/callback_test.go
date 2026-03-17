package pipeline

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestOnProgressCallbackCalled(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "test.mp4")
	if err := os.WriteFile(input, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var updates []StepName

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	opts := Options{
		InputPath:     input,
		OutputDir:     dir,
		SkipTelemetry: true,
		Logger:        logger,
		OnProgress: func(step StepName, status StepStatus, msg string) {
			mu.Lock()
			defer mu.Unlock()
			if status == StepRunning {
				updates = append(updates, step)
			}
		},
	}

	_, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should have received running notifications for at least codec, markdown, pdf.
	if len(updates) < 3 {
		t.Errorf("expected at least 3 step running notifications, got %d: %v", len(updates), updates)
	}

	// Verify specific steps were notified.
	has := func(name StepName) bool {
		for _, u := range updates {
			if u == name {
				return true
			}
		}
		return false
	}

	if !has(StepCodec) {
		t.Error("missing StepCodec notification")
	}
	if !has(StepMarkdown) {
		t.Error("missing StepMarkdown notification")
	}
	if !has(StepPDF) {
		t.Error("missing StepPDF notification")
	}
}

func TestOnProgressNilSafe(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "test.mp4")
	if err := os.WriteFile(input, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// OnProgress is nil — should not panic.
	opts := Options{
		InputPath:     input,
		OutputDir:     dir,
		SkipTelemetry: true,
		Logger:        logger,
		OnProgress:    nil,
	}

	_, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run() with nil OnProgress error: %v", err)
	}
}

func TestAllStepsWithTranscode(t *testing.T) {
	steps := AllSteps(true)
	if steps[len(steps)-1] != StepTranscode {
		t.Error("last step should be StepTranscode when transcode=true")
	}
}

func TestAllStepsWithoutTranscode(t *testing.T) {
	steps := AllSteps(false)
	for _, s := range steps {
		if s == StepTranscode {
			t.Error("StepTranscode should not be present when transcode=false")
		}
	}
}
