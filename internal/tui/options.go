package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/askrejans/downwash/internal/pipeline"
)

// optionsModel lets the user configure what to produce before processing.
type optionsModel struct {
	cursor       int
	selectedPath string
	isDir        bool

	// Output toggles (all default true).
	produceGPX      bool
	produceCharts   bool
	produceMarkdown bool
	producePDF      bool

	// Transcode settings.
	transcode        bool
	transcodeCodec   int // index into codecChoices
	transcodeBitrate int // index into bitrateChoices
	transcodePreset  int // index into presetChoices

	confirmed bool
	cancelled bool
}

var (
	codecChoices   = []string{"h264", "h265"}
	bitrateChoices = []string{"8M", "10M", "15M", "20M", "30M"}
	presetChoices  = []string{"ultrafast", "fast", "medium", "slow", "veryslow"}
)

func newOptionsModel(path string, isDir bool) optionsModel {
	return optionsModel{
		selectedPath:     path,
		isDir:            isDir,
		produceGPX:       true,
		produceCharts:    true,
		produceMarkdown:  true,
		producePDF:       true,
		transcode:        false,
		transcodeCodec:   0, // h264
		transcodeBitrate: 2, // 15M
		transcodePreset:  2, // medium
	}
}

// menuItems returns the list of visible menu items for cursor navigation.
func (m optionsModel) menuItems() []string {
	items := []string{
		"gpx", "charts", "markdown", "pdf",
		"sep1",
		"transcode",
	}
	if m.transcode {
		items = append(items, "codec", "bitrate", "preset")
	}
	items = append(items, "sep2", "start")
	return items
}

func (m optionsModel) update(msg tea.Msg) (optionsModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	items := m.menuItems()
	maxCursor := len(items) - 1

	switch km.String() {
	case "up", "k":
		m.cursor--
		if m.cursor < 0 {
			m.cursor = 0
		}
		// Skip separators.
		for m.cursor >= 0 && strings.HasPrefix(items[m.cursor], "sep") {
			m.cursor--
		}
		if m.cursor < 0 {
			m.cursor = 0
		}

	case "down", "j":
		m.cursor++
		if m.cursor > maxCursor {
			m.cursor = maxCursor
		}
		for m.cursor <= maxCursor && strings.HasPrefix(items[m.cursor], "sep") {
			m.cursor++
		}
		if m.cursor > maxCursor {
			m.cursor = maxCursor
		}

	case " ", "enter":
		item := items[m.cursor]
		switch item {
		case "gpx":
			m.produceGPX = !m.produceGPX
		case "charts":
			m.produceCharts = !m.produceCharts
		case "markdown":
			m.produceMarkdown = !m.produceMarkdown
		case "pdf":
			m.producePDF = !m.producePDF
		case "transcode":
			m.transcode = !m.transcode
			// Clamp cursor if sub-options disappear.
			if !m.transcode && m.cursor > len(m.menuItems())-1 {
				m.cursor = len(m.menuItems()) - 1
			}
		case "start":
			m.confirmed = true
		}

	case "left", "h":
		item := items[m.cursor]
		switch item {
		case "codec":
			m.transcodeCodec--
			if m.transcodeCodec < 0 {
				m.transcodeCodec = len(codecChoices) - 1
			}
		case "bitrate":
			m.transcodeBitrate--
			if m.transcodeBitrate < 0 {
				m.transcodeBitrate = len(bitrateChoices) - 1
			}
		case "preset":
			m.transcodePreset--
			if m.transcodePreset < 0 {
				m.transcodePreset = len(presetChoices) - 1
			}
		}

	case "right", "l":
		item := items[m.cursor]
		switch item {
		case "codec":
			m.transcodeCodec = (m.transcodeCodec + 1) % len(codecChoices)
		case "bitrate":
			m.transcodeBitrate = (m.transcodeBitrate + 1) % len(bitrateChoices)
		case "preset":
			m.transcodePreset = (m.transcodePreset + 1) % len(presetChoices)
		}

	case "esc", "backspace":
		m.cancelled = true
	}

	return m, nil
}

