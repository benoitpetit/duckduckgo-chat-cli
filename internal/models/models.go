package models

import (
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"

	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
)

const (
	StatusURL        = "https://duck.ai/duckchat/v1/status"
	ChatURL          = "https://duck.ai/duckchat/v1/chat"
	StatusHeaders    = "1"
	MinChromeVersion = "115.0.5790.110"
)

type Model string
type ModelAlias string

const (
	GPT5Luna     Model = "gpt-5.6-luna"
	GPT54Mini    Model = "gpt-5.4-mini"
	ClaudeHaiku  Model = "claude-haiku-4-5"
	MistralSmall Model = "mistral-small-4"
	GPTOSS120B   Model = "gpt-oss-120b"
	Gemma431B    Model = "gemma-4-31b"

	GPT5LunaAlias     ModelAlias = "gpt-5.6-luna"
	GPT54MiniAlias    ModelAlias = "gpt-5.4-mini"
	ClaudeHaikuAlias  ModelAlias = "claude-haiku-4-5"
	MistralSmallAlias ModelAlias = "mistral-small-4"
	GPTOSS120BAlias   ModelAlias = "gpt-oss-120b"
	Gemma431BAlias    ModelAlias = "gemma-4-31b"
)

var modelMap = map[ModelAlias]Model{
	GPT5LunaAlias:     GPT5Luna,
	GPT54MiniAlias:    GPT54Mini,
	ClaudeHaikuAlias:  ClaudeHaiku,
	MistralSmallAlias: MistralSmall,
	GPTOSS120BAlias:   GPTOSS120B,
	Gemma431BAlias:    Gemma431B,
	"gpt-4o-mini":     GPT5Luna,
	"claude-3-haiku":  ClaudeHaiku,
	"llama":           GPTOSS120B,
	"mixtral":         MistralSmall,
	"o4mini":          GPT54Mini,
}

var modelDisplayMap = map[Model]string{
	GPT5Luna:     "GPT-5.6 Luna",
	GPT54Mini:    "GPT-5.4 Mini",
	ClaudeHaiku:  "Claude Haiku 4.5",
	MistralSmall: "Mistral Small 4",
	GPTOSS120B:   "GPT OSS 120B",
	Gemma431B:    "Gemma 4 31B",
}

func GetModel(alias string) Model {
	if model, ok := modelMap[ModelAlias(strings.ToLower(alias))]; ok {
		return model
	}
	return GPT5Luna // default model
}

// ResolveModel validates a model name without silently falling back to a
// different model. GetModel remains compatible with older callers that rely
// on the historical default behavior.
func ResolveModel(alias string) (Model, bool) {
	model, ok := modelMap[ModelAlias(strings.ToLower(strings.TrimSpace(alias)))]
	return model, ok
}

func CheckChromeVersion() {
	version, err := getChromeVersion()
	if err != nil {
		color.Yellow("Warning: %v", err)
		color.Yellow("Continuing without Chrome version check...")
		return // Continue instead of panic
	}

	result, err := compareVersions(version, MinChromeVersion)
	if err != nil {
		color.Yellow("Warning: Chrome version check failed: %v", err)
		color.Yellow("Continuing without version verification...")
		return // Continue instead of panic
	}

	if result < 0 {
		color.Yellow("Warning: Chrome %s+ recommended, found %s", MinChromeVersion, version)
		color.Yellow("The application might still work, but it's recommended to upgrade Chrome")
	}
}

func compareVersions(v1, v2 string) (int, error) {
	// Clean version strings
	v1 = strings.TrimSpace(v1)
	v1 = strings.TrimPrefix(v1, "Google Chrome ")
	v1 = strings.TrimPrefix(v1, "Chromium ")

	// Remove "snap" suffix if present
	if idx := strings.Index(v1, " snap"); idx != -1 {
		v1 = v1[:idx]
	}

	v1parts := strings.Split(v1, ".")
	v2parts := strings.Split(v2, ".")

	if len(v1parts) == 0 || len(v2parts) == 0 {
		return 0, fmt.Errorf("invalid version format")
	}

	// Compare major version numbers
	v1num, err := strconv.Atoi(strings.TrimSpace(v1parts[0]))
	if err != nil {
		return 0, fmt.Errorf("invalid version: %s", v1)
	}

	v2num, err := strconv.Atoi(strings.TrimSpace(v2parts[0]))
	if err != nil {
		return 0, fmt.Errorf("invalid version: %s", v2)
	}

	if v1num > v2num {
		return 1, nil
	} else if v1num < v2num {
		return -1, nil
	}
	return 0, nil
}

