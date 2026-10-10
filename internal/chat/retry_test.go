package chat

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/browserrelay"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/chromedp/cdproto/network"
)

func TestCapturedJourneyIDTravelsWithProof(t *testing.T) {
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &fixedHeadersProofBrowser{headers: headersFromCDP(network.Headers{
			"X-Vqd-Hash-1": "proof", "x-fe-signals": "signals", "x-fe-version": "version",
			"x-ddg-journey-id": "browser-journey",
		})}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	chat := &Chat{Model: models.GPT54Mini, Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("x-ddg-journey-id"); got != "browser-journey" {
			t.Errorf("journey ID = %q, want browser-journey from captured proof", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")), Request: req}, nil
	})}}
	response, err := chat.FetchContext(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

func TestRateLimitResetHonorsCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	chat := &Chat{}
	if err := chat.resetRateLimitSessionContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("resetRateLimitSessionContext() error = %v, want context canceled", err)
	}
}

func TestRecoveryErrorDetailAvoidsRepeatingRateLimitGuidance(t *testing.T) {
	got := recoveryErrorDetail(errors.Join(errRetryProofCapture, &RateLimitError{Code: "ERR_RATE_LIMIT"}))
	if got != "Duck.ai still reports HTTP 429 in that browser session" {
		t.Fatalf("recovery error detail = %q", got)
	}
}

func TestChallengeResponseHidesOpaqueChallengeData(t *testing.T) {
	chat := &Chat{Model: models.GPT54Mini}
	req, err := http.NewRequest(http.MethodPost, models.ChatURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{
		StatusCode: http.StatusTeapot,
		Status:     "418 I'm a teapot",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_CHALLENGE","cd":{"token":"opaque-secret"}}`)),
	}
	_, err = chat.handleFetchResponse(context.Background(), response, req, "hello", nil, 0, nil, time.Now())
	if !errors.Is(err, ErrDuckAIChallenge) || strings.Contains(err.Error(), "opaque-secret") {
		t.Fatalf("challenge error = %v, want concise redacted classification", err)
	}
}

func TestLegacyBrowserSettingRetriesOnlyInHeadlessSession(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &countingProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	cfg := &config.Config{
		ExportDir: t.TempDir(), Dashboard: config.DashboardConfig{RetentionDays: 30},
		RateLimit: config.RateLimitConfig{OpenBrowser: true},
	}
	chat := NewChat("", "", "", "", models.GPT54Mini, cfg)
	var requests int
	chat.Client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)), Request: req}, nil
	})
	_, err := chat.FetchContext(context.Background(), "hello")
	if !errors.Is(err, ErrRateLimited) || requests != 2 || proof.captures.Load() != 2 {
		t.Fatalf("error=%v requests=%d proof captures=%d, want one local reset and retry", err, requests, proof.captures.Load())
	}
}

func TestHeadlessProofRateLimitResetsOnceWithoutVisibleBrowser(t *testing.T) {
	previous := sharedDuckAIBrowser
	var browserStarts int
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		browserStarts++
		if browserStarts == 1 {
			return &rateLimitedProofBrowser{}, nil
		}
		return &countingProofBrowser{}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	var requests int
	chat := &Chat{Model: models.GPT54Mini, Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")), Request: req}, nil
	})}}
	response, err := chat.FetchContext(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if browserStarts != 2 || requests != 1 {
		t.Fatalf("headless starts=%d HTTP requests=%d, want one reset before one send", browserStarts, requests)
	}
}

