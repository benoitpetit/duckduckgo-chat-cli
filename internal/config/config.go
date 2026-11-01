package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/interfaces"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/security"
	"duckduckgo-chat-cli/internal/shortcut"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

type SearchConfig struct {
	MaxResults     int  `json:"max_results"`
	IncludeSnippet bool `json:"include_snippet"`
	MaxRetries     int  `json:"max_retries"`
	RetryDelay     int  `json:"retry_delay"`
}

type LibraryConfig struct {
	Directories []string `json:"directories"`
	Enabled     bool     `json:"enabled"`
}

type APIConfig struct {
	Enabled        bool     `json:"enabled"`
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	Autostart      bool     `json:"autostart"`
	LogRequests    bool     `json:"log_requests"`
	ShowGinLogs    bool     `json:"show_gin_logs"`
	APIKey         string   `json:"api_key,omitempty"`
	AllowedOrigins []string `json:"allowed_origins,omitempty"`
}

// ToolsConfig controls Duck.ai's built-in tools. These flags are opt-in
// because the tool protocol is not part of Duck.ai's public API and can
// change independently of the chat endpoint.
type ToolsConfig struct {
	Enabled         bool `json:"enabled"`
	WebSearch       bool `json:"web_search"`
	ImageGeneration bool `json:"image_generation"`
}

// AppearanceConfig contains terminal theme and frame presentation settings.
type AppearanceConfig struct {
	Theme           string             `json:"theme"`
	FrameResponses  bool               `json:"frame_responses"`
	FrameWidthMode  ui.FrameWidthMode  `json:"frame_width_mode"`
	FrameMaxWidth   int                `json:"frame_max_width"`
	FrameLineWeight ui.FrameLineWeight `json:"frame_line_weight"`
}

// SpeakVoice describes a selectable Duck.ai voice.
type SpeakVoice struct {
	ID          string
	Name        string
	Description string
}

var speakVoices = []SpeakVoice{
	{ID: "voice-1", Name: "Voice 1", Description: "Open and upbeat"},
	{ID: "voice-2", Name: "Voice 2", Description: "Animated and earnest"},
	{ID: "voice-3", Name: "Voice 3", Description: "Confident and optimistic"},
	{ID: "voice-4", Name: "Voice 4", Description: "Cheerful and candid"},
}

// SpeakVoices returns the selectable Duck.ai voices in display order.
func SpeakVoices() []SpeakVoice {
	return append([]SpeakVoice(nil), speakVoices...)
}

// DefaultSpeakVoice returns the voice Duck.ai uses when none is selected.
func DefaultSpeakVoice() string {
	return speakVoices[0].ID
}

// SpeakVoiceByID returns the voice with the given identifier.
func SpeakVoiceByID(id string) (SpeakVoice, bool) {
	for _, voice := range speakVoices {
		if voice.ID == id {
			return voice, true
		}
	}
	return SpeakVoice{}, false
}

// SpeakConfig controls the Chromium window and playback preferences used for
// live Duck.ai voice sessions.
type SpeakConfig struct {
	Voice          string `json:"voice"`
	WindowWidth    int    `json:"window_width"`
	WindowHeight   int    `json:"window_height"`
	StartMuted     bool   `json:"start_muted"`
	ShowTranscript bool   `json:"show_transcript"`
	Volume         int    `json:"volume"`
	ReadinessSound bool   `json:"readiness_sound"`
	AutoStart      bool   `json:"auto_start"`
	SchemaVersion  int    `json:"schema_version,omitempty"`
}

// TrayConfig stores global shortcuts used by the resident tray service.
type TrayConfig struct {
	TextShortcut  string `json:"text_shortcut"`
	VoiceShortcut string `json:"voice_shortcut"`
}

func defaultTrayConfig() TrayConfig {
	return TrayConfig{TextShortcut: "Ctrl+Alt+D", VoiceShortcut: "Ctrl+Alt+V"}
}

func normalizeTrayConfig(cfg *TrayConfig) {
	defaults := defaultTrayConfig()
	if _, err := shortcut.ParseShortcut(cfg.TextShortcut); err != nil {
		cfg.TextShortcut = defaults.TextShortcut
	}
	if _, err := shortcut.ParseShortcut(cfg.VoiceShortcut); err != nil {
		cfg.VoiceShortcut = defaults.VoiceShortcut
	}
	if err := shortcut.ValidateShortcuts(cfg.TextShortcut, cfg.VoiceShortcut); err != nil {
		*cfg = defaults
	}
}

func defaultSpeakConfig() SpeakConfig {
	return SpeakConfig{
		Voice:          DefaultSpeakVoice(),
		WindowWidth:    480,
		WindowHeight:   500,
		StartMuted:     false,
		ShowTranscript: true,
		Volume:         100,
		ReadinessSound: true,
		AutoStart:      true,
		SchemaVersion:  3,
	}
}

// DefaultSpeakConfig returns the default voice, window, and playback settings.
func DefaultSpeakConfig() SpeakConfig {
	return defaultSpeakConfig()
}

func normalizeSpeakConfig(cfg *SpeakConfig) {
	defaults := defaultSpeakConfig()
	if cfg.SchemaVersion < defaults.SchemaVersion {
		switch {
		case cfg.SchemaVersion == 0 && cfg.WindowWidth == 500 && cfg.WindowHeight == 500:
			// The original default was 500 × 500.
			cfg.WindowWidth = defaults.WindowWidth
			cfg.WindowHeight = defaults.WindowHeight
		case cfg.SchemaVersion == 1 && cfg.WindowWidth == 400 && cfg.WindowHeight == 460:
			// Migrate the interim compact default while preserving custom sizes.
			cfg.WindowWidth = defaults.WindowWidth
			cfg.WindowHeight = defaults.WindowHeight
		case cfg.SchemaVersion == 2 && cfg.WindowWidth == 400 && cfg.WindowHeight == 400:
			// Migrate the compact default so the companion and subtitles fit.
			cfg.WindowWidth = defaults.WindowWidth
			cfg.WindowHeight = defaults.WindowHeight
		}
		cfg.SchemaVersion = defaults.SchemaVersion
	}
	if _, ok := SpeakVoiceByID(cfg.Voice); !ok {
		cfg.Voice = defaults.Voice
	}
	if cfg.WindowWidth < 320 || cfg.WindowWidth > 1400 {
		cfg.WindowWidth = defaults.WindowWidth
	}
	if cfg.WindowHeight < 360 || cfg.WindowHeight > 1400 {
		cfg.WindowHeight = defaults.WindowHeight
	}
	if cfg.Volume < 0 || cfg.Volume > 100 {
		cfg.Volume = defaults.Volume
	}
}

// RateLimitConfig keeps legacy browser fallback settings readable. Automatic
// browser opening is no longer supported; existing open_browser values are
// ignored by chat sessions.
type RateLimitConfig struct {
	OpenBrowser     bool `json:"open_browser"`
	CooldownMinutes int  `json:"cooldown_minutes"`
}

func defaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{OpenBrowser: false, CooldownMinutes: 10}
}

func normalizeRateLimitConfig(cfg *RateLimitConfig) {
	if cfg.CooldownMinutes < 1 {
		cfg.CooldownMinutes = defaultRateLimitConfig().CooldownMinutes
	}
}

// DashboardConfig contains settings for the loopback-only usage dashboard.
type DashboardConfig struct {
	Autostart                 bool   `json:"autostart"`
	Port                      int    `json:"port"`
	RefreshIntervalSeconds    int    `json:"refresh_interval_seconds"`
	RetentionDays             int    `json:"retention_days"`
	ShowConversations         bool   `json:"show_conversations"`
	ShowConversationContent   bool   `json:"show_conversation_content"`
	AllowConversationAnalysis bool   `json:"allow_conversation_analysis"`
	AnalysisTokenBudget       int    `json:"analysis_token_budget"`
	PasswordSalt              string `json:"password_salt,omitempty"`
	PasswordHash              string `json:"password_hash,omitempty"`
}

// CoordinateAgentConfig stores one model and its role in the reusable
// multi-model coordination preset.
type CoordinateAgentConfig struct {
	Model string `json:"model"`
	Role  string `json:"role"`
}

// CoordinatePresetConfig stores the agents and execution options for one
// coordinate strategy.
type CoordinatePresetConfig struct {
	Agents       []CoordinateAgentConfig `json:"agents,omitempty"`
	DebateRounds int                     `json:"debate_rounds,omitempty"`
	Parallel     bool                    `json:"parallel,omitempty"`
}

// CoordinateConfig stores reusable, strategy-specific presets. The direct
// fields retain compatibility with configs written before profiles existed.
type CoordinateConfig struct {
	Strategy     string                            `json:"strategy,omitempty"`
	Agents       []CoordinateAgentConfig           `json:"agents,omitempty"`
	DebateRounds int                               `json:"debate_rounds,omitempty"`
	Parallel     bool                              `json:"parallel,omitempty"`
	Profiles     map[string]CoordinatePresetConfig `json:"profiles,omitempty"`
}

