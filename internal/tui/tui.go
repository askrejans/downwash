package tui

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/askrejans/downwash/internal/pipeline"
)

// state tracks which TUI phase is active.
type state int

const (
	stateFilePicker state = iota
	stateOptions
	stateProcessing
	stateDone
)

// Config holds all parameters the TUI needs from the CLI layer.
type Config struct {
	Version      string
	FilePath     string // if non-empty, skip file picker
	PipelineOpts pipeline.Options
}

// Model is the top-level Bubble Tea model.
type Model struct {
	cfg        Config
	state      state
	filePicker filePickerModel
	options    optionsModel
	progress   progressModel
	result     pipeline.Result
	err        error
	progressCh chan tea.Msg
	ctx        context.Context
	cancel     context.CancelFunc
	width      int
	// Batch mode state.
	batchFiles   []string
	batchIndex   int
	batchResults []pipeline.Result
	batchErrors  []error
}

// New creates the TUI model. If cfg.FilePath is set, the options screen is
// shown immediately; otherwise the file picker is shown.
func New(cfg Config) Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := Model{
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	if cfg.FilePath != "" {
		isDir := false
		if info, err := os.Stat(cfg.FilePath); err == nil && info.IsDir() {
			isDir = true
		}
		m.state = stateOptions
		m.options = newOptionsModel(cfg.FilePath, isDir)
	} else {
		m.state = stateFilePicker
		m.filePicker = newFilePickerModel("")
	}
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	if m.state == stateFilePicker {
		return m.filePicker.init()
	}
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.cancel()
			return m, tea.Quit
		case "q":
			if m.state == stateDone || m.state == stateFilePicker {
				m.cancel()
				return m, tea.Quit
			}
		case "enter":
			if m.state == stateDone {
				return m, tea.Quit
			}
		}

	case spinner.TickMsg:
		if m.state == stateProcessing {
			var cmd tea.Cmd
			m.progress.spinner, cmd = m.progress.spinner.Update(msg)
			return m, cmd
		}

	case stepUpdateMsg:
		m.progress.update(msg.Step, msg.Status, msg.Msg)
		if m.progressCh != nil {
			return m, waitForProgress(m.progressCh)
		}

	case pipelineDoneMsg:
		if len(m.batchFiles) > 0 {
			// Batch mode: save result and process next file.
			m.batchResults = append(m.batchResults, msg.Result)
			m.batchErrors = append(m.batchErrors, msg.Err)
			m.batchIndex++
			if m.batchIndex < len(m.batchFiles) {
				return m, m.startNextBatchFile()
			}
			// All batch files done.
			m.state = stateDone
			m.result = combineBatchResults(m.batchResults)
			m.err = combineBatchErrors(m.batchErrors, m.batchFiles)
		} else {
			m.state = stateDone
			m.result = msg.Result
			m.err = msg.Err
		}
		if m.progressCh != nil {
			close(m.progressCh)
			m.progressCh = nil
		}
		return m, nil
	}

	// State-specific update handling.
	switch m.state {
	case stateFilePicker:
		return m.updateFilePicker(msg)
	case stateOptions:
		return m.updateOptions(msg)
	}

	return m, nil
}

func (m Model) updateFilePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.filePicker, cmd = m.filePicker.update(msg)

	if m.filePicker.selected != "" {
		m.state = stateOptions
		m.options = newOptionsModel(m.filePicker.selected, m.filePicker.isDir)
		return m, nil
	}
	return m, cmd
}

func (m Model) updateOptions(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.options, cmd = m.options.update(msg)

	if m.options.cancelled {
		m.state = stateFilePicker
		m.filePicker.selected = ""
		m.filePicker.isDir = false
		return m, m.filePicker.init()
	}

	if m.options.confirmed {
		pipeOpts := m.options.toPipelineOpts()
		// Merge CLI-level options that aren't in the options menu.
		pipeOpts.InputPath = m.options.selectedPath
		m.cfg.PipelineOpts = pipeOpts

		if m.options.isDir {
			return m.startBatchProcessing()
		}
		return m.startSingleProcessing(m.options.selectedPath)
	}

	return m, cmd
}

func (m Model) startSingleProcessing(filePath string) (tea.Model, tea.Cmd) {
	m.cfg.FilePath = filePath
	m.cfg.PipelineOpts.InputPath = filePath
	m.state = stateProcessing
	m.progress = newProgressModel(pipeline.FilteredSteps(m.cfg.PipelineOpts))
	m.progressCh = make(chan tea.Msg, 16)
	return m, tea.Batch(
		m.progress.spinner.Tick,
		m.runPipeline(filePath),
		waitForProgress(m.progressCh),
	)
}

func (m Model) startBatchProcessing() (tea.Model, tea.Cmd) {
	dir := m.options.selectedPath
	files, err := pipeline.FindVideos(dir, false)
	if err != nil || len(files) == 0 {
		m.state = stateDone
		if err != nil {
			m.err = fmt.Errorf("scan directory: %w", err)
		} else {
			m.err = fmt.Errorf("no MP4 files found in %s", dir)
		}
		return m, nil
	}

	m.batchFiles = files
	m.batchIndex = 0
	m.batchResults = nil
	m.batchErrors = nil

	// Set output dir to <dir>/processed/.
	m.cfg.PipelineOpts.OutputDir = filepath.Join(dir, "processed")

	m.cfg.FilePath = fmt.Sprintf("%s (%d files)", dir, len(files))
	m.state = stateProcessing
	m.progress = newProgressModel(pipeline.FilteredSteps(m.cfg.PipelineOpts))
	m.progressCh = make(chan tea.Msg, 16)

	return m, tea.Batch(
		m.progress.spinner.Tick,
		m.runPipelineForFile(files[0]),
		waitForProgress(m.progressCh),
	)
}

