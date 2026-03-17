package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/askrejans/downwash/internal/pipeline"
)

// ---------- header tests -----------------------------------------------------

func TestRenderHeader(t *testing.T) {
	header := renderHeader("0.1.0")
	if !strings.Contains(header, "v0.1.0") {
		t.Error("header missing version string")
	}
	if !strings.Contains(header, "DJI Post-Flight Analysis Toolkit") {
		t.Error("header missing subtitle")
	}
}

func TestRenderHeaderEmpty(t *testing.T) {
	header := renderHeader("")
	if header == "" {
		t.Error("header should not be empty even with blank version")
	}
}

// ---------- progress model tests ---------------------------------------------

func TestProgressModelNewAndView(t *testing.T) {
	steps := pipeline.AllSteps(false)
	pm := newProgressModel(steps)

	view := pm.view()
	for _, step := range steps {
		if !strings.Contains(view, string(step)) {
			t.Errorf("progress view missing step %q", step)
		}
	}
}

func TestProgressModelUpdate(t *testing.T) {
	steps := pipeline.AllSteps(true)
	pm := newProgressModel(steps)

	pm.update(pipeline.StepTelemetry, pipeline.StepRunning, "")
	pm.update(pipeline.StepTelemetry, pipeline.StepDone, "5760 frames")

	for _, s := range pm.steps {
		if s.Name == pipeline.StepTelemetry {
			if s.Status != pipeline.StepDone {
				t.Errorf("StepTelemetry status = %d, want StepDone", s.Status)
			}
			if s.Detail != "5760 frames" {
				t.Errorf("StepTelemetry detail = %q, want %q", s.Detail, "5760 frames")
			}
		}
	}

	view := pm.view()
	if !strings.Contains(view, "5760 frames") {
		t.Error("progress view missing detail for completed step")
	}
}

func TestProgressModelSkipped(t *testing.T) {
	steps := pipeline.AllSteps(false)
	pm := newProgressModel(steps)
	pm.update(pipeline.StepTelemetry, pipeline.StepSkipped, "")
	view := pm.view()
	if view == "" {
		t.Error("view should not be empty after skip")
	}
}

func TestProgressModelFailed(t *testing.T) {
	steps := pipeline.AllSteps(false)
	pm := newProgressModel(steps)
	pm.update(pipeline.StepGPX, pipeline.StepFailed, "write error")
	for _, s := range pm.steps {
		if s.Name == pipeline.StepGPX && s.Status != pipeline.StepFailed {
			t.Error("expected StepFailed status")
		}
	}
}

func TestProgressModelAllStatuses(t *testing.T) {
	steps := pipeline.AllSteps(true)
	pm := newProgressModel(steps)
	pm.update(pipeline.StepTelemetry, pipeline.StepRunning, "")
	pm.update(pipeline.StepCodec, pipeline.StepDone, "hevc")
	pm.update(pipeline.StepGPX, pipeline.StepFailed, "write err")
	pm.update(pipeline.StepAltChart, pipeline.StepSkipped, "")

	view := pm.view()
	if !strings.Contains(view, string(pipeline.StepTelemetry)) {
		t.Error("view missing running step")
	}
	if !strings.Contains(view, "hevc") {
		t.Error("view missing done step detail")
	}
}

// ---------- AllSteps / FilteredSteps tests -----------------------------------

func TestAllSteps(t *testing.T) {
	withTranscode := pipeline.AllSteps(true)
	withoutTranscode := pipeline.AllSteps(false)

	if len(withTranscode) != len(withoutTranscode)+1 {
		t.Errorf("AllSteps(true) has %d steps, AllSteps(false) has %d — expected difference of 1",
			len(withTranscode), len(withoutTranscode))
	}
	if withTranscode[len(withTranscode)-1] != pipeline.StepTranscode {
		t.Error("last step with transcode should be StepTranscode")
	}
}

