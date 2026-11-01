package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/core"
	"github.com/charmbracelet/x/ansi"
)

func TestInitializePreservesExplicitDisabledOptions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(Config{
		Search:  SearchConfig{IncludeSnippet: false},
		Library: LibraryConfig{Enabled: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Initialize()
	if cfg.Search.IncludeSnippet {
		t.Fatal("IncludeSnippet was re-enabled")
	}
	if cfg.Library.Enabled {
		t.Fatal("Library.Enabled was re-enabled")
	}
}

func TestSaveConfigUsesPrivateAtomicFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	if err := SaveConfig(&Config{DefaultModel: "gpt-5.6-luna"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "duckduckgo-chat-cli", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("config permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestSaveConfigCreatesPrivateDirectory(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	if err := SaveConfig(&Config{DefaultModel: "gpt-5.6-luna"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, "duckduckgo-chat-cli"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("config directory mode = %#o, want 0700", got)
	}
}

func TestEnsureExportDirCreatesPrivateDirectory(t *testing.T) {
	root := t.TempDir()
	exportDir := filepath.Join(root, "new", "exports")
	if err := ensureExportDir(&Config{ExportDir: exportDir}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("new export directory mode = %#o, want 0700", got)
	}
}

func TestEnsureExportDirPreservesExistingDirectoryMode(t *testing.T) {
	exportDir := filepath.Join(t.TempDir(), "exports")
	if err := os.Mkdir(exportDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := ensureExportDir(&Config{ExportDir: exportDir}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("existing export directory mode = %#o, want unchanged 0750", got)
	}
}

func TestInitializeAddsDashboardDefaultsToLegacyConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"search":{"include_snippet":false},"library":{"enabled":false}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Initialize()
	if cfg.Search.IncludeSnippet || cfg.Library.Enabled {
		t.Fatal("legacy explicit false settings were not preserved")
	}
	want := DashboardConfig{Port: 8765, RefreshIntervalSeconds: 3, RetentionDays: 90, AnalysisTokenBudget: 8000}
	if cfg.Dashboard != want {
		t.Fatalf("Dashboard = %+v, want defaults %+v", cfg.Dashboard, want)
	}
}

func TestInitializePreservesAvailableLegacyModel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"default_model":"gpt-5.6-luna"}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Initialize()
	if cfg.DefaultModel != "gpt-5.6-luna" {
		t.Fatalf("DefaultModel = %q, want configured available model gpt-5.6-luna", cfg.DefaultModel)
	}
}

func TestDashboardConfigJSONRoundTrip(t *testing.T) {
	want := DashboardConfig{
		Autostart: true, Port: 4321, RefreshIntervalSeconds: 7, RetentionDays: 45,
		ShowConversations: true, AllowConversationAnalysis: true, AnalysisTokenBudget: 12000,
	}
	data, err := json.Marshal(Config{Dashboard: want})
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Dashboard != want {
		t.Fatalf("Dashboard round trip = %+v, want %+v", got.Dashboard, want)
	}
}

func TestCoordinateProfilesSurviveConfigRoundTrip(t *testing.T) {
	input := `{"coordinate":{"strategy":"chain","profiles":{"independent":{"agents":[{"model":"gpt-6-luna","role":"Analyst"}]},"chain":{"agents":[{"model":"gpt-5.6-luna","role":"Solver"}],"parallel":true},"debate":{"agents":[{"model":"gpt-5.4-mini","role":"Debater"}],"debate_rounds":2}}}}`
	var cfg Config
	if err := json.Unmarshal([]byte(input), &cfg); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var encoded map[string]any
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	coordinate, ok := encoded["coordinate"].(map[string]any)
	if !ok {
		t.Fatalf("coordinate config missing after round trip: %s", data)
	}
	profiles, ok := coordinate["profiles"].(map[string]any)
	if !ok || len(profiles) != 3 {
		t.Fatalf("coordinate profiles did not survive round trip: %s", data)
	}
}

func TestDashboardConfigInvalidValuesUseDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"dashboard":{"port":70000,"refresh_interval_seconds":0,"retention_days":9000,"analysis_token_budget":10}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Initialize().Dashboard
	if got.Port != 8765 || got.RefreshIntervalSeconds != 3 || got.RetentionDays != 90 || got.AnalysisTokenBudget != 8000 {
		t.Fatalf("invalid dashboard values were not reset to defaults: %+v", got)
	}
}

