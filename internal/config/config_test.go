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
