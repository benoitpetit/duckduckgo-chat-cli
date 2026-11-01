package ui

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/fatih/color"
)

func TestPrintStyledPanelAtWidthUsesSharedMaximumAndHeavyGlyphs(t *testing.T) {
	prior := CurrentFrameOptions()
	t.Cleanup(func() { SetFrameOptions(prior) })
	t.Setenv("TERM", "xterm-256color")
	SetFrameOptions(FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: 80, LineWeight: FrameLineHeavy})

	output := capturePanel(t, func() {
		PrintStyledPanelAtWidth("Title", []PanelLine{{{Text: "content"}}}, 144)
	})
	rows := strings.Split(strings.TrimSpace(output), "\n")
	if len(rows) != 3 {
		t.Fatalf("panel has %d rows, want 3: %q", len(rows), output)
	}
	for _, row := range rows {
		if got := ansi.StringWidth(row); got != 80 {
			t.Errorf("panel row width = %d, want 80: %q", got, row)
		}
	}
	if !strings.HasPrefix(rows[0], "┏━ Title ") || !strings.HasSuffix(rows[0], "┓") {
		t.Errorf("panel top border does not use heavy glyphs: %q", rows[0])
	}
	if !strings.HasPrefix(rows[1], "┃ content") || !strings.HasSuffix(rows[1], "┃") {
		t.Errorf("panel content does not use heavy vertical glyphs: %q", rows[1])
	}
	if want := "┗" + strings.Repeat("━", 78) + "┛"; rows[2] != want {
		t.Errorf("panel bottom border = %q, want %q", rows[2], want)
	}
}

func TestPrintStyledPanelAtWidthUsesFullTerminalWidth(t *testing.T) {
	prior := CurrentFrameOptions()
	t.Cleanup(func() { SetFrameOptions(prior) })
	SetFrameOptions(FrameOptions{WidthMode: FrameWidthFull, MaxWidth: 112, LineWeight: FrameLineSingle})

	output := capturePanel(t, func() {
		PrintStyledPanelAtWidth("Title", []PanelLine{{{Text: "content"}}}, 144)
	})
	for _, row := range strings.Split(strings.TrimSpace(output), "\n") {
		if got := ansi.StringWidth(row); got != 144 {
			t.Errorf("full-width panel row width = %d, want 144: %q", got, row)
		}
	}
}

func TestPrintStyledPanelAtWidthKeepsASCIIOnDumbTerminals(t *testing.T) {
	prior := CurrentFrameOptions()
	t.Cleanup(func() { SetFrameOptions(prior) })
	t.Setenv("TERM", "dumb")
	SetFrameOptions(FrameOptions{WidthMode: FrameWidthMaximum, MaxWidth: 80, LineWeight: FrameLineHeavy})

	output := capturePanel(t, func() {
		PrintStyledPanelAtWidth("Title", []PanelLine{{{Text: "content"}}}, 80)
	})
	if strings.ContainsAny(output, "╭╮╰╯─│┏┓┗┛━┃") || !strings.Contains(output, "+- Title") || !strings.Contains(output, "| content") {
		t.Fatalf("dumb terminal panel should use ASCII borders: %q", output)
	}
}

func TestPrintStyledPanelAtVeryNarrowWidthWrapsWithoutFrame(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	output := capturePanel(t, func() {
		PrintStyledPanelAtWidth("Long panel title", []PanelLine{{{Text: "A long line with 界 characters"}}}, 7)
	})
	for index, row := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if got := ansi.StringWidth(row); got > 7 {
			t.Errorf("narrow fallback row %d width = %d, want at most 7: %q", index+1, got, row)
		}
	}
	if strings.ContainsAny(output, "╭╮╰╯─│┏┓┗┛━┃+-|") {
		t.Fatalf("very narrow fallback should omit frame glyphs: %q", output)
	}
}

func capturePanel(t *testing.T, draw func()) string {
	t.Helper()
	oldOutput, oldNoColor := color.Output, color.NoColor
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	color.Output = writer
	color.NoColor = true
	t.Cleanup(func() {
		color.Output, color.NoColor = oldOutput, oldNoColor
		reader.Close()
	})
	draw()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}
