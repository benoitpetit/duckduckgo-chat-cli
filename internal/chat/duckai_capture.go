package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/browserrelay"
	"duckduckgo-chat-cli/internal/models"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/storage"
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
	BrowserCookies []*http.Cookie
}

type proofBrowser interface {
	Capture(context.Context) (*DynamicHeaders, error)
	Close() error
}

// siteDataCleaner keeps the browser process alive while removing the same
// Duck.ai origin data that Chrome's "Clear site data" action removes.
type siteDataCleaner interface {
	ClearSiteData(context.Context) error
}

type browserPageRequester interface {
	Request(context.Context, browserrelay.Request) (*http.Response, error)
}

var errBrowserPageRequestUnavailable = fmt.Errorf("Duck.ai proof browser page request is unavailable")

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
	if err != nil && ctx.Err() == nil && !errors.Is(err, ErrRateLimited) {
		closeErr := m.browser.Close()
		m.browser = nil
		if closeErr != nil {
			return nil, fmt.Errorf("Duck.ai proof capture failed: %w; closing browser failed: %v", err, closeErr)
		}
	}
	return headers, err
}

// ClearSiteData resets the active browser in place. A fake or older browser
// without this capability is discarded so the next capture still starts clean.
func (m *browserManager) ClearSiteData(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.captureGate <- struct{}{}:
		defer func() { <-m.captureGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.browser == nil {
		return nil
	}
	if cleaner, ok := m.browser.(siteDataCleaner); ok {
		if err := cleaner.ClearSiteData(ctx); err == nil {
			return nil
		} else {
			// A failed CDP clear cannot leave a possibly stale browser active.
			closeErr := m.browser.Close()
			m.browser = nil
			if closeErr != nil {
				return fmt.Errorf("clear Duck.ai site data: %w; close browser: %v", err, closeErr)
			}
			return fmt.Errorf("clear Duck.ai site data: %w", err)
		}
	}
	browser := m.browser
	m.browser = nil
	return browser.Close()
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

// PageRequest sends a request from the same temporary Chrome profile that
// generated its proof. This keeps browser cookies and network identity aligned.
func (m *browserManager) PageRequest(ctx context.Context, request browserrelay.Request) (*http.Response, error) {
	select {
	case m.captureGate <- struct{}{}:
		defer func() { <-m.captureGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	requester, ok := m.browser.(browserPageRequester)
	if !ok {
		return nil, errBrowserPageRequestUnavailable
	}
	response, err := requester.Request(ctx, request)
	if err != nil && ctx.Err() == nil {
		_ = m.browser.Close()
		m.browser = nil
	}
	return response, err
}

type chromedpProofBrowser struct {
	browserCtx      context.Context
	cancelBrowser   context.CancelFunc
	cancelAllocator context.CancelFunc
	targetCtx       context.Context
	cancelTarget    context.CancelFunc
}

// chromedp starts the target event loop with the context used by its first
// Run. Create it with targetCtx, which lives across requests, before deriving
// a per-request context for navigation and proof capture.
func (b *chromedpProofBrowser) ensureTarget(parent context.Context) error {
	if b.targetCtx != nil && b.targetCtx.Err() == nil {
		return nil
	}
	if b.cancelTarget != nil {
		b.cancelTarget()
	}
	b.targetCtx, b.cancelTarget = chromedp.NewContext(b.browserCtx)
	cancelTarget := b.cancelTarget
	stopOnCancel := context.AfterFunc(parent, cancelTarget)
	err := chromedp.Run(b.targetCtx)
	stillActive := stopOnCancel()
	if err == nil && parent.Err() == nil && stillActive {
		return nil
	}
	if err == nil {
		err = parent.Err()
		if err == nil {
			err = context.Canceled
		}
	}
	cancelTarget()
	b.targetCtx, b.cancelTarget = nil, nil
	return fmt.Errorf("start Duck.ai browser tab: %w", err)
}

func (b *chromedpProofBrowser) ClearSiteData(parent context.Context) error {
	if b.browserCtx == nil {
		return fmt.Errorf("Duck.ai browser is closed")
	}
	resetParent, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	ctx, cleanup := proofCaptureContext(b.browserCtx, resetParent)
	defer cleanup()
	// Leave the Duck.ai page before deleting its storage, just as a browser
	// reload after "Clear site data" starts without the old page state.
	if b.targetCtx != nil {
		targetCtx, targetCleanup := proofCaptureContext(b.targetCtx, resetParent)
		if err := chromedp.Run(targetCtx, chromedp.Navigate("about:blank")); err != nil {
			targetCleanup()
			return err
		}
		targetCleanup()
	}
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := storage.ClearDataForOrigin("https://duck.ai", "all").Do(ctx); err != nil {
			return err
		}
		if err := network.ClearBrowserCookies().Do(ctx); err != nil {
			return err
		}
		return network.ClearBrowserCache().Do(ctx)
	}))
}

func (b *chromedpProofBrowser) Close() error {
	if b.browserCtx == nil {
		return nil
	}
	browserCtx := b.browserCtx
	cancelBrowser, cancelAllocator := b.cancelBrowser, b.cancelAllocator
	if b.cancelTarget != nil {
		b.cancelTarget()
	}
	b.targetCtx, b.cancelTarget = nil, nil
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

func proofCaptureContext(targetCtx, parent context.Context) (context.Context, func()) {
	captureCtx, cancel := context.WithCancelCause(targetCtx)
	stop := context.AfterFunc(parent, func() {
		cancel(context.Cause(parent))
	})
	return captureCtx, func() {
		stop()
		cancel(context.Canceled)
	}
}

func newChromedpProofBrowser(startupCtx context.Context) (proofBrowser, error) {
	return newChromedpProofBrowserMode(startupCtx, true)
}

func newVisibleChromedpProofBrowser(startupCtx context.Context) (proofBrowser, error) {
	return newChromedpProofBrowserMode(startupCtx, false)
}

func newChromedpProofBrowserMode(startupCtx context.Context, headless bool) (proofBrowser, error) {
	execPath, err := findBrowserExecutable()
	if err != nil {
		return nil, err
	}

	allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions,
		chromedp.ExecPath(execPath),
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.WindowSize(1280, 900),
	)
	if headless {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("headless", "new"))
	}

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

	// Keep one tab for the lifetime of the headless browser. A new target for
	// every message gives Duck.ai a different tab/session each time, unlike a
	// normal conversation in Chrome. Navigation below resets the page state.
	if err := b.ensureTarget(parent); err != nil {
		return nil, err
	}
	ctx, cleanupCapture := proofCaptureContext(b.targetCtx, parent)
	defer cleanupCapture()

	type captureResult struct {
		headers *DynamicHeaders
		err     error
	}
	captured := make(chan captureResult, 1)
	chromedp.ListenTarget(ctx, func(event any) {
		if paused, ok := event.(*fetch.EventRequestPaused); ok {
			execCtx := cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
			if strings.Contains(paused.Request.URL, "/duckchat/v1/chat") {
				headers := headersFromCDP(paused.Request.Headers)
				// Listener callbacks run on chromedp's event loop. CDP
				// commands issued directly here can deadlock the tab.
				go func() {
					err := fetch.FailRequest(paused.RequestID, network.ErrorReasonAborted).Do(execCtx)
					select {
					case captured <- captureResult{headers: headers, err: err}:
					case <-ctx.Done():
					}
				}()
				// The calibration request exists only to make Duck.ai compute
				// the proof. Abort it before it consumes quota or a chat turn.
				return
			}
			go func() { _ = fetch.ContinueRequest(paused.RequestID).Do(execCtx) }()
			return
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

	const pageReady = `(function() {
  const body = (document.body?.innerText || '').toLowerCase();
  if (body.includes('too many requests') || body.includes('trop de requêtes') || body.includes('trop de requetes') || body.includes('err_rate_limit')) return 'rate-limited';
  const textarea = document.querySelector('textarea[name="user-prompt"]');
  return textarea && textarea.getClientRects().length ? 'ready' : '';
})()`
	const submitState = `(function() {
  const body = (document.body?.innerText || '').toLowerCase();
  if (body.includes('too many requests') || body.includes('trop de requêtes') || body.includes('trop de requetes') || body.includes('err_rate_limit')) return 'rate-limited';
  const button = document.querySelector('button[type="submit"]');
  return button && !button.disabled ? 'ready' : '';
})()`
	var result, state string
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
		chromedp.Poll(pageReady, &state, chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.ActionFunc(func(context.Context) error {
			if state == "rate-limited" {
				return &RateLimitError{Code: "ERR_RATE_LIMIT"}
			}
			return nil
		}),
		chromedp.Evaluate(setPrompt, &result),
		// Poll the first submit button only. chromedp.WaitEnabled applies its
		// check to every matched node, so a page that also renders a hidden or
		// permanently disabled submit variant would never satisfy it.
		chromedp.Poll(submitState, &state, chromedp.WithPollingInterval(100*time.Millisecond)),
		chromedp.ActionFunc(func(context.Context) error {
			if state == "rate-limited" {
				return &RateLimitError{Code: "ERR_RATE_LIMIT"}
			}
			return nil
		}),
		chromedp.Evaluate(clickPrompt, &result),
	)
	if err != nil {
		return nil, fmt.Errorf("Duck.ai browser bootstrap failed: %w", err)
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case result := <-captured:
			if result.err != nil {
				return nil, fmt.Errorf("abort Duck.ai proof calibration request: %w", result.err)
			}
			headers := result.headers
			if headers.VqdHash1 == "" {
				return nil, fmt.Errorf("Duck.ai browser request had no X-Vqd-Hash-1 proof")
			}
			// The proof and the page's cookies belong to the same temporary Chrome
			// profile. Carry both to the Go request without persisting profile data.
			var cookies []*network.Cookie
			err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				var getErr error
				cookies, getErr = network.GetCookies().WithURLs([]string{"https://duck.ai/duckchat/v1/chat"}).Do(ctx)
				return getErr
			}))
			if err != nil {
				return nil, fmt.Errorf("read Duck.ai proof browser cookies: %w", err)
			}
			for _, cookie := range cookies {
				if cookie == nil {
					continue
				}
				browserCookie := &http.Cookie{
					Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain,
					Path: cookie.Path, Secure: cookie.Secure, HttpOnly: cookie.HTTPOnly,
				}
				if !cookie.Session && cookie.Expires > 0 {
					browserCookie.Expires = time.Unix(int64(cookie.Expires), 0)
				}
				headers.BrowserCookies = append(headers.BrowserCookies, browserCookie)
			}
			return headers, nil
		case <-ticker.C:
			var pageText string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body?.innerText || ''`, &pageText)); err == nil && duckAIPageRateLimited(pageText) {
				return nil, &RateLimitError{Code: "ERR_RATE_LIMIT"}
			}
		case <-ctx.Done():
			cause := context.Cause(ctx)
			if cause == nil {
				cause = ctx.Err()
			}
			if cause == context.DeadlineExceeded {
				return nil, fmt.Errorf("timed out waiting for Duck.ai chat headers: %w", cause)
			}
			return nil, fmt.Errorf("waiting for Duck.ai chat headers was canceled: %w", cause)
		}
	}
}

func (b *chromedpProofBrowser) Request(parent context.Context, request browserrelay.Request) (*http.Response, error) {
	if request.URL != models.ChatURL || request.Method != http.MethodPost {
		return nil, fmt.Errorf("invalid Duck.ai browser retry target")
	}
	const browserRequestTimeout = 90 * time.Second
	requestParent, cancelTimeout := context.WithTimeout(parent, browserRequestTimeout)
	defer cancelTimeout()
	targetCtx, cancelTarget := chromedp.NewContext(b.browserCtx)
	ctx, cleanup := proofCaptureContext(targetCtx, requestParent)
	defer cleanup()
	defer cancelTarget()

	job, err := json.Marshal(struct {
		URL     string              `json:"url"`
		Headers map[string][]string `json:"headers"`
		Body    string              `json:"body"`
	}{request.URL, browserrelay.FilterRequestHeaders(request.Header), string(request.Body)})
	if err != nil {
		return nil, fmt.Errorf("encode Duck.ai browser retry: %w", err)
	}
	script := `(async () => {
  const job = ` + string(job) + `;
  const headers = new Headers(job.headers);
  const response = await fetch(job.url, {
    method: 'POST', headers, body: job.body, credentials: 'include',
    cache: 'no-store', redirect: 'error'
  });
  const reader = response.body?.getReader();
  const decoder = new TextDecoder();
  let size = 0;
  const chunks = [];
  if (reader) {
    while (true) {
      const part = await reader.read();
      if (part.done) break;
      size += part.value.byteLength;
      if (size > 32 * 1024 * 1024) {
        await reader.cancel();
        throw new Error('Duck.ai browser retry response is too large');
      }
      chunks.push(decoder.decode(part.value, {stream: true}));
    }
    chunks.push(decoder.decode());
  }
  const responseHeaders = {};
  for (const name of ['content-type', 'retry-after', 'x-vqd-4', 'cache-control']) {
    const value = response.headers.get(name);
    if (value !== null) responseHeaders[name] = value;
  }
  return {status: response.status, headers: responseHeaders, body: chunks.join('')};
})()`
	var result struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	err = chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`Object.defineProperty(navigator, 'webdriver', { get: () => undefined });`).Do(ctx)
			return err
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, _, userAgent, _, err := cdpbrowser.GetVersion().Do(ctx)
			if err != nil {
				return err
			}
			return emulation.SetUserAgentOverride(normalBrowserUserAgent(userAgent)).Do(ctx)
		}),
		chromedp.Navigate("https://duck.ai/"),
		chromedp.Evaluate(script, &result, func(params *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
			return params.WithAwaitPromise(true)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("Duck.ai in-page retry failed: %w", err)
	}
	if result.Status < 100 || result.Status > 599 {
		return nil, fmt.Errorf("Duck.ai in-page retry returned invalid status %d", result.Status)
	}
	header := make(http.Header)
	for name, value := range result.Headers {
		header.Set(name, value)
	}
	return &http.Response{
		StatusCode: result.Status,
		Status:     fmt.Sprintf("%d %s", result.Status, http.StatusText(result.Status)),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(result.Body)),
	}, nil
}

func duckAIPageRateLimited(pageText string) bool {
	text := strings.ToLower(pageText)
	return strings.Contains(text, "too many requests") ||
		strings.Contains(text, "trop de requêtes") ||
		strings.Contains(text, "trop de requetes") ||
		strings.Contains(text, "err_rate_limit")
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