func defaultDashboardConfig() DashboardConfig {
	return DashboardConfig{Port: 8765, RefreshIntervalSeconds: 3, RetentionDays: 90, AnalysisTokenBudget: 8000}
}

func normalizeDashboardConfig(cfg *DashboardConfig) {
	defaults := defaultDashboardConfig()
	if cfg.Port < 1 || cfg.Port > 65535 {
		cfg.Port = defaults.Port
	}
	if cfg.RefreshIntervalSeconds < 1 || cfg.RefreshIntervalSeconds > 60 {
		cfg.RefreshIntervalSeconds = defaults.RefreshIntervalSeconds
	}
	if cfg.RetentionDays < 1 || cfg.RetentionDays > 3650 {
		cfg.RetentionDays = defaults.RetentionDays
	}
	if cfg.AnalysisTokenBudget < 1000 || cfg.AnalysisTokenBudget > 32000 {
		cfg.AnalysisTokenBudget = defaults.AnalysisTokenBudget
	}
}

type Config struct {
	TOSAccepted      bool              `json:"tos_accepted"`
	DefaultModel     string            `json:"default_model"`
	ExportDir        string            `json:"export_dir"`
	LastUpdateTime   time.Time         `json:"last_update_time"`
	Appearance       AppearanceConfig  `json:"appearance"`
	Search           SearchConfig      `json:"search"`
	Library          LibraryConfig     `json:"library"`
	API              APIConfig         `json:"api"`
	Tools            ToolsConfig       `json:"tools"`
	Speak            SpeakConfig       `json:"speak"`
	Tray             TrayConfig        `json:"tray"`
	RateLimit        RateLimitConfig   `json:"rate_limit"`
	Dashboard        DashboardConfig   `json:"dashboard"`
	Coordinate       CoordinateConfig  `json:"coordinate,omitempty"`
	ShowMenu         bool              `json:"show_menu"`
	GlobalPrompt     string            `json:"global_prompt"`
	ConfirmLongInput bool              `json:"confirm_long_input"`
	Prompts          map[string]string `json:"prompts"`
}

func Initialize() *Config {
	cfg := loadConfig()
	normalizeAppearanceConfig(&cfg.Appearance)
	ui.SetTheme(cfg.Appearance.Theme)
	ui.SetFrameOptions(frameOptionsFromAppearance(cfg.Appearance))
	normalizeSpeakConfig(&cfg.Speak)
	normalizeTrayConfig(&cfg.Tray)
	normalizeRateLimitConfig(&cfg.RateLimit)
	normalizeDashboardConfig(&cfg.Dashboard)
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = string(models.Default())
	} else if _, ok := models.ResolveModel(cfg.DefaultModel); !ok {
		ui.Warningln("Warning: Unknown configured model %q; using %s", cfg.DefaultModel, models.Default())
		cfg.DefaultModel = string(models.Default())
	}
	if cfg.Search.MaxResults < 1 || cfg.Search.MaxResults > 50 {
		cfg.Search.MaxResults = 10
	}
	if cfg.Search.MaxRetries < 1 || cfg.Search.MaxRetries > 10 {
		cfg.Search.MaxRetries = 3
	}
	if cfg.Search.RetryDelay < 1 || cfg.Search.RetryDelay > 30 {
		cfg.Search.RetryDelay = 1
	}
	// Defaults are initialized before unmarshalling, so an explicit false in
	// an existing configuration remains false.

	// Initialize library config with defaults
	if len(cfg.Library.Directories) == 0 {
		cfg.Library.Directories = []string{}
	}

	// Initialize prompts map if nil
	if cfg.Prompts == nil {
		cfg.Prompts = make(map[string]string)
	}

	// Initialize API config with defaults - check if config file exists first
	configExists := configFileExists()
	if cfg.API.Port == 0 {
		cfg.API.Port = 8080 // default port
	}
	if cfg.API.Host == "" {
		cfg.API.Host = "127.0.0.1"
	}

	// Only set defaults if no config file exists (first run) or if explicitly not set
	if !configExists {
		cfg.API.LogRequests = true // default to true for new installs
		cfg.API.ShowGinLogs = true // default to true for new installs
	}

	if err := ensureExportDir(cfg); err != nil {
		ui.Warningln("Warning: Failed to create export directory: %v", err)
	}
	return cfg
}

func normalizeAppearanceConfig(cfg *AppearanceConfig) {
	theme, ok := ui.ResolveTheme(cfg.Theme)
	if !ok {
		cfg.Theme = string(ui.ThemeOfficial)
	} else {
		cfg.Theme = string(theme.ID)
	}
	options := ui.NormalizeFrameOptions(frameOptionsFromAppearance(*cfg))
	cfg.FrameWidthMode = options.WidthMode
	cfg.FrameMaxWidth = options.MaxWidth
	cfg.FrameLineWeight = options.LineWeight
}

func defaultConfig() *Config {
	return &Config{
		TOSAccepted:      false,
		DefaultModel:     string(models.Default()),
		ExportDir:        defaultExportPath(),
		LastUpdateTime:   time.Now(),
		Appearance:       defaultAppearanceConfig(),
		ConfirmLongInput: true, // default to enabled for safety
		Search:           SearchConfig{MaxResults: 10, IncludeSnippet: true, MaxRetries: 3, RetryDelay: 1},
		Library:          LibraryConfig{Directories: []string{}, Enabled: true},
		API:              APIConfig{Host: "127.0.0.1", Port: 8080},
		Speak:            defaultSpeakConfig(),
		Tray:             defaultTrayConfig(),
		RateLimit:        defaultRateLimitConfig(),
		Dashboard:        defaultDashboardConfig(),
		Prompts:          make(map[string]string),
	}
}

func defaultAppearanceConfig() AppearanceConfig {
	options := ui.DefaultFrameOptions()
	return AppearanceConfig{
		Theme:           string(ui.ThemeOfficial),
		FrameResponses:  true,
		FrameWidthMode:  options.WidthMode,
		FrameMaxWidth:   options.MaxWidth,
		FrameLineWeight: options.LineWeight,
	}
}

func frameOptionsFromAppearance(appearance AppearanceConfig) ui.FrameOptions {
	return ui.NormalizeFrameOptions(ui.FrameOptions{
		WidthMode:  appearance.FrameWidthMode,
		MaxWidth:   appearance.FrameMaxWidth,
		LineWeight: appearance.FrameLineWeight,
	})
}

func resetConfigurationToDefaults(cfg *Config, chatSession interfaces.ChatSession) error {
	if cfg == nil {
		return fmt.Errorf("configuration is unavailable")
	}
	defaults := defaultConfig()
	// A brand-new install enables these diagnostic logs. Keep explicit false
	// values in existing configuration files unchanged during normal loading.
	defaults.API.LogRequests = true
	defaults.API.ShowGinLogs = true
	if err := SaveConfig(defaults); err != nil {
		return fmt.Errorf("could not save default configuration: %w", err)
	}

	*cfg = *defaults
	ui.SetTheme(cfg.Appearance.Theme)
	ui.SetFrameOptions(frameOptionsFromAppearance(cfg.Appearance))
	if chatSession != nil {
		chatSession.ChangeModel(models.GetModel(cfg.DefaultModel))
		chatSession.SetNativeTools(cfg.Tools.Enabled, cfg.Tools.WebSearch, cfg.Tools.ImageGeneration)
	}
	ui.AIln("All DuckChat settings have been reset to their defaults.")
	return nil
}

const (
	configResetConfirmationPhrase = "RESET"
	configResetConfirmationHelp   = "This clears the local API key, dashboard password, saved prompts, coordinate profiles, and terms acceptance. API and dashboard services stop. Session and dashboard-history retention returns to 90 days, so older records may be removed. Exported files are not deleted."
)

type configResetPrompter interface {
	ConfirmReset(message, help string, defaultAnswer bool) (bool, error)
	EnterResetPhrase(expected string) (string, error)
}

type terminalConfigResetPrompter struct{}

func (terminalConfigResetPrompter) ConfirmReset(message, help string, defaultAnswer bool) (bool, error) {
	var confirmed bool
	prompt := &survey.Confirm{Message: message, Help: help, Default: defaultAnswer}
	if err := survey.AskOne(prompt, &confirmed); err != nil {
		return false, err
	}
	return confirmed, nil
}

func (terminalConfigResetPrompter) EnterResetPhrase(expected string) (string, error) {
	var answer string
	prompt := &survey.Input{Message: fmt.Sprintf("Type %q exactly to continue:", expected)}
	validate := func(value interface{}) error {
		phrase, ok := value.(string)
		if !ok || phrase != expected {
			return fmt.Errorf("type %q exactly to confirm the reset", expected)
		}
		return nil
	}
	if err := survey.AskOne(prompt, &answer, survey.WithValidator(validate)); err != nil {
		return "", err
	}
	return answer, nil
}

