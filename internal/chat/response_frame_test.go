package chat

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/fatih/color"

	"duckduckgo-chat-cli/internal/ui"
)

func TestNormalizeFrameLineExpandsTabsAndKeepsStyles(t *testing.T) {
	input := "\x1b[33m \t\tfoo\x1b[0m\r\b\x1b[2K"
	got := normalizeFrameLine(input)
	if got != "\x1b[33m        foo\x1b[0m" {
		t.Fatalf("normalized line = %q", got)
	}
	if width := ansi.StringWidth(got); width != 11 {
		t.Fatalf("normalized width = %d, want 11", width)
	}
}

func TestNormalizeFrameLineKeepsHyperlinksAndDropsUnsafeEscapes(t *testing.T) {
	link := "\x1b]8;;https://example.com\x07link\x1b]8;;\x07"
	got := normalizeFrameLine(link + "\x1b[1G\x1b]0;window title\x07!")
	if got != link+"!" {
		t.Fatalf("normalized line = %q", got)
	}
	if width := ansi.StringWidth(got); width != 5 {
		t.Fatalf("normalized hyperlink width = %d, want 5", width)
	}
}

func TestResponseFrameKeepsAllBordersAligned(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	frame := newResponseFrame("GPT-6 Luna", 100)
	output := captureResponseFrame(t, func() {
		frame.Write("Example:\n\tfunc main() {\n\t\tfmt.Println(\"🙂\")\r\n\t}\n"+strings.Repeat("very-long-code-token", 10)+"\n中文 😊 \x1b[36mcolored\x1b[0m", false)
		frame.Close()
	})
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	if len(lines) < 7 {
		t.Fatalf("frame has only %d lines: %q", len(lines), output)
	}
	wantWidth := frame.width + ansi.StringWidth(frame.indent)
	for i, line := range lines {
		if strings.ContainsAny(line, "\t\r\b") {
			t.Errorf("line %d still contains a cursor control: %q", i+1, line)
		}
		if got := ansi.StringWidth(line); got != wantWidth {
			t.Errorf("line %d width = %d, want %d: %q", i+1, got, wantWidth, line)
		}
	}
}

func TestResponseFrameMatchesStyledPanelWidthAndAlignment(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	for _, terminalWidth := range []int{80, 140} {
		t.Run(fmt.Sprintf("terminal-%d", terminalWidth), func(t *testing.T) {
			frame := newResponseFrame("Coordinate", terminalWidth)
			response := captureResponseFrame(t, func() {
				frame.Write("answer", false)
				frame.Close()
			})
			panel := captureResponseFrame(t, func() {
				ui.PrintStyledPanelAtWidth("Coordinate results", []ui.PanelLine{{{Text: "details"}}}, terminalWidth)
			})

			responseTop := strings.Split(strings.TrimSuffix(response, "\n"), "\n")[0]
			panelTop := strings.Split(strings.TrimSuffix(panel, "\n"), "\n")[0]
			if got, want := ansi.StringWidth(responseTop), ansi.StringWidth(panelTop); got != want {
				t.Fatalf("response frame width = %d, panel width = %d at terminal width %d", got, want, terminalWidth)
			}
			if strings.HasPrefix(responseTop, " ") != strings.HasPrefix(panelTop, " ") {
				t.Fatalf("response and panel have different left alignment: response=%q panel=%q", responseTop, panelTop)
			}
		})
	}
}

func TestResponseFrameUsesSharedFullAndMaximumWidths(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	prior := ui.CurrentFrameOptions()
	t.Cleanup(func() { ui.SetFrameOptions(prior) })

	ui.SetFrameOptions(ui.FrameOptions{WidthMode: ui.FrameWidthFull, MaxWidth: 112, LineWeight: ui.FrameLineSingle})
	for _, terminalWidth := range []int{160, 240} {
		if frame := newResponseFrame("Assistant", terminalWidth); frame == nil || frame.width != terminalWidth {
			t.Fatalf("full-width response frame at terminal width %d = %#v", terminalWidth, frame)
		}
	}

	ui.SetFrameOptions(ui.FrameOptions{WidthMode: ui.FrameWidthMaximum, MaxWidth: 80, LineWeight: ui.FrameLineSingle})
	if frame := newResponseFrame("Assistant", 160); frame == nil || frame.width != 80 {
		t.Fatalf("maximum-width response frame = %#v, want width 80", frame)
	}
}

