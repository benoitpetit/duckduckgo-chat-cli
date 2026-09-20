package chat

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type DynamicHeaders struct {
	FeSignals string
	FeVersion string
	VqdHash1  string
	UserAgent string
}

// captureDuckAIHeaders lets Duck.ai's own frontend solve its browser-bound
// VQD proof of work and captures the headers used for the resulting request.
// The challenge is intentionally not hard-coded: Duck.ai rotates it regularly.
func captureDuckAIHeaders() (*DynamicHeaders, error) {
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
		chromedp.Flag("user-agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36"),
		chromedp.WindowSize(1280, 900),
	)

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAllocator()

	ctx, cancel := chromedp.NewContext(allocatorCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	captured := make(chan *DynamicHeaders, 1)
	chromedp.ListenTarget(ctx, func(event any) {
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
	err = chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
Object.defineProperty(navigator, 'platform', { get: () => 'Linux x86_64' });
			`).Do(ctx)
			return err
		}),
		network.Enable(),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{
			URLPattern:   "*duckchat/v1/chat*",
			RequestStage: fetch.RequestStageRequest,
		}}),
		chromedp.Navigate("https://duck.ai/"),
		chromedp.WaitVisible(`textarea[name="user-prompt"]`),
		chromedp.Evaluate(setPrompt, &result),
		chromedp.Sleep(500*time.Millisecond),
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
		case "user-agent":
			headers.UserAgent = fmt.Sprint(value)
		}
	}
	return headers
}

func getCurrentDuckAIHeaders() (*DynamicHeaders, error) {
	return captureDuckAIHeaders()
}

func findBrowserExecutable() (string, error) {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("Chrome/Chromium is required to authenticate with Duck.ai")
}