func TestFilteredStepsSkips(t *testing.T) {
	opts := pipeline.Options{
		SkipGPX:    true,
		SkipCharts: true,
	}
	steps := pipeline.FilteredSteps(opts)
	for _, s := range steps {
		if s == pipeline.StepGPX || s == pipeline.StepAltChart || s == pipeline.StepTrackChart {
			t.Errorf("FilteredSteps should not include %q when skipped", s)
		}
	}
	// Should still have telemetry, codec, markdown, pdf.
	if len(steps) < 4 {
		t.Errorf("FilteredSteps returned too few steps: %d", len(steps))
	}
}

// ---------- renderResult tests -----------------------------------------------

func TestRenderResult(t *testing.T) {
	r := pipeline.Result{
		GPXPath:      "/tmp/track.gpx",
		AltPNGPath:   "/tmp/altitude.png",
		TrackPNGPath: "/tmp/track.png",
		MarkdownPath: "/tmp/report.md",
		PDFPath:      "/tmp/briefing.pdf",
	}
	out := renderResult(r)
	for _, want := range []string{"track.gpx", "altitude.png", "track.png", "report.md", "briefing.pdf"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderResult missing %q", want)
		}
	}
}

func TestRenderResultEmpty(t *testing.T) {
	out := renderResult(pipeline.Result{})
	if !strings.Contains(out, "Output artefacts") {
		t.Error("renderResult should show header even with empty result")
	}
}

func TestRenderResultWithVideo(t *testing.T) {
	r := pipeline.Result{VideoPath: "/tmp/test_h264.mp4"}
	out := renderResult(r)
	if !strings.Contains(out, "test_h264.mp4") {
		t.Error("renderResult missing video path")
	}
}

// ---------- dirOf tests ------------------------------------------------------

