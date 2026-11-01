package update

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
	}{
		{left: "1.5.1", right: "1.5.0", want: 1},
		{left: "v1.5.0", right: "1.5.0", want: 0},
		{left: "1.4.9", right: "1.5.0", want: -1},
	}
	for _, tt := range tests {
		got, err := compareVersions(tt.left, tt.right)
		if err != nil || got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, %v; want %d", tt.left, tt.right, got, err, tt.want)
		}
	}
}

func TestVersionNeedsUpdateDoesNotDowngrade(t *testing.T) {
	if versionNeedsUpdate("1.6.0", "1.5.1") {
		t.Fatal("updater should not downgrade a newer local version")
	}
	if !versionNeedsUpdate("dev", "1.5.1") {
		t.Fatal("development builds should check for updates")
	}
}

func TestValidateUpdateBinaryName(t *testing.T) {
	tests := []struct {
		name    string
		invalid bool
	}{
		{name: "duckduckgo-chat-cli_v1.2.3_linux_amd64"},
		{name: "duckduckgo-chat-cli_v1.2.3_windows_amd64.exe"},
		{name: "", invalid: true},
		{name: "../duckchat", invalid: true},
		{name: `..\duckchat.exe`, invalid: true},
		{name: "/tmp/duckchat", invalid: true},
		{name: "C:duckchat.exe", invalid: true},
		{name: ".", invalid: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateUpdateBinaryName(test.name)
			if (err != nil) != test.invalid {
				t.Fatalf("validateUpdateBinaryName(%q) error = %v, invalid = %t", test.name, err, test.invalid)
			}
		})
	}
}

func TestDownloadAndVerifyRejectsUnsafeBinaryNameBeforeNetwork(t *testing.T) {
	_, err := DownloadAndVerify(&UpdateInfo{BinaryName: "../escaped"})
	if err == nil || !strings.Contains(err.Error(), "invalid update binary name") {
		t.Fatalf("DownloadAndVerify() error = %v, want invalid-name error before network", err)
	}
}

func TestUpdateCachePathUsesPerUserCache(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	got, err := updateCachePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cacheHome, "duckduckgo-chat-cli", UpdateCache)
	if got != want {
		t.Fatalf("updateCachePath() = %q, want %q", got, want)
	}
}

func TestWriteUpdateCheckCacheUsesPrivateDirectoryAndFile(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "duckduckgo-chat-cli")
	if err := os.Mkdir(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cacheDir, UpdateCache)
	now := time.Date(2026, time.October, 31, 12, 34, 56, 0, time.UTC)
	if err := writeUpdateCheckCache(cachePath, now); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("cache file mode = %#o, want 0600", got)
	}
	directoryInfo, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("cache directory mode = %#o, want 0700", got)
	}
	if got, err := os.ReadFile(cachePath); err != nil || string(got) != now.Format(time.RFC3339) {
		t.Fatalf("cache contents = %q, %v; want %q", got, err, now.Format(time.RFC3339))
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != UpdateCache {
		t.Fatalf("cache directory entries = %v, want only %q", entries, UpdateCache)
	}
}

func TestShouldCheckForUpdatesRejectsSymlinkCache(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	cachePath, err := updateCachePath()
	if err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, cachePath); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if !ShouldCheckForUpdates() {
		t.Fatal("a symlink cache must trigger an update check")
	}
	if err := UpdateLastCheckTime(); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "sentinel" {
		t.Fatalf("symlink target changed: contents=%q err=%v", got, err)
	}
	info, err := os.Lstat(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("cache symlink was not replaced by a regular file")
	}
}

func TestShouldCheckForUpdatesUsesCacheAgeAndPrivacy(t *testing.T) {
	cacheHome := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	cachePath, err := updateCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if !ShouldCheckForUpdates() {
		t.Fatal("missing cache should trigger an update check")
	}
	if err := writeUpdateCheckCache(cachePath, time.Now()); err != nil {
		t.Fatal(err)
	}
	if ShouldCheckForUpdates() {
		t.Fatal("fresh private cache should avoid an update check")
	}
	if err := os.Chmod(cachePath, 0o644); err != nil {
		t.Fatal(err)
	}
	if !ShouldCheckForUpdates() {
		t.Fatal("cache with public permissions should trigger an update check")
	}
	if err := os.Chmod(cachePath, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-CheckInterval - time.Minute)
	if err := os.Chtimes(cachePath, old, old); err != nil {
		t.Fatal(err)
	}
	if !ShouldCheckForUpdates() {
		t.Fatal("stale cache should trigger an update check")
	}
}

func TestWriteUpdateCheckCacheRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "duckduckgo-chat-cli")
	targetDir := t.TempDir()
	if err := os.Chmod(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetDir, cacheDir); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	cachePath := filepath.Join(cacheDir, UpdateCache)
	if err := writeUpdateCheckCache(cachePath, time.Now()); err == nil {
		t.Fatal("writeUpdateCheckCache() succeeded through a symlink directory")
	}
	info, err := os.Stat(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("symlink target directory mode = %#o, want unchanged 0755", got)
	}
	if _, err := os.Stat(filepath.Join(targetDir, UpdateCache)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cache was written into symlink target: %v", err)
	}
}

func TestUpdateLastCheckTimeReturnsCachePathErrors(t *testing.T) {
	cacheHome := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(cacheHome, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	if err := UpdateLastCheckTime(); err == nil {
		t.Fatal("UpdateLastCheckTime() succeeded with a file as the cache directory")
	}
}

type closeErrorReader struct {
	io.Reader
	err error
}

func (r closeErrorReader) Close() error { return r.err }

type closeErrorWriter struct {
	bytes.Buffer
	err error
}

func (w *closeErrorWriter) Close() error { return w.err }

func TestCopyAndCloseReturnsBothCloseErrors(t *testing.T) {
	readCloseErr := errors.New("read close failed")
	writeCloseErr := errors.New("write close failed")
	source := closeErrorReader{Reader: strings.NewReader("update"), err: readCloseErr}
	destination := &closeErrorWriter{err: writeCloseErr}
	err := copyAndClose(destination, source)
	if !errors.Is(err, readCloseErr) || !errors.Is(err, writeCloseErr) {
		t.Fatalf("copyAndClose() error = %v, want both close errors", err)
	}
	if got := destination.String(); got != "update" {
		t.Fatalf("copied data = %q, want %q", got, "update")
	}
}