func getChromeVersion() (string, error) {
	switch runtime.GOOS {
	case "windows":
		paths := []string{
			"reg query \"HKEY_CURRENT_USER\\Software\\Google\\Chrome\\BLBeacon\" /v version",
			"reg query \"HKLM\\SOFTWARE\\Wow6432Node\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\Google Chrome\" /v Version",
		}

		for _, cmd := range paths {
			if output, err := exec.Command("cmd", "/C", cmd).Output(); err == nil {
				if version := extractWindowsVersion(string(output)); version != "" {
					return version, nil
				}
			}
		}
		return "", fmt.Errorf("chrome not found in Windows registry")

	case "linux":
		browsers := []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
		}

		for _, browser := range browsers {
			if output, err := exec.Command(browser, "--version").Output(); err == nil {
				return strings.TrimSpace(string(output)), nil
			}
		}

		// Vérifier les chemins communs
		paths := []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/snap/bin/chromium",
		}

		for _, path := range paths {
			if _, err := os.Stat(path); err == nil {
				if output, err := exec.Command(path, "--version").Output(); err == nil {
					return strings.TrimSpace(string(output)), nil
				}
			}
		}

		return "", fmt.Errorf("neither Chrome nor Chromium found on system")

	case "darwin":
		paths := []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}

		for _, path := range paths {
			if output, err := exec.Command(path, "--version").Output(); err == nil {
				return strings.TrimSpace(string(output)), nil
			}
		}
		return "", fmt.Errorf("chrome not found on macOS")

	default:
		return "", fmt.Errorf("unsupported operating system: %s", runtime.GOOS)
	}
}

func extractWindowsVersion(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "REG_SZ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				return fields[len(fields)-1]
			}
		}
	}
	return ""
}

func HandleModelChange(chat interface{}, modelArg string) ModelAlias {
	// If a model argument is provided, try to use it directly
	if modelArg != "" {
		for alias, model := range modelMap {
			if strings.EqualFold(modelArg, string(alias)) || strings.EqualFold(modelArg, string(model)) {
				return alias
			}
		}
		ui.Errorln("Invalid model choice: %s", modelArg)
		return ""
	}

	// Show an interactive menu if no argument is provided
	modelOptions := []string{
		"GPT-5.6 Luna",
		"GPT-5.4 Mini",
		"Claude Haiku 4.5",
		"Mistral Small 4",
		"GPT OSS 120B",
		"Gemma 4 31B",
		"Cancel",
	}

	currentModel := GetCurrentModel(chat)
	defaultModel, ok := modelDisplayMap[currentModel]
	if !ok {
		defaultModel = "GPT-5.6 Luna" // Fallback
	}

	var choice string
	prompt := &survey.Select{
		Message: "Choose a new model:",
		Options: modelOptions,
		Default: defaultModel,
	}
	err := survey.AskOne(prompt, &choice, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		// Gracefully exit on any error, including Ctrl+C (Interrupt)
		return ""
	}

	switch strings.ToLower(choice) {
	case "gpt-5.6 luna":
		return GPT5LunaAlias
	case "gpt-5.4 mini":
		return GPT54MiniAlias
	case "claude haiku 4.5":
		return ClaudeHaikuAlias
	case "mistral small 4":
		return MistralSmallAlias
	case "gpt oss 120b":
		return GPTOSS120BAlias
	case "gemma 4 31b":
		return Gemma431BAlias
	case "cancel":
		ui.Warningln("Model change canceled")
		return ""
	default:
		return ""
	}
}

func GetCurrentModel(chat interface{}) Model {
	v := reflect.ValueOf(chat)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if modelField := v.FieldByName("Model"); modelField.IsValid() {
		return Model(modelField.String())
	}
	return ""
}
