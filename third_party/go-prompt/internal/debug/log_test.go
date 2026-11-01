package debug

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenLogFileUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "go-prompt.log")
	if err := os.WriteFile(path, []byte("existing log"), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("debug log mode = %#o, want 0600", got)
	}
}
