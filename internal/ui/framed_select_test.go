package ui

import (
	"strings"
	"testing"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/core"
	"github.com/charmbracelet/x/ansi"
)

func TestFitFramedSelectPromptKeepsLongValuesInsideNarrowFrames(t *testing.T) {
	original := "Appearance · Frame line weight"
	prompt := &survey.Select{
		Message: "Choose a setting with a long title",
		Options: []string{original, "Full terminal"},
		Default: original,
	}
	restore := FitFramedSelectPrompt(prompt, 24)
	if len(prompt.Options) != 2 || prompt.Options[0] == original {
		t.Fatalf("fitted options = %q, want opaque display-safe option values", prompt.Options)
	}
	if got, want := ansi.StringWidth(prompt.Description(prompt.Options[0], 0)), 18; got != want {
		t.Fatalf("fitted option label width = %d, want %d", got, want)
	}
	prompt.FilterMessage = " very-long-filter-界界界"
	if got, want := ansi.StringWidth(prompt.Description("__duckchat_filter_row__", 0)), 20; got != want {
		t.Fatalf("fitted filter row width = %d, want %d", got, want)
	}
	if got, want := ansi.StringWidth(prompt.Description("__duckchat_answer_row__"+prompt.Options[0], 0)), 20; got != want {
		t.Fatalf("fitted answer row width = %d, want %d", got, want)
	}
	if got, want := FitFrameCellText("界e\u0301", 8), 8; ansi.StringWidth(got) != want {
		t.Fatalf("fitted Unicode text width = %d, want %d: %q", ansi.StringWidth(got), want, got)
	}
	if !prompt.Filter("line", prompt.Options[0], 0) {
		t.Fatal("filter did not match the original option value")
	}
	if prompt.Default != prompt.Options[0] {
		t.Fatalf("fitted default = %v, want %q", prompt.Default, prompt.Options[0])
	}

	choice := prompt.Options[0]
	restore(&choice)
	if choice != original || prompt.Options[0] != original || prompt.Message != "Choose a setting with a long title" || prompt.Default != original || prompt.FilterMessage != " very-long-filter-界界界" {
		t.Fatalf("restored selector = choice %q, prompt %+v", choice, prompt)
	}
}

func TestFramedSelectTemplateKeepsRenderedRowsInsideFrame(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	width := 24
	prompt := &survey.Select{
		Message: "Choose an Appearance · Frame line weight setting",
		Options: []string{"Appearance · Frame line weight", strings.Repeat("界", 40)},
	}
	prompt.FilterMessage = " a-long-filter-with-wide-text-界界界"
	restore := FitFramedSelectPrompt(prompt, width)
	defer restore(nil)
	prompt.Help = framedSelectSummary([]PanelLine{{{Text: strings.Repeat("summary界", 12)}}}, width)

	selectTemplate, _ := framedSelectTemplates("Configuration · Appearance · Frame line weight", width, width)
	data := survey.SelectTemplateData{
		Select:        *prompt,
		PageEntries:   core.OptionAnswerList(prompt.Options),
		SelectedIndex: 0,
		Description:   prompt.Description,
		Config:        &survey.PromptConfig{},
	}
	for _, showAnswer := range []bool{false, true} {
		data.ShowAnswer = showAnswer
		if showAnswer {
			data.Answer = prompt.Options[0]
		}
		output, _, err := core.RunTemplate(selectTemplate, data)
		if err != nil {
			t.Fatalf("render selector template: %v", err)
		}
		for index, row := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
			if got := ansi.StringWidth(row); got != width {
				t.Errorf("answer=%v row %d width = %d, want %d: %q", showAnswer, index+1, got, width, row)
			}
		}
	}
}

func TestFramedSelectSummaryKeepsMenuDetailsInsideFrame(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	lines := []PanelLine{
		{{Text: "Style        ", Style: PanelAccent}, {Text: "Photorealistic", Style: PanelForeground}},
		{{Text: "Reference    ", Style: PanelAccent}, {Text: "None", Style: PanelMuted}},
	}
	framed := framedSelectSummary(lines, 48)
	for _, want := range []string{"│", "Style", "Photorealistic", "Reference", "None"} {
		if !strings.Contains(framed, want) {
			t.Errorf("framed summary %q does not contain %q", framed, want)
		}
	}
	if !strings.Contains(framed, "Style        Photorealistic") && !strings.Contains(framed, "Style Photorealistic") {
		t.Errorf("summary does not separate label from its value: %q", framed)
	}
	for _, row := range strings.Split(strings.TrimSpace(framed), "\n") {
		if !strings.HasPrefix(row, "│ ") || !strings.HasSuffix(row, " │") {
			t.Errorf("summary row is outside the frame: %q", row)
		}
	}
}

func TestFramedSelectSummaryUsesConfiguredGlyphsAndFitsFrame(t *testing.T) {
	prior := CurrentFrameOptions()
	t.Cleanup(func() { SetFrameOptions(prior) })
	t.Setenv("TERM", "xterm-256color")
	SetFrameOptions(FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: 80, LineWeight: FrameLineHeavy})

	framed := framedSelectSummary([]PanelLine{{{Text: "Appearance", Style: PanelAccent}}}, 40)
	row := strings.TrimSpace(framed)
	if !strings.HasPrefix(row, "┃ ") || !strings.HasSuffix(row, " ┃") {
		t.Fatalf("summary row does not use heavy border glyphs: %q", row)
	}
	if got := ansi.StringWidth(row); got != 40 {
		t.Fatalf("summary row width = %d, want 40: %q", got, row)
	}
}

func TestResolveFramedSelectWidthUsesSharedWidthMode(t *testing.T) {
	prior := CurrentFrameOptions()
	t.Cleanup(func() { SetFrameOptions(prior) })

	tests := []struct {
		name    string
		options FrameOptions
		columns int
		want    int
	}{
		{name: "maximum width", options: FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: 112, LineWeight: FrameLineSingle}, columns: 160, want: 112},
		{name: "narrow terminal", options: FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: 112, LineWeight: FrameLineSingle}, columns: 80, want: 80},
		{name: "full terminal", options: FrameOptions{WidthMode: FrameWidthFull, MaxWidth: 112, LineWeight: FrameLineSingle}, columns: 160, want: 160},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			SetFrameOptions(test.options)
			if got := resolveFramedSelectWidth(test.columns); got != test.want {
				t.Fatalf("resolveFramedSelectWidth(%d) = %d, want %d", test.columns, got, test.want)
			}
		})
	}
}
