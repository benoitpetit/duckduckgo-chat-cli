package ui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestProgressGlyphMapsEveryStage(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	tests := []struct {
		stage ProgressStage
		want  string
	}{
		{ProgressConnecting, "↔"},
		{ProgressPreparing, "↔"},
		{ProgressSearching, "⌕"},
		{ProgressSources, "≡"},
		{ProgressResponding, "✦"},
		{ProgressImage, "▧"},
		{ProgressComplete, "✓"},
		{ProgressFailed, "!"},
		{ProgressCancelled, "!"},
	}
	for _, test := range tests {
		if got := progressGlyph(test.stage); got != test.want {
			t.Errorf("progressGlyph(%q) = %q, want %q", test.stage, got, test.want)
		}
	}
	if got := progressGlyph(ProgressStage("unknown")); got != "•" {
		t.Errorf("progressGlyph(unknown) = %q, want generic glyph %q", got, "•")
	}
}

func TestProgressGlyphUsesASCIIOnDumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if got := progressGlyph(ProgressImage); got != "I" {
		t.Fatalf("image glyph on dumb terminal = %q, want ASCII fallback I", got)
	}
	if got := progressGlyph(ProgressComplete); got != "+" {
		t.Fatalf("complete glyph on dumb terminal = %q, want ASCII fallback +", got)
	}
}

func TestEventProgressUsesTheGlyphForItsStage(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	got := eventProgressMessage(ProgressSearching, "Searching the web")
	if want := "⌕ Searching the web"; got != want {
		t.Fatalf("event progress = %q, want %q", got, want)
	}
}

func TestInitialModelSpinnerMessageLeavesLoaderAfterModel(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	got := modelSpinnerMessage("GPT-5.4 Mini")
	if want := "GPT-5.4 Mini:"; got != want {
		t.Fatalf("initial spinner message = %q, want model label before loader: %q", got, want)
	}
}

func TestEventProgressHidesModelAndKeepsIconNextToStatus(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	got := eventProgressMessage(ProgressImage, "Generating image")
	if want := "▧ Generating image"; got != want {
		t.Fatalf("event progress = %q, want the icon next to status without model: %q", got, want)
	}
}

func TestAgentSpinnerRendererPutsLoaderAfterMessageAndClearsFinalLine(t *testing.T) {
	var output bytes.Buffer
	renderer := newAgentSpinnerRenderer(&output)
	renderer.RenderFrame("⠋", "GPT-5.4 Mini:")
	renderer.RenderFrame("⠙", "⌕ Searching the web")
	renderer.RenderFinal("✓", "")
	renderer.RenderFrame("⠹", "late event")

	if got, want := output.String(), "\r\033[KGPT-5.4 Mini: ⠋\r\033[K⌕ Searching the web ⠙\r\033[K"; got != want {
		t.Fatalf("agent spinner rendering = %q, want model/status before loader and a cleared final line %q", got, want)
	}
}

func TestDisabledSpinnerDoesNotEmitAnimation(t *testing.T) {
	for _, test := range []struct {
		name     string
		disabled bool
	}{
		{name: "disabled", disabled: true},
		{name: "non-tty"},
	} {
		t.Run(test.name, func(t *testing.T) {
			originalStdout := os.Stdout
			outputFile, err := os.CreateTemp(t.TempDir(), "spinner-output-*")
			if err != nil {
				t.Fatal(err)
			}
			os.Stdout = outputFile
			SetSpinnerDisabled(test.disabled)
			spinner := StartSpinner("Connecting")
			spinner.SetProgress(ProgressSearching, "Searching the web")
			spinner.Stop()
			os.Stdout = originalStdout
			SetSpinnerDisabled(false)

			if _, err := outputFile.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}
			output, err := io.ReadAll(outputFile)
			if err != nil {
				t.Fatal(err)
			}
			if len(output) != 0 || strings.Contains(string(output), "\x1b[") {
				t.Fatalf("inactive spinner wrote terminal output %q", output)
			}
		})
	}
}

func TestLoaderSpinnerUsesASCIIFramesOnDumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")
	got := loaderSpinnerDefinition().Frames
	want := []string{"|", "/", "-", "\\"}
	if len(got) != len(want) {
		t.Fatalf("loader frames = %v, want ASCII frames %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("loader frame %d = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestFormatProgressIncludesGlyphAndLabel(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	got := eventProgressMessage(ProgressSearching, "Searching the web")
	if want := "⌕ Searching the web"; got != want {
		t.Fatalf("formatProgress() = %q, want %q", got, want)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("formatProgress() includes ANSI color escapes with plain output: %q", got)
	}
}