func (m optionsModel) view() string {
	var sb strings.Builder

	// Header showing selected path.
	if m.isDir {
		sb.WriteString(resultLabelStyle.Render(fmt.Sprintf("  Batch mode: %s", m.selectedPath)))
	} else {
		sb.WriteString(resultLabelStyle.Render(fmt.Sprintf("  File: %s", m.selectedPath)))
	}
	sb.WriteString("\n\n")

	sb.WriteString(optionsSectionStyle.Render("  Output artefacts"))
	sb.WriteString("\n")

	items := m.menuItems()
	for i, item := range items {
		if strings.HasPrefix(item, "sep") {
			sb.WriteString("\n")
			if item == "sep1" {
				sb.WriteString(optionsSectionStyle.Render("  Video"))
				sb.WriteString("\n")
			}
			continue
		}

		cursor := "  "
		if i == m.cursor {
			cursor = optionsCursorStyle.Render("▸ ")
		}

		switch item {
		case "gpx":
			sb.WriteString(fmt.Sprintf("  %s%s  %s\n", cursor, toggleStr(m.produceGPX), optionLabel("GPX track", i == m.cursor)))
		case "charts":
			sb.WriteString(fmt.Sprintf("  %s%s  %s\n", cursor, toggleStr(m.produceCharts), optionLabel("Charts (altitude + track map)", i == m.cursor)))
		case "markdown":
			sb.WriteString(fmt.Sprintf("  %s%s  %s\n", cursor, toggleStr(m.produceMarkdown), optionLabel("Markdown report", i == m.cursor)))
		case "pdf":
			sb.WriteString(fmt.Sprintf("  %s%s  %s\n", cursor, toggleStr(m.producePDF), optionLabel("PDF briefing", i == m.cursor)))
		case "transcode":
			sb.WriteString(fmt.Sprintf("  %s%s  %s\n", cursor, toggleStr(m.transcode), optionLabel("Transcode video", i == m.cursor)))
		case "codec":
			sb.WriteString(fmt.Sprintf("  %s     Codec: %s\n", cursor, cycleStr(codecChoices, m.transcodeCodec, i == m.cursor)))
		case "bitrate":
			sb.WriteString(fmt.Sprintf("  %s     Bitrate: %s\n", cursor, cycleStr(bitrateChoices, m.transcodeBitrate, i == m.cursor)))
		case "preset":
			sb.WriteString(fmt.Sprintf("  %s     Preset: %s\n", cursor, cycleStr(presetChoices, m.transcodePreset, i == m.cursor)))
		case "start":
			label := "Start Processing"
			if m.isDir {
				label = "Start Batch Processing"
			}
			if i == m.cursor {
				sb.WriteString(fmt.Sprintf("\n  %s%s\n", cursor, optionsButtonStyle.Render("[ "+label+" ]")))
			} else {
				sb.WriteString(fmt.Sprintf("\n  %s%s\n", cursor, helpStyle.Render("[ "+label+" ]")))
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("  ↑/↓ navigate • space toggle • ←/→ cycle • enter confirm • esc back"))
	return sb.String()
}

// toPipelineOpts converts the options model into pipeline.Options fields.
func (m optionsModel) toPipelineOpts() pipeline.Options {
	return pipeline.Options{
		SkipGPX:          !m.produceGPX,
		SkipCharts:       !m.produceCharts,
		SkipMarkdown:     !m.produceMarkdown,
		SkipPDF:          !m.producePDF,
		Transcode:        m.transcode,
		TranscodeCodec:   codecChoices[m.transcodeCodec],
		TranscodeBitrate: bitrateChoices[m.transcodeBitrate],
		TranscodePreset:  presetChoices[m.transcodePreset],
	}
}

// ---------- view helpers -----------------------------------------------------

func toggleStr(on bool) string {
	if on {
		return optionsOnStyle.Render("[ON] ")
	}
	return optionsOffStyle.Render("[OFF]")
}

func optionLabel(label string, focused bool) string {
	if focused {
		return optionsLabelFocusedStyle.Render(label)
	}
	return optionsLabelStyle.Render(label)
}

func cycleStr(choices []string, idx int, focused bool) string {
	val := choices[idx]
	if focused {
		return optionsCursorStyle.Render("◂ " + val + " ▸")
	}
	return optionsLabelStyle.Render(val)
}
