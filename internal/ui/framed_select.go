package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// AskFramedSelect renders a survey selector and its summary inside the same
// theme-aware border used by the configuration menu.
func AskFramedSelect(prompt *survey.Select, choice *string, title string, summary []PanelLine, options ...survey.AskOpt) error {
	width, cursorWidth := framedSelectWidths()
	if width < 24 {
		return survey.AskOne(prompt, choice, options...)
	}
	restorePrompt := FitFramedSelectPrompt(prompt, width)
	defer restorePrompt(choice)

	priorSelect, priorMulti := survey.SelectQuestionTemplate, survey.MultiSelectQuestionTemplate
	priorHelp := prompt.Help
	prompt.Help = framedSelectSummary(summary, width)
	survey.SelectQuestionTemplate, survey.MultiSelectQuestionTemplate = framedSelectTemplates(title, width, cursorWidth)
	defer func() {
		survey.SelectQuestionTemplate, survey.MultiSelectQuestionTemplate = priorSelect, priorMulti
		prompt.Help = priorHelp
	}()
	if err := survey.AskOne(prompt, choice, options...); err != nil {
		return err
	}
	clearFramedSelectAnswer(prompt, choice, width)
	return nil
}

func framedSelectTemplates(title string, width, cursorWidth int) (string, string) {
	contentWidth := width - 4
	filterRow := FitFrameCellText("↑/↓ move · Enter select · type to filter · ? for help", contentWidth)
	helpRow := FitFrameCellText("Use ↑/↓ to move, Enter to select, or type to filter.", contentWidth)
	if ansi.StringWidth(title) > width-6 {
		title = ansi.Truncate(title, max(1, width-6), "…")
	}

	glyphs := ResolveFrameGlyphs(os.Getenv("TERM") == "dumb")
	top := MutedColor.Sprint(glyphs.TopLeft+glyphs.Horizontal+" ") + AccentColor.Sprint(title) + MutedColor.Sprint(" "+strings.Repeat(glyphs.Horizontal, max(0, width-ansi.StringWidth(glyphs.Horizontal+" "+title+" ")-2))+glyphs.TopRight)
	bottom := MutedColor.Sprint(glyphs.BottomLeft + strings.Repeat(glyphs.Horizontal, width-2) + glyphs.BottomRight)
	verticalGlyph := glyphs.Vertical
	vertical := MutedColor.Sprint(verticalGlyph)
	focus := AccentColor.Sprint("›")

	selectTemplate := `
{{- define "option"}}
{{- printf "%s " "` + vertical + `"}}
{{- if eq .SelectedIndex .CurrentIndex}}{{printf "%s " "` + focus + `"}}{{else}}  {{end}}
{{- printf "%s" ($.GetDescription .CurrentOpt)}}
{{- printf " %s\n" "` + vertical + `"}}
{{- end}}
{{- printf "%s\n" "` + top + `"}}
{{- printf "%s" .Help}}
{{- if .ShowAnswer}}
{{- printf "%s %s %s\n" "` + vertical + `" (call .Description (printf "__duckchat_answer_row__%s" .Answer) 0) "` + vertical + `"}}
{{- printf "%s\n" "` + bottom + `"}}
{{- else}}
{{- printf "%s %s %s\n" "` + vertical + `" (call .Description "__duckchat_filter_row__" 0) "` + vertical + `"}}
{{- printf "%s %s %s\n" "` + vertical + `" "` + filterRow + `" "` + vertical + `"}}
{{- if .ShowHelp}}{{- printf "%s %s %s\n" "` + vertical + `" "` + helpRow + `" "` + vertical + `"}}{{end}}
{{- range $ix, $option := .PageEntries}}{{template "option" $.IterateOption $ix $option}}{{end}}
{{- printf "%s\n" "` + bottom + `"}}
{{- end}}`
	// Survey uses the multi-select template to calculate cursor offsets even
	// for single-select prompts, so its option rows use the same cell widths.
	offsetPadding := strings.Repeat(" ", cursorWidth+1)
	offsetTemplate := `{{- define "option"}}{{if eq .SelectedIndex .CurrentIndex}}{{printf "%s" "` + offsetPadding + `"}}{{else}}` + verticalGlyph + `   {{printf "%s" ($.GetDescription .CurrentOpt)}} ` + verticalGlyph + `{{end}}{{end}}`
	return selectTemplate, offsetTemplate
}

