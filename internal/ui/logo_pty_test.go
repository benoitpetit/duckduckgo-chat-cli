package ui

import (
	"strings"
	"testing"

	"github.com/fatih/color"

	"duckduckgo-chat-cli/internal/ptytest"
)

func TestPrintLogoDrawsHalfBlocksOnARealTerminal(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("cannot allocate a pseudo-terminal: %v", err)
	}
	defer master.Close()

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	// go test pipes stdout, which makes fatih/color default NoColor to true.
	previous := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = previous }()

	got, err := ptytest.Capture(master, slave, func() error { return PrintLogo(slave) })
	if err != nil {
		t.Fatalf("PrintLogo on a terminal returned an error: %v", err)
	}

	if !strings.Contains(got, "▀") {
		t.Error("expected upper half blocks on a real terminal")
	}
	if !strings.Contains(got, "\x1b[38;2;") {
		t.Error("expected truecolor foreground sequences on a real terminal")
	}

	// A pty rewrites "\n" as "\r\n", so the block must end with a newline and a
	// single blank separator line.
	trimmed := strings.TrimRight(got, "\r\n")
	if suffix := got[len(trimmed):]; suffix != "\r\n\r\n" {
		t.Errorf("expected PrintLogo to end with a blank separator line, got suffix %q", suffix)
	}
}

func TestPrintLogoStaysSilentOnATerminalWhenColorsAreSuppressed(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("cannot allocate a pseudo-terminal: %v", err)
	}
	defer master.Close()

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	previous := color.NoColor
	color.NoColor = true
	defer func() { color.NoColor = previous }()

	got, err := ptytest.Capture(master, slave, func() error { return PrintLogo(slave) })
	if err != nil {
		t.Fatalf("PrintLogo returned an error: %v", err)
	}
	if got != "" {
		t.Errorf("color.NoColor must suppress the logo, got %d bytes", len(got))
	}
}