func (m Model) startNextBatchFile() tea.Cmd {
	file := m.batchFiles[m.batchIndex]
	// Reset progress for next file.
	m.progress = newProgressModel(pipeline.FilteredSteps(m.cfg.PipelineOpts))
	return m.runPipelineForFile(file)
}

// View implements tea.Model.
func (m Model) View() string {
	var sb strings.Builder
	sb.WriteString(renderHeader(m.cfg.Version))
	sb.WriteString("\n")

	switch m.state {
	case stateFilePicker:
		sb.WriteString(m.filePicker.view())

	case stateOptions:
		sb.WriteString(m.options.view())

	case stateProcessing:
		label := m.cfg.FilePath
		if len(m.batchFiles) > 0 && m.batchIndex < len(m.batchFiles) {
			label = fmt.Sprintf("[%d/%d] %s", m.batchIndex+1, len(m.batchFiles),
				filepath.Base(m.batchFiles[m.batchIndex]))
		}
		sb.WriteString(resultLabelStyle.Render(fmt.Sprintf("  Processing: %s", label)))
		sb.WriteString("\n")
		sb.WriteString(m.progress.view())
		sb.WriteString("\n")
		sb.WriteString(helpStyle.Render("  ctrl+c cancel"))

	case stateDone:
		sb.WriteString(resultLabelStyle.Render(fmt.Sprintf("  Completed: %s", m.cfg.FilePath)))
		sb.WriteString("\n")
		sb.WriteString(m.progress.view())
		sb.WriteString("\n")

		if m.err != nil {
			sb.WriteString(errorStyle.Render(fmt.Sprintf("  Error: %v", m.err)))
			sb.WriteString("\n")
		}

		sb.WriteString(renderResult(m.result))
		sb.WriteString("\n")
		sb.WriteString(helpStyle.Render("  Press enter or q to exit"))
	}

	return sb.String()
}

// renderResult formats the pipeline result as styled output.
func renderResult(r pipeline.Result) string {
	var sb strings.Builder
	sb.WriteString(resultLabelStyle.Render("  Output artefacts:") + "\n")
	items := []struct{ label, path string }{
		{"GPX track", r.GPXPath},
		{"Altitude chart", r.AltPNGPath},
		{"Track map", r.TrackPNGPath},
		{"Markdown report", r.MarkdownPath},
		{"Metadata JSON", r.MetadataPath},
		{"PDF briefing", r.PDFPath},
		{"ZIP package", r.ZipPath},
		{"Transcoded video", r.VideoPath},
	}
	for _, item := range items {
		if item.path != "" {
			sb.WriteString(fmt.Sprintf("    %s  %s\n",
				resultLabelStyle.Render(item.label+":"),
				resultValueStyle.Render(item.path)))
		}
	}
	return sb.String()
}

// runPipeline starts the pipeline in a goroutine and sends progress messages.
func (m Model) runPipeline(filePath string) tea.Cmd {
	return m.runPipelineForFile(filePath)
}

// runPipelineForFile runs the pipeline for a single file.
func (m Model) runPipelineForFile(filePath string) tea.Cmd {
	ch := m.progressCh
	ctx := m.ctx
	opts := m.cfg.PipelineOpts
	opts.InputPath = filePath

	if opts.OutputDir == "" {
		opts.OutputDir = "."
		if dir := dirOf(filePath); dir != "" {
			opts.OutputDir = dir
		}
	}

	// Suppress slog output in TUI mode.
	opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	return func() tea.Msg {
		opts.OnProgress = func(step pipeline.StepName, status pipeline.StepStatus, msg string) {
			if ch != nil {
				ch <- stepUpdateMsg{Step: step, Status: status, Msg: msg}
			}
		}
		result, err := pipeline.Run(ctx, opts)
		return pipelineDoneMsg{Result: result, Err: err}
	}
}

// waitForProgress reads one message from the progress channel.
func waitForProgress(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// dirOf returns the directory portion of a file path.
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}

// combineBatchResults merges multiple pipeline results into one summary.
func combineBatchResults(results []pipeline.Result) pipeline.Result {
	if len(results) == 0 {
		return pipeline.Result{}
	}
	// Return the last successful result for display purposes.
	for i := len(results) - 1; i >= 0; i-- {
		if results[i].MarkdownPath != "" || results[i].PDFPath != "" {
			return results[i]
		}
	}
	return results[len(results)-1]
}

// combineBatchErrors summarises batch errors.
func combineBatchErrors(errs []error, files []string) error {
	var failures int
	for _, err := range errs {
		if err != nil {
			failures++
		}
	}
	if failures == 0 {
		return nil
	}
	return fmt.Errorf("%d of %d files failed", failures, len(files))
}