// FitFramedSelectPrompt temporarily replaces option values with private keys
// and exposes cell-truncated labels through Survey's description callback.
// The returned function maps the chosen key back and restores the prompt.
func FitFramedSelectPrompt(prompt *survey.Select, width int) func(*string) {
	originalOptions := append([]string(nil), prompt.Options...)
	originalMessage := prompt.Message
	originalDefault := prompt.Default
	originalDescription := prompt.Description
	originalFilter := prompt.Filter

	indices := make(map[string]int, len(originalOptions))
	displayOptions := make([]string, len(originalOptions))
	for index := range originalOptions {
		key := "\x00duckchat-option:" + strconv.Itoa(index)
		indices[key] = index
		displayOptions[index] = key
	}
	labelWidth := max(1, width-6)
	contentWidth := max(1, width-4)
	prompt.Options = displayOptions
	prompt.Description = func(value string, index int) string {
		if value == "__duckchat_filter_row__" {
			return FitFrameCellText(prompt.Message+prompt.FilterMessage, contentWidth)
		}
		if strings.HasPrefix(value, "__duckchat_answer_row__") {
			optionKey := strings.TrimPrefix(value, "__duckchat_answer_row__")
			if optionIndex, ok := indices[optionKey]; ok {
				return FitFrameCellText(prompt.Message+": "+optionLabel(originalOptions, originalDescription, optionIndex), contentWidth)
			}
			return FitFrameCellText(prompt.Message, contentWidth)
		}
		optionIndex, ok := indices[value]
		if !ok || optionIndex < 0 || optionIndex >= len(originalOptions) {
			if originalDescription != nil {
				return FitFrameCellText(originalDescription(value, index), labelWidth)
			}
			return FitFrameCellText("", labelWidth)
		}
		return FitFrameCellText(optionLabel(originalOptions, originalDescription, optionIndex), labelWidth)
	}
	prompt.Filter = func(filter, value string, index int) bool {
		optionIndex, ok := indices[value]
		if !ok || optionIndex < 0 || optionIndex >= len(originalOptions) {
			return false
		}
		if originalFilter != nil {
			return originalFilter(filter, originalOptions[optionIndex], optionIndex)
		}
		return strings.Contains(strings.ToLower(originalOptions[optionIndex]), strings.ToLower(filter))
	}
	if defaultValue, ok := originalDefault.(string); ok {
		for index, option := range originalOptions {
			if option == defaultValue {
				prompt.Default = displayOptions[index]
			}
		}
	}

	return func(choice *string) {
		if choice != nil {
			if index, ok := indices[*choice]; ok && index >= 0 && index < len(originalOptions) {
				*choice = originalOptions[index]
			}
		}
		prompt.Options = originalOptions
		prompt.Message = originalMessage
		prompt.Default = originalDefault
		prompt.Description = originalDescription
		prompt.Filter = originalFilter
	}
}

func optionLabel(options []string, describe func(string, int) string, index int) string {
	label := options[index]
	if describe != nil {
		if description := describe(options[index], index); description != "" {
			label += " · " + description
		}
	}
	return label
}

// FitFrameCellText returns sanitized text truncated and padded to a terminal
// cell width, so Unicode labels and ANSI styles cannot push a frame border.
func FitFrameCellText(text string, width int) string {
	width = max(1, width)
	text = ansi.Truncate(sanitizePanelLine(text), width, "…")
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

func framedSelectWidths() (width, cursorWidth int) {
	width, cursorWidth = 80, 10000
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if columns, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && columns > 0 {
			width, cursorWidth = columns, columns
		}
	}
	return resolveFramedSelectWidth(width), cursorWidth
}

func resolveFramedSelectWidth(terminalWidth int) int {
	if terminalWidth <= 0 {
		terminalWidth = 80
	}
	return ResolveFrameWidth(terminalWidth)
}

func framedSelectSummary(lines []PanelLine, width int) string {
	var framed strings.Builder
	contentWidth := max(1, width-4)
	for _, line := range lines {
		var styled strings.Builder
		for _, span := range line {
			value := sanitizePanelLine(span.Text)
			if value != "" {
				if styled.Len() > 0 {
					styled.WriteByte(' ')
				}
				styled.WriteString(panelColor(span.Style).Sprint(value))
			}
		}
		parts := []string{styled.String()}
		if ansi.StringWidth(parts[0]) > contentWidth {
			parts = strings.Split(ansi.Hardwrap(parts[0], contentWidth, true), "\n")
		}
		for _, part := range parts {
			padding := max(0, contentWidth-ansi.StringWidth(part))
			vertical := MutedColor.Sprint(ResolveFrameGlyphs(os.Getenv("TERM") == "dumb").Vertical)
			fmt.Fprintf(&framed, "%s %s%s %s\n", vertical, part, strings.Repeat(" ", padding), vertical)
		}
	}
	return framed.String()
}

func clearFramedSelectAnswer(prompt *survey.Select, choice *string, width int) {
	if !term.IsTerminal(int(os.Stdout.Fd())) || choice == nil {
		return
	}
	helpRows := 0
	if prompt.Help != "" {
		helpRows = len(strings.Split(strings.TrimSuffix(prompt.Help, "\n"), "\n"))
	}
	rows := helpRows + 3 // one answer row, plus the top and bottom borders
	cursor := &terminal.Cursor{Out: os.Stdout}
	if err := cursor.Up(1); err != nil {
		return
	}
	for row := 0; row < rows; row++ {
		if err := cursor.HorizontalAbsolute(0); err != nil {
			return
		}
		if err := terminal.EraseLine(os.Stdout, terminal.ERASE_LINE_ALL); err != nil {
			return
		}
		if row+1 < rows {
			if err := cursor.Up(1); err != nil {
				return
			}
		}
	}
}
