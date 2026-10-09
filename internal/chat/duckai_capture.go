package chat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const (
	// duckAIProofCaptureTimeout bounds one capture end to end: queue wait,
	// Chrome startup, page load, and proof acquisition.
	duckAIProofCaptureTimeout = 60 * time.Second
	// browserCloseWait bounds shutdown so a wedged browser cannot stall exit.
	browserCloseWait = 5 * time.Second
)

type DynamicHeaders struct {
	FeSignals      string
	FeVersion      string
	VqdHash1       string
	UserAgent      string
	AcceptLanguage string
	JourneyID      string
	BrowserHeaders map[string]string
}

type proofBrowser interface {
	Capture(context.Context) (*DynamicHeaders, error)
	Close() error
}

type proofBrowserFactory func(context.Context) (proofBrowser, error)

// browserManager owns one proof browser and serializes captures so concurrent
// callers cannot observe one another's browser events or proof headers.
type browserManager struct {
	captureGate chan struct{}
	factory     proofBrowserFactory
	browser     proofBrowser
}

func newBrowserManager(factory proofBrowserFactory) *browserManager {
	return &browserManager{captureGate: make(chan struct{}, 1), factory: factory}
}

func (m *browserManager) Capture(ctx context.Context) (*DynamicHeaders, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	captureCtx, cancelCapture := context.WithTimeout(ctx, duckAIProofCaptureTimeout)
	defer cancelCapture()
	select {
	case m.captureGate <- struct{}{}:
		defer func() { <-m.captureGate }()
	case <-captureCtx.Done():
		return nil, captureCtx.Err()
	}

	if err := captureCtx.Err(); err != nil {
		return nil, err
	}
	if m.browser == nil {
		browser, err := m.factory(captureCtx)
		if err != nil {
			return nil, err
		}
		if browser == nil {
			return nil, fmt.Errorf("Duck.ai proof browser factory returned nil")
		}
		m.browser = browser
	}

	headers, err := m.browser.Capture(captureCtx)
	if err != nil && ctx.Err() == nil {
		closeErr := m.browser.Close()
		m.browser = nil
		if closeErr != nil {
			return nil, fmt.Errorf("Duck.ai proof capture failed: %w; closing browser failed: %v", err, closeErr)
		}
	}
	return headers, err
}

func (m *browserManager) Close() error {
	// Bound the wait so a hung in-flight capture cannot stall process shutdown
	// for the full capture budget.
	select {
	case m.captureGate <- struct{}{}:
	case <-time.After(browserCloseWait):
		return fmt.Errorf("timed out waiting %s for the in-flight Duck.ai proof capture to finish", browserCloseWait)
	}
	defer func() { <-m.captureGate }()
	if m.browser == nil {
		return nil
	}
	browser := m.browser
	m.browser = nil
	return browser.Close()
}

type chromedpProofBrowser struct {
	browserCtx      context.Context
	cancelBrowser   context.CancelFunc
	cancelAllocator context.CancelFunc
}

func (b *chromedpProofBrowser) Close() error {
	if b.browserCtx == nil {
		return nil
	}
	browserCtx := b.browserCtx
	cancelBrowser, cancelAllocator := b.cancelBrowser, b.cancelAllocator
	b.browserCtx = nil
	b.cancelBrowser, b.cancelAllocator = nil, nil

	// A browser whose context is already done was stopped by startup or caller
	// cancellation; chromedp.Cancel would only report that cancellation back.
	var err error
	if browserCtx.Err() == nil {
		// Keep the chromedp context values while bounding the graceful close.
		closeCtx, cancel := context.WithTimeout(browserCtx, browserCloseWait)
		err = chromedp.Cancel(closeCtx)
		cancel()
	}
	cancelBrowser()
	cancelAllocator()
	return err
}

