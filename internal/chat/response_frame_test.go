package chat

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/fatih/color"
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
