package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeProofBrowser struct {
	mu        sync.Mutex
	active    int
	maxActive int
	calls     int
	closed    int
	gate      <-chan struct{}
	started   chan<- struct{}
	errors    []error
}

func (b *fakeProofBrowser) Capture(ctx context.Context) (*DynamicHeaders, error) {
	b.mu.Lock()
	b.active++
	b.calls++
	if b.active > b.maxActive {
		b.maxActive = b.active
	}
	call := b.calls
	var err error
	if len(b.errors) > 0 {
		err = b.errors[0]
		b.errors = b.errors[1:]
	}
	gate := b.gate
	started := b.started
	b.mu.Unlock()

	if started != nil {
		started <- struct{}{}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			b.mu.Lock()
			b.active--
			b.mu.Unlock()
			return nil, ctx.Err()
		}
	}

	b.mu.Lock()
	b.active--
	b.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &DynamicHeaders{VqdHash1: "proof-" + string(rune('0'+call))}, nil
}

func (b *fakeProofBrowser) Close() error {
	b.mu.Lock()
	b.closed++
	b.mu.Unlock()
	return nil
}

func (b *fakeProofBrowser) stats() (calls, maxActive, closed int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls, b.maxActive, b.closed
}

func TestNormalBrowserUserAgentKeepsInstalledVersion(t *testing.T) {
	const headless = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/152.0.7977.82 Safari/537.36"
	const want = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.7977.82 Safari/537.36"
	if got := normalBrowserUserAgent(headless); got != want {
		t.Fatalf("normalBrowserUserAgent() = %q, want %q", got, want)
	}
	if got := normalBrowserUserAgent(want); got != want {
		t.Fatalf("normalBrowserUserAgent() changed regular Chrome UA to %q", got)
	}
}

func TestBrowserManagerLazilyReusesBrowserAndCapturesFreshProofs(t *testing.T) {
	browser := &fakeProofBrowser{}
	factoryCalls := 0
	manager := newBrowserManager(func(context.Context) (proofBrowser, error) {
		factoryCalls++
		return browser, nil
	})

	if factoryCalls != 0 {
		t.Fatal("browser factory ran before the first capture")
	}
	first, err := manager.Capture(context.Background())
	if err != nil {
		t.Fatalf("first Capture() error = %v", err)
	}
	second, err := manager.Capture(context.Background())
	if err != nil {
		t.Fatalf("second Capture() error = %v", err)
	}
	if factoryCalls != 1 {
		t.Fatalf("browser factory calls = %d, want 1", factoryCalls)
	}
	if first.VqdHash1 != "proof-1" || second.VqdHash1 != "proof-2" {
		t.Fatalf("captured proofs = %q, %q; want fresh proofs proof-1, proof-2", first.VqdHash1, second.VqdHash1)
	}
}

func TestBrowserManagerSerializesConcurrentCaptures(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 2)
	browser := &fakeProofBrowser{gate: gate, started: started}
	manager := newBrowserManager(func(context.Context) (proofBrowser, error) { return browser, nil })

	results := make(chan error, 2)
	go func() {
		_, err := manager.Capture(context.Background())
		results <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first capture did not start")
	}
	secondCalling := make(chan struct{})
	go func() {
		close(secondCalling)
		_, err := manager.Capture(context.Background())
		results <- err
	}()
	<-secondCalling
	select {
	case <-started:
		t.Fatal("second browser capture started before the first finished")
	case <-time.After(25 * time.Millisecond):
	}
	close(gate)
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("Capture() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent captures did not finish")
		}
	}
	_, maxActive, _ := browser.stats()
	if maxActive != 1 {
		t.Fatalf("maximum concurrent browser captures = %d, want 1", maxActive)
	}
}

