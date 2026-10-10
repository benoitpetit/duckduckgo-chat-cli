package browserrelay

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Keep the unpacked extension's ID stable for existing Chrome installations.
const bundledExtensionID = "jfoajifaebegcppnbccjipgbpbnmmdmo"

func TestExtensionManifestRestrictsBrowserAndLoopbackPermissions(t *testing.T) {
	data, err := os.ReadFile("../../browser-extension/duckchat-relay/manifest.json")
	if err != nil {
		t.Fatalf("read relay extension manifest: %v", err)
	}
	var manifest struct {
		ManifestVersion int      `json:"manifest_version"`
		Key             string   `json:"key"`
		Permissions     []string `json:"permissions"`
		HostPermissions []string `json:"host_permissions"`
		Background      struct {
			ServiceWorker string `json:"service_worker"`
		} `json:"background"`
		ContentScripts []struct {
			Matches []string `json:"matches"`
			JS      []string `json:"js"`
			RunAt   string   `json:"run_at"`
			World   string   `json:"world"`
		} `json:"content_scripts"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode relay extension manifest: %v", err)
	}
	if manifest.ManifestVersion != 3 {
		t.Fatalf("manifest version = %d, want 3", manifest.ManifestVersion)
	}
	publicKey, err := base64.StdEncoding.DecodeString(manifest.Key)
	if err != nil {
		t.Fatalf("decode extension public key: %v", err)
	}
	hash := sha256.Sum256(publicKey)
	derivedID := make([]byte, 32)
	for i := 0; i < 16; i++ {
		derivedID[2*i], derivedID[2*i+1] = 'a'+(hash[i]>>4), 'a'+(hash[i]&0x0f)
	}
	if string(derivedID) != bundledExtensionID {
		t.Fatalf("extension ID = %s, existing installations use %s", derivedID, bundledExtensionID)
	}
	if !reflect.DeepEqual(manifest.Permissions, []string{"scripting", "browsingData"}) {
		t.Fatalf("extension permissions = %v, want scripting and site-data reset only", manifest.Permissions)
	}
	if !reflect.DeepEqual(manifest.HostPermissions, []string{"https://duck.ai/*", "http://127.0.0.1/*"}) {
		t.Fatalf("host permissions = %v, want Duck.ai and loopback only", manifest.HostPermissions)
	}
	if manifest.Background.ServiceWorker != "background.js" {
		t.Fatalf("service worker = %q, want background.js", manifest.Background.ServiceWorker)
	}
	if len(manifest.ContentScripts) != 1 || !reflect.DeepEqual(manifest.ContentScripts[0].Matches, []string{"https://duck.ai/*"}) || !reflect.DeepEqual(manifest.ContentScripts[0].JS, []string{"content.js"}) || manifest.ContentScripts[0].RunAt != "document_start" {
		t.Fatalf("content scripts = %+v, want one Duck.ai document_start script", manifest.ContentScripts)
	}
	background, err := os.ReadFile("../../browser-extension/duckchat-relay/background.js")
	if err != nil {
		t.Fatalf("read relay service worker: %v", err)
	}
	content, err := os.ReadFile("../../browser-extension/duckchat-relay/content.js")
	if err != nil {
		t.Fatalf("read relay content script: %v", err)
	}
	workerSource, contentSource := string(background), string(content)
	if strings.Contains(workerSource, "chrome.cookies") || strings.Contains(contentSource, "chrome.cookies") {
		t.Fatal("extension must not read Chrome's cookies API")
	}
	if !strings.Contains(workerSource, `origins: ["https://duck.ai"]`) {
		t.Fatal("extension must restrict site-data reset to Duck.ai")
	}
	for _, required := range []string{
		"/v1/next", "/v1/ping", "/v1/start", "/v1/chunk", "/v1/finish", "/v1/error",
		"job?.method", "job.id", "job.url", "job.headers", "job.body",
		"relayURL.origin", `credentials: "include"`,
	} {
		if !strings.Contains(workerSource+contentSource, required) {
			t.Errorf("extension source is missing required protocol/security detail %q", required)
		}
	}
}