func newChromedpProofBrowser(startupCtx context.Context) (proofBrowser, error) {
	execPath, err := findBrowserExecutable()
	if err != nil {
		return nil, err
	}

	allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions,
		chromedp.ExecPath(execPath),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.WindowSize(1280, 900),
	)

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	browser := &chromedpProofBrowser{
		browserCtx:      browserCtx,
		cancelBrowser:   cancelBrowser,
		cancelAllocator: cancelAllocator,
	}
	stopOnStartupCancel := context.AfterFunc(startupCtx, cancelBrowser)
	err = chromedp.Run(browserCtx)
	startupActive := stopOnStartupCancel()
	if err != nil || !startupActive || startupCtx.Err() != nil {
		if err == nil {
			err = startupCtx.Err()
		}
		cancelBrowser()
		cancelAllocator()
		return nil, fmt.Errorf("Duck.ai Chrome startup failed: %w", err)
	}
	return browser, nil
}

func (b *chromedpProofBrowser) Capture(parent context.Context) (*DynamicHeaders, error) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}

	// Each capture gets its own target so an aborted calibration request and
	// the resulting frontend error state cannot leak into the next capture.
	targetCtx, cancelTarget := chromedp.NewContext(b.browserCtx)
	stopOnCallerCancel := context.AfterFunc(parent, cancelTarget)
	defer stopOnCallerCancel()
	defer cancelTarget()
	// browserManager.Capture already applies duckAIProofCaptureTimeout and that
	// deadline always expires first; this is a fallback for a direct caller.
	ctx, cancelTimeout := context.WithTimeout(targetCtx, duckAIProofCaptureTimeout)
	defer cancelTimeout()

	captured := make(chan *DynamicHeaders, 1)
	chromedp.ListenTarget(targetCtx, func(event any) {
		if paused, ok := event.(*fetch.EventRequestPaused); ok {
			if strings.Contains(paused.Request.URL, "/duckchat/v1/chat") {
				headers := headersFromCDP(paused.Request.Headers)
				if headers.VqdHash1 != "" {
					select {
					case captured <- headers:
					default:
					}
				}
				// The calibration request exists only to make Duck.ai compute
				// the proof. Abort it before it consumes quota or a chat turn.
				_ = fetch.FailRequest(paused.RequestID, network.ErrorReasonAborted).Do(ctx)
				return
			}
			_ = fetch.ContinueRequest(paused.RequestID).Do(ctx)
			return
		}
		requestEvent, ok := event.(*network.EventRequestWillBeSent)
		if !ok || !strings.Contains(requestEvent.Request.URL, "/duckchat/v1/chat") {
			return
		}
		headers := headersFromCDP(requestEvent.Request.Headers)
		if headers.VqdHash1 == "" {
			return
		}
		select {
		case captured <- headers:
		default:
		}
	})

	setPrompt := `(function() {
  const ta = document.querySelector('textarea[name="user-prompt"]');
  if (!ta) return 'missing textarea';
  const setter = Object.getOwnPropertyDescriptor(
    window.HTMLTextAreaElement.prototype, 'value').set;
  setter.call(ta, 'duckduckgo-chat-cli-' + Date.now());
  ta.dispatchEvent(new Event('input', { bubbles: true }));
  return 'ok';
})()`
	clickPrompt := `(function() {
  const button = document.querySelector('button[type="submit"]');
  if (!button || button.disabled) return 'submit unavailable';
  button.click();
  return 'clicked';
})()`

	var result string
	var submitReady bool
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
			`).Do(ctx)
			return err
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, _, userAgent, _, err := cdpbrowser.GetVersion().Do(ctx)
			if err != nil {
				return err
			}
			// Duck.ai rejects the HeadlessChrome token even when its own page
			// generated the proof. Keep the installed browser version intact.
			return emulation.SetUserAgentOverride(normalBrowserUserAgent(userAgent)).Do(ctx)
		}),
		network.Enable(),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{
			URLPattern:   "*duckchat/v1/chat*",
			RequestStage: fetch.RequestStageRequest,
		}}),
		chromedp.Navigate("https://duck.ai/"),
		chromedp.WaitVisible(`textarea[name="user-prompt"]`),
		chromedp.Evaluate(setPrompt, &result),
		// Poll the first submit button only. chromedp.WaitEnabled applies its
		// check to every matched node, so a page that also renders a hidden or
		// permanently disabled submit variant would never satisfy it.
		chromedp.Poll(`(function() {
  const button = document.querySelector('button[type="submit"]');
  return !!button && !button.disabled;
})()`, &submitReady, chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.Evaluate(clickPrompt, &result),
	)
	if err != nil {
		return nil, fmt.Errorf("Duck.ai browser bootstrap failed: %w", err)
	}

	select {
	case headers := <-captured:
		return headers, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("timed out waiting for Duck.ai chat headers: %w", ctx.Err())
	}
}

var sharedDuckAIBrowser = newBrowserManager(newChromedpProofBrowser)

func normalBrowserUserAgent(userAgent string) string {
	return strings.Replace(userAgent, "HeadlessChrome/", "Chrome/", 1)
}

func ShutdownBrowser() error {
	return sharedDuckAIBrowser.Close()
}

func headersFromCDP(raw network.Headers) *DynamicHeaders {
	headers := &DynamicHeaders{}
	for name, value := range raw {
		switch strings.ToLower(name) {
		case "x-vqd-hash-1":
			headers.VqdHash1 = fmt.Sprint(value)
		case "x-fe-signals":
			headers.FeSignals = fmt.Sprint(value)
		case "x-fe-version":
			headers.FeVersion = fmt.Sprint(value)
		case "x-ddg-journey-id":
			headers.JourneyID = fmt.Sprint(value)
		case "user-agent":
			headers.UserAgent = fmt.Sprint(value)
		case "accept-language":
			headers.AcceptLanguage = fmt.Sprint(value)
		case "sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform",
			"sec-fetch-dest", "sec-fetch-mode", "sec-fetch-site", "sec-gpc", "dnt", "priority":
			if headers.BrowserHeaders == nil {
				headers.BrowserHeaders = make(map[string]string)
			}
			headers.BrowserHeaders[name] = fmt.Sprint(value)
		}
	}
	return headers
}

func getCurrentDuckAIHeaders(ctx context.Context) (*DynamicHeaders, error) {
	return sharedDuckAIBrowser.Capture(ctx)
}

// CaptureDynamicHeaders returns fresh Duck.ai request headers captured by the
// shared browser manager. Callers must keep these values in memory only.
func CaptureDynamicHeaders(ctx context.Context) (*DynamicHeaders, error) {
	return getCurrentDuckAIHeaders(ctx)
}

// BrowserAvailable reports whether the local browser dependency needed for
// Duck.ai proof generation is installed.
func BrowserAvailable() bool {
	_, err := findBrowserExecutable()
	return err == nil
}

func findBrowserExecutable() (string, error) {
	var names []string
	switch runtime.GOOS {
	case "darwin":
		names = []string{"google-chrome", "chromium", "Google Chrome", "Chromium"}
	case "windows":
		names = []string{"chrome.exe", "chromium.exe", "chrome", "chromium"}
	default:
		names = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
	}
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	for _, path := range browserInstallPaths(runtime.GOOS) {
		if executable, err := exec.LookPath(path); err == nil {
			return executable, nil
		}
	}
	return "", fmt.Errorf("Google Chrome or Chromium is required for Duck.ai browser access; install one and make sure its executable is available")
}

func browserInstallPaths(goos string) []string {
	var paths []string
	switch goos {
	case "darwin":
		paths = append(paths,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		)
		if userHome, err := os.UserHomeDir(); err == nil {
			paths = append(paths,
				filepath.Join(userHome, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
				filepath.Join(userHome, "Applications/Chromium.app/Contents/MacOS/Chromium"),
			)
		}
	case "windows":
		for _, programFiles := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if programFiles == "" {
				continue
			}
			paths = append(paths,
				filepath.Join(programFiles, "Google/Chrome/Application/chrome.exe"),
				filepath.Join(programFiles, "Chromium/Application/chrome.exe"),
			)
		}
	}
	return paths
}

// BrowserExecutable returns the supported local Chrome/Chromium executable.
func BrowserExecutable() (string, error) {
	return findBrowserExecutable()
}
