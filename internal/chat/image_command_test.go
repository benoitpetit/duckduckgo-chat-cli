package chat

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/fatih/color"
)

type imageSurveyCall struct {
	kind         string
	message      string
	options      []string
	defaultValue string
}

type fakeImagePromptSurvey struct {
	selections  []string
	inputs      []string
	selectErrAt int
	inputErrAt  int
	err         error
	calls       []imageSurveyCall
}

func (f *fakeImagePromptSurvey) Select(message string, options []string, defaultValue string) (string, error) {
	callIndex := len(f.calls)
	f.calls = append(f.calls, imageSurveyCall{kind: "select", message: message, options: append([]string(nil), options...), defaultValue: defaultValue})
	if f.err != nil && f.selectErrAt == callIndex {
		return "", f.err
	}
	selectionIndex := 0
	for _, call := range f.calls {
		if call.kind == "select" {
			selectionIndex++
		}
	}
	selectionIndex--
	if selectionIndex >= len(f.selections) {
		return "", errors.New("no fake selection available")
	}
	return f.selections[selectionIndex], nil
}

func (f *fakeImagePromptSurvey) Input(message string) (string, error) {
	callIndex := len(f.calls)
	f.calls = append(f.calls, imageSurveyCall{kind: "input", message: message})
	if f.err != nil && f.inputErrAt == callIndex {
		return "", f.err
	}
	inputIndex := 0
	for _, call := range f.calls {
		if call.kind == "input" {
			inputIndex++
		}
	}
	inputIndex--
	if inputIndex >= len(f.inputs) {
		return "", errors.New("no fake input available")
	}
	return f.inputs[inputIndex], nil
}

func TestBuildImagePromptPreservesIdeaAndSelections(t *testing.T) {
	idea := "  a blue bird carrying a tiny lantern  "
	options := ImagePromptOptions{
		Style:       "Watercolor illustration",
		Typography:  "No text",
		Composition: "Centered subject",
		Format:      "Square (1:1)",
	}

	prompt, err := BuildImagePrompt(idea, options)
	if err != nil {
		t.Fatalf("BuildImagePrompt() error = %v", err)
	}
	for _, want := range []string{
		"a blue bird carrying a tiny lantern",
		"Style: Watercolor illustration",
		"Typography: No text",
		"Composition: Centered subject",
		"Preferred format: Square (1:1)",
		"Generate an image",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("BuildImagePrompt() = %q, missing %q", prompt, want)
		}
	}
	if strings.Contains(prompt, "Select style") || strings.Contains(prompt, "Enter your idea") {
		t.Fatalf("survey boilerplate leaked into prompt: %q", prompt)
	}
}

func TestBuildImagePromptRejectsBlankIdea(t *testing.T) {
	if _, err := BuildImagePrompt(" \n\t ", ImagePromptOptions{}); err == nil {
		t.Fatal("BuildImagePrompt() accepted a blank idea")
	}
}

func TestCollectImagePromptAsksInSpecOrder(t *testing.T) {
	fake := &fakeImagePromptSurvey{
		selections: []string{"Photorealistic", "No text", "Editorial layout", "Landscape (16:9)"},
		inputs:     []string{"A fox reading beside a window"},
	}
	idea, options, err := collectImagePrompt(fake)
	if err != nil {
		t.Fatalf("collectImagePrompt() error = %v", err)
	}
	if idea != "A fox reading beside a window" {
		t.Fatalf("idea = %q", idea)
	}
	if options.Style != "Photorealistic" || options.Typography != "No text" || options.Composition != "Editorial layout" || options.Format != "Landscape (16:9)" {
		t.Fatalf("options = %+v", options)
	}
	wantOrder := []string{"select:Style", "select:Typography", "select:Design / composition", "select:Format", "input:Image idea"}
	gotOrder := make([]string, 0, len(fake.calls))
	for _, call := range fake.calls {
		gotOrder = append(gotOrder, call.kind+":"+call.message)
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("survey order = %v, want %v", gotOrder, wantOrder)
	}
	if fake.calls[1].defaultValue != "No text" {
		t.Fatalf("typography default = %q, want No text", fake.calls[1].defaultValue)
	}
}