func resetConfigurationWithConfirmation(cfg *Config, chatSession interfaces.ChatSession, prompts configResetPrompter) bool {
	if prompts == nil {
		ui.Errorln("Configuration reset prompt is unavailable.")
		return false
	}
	confirmed, err := prompts.ConfirmReset("Reset all DuckChat settings?", configResetConfirmationHelp, false)
	if err != nil {
		ui.Warningln("Configuration reset canceled; no settings were changed.")
		return false
	}
	if !confirmed {
		ui.AIln("Configuration reset canceled; no settings were changed.")
		return false
	}
	phrase, err := prompts.EnterResetPhrase(configResetConfirmationPhrase)
	if err != nil {
		ui.Warningln("Configuration reset canceled; no settings were changed.")
		return false
	}
	if phrase != configResetConfirmationPhrase {
		ui.Warningln("Reset confirmation did not match; no settings were changed.")
		return false
	}
	if err := resetConfigurationToDefaults(cfg, chatSession); err != nil {
		ui.Errorln("Configuration reset failed: %v", err)
		return false
	}
	return true
}

func loadConfig() *Config {
	cfg := defaultConfig()

	if data, err := os.ReadFile(configPath()); err == nil {
		if err := json.Unmarshal(data, cfg); err != nil {
			ui.SetTheme(string(ui.ThemeOfficial))
			ui.Warningln("Warning: Failed to parse config file: %v", err)
		}
	}
	if cfg.Prompts == nil {
		cfg.Prompts = make(map[string]string)
	}
	return cfg
}

// ReadOnlyDiagnosticsConfig loads existing settings for diagnostics without
// creating directories, normalizing the user's choices, or saving defaults.
func ReadOnlyDiagnosticsConfig() (Config, string, error) {
	path := configPath()
	cfg := defaultConfig()
	// #nosec G304 -- configPath is fixed under the current user's config directory.
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return *cfg, path, nil
	}
	if err != nil {
		return *cfg, path, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return *cfg, path, fmt.Errorf("failed to parse config file: %w", err)
	}
	if cfg.Prompts == nil {
		cfg.Prompts = make(map[string]string)
	}
	return *cfg, path, nil
}

func defaultExportPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("USERPROFILE"), "Documents", "duckchat")
	}
	return filepath.Join(os.Getenv("HOME"), "Documents", "duckchat")
}

func configPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		// Fallback to home directory if config dir is not available
		if runtime.GOOS == "windows" {
			configDir = filepath.Join(os.Getenv("USERPROFILE"), ".config")
		} else {
			configDir = filepath.Join(os.Getenv("HOME"), ".config")
		}
	}
	return filepath.Join(configDir, "duckduckgo-chat-cli", "config.json")
}

// DashboardHistoryPath returns the local analytics history file, separate from exports.
func DashboardHistoryPath() string {
	return filepath.Join(filepath.Dir(configPath()), "dashboard-history.json")
}

// configFileExists checks if the configuration file exists
func configFileExists() bool {
	_, err := os.Stat(configPath())
	return err == nil
}

func ensureExportDir(cfg *Config) error {
	return os.MkdirAll(cfg.ExportDir, 0o700)
}

// SaveConfig saves the configuration to file (exported version of saveConfig)
func SaveConfig(cfg *Config) error {
	configDir := filepath.Dir(configPath())
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	tmp, err := os.CreateTemp(configDir, ".config.json-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary config: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		return fmt.Errorf("failed to secure temporary config: %w", errors.Join(err, tmp.Close()))
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("failed to write config: %w", errors.Join(err, tmp.Close()))
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("failed to sync config: %w", errors.Join(err, tmp.Close()))
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close config: %v", err)
	}
	if err := os.Rename(tmpName, configPath()); err != nil {
		return fmt.Errorf("failed to replace config: %v", err)
	}

	return nil
}

// Private version for internal use
func saveConfig(cfg *Config) error {
	return SaveConfig(cfg)
}

func AcceptTermsOfService(cfg *Config, askOptions ...survey.AskOpt) bool {
	if cfg.TOSAccepted {
		return true
	}

	var accepted bool
	prompt := &survey.Confirm{
		Message: "Please accept the terms of service to continue. Do you accept?",
		Default: true,
	}
	if err := survey.AskOne(prompt, &accepted, askOptions...); err != nil {
		ui.Warningln("Terms of service prompt canceled.")
		return false
	}

	if accepted {
		cfg.TOSAccepted = true
		if err := saveConfig(cfg); err != nil {
			ui.Warningln("Warning: Failed to save config: %v", err)
		}
	}
	return accepted
}

func HandleConfiguration(cfg *Config, chatSession interfaces.ChatSession) bool {
	resetApplied := false
	for {
		frameOptions := frameOptionsFromAppearance(cfg.Appearance)
		frameStatus := "Off"
		if cfg.Appearance.FrameResponses {
			frameStatus = "On"
		}
		themeLabel := ui.CurrentTheme().Label
		summary := []string{
			fmt.Sprintf("Default model: %s", cfg.DefaultModel),
			fmt.Sprintf("Theme: %s", themeLabel),
			fmt.Sprintf("Response frame: %s", frameStatus),
			fmt.Sprintf("Frame width: %s", frameWidthSummaryLabel(frameOptions)),
			fmt.Sprintf("Frame lines: %s", frameLineWeightLabel(frameOptions.LineWeight)),
			fmt.Sprintf("Text shortcut: %s", cfg.Tray.TextShortcut),
			fmt.Sprintf("Voice shortcut: %s", cfg.Tray.VoiceShortcut),
			"",
			"Settings are grouped by area. Changes are saved as you go.",
		}

		choice := ""
		prompt := &survey.Select{
			Message: "Choose a setting",
			Help:    "",
			Options: []string{
				"Appearance · Theme",
				"Appearance · Response frame",
				"Appearance · Frame width",
				"Appearance · Frame line weight",
				"Chat · Default model",
				"Chat · Shortcuts",
				"Chat · Global prompt",
				"Chat · Long input protection",
				"Chat · Show commands menu",
				"Search · Search settings",
				"Search · Library settings",
				"Tools · Duck.ai native tools",
				"Prompts · Prompt management",
				"Services · API settings",
				"Services · Dashboard settings",
				"Services · Audio agent speak",
				"Files · Export directory",
				"Reset all settings to defaults",
				"Back to chat",
			},
			Default: "Back to chat",
		}
		if err := askFramedConfigChoice(prompt, &choice, summary); err != nil {
			ui.Warningln("Configuration menu canceled.")
			return resetApplied
		}

		switch choice {
		case "Appearance · Theme":
			handleThemeSettings(cfg)
		case "Appearance · Response frame":
			handleResponseFrameSettings(cfg)
		case "Appearance · Frame width":
			handleFrameWidthSettings(cfg)
		case "Appearance · Frame line weight":
			handleFrameLineWeightSettings(cfg)
		case "Chat · Shortcuts":
			HandleShortcutConfiguration(cfg)
		case "Chat · Default model":
			handleModelChange(cfg, chatSession)
		case "Files · Export directory":
			handleExportDirChange(cfg)
		case "Search · Search settings":
			handleSearchSettings(cfg)
		case "Chat · Show commands menu":
			handleShowMenuChange(cfg)
		case "Chat · Global prompt":
			handleGlobalPromptChange(cfg)
		case "Chat · Long input protection":
			handleLongInputProtectionChange(cfg)
		case "Search · Library settings":
			handleLibrarySettings(cfg)
		case "Services · API settings":
			handleAPISettings(cfg)
		case "Services · Dashboard settings":
			handleDashboardSettings(cfg)
		case "Tools · Duck.ai native tools":
			handleNativeToolsChange(cfg, chatSession)
		case "Services · Audio agent speak":
			handleSpeakSettings(cfg)
		case "Prompts · Prompt management":
			HandlePromptManagement(cfg)
		case "Reset all settings to defaults":
			if resetConfigurationWithConfirmation(cfg, chatSession, terminalConfigResetPrompter{}) {
				resetApplied = true
			}
		case "Back to chat", "":
			return resetApplied
		default:
			ui.Errorln("Invalid choice. Please try again.")
		}
	}
}

// askFramedConfigChoice temporarily applies a framed survey template to the
// configuration selector. Survey exposes its templates as package globals, so
// restore both immediately after this prompt to leave other prompts untouched.
func askFramedConfigChoice(prompt *survey.Select, choice *string, summary []string) error {
	previousSelectTemplate := survey.SelectQuestionTemplate
	previousMultiSelectTemplate := survey.MultiSelectQuestionTemplate
	previousHelp := prompt.Help
	menuTitle := "Configuration"
	if prompt.Message != "" && prompt.Message != "Choose a setting" {
		label := strings.TrimSuffix(strings.TrimPrefix(prompt.Message, "Choose "), ":")
		menuTitle += " · " + label
	}
	selectTemplate, offsetTemplate, width := configurationSelectTemplates(menuTitle)
	if width < 24 {
		return survey.AskOne(prompt, choice)
	}
	restorePrompt := ui.FitFramedSelectPrompt(prompt, width)
	defer restorePrompt(choice)
	frameSummary := append([]string(nil), summary...)
	if previousHelp != "" {
		if len(frameSummary) > 0 {
			frameSummary = append(frameSummary, "")
		}
		frameSummary = append(frameSummary, previousHelp)
	}
	prompt.Help = configurationFrameLines(frameSummary, width)
	survey.SelectQuestionTemplate = selectTemplate
	survey.MultiSelectQuestionTemplate = offsetTemplate
	defer func() {
		survey.SelectQuestionTemplate = previousSelectTemplate
		survey.MultiSelectQuestionTemplate = previousMultiSelectTemplate
		prompt.Help = previousHelp
	}()

	if err := survey.AskOne(prompt, choice); err != nil {
		restorePrompt(choice)
		return err
	}
	restorePrompt(choice)
	clearConfigurationAnswer(prompt, choice, width)
	return nil
}

