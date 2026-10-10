package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/browserrelay"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// This smoke test exercises the proof capture and the actual chat transport.
func TestLivePlainChat(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_TEST=1 for the live Duck.ai request")
	}
	t.Cleanup(func() {
		if err := ShutdownBrowser(); err != nil {
			t.Errorf("ShutdownBrowser() failed: %v", err)
		}
	})
	chat := NewChat("", "", "", "", models.GPT54Mini, &config.Config{ExportDir: t.TempDir(), Dashboard: config.DashboardConfig{RetentionDays: 30}})
	chat.Messages = []Message{{Role: "user", Content: "Reply PONG only"}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	response, err := chat.FetchContext(ctx, "Reply PONG only")
	if err != nil {
		t.Fatalf("live chat transport failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "data: [DONE]") || !strings.Contains(string(body), `"message":`) {
		t.Fatalf("chat response did not complete: %s", body)
	}
}

// Exercise the same Chat instance across two completed responses. A target
// created with the first request's short-lived context used to hang here.
func TestLiveTwoTurnChatKeepsHeadlessTabAlive(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_TWO_TURN_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_TWO_TURN_TEST=1 for two live Duck.ai turns")
	}
	t.Cleanup(func() { _ = ShutdownBrowser() })
	cfg := &config.Config{ExportDir: t.TempDir(), Dashboard: config.DashboardConfig{RetentionDays: 30}}
	chat := NewChat("", "", "", "", models.GPT54Mini, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for turn, prompt := range []string{"Reply with hello in one word.", "Show a tiny Go function."} {
		response, err := ProcessInputContext(ctx, chat, prompt, cfg)
		if err != nil {
			t.Fatalf("turn %d failed: %v", turn+1, err)
		}
		if strings.TrimSpace(response) == "" {
			t.Fatalf("turn %d returned an empty response", turn+1)
		}
	}
}

// This opt-in check sends one normal CLI payload through the user's installed
// Chrome extension after it clears Duck.ai site data in that browser profile.
func TestLiveDefaultChromeChatAfterSiteDataReset(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_DEFAULT_CHROME_CHAT_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_DEFAULT_CHROME_CHAT_TEST=1 to test a real chat via Chrome")
	}
	chat := &Chat{Model: models.GPT54Mini, Messages: []Message{{Role: "user", Content: "Reply PONG only"}}}
	durable, err := chat.durableStreamForRequest()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(chat.buildPayload(durable))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	response, _, err := browserrelay.New(browserrelay.Options{}).Do(ctx, browserrelay.Request{
		URL: models.ChatURL, Method: http.MethodPost,
		Header: http.Header{"Accept": {"text/event-stream"}, "Content-Type": {"application/json"}},
		Body:   payload,
	})
	if err != nil {
		t.Fatalf("Chrome chat relay failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Duck.ai rejected the clean Chrome session with HTTP %d", response.StatusCode)
	}
	if !strings.Contains(string(body), "data: [DONE]") || !strings.Contains(string(body), `"message":`) {
		t.Fatal("Duck.ai returned HTTP 200 but no completed assistant response")
	}
}

// Opt-in end-to-end check for the CLI's 429 branch. Only the final browser
// request reaches Duck.ai; the two preceding HTTP 429s are local fixtures.
func TestLiveCLIRateLimitRecoveryViaDefaultChrome(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_CLI_429_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_CLI_429_TEST=1 to exercise the full CLI 429 recovery")
	}
	previousBrowser := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &countingProofBrowser{}, nil
	})
	t.Cleanup(func() {
		_ = sharedDuckAIBrowser.Close()
		sharedDuckAIBrowser = previousBrowser
	})
	cfg := &config.Config{
		ExportDir: t.TempDir(), Dashboard: config.DashboardConfig{RetentionDays: 30},
		RateLimit: config.RateLimitConfig{OpenBrowser: true},
	}
	chat := NewChat("", "", "", "", models.GPT54Mini, cfg)
	chat.visibleProofBrowserFactory = nil
	chat.Client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)),
			Request:    request,
		}, nil
	})
	chat.Messages = []Message{{Role: "user", Content: "Reply PONG only"}}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	stream, failures, err := chat.FetchStreamWithErrors(ctx, "Reply PONG only")
	if err != nil {
		t.Fatalf("CLI 429 recovery failed: %v", err)
	}
	var answer strings.Builder
	for part := range stream {
		answer.WriteString(part)
	}
	for failure := range failures {
		if failure != nil {
			t.Fatalf("CLI browser response failed: %v", failure)
		}
	}
	if answer.Len() == 0 {
		t.Fatal("CLI browser retry returned no assistant text")
	}
}