func TestDashboardHistoryPathIsSeparateFromExports(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	path := DashboardHistoryPath()
	if filepath.Dir(path) != filepath.Join(root, "duckduckgo-chat-cli") {
		t.Fatalf("DashboardHistoryPath() = %q, outside config directory", path)
	}
	if filepath.Clean(path) == filepath.Clean(defaultExportPath()) {
		t.Fatalf("dashboard history path overlaps export directory: %q", path)
	}
}

func TestDashboardPasswordConfigOmitsEmptyFields(t *testing.T) {
	var cfg Config
	if err := json.Unmarshal([]byte(`{"dashboard":{"password_salt":"salt-value","password_hash":"hash-value"}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Dashboard.PasswordSalt != "salt-value" || cfg.Dashboard.PasswordHash != "hash-value" {
		t.Fatalf("password verifier config did not round trip: %+v", cfg.Dashboard)
	}

	data, err := json.Marshal(Config{Dashboard: DashboardConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	var encoded struct {
		Dashboard map[string]json.RawMessage `json:"dashboard"`
	}
	if err := json.Unmarshal(data, &encoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"password_salt", "password_hash"} {
		if _, exists := encoded.Dashboard[key]; exists {
			t.Errorf("empty dashboard config unexpectedly serialized %q", key)
		}
	}
}

func TestInitializeAddsSpeakWindowDefaultsToLegacyConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"default_model":"gpt-5.6-luna"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Initialize().Speak
	want := SpeakConfig{Voice: "voice-1", WindowWidth: 480, WindowHeight: 500, ShowTranscript: true, Volume: 100, ReadinessSound: true, AutoStart: true, SchemaVersion: 3}
	if got != want {
		t.Fatalf("Speak config = %+v, want %+v", got, want)
	}
}

func TestInitializeAddsSpeakVoiceAndPlaybackDefaults(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)

	data, err := json.Marshal(Initialize().Speak)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"voice":           `"voice-1"`,
		"start_muted":     "false",
		"show_transcript": "true",
		"volume":          "100",
		"readiness_sound": "true",
		"auto_start":      "true",
	}
	for key, wantValue := range want {
		raw, ok := got[key]
		if !ok {
			t.Errorf("Speak JSON is missing %q; got %s", key, data)
			continue
		}
		if string(raw) != wantValue {
			t.Errorf("Speak JSON %q = %s, want %s", key, raw, wantValue)
		}
	}
}

func TestInitializeNormalizesSpeakVoiceAndVolumeWithoutOverridingToggles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"speak":{"voice":"voice-9","volume":101,"start_muted":true,"show_transcript":false,"readiness_sound":false,"auto_start":false}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(Initialize().Speak)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"voice":           `"voice-1"`,
		"volume":          "100",
		"start_muted":     "true",
		"show_transcript": "false",
		"readiness_sound": "false",
		"auto_start":      "false",
	}
	for key, wantValue := range want {
		if string(got[key]) != wantValue {
			t.Errorf("normalized Speak JSON %q = %s, want %s", key, got[key], wantValue)
		}
	}
}

func TestInitializePreservesValidSpeakVoiceAndVolume(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"speak":{"voice":"voice-3","volume":35}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(Initialize().Speak)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if string(got["voice"]) != `"voice-3"` || string(got["volume"]) != "35" {
		t.Fatalf("valid Speak choices were not preserved: %s", encoded)
	}
}

func TestInitializeAddsTrayShortcutDefaultsToLegacyConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"default_model":"gpt-5.6-luna"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Initialize().Tray
	want := TrayConfig{TextShortcut: "Ctrl+Alt+D", VoiceShortcut: "Ctrl+Alt+V"}
	if got != want {
		t.Fatalf("Tray config = %+v, want defaults %+v", got, want)
	}
}

func TestInitializePreservesValidTrayShortcutsAndResetsDuplicates(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"tray":{"text_shortcut":"Shift+Ctrl+T","voice_shortcut":"Alt+Ctrl+V"}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Initialize().Tray
	want := TrayConfig{TextShortcut: "Shift+Ctrl+T", VoiceShortcut: "Alt+Ctrl+V"}
	if got != want {
		t.Fatalf("valid Tray config = %+v, want %+v", got, want)
	}

	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"tray":{"text_shortcut":"Ctrl+Alt+T","voice_shortcut":"alt+ctrl+t"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got = Initialize().Tray
	want = TrayConfig{TextShortcut: "Ctrl+Alt+D", VoiceShortcut: "Ctrl+Alt+V"}
	if got != want {
		t.Fatalf("duplicate Tray config = %+v, want safe defaults %+v", got, want)
	}
}

func TestInitializeNormalizesSpeakWindowSizeAndPreservesChoices(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"speak":{"window_width":319,"window_height":1401,"center_on_start":false,"resizable":false}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Initialize().Speak
	want := SpeakConfig{Voice: "voice-1", WindowWidth: 480, WindowHeight: 500, ShowTranscript: true, Volume: 100, ReadinessSound: true, AutoStart: true, SchemaVersion: 3}
	if got != want {
		t.Fatalf("Speak config = %+v, want %+v", got, want)
	}
}

func TestSpeakConfigJSONRoundTrip(t *testing.T) {
	want := SpeakConfig{
		Voice:          "voice-2",
		WindowWidth:    720,
		WindowHeight:   640,
		StartMuted:     true,
		ShowTranscript: false,
		Volume:         35,
		ReadinessSound: false,
		AutoStart:      false,
	}
	data, err := json.Marshal(Config{Speak: want})
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Speak != want {
		t.Fatalf("Speak config round trip = %+v, want %+v", got.Speak, want)
	}
}

func TestInitializeAddsRateLimitDefaultsToLegacyConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"default_model":"gpt-5.6-luna"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Initialize().RateLimit
	want := RateLimitConfig{OpenBrowser: false, CooldownMinutes: 10}
	if got != want {
		t.Fatalf("RateLimit config = %+v, want %+v", got, want)
	}
}

func TestInitializeKeepsRateLimitBrowserFallbackDisabled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"rate_limit":{"open_browser":false,"cooldown_minutes":25}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Initialize().RateLimit
	want := RateLimitConfig{OpenBrowser: false, CooldownMinutes: 25}
	if got != want {
		t.Fatalf("RateLimit config = %+v, want %+v", got, want)
	}
}

func TestInitializeReplacesOutOfRangeRateLimitCooldown(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"rate_limit":{"open_browser":true,"cooldown_minutes":-5}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Initialize().RateLimit
	want := RateLimitConfig{OpenBrowser: true, CooldownMinutes: 10}
	if got != want {
		t.Fatalf("RateLimit config = %+v, want %+v", got, want)
	}
}

func TestRateLimitConfigJSONRoundTrip(t *testing.T) {
	want := RateLimitConfig{OpenBrowser: false, CooldownMinutes: 3}
	data, err := json.Marshal(Config{RateLimit: want})
	if err != nil {
		t.Fatal(err)
	}
	var got Config
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.RateLimit != want {
		t.Fatalf("RateLimit config round trip = %+v, want %+v", got.RateLimit, want)
	}
}

func TestReadOnlyDiagnosticsConfigDoesNotCreateConfig(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "missing-config-home")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", root)

	cfg, path, err := ReadOnlyDiagnosticsConfig()
	if err != nil {
		t.Fatalf("ReadOnlyDiagnosticsConfig() error = %v", err)
	}
	if path != filepath.Join(configHome, "duckduckgo-chat-cli", "config.json") {
		t.Fatalf("path = %q, want config location under XDG_CONFIG_HOME", path)
	}
	if cfg.DefaultModel == "" {
		t.Fatal("default model is empty")
	}
	if _, err := os.Stat(configHome); !os.IsNotExist(err) {
		t.Fatalf("diagnostics created config home: stat error = %v", err)
	}
}

func TestReadOnlyDiagnosticsConfigReadsExistingConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"tos_accepted":true,"default_model":"gpt-5.6-luna"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	cfg, path, err := ReadOnlyDiagnosticsConfig()
	if err != nil {
		t.Fatalf("ReadOnlyDiagnosticsConfig() error = %v", err)
	}
	if path != configPath || !cfg.TOSAccepted || cfg.DefaultModel != "gpt-5.6-luna" {
		t.Fatalf("ReadOnlyDiagnosticsConfig() = (%+v, %q), unexpected config", cfg, path)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("diagnostics changed the config file")
	}
}

func TestFrameAppearanceDefaultsAndJSONSaveLoad(t *testing.T) {
	defaults := defaultConfig().Appearance
	wantDefaults := AppearanceConfig{
		Theme:           "official",
		FrameResponses:  true,
		FrameWidthMode:  ui.FrameWidthMaximum,
		FrameMaxWidth:   112,
		FrameLineWeight: ui.FrameLineSingle,
	}
	if defaults != wantDefaults {
		t.Fatalf("default appearance = %+v, want %+v", defaults, wantDefaults)
	}

	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	want := AppearanceConfig{
		Theme:           "nord",
		FrameResponses:  false,
		FrameWidthMode:  ui.FrameWidthFull,
		FrameMaxWidth:   144,
		FrameLineWeight: ui.FrameLineHeavy,
	}
	if err := SaveConfig(&Config{Appearance: want}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	if got := loadConfig().Appearance; got != want {
		t.Fatalf("saved appearance = %+v, want %+v", got, want)
	}
}

func TestInitializeMigratesMissingFrameAppearanceFields(t *testing.T) {
	prior := ui.CurrentFrameOptions()
	t.Cleanup(func() { ui.SetFrameOptions(prior) })
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	configDir := filepath.Join(root, "duckduckgo-chat-cli")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"appearance":{"theme":"nord","frame_responses":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got := Initialize()
	want := AppearanceConfig{Theme: "nord", FrameResponses: false, FrameWidthMode: ui.FrameWidthMaximum, FrameMaxWidth: 112, FrameLineWeight: ui.FrameLineSingle}
	if got.Appearance != want {
		t.Fatalf("legacy appearance = %+v, want %+v", got.Appearance, want)
	}
	if gotOptions, wantOptions := ui.CurrentFrameOptions(), (ui.FrameOptions{WidthMode: ui.FrameWidthMaximum, MaxWidth: 112, LineWeight: ui.FrameLineSingle}); gotOptions != wantOptions {
		t.Fatalf("startup frame options = %+v, want %+v", gotOptions, wantOptions)
	}
}

func TestNormalizeAppearanceKeepsThemeAndNormalizesFrameSettingsIndependently(t *testing.T) {
	appearance := AppearanceConfig{Theme: "nord", FrameWidthMode: "unknown", FrameMaxWidth: 111, FrameLineWeight: "unknown"}
	normalizeAppearanceConfig(&appearance)
	want := AppearanceConfig{Theme: "nord", FrameWidthMode: ui.FrameWidthMaximum, FrameMaxWidth: 112, FrameLineWeight: ui.FrameLineSingle}
	if appearance != want {
		t.Fatalf("normalized appearance = %+v, want %+v", appearance, want)
	}

	appearance = AppearanceConfig{Theme: "unknown", FrameWidthMode: ui.FrameWidthFull, FrameMaxWidth: 144, FrameLineWeight: ui.FrameLineHeavy}
	normalizeAppearanceConfig(&appearance)
	if appearance.Theme != "official" || appearance.FrameWidthMode != ui.FrameWidthFull || appearance.FrameMaxWidth != 144 || appearance.FrameLineWeight != ui.FrameLineHeavy {
		t.Fatalf("invalid theme prevented valid frame settings from normalizing: %+v", appearance)
	}
}

func TestFrameWidthChoicesExposeFullAndSupportedMaximums(t *testing.T) {
	choices := frameWidthOptions(ui.FrameOptions{WidthMode: ui.FrameWidthFull, MaxWidth: 112, LineWeight: ui.FrameLineSingle})
	wantLabels := []string{"Full terminal", "Maximum: 80", "Maximum: 96", "Maximum: 112", "Maximum: 128", "Maximum: 144", "Maximum: 160"}
	labels := make([]string, len(choices))
	for index, choice := range choices {
		labels[index] = choice.label
	}
	if !reflect.DeepEqual(labels, wantLabels) {
		t.Fatalf("frame width choices = %v, want %v", labels, wantLabels)
	}
	if choices[0].options.WidthMode != ui.FrameWidthFull || choices[5].options.MaxWidth != 144 {
		t.Fatalf("frame width choices lost their values: %+v", choices)
	}
}

func TestFrameLineWeightChoicesExposeSingleAndHeavy(t *testing.T) {
	choices := frameLineWeightOptions()
	if len(choices) != 2 || choices[0] != (frameLineWeightChoice{label: "Single", weight: ui.FrameLineSingle}) || choices[1] != (frameLineWeightChoice{label: "Heavy", weight: ui.FrameLineHeavy}) {
		t.Fatalf("frame line weight choices = %+v, want Single and Heavy", choices)
	}
}

func TestSaveFrameOptionsRestoresConfigAndUIWhenPersistenceFails(t *testing.T) {
	prior := ui.CurrentFrameOptions()
	t.Cleanup(func() { ui.SetFrameOptions(prior) })
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", root)
	if err := os.WriteFile(filepath.Join(root, "duckduckgo-chat-cli"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := AppearanceConfig{Theme: "official", FrameResponses: true, FrameWidthMode: ui.FrameWidthMaximum, FrameMaxWidth: 96, FrameLineWeight: ui.FrameLineHeavy}
	cfg := &Config{Appearance: previous}
	ui.SetFrameOptions(ui.FrameOptions{WidthMode: previous.FrameWidthMode, MaxWidth: previous.FrameMaxWidth, LineWeight: previous.FrameLineWeight})

	if err := saveFrameOptions(cfg, ui.FrameOptions{WidthMode: ui.FrameWidthFull, MaxWidth: 160, LineWeight: ui.FrameLineSingle}); err == nil {
		t.Fatal("saveFrameOptions() succeeded despite an unwritable config path")
	}
	if cfg.Appearance != previous {
		t.Fatalf("failed save left appearance = %+v, want %+v", cfg.Appearance, previous)
	}
	wantUI := ui.FrameOptions{WidthMode: ui.FrameWidthMaximum, MaxWidth: 96, LineWeight: ui.FrameLineHeavy}
	if got := ui.CurrentFrameOptions(); got != wantUI {
		t.Fatalf("failed save left UI options = %+v, want %+v", got, wantUI)
	}
}

func TestConfigurationSelectorTemplatesUseSharedWidthAndLineWeight(t *testing.T) {
	prior := ui.CurrentFrameOptions()
	t.Cleanup(func() { ui.SetFrameOptions(prior) })
	t.Setenv("TERM", "xterm-256color")

	ui.SetFrameOptions(ui.FrameOptions{WidthMode: ui.FrameWidthMaximum, MaxWidth: 80, LineWeight: ui.FrameLineHeavy})
	selectTemplate, offsetTemplate, width := configurationSelectTemplatesAtWidth("Configuration", 160, 160)
	if width != 80 {
		t.Fatalf("maximum-width configuration menu = %d columns, want 80", width)
	}
	if !strings.Contains(selectTemplate, "┏━") || !strings.Contains(selectTemplate, "┃") || !strings.Contains(selectTemplate, "┗") {
		t.Errorf("configuration selector template does not use heavy border glyphs: %q", selectTemplate)
	}
	if !strings.Contains(offsetTemplate, "┃") {
		t.Errorf("configuration selector cursor-offset template does not use heavy vertical glyphs: %q", offsetTemplate)
	}

	ui.SetFrameOptions(ui.FrameOptions{WidthMode: ui.FrameWidthFull, MaxWidth: 112, LineWeight: ui.FrameLineSingle})
	_, _, width = configurationSelectTemplatesAtWidth("Configuration", 160, 160)
	if width != 160 {
		t.Fatalf("full-width configuration menu = %d columns, want 160", width)
	}
}

func TestConfigurationFrameSummaryUsesSharedGlyphsAndWidth(t *testing.T) {
	prior := ui.CurrentFrameOptions()
	t.Cleanup(func() { ui.SetFrameOptions(prior) })
	t.Setenv("TERM", "xterm-256color")
	ui.SetFrameOptions(ui.FrameOptions{WidthMode: ui.FrameWidthMaximum, MaxWidth: 80, LineWeight: ui.FrameLineHeavy})

	framed := configurationFrameLines([]string{"Theme: Nord"}, 40)
	rows := strings.Split(strings.TrimSpace(framed), "\n")
	if len(rows) != 1 {
		t.Fatalf("configuration summary has %d rows, want 1: %q", len(rows), framed)
	}
	if got := ansi.StringWidth(rows[0]); got != 40 {
		t.Fatalf("configuration summary row width = %d, want 40: %q", got, rows[0])
	}
	if !strings.HasPrefix(rows[0], "┃ ") || !strings.HasSuffix(rows[0], " ┃") {
		t.Fatalf("configuration summary does not use heavy border glyphs: %q", rows[0])
	}
}

func TestConfigurationSelectorUsesPlainFallbackOnVeryNarrowTerminal(t *testing.T) {
	prior := ui.CurrentFrameOptions()
	t.Cleanup(func() { ui.SetFrameOptions(prior) })
	ui.SetFrameOptions(ui.FrameOptions{WidthMode: ui.FrameWidthFull, MaxWidth: 112, LineWeight: ui.FrameLineSingle})

	selectTemplate, offsetTemplate, width := configurationSelectTemplatesAtWidth("Configuration", 20, 20)
	if width != 20 || selectTemplate != "" || offsetTemplate != "" {
		t.Fatalf("narrow selector = (%q, %q, %d), want no frame templates at width 20", selectTemplate, offsetTemplate, width)
	}
}

func TestConfigurationSelectorTemplateKeepsLongRowsInsideFrame(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	width := 24
	prompt := &survey.Select{
		Message: "Choose an Appearance · Frame line weight setting",
		Options: []string{"Appearance · Frame line weight", strings.Repeat("界", 40)},
	}
	prompt.FilterMessage = " a-long-filter-with-wide-text-界界界"
	restore := ui.FitFramedSelectPrompt(prompt, width)
	defer restore(nil)
	prompt.Help = configurationFrameLines([]string{strings.Repeat("summary界", 12)}, width)

	selectTemplate, _, _ := configurationSelectTemplatesAtWidth("Configuration · Appearance · Frame line weight", width, width)
	data := survey.SelectTemplateData{
		Select:        *prompt,
		PageEntries:   core.OptionAnswerList(prompt.Options),
		SelectedIndex: 0,
		Description:   prompt.Description,
		Config:        &survey.PromptConfig{},
	}
	for _, showAnswer := range []bool{false, true} {
		data.ShowAnswer = showAnswer
		if showAnswer {
			data.Answer = prompt.Options[0]
		}
		output, _, err := core.RunTemplate(selectTemplate, data)
		if err != nil {
			t.Fatalf("render configuration selector template: %v", err)
		}
		for index, row := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
			if got := ansi.StringWidth(row); got != width {
				t.Errorf("answer=%v row %d width = %d, want %d: %q", showAnswer, index+1, got, width, row)
			}
		}
	}
}
