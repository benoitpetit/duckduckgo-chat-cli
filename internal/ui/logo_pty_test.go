package ui

import (
	"bytes"
	"io"
	"os"
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

// captureAt runs write on a pseudo-terminal reporting the given column count.
func captureAt(t *testing.T, cols uint16, write func(*os.File) error) string {
	t.Helper()

	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("cannot allocate a pseudo-terminal: %v", err)
	}
	defer master.Close()

	if err := ptytest.SetSize(slave, cols, 40); err != nil {
		t.Fatalf("set pty size to %d columns: %v", cols, err)
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	// go test pipes stdout, which makes fatih/color default NoColor to true.
	previous := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = previous }()

	got, err := ptytest.Capture(master, slave, func() error { return write(slave) })
	if err != nil {
		t.Fatalf("PrintLogoBeside returned an error: %v", err)
	}
	return got
}

func TestPrintLogoBesidePutsTextToTheRightOfTheLogo(t *testing.T) {
	got := captureAt(t, 120, func(w *os.File) error {
		return PrintLogoBeside(w, []string{"FIRST-LINE", "SECOND-LINE"})
	})

	lines := strings.Split(strings.ReplaceAll(got, "\r\n", "\n"), "\n")

	if len(lines) < 2 {
		t.Fatalf("expected at least two lines, got %d", len(lines))
	}
	if !strings.HasPrefix(stripANSI(lines[0]), "▀") && !strings.HasPrefix(stripANSI(lines[0]), " ") {
		t.Errorf("line 0 should start with the logo, got %q", stripANSI(lines[0]))
	}
	if !strings.Contains(lines[0], "FIRST-LINE") {
		t.Errorf("line 0 should carry the first text line, got %q", stripANSI(lines[0]))
	}
	if !strings.Contains(lines[1], "SECOND-LINE") {
		t.Errorf("line 1 should carry the second text line, got %q", stripANSI(lines[1]))
	}
	if got := len([]rune(stripANSI(lines[0]))); got > 120 {
		t.Errorf("line is %d cells wide, wider than the 120-column terminal", got)
	}
}

func TestPrintLogoBesideStacksOnANarrowTerminal(t *testing.T) {
	got := captureAt(t, 40, func(w *os.File) error {
		return PrintLogoBeside(w, []string{"BELOW-THE-LOGO"})
	})

	lines := strings.Split(strings.ReplaceAll(got, "\r\n", "\n"), "\n")

	for i, line := range lines {
		if strings.Contains(line, "BELOW-THE-LOGO") {
			if i == 0 {
				t.Errorf("text should sit below the logo, not beside it, got %q", stripANSI(line))
			}
			return
		}
	}
	t.Error("text line was never printed")
}

func TestPrintLogoBesideWrapsLongTextToTheAvailableWidth(t *testing.T) {
	got := captureAt(t, 72, func(w *os.File) error {
		return PrintLogoBeside(w, []string{
			"      --prompt TEXT   Send one prompt and exit; use - to read from stdin",
		})
	})

	plain := stripANSI(strings.ReplaceAll(got, "\r\n", "\n"))

	for _, line := range strings.Split(plain, "\n") {
		if width := len([]rune(line)); width > 72 {
			t.Errorf("wrapped line is %d cells wide, terminal is 72: %q", width, line)
		}
	}
	// The option name and the tail of its description must both survive, which
	// means the line was carried across several rows beside the logo.
	if !strings.Contains(plain, "--prompt") {
		t.Error("expected the option name to be printed")
	}
	if !strings.Contains(plain, "stdin") {
		t.Errorf("expected the wrapped tail of the description to be printed, got %q", plain)
	}
}

func TestPrintLogoBesideWritesNoLogoToANonTerminalWriter(t *testing.T) {
	var out bytes.Buffer

	if err := PrintLogoBeside(&out, []string{"ONLY-TEXT"}); err != nil {
		t.Fatalf("PrintLogoBeside returned an error: %v", err)
	}
	if got := out.String(); got != "ONLY-TEXT\n" {
		t.Errorf("a non-terminal writer should get the text and no logo, got %q", got)
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

// TextWidth must describe the same writer PrintLogoBeside renders to, so
// callers cannot size their content against a different file.
func TestLogoTextWidthFollowsTheWriterItIsGiven(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("pty unsupported: %v", err)
	}
	defer master.Close()

	const narrow = 42
	const wide = 100
	if err := ptytest.SetSize(slave, narrow, 40); err != nil {
		t.Fatalf("set pty size: %v", err)
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	previousOutput, previousNoColor := color.Output, color.NoColor
	color.NoColor = false
	defer func() {
		color.Output, color.NoColor = previousOutput, previousNoColor
	}()

	if got := TextWidth(slave); got != narrow {
		t.Errorf("narrow terminal: TextWidth = %d, want the full width %d because the layout stacks", got, narrow)
	}

	if err := ptytest.SetSize(slave, wide, 40); err != nil {
		t.Fatalf("resize pty: %v", err)
	}
	want := layoutLogoBeside(wide).TextWidth
	if got := TextWidth(slave); got != want {
		t.Errorf("wide terminal: TextWidth = %d, want the beside column %d", got, want)
	}
}

// A non-terminal writer gets the plain fallback width: no logo, so text uses
// whatever room it was given.
func TestLogoTextWidthFallsBackForNonTerminals(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	previousNoColor := color.NoColor
	color.NoColor = false
	defer func() { color.NoColor = previousNoColor }()

	if got := TextWidth(io.Discard); got != fallbackTextWidth {
		t.Errorf("TextWidth(io.Discard) = %d, want the fallback %d", got, fallbackTextWidth)
	}
}