func TestDirOf(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/tmp/file.mp4", "/tmp"},
		{"file.mp4", ""},
		{"/a/b/c.mp4", "/a/b"},
		{"", ""},
	}
	for _, tc := range cases {
		got := dirOf(tc.path)
		if got != tc.want {
			t.Errorf("dirOf(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestDirOfWindows(t *testing.T) {
	got := dirOf(`C:\Users\test\video.mp4`)
	if got != `C:\Users\test` {
		t.Errorf("dirOf Windows path = %q, want C:\\Users\\test", got)
	}
}

// ---------- Model state tests ------------------------------------------------

func TestNewModelWithFile(t *testing.T) {
	m := New(Config{
		Version:  "0.1.0",
		FilePath: "/tmp/test.mp4",
		PipelineOpts: pipeline.Options{
			InputPath: "/tmp/test.mp4",
			OutputDir: "/tmp",
		},
	})
	// With a file, model should start at options screen.
	if m.state != stateOptions {
		t.Errorf("state = %d, want stateOptions when file is given", m.state)
	}
	if m.options.selectedPath != "/tmp/test.mp4" {
		t.Errorf("options.selectedPath = %q, want /tmp/test.mp4", m.options.selectedPath)
	}
}

func TestNewModelWithoutFile(t *testing.T) {
	m := New(Config{Version: "0.1.0"})
	if m.state != stateFilePicker {
		t.Errorf("state = %d, want stateFilePicker when no file is given", m.state)
	}
}

func TestModelViewOptions(t *testing.T) {
	m := New(Config{
		Version:  "0.1.0",
		FilePath: "/tmp/test.mp4",
	})
	view := m.View()
	if !strings.Contains(view, "v0.1.0") {
		t.Error("view missing version")
	}
	if !strings.Contains(view, "test.mp4") {
		t.Error("view missing file path")
	}
	if !strings.Contains(view, "GPX") {
		t.Error("options view missing GPX toggle")
	}
	if !strings.Contains(view, "Start Processing") {
		t.Error("options view missing start button")
	}
}

func TestModelViewDone(t *testing.T) {
	m := New(Config{Version: "0.1.0", FilePath: "/tmp/test.mp4"})
	m.state = stateDone
	m.result = pipeline.Result{
		GPXPath:      "/tmp/test_track.gpx",
		MarkdownPath: "/tmp/test_report.md",
	}
	view := m.View()
	if !strings.Contains(view, "test_track.gpx") {
		t.Error("done view missing GPX path")
	}
	if !strings.Contains(view, "enter") {
		t.Error("done view missing exit hint")
	}
}

func TestModelViewDoneWithError(t *testing.T) {
	m := New(Config{Version: "0.1.0", FilePath: "/tmp/test.mp4"})
	m.state = stateDone
	m.err = fmt.Errorf("something broke")
	view := m.View()
	if !strings.Contains(view, "something broke") {
		t.Error("done view missing error message")
	}
}

func TestModelViewFilePicker(t *testing.T) {
	m := New(Config{Version: "0.1.0"})
	view := m.View()
	if !strings.Contains(view, "Select") {
		t.Error("file picker view missing Select prompt")
	}
}

// ---------- file picker tests ------------------------------------------------

func TestNewFilePickerModel(t *testing.T) {
	fp := newFilePickerModel(".")
	if fp.selected != "" {
		t.Error("new file picker should have no selection")
	}
	if fp.isDir {
		t.Error("new file picker should not be in dir mode")
	}
}

func TestFilePickerView(t *testing.T) {
	fp := newFilePickerModel(".")
	view := fp.view()
	if !strings.Contains(view, "Select a DJI MP4 video file") {
		t.Error("file picker view missing title")
	}
	if !strings.Contains(view, "s select folder") {
		t.Error("file picker view missing folder select hint")
	}
}

func TestFilePickerSelectDir(t *testing.T) {
	fp := newFilePickerModel("/tmp")
	fp, _ = fp.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if !fp.isDir {
		t.Error("pressing 's' should set isDir")
	}
	if fp.selected == "" {
		t.Error("pressing 's' should set selected to current directory")
	}
}

// ---------- options model tests ----------------------------------------------

func TestNewOptionsModelDefaults(t *testing.T) {
	m := newOptionsModel("/tmp/test.mp4", false)
	if !m.produceGPX || !m.produceCharts || !m.produceMarkdown || !m.producePDF {
		t.Error("all outputs should be enabled by default")
	}
	if m.transcode {
		t.Error("transcode should be off by default")
	}
	if m.isDir {
		t.Error("should not be in dir mode for a file")
	}
}

func TestOptionsModelToggle(t *testing.T) {
	m := newOptionsModel("/tmp/test.mp4", false)

	// Toggle GPX off (cursor is at 0 = gpx).
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if m.produceGPX {
		t.Error("GPX should be toggled off")
	}
	// Toggle back on.
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !m.produceGPX {
		t.Error("GPX should be toggled back on")
	}
}

func TestOptionsModelNavigation(t *testing.T) {
	m := newOptionsModel("/tmp/test.mp4", false)
	if m.cursor != 0 {
		t.Errorf("cursor should start at 0, got %d", m.cursor)
	}

	// Move down to charts.
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 {
		t.Errorf("cursor should be 1 after down, got %d", m.cursor)
	}

	// Move up back to gpx.
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyUp})
	if m.cursor != 0 {
		t.Errorf("cursor should be 0 after up, got %d", m.cursor)
	}
}

func TestOptionsModelTranscodeSubOptions(t *testing.T) {
	m := newOptionsModel("/tmp/test.mp4", false)
	// Navigate to transcode toggle.
	items := m.menuItems()
	for i, item := range items {
		if item == "transcode" {
			m.cursor = i
			break
		}
	}

	// Enable transcode.
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if !m.transcode {
		t.Error("transcode should be enabled")
	}

	// Sub-options should now be visible.
	newItems := m.menuItems()
	hasCodec := false
	for _, item := range newItems {
		if item == "codec" {
			hasCodec = true
		}
	}
	if !hasCodec {
		t.Error("codec option should be visible when transcode is on")
	}
}

func TestOptionsModelCycleCodec(t *testing.T) {
	m := newOptionsModel("/tmp/test.mp4", false)
	m.transcode = true

	// Find codec item.
	items := m.menuItems()
	for i, item := range items {
		if item == "codec" {
			m.cursor = i
			break
		}
	}

	if codecChoices[m.transcodeCodec] != "h264" {
		t.Errorf("default codec should be h264, got %s", codecChoices[m.transcodeCodec])
	}

	m, _ = m.update(tea.KeyMsg{Type: tea.KeyRight})
	if codecChoices[m.transcodeCodec] != "h265" {
		t.Errorf("after right, codec should be h265, got %s", codecChoices[m.transcodeCodec])
	}

	m, _ = m.update(tea.KeyMsg{Type: tea.KeyLeft})
	if codecChoices[m.transcodeCodec] != "h264" {
		t.Errorf("after left, codec should be h264, got %s", codecChoices[m.transcodeCodec])
	}
}

