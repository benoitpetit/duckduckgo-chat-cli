package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/ui"
)

type resetTestChatSession struct {
	model      models.Model
	modelCalls int
	tools      [3]bool
	toolsCalls int
}

func (s *resetTestChatSession) ChangeModel(model models.Model) {
	s.model = model
	s.modelCalls++
}

func (s *resetTestChatSession) SetNativeTools(enabled, webSearch, imageGeneration bool) {
	s.tools = [3]bool{enabled, webSearch, imageGeneration}
	s.toolsCalls++
}

type fixedConfigResetPrompter struct {
	confirmed    bool
	confirmErr   error
	phrase       string
	phraseErr    error
	defaultValue bool
	message      string
	help         string
	phrasePrompt string
	phraseCalls  int
}

func (p *fixedConfigResetPrompter) ConfirmReset(message, help string, defaultValue bool) (bool, error) {
	p.message = message
	p.help = help
	p.defaultValue = defaultValue
	return p.confirmed, p.confirmErr
}

func (p *fixedConfigResetPrompter) EnterResetPhrase(expected string) (string, error) {
	p.phrasePrompt = expected
	p.phraseCalls++
	return p.phrase, p.phraseErr
}

func TestResetConfigurationToDefaultsResetsEveryFieldAndPersists(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)

	cfg := &Config{
		TOSAccepted: true, DefaultModel: "gpt-5.6-luna", ExportDir: "/custom/export", LastUpdateTime: time.Unix(1, 0),
		Appearance: AppearanceConfig{Theme: "nord", FrameResponses: false, FrameWidthMode: ui.FrameWidthFull, FrameMaxWidth: 144, FrameLineWeight: ui.FrameLineHeavy},
		Search:     SearchConfig{MaxResults: 50, IncludeSnippet: false, MaxRetries: 10, RetryDelay: 30},
		Library:    LibraryConfig{Directories: []string{"/custom/library"}, Enabled: false},
		API:        APIConfig{Enabled: true, Host: "0.0.0.0", Port: 9999, Autostart: true, LogRequests: false, ShowGinLogs: false, APIKey: "secret", AllowedOrigins: []string{"https://example.test"}},
		Tools:      ToolsConfig{Enabled: true, WebSearch: true, ImageGeneration: true},
		Speak:      SpeakConfig{Voice: "voice-4", WindowWidth: 900, WindowHeight: 900, StartMuted: true, ShowTranscript: false, Volume: 10, ReadinessSound: false, AutoStart: false, SchemaVersion: 1},
		Tray:       TrayConfig{TextShortcut: "Ctrl+Shift+X", VoiceShortcut: "Ctrl+Shift+Y"},
		RateLimit:  RateLimitConfig{OpenBrowser: true, CooldownMinutes: 60},
		Dashboard:  DashboardConfig{Autostart: true, Port: 9000, RefreshIntervalSeconds: 10, RetentionDays: 3650, ShowConversations: true, ShowConversationContent: true, AllowConversationAnalysis: true, AnalysisTokenBudget: 32000, PasswordSalt: "salt", PasswordHash: "hash"},
		Coordinate: CoordinateConfig{Strategy: "debate", Agents: []CoordinateAgentConfig{{Model: "gpt-5.6-luna", Role: "Custom"}}, DebateRounds: 2, Parallel: true, Profiles: map[string]CoordinatePresetConfig{"debate": {Agents: []CoordinateAgentConfig{{Model: "gpt-5.6-luna", Role: "Custom"}}}}},
		ShowMenu:   true, GlobalPrompt: "custom global prompt", ConfirmLongInput: false, Prompts: map[string]string{"custom": "custom prompt"},
	}
	session := &resetTestChatSession{}
	started := time.Now()
	if err := resetConfigurationToDefaults(cfg, session); err != nil {
		t.Fatalf("resetConfigurationToDefaults() error = %v", err)
	}
	finished := time.Now()

	got := *cfg
	got.LastUpdateTime = time.Time{}
	want := Config{
		DefaultModel: string(models.Default()), ExportDir: defaultExportPath(),
		Appearance:       AppearanceConfig{Theme: "official", FrameResponses: true, FrameWidthMode: ui.FrameWidthMaximum, FrameMaxWidth: 112, FrameLineWeight: ui.FrameLineSingle},
		Search:           SearchConfig{MaxResults: 10, IncludeSnippet: true, MaxRetries: 3, RetryDelay: 1},
		Library:          LibraryConfig{Directories: []string{}, Enabled: true},
		API:              APIConfig{Host: "127.0.0.1", Port: 8080, LogRequests: true, ShowGinLogs: true},
		Speak:            SpeakConfig{Voice: "voice-1", WindowWidth: 480, WindowHeight: 500, ShowTranscript: true, Volume: 100, ReadinessSound: true, AutoStart: true, SchemaVersion: 3},
		Tray:             TrayConfig{TextShortcut: "Ctrl+Alt+D", VoiceShortcut: "Ctrl+Alt+V"},
		RateLimit:        RateLimitConfig{CooldownMinutes: 10},
		Dashboard:        DashboardConfig{Port: 8765, RefreshIntervalSeconds: 3, RetentionDays: 90, AnalysisTokenBudget: 8000},
		ConfirmLongInput: true, Prompts: map[string]string{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reset config = %#v, want every default %#v", got, want)
	}
	if gotOptions, wantOptions := ui.CurrentFrameOptions(), (ui.FrameOptions{WidthMode: ui.FrameWidthMaximum, MaxWidth: 112, LineWeight: ui.FrameLineSingle}); gotOptions != wantOptions {
		t.Fatalf("reset frame options = %+v, want %+v", gotOptions, wantOptions)
	}
	if cfg.LastUpdateTime.Before(started) || cfg.LastUpdateTime.After(finished) {
		t.Fatalf("LastUpdateTime = %s, want reset time between %s and %s", cfg.LastUpdateTime, started, finished)
	}
	if session.model != models.Default() || session.modelCalls != 1 || session.tools != [3]bool{} || session.toolsCalls != 1 {
		t.Fatalf("active chat settings were not reset: %+v", session)
	}

	data, err := os.ReadFile(filepath.Join(root, "duckduckgo-chat-cli", "config.json"))
	if err != nil {
		t.Fatalf("read reset config file: %v", err)
	}
	var persisted Config
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decode reset config file: %v", err)
	}
	if !persisted.LastUpdateTime.Equal(cfg.LastUpdateTime) {
		t.Fatalf("persisted LastUpdateTime = %s, in-memory LastUpdateTime = %s", persisted.LastUpdateTime, cfg.LastUpdateTime)
	}
	persisted.LastUpdateTime = time.Time{}
	current := *cfg
	current.LastUpdateTime = time.Time{}
	if !reflect.DeepEqual(persisted, current) {
		t.Fatalf("persisted reset config = %#v, in-memory config = %#v", persisted, *cfg)
	}
}