// Survey renders a final answer panel after a selection. Remove that panel so
// the configuration frame is visible only while the selector is open.
func clearConfigurationAnswer(prompt *survey.Select, choice *string, width int) {
	if !term.IsTerminal(int(os.Stdout.Fd())) || choice == nil {
		return
	}

	helpRows := 0
	if prompt.Help != "" {
		helpRows = len(strings.Split(strings.TrimSuffix(prompt.Help, "\n"), "\n"))
	}
	rows := helpRows + 3 // one answer row, plus the top and bottom borders

	cursor := &terminal.Cursor{Out: os.Stdout}
	if err := cursor.Up(1); err != nil {
		return
	}
	for row := 0; row < rows; row++ {
		if err := cursor.HorizontalAbsolute(0); err != nil {
			return
		}
		if err := terminal.EraseLine(os.Stdout, terminal.ERASE_LINE_ALL); err != nil {
			return
		}
		if row+1 < rows {
			if err := cursor.Up(1); err != nil {
				return
			}
		}
	}
}

func configurationSelectTemplates(title string) (string, string, int) {
	terminalWidth := 80
	cursorWidth := 10000
	if term.IsTerminal(int(os.Stdout.Fd())) {
		if columns, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && columns > 0 {
			terminalWidth = columns
			cursorWidth = columns
		}
	}
	return configurationSelectTemplatesAtWidth(title, terminalWidth, cursorWidth)
}

func configurationSelectTemplatesAtWidth(title string, terminalWidth, cursorWidth int) (string, string, int) {
	width := ui.ResolveFrameWidth(terminalWidth)
	if width < 24 {
		return "", "", width
	}
	contentWidth := width - 4
	filterRow := ui.FitFrameCellText("↑/↓ move · Enter select · type to filter · ? for help", contentWidth)
	helpRow := ui.FitFrameCellText("Use ↑/↓ to move, Enter to edit, or type to filter.", contentWidth)
	if ansi.StringWidth(title) > width-6 {
		title = ansi.Truncate(title, max(1, width-6), "…")
	}
	glyphs := ui.ResolveFrameGlyphs(os.Getenv("TERM") == "dumb")
	topTitle := glyphs.Horizontal + " " + title + " "
	top := ui.MutedColor.Sprint(glyphs.TopLeft+glyphs.Horizontal+" ") + ui.AccentColor.Sprint(title) + ui.MutedColor.Sprint(" "+strings.Repeat(glyphs.Horizontal, max(0, width-ansi.StringWidth(topTitle)-2))+glyphs.TopRight)
	bottom := ui.MutedColor.Sprint(glyphs.BottomLeft + strings.Repeat(glyphs.Horizontal, width-2) + glyphs.BottomRight)
	verticalGlyph := glyphs.Vertical
	vertical := ui.MutedColor.Sprint(verticalGlyph)
	focus := ui.AccentColor.Sprint("›")

	selectTemplate := `
{{- define "option"}}
{{- printf "%s " "` + vertical + `"}}
{{- if eq .SelectedIndex .CurrentIndex}}{{printf "%s " "` + focus + `"}}{{else}}  {{end}}
{{- printf "%s" ($.GetDescription .CurrentOpt)}}
{{- printf " %s\n" "` + vertical + `"}}
{{- end}}
{{- printf "%s\n" "` + top + `"}}
{{- printf "%s" .Help}}
{{- if .ShowAnswer}}
{{- printf "%s %s %s\n" "` + vertical + `" (call .Description (printf "__duckchat_answer_row__%s" .Answer) 0) "` + vertical + `"}}
{{- printf "%s\n" "` + bottom + `"}}
{{- else}}
{{- printf "%s %s %s\n" "` + vertical + `" (call .Description "__duckchat_filter_row__" 0) "` + vertical + `"}}
{{- printf "%s %s %s\n" "` + vertical + `" "` + filterRow + `" "` + vertical + `"}}
{{- if .ShowHelp}}{{- printf "%s %s %s\n" "` + vertical + `" "` + helpRow + `" "` + vertical + `"}}{{end}}
{{- range $ix, $option := .PageEntries}}{{template "option" $.IterateOption $ix $option}}{{end}}
{{- printf "%s\n" "` + bottom + `"}}
{{- end}}`

	// RenderWithCursorOffset in survey v2.3.7 calculates the focus row with
	// MultiSelectQuestionTemplate even for Select prompts. Keep its option row
	// width in step with the framed row above. The selected offset row includes
	// one synthetic wrap to account for the footer line.
	offsetPadding := strings.Repeat(" ", cursorWidth+1)
	offsetTemplate := `{{- define "option"}}{{if eq .SelectedIndex .CurrentIndex}}{{printf "%s" "` + offsetPadding + `"}}{{else}}` + verticalGlyph + `   {{printf "%s" ($.GetDescription .CurrentOpt)}} ` + verticalGlyph + `{{end}}{{end}}`
	return selectTemplate, offsetTemplate, width
}

func configurationFrameLines(lines []string, width int) string {
	var framed strings.Builder
	vertical := ui.MutedColor.Sprint(ui.ResolveFrameGlyphs(os.Getenv("TERM") == "dumb").Vertical)
	for _, part := range configurationWrappedLines(lines, width) {
		contentWidth := max(1, width-4)
		content := styleConfigurationSummary(part)
		padding := max(0, contentWidth-ansi.StringWidth(content))
		fmt.Fprintf(&framed, "%s %s%s %s\n", vertical, content, strings.Repeat(" ", padding), vertical)
	}
	return framed.String()
}

func styleConfigurationSummary(line string) string {
	label, value, found := strings.Cut(line, ": ")
	if !found {
		return ui.WhiteColor.Sprint(line)
	}
	return ui.AccentColor.Sprint(label+": ") + ui.WhiteColor.Sprint(value)
}

func configurationWrappedLines(lines []string, width int) []string {
	var wrappedLines []string
	for _, line := range lines {
		wrapped := []string{line}
		if line != "" && width > 4 {
			wrapped = strings.Split(ansi.Hardwrap(line, width-4, true), "\n")
		}
		wrappedLines = append(wrappedLines, wrapped...)
	}
	return wrappedLines
}

func handleResponseFrameSettings(cfg *Config) {
	previous := cfg.Appearance.FrameResponses
	enabled := previous
	prompt := &survey.Confirm{
		Message: "Frame model responses?",
		Default: previous,
		Help:    "Frames responses in interactive terminals; narrow terminals use a compact rail, very narrow terminals stay unframed.",
	}
	if err := survey.AskOne(prompt, &enabled); err != nil {
		ui.Warningln("Response frame setting canceled.")
		return
	}
	cfg.Appearance.FrameResponses = enabled
	if err := saveConfig(cfg); err != nil {
		cfg.Appearance.FrameResponses = previous
		ui.Errorln("Could not save response frame setting: %v", err)
		return
	}
	status := "disabled"
	if enabled {
		status = "enabled"
	}
	ui.AIln("Response frame %s.", status)
}

type frameWidthChoice struct {
	label   string
	options ui.FrameOptions
}

type frameLineWeightChoice struct {
	label  string
	weight ui.FrameLineWeight
}

func frameWidthOptions(current ui.FrameOptions) []frameWidthChoice {
	current = ui.NormalizeFrameOptions(current)
	full := current
	full.WidthMode = ui.FrameWidthFull
	choices := []frameWidthChoice{{label: "Full terminal", options: full}}
	for _, width := range []int{80, 96, 112, 128, 144, 160} {
		maximum := current
		maximum.WidthMode = ui.FrameWidthMaximum
		maximum.MaxWidth = width
		choices = append(choices, frameWidthChoice{label: fmt.Sprintf("Maximum: %d", width), options: maximum})
	}
	return choices
}

func frameLineWeightOptions() []frameLineWeightChoice {
	return []frameLineWeightChoice{
		{label: "Single", weight: ui.FrameLineSingle},
		{label: "Heavy", weight: ui.FrameLineHeavy},
	}
}

func frameWidthSummaryLabel(options ui.FrameOptions) string {
	options = ui.NormalizeFrameOptions(options)
	if options.WidthMode == ui.FrameWidthFull {
		return "Full terminal"
	}
	return fmt.Sprintf("Maximum: %d columns", options.MaxWidth)
}

