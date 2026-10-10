package browserrelay

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in smoke check for the bundled extension in an isolated Chromium profile.
// It sends an empty chat payload, so a 400 response is sufficient to prove the
// reset, fresh-page proof capture, and relay reached Duck.ai.
func TestLiveBundledChromeExtensionConnects(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_EXTENSION_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_EXTENSION_TEST=1 to exercise the bundled extension")
	}
	executable, err := exec.LookPath("chromium")
	if err != nil {
		t.Skipf("Chromium is unavailable: %v", err)
	}
	extension, err := filepath.Abs("../../browser-extension/duckchat-relay")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var browser *exec.Cmd
	profile := t.TempDir()
	relay := New(Options{WaitTimeout: 25 * time.Second, OpenBrowser: func(target string) error {
		browser = exec.CommandContext(ctx, executable,
			"--user-data-dir="+profile,
			"--disable-extensions-except="+extension,
			"--load-extension="+extension,
			"--no-first-run", "--no-default-browser-check", "--no-sandbox",
			"--new-window", target,
		)
		browser.Stdout, browser.Stderr = io.Discard, io.Discard
		return browser.Start()
	}})
	defer func() {
		cancel()
		if browser != nil {
			_ = browser.Wait()
		}
	}()
	response, _, err := relay.Do(ctx, Request{
		URL: "https://duck.ai/duckchat/v1/chat", Method: http.MethodPost,
		Header: http.Header{"Accept": {"text/event-stream"}, "Content-Type": {"application/json"}},
		Body:   []byte("{}"),
	})
	if err != nil {
		t.Fatalf("bundled extension did not complete the Duck.ai request: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	t.Logf("bundled Chrome extension returned HTTP %d", response.StatusCode)
}

// Opt-in check for an extension already installed in the user's default
// Chrome profile. It clears Duck.ai site data in that profile.
func TestLiveDefaultChromeExtensionCompletesRequest(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_DEFAULT_CHROME_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_DEFAULT_CHROME_TEST=1 to exercise the installed default-browser extension")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	response, _, err := New(Options{}).Do(ctx, Request{
		URL: "https://duck.ai/duckchat/v1/chat", Method: http.MethodPost,
		Header: http.Header{"Accept": {"text/event-stream"}, "Content-Type": {"application/json"}},
		Body:   []byte("{}"),
	})
	if err != nil {
		t.Fatalf("installed Chrome extension did not complete the Duck.ai request: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	t.Logf("default Chrome extension returned HTTP %d", response.StatusCode)
}
