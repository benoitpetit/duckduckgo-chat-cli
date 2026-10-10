package chat

import (
	"errors"
	"os"
	"strings"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	surveyterminal "github.com/AlecAivazis/survey/v2/terminal"
	"golang.org/x/term"
)

// ImagePromptOptions contains optional visual direction for image generation.
type ImagePromptOptions struct {
	Style       string
	Typography  string
	Composition string
	Format      string
}

type imagePromptSurvey interface {
	Select(message string, options []string, defaultValue string) (string, error)
	Input(message string) (string, error)
}

type terminalImagePromptSurvey struct{}

func (terminalImagePromptSurvey) Select(message string, options []string, defaultValue string) (string, error) {
	var choice string
	err := survey.AskOne(&survey.Select{Message: message, Options: options, Default: defaultValue}, &choice,
		survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	return choice, err
}

func (terminalImagePromptSurvey) Input(message string) (string, error) {
	var value string
	err := survey.AskOne(&survey.Input{Message: message}, &value,
		survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	return value, err
}

// BuildImagePrompt composes the user's idea with any selected visual direction.
func BuildImagePrompt(idea string, options ImagePromptOptions) (string, error) {
	idea = strings.TrimSpace(idea)
	if idea == "" {
		return "", errors.New("image idea cannot be empty")
	}

	lines := []string{
		"Generate an image from the user's idea. Preserve the subjects and specific details described below.",
	}
	if value := strings.TrimSpace(options.Style); value != "" {
		lines = append(lines, "Style: "+value)
	}
	if value := strings.TrimSpace(options.Typography); value != "" {
		lines = append(lines, "Typography: "+value)
	}
	if value := strings.TrimSpace(options.Composition); value != "" {
		lines = append(lines, "Composition: "+value)
	}
	if value := strings.TrimSpace(options.Format); value != "" {
		lines = append(lines, "Preferred format: "+value)
	}
	lines = append(lines, "Idea:", idea)
	return strings.Join(lines, "\n"), nil
}

func collectImagePrompt(prompts imagePromptSurvey) (string, ImagePromptOptions, error) {
	var options ImagePromptOptions
	var err error
	options.Style, err = selectImagePromptValue(prompts, "Style", []string{"Photorealistic", "Illustration", "3D render", "Pixel art", "Other"}, "Photorealistic", "Describe the style")
	if err != nil {
		return "", ImagePromptOptions{}, err
	}
	options.Typography, err = selectImagePromptValue(prompts, "Typography", []string{"No text", "Text described in the idea", "Other"}, "No text", "Describe the typography")
	if err != nil {
		return "", ImagePromptOptions{}, err
	}
	options.Composition, err = selectImagePromptValue(prompts, "Design / composition", []string{"Centered subject", "Editorial layout", "Minimalist", "Cinematic", "Other"}, "Centered subject", "Describe the composition")
	if err != nil {
		return "", ImagePromptOptions{}, err
	}

	options.Format, err = prompts.Select("Format", []string{"Square (1:1)", "Portrait (4:5)", "Landscape (16:9)", "Custom aspect ratio"}, "Square (1:1)")
	if err != nil {
		return "", ImagePromptOptions{}, err
	}
	if options.Format == "Custom aspect ratio" {
		options.Format, err = prompts.Input("Aspect ratio (for example 3:2)")
		if err != nil {
			return "", ImagePromptOptions{}, err
		}
	}

	idea, err := prompts.Input("Image idea")
	if err != nil {
		return "", ImagePromptOptions{}, err
	}
	return idea, options, nil
}

func selectImagePromptValue(prompts imagePromptSurvey, message string, options []string, defaultValue, customMessage string) (string, error) {
	value, err := prompts.Select(message, options, defaultValue)
	if err != nil || value != "Other" {
		return value, err
	}
	return prompts.Input(customMessage)
}

func resolveImagePrompt(idea string, prompts imagePromptSurvey) (string, error) {
	if strings.TrimSpace(idea) != "" {
		return BuildImagePrompt(idea, ImagePromptOptions{})
	}
	idea, options, err := collectImagePrompt(prompts)
	if err != nil {
		return "", err
	}
	return BuildImagePrompt(idea, options)
}

func runImageCommand(idea string, prompts imagePromptSurvey, generate func(string)) error {
	prompt, err := resolveImagePrompt(idea, prompts)
	if err != nil {
		return err
	}
	if generate == nil {
		return errors.New("image generator is unavailable")
	}
	generate(prompt)
	return nil
}

// HandleImageCommand starts direct or guided image generation from the CLI.
func HandleImageCommand(c *Chat, idea string, cfg *config.Config) {
	if strings.TrimSpace(idea) == "" && (!term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd()))) {
		ui.Errorln("The guided /image flow requires an interactive terminal.")
		return
	}
	err := runImageCommand(idea, terminalImagePromptSurvey{}, func(prompt string) {
		withNativeImageGeneration(c, func() {
			logImagePrompt(prompt)
			ProcessInput(c, prompt, cfg)
		})
	})
	if errors.Is(err, surveyterminal.InterruptErr) {
		return
	}
	if err != nil {
		ui.Errorln("Could not prepare image request: %v", err)
	}
}

func withNativeImageGeneration(c *Chat, run func()) {
	previousEnabled := c.NativeToolsEnabled
	previousWebSearch := c.NativeWebSearch
	previousImageGeneration := c.NativeImageGeneration
	previousSuppressDebugPayload := c.suppressDebugPayload
	defer func() {
		c.NativeToolsEnabled = previousEnabled
		c.NativeWebSearch = previousWebSearch
		c.NativeImageGeneration = previousImageGeneration
		c.suppressDebugPayload = previousSuppressDebugPayload
	}()
	c.NativeToolsEnabled = true
	c.NativeWebSearch = false
	c.NativeImageGeneration = true
	c.suppressDebugPayload = true
	run()
}

func logImagePrompt(prompt string) {
	ui.Debugln("Image generation prompt: %s", prompt)
}