func TestBrowserManagerCallerCancellationKeepsBrowserUsable(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	browser := &fakeProofBrowser{gate: gate, started: started}
	factoryCalls := 0
	manager := newBrowserManager(func(context.Context) (proofBrowser, error) {
		factoryCalls++
		return browser, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := manager.Capture(ctx)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("capture did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Capture() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled capture did not return")
	}

	close(gate)
	if _, err := manager.Capture(context.Background()); err != nil {
		t.Fatalf("Capture() after caller cancellation error = %v", err)
	}
	if factoryCalls != 1 {
		t.Fatalf("browser factory calls = %d, want same browser retained", factoryCalls)
	}
	_, _, closed := browser.stats()
	if closed != 0 {
		t.Fatalf("browser closed after caller cancellation %d times, want 0", closed)
	}
}

func TestBrowserManagerFailedCaptureResetsBrowser(t *testing.T) {
	first := &fakeProofBrowser{errors: []error{errors.New("browser disconnected")}}
	second := &fakeProofBrowser{}
	factoryCalls := 0
	manager := newBrowserManager(func(context.Context) (proofBrowser, error) {
		factoryCalls++
		if factoryCalls == 1 {
			return first, nil
		}
		return second, nil
	})

	if _, err := manager.Capture(context.Background()); err == nil {
		t.Fatal("Capture() unexpectedly succeeded after browser failure")
	}
	if _, err := manager.Capture(context.Background()); err != nil {
		t.Fatalf("Capture() after browser failure error = %v", err)
	}
	if factoryCalls != 2 {
		t.Fatalf("browser factory calls = %d, want replacement after failure", factoryCalls)
	}
	_, _, closed := first.stats()
	if closed != 1 {
		t.Fatalf("failed browser Close() calls = %d, want 1", closed)
	}
}

func TestBrowserManagerCloseIsIdempotentAndAllowsRestart(t *testing.T) {
	first := &fakeProofBrowser{}
	second := &fakeProofBrowser{}
	factoryCalls := 0
	manager := newBrowserManager(func(context.Context) (proofBrowser, error) {
		factoryCalls++
		if factoryCalls == 1 {
			return first, nil
		}
		return second, nil
	})

	if _, err := manager.Capture(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if _, err := manager.Capture(context.Background()); err != nil {
		t.Fatalf("Capture() after Close() error = %v", err)
	}
	_, _, firstClosed := first.stats()
	if firstClosed != 1 {
		t.Fatalf("first browser Close() calls = %d, want 1", firstClosed)
	}
	if factoryCalls != 2 {
		t.Fatalf("browser factory calls = %d, want a replacement after Close()", factoryCalls)
	}
}

func TestBrowserManagerBoundsBrowserStartupAndCancelsItWithCaller(t *testing.T) {
	type startupInfo struct {
		hasDeadline bool
		remaining   time.Duration
	}
	started := make(chan startupInfo, 1)
	manager := newBrowserManager(func(ctx context.Context) (proofBrowser, error) {
		deadline, ok := ctx.Deadline()
		info := startupInfo{hasDeadline: ok}
		if ok {
			info.remaining = time.Until(deadline)
		}
		started <- info
		<-ctx.Done()
		return nil, ctx.Err()
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := manager.Capture(ctx)
		result <- err
	}()
	select {
	case info := <-started:
		if !info.hasDeadline || info.remaining <= 0 || info.remaining > 60*time.Second {
			t.Fatalf("browser startup deadline = %+v, want a positive deadline no more than 60s away", info)
		}
	case <-time.After(time.Second):
		t.Fatal("browser factory did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Capture() error after caller cancellation = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not stop browser startup")
	}
}

func TestGetCurrentDuckAIHeadersUsesSharedBrowserManager(t *testing.T) {
	browser := &fakeProofBrowser{}
	factoryCalls := 0
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		factoryCalls++
		return browser, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	first, err := getCurrentDuckAIHeaders(context.Background())
	if err != nil {
		t.Fatalf("first getCurrentDuckAIHeaders() error = %v", err)
	}
	second, err := getCurrentDuckAIHeaders(context.Background())
	if err != nil {
		t.Fatalf("second getCurrentDuckAIHeaders() error = %v", err)
	}
	if first.VqdHash1 != "proof-1" || second.VqdHash1 != "proof-2" {
		t.Fatalf("proofs = %q, %q; want fresh proof-1 and proof-2", first.VqdHash1, second.VqdHash1)
	}
	if factoryCalls != 1 {
		t.Fatalf("shared browser factory calls = %d, want 1", factoryCalls)
	}
}

func TestShutdownBrowserClosesSharedManagerAndAllowsRestart(t *testing.T) {
	first := &fakeProofBrowser{}
	second := &fakeProofBrowser{}
	factoryCalls := 0
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		factoryCalls++
		if factoryCalls == 1 {
			return first, nil
		}
		return second, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	if _, err := getCurrentDuckAIHeaders(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ShutdownBrowser(); err != nil {
		t.Fatalf("ShutdownBrowser() error = %v", err)
	}
	if err := ShutdownBrowser(); err != nil {
		t.Fatalf("second ShutdownBrowser() error = %v", err)
	}
	if _, err := getCurrentDuckAIHeaders(context.Background()); err != nil {
		t.Fatalf("getCurrentDuckAIHeaders() after shutdown error = %v", err)
	}
	_, _, firstClosed := first.stats()
	if firstClosed != 1 {
		t.Fatalf("browser Close() calls = %d, want 1", firstClosed)
	}
	if factoryCalls != 2 {
		t.Fatalf("browser factory calls = %d, want replacement after shutdown", factoryCalls)
	}
}

func TestLiveProofCaptureReusesBrowserAndRefreshesHeaders(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_TEST=1 to capture live Duck.ai proofs")
	}
	t.Cleanup(func() {
		if err := ShutdownBrowser(); err != nil {
			t.Errorf("ShutdownBrowser() error = %v", err)
		}
	})

	started := time.Now()
	first, err := getCurrentDuckAIHeaders(context.Background())
	coldDuration := time.Since(started)
	if err != nil {
		t.Fatalf("first live proof capture error = %v", err)
	}
	if first.VqdHash1 == "" {
		t.Fatal("first live proof capture returned an empty X-Vqd-Hash-1")
	}

	started = time.Now()
	second, err := getCurrentDuckAIHeaders(context.Background())
	warmDuration := time.Since(started)
	if err != nil {
		t.Fatalf("second live proof capture error = %v", err)
	}
	if second.VqdHash1 == "" {
		t.Fatal("second live proof capture returned an empty X-Vqd-Hash-1")
	}
	t.Logf("live proof capture durations: cold=%s warm=%s", coldDuration.Round(time.Millisecond), warmDuration.Round(time.Millisecond))
}

type fixedHeadersProofBrowser struct {
	headers *DynamicHeaders
}

func (b *fixedHeadersProofBrowser) Capture(context.Context) (*DynamicHeaders, error) {
	copy := *b.headers
	return &copy, nil
}

func (*fixedHeadersProofBrowser) Close() error { return nil }

func TestCaptureDynamicHeadersDelegatesToSharedManager(t *testing.T) {
	want := &DynamicHeaders{
		VqdHash1:  "proof-from-shared-browser",
		FeSignals: "signals-from-shared-browser",
		FeVersion: "version-from-shared-browser",
		UserAgent: "DuckAI test browser",
	}
	previous := sharedDuckAIBrowser
	manager := newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &fixedHeadersProofBrowser{headers: want}, nil
	})
	sharedDuckAIBrowser = manager
	t.Cleanup(func() {
		_ = manager.Close()
		sharedDuckAIBrowser = previous
	})

	got, err := CaptureDynamicHeaders(context.Background())
	if err != nil {
		t.Fatalf("CaptureDynamicHeaders() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CaptureDynamicHeaders() = %+v, want %+v", got, want)
	}
}

func TestBrowserExecutableFindsSupportedChromium(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "chromium")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got, err := BrowserExecutable()
	if err != nil {
		t.Fatalf("BrowserExecutable() error = %v", err)
	}
	if got != want {
		t.Fatalf("BrowserExecutable() = %q, want %q", got, want)
	}
}

func TestBrowserExecutableReportsMissingBrowser(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if got, err := BrowserExecutable(); err == nil {
		t.Fatalf("BrowserExecutable() = %q, want an error", got)
	}
}