// Compare a fresh temporary headless Chromium profile with that same profile
// after the Duck.ai site data categories selected in Chrome have been cleared.
func TestLiveHeadlessChatAfterSiteDataReset(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_HEADLESS_RESET_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_HEADLESS_RESET_TEST=1 to test headless site-data reset")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	created, err := newChromedpProofBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	browser := created.(*chromedpProofBrowser)
	defer browser.Close()
	chat := &Chat{Model: models.GPT54Mini, Messages: []Message{{Role: "user", Content: "Reply PONG only"}}}
	request := func() (int, bool, error) {
		return liveTemporaryBrowserChat(ctx, browser, chat)
	}
	beforeStatus, beforeComplete, err := request()
	if err != nil {
		t.Fatalf("headless request before reset: %v", err)
	}
	if err := chromedp.Run(browser.browserCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return storage.ClearDataForOrigin("https://duck.ai", "all").Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error { return network.ClearBrowserCookies().Do(ctx) }),
		chromedp.ActionFunc(func(ctx context.Context) error { return network.ClearBrowserCache().Do(ctx) }),
	); err != nil {
		t.Fatalf("clear headless Duck.ai site data: %v", err)
	}
	chat.resetDurableConversation()
	afterStatus, afterComplete, err := request()
	if err != nil {
		t.Fatalf("headless request after reset: %v", err)
	}
	t.Logf("headless before reset: HTTP %d, completed=%t; after reset: HTTP %d, completed=%t", beforeStatus, beforeComplete, afterStatus, afterComplete)
	if afterStatus != http.StatusOK || !afterComplete {
		t.Fatalf("headless site-data reset did not produce a completed chat (HTTP %d)", afterStatus)
	}
}

func TestLiveVisibleTemporaryBrowserChat(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_VISIBLE_TEMPORARY_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_VISIBLE_TEMPORARY_TEST=1 to test a clean visible Chromium profile")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	created, err := newVisibleChromedpProofBrowser(ctx)
	if err != nil {
		t.Fatal(err)
	}
	browser := created.(*chromedpProofBrowser)
	defer browser.Close()
	chat := &Chat{Model: models.GPT54Mini, Messages: []Message{{Role: "user", Content: "Reply PONG only"}}}
	status, complete, err := liveTemporaryBrowserChat(ctx, browser, chat)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("clean visible temporary Chromium: HTTP %d, completed=%t", status, complete)
	if status != http.StatusOK || !complete {
		t.Fatalf("clean visible temporary Chromium did not complete chat (HTTP %d)", status)
	}
}

func TestLiveIsolatedChromeExtensionChat(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_ISOLATED_EXTENSION_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_ISOLATED_EXTENSION_TEST=1 to test the extension in a clean automated Chrome profile")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	executable, err := exec.LookPath("chromium")
	if err != nil {
		t.Skipf("Chromium is unavailable: %v", err)
	}
	extension, err := filepath.Abs("../../browser-extension/duckchat-relay")
	if err != nil {
		t.Fatal(err)
	}
	profile := t.TempDir()
	var process *exec.Cmd
	relay := browserrelay.New(browserrelay.Options{OpenBrowser: func(target string) error {
		process = exec.CommandContext(ctx, executable,
			"--user-data-dir="+profile,
			"--disable-extensions-except="+extension,
			"--load-extension="+extension,
			"--no-first-run", "--no-default-browser-check", "--no-sandbox",
			"--new-window", target,
		)
		process.Stdout, process.Stderr = io.Discard, io.Discard
		return process.Start()
	}})
	defer func() {
		cancel()
		if process != nil {
			_ = process.Wait()
		}
	}()
	chat := &Chat{Model: models.GPT54Mini, Messages: []Message{{Role: "user", Content: "Reply PONG only"}}}
	durable, err := chat.durableStreamForRequest()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(chat.buildPayload(durable))
	if err != nil {
		t.Fatal(err)
	}
	response, _, err := relay.Do(ctx, browserrelay.Request{
		URL: models.ChatURL, Method: http.MethodPost,
		Header: http.Header{"Accept": {"text/event-stream"}, "Content-Type": {"application/json"}},
		Body:   payload,
	})
	if err != nil {
		t.Fatalf("isolated Chrome extension failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	complete := strings.Contains(string(body), "data: [DONE]")
	t.Logf("isolated automated Chrome extension: HTTP %d, completed=%t", response.StatusCode, complete)
	if response.StatusCode != http.StatusOK || !complete {
		t.Fatalf("isolated automated Chrome extension did not complete chat (HTTP %d)", response.StatusCode)
	}
}

func liveTemporaryBrowserChat(ctx context.Context, browser *chromedpProofBrowser, chat *Chat) (int, bool, error) {
	proof, err := browser.Capture(ctx)
	if errors.Is(err, ErrRateLimited) {
		return http.StatusTooManyRequests, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	durable, err := chat.durableStreamForRequest()
	if err != nil {
		return 0, false, err
	}
	payload, err := json.Marshal(chat.buildPayload(durable))
	if err != nil {
		return 0, false, err
	}
	headers := http.Header{"Accept": {"text/event-stream"}, "Content-Type": {"application/json"}}
	headers.Set("x-vqd-hash-1", proof.VqdHash1)
	if proof.FeSignals != "" {
		headers.Set("x-fe-signals", proof.FeSignals)
	}
	if proof.FeVersion != "" {
		headers.Set("x-fe-version", proof.FeVersion)
	}
	if proof.JourneyID != "" {
		headers.Set("x-ddg-journey-id", proof.JourneyID)
	}
	response, err := browser.Request(ctx, browserrelay.Request{
		URL: models.ChatURL, Method: http.MethodPost, Header: headers, Body: payload,
	})
	if err != nil {
		return 0, false, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return response.StatusCode, strings.Contains(string(body), "data: [DONE]"), err
}