func TestResetConfigurationSaveFailureLeavesConfigAndChatUntouched(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	if err := os.WriteFile(filepath.Join(root, "duckduckgo-chat-cli"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{TOSAccepted: true, DefaultModel: "gpt-5.6-luna", GlobalPrompt: "keep me", Prompts: map[string]string{"keep": "me"}}
	before := *cfg
	session := &resetTestChatSession{}
	if err := resetConfigurationToDefaults(cfg, session); err == nil {
		t.Fatal("resetConfigurationToDefaults() succeeded with an unwritable config path")
	}
	if !reflect.DeepEqual(*cfg, before) {
		t.Fatalf("failed reset changed in-memory config: got %+v, want %+v", *cfg, before)
	}
	if session.modelCalls != 0 || session.toolsCalls != 0 {
		t.Fatalf("failed reset changed the active chat session: %+v", session)
	}
}

func TestConfigResetRequiresDefaultNoConfirmationAndExactPhrase(t *testing.T) {
	for _, test := range []struct {
		name      string
		confirmed bool
		phrase    string
		wantAsk   bool
		wantReset bool
	}{
		{name: "declined", confirmed: false, phrase: "RESET", wantAsk: false},
		{name: "wrong phrase", confirmed: true, phrase: "reset", wantAsk: true},
		{name: "confirmed", confirmed: true, phrase: "RESET", wantAsk: true, wantReset: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", root)
			t.Setenv("HOME", root)
			cfg := &Config{TOSAccepted: true, DefaultModel: "gpt-5.6-luna", GlobalPrompt: "keep me"}
			before := *cfg
			prompt := &fixedConfigResetPrompter{confirmed: test.confirmed, phrase: test.phrase}
			session := &resetTestChatSession{}
			reset := resetConfigurationWithConfirmation(cfg, session, prompt)
			if reset != test.wantReset {
				t.Fatalf("resetConfigurationWithConfirmation() = %t, want %t", reset, test.wantReset)
			}
			if prompt.defaultValue {
				t.Fatal("reset confirmation defaults to yes")
			}
			for _, warning := range []string{"API key", "dashboard password", "terms", "prompts", "90 days"} {
				if !strings.Contains(prompt.message+" "+prompt.help, warning) {
					t.Errorf("security confirmation omits %q warning", warning)
				}
			}
			expectedPhraseCalls := 0
			if test.wantAsk {
				expectedPhraseCalls = 1
			}
			if prompt.phraseCalls != expectedPhraseCalls {
				t.Fatalf("phrase prompt calls = %d, confirmed = %t", prompt.phraseCalls, test.confirmed)
			}
			if test.wantAsk && prompt.phrasePrompt != "RESET" {
				t.Fatalf("reset phrase prompt expected %q, want RESET", prompt.phrasePrompt)
			}
			if test.wantReset {
				if cfg.TOSAccepted || cfg.GlobalPrompt != "" || cfg.DefaultModel != string(models.Default()) || session.modelCalls != 1 {
					t.Fatalf("confirmed reset did not apply defaults: config=%+v session=%+v", cfg, session)
				}
			} else if !reflect.DeepEqual(*cfg, before) {
				t.Fatalf("unconfirmed reset changed config: got %+v, want %+v", *cfg, before)
			}
		})
	}
}
