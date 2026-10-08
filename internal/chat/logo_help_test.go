package chat

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/fatih/color"

	"duckduckgo-chat-cli/internal/ptytest"
)

// sgrSequence matches the colour escapes fatih/color wraps around help text.
var sgrSequence = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestPrintWelcomeMessageDrawsTheLogoOnATerminal(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("cannot allocate a pseudo-terminal: %v", err)
	}
	defer master.Close()

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	previousOutput := color.Output
	color.Output = slave
	defer func() { color.Output = previousOutput }()
	color.NoColor = false

	got, err := ptytest.Capture(master, slave, func() error {
		PrintWelcomeMessage()
		return nil
	})
	if err != nil {
		t.Fatalf("capturing /help output failed: %v", err)
	}

	if !strings.Contains(got, "▀") {
		t.Error("expected /help on a terminal to draw the logo")
	}
	if !strings.Contains(got, "DuckDuckGo AI Chat CLI - Help") {
		t.Error("expected /help to still print its title")
	}
	if strings.Index(got, "▀") > strings.Index(got, "DuckDuckGo AI Chat CLI - Help") {
		t.Error("expected the logo to appear above the help title")
	}
	plain := strings.ReplaceAll(sgrSequence.ReplaceAllString(got, ""), "\r\n", "\n")
	if !strings.Contains(plain, "\n\nDuckDuckGo AI Chat CLI - Help") {
		t.Error("expected exactly one blank line between the logo and the help title")
	}
}

func TestPrintWelcomeMessageStaysPlainWhenRedirected(t *testing.T) {
	var out bytes.Buffer

	previousOutput := color.Output
	previousNoColor := color.NoColor
	color.Output = &out
	color.NoColor = true
	defer func() {
		color.Output = previousOutput
		color.NoColor = previousNoColor
	}()

	PrintWelcomeMessage()

	got := out.String()
	if strings.Contains(got, "▀") {
		t.Error("redirected /help output must not contain half blocks")
	}
	if strings.Contains(got, "\x1b[38;2;") {
		t.Error("redirected /help output must not contain truecolor sequences")
	}
	if !strings.Contains(got, "DuckDuckGo AI Chat CLI - Help") {
		t.Error("expected /help to still print its title")
	}
}
