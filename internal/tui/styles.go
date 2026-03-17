// Package tui provides a terminal user interface for the downwash pipeline
// using Bubble Tea. It displays an ASCII art header, interactive file picker,
// and real-time pipeline progress.
package tui

import "github.com/charmbracelet/lipgloss"

// Cyberpunk colour palette — black, neon turquoise, greenish-blue, violet.
var (
	colorNeonCyan    = lipgloss.Color("#00FFDD") // neon turquoise
	colorNeonViolet  = lipgloss.Color("#BF40FF") // electric violet
	colorGreenBlue   = lipgloss.Color("#00CC99") // greenish-blue
	colorNeonPink    = lipgloss.Color("#FF2E97") // hot pink accent
	colorDimCyan     = lipgloss.Color("#337777") // muted cyan
	colorText        = lipgloss.Color("#B0E0E6") // powder blue text
	colorDark        = lipgloss.Color("#0A0A14") // near-black
	colorSuccess     = lipgloss.Color("#00FF88") // neon green
	colorError       = lipgloss.Color("#FF3355") // neon red
)

var (
	headerStyle = lipgloss.NewStyle().
			Foreground(colorNeonCyan).
			Bold(true)

	versionStyle = lipgloss.NewStyle().
			Foreground(colorNeonViolet).
			Bold(true)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(colorDimCyan).
			Italic(true)

	stepPendingStyle = lipgloss.NewStyle().
				Foreground(colorDimCyan)

	stepRunningStyle = lipgloss.NewStyle().
				Foreground(colorNeonCyan).
				Bold(true)

	stepDoneStyle = lipgloss.NewStyle().
			Foreground(colorSuccess)

	stepFailedStyle = lipgloss.NewStyle().
			Foreground(colorError)

	stepSkippedStyle = lipgloss.NewStyle().
				Foreground(colorDimCyan).
				Strikethrough(true)

	resultLabelStyle = lipgloss.NewStyle().
				Foreground(colorNeonViolet).
				Bold(true)

	resultValueStyle = lipgloss.NewStyle().
				Foreground(colorText)

	errorStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	pickerTitleStyle = lipgloss.NewStyle().
				Foreground(colorNeonCyan).
				Bold(true).
				MarginBottom(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDimCyan)

	optionsCursorStyle = lipgloss.NewStyle().
				Foreground(colorNeonCyan).
				Bold(true)

	optionsOnStyle = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	optionsOffStyle = lipgloss.NewStyle().
			Foreground(colorDimCyan)

	optionsLabelStyle = lipgloss.NewStyle().
				Foreground(colorText)

	optionsLabelFocusedStyle = lipgloss.NewStyle().
					Foreground(colorNeonCyan)

	optionsSectionStyle = lipgloss.NewStyle().
				Foreground(colorNeonViolet).
				Bold(true)

	optionsButtonStyle = lipgloss.NewStyle().
				Foreground(colorNeonPink).
				Bold(true)
)