func TestCollectImagePromptRequestsCustomValues(t *testing.T) {
	fake := &fakeImagePromptSurvey{
		selections: []string{"Other", "Other", "Other", "Custom aspect ratio"},
		inputs:     []string{"Woodblock print", "Hand-lettered title", "Diagonal composition", "3:2", "A lighthouse in a storm"},
	}
	idea, options, err := collectImagePrompt(fake)
	if err != nil {
		t.Fatalf("collectImagePrompt() error = %v", err)
	}
	if idea != "A lighthouse in a storm" {
		t.Fatalf("idea = %q", idea)
	}
	if options.Style != "Woodblock print" || options.Typography != "Hand-lettered title" || options.Composition != "Diagonal composition" || options.Format != "3:2" {
		t.Fatalf("custom options = %+v", options)
	}
	wantOrder := []string{
		"select:Style", "input:Describe the style", "select:Typography", "input:Describe the typography",
		"select:Design / composition", "input:Describe the composition", "select:Format", "input:Aspect ratio (for example 3:2)", "input:Image idea",
	}
	gotOrder := make([]string, 0, len(fake.calls))
	for _, call := range fake.calls {
		gotOrder = append(gotOrder, call.kind+":"+call.message)
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("custom survey order = %v, want %v", gotOrder, wantOrder)
	}
}

func TestCollectImagePromptCancellationStopsBeforeIdea(t *testing.T) {
	interrupt := errors.New("survey interrupted")
	fake := &fakeImagePromptSurvey{
		selections:  []string{"Other"},
		inputs:      []string{"custom style"},
		selectErrAt: 2,
		err:         interrupt,
	}
	_, _, err := collectImagePrompt(fake)
	if !errors.Is(err, interrupt) {
		t.Fatalf("collectImagePrompt() error = %v, want interrupt", err)
	}
	for _, call := range fake.calls {
		if call.kind == "input" && call.message == "Image idea" {
			t.Fatal("asked for idea after questionnaire cancellation")
		}
	}
}

