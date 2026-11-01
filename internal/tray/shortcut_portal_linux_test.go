//go:build linux

package tray

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDesktopEntryKeepsDesktopReadableModes(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	if err := ensureDesktopEntry("/usr/bin/duckchat"); err != nil {
		t.Fatal(err)
	}
	applications := filepath.Join(dataHome, "applications")
	directoryInfo, err := os.Stat(applications)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o755 {
		t.Fatalf("desktop applications directory mode = %#o, want 0755", got)
	}
	entryPath := filepath.Join(applications, desktopApplicationID+".desktop")
	entryInfo, err := os.Stat(entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := entryInfo.Mode().Perm(); got != 0o644 {
		t.Fatalf("desktop entry mode = %#o, want 0644", got)
	}
}