func TestCapturedBrowserCookiesTravelWithProof(t *testing.T) {
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &fixedHeadersProofBrowser{headers: &DynamicHeaders{
			VqdHash1: "proof", BrowserCookies: []*http.Cookie{{Name: "browser-session", Value: "same-profile"}},
		}}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	jar := newDuckAICookieJar()
	chat := &Chat{Model: models.GPT54Mini, CookieJar: jar, Client: &http.Client{Jar: jar, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if cookie, err := req.Cookie("browser-session"); err != nil || cookie.Value != "same-profile" {
			t.Errorf("Duck.ai request did not carry the browser proof session cookie: %v", err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")), Request: req}, nil
	})}}
	response, err := chat.FetchContext(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
}

type countingProofBrowser struct{ captures atomic.Int32 }

func (b *countingProofBrowser) Capture(context.Context) (*DynamicHeaders, error) {
	b.captures.Add(1)
	return &DynamicHeaders{VqdHash1: "proof", FeSignals: "signals", FeVersion: "version"}, nil
}

func (b *countingProofBrowser) Close() error { return nil }

type retryFailingProofBrowser struct{ captures atomic.Int32 }

func (b *retryFailingProofBrowser) Capture(context.Context) (*DynamicHeaders, error) {
	if b.captures.Add(1) == 2 {
		return nil, errors.New("timed out waiting for Duck.ai chat headers: context deadline exceeded")
	}
	return &DynamicHeaders{VqdHash1: "proof"}, nil
}

func (b *retryFailingProofBrowser) Close() error { return nil }

type blockingRetryProofBrowser struct{ captures atomic.Int32 }

func (b *blockingRetryProofBrowser) Capture(ctx context.Context) (*DynamicHeaders, error) {
	if b.captures.Add(1) == 1 {
		return &DynamicHeaders{VqdHash1: "proof"}, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func (b *blockingRetryProofBrowser) Close() error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type fakeBrowserRetry struct {
	calls    int
	request  browserrelay.Request
	response *http.Response
	opened   bool
	err      error
}

type fakePageProofBrowser struct {
	requests int
	request  browserrelay.Request
}

type rateLimitedProofBrowser struct{}

func (*rateLimitedProofBrowser) Capture(context.Context) (*DynamicHeaders, error) {
	return nil, &RateLimitError{Code: "ERR_RATE_LIMIT"}
}

func (*rateLimitedProofBrowser) Close() error { return nil }

func (*fakePageProofBrowser) Capture(context.Context) (*DynamicHeaders, error) {
	return &DynamicHeaders{VqdHash1: "page-proof"}, nil
}

func (*fakePageProofBrowser) Close() error { return nil }

func (b *fakePageProofBrowser) Request(_ context.Context, request browserrelay.Request) (*http.Response, error) {
	b.requests++
	b.request = request
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {\"role\":\"assistant\",\"message\":\"PONG\"}\n\n" +
			"data: [DONE]\n\n")),
	}, nil
}

func Test429UsesSameProofBrowserSessionBeforeResetOrExternalChrome(t *testing.T) {
	previous := sharedDuckAIBrowser
	page := &fakePageProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return page, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	localRequests := 0
	external := &fakeBrowserRetry{}
	chat := &Chat{
		Model: models.GPT5Luna, BrowserRetry: external,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			localRequests++
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)), Request: req}, nil
		})},
	}
	stream, streamErrors, err := chat.FetchStreamWithErrors(context.Background(), "ping")
	if err != nil {
		t.Fatalf("FetchStreamWithErrors() = %v, want in-page retry success", err)
	}
	var body strings.Builder
	for chunk := range stream {
		body.WriteString(chunk)
	}
	for streamErr := range streamErrors {
		if streamErr != nil {
			t.Fatal(streamErr)
		}
	}
	if body.String() != "PONG" || localRequests != 1 || page.requests != 1 || external.calls != 0 {
		t.Fatalf("body=%q local=%d page=%d external=%d", body.String(), localRequests, page.requests, external.calls)
	}
	if page.request.Header.Get("x-vqd-hash-1") != "page-proof" {
		t.Fatalf("page request lost the matching proof: %v", page.request.Header)
	}
}