func TestResolveImagePromptDirectSkipsSurvey(t *testing.T) {
	fake := &fakeImagePromptSurvey{}
	prompt, err := resolveImagePrompt("A paper boat on a lake", fake)
	if err != nil {
		t.Fatalf("resolveImagePrompt() error = %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("direct mode made %d survey calls", len(fake.calls))
	}
	if !strings.Contains(prompt, "A paper boat on a lake") {
		t.Fatalf("direct prompt = %q", prompt)
	}
}

func TestRunImageCommandDirectGeneratesOnce(t *testing.T) {
	fake := &fakeImagePromptSurvey{}
	generated := 0
	var generatedPrompt string
	err := runImageCommand("An orange fox under a pine tree", fake, func(prompt string) {
		generated++
		generatedPrompt = prompt
	})
	if err != nil {
		t.Fatalf("runImageCommand() error = %v", err)
	}
	if generated != 1 || !strings.Contains(generatedPrompt, "An orange fox under a pine tree") {
		t.Fatalf("generated %d times with prompt %q", generated, generatedPrompt)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("direct mode made %d survey calls", len(fake.calls))
	}
}

func TestRunImageCommandCancellationDoesNotGenerate(t *testing.T) {
	interrupt := errors.New("survey interrupted")
	fake := &fakeImagePromptSurvey{selectErrAt: 0, err: interrupt}
	generated := false
	err := runImageCommand("", fake, func(string) { generated = true })
	if !errors.Is(err, interrupt) {
		t.Fatalf("runImageCommand() error = %v, want interrupt", err)
	}
	if generated {
		t.Fatal("runImageCommand() generated an image after survey cancellation")
	}
}

func TestWithNativeImageGenerationRestoresSettings(t *testing.T) {
	chat := &Chat{NativeToolsEnabled: false, NativeWebSearch: true, NativeImageGeneration: false}
	called := false
	withNativeImageGeneration(chat, func() {
		called = true
		if !chat.NativeToolsEnabled || chat.NativeWebSearch || !chat.NativeImageGeneration {
			t.Fatalf("temporary native tool settings = enabled:%t web:%t image:%t", chat.NativeToolsEnabled, chat.NativeWebSearch, chat.NativeImageGeneration)
		}
	})
	if !called {
		t.Fatal("generation callback was not called")
	}
	if chat.NativeToolsEnabled || !chat.NativeWebSearch || chat.NativeImageGeneration {
		t.Fatalf("native tool settings after success = enabled:%t web:%t image:%t", chat.NativeToolsEnabled, chat.NativeWebSearch, chat.NativeImageGeneration)
	}
}

func TestWithNativeImageGenerationRestoresSettingsAfterPanic(t *testing.T) {
	chat := &Chat{NativeToolsEnabled: true, NativeWebSearch: true, NativeImageGeneration: false}
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("generation callback did not panic")
			}
		}()
		withNativeImageGeneration(chat, func() {
			if !chat.NativeToolsEnabled || chat.NativeWebSearch || !chat.NativeImageGeneration {
				t.Fatalf("temporary native tool settings = enabled:%t web:%t image:%t", chat.NativeToolsEnabled, chat.NativeWebSearch, chat.NativeImageGeneration)
			}
			panic("generation failed")
		})
	}()
	if !chat.NativeToolsEnabled || !chat.NativeWebSearch || chat.NativeImageGeneration {
		t.Fatalf("native tool settings after panic = enabled:%t web:%t image:%t", chat.NativeToolsEnabled, chat.NativeWebSearch, chat.NativeImageGeneration)
	}
}

func TestImageCommandSuppressesPayloadDebug(t *testing.T) {
	t.Setenv("DEBUG", "true")
	chat := &Chat{}
	if !shouldLogRequestPayload(chat) {
		t.Fatal("ordinary chat request payload should remain available in debug mode")
	}
	withNativeImageGeneration(chat, func() {
		if shouldLogRequestPayload(chat) {
			t.Fatal("image request payload would duplicate the separately logged image prompt")
		}
	})
	if !shouldLogRequestPayload(chat) {
		t.Fatal("request payload debug setting was not restored after image generation")
	}
}

func TestImagePromptDebugVisibility(t *testing.T) {
	const prompt = "Generate an image from the user's idea: a silver moon"
	for _, test := range []struct {
		name      string
		debug     string
		wantCount int
	}{
		{name: "normal mode", debug: "", wantCount: 0},
		{name: "debug mode", debug: "true", wantCount: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DEBUG", test.debug)
			output := captureImageCommandStdout(t, func() { logImagePrompt(prompt) })
			if got := strings.Count(output, prompt); got != test.wantCount {
				t.Fatalf("prompt appeared %d times in output, want %d; output=%q", got, test.wantCount, output)
			}
			if test.wantCount == 1 && !strings.Contains(output, "Image generation prompt:") {
				t.Fatalf("debug output is missing the image prompt label: %q", output)
			}
		})
	}
}

func captureImageCommandStdout(t *testing.T, run func()) string {
	t.Helper()
	originalStdout := os.Stdout
	originalColorOutput := color.Output
	outputFile, err := os.CreateTemp(t.TempDir(), "image-command-output-*")
	if err != nil {
		t.Fatal(err)
	}
	defer outputFile.Close()
	os.Stdout = outputFile
	color.Output = outputFile
	defer func() {
		os.Stdout = originalStdout
		color.Output = originalColorOutput
	}()
	run()
	if _, err := outputFile.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(outputFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(output)
}
