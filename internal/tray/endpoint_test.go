package tray

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateEndpointIsPrivateAndExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tray-service.json")
	endpoint := serviceEndpoint{Address: "127.0.0.1:12345", Token: strings.Repeat("a", 64), PID: 123}
	if err := createEndpoint(path, endpoint); err != nil {
		t.Fatalf("createEndpoint() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("endpoint permissions = %o, want 600", info.Mode().Perm())
	}
	if err := createEndpoint(path, endpoint); !os.IsExist(err) {
		t.Fatalf("second createEndpoint() error = %v, want file exists", err)
	}
}

func TestRemoveStaleEndpointButKeepLiveEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tray-service.json")
	endpoint := serviceEndpoint{Address: "127.0.0.1:12345", Token: strings.Repeat("b", 64), PID: 123}
	if err := createEndpoint(path, endpoint); err != nil {
		t.Fatal(err)
	}
	removed, err := removeStaleEndpointAt(path, func(pid int) bool { return pid == 123 })
	if err != nil || removed {
		t.Fatalf("remove live endpoint = (%v, %v), want (false, nil)", removed, err)
	}
	removed, err = removeStaleEndpointAt(path, func(int) bool { return false })
	if err != nil || !removed {
		t.Fatalf("remove stale endpoint = (%v, %v), want (true, nil)", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stale endpoint still exists, stat error = %v", err)
	}
}
