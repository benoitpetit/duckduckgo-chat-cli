package ui

import (
	"os"
	"strings"
	"sync"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/fatih/color"
	"golang.org/x/term"
)

const panelMaxWidth = 112

var panelRecorder struct {
	sync.RWMutex
	record func(string, []PanelLine)
}

// PanelStyle describes the semantic color used for a span in a framed panel.
type PanelStyle uint8

const (
	PanelForeground PanelStyle = iota
	PanelUser
	PanelAssistant
	PanelAccent
	PanelMuted
	PanelInfo
	PanelSuccess
	PanelWarning
	PanelError
)

// PanelSpan is plain text that the panel renderer colors after sanitizing it.
type PanelSpan struct {
	Text  string
	Style PanelStyle
}

// PanelLine contains adjacent semantic spans rendered on one logical line.
type PanelLine []PanelSpan

// PrintPanel writes plain text lines inside a theme-aware, terminal-width
// responsive frame. Terminal control sequences in the content are stripped so
// user or model text cannot move the frame's cursor or erase its borders.
func PrintPanel(title string, lines []string) {
	styled := make([]PanelLine, 0, len(lines))
	for _, line := range lines {
		styled = append(styled, PanelLine{{Text: line, Style: PanelForeground}})
	}
	PrintStyledPanel(title, styled)
}

// PrintStyledPanel writes semantic text spans inside a responsive themed frame.
// Each span is sanitized before the theme color is applied, so untrusted text
// cannot inject terminal controls while trusted UI keeps its intended palette.
func PrintStyledPanel(title string, lines []PanelLine) {
	width := panelWidth()
	panelRecorder.RLock()
	record := panelRecorder.record
	panelRecorder.RUnlock()
	if record != nil {
		record(title, clonePanelLines(lines))
	}
	PrintStyledPanelAtWidth(title, lines, width)
}

// SetPanelRecorder installs a callback that lets a terminal UI retain panels
// for redraw after a resize. Pass nil to remove it.
func SetPanelRecorder(record func(string, []PanelLine)) {
	panelRecorder.Lock()
	panelRecorder.record = record
	panelRecorder.Unlock()
}

func clonePanelLines(lines []PanelLine) []PanelLine {
	cloned := make([]PanelLine, len(lines))
	for i, line := range lines {
		cloned[i] = append(PanelLine(nil), line...)
	}
	return cloned
}

func panelWidth() int {
	width := 80
	output := panelOutputFile()
	if term.IsTerminal(int(output.Fd())) {
		if columns, _, err := term.GetSize(int(output.Fd())); err == nil && columns > 0 {
			width = columns
		}
	}
	if width > panelMaxWidth {
		width = panelMaxWidth
	}
	if width < 8 {
		width = 8
	}
	return width
}

// PrintStyledPanelAtWidth draws a panel at a specified width without
// recording it. Conversation redraws use it to reflow retained panels.
func PrintStyledPanelAtWidth(title string, lines []PanelLine, width int) {
	if width > panelMaxWidth {
		width = panelMaxWidth
	}
	if width < 8 {
		width = 8
	}

	ascii := os.Getenv("TERM") == "dumb"
	topLeft, horizontal, topRight := "╭", "─", "╮"
	vertical, bottomLeft, bottomRight := "│", "╰", "╯"
	if ascii {
		topLeft, horizontal, topRight = "+", "-", "+"
		vertical, bottomLeft, bottomRight = "|", "+", "+"
	}
	contentWidth := width - 4
	cleanTitle := sanitizePanelLine(title)
	maxTitleWidth := max(1, width-6)
	if ansi.StringWidth(cleanTitle) > maxTitleWidth {
		cleanTitle = ansi.Truncate(cleanTitle, maxTitleWidth, "…")
	}
	topFill := max(0, width-5-ansi.StringWidth(cleanTitle))
	MutedColor.Printf("%s%s %s %s%s\n", topLeft, horizontal, AccentColor.Sprint(cleanTitle), strings.Repeat(horizontal, topFill), topRight)

	for _, spans := range lines {
		var rendered strings.Builder
		for _, span := range spans {
			text := sanitizePanelLine(span.Text)
			if text != "" {
				rendered.WriteString(panelColor(span.Style).Sprint(text))
			}
		}
		line := rendered.String()
		wrapped := []string{line}
		if line != "" {
			wrapped = strings.Split(ansi.Hardwrap(line, contentWidth, true), "\n")
		}
		for _, part := range wrapped {
			padding := max(0, contentWidth-ansi.StringWidth(part))
			MutedColor.Printf("%s ", vertical)
			WhiteColor.Printf("%s%s", part, strings.Repeat(" ", padding))
			MutedColor.Printf(" %s\n", vertical)
		}
	}
	MutedColor.Printf("%s%s%s\n", bottomLeft, strings.Repeat(horizontal, width-2), bottomRight)
}

func panelOutputFile() *os.File {
	if output, ok := color.Output.(*os.File); ok && output != nil {
		return output
	}
	return os.Stdout
}

func panelColor(style PanelStyle) themedColor {
	switch style {
	case PanelUser:
		return UserColor
	case PanelAssistant:
		return AIColor
	case PanelAccent:
		return AccentColor
	case PanelMuted:
		return MutedColor
	case PanelInfo:
		return SystemColor
	case PanelSuccess:
		return SuccessColor
	case PanelWarning:
		return WarningColor
	case PanelError:
		return ErrorColor
	default:
		return WhiteColor
	}
}

func sanitizePanelLine(line string) string {
	line = strings.ReplaceAll(ansi.Strip(line), "\t", "    ")
	line = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, line)
	return strings.TrimRight(line, " \r")
}