func frameLineWeightLabel(weight ui.FrameLineWeight) string {
	if weight == ui.FrameLineHeavy {
		return "Heavy"
	}
	return "Single"
}

func saveFrameOptions(cfg *Config, options ui.FrameOptions) error {
	if cfg == nil {
		return fmt.Errorf("configuration is unavailable")
	}
	previous := cfg.Appearance
	options = ui.NormalizeFrameOptions(options)
	cfg.Appearance.FrameWidthMode = options.WidthMode
	cfg.Appearance.FrameMaxWidth = options.MaxWidth
	cfg.Appearance.FrameLineWeight = options.LineWeight
	if err := saveConfig(cfg); err != nil {
		cfg.Appearance = previous
		ui.SetFrameOptions(frameOptionsFromAppearance(previous))
		return err
	}
	ui.SetFrameOptions(options)
	return nil
}

func handleFrameWidthSettings(cfg *Config) {
	if cfg == nil {
		return
	}
	current := frameOptionsFromAppearance(cfg.Appearance)
	choices := frameWidthOptions(current)
	labels := make([]string, len(choices))
	defaultChoice := frameWidthSummaryLabel(current)
	for index, choice := range choices {
		labels[index] = choice.label
		if choice.options.WidthMode == ui.FrameWidthFull && current.WidthMode == ui.FrameWidthFull {
			defaultChoice = choice.label
		} else if choice.options.WidthMode == ui.FrameWidthMaximum && current.WidthMode == ui.FrameWidthMaximum && choice.options.MaxWidth == current.MaxWidth {
			defaultChoice = choice.label
		}
	}
	choice := ""
	prompt := &survey.Select{
		Message: "Choose frame width:",
		Help:    "Full terminal uses all available columns. Maximum width caps the frame and adapts to narrower terminals.",
		Options: labels,
		Default: defaultChoice,
	}
	if err := askFramedConfigChoice(prompt, &choice, nil); err != nil {
		ui.Warningln("Frame width setting canceled.")
		return
	}
	for _, option := range choices {
		if option.label != choice {
			continue
		}
		if option.options == current {
			ui.AIln("Frame width is already set to %s.", frameWidthSummaryLabel(current))
			return
		}
		if err := saveFrameOptions(cfg, option.options); err != nil {
			ui.Errorln("Could not save frame width setting: %v", err)
			return
		}
		ui.AIln("Frame width changed to %s.", frameWidthSummaryLabel(option.options))
		return
	}
	ui.Errorln("Invalid frame width selection. No changes made.")
}

func handleFrameLineWeightSettings(cfg *Config) {
	if cfg == nil {
		return
	}
	current := frameOptionsFromAppearance(cfg.Appearance)
	choices := frameLineWeightOptions()
	labels := make([]string, len(choices))
	defaultChoice := frameLineWeightLabel(current.LineWeight)
	for index, option := range choices {
		labels[index] = option.label
	}
	choice := ""
	prompt := &survey.Select{
		Message: "Choose frame line weight:",
		Help:    "Single uses light borders; Heavy uses thicker borders. Dumb terminals continue to use ASCII borders.",
		Options: labels,
		Default: defaultChoice,
	}
	if err := askFramedConfigChoice(prompt, &choice, nil); err != nil {
		ui.Warningln("Frame line weight setting canceled.")
		return
	}
	for _, option := range choices {
		if option.label != choice {
			continue
		}
		if option.weight == current.LineWeight {
			ui.AIln("Frame line weight is already set to %s.", option.label)
			return
		}
		current.LineWeight = option.weight
		if err := saveFrameOptions(cfg, current); err != nil {
			ui.Errorln("Could not save frame line weight setting: %v", err)
			return
		}
		ui.AIln("Frame line weight changed to %s.", option.label)
		return
	}
	ui.Errorln("Invalid frame line weight selection. No changes made.")
}

// HandleShortcutConfiguration edits and saves the text and voice chat shortcuts.
// It returns true only after both bindings have been validated and saved.
func HandleShortcutConfiguration(cfg *Config) bool {
	if cfg == nil {
		return false
	}
	previous := cfg.Tray
	textShortcut, err := askShortcut("Text chat shortcut", previous.TextShortcut, nil)
	if err != nil {
		ui.Warningln("Shortcut settings canceled.")
		return false
	}
	voiceShortcut, err := askShortcut("Voice chat shortcut", previous.VoiceShortcut, func(value string) error {
		return shortcut.ValidateShortcuts(textShortcut, value)
	})
	if err != nil {
		ui.Warningln("Shortcut settings canceled.")
		return false
	}
	cfg.Tray = TrayConfig{TextShortcut: textShortcut, VoiceShortcut: voiceShortcut}
	if err := SaveConfig(cfg); err != nil {
		cfg.Tray = previous
		ui.Errorln("Could not save shortcut settings: %v", err)
		return false
	}
	ui.AIln("Chat shortcuts saved: text %s, voice %s", cfg.Tray.TextShortcut, cfg.Tray.VoiceShortcut)
	return true
}

func askShortcut(message, current string, validatePair func(string) error) (string, error) {
	value := current
	prompt := &survey.Input{
		Message: message,
		Default: current,
		Help:    "Use modifiers such as Ctrl, Alt, Shift, or Super with one letter, number, Space, or F1-F12 key.",
	}
	validate := func(answer interface{}) error {
		candidate, ok := answer.(string)
		if !ok {
			return fmt.Errorf("shortcut must be text")
		}
		if _, err := shortcut.ParseShortcut(candidate); err != nil {
			return err
		}
		if validatePair != nil {
			return validatePair(candidate)
		}
		return nil
	}
	if err := survey.AskOne(prompt, &value, survey.WithValidator(validate)); err != nil {
		return "", err
	}
	return value, nil
}

func handleThemeSettings(cfg *Config) {
	type themeOption struct {
		theme ui.Theme
		label string
	}
	options := make([]themeOption, 0, len(ui.Themes()))
	defaultOption := ""
	for _, theme := range ui.Themes() {
		description := ""
		switch theme.ID {
		case ui.ThemeOfficial:
			description = "DuckDuckGo logo colors"
		case ui.ThemeRetro:
			description = "Aurelia inspired"
		case ui.ThemeMono:
			description = "Grayscale"
		case ui.ThemeDracula:
			description = "Dracula classic palette"
		case ui.ThemeNord:
			description = "Arctic blue palette"
		}
		label := fmt.Sprintf("%s - %s (font: %s)", theme.Label, description, theme.FontHint)
		options = append(options, themeOption{theme: theme, label: label})
		if string(theme.ID) == cfg.Appearance.Theme {
			defaultOption = label
		}
	}

	labels := make([]string, 0, len(options))
	for _, option := range options {
		labels = append(labels, option.label)
	}
	choice := ""
	prompt := &survey.Select{
		Message: "Choose CLI theme:",
		Help:    "The font is a recommendation; configure the actual font in your terminal.",
		Options: labels,
		Default: defaultOption,
	}
	if err := askFramedConfigChoice(prompt, &choice, nil); err != nil {
		ui.Warningln("Theme selection canceled.")
		return
	}

	var selected ui.Theme
	for _, option := range options {
		if option.label == choice {
			selected = option.theme
			break
		}
	}
	if selected.ID == "" {
		ui.Errorln("Invalid theme selection. No changes made.")
		return
	}
	if string(selected.ID) == cfg.Appearance.Theme {
		ui.AIln("Theme is already active: %s", selected.Label)
		return
	}

	previous := cfg.Appearance.Theme
	cfg.Appearance.Theme = string(selected.ID)
	if err := saveConfig(cfg); err != nil {
		cfg.Appearance.Theme = previous
		ui.Errorln("Error saving config: %v", err)
		return
	}
	if !ui.SetTheme(cfg.Appearance.Theme) {
		cfg.Appearance.Theme = previous
		if err := saveConfig(cfg); err != nil {
			ui.Warningln("Could not restore saved theme after applying it failed: %v", err)
		}
		ui.Errorln("Could not apply theme %q.", selected.ID)
		return
	}
	ui.AIln("Theme changed to %s (font recommendation: %s)", selected.Label, selected.FontHint)
}

