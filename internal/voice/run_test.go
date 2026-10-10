package voice

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewJourneyIDIsRandomOpaqueHex(t *testing.T) {
	first, err := newJourneyID()
	if err != nil {
		t.Fatalf("newJourneyID() error = %v", err)
	}
	second, err := newJourneyID()
	if err != nil {
		t.Fatalf("second newJourneyID() error = %v", err)
	}
	if len(first) != 32 || first == second {
		t.Fatalf("journey IDs = %q and %q, want distinct 32-character IDs", first, second)
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("journey ID is not hexadecimal: %v", err)
	}
}

type fakeBrowserSession struct {
	mu     sync.Mutex
	closed int
	done   chan struct{}
}

func (b *fakeBrowserSession) Close() error {
	b.mu.Lock()
	b.closed++
	b.mu.Unlock()
	return nil
}

func (b *fakeBrowserSession) closeCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *fakeBrowserSession) Done() <-chan struct{} {
	return b.done
}

type fakeBrowserOpener struct {
	opened  chan string
	session BrowserSession
	err     error
}

func (o *fakeBrowserOpener) Open(_ context.Context, address string) (BrowserSession, error) {
	o.opened <- address
	return o.session, o.err
}

type contextBlockingBrowserOpener struct {
	opened chan string
}

func (o *contextBlockingBrowserOpener) Open(ctx context.Context, address string) (BrowserSession, error) {
	o.opened <- address
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestVoiceAssetsAreEmbedded(t *testing.T) {
	handler := newTestHandler(t, &testProofProvider{}, &testSignalingClient{})
	for _, path := range []string{"/", "/app.js", "/logo.png"} {
		recorder := doVoiceRequest(handler, http.MethodGet, path, "", "", "", "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200", path, recorder.Code)
		}
		if recorder.Body.Len() == 0 {
			t.Fatalf("GET %s returned an empty asset", path)
		}
	}
}

func TestVoiceAssetsExposeSessionTerminationActions(t *testing.T) {
	page, err := webAssets.ReadFile("webui/index.html")
	if err != nil {
		t.Fatalf("read voice page asset: %v", err)
	}
	for _, expected := range []string{
		`id="termination-panel"`,
		`id="retry-button"`,
		`id="close-voice-button"`,
		"We ended this conversation due to inactivity. Would you like to try again?",
	} {
		if !strings.Contains(string(page), expected) {
			t.Errorf("voice page is missing session termination UI %q", expected)
		}
	}

	script, err := webAssets.ReadFile("webui/app.js")
	if err != nil {
		t.Fatalf("read voice script asset: %v", err)
	}
	for _, expected := range []string{
		`event.item.name === "session_terminated"`,
		"handleSessionTermination()",
		`request("/api/session-terminated"`,
	} {
		if !strings.Contains(string(script), expected) {
			t.Errorf("voice script is missing session termination behavior %q", expected)
		}
	}
	if strings.Contains(string(script), "SESSION_TERMINATION_DISPLAY_MS") {
		t.Fatal("voice script still automatically closes after session termination")
	}

	style, err := webAssets.ReadFile("webui/style.css")
	if err != nil {
		t.Fatalf("read voice stylesheet asset: %v", err)
	}
	for _, expected := range []string{
		".termination-panel",
		".termination-actions",
	} {
		if !strings.Contains(string(style), expected) {
			t.Errorf("voice stylesheet is missing session termination styling %q", expected)
		}
	}
}

func TestRunBindsLoopbackAndClosesOnStop(t *testing.T) {
	session := &fakeBrowserSession{}
	opener := &fakeBrowserOpener{opened: make(chan string, 1), session: session}
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Dependencies{Proof: &testProofProvider{}, Signaling: &testSignalingClient{}}, opener)
	}()

	var address string
	select {
	case address = <-opener.opened:
	case err := <-done:
		t.Fatalf("Run() returned before opening browser: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("browser opener was not called")
	}
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatalf("parse browser URL: %v", err)
	}
	if parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" {
		t.Fatalf("browser URL = %q, want an ephemeral 127.0.0.1 address", address)
	}
	response, err := http.Get(address)
	if err != nil {
		t.Fatalf("local server did not serve the browser URL: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("local page status = %d, want 200", response.StatusCode)
	}

	if !StopActive() {
		t.Fatal("StopActive() = false, want active session stopped")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error after StopActive() = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not return after StopActive()")
	}
	if got := session.closeCount(); got != 1 {
		t.Fatalf("browser Close() calls = %d, want 1", got)
	}
	if _, err := http.Get(address); err == nil {
		t.Fatal("local server still accepted requests after Run() returned")
	}
}

func TestRunReturnsWhenBrowserOpenFails(t *testing.T) {
	opener := &fakeBrowserOpener{opened: make(chan string, 1), err: errors.New("browser unavailable")}
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Dependencies{Proof: &testProofProvider{}, Signaling: &testSignalingClient{}}, opener)
	}()

	var address string
	select {
	case address = <-opener.opened:
	case err := <-done:
		t.Fatalf("Run() returned before browser open request: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("browser opener was not called")
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "browser unavailable") {
			t.Fatalf("Run() error = %v, want browser startup error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not return after browser open failed")
	}
	if _, err := http.Get(address); err == nil {
		t.Fatal("local server remained available after browser open failed")
	}
}

func TestRunReturnsWhenBrowserWindowCloses(t *testing.T) {
	session := &fakeBrowserSession{done: make(chan struct{})}
	opener := &fakeBrowserOpener{opened: make(chan string, 1), session: session}
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Dependencies{Proof: &testProofProvider{}, Signaling: &testSignalingClient{}}, opener)
	}()

	var address string
	select {
	case address = <-opener.opened:
	case err := <-done:
		t.Fatalf("Run() returned before opening browser: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("browser opener was not called")
	}
	close(session.done)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error after browser closed = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not return after browser closed")
	}
	if got := session.closeCount(); got != 1 {
		t.Fatalf("browser Close() calls = %d, want 1", got)
	}
	if _, err := http.Get(address); err == nil {
		t.Fatal("local server still accepted requests after the browser closed")
	}
}

func TestRunStopsCleanlyWhileNativeWindowIsOpening(t *testing.T) {
	opener := &contextBlockingBrowserOpener{opened: make(chan string, 1)}
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Dependencies{Proof: &testProofProvider{}, Signaling: &testSignalingClient{}}, opener)
	}()

	select {
	case <-opener.opened:
	case err := <-done:
		t.Fatalf("Run() returned before opening the native window: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("native window opener was not called")
	}
	if !StopActive() {
		t.Fatal("StopActive() = false, want active voice launch stopped")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error after stopping while opening = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not stop while the native window was opening")
	}
}