func TestResponseFrameHeavyAndSingleBordersStayAligned(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	prior := ui.CurrentFrameOptions()
	t.Cleanup(func() { ui.SetFrameOptions(prior) })

	for _, test := range []struct {
		name   string
		weight ui.FrameLineWeight
		left   string
		horiz  string
		right  string
	}{
		{name: "single", weight: ui.FrameLineSingle, left: "╭", horiz: "─", right: "╮"},
		{name: "heavy", weight: ui.FrameLineHeavy, left: "┏", horiz: "━", right: "┓"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ui.SetFrameOptions(ui.FrameOptions{WidthMode: ui.FrameWidthMaximum, MaxWidth: 80, LineWeight: test.weight})
			frame := newResponseFrame("Assistant", 80)
			output := captureResponseFrame(t, func() {
				frame.Write("answer", false)
				frame.Close()
			})
			lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
			if len(lines) != 3 {
				t.Fatalf("response frame has %d lines, want 3: %q", len(lines), output)
			}
			for index, line := range lines {
				if got := ansi.StringWidth(line); got != 80 {
					t.Errorf("line %d width = %d, want 80: %q", index, got, line)
				}
			}
			if !strings.HasPrefix(lines[0], test.left+test.horiz+" Assistant") || !strings.HasSuffix(lines[0], test.right) {
				t.Errorf("top border does not use %s glyphs: %q", test.name, lines[0])
			}
			if !strings.HasPrefix(lines[1], "│ answer") && !strings.HasPrefix(lines[1], "┃ answer") {
				t.Errorf("content row has no vertical border: %q", lines[1])
			}
		})
	}
}

func TestResponseFrameKeepsCompactRailAndUnframedFallback(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	prior := ui.CurrentFrameOptions()
	t.Cleanup(func() { ui.SetFrameOptions(prior) })
	ui.SetFrameOptions(ui.FrameOptions{WidthMode: ui.FrameWidthFull, MaxWidth: 112, LineWeight: ui.FrameLineHeavy})

	if frame := newResponseFrame("Assistant", 40); frame == nil || frame.mode != responseFrameRail {
		t.Fatalf("40-column response frame = %#v, want compact rail", frame)
	}
	if frame := newResponseFrame("Assistant", 31); frame != nil {
		t.Fatalf("31-column response frame = %#v, want unframed fallback", frame)
	}
}

func TestResponseRailExpandsTabsWithinTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")
	frame := newResponseFrame("GPT-6 Luna", 40)
	output := captureResponseFrame(t, func() {
		frame.Write("\tfoo\tbar\n"+strings.Repeat("x", 100), false)
		frame.Close()
	})
	for i, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if strings.ContainsRune(line, '\t') {
			t.Errorf("rail line %d contains a tab: %q", i+1, line)
		}
		if got := ansi.StringWidth(line); got > 40 {
			t.Errorf("rail line %d width = %d, exceeds terminal: %q", i+1, got, line)
		}
	}
}

func TestResponseFrameSanitizesTitle(t *testing.T) {
	frame := newResponseFrame("GPT\t\r\x1b[2K Luna", 80)
	if frame.modelName != "GPT Luna" {
		t.Fatalf("frame title = %q, want a single safe line", frame.modelName)
	}
}

func captureResponseFrame(t *testing.T, render func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previousStdout := os.Stdout
	previousColorOutput := color.Output
	previousNoColor := color.NoColor
	os.Stdout = writer
	color.Output = writer
	color.NoColor = true
	defer func() {
		os.Stdout = previousStdout
		color.Output = previousColorOutput
		color.NoColor = previousNoColor
		_ = reader.Close()
	}()
	render()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}
