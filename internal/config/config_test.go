package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
