package tui

import (
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/filepicker"
	tea "charm.land/bubbletea/v2"
)

// filePickerModel wraps the bubbles filepicker filtered to MP4 files.
type filePickerModel struct {
	picker   filepicker.Model
	selected string
	isDir    bool
	err      error
}

func newFilePickerModel(startDir string) filePickerModel {
	if startDir == "" || startDir == "." {
		if home, err := os.UserHomeDir(); err == nil {
			startDir = home
		}
	}

	fp := filepicker.New()
	fp.CurrentDirectory = startDir
	fp.AllowedTypes = []string{".mp4", ".MP4", ".srt", ".SRT"}
	fp.SetHeight(15)

	return filePickerModel{picker: fp}
}

func (m filePickerModel) init() tea.Cmd {
	return m.picker.Init()
}

func (m filePickerModel) update(msg tea.Msg) (filePickerModel, tea.Cmd) {
	// Handle 's' key for selecting current directory (batch mode).
	if km, ok := msg.(tea.KeyPressMsg); ok && km.String() == "s" {
		m.selected = m.picker.CurrentDirectory
		m.isDir = true
		return m, nil
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)

	if didSelect, path := m.picker.DidSelectFile(msg); didSelect {
		m.selected = path
		m.isDir = false
		// Validate it's actually an MP4.
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".mp4" && ext != ".srt" {
			m.err = nil
			m.selected = ""
		}
	}

	if didSelect, path := m.picker.DidSelectDisabledFile(msg); didSelect {
		_ = path
		m.err = nil
	}

	return m, cmd
}

func (m filePickerModel) view() string {
	var s strings.Builder
	s.WriteString(pickerTitleStyle.Render("Select a DJI MP4 video file or SRT telemetry:"))
	s.WriteString("\n")
	s.WriteString(m.picker.View())
	s.WriteString("\n")
	s.WriteString(helpStyle.Render("  ↑/↓ navigate • enter open/select • s select folder • ← back • q quit"))
	return s.String()
}
