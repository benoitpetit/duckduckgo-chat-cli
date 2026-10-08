package chat

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"duckduckgo-chat-cli/internal/models"

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

type countingProofBrowser struct{ captures atomic.Int32 }

func (b *countingProofBrowser) Capture(context.Context) (*DynamicHeaders, error) {
	b.captures.Add(1)
	return &DynamicHeaders{VqdHash1: "proof", FeSignals: "signals", FeVersion: "version"}, nil
}

func (b *countingProofBrowser) Close() error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestFetchContextDoesNotRetryRateLimit(t *testing.T) {
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

	_, err := chat.FetchContext(context.Background(), "make this image square")
	if err == nil || !strings.Contains(err.Error(), "ERR_RATE_LIMIT") {
		t.Fatalf("FetchContext() error = %v, want rate-limit error", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests = %d, want 1 for a rate-limited request", got)
	}
	if got := proof.captures.Load(); got != 1 {
		t.Fatalf("proof captures = %d, want 1 when no retry is made", got)
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