func handleSpeakSettings(cfg *Config) {
	for {
		choice := ""
		prompt := &survey.Select{
			Message: "Audio Agent Speak Configuration",
			Options: []string{
				fmt.Sprintf("Voice (%s)", speakVoiceLabel(cfg.Speak.Voice)),
				fmt.Sprintf("Window width (%d px)", cfg.Speak.WindowWidth),
				fmt.Sprintf("Window height (%d px)", cfg.Speak.WindowHeight),
				fmt.Sprintf("Start muted (%s)", speakYesNo(cfg.Speak.StartMuted)),
				fmt.Sprintf("Show subtitles (%s)", speakYesNo(cfg.Speak.ShowTranscript)),
				fmt.Sprintf("Output volume (%d%%)", cfg.Speak.Volume),
				fmt.Sprintf("Connection sound (%s)", speakYesNo(cfg.Speak.ReadinessSound)),
				fmt.Sprintf("Auto-start calls (%s)", speakYesNo(cfg.Speak.AutoStart)),
				"Back",
			},
			Default: "Back",
		}
		if err := askFramedConfigChoice(prompt, &choice, nil); err != nil {
			ui.Warningln("Audio Agent Speak settings canceled.")
			return
		}

		switch {
		case strings.HasPrefix(choice, "Voice"):
			handleSpeakVoiceSetting(cfg)
		case strings.HasPrefix(choice, "Window width"):
			editDashboardInteger(cfg, "Audio window width in pixels", cfg.Speak.WindowWidth, 320, 1400, func(value int) { cfg.Speak.WindowWidth = value })
		case strings.HasPrefix(choice, "Window height"):
			editDashboardInteger(cfg, "Audio window height in pixels", cfg.Speak.WindowHeight, 360, 1400, func(value int) { cfg.Speak.WindowHeight = value })
		case strings.HasPrefix(choice, "Start muted"):
			editSpeakBool(cfg, "Start voice calls with the microphone muted?", "Start muted", cfg.Speak.StartMuted, func(value bool) { cfg.Speak.StartMuted = value })
		case strings.HasPrefix(choice, "Show subtitles"):
			editSpeakBool(cfg, "Show user and assistant subtitles?", "Show subtitles", cfg.Speak.ShowTranscript, func(value bool) { cfg.Speak.ShowTranscript = value })
		case strings.HasPrefix(choice, "Output volume"):
			editDashboardInteger(cfg, "Audio output volume (percent)", cfg.Speak.Volume, 0, 100, func(value int) { cfg.Speak.Volume = value })
		case strings.HasPrefix(choice, "Connection sound"):
			editSpeakBool(cfg, "Play a short sound when the voice connection is ready?", "Connection sound", cfg.Speak.ReadinessSound, func(value bool) { cfg.Speak.ReadinessSound = value })
		case strings.HasPrefix(choice, "Auto-start calls"):
			editSpeakBool(cfg, "Start the voice session automatically when the window opens?", "Auto-start calls", cfg.Speak.AutoStart, func(value bool) { cfg.Speak.AutoStart = value })
		case choice == "Back":
			return
		}
	}
}

func handleSpeakVoiceSetting(cfg *Config) {
	voices := SpeakVoices()
	options := make([]string, 0, len(voices))
	defaultChoice := ""
	for _, voice := range voices {
		label := speakVoiceMenuLabel(voice)
		options = append(options, label)
		if voice.ID == cfg.Speak.Voice {
			defaultChoice = label
		}
	}
	choice := ""
	prompt := &survey.Select{
		Message: "Choose a Duck.ai voice:",
		Options: options,
		Default: defaultChoice,
	}
	if err := askFramedConfigChoice(prompt, &choice, nil); err != nil {
		ui.Warningln("Voice selection canceled.")
		return
	}
	for _, voice := range voices {
		if speakVoiceMenuLabel(voice) == choice {
			cfg.Speak.Voice = voice.ID
			saveAndReport(cfg, fmt.Sprintf("Duck.ai voice set to: %s", speakVoiceMenuLabel(voice)))
			return
		}
	}
	ui.Errorln("Invalid voice selection. No changes made.")
}

func speakVoiceLabel(id string) string {
	voice, ok := SpeakVoiceByID(id)
	if !ok {
		voice, _ = SpeakVoiceByID(DefaultSpeakVoice())
	}
	return speakVoiceMenuLabel(voice)
}

func speakVoiceMenuLabel(voice SpeakVoice) string {
	return fmt.Sprintf("%s - %s", voice.Name, voice.Description)
}