func TestOptionsModelToPipelineOpts(t *testing.T) {
	m := newOptionsModel("/tmp/test.mp4", false)
	m.produceGPX = false
	m.transcode = true
	m.transcodeCodec = 1 // h265

	opts := m.toPipelineOpts()
	if !opts.SkipGPX {
		t.Error("SkipGPX should be true when GPX is disabled")
	}
	if opts.SkipCharts || opts.SkipMarkdown || opts.SkipPDF {
		t.Error("other skip flags should be false")
	}
	if !opts.Transcode {
		t.Error("Transcode should be true")
	}
	if opts.TranscodeCodec != "h265" {
		t.Errorf("TranscodeCodec = %q, want h265", opts.TranscodeCodec)
	}
}

func TestOptionsModelConfirm(t *testing.T) {
	m := newOptionsModel("/tmp/test.mp4", false)
	// Navigate to start button.
	items := m.menuItems()
	for i, item := range items {
		if item == "start" {
			m.cursor = i
			break
		}
	}
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.confirmed {
		t.Error("pressing enter on start should confirm")
	}
}

func TestOptionsModelCancel(t *testing.T) {
	m := newOptionsModel("/tmp/test.mp4", false)
	m, _ = m.update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.cancelled {
		t.Error("pressing esc should cancel")
	}
}

func TestOptionsModelView(t *testing.T) {
	m := newOptionsModel("/tmp/test.mp4", false)
	view := m.view()
	if !strings.Contains(view, "GPX") {
		t.Error("options view missing GPX")
	}
	if !strings.Contains(view, "Charts") {
		t.Error("options view missing Charts")
	}
	if !strings.Contains(view, "Markdown") {
		t.Error("options view missing Markdown")
	}
	if !strings.Contains(view, "PDF") {
		t.Error("options view missing PDF")
	}
	if !strings.Contains(view, "Transcode") {
		t.Error("options view missing Transcode")
	}
	if !strings.Contains(view, "Start Processing") {
		t.Error("options view missing Start button")
	}
}

func TestOptionsModelBatchView(t *testing.T) {
	m := newOptionsModel("/tmp/dji_media", true)
	view := m.view()
	if !strings.Contains(view, "Batch mode") {
		t.Error("batch options should show Batch mode")
	}
	if !strings.Contains(view, "Start Batch Processing") {
		t.Error("batch options should show Start Batch Processing")
	}
}

// ---------- batch helpers tests ----------------------------------------------

func TestCombineBatchErrors(t *testing.T) {
	files := []string{"a.mp4", "b.mp4", "c.mp4"}
	errs := []error{nil, fmt.Errorf("fail"), nil}
	err := combineBatchErrors(errs, files)
	if err == nil {
		t.Error("expected error for batch with failures")
	}
	if !strings.Contains(err.Error(), "1 of 3") {
		t.Errorf("error = %q, expected '1 of 3'", err.Error())
	}
}

func TestCombineBatchErrorsAllOK(t *testing.T) {
	files := []string{"a.mp4", "b.mp4"}
	errs := []error{nil, nil}
	if err := combineBatchErrors(errs, files); err != nil {
		t.Errorf("expected nil for all-success batch, got %v", err)
	}
}

func TestCombineBatchResults(t *testing.T) {
	results := []pipeline.Result{
		{MarkdownPath: "/tmp/a_report.md"},
		{MarkdownPath: "/tmp/b_report.md", PDFPath: "/tmp/b_briefing.pdf"},
	}
	combined := combineBatchResults(results)
	if combined.PDFPath != "/tmp/b_briefing.pdf" {
		t.Error("combineBatchResults should return last successful result")
	}
}