func Test429UsesChromeRelayWithoutOpeningTemporaryBrowser(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &retryFailingProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return proof, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	visible := &fakePageProofBrowser{}
	external := &fakeBrowserRetry{opened: true, response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader("data: {\"role\":\"assistant\",\"message\":\"PONG\"}\n\n" +
			"data: [DONE]\n\n")),
	}}
	localRequests := 0
	chat := &Chat{
		Model: models.GPT5Luna, BrowserRetry: external,
		visibleProofBrowserFactory: func(context.Context) (proofBrowser, error) { return visible, nil },
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			localRequests++
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)), Request: req}, nil
		})},
	}
	stream, streamErrors, err := chat.FetchStreamWithErrors(context.Background(), "ping")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for chunk := range stream {
		body.WriteString(chunk)
	}
	for streamErr := range streamErrors {
		if streamErr != nil {
			t.Fatal(streamErr)
		}
	}
	if body.String() != "PONG" || localRequests != 1 || visible.requests != 0 || external.calls != 1 {
		t.Fatalf("body=%q local=%d visible=%d external=%d", body.String(), localRequests, visible.requests, external.calls)
	}
}

func TestInitialHeadlessRateLimitUsesCleanVisibleBrowser(t *testing.T) {
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &rateLimitedProofBrowser{}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	visible := &fakePageProofBrowser{}
	chat := &Chat{
		Model:                      models.GPT5Luna,
		visibleProofBrowserFactory: func(context.Context) (proofBrowser, error) { return visible, nil },
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("headless rate limit should not send a Go HTTP chat request")
			return nil, nil
		})},
	}
	stream, streamErrors, err := chat.FetchStreamWithErrors(context.Background(), "ping")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for chunk := range stream {
		body.WriteString(chunk)
	}
	for streamErr := range streamErrors {
		if streamErr != nil {
			t.Fatal(streamErr)
		}
	}
	if body.String() != "PONG" || visible.requests != 1 || visible.request.Header.Get("x-vqd-hash-1") != "page-proof" {
		t.Fatalf("body=%q visible requests=%d headers=%v", body.String(), visible.requests, visible.request.Header)
	}
}

func TestInitialHeadlessAndVisibleRateLimitUsesChromeRelay(t *testing.T) {
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &rateLimitedProofBrowser{}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	external := &fakeBrowserRetry{opened: true, response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"role\":\"assistant\",\"message\":\"PONG\"}\n\ndata: [DONE]\n\n")),
	}}
	chat := &Chat{
		Model: models.GPT5Luna, BrowserRetry: external,
		visibleProofBrowserFactory: func(context.Context) (proofBrowser, error) { return &rateLimitedProofBrowser{}, nil },
	}
	stream, streamErrors, err := chat.FetchStreamWithErrors(context.Background(), "ping")
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for chunk := range stream {
		body.WriteString(chunk)
	}
	for streamErr := range streamErrors {
		if streamErr != nil {
			t.Fatal(streamErr)
		}
	}
	if body.String() != "PONG" || external.calls != 1 {
		t.Fatalf("body=%q Chrome relay calls=%d", body.String(), external.calls)
	}
}

func (f *fakeBrowserRetry) Do(_ context.Context, request browserrelay.Request) (*http.Response, bool, error) {
	f.calls++
	f.request = request
	return f.response, f.opened, f.err
}

func TestFetchContextUsesOneBrowserSessionRetryAfterFreshSession429(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &countingProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Status:     "429 Too Many Requests",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)),
				Request:    req,
			}, nil
		})},
	}
	chat.BrowserRetry = &fakeBrowserRetry{
		opened: true,
		response: &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Vqd-4": {"next-vqd"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
		},
	}

	response, err := chat.FetchContext(context.Background(), "hello")
	if err != nil {
		t.Fatalf("FetchContext() error = %v, want browser retry success", err)
	}
	_ = response.Body.Close()
	if got := requests.Load(); got != 2 {
		t.Fatalf("local HTTP requests = %d, want initial request plus one clean-session retry", got)
	}
	if got := chat.BrowserRetry.(*fakeBrowserRetry).calls; got != 1 {
		t.Fatalf("browser retry calls = %d, want exactly one", got)
	}
	request := chat.BrowserRetry.(*fakeBrowserRetry).request
	if request.URL != models.ChatURL || request.Method != http.MethodPost || len(request.Body) == 0 {
		t.Fatalf("browser request = %+v, want the built Duck.ai POST payload", request)
	}
	if request.Header.Get("x-vqd-hash-1") != "proof" || request.Header.Get("Cookie") != "" || request.Header.Get("Origin") != "" {
		t.Fatalf("browser request headers = %v, want proof and no browser-owned credentials", request.Header)
	}
	if got := chat.NewVqd; got != "next-vqd" {
		t.Fatalf("NewVqd = %q, want response header from browser stream", got)
	}
}