func speakYesNo(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

func editSpeakBool(cfg *Config, message, label string, current bool, set func(bool)) {
	value := current
	prompt := &survey.Confirm{Message: message, Default: current}
	if err := survey.AskOne(prompt, &value); err != nil {
		ui.Warningln("Operation canceled. No changes made.")
		return
	}
	set(value)
	saveAndReport(cfg, fmt.Sprintf("%s set to: %s", label, speakYesNo(value)))
}

func handleModelChange(cfg *Config, chatSession interfaces.ChatSession) {
	model := ""
	modelOptions := make([]string, 0, len(models.Available()))
	for _, definition := range models.Available() {
		modelOptions = append(modelOptions, string(definition.Alias))
	}
	prompt := &survey.Select{
		Message: "Choose Default Model:",
		Options: modelOptions,
		Default: cfg.DefaultModel,
	}
	if err := askFramedConfigChoice(prompt, &model, nil); err != nil {
		ui.Warningln("Model selection canceled.")
		return
	}

	if model != "" {
		cfg.DefaultModel = model
		if err := saveConfig(cfg); err != nil {
			ui.Errorln("Error saving config: %v", err)
			return
		}
		chatSession.ChangeModel(models.GetModel(model))
		ui.AIln("Default model updated and applied: %s", model)
	} else {
		ui.Errorln("Invalid choice. No changes made.")
	}
}

func handleExportDirChange(cfg *Config) {
	path := ""
	prompt := &survey.Input{
		Message: "Enter new export directory path:",
		Default: cfg.ExportDir,
		Help:    "Press Enter to use the default path.",
	}
	if err := survey.AskOne(prompt, &path); err != nil {
		ui.Warningln("Export directory selection canceled.")
		return
	}

	if path == "" {
		path = defaultExportPath()
	}

	cfg.ExportDir = path
	if err := ensureExportDir(cfg); err != nil {
		ui.Errorln("Error creating directory: %v", err)
	}

	if err := saveConfig(cfg); err != nil {
		ui.Errorln("Error saving config: %v", err)
	} else {
		ui.AIln("Export directory updated to: %s", path)
	}
}

func handleSearchSettings(cfg *Config) {
	qs := []*survey.Question{
		{
			Name:   "max_results",
			Prompt: &survey.Input{Message: "Max search results:", Default: strconv.Itoa(cfg.Search.MaxResults)},
		},
		{
			Name:   "max_retries",
			Prompt: &survey.Input{Message: "Max search retries:", Default: strconv.Itoa(cfg.Search.MaxRetries)},
		},
		{
			Name:   "retry_delay",
			Prompt: &survey.Input{Message: "Retry delay in seconds:", Default: strconv.Itoa(cfg.Search.RetryDelay)},
		},
		{
			Name:   "include_snippet",
			Prompt: &survey.Confirm{Message: "Include snippets in search results?", Default: cfg.Search.IncludeSnippet},
		},
	}
	answers := struct {
		MaxResults     string `survey:"max_results"`
		MaxRetries     string `survey:"max_retries"`
		RetryDelay     string `survey:"retry_delay"`
		IncludeSnippet bool   `survey:"include_snippet"`
	}{}

	err := survey.Ask(qs, &answers)
	if err != nil {
		ui.Errorln("Error reading input: %v", err)
		return
	}

	maxResults, err := strconv.Atoi(answers.MaxResults)
	if err != nil || maxResults < 1 || maxResults > 50 {
		ui.Errorln("Invalid max results; expected a value between 1 and 50")
	} else {
		cfg.Search.MaxResults = maxResults
	}
	if maxRetries, parseErr := strconv.Atoi(answers.MaxRetries); parseErr == nil && maxRetries >= 1 && maxRetries <= 10 {
		cfg.Search.MaxRetries = maxRetries
	} else {
		ui.Errorln("Invalid max retries; keeping the previous value")
	}
	if retryDelay, parseErr := strconv.Atoi(answers.RetryDelay); parseErr == nil && retryDelay >= 1 && retryDelay <= 30 {
		cfg.Search.RetryDelay = retryDelay
	} else {
		ui.Errorln("Invalid retry delay; keeping the previous value")
	}
	cfg.Search.IncludeSnippet = answers.IncludeSnippet

	if err := saveConfig(cfg); err != nil {
		ui.Errorln("Error saving config: %v", err)
	} else {
		ui.AIln("Search settings updated.")
	}
}

func handleShowMenuChange(cfg *Config) {
	showMenu := false
	prompt := &survey.Confirm{
		Message: "Show commands menu on startup?",
		Default: cfg.ShowMenu,
	}
	if err := survey.AskOne(prompt, &showMenu); err != nil {
		ui.Warningln("Menu preference change canceled.")
		return
	}
	cfg.ShowMenu = showMenu
	if err := saveConfig(cfg); err != nil {
		ui.Errorln("Error saving config: %v", err)
	} else {
		ui.AIln("Show menu preference updated.")
	}
}

func handleGlobalPromptChange(cfg *Config) {
	prompt := ""
	p := &survey.Input{
		Message: "Enter global prompt (or leave empty to clear):",
		Default: cfg.GlobalPrompt,
	}
	if err := survey.AskOne(p, &prompt); err != nil {
		ui.Warningln("Global prompt change canceled.")
		return
	}
	cfg.GlobalPrompt = prompt
	if err := saveConfig(cfg); err != nil {
		ui.Errorln("Error saving config: %v", err)
	} else {
		ui.AIln("Global prompt updated.")
	}
}

func handleNativeToolsChange(cfg *Config, chatSession interfaces.ChatSession) {
	answers := struct {
		Enabled         bool `survey:"enabled"`
		WebSearch       bool `survey:"web_search"`
		ImageGeneration bool `survey:"image_generation"`
	}{}

	questions := []*survey.Question{
		{
			Name: "enabled",
			Prompt: &survey.Confirm{
				Message: "Enable Duck.ai native tools?",
				Default: cfg.Tools.Enabled,
			},
		},
		{
			Name: "web_search",
			Prompt: &survey.Confirm{
				Message: "Allow native Web Search?",
				Default: cfg.Tools.WebSearch,
			},
		},
		{
			Name: "image_generation",
			Prompt: &survey.Confirm{
				Message: "Allow native image generation?",
				Default: cfg.Tools.ImageGeneration,
			},
		},
	}

	if err := survey.Ask(questions, &answers); err != nil {
		ui.Errorln("Error reading native tool settings: %v", err)
		return
	}

	cfg.Tools.Enabled = answers.Enabled
	cfg.Tools.WebSearch = answers.WebSearch
	cfg.Tools.ImageGeneration = answers.ImageGeneration
	if !cfg.Tools.Enabled {
		cfg.Tools.WebSearch = false
		cfg.Tools.ImageGeneration = false
	}
	chatSession.SetNativeTools(cfg.Tools.Enabled, cfg.Tools.WebSearch, cfg.Tools.ImageGeneration)

	if err := saveConfig(cfg); err != nil {
		ui.Errorln("Error saving native tool settings: %v", err)
	} else {
		ui.AIln("Duck.ai native tool settings updated.")
	}
}

func handleLibrarySettings(cfg *Config) {
	choice := ""
	prompt := &survey.Select{
		Message: "Library Settings",
		Options: []string{
			fmt.Sprintf("Enabled (%t)", cfg.Library.Enabled),
			"Manage Directories",
			"Back",
		},
		Default: "Back",
	}
	if err := askFramedConfigChoice(prompt, &choice, nil); err != nil {
		ui.Warningln("Library settings canceled.")
		return
	}

	switch {
	case strings.HasPrefix(choice, "Enabled"):
		cfg.Library.Enabled = !cfg.Library.Enabled
		if err := saveConfig(cfg); err != nil {
			ui.Errorln("Error saving config: %v", err)
		} else {
			ui.AIln("Library system set to: %t", cfg.Library.Enabled)
		}
	case choice == "Manage Directories":
		ui.Warningln("Directory management is handled via /library add and /library remove commands.")
	}
}

func handleAPISettings(cfg *Config) {
	for {
		choice := ""
		prompt := &survey.Select{
			Message: "API Settings",
			Options: []string{
				fmt.Sprintf("Enabled (%t)", cfg.API.Enabled),
				fmt.Sprintf("Host (%s)", cfg.API.Host),
				fmt.Sprintf("Port (%d)", cfg.API.Port),
				fmt.Sprintf("API key (%t)", cfg.API.APIKey != ""),
				fmt.Sprintf("Autostart on launch (%t)", cfg.API.Autostart),
				fmt.Sprintf("Log API Requests (%t)", cfg.API.LogRequests),
				fmt.Sprintf("Show GIN Logs (%t)", cfg.API.ShowGinLogs),
				"Back",
			},
			Default: "Back",
		}
		if err := askFramedConfigChoice(prompt, &choice, nil); err != nil {
			ui.Warningln("API settings canceled.")
			return
		}

		switch {
		case strings.HasPrefix(choice, "Enabled"):
			cfg.API.Enabled = !cfg.API.Enabled
			saveAndReport(cfg, fmt.Sprintf("API Enabled status set to: %t", cfg.API.Enabled))
		case strings.HasPrefix(choice, "Host"):
			handleAPIHostChange(cfg)
		case strings.HasPrefix(choice, "Port"):
			handleAPIPortChange(cfg)
		case strings.HasPrefix(choice, "API key"):
			handleAPIKeyChange(cfg)
		case strings.HasPrefix(choice, "Autostart"):
			cfg.API.Autostart = !cfg.API.Autostart
			saveAndReport(cfg, fmt.Sprintf("API Autostart set to: %t", cfg.API.Autostart))
		case strings.HasPrefix(choice, "Log API Requests"):
			cfg.API.LogRequests = !cfg.API.LogRequests
			saveAndReport(cfg, fmt.Sprintf("API Request Logging set to: %t", cfg.API.LogRequests))
		case strings.HasPrefix(choice, "Show GIN Logs"):
			cfg.API.ShowGinLogs = !cfg.API.ShowGinLogs
			saveAndReport(cfg, fmt.Sprintf("GIN Logs visibility set to: %t", cfg.API.ShowGinLogs))
		case choice == "Back":
			return
		}
	}
}

func handleDashboardSettings(cfg *Config) {
	for {
		choice := ""
		prompt := &survey.Select{
			Message: "Local Dashboard Settings",
			Options: []string{
				fmt.Sprintf("Autostart (%t)", cfg.Dashboard.Autostart),
				fmt.Sprintf("Set/change dashboard password (%t)", cfg.Dashboard.PasswordHash != ""),
				"Remove dashboard password",
				fmt.Sprintf("Port (%d)", cfg.Dashboard.Port),
				fmt.Sprintf("Refresh interval seconds (%d)", cfg.Dashboard.RefreshIntervalSeconds),
				fmt.Sprintf("History retention days (%d)", cfg.Dashboard.RetentionDays),
				fmt.Sprintf("Show conversations (%t)", cfg.Dashboard.ShowConversations),
				fmt.Sprintf("Show prompt and response content (%t)", cfg.Dashboard.ShowConversationContent),
				fmt.Sprintf("Allow conversation analysis (%t)", cfg.Dashboard.AllowConversationAnalysis),
				fmt.Sprintf("Analysis token budget (%d)", cfg.Dashboard.AnalysisTokenBudget),
				"Back",
			},
			Default: "Back",
		}
		if err := askFramedConfigChoice(prompt, &choice, nil); err != nil {
			ui.Warningln("Dashboard settings canceled.")
			return
		}

		switch {
		case strings.HasPrefix(choice, "Autostart"):
			cfg.Dashboard.Autostart = !cfg.Dashboard.Autostart
			saveAndReport(cfg, fmt.Sprintf("Dashboard autostart set to: %t", cfg.Dashboard.Autostart))
		case strings.HasPrefix(choice, "Set/change dashboard password"):
			handleDashboardPasswordChange(cfg)
		case choice == "Remove dashboard password":
			handleDashboardPasswordRemoval(cfg)
		case strings.HasPrefix(choice, "Show conversations"):
			cfg.Dashboard.ShowConversations = !cfg.Dashboard.ShowConversations
			saveAndReport(cfg, fmt.Sprintf("Dashboard conversations visible: %t", cfg.Dashboard.ShowConversations))
		case strings.HasPrefix(choice, "Show prompt and response content"):
			cfg.Dashboard.ShowConversationContent = !cfg.Dashboard.ShowConversationContent
			saveAndReport(cfg, fmt.Sprintf("Dashboard prompt and response content visible: %t", cfg.Dashboard.ShowConversationContent))
		case strings.HasPrefix(choice, "Allow conversation analysis"):
			cfg.Dashboard.AllowConversationAnalysis = !cfg.Dashboard.AllowConversationAnalysis
			saveAndReport(cfg, fmt.Sprintf("Dashboard conversation analysis allowed: %t", cfg.Dashboard.AllowConversationAnalysis))
		case strings.HasPrefix(choice, "Port"):
			editDashboardInteger(cfg, "Dashboard port", cfg.Dashboard.Port, 1, 65535, func(v int) { cfg.Dashboard.Port = v })
		case strings.HasPrefix(choice, "Refresh interval"):
			editDashboardInteger(cfg, "Refresh interval in seconds", cfg.Dashboard.RefreshIntervalSeconds, 1, 60, func(v int) { cfg.Dashboard.RefreshIntervalSeconds = v })
		case strings.HasPrefix(choice, "History retention"):
			editDashboardInteger(cfg, "History retention in days", cfg.Dashboard.RetentionDays, 1, 3650, func(v int) { cfg.Dashboard.RetentionDays = v })
		case strings.HasPrefix(choice, "Analysis token budget"):
			editDashboardInteger(cfg, "Estimated analysis token budget", cfg.Dashboard.AnalysisTokenBudget, 1000, 32000, func(v int) { cfg.Dashboard.AnalysisTokenBudget = v })
		case choice == "Back":
			return
		}
	}
}

func handleDashboardPasswordChange(cfg *Config) {
	password := ""
	if err := survey.AskOne(&survey.Password{Message: "Enter dashboard password:"}, &password); err != nil {
		ui.Warningln("Dashboard password change canceled.")
		return
	}
	confirmation := ""
	if err := survey.AskOne(&survey.Password{Message: "Confirm dashboard password:"}, &confirmation); err != nil {
		ui.Warningln("Dashboard password change canceled.")
		return
	}
	if password != confirmation {
		ui.Errorln("Passwords do not match. No changes made.")
		return
	}
	salt, hash, err := security.HashPassword(password)
	if err != nil {
		ui.Errorln("Could not set dashboard password: %v", err)
		return
	}
	cfg.Dashboard.PasswordSalt = salt
	cfg.Dashboard.PasswordHash = hash
	saveAndReport(cfg, "Dashboard password protection enabled.")
}

func handleDashboardPasswordRemoval(cfg *Config) {
	if cfg.Dashboard.PasswordHash == "" {
		ui.AIln("Dashboard password protection is already disabled.")
		return
	}
	confirmed := false
	if err := survey.AskOne(&survey.Confirm{Message: "Disable dashboard password protection?", Default: false}, &confirmed); err != nil {
		ui.Warningln("Dashboard password removal canceled.")
		return
	}
	if !confirmed {
		ui.AIln("Dashboard password protection was not changed.")
		return
	}
	cfg.Dashboard.PasswordSalt = ""
	cfg.Dashboard.PasswordHash = ""
	saveAndReport(cfg, "Dashboard password protection disabled.")
}

func editDashboardInteger(cfg *Config, label string, current, minimum, maximum int, set func(int)) {
	input := ""
	prompt := &survey.Input{Message: label + ":", Default: strconv.Itoa(current)}
	if err := survey.AskOne(prompt, &input); err != nil {
		ui.Warningln("Operation canceled. No changes made.")
		return
	}
	value, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || value < minimum || value > maximum {
		ui.Errorln("Value must be between %d and %d. No changes made.", minimum, maximum)
		return
	}
	set(value)
	saveAndReport(cfg, fmt.Sprintf("%s updated to: %d", label, value))
}

func handleAPIPortChange(cfg *Config) {
	portStr := ""
	prompt := &survey.Input{
		Message: "Enter API Port:",
		Default: strconv.Itoa(cfg.API.Port),
	}
	if err := survey.AskOne(prompt, &portStr); err != nil {
		ui.Warningln("API port change canceled.")
		return
	}

	if port, err := strconv.Atoi(portStr); err == nil && port >= 1 && port <= 65535 {
		cfg.API.Port = port
		saveAndReport(cfg, fmt.Sprintf("API Port updated to: %d", port))
	} else {
		ui.Errorln("Invalid port number. No changes made.")
	}
}

func handleAPIHostChange(cfg *Config) {
	host := ""
	prompt := &survey.Input{Message: "Enter API bind address:", Default: cfg.API.Host}
	if err := survey.AskOne(prompt, &host); err != nil {
		return
	}
	if strings.TrimSpace(host) == "" {
		ui.Errorln("Bind address cannot be empty.")
		return
	}
	cfg.API.Host = strings.TrimSpace(host)
	saveAndReport(cfg, fmt.Sprintf("API bind address updated to: %s", cfg.API.Host))
}

func handleAPIKeyChange(cfg *Config) {
	key := ""
	prompt := &survey.Password{Message: "Enter API key (leave empty to disable authentication):"}
	if err := survey.AskOne(prompt, &key); err != nil {
		return
	}
	cfg.API.APIKey = strings.TrimSpace(key)
	saveAndReport(cfg, fmt.Sprintf("API key protection enabled: %t", cfg.API.APIKey != ""))
}

func handleLongInputProtectionChange(cfg *Config) {
	confirmLongInput := cfg.ConfirmLongInput
	prompt := &survey.Confirm{
		Message: "Enable Long Input Protection?",
		Default: cfg.ConfirmLongInput,
		Help:    "When enabled, you'll be asked to confirm before sending long text (>500 chars), URLs, or multi-line content to the AI.",
	}
	if err := survey.AskOne(prompt, &confirmLongInput); err != nil {
		// If the user presses Ctrl+C, AskOne returns an error.
		// We can interpret this as "no change".
		ui.Warningln("Operation cancelled. No changes made.")
		return
	}

	cfg.ConfirmLongInput = confirmLongInput
	saveAndReport(cfg, fmt.Sprintf("Long input protection set to: %v", cfg.ConfirmLongInput))
}

func saveAndReport(cfg *Config, message string) {
	if err := saveConfig(cfg); err != nil {
		ui.Errorln("Error saving config: %v", err)
	} else {
		ui.AIln(message)
	}
}

// AddPrompt adds a new prompt to the config
func AddPrompt(cfg *Config, name, content string) error {
	if cfg.Prompts == nil {
		cfg.Prompts = make(map[string]string)
	}
	if _, exists := cfg.Prompts[name]; exists {
		return fmt.Errorf("prompt '%s' already exists", name)
	}
	cfg.Prompts[name] = content
	return saveConfig(cfg)
}

// EditPrompt edits an existing prompt
func EditPrompt(cfg *Config, name, content string) error {
	if cfg.Prompts == nil {
		return fmt.Errorf("no prompts configured")
	}
	if _, exists := cfg.Prompts[name]; !exists {
		return fmt.Errorf("prompt '%s' does not exist", name)
	}
	cfg.Prompts[name] = content
	return saveConfig(cfg)
}

// RemovePrompt removes a prompt by name
func RemovePrompt(cfg *Config, name string) error {
	if cfg.Prompts == nil {
		return fmt.Errorf("no prompts configured")
	}
	if _, exists := cfg.Prompts[name]; !exists {
		return fmt.Errorf("prompt '%s' does not exist", name)
	}
	delete(cfg.Prompts, name)
	return saveConfig(cfg)
}

// ListPrompts returns a list of prompt names
func ListPrompts(cfg *Config) []string {
	if cfg.Prompts == nil {
		return []string{}
	}
	names := make([]string, 0, len(cfg.Prompts))
	for name := range cfg.Prompts {
		names = append(names, name)
	}
	return names
}

// GetPrompt returns the content of a prompt by name
func GetPrompt(cfg *Config, name string) (string, error) {
	if cfg.Prompts == nil {
		return "", fmt.Errorf("no prompts configured")
	}
	content, exists := cfg.Prompts[name]
	if !exists {
		return "", fmt.Errorf("prompt '%s' does not exist", name)
	}
	return content, nil
}

// handlePromptManagement provides an interactive menu for managing prompts
func HandlePromptManagement(cfg *Config) {
	for {
		choice := ""
		prompt := &survey.Select{
			Message: "Prompt Management",
			Options: []string{"List Prompts", "Add Prompt", "Edit Prompt", "Remove Prompt", "Back"},
			Default: "Back",
		}
		if err := askFramedConfigChoice(prompt, &choice, nil); err != nil {
			ui.Warningln("Prompt management canceled.")
			return
		}
		switch choice {
		case "List Prompts":
			names := ListPrompts(cfg)
			if len(names) == 0 {
				ui.AIln("No prompts saved.")
			} else {
				ui.AIln("Saved prompts:")
				for _, name := range names {
					content, _ := GetPrompt(cfg, name)
					preview := content
					if len(preview) > 80 {
						preview = preview[:80] + "..."
					}
					ui.AIln("- %s: %s", name, preview)
				}
			}
		case "Add Prompt":
			name := ""
			content := ""
			if err := survey.AskOne(&survey.Input{Message: "Prompt name:"}, &name); err != nil {
				return
			}
			if err := survey.AskOne(&survey.Input{Message: "Prompt content:"}, &content); err != nil {
				return
			}
			if name == "" || content == "" {
				ui.Errorln("Name and content required.")
				continue
			}
			err := AddPrompt(cfg, name, content)
			if err != nil {
				ui.Errorln("%v", err)
			} else {
				ui.AIln("Prompt '%s' added.", name)
			}
		case "Edit Prompt":
			names := ListPrompts(cfg)
			if len(names) == 0 {
				ui.AIln("No prompts to edit.")
				continue
			}
			name := ""
			prompt := &survey.Select{Message: "Select prompt to edit:", Options: names}
			if err := askFramedConfigChoice(prompt, &name, nil); err != nil {
				return
			}
			oldContent, _ := GetPrompt(cfg, name)
			content := oldContent
			if err := survey.AskOne(&survey.Input{Message: "New content:", Default: oldContent}, &content); err != nil {
				return
			}
			err := EditPrompt(cfg, name, content)
			if err != nil {
				ui.Errorln("%v", err)
			} else {
				ui.AIln("Prompt '%s' updated.", name)
			}
		case "Remove Prompt":
			names := ListPrompts(cfg)
			if len(names) == 0 {
				ui.AIln("No prompts to remove.")
				continue
			}
			name := ""
			prompt := &survey.Select{Message: "Select prompt to remove:", Options: names}
			if err := askFramedConfigChoice(prompt, &name, nil); err != nil {
				return
			}
			err := RemovePrompt(cfg, name)
			if err != nil {
				ui.Errorln("%v", err)
			} else {
				ui.AIln("Prompt '%s' removed.", name)
			}
		case "Back":
			return
		}
	}
}
