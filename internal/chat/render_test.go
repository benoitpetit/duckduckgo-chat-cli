package chat

import (
	"io"
	"os"
	"strings"
	"testing"

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