func TestFetchContextDoesNotRetryBrowser429Again(t *testing.T) {
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &countingProofBrowser{}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Status:     "429 Too Many Requests",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)),
				Request:    req,
			}, nil
		})},
		BrowserRetry: &fakeBrowserRetry{
			opened: true,
			response: &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Status:     "429 Too Many Requests",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)),
			},
		},
	}

	_, err := chat.FetchContext(context.Background(), "hello")
	if err == nil || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("FetchContext() error = %v, want rate-limit classification", err)
	}
	if got := chat.BrowserRetry.(*fakeBrowserRetry).calls; got != 1 {
		t.Fatalf("browser retry calls = %d, want exactly one", got)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("local HTTP requests = %d, want exactly two", got)
	}
}

func TestFetchContextKeepsRateLimitWhenBrowserRelayFails(t *testing.T) {
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &countingProofBrowser{}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Status:     "429 Too Many Requests",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)),
				Request:    req,
			}, nil
		})},
		BrowserRetry: &fakeBrowserRetry{opened: true, err: errors.New("browser relay unavailable")},
	}

	_, err := chat.FetchContext(context.Background(), "hello")
	if !errors.Is(err, ErrRateLimited) || !strings.Contains(err.Error(), "browser relay unavailable") {
		t.Fatalf("FetchContext() error = %v, want original 429 and relay failure detail", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("local HTTP requests = %d, want exactly two before browser failure", got)
	}
	if got := chat.BrowserRetry.(*fakeBrowserRetry).calls; got != 1 {
		t.Fatalf("browser retry calls = %d, want exactly one", got)
	}
}

func TestFetchContextHonorsBrowserRetryAfterCancellation(t *testing.T) {
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &countingProofBrowser{}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			count := requests.Add(1)
			headers := make(http.Header)
			if count == 2 {
				headers.Set("Retry-After", "3600")
				time.AfterFunc(25*time.Millisecond, cancel)
			}
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Status:     "429 Too Many Requests",
				Header:     headers,
				Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)),
				Request:    req,
			}, nil
		})},
		BrowserRetry: &fakeBrowserRetry{opened: true},
	}

	_, err := chat.FetchContext(ctx, "hello")
	if !errors.Is(err, ErrRateLimited) || !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchContext() error = %v, want both rate-limit and cancellation classification", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("local HTTP requests = %d, want cancellation during browser Retry-After", got)
	}
	if got := chat.BrowserRetry.(*fakeBrowserRetry).calls; got != 0 {
		t.Fatalf("browser retry calls = %d, want none after cancellation", got)
	}
}

func TestFetchContextStillUsesBrowserRetryAfterPreviousFallback(t *testing.T) {
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &countingProofBrowser{}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Status:     "429 Too Many Requests",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)),
				Request:    req,
			}, nil
		})},
		BrowserRetry:          &fakeBrowserRetry{},
		rateLimitFallbackOpen: time.Now(),
	}

	_, err := chat.FetchContext(context.Background(), "hello")
	if err == nil || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("FetchContext() error = %v, want rate-limit classification", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("local HTTP requests = %d, want two before browser fallback", got)
	}
	if got := chat.BrowserRetry.(*fakeBrowserRetry).calls; got != 1 {
		t.Fatalf("browser retry calls = %d, want a new browser attempt for this prompt", got)
	}
}

func TestWaitRetryAfterStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- waitRetryAfter(ctx, time.Hour) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waitRetryAfter() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waitRetryAfter() did not stop after cancellation")
	}
}

func TestFetchContextRetriesRateLimitWithFreshLocalSession(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &countingProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			count := requests.Add(1)
			status := http.StatusTooManyRequests
			body := `{"type":"ERR_RATE_LIMIT"}`
			if count == 2 {
				status = http.StatusOK
				body = "data: [DONE]\n\n"
			}
			return &http.Response{
				StatusCode: status,
				Status:     http.StatusText(status),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		})},
	}

	response, err := chat.FetchContext(context.Background(), "make this image square")
	if err != nil {
		t.Fatalf("FetchContext() error = %v, want one successful retry", err)
	}
	_ = response.Body.Close()
	if got := requests.Load(); got != 2 {
		t.Fatalf("HTTP requests = %d, want 2 total requests", got)
	}
	if got := proof.captures.Load(); got != 2 {
		t.Fatalf("proof captures = %d, want a fresh proof for the retry", got)
	}
}

func TestFetchContextKeepsRateLimitFallbackWhenRetryProofCaptureFails(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &retryFailingProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Status:     http.StatusText(http.StatusTooManyRequests),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)),
				Request:    req,
			}, nil
		})},
	}

	_, err := chat.FetchContext(context.Background(), "hello")
	if err == nil || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("FetchContext() error = %v, want rate-limit classification for browser fallback", err)
	}
	if !strings.Contains(err.Error(), "retry failed") || !strings.Contains(err.Error(), "timed out waiting for Duck.ai chat headers") {
		t.Fatalf("FetchContext() error = %v, want the failed retry reason", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests = %d, want 1 because proof capture failed before the retry request", got)
	}
}

func TestProofCaptureFailureAfter429UsesBrowserSession(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &retryFailingProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	localRequests := 0
	browser := &fakeBrowserRetry{opened: true, response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"role\":\"assistant\",\"message\":\"PONG\"}\n\ndata: [DONE]\n\n")),
	}}
	chat := &Chat{
		Model:        models.GPT5Luna,
		BrowserRetry: browser,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			localRequests++
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)), Request: req}, nil
		})},
	}

	stream, streamErrors, err := chat.FetchStreamWithErrors(context.Background(), "ping")
	if err != nil {
		t.Fatalf("FetchStreamWithErrors() = %v, want browser session response", err)
	}
	var response strings.Builder
	for chunk := range stream {
		response.WriteString(chunk)
	}
	for streamErr := range streamErrors {
		if streamErr != nil {
			t.Fatalf("browser response stream error: %v", streamErr)
		}
	}
	if got := response.String(); got != "PONG" {
		t.Fatalf("response = %q, want PONG from the browser session", got)
	}
	if localRequests != 1 || browser.calls != 1 {
		t.Fatalf("local requests = %d, browser requests = %d; want one of each", localRequests, browser.calls)
	}
}

