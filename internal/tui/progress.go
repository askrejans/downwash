package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"

	"github.com/askrejans/downwash/internal/pipeline"
)

// stepState tracks the display state of one pipeline step.
type stepState struct {
	Name   pipeline.StepName
	Status pipeline.StepStatus
	Detail string
}

// progressModel renders the pipeline step list with spinners and status icons.
type progressModel struct {
	steps   []stepState
	spinner spinner.Model
}

func newProgressModel(steps []pipeline.StepName) progressModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorNeonCyan)

	ss := make([]stepState, len(steps))
	for i, name := range steps {
		ss[i] = stepState{Name: name, Status: pipeline.StepPending}
	}
	return progressModel{steps: ss, spinner: s}
}

func (m *progressModel) update(step pipeline.StepName, status pipeline.StepStatus, msg string) {
	for i := range m.steps {
		if m.steps[i].Name == step {
			m.steps[i].Status = status
			if msg != "" {
				m.steps[i].Detail = msg
			}
			break
		}
	}
}

func (m progressModel) view() string {
	var sb strings.Builder
	sb.WriteString("\n")
	for _, s := range m.steps {
		var icon string
		var style lipgloss.Style
		switch s.Status {
		case pipeline.StepPending:
			icon = "  "
			style = stepPendingStyle
		case pipeline.StepRunning:
			icon = m.spinner.View()
			style = stepRunningStyle
		case pipeline.StepDone:
			icon = stepDoneStyle.Render("✓ ")
			style = stepDoneStyle
		case pipeline.StepFailed:
			icon = stepFailedStyle.Render("✗ ")
			style = stepFailedStyle
		case pipeline.StepSkipped:
			icon = "- "
			style = stepSkippedStyle
		}

		line := fmt.Sprintf("%s%s", icon, style.Render(string(s.Name)))
		if s.Detail != "" && s.Status == pipeline.StepDone {
			line += stepPendingStyle.Render("  " + s.Detail)
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}
