package chat

import (
	"io"
	"os"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/ptytest"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/fatih/color"
)

func TestStreamResponseStartsBesideModelName(t *testing.T) {
	previousStdout := os.Stdout
	previousColorOutput := color.Output
	previousNoColor := color.NoColor
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	color.Output = writer
	color.NoColor = true
	defer func() {
		os.Stdout = previousStdout
		color.Output = previousColorOutput
		color.NoColor = previousNoColor
		_ = reader.Close()
	}()

	stream := make(chan string, 1)
	stream <- "Hello from the model."
	close(stream)
	renderStreamFallback(stream, "GPT-5.4 Mini", nil)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(output), "GPT-5.4 Mini: Hello from the model.\n"; got != want {
		t.Fatalf("rendered response = %q, want model and response on one line: %q", got, want)
	}
	if strings.Contains(string(output), "GPT-5.4 Mini:\n") {
		t.Fatal("model prefix was separated from the response")
	}
}

func TestNormalizeTerminalWidthPreservesWideTerminals(t *testing.T) {
	if got := normalizeTerminalWidth(240); got != 240 {
		t.Fatalf("wide terminal width = %d, want 240", got)
	}
}

func TestNewStreamRendererUsesFullWidthFromWideTerminal(t *testing.T) {
	master, slave, err := ptytest.Open()
	if err != nil {
		t.Skipf("cannot allocate a pseudo-terminal: %v", err)
	}
	defer master.Close()
	t.Cleanup(func() { _ = slave.Close() })
	if err := ptytest.SetSize(slave, 240, 40); err != nil {
		t.Fatalf("set pseudo-terminal width: %v", err)
	}

	previousStdout := os.Stdout
	os.Stdout = slave
	t.Cleanup(func() { os.Stdout = previousStdout })
	previousOptions := ui.CurrentFrameOptions()
	t.Cleanup(func() { ui.SetFrameOptions(previousOptions) })
	ui.SetFrameOptions(ui.FrameOptions{WidthMode: ui.FrameWidthFull, MaxWidth: 112, LineWeight: ui.FrameLineSingle})
	t.Setenv("TERM", "xterm-256color")

	renderer, err := NewStreamRenderer("Assistant", true)
	if err != nil {
		t.Fatalf("create renderer on a 240-column terminal: %v", err)
	}
	if renderer.frame == nil || renderer.frame.width != 240 {
		t.Fatalf("renderer frame = %#v, want width 240", renderer.frame)
	}
}