func TestFreshSessionProofCaptureHasItsOwnDeadline(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &blockingRetryProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	chat := &Chat{
		Model:             models.GPT5Luna,
		retryProofTimeout: 25 * time.Millisecond,
		BrowserRetry: &fakeBrowserRetry{opened: true, response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: {\"role\":\"assistant\",\"message\":\"PONG\"}\n\ndata: [DONE]\n\n")),
		}},
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"type":"ERR_RATE_LIMIT"}`)), Request: req}, nil
		})},
	}

	start := time.Now()
	stream, streamErrors, err := chat.FetchStreamWithErrors(context.Background(), "ping")
	if err != nil {
		t.Fatalf("FetchStreamWithErrors() = %v, want browser response after proof timeout", err)
	}
	var response strings.Builder
	for chunk := range stream {
		response.WriteString(chunk)
	}
	for streamErr := range streamErrors {
		if streamErr != nil {
			t.Fatal(streamErr)
		}
	}
	if got := response.String(); got != "PONG" {
		t.Fatalf("response = %q, want PONG", got)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("retry took %s despite its short proof deadline", elapsed)
	}
}

func TestCompletedStreamWithoutAssistantResponseIsFailure(t *testing.T) {
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		return &countingProofBrowser{}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	chat := &Chat{Model: models.GPT5Luna, Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")), Request: req}, nil
	})}}

	response, err := ProcessInputContext(context.Background(), chat, "ping", &config.Config{})
	if err == nil || response != "" {
		t.Fatalf("response = %q, error = %v; want an explicit empty-response failure", response, err)
	}
	if len(chat.Messages) != 0 {
		t.Fatalf("chat history = %+v, want failed prompt rolled back", chat.Messages)
	}
}

func TestFetchStreamReportsRateLimitRetryProgress(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &countingProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			count := requests.Add(1)
			status := http.StatusTooManyRequests
			body := `{"type":"ERR_RATE_LIMIT"}`
			if count == 2 {
				status = http.StatusOK
				body = "data: {\"type\":\"message\",\"message\":\"ok\"}\n\ndata: [DONE]\n\n"
			}
			return &http.Response{
				StatusCode: status,
				Status:     http.StatusText(status),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		})},
	}

	var progress []ProgressUpdate
	stream, streamErrors, err := chat.FetchStreamWithErrorsAndProgress(context.Background(), "hello", func(update ProgressUpdate) {
		progress = append(progress, update)
	})
	if err != nil {
		t.Fatalf("FetchStreamWithErrorsAndProgress() error = %v", err)
	}
	for range stream {
	}
	for err := range streamErrors {
		if err != nil {
			t.Fatalf("stream error = %v", err)
		}
	}

	want := []ProgressUpdate{
		{Stage: ui.ProgressPreparing, Label: "Clearing Duck.ai site data"},
		{Stage: ui.ProgressConnecting, Label: "Retrying after site data reset"},
	}
	for _, expected := range want {
		found := false
		for _, actual := range progress {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("progress updates %v do not contain retry state %+v", progress, expected)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("HTTP requests = %d, want initial request plus one retry", got)
	}
}

func TestFetchContextReturnsGenericBadRequestWithoutRetry(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &countingProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests.Add(1)
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Status:     "400 Bad Request",
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"type":"ERR_INVALID_MESSAGE","message":"invalid image content"}`)),
				Request:    req,
			}, nil
		})},
	}

	_, err := chat.FetchContext(context.Background(), "make this image square")
	if err == nil || !strings.Contains(err.Error(), "invalid image content") {
		t.Fatalf("FetchContext() error = %v, want original bad-request detail", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests = %d, want 1 for a generic bad request", got)
	}
	if got := proof.captures.Load(); got != 1 {
		t.Fatalf("proof captures = %d, want 1 when no retry is made", got)
	}
}

func TestFetchContextRetriesOnlyExplicitProofFailures(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &countingProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	var requests atomic.Int32
	chat := &Chat{
		Model: models.GPT5Luna,
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			count := requests.Add(1)
			body := `{"type":"ERR_INVALID_VQD"}`
			status := http.StatusBadRequest
			if count == 2 {
				status = http.StatusOK
				body = "data: [DONE]\n\n"
			}
			return &http.Response{
				StatusCode: status,
				Status:     http.StatusText(status),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		})},
	}

	resp, err := chat.FetchContext(context.Background(), "hello")
	if err != nil {
		t.Fatalf("FetchContext() error = %v", err)
	}
	_ = resp.Body.Close()
	if got := requests.Load(); got != 2 {
		t.Fatalf("HTTP requests = %d, want one retry for explicit invalid VQD", got)
	}
	if got := proof.captures.Load(); got != 2 {
		t.Fatalf("proof captures = %d, want one fresh proof per HTTP attempt", got)
	}
}
