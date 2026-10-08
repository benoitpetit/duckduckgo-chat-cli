package voice

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"duckduckgo-chat-cli/internal/chat"
)

func TestGetICEServersSendsCurrentProofHeaders(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/duckchat/v1/ice-servers" {
			t.Errorf("request = %s %s, want GET /duckchat/v1/ice-servers", r.Method, r.URL.Path)
		}
		for name, want := range map[string]string{
			"X-Fe-Signals":     "signals-value",
			"X-Fe-Version":     "version-value",
			"X-DDG-Journey-ID": "journey-value",
			"User-Agent":       "DuckAI test browser",
			"Referer":          server.URL + "/",
		} {
			if got := r.Header.Get(name); got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
		if got := r.Header.Get("X-Vqd-Hash-1"); got != "" {
			t.Errorf("X-Vqd-Hash-1 on ICE request = %q, want empty", got)
		}
		if got := r.Header.Get("Origin"); got != "" {
			t.Errorf("Origin on ICE request = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"iceServers":[{"urls":["turn:turn.example:3478?transport=udp"],"username":"ephemeral-user","credential":"ephemeral-secret"}]}`)
	}))
	defer server.Close()

	client := &DuckAIClient{HTTPClient: server.Client(), BaseURL: server.URL}
	proof := &chat.DynamicHeaders{
		FeSignals: "signals-value",
		FeVersion: "version-value",
		VqdHash1:  "session-only-proof",
		JourneyID: "journey-value",
		UserAgent: "DuckAI test browser",
	}
	got, err := client.GetICEServers(context.Background(), proof)
	if err != nil {
		t.Fatalf("GetICEServers() error = %v", err)
	}
	if len(got.ICEServers) != 1 {
		t.Fatalf("ICE server count = %d, want 1", len(got.ICEServers))
	}
	ice := got.ICEServers[0]
	if len(ice.URLs) != 1 || ice.URLs[0] != "turn:turn.example:3478?transport=udp" || ice.Username != "ephemeral-user" || ice.Credential != "ephemeral-secret" {
		t.Fatalf("decoded ICE server = %+v, unexpected fields", ice)
	}
}

func TestCreateSessionSendsSDPOfferAndReturnsSDPAnswer(t *testing.T) {
	const offer = "v=0\r\no=- offer\r\n"
	const answer = "v=0\r\no=- answer\r\n"
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/duckchat/v1/session" {
			t.Errorf("request = %s %s, want POST /duckchat/v1/session", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/sdp" {
			t.Errorf("Content-Type = %q, want application/sdp", got)
		}
		for name, want := range map[string]string{
			"X-Vqd-Hash-1":     "session-proof",
			"X-Fe-Signals":     "signals-value",
			"X-Fe-Version":     "version-value",
			"X-DDG-Journey-ID": "journey-value",
			"User-Agent":       "DuckAI test browser",
			"Origin":           server.URL,
			"Referer":          server.URL + "/",
		} {
			if got := r.Header.Get(name); got != want {
				t.Errorf("%s = %q, want %q", name, got, want)
			}
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read offer: %v", err)
		}
		if string(body) != offer {
			t.Errorf("SDP offer = %q, want %q", body, offer)
		}
		w.Header().Set("Content-Type", "application/sdp")
		_, _ = io.WriteString(w, answer)
	}))
	defer server.Close()

	client := &DuckAIClient{HTTPClient: server.Client(), BaseURL: server.URL}
	proof := &chat.DynamicHeaders{
		FeSignals: "signals-value",
		FeVersion: "version-value",
		VqdHash1:  "session-proof",
		JourneyID: "journey-value",
		UserAgent: "DuckAI test browser",
	}
	got, err := client.CreateSession(context.Background(), proof, offer)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if got != answer {
		t.Fatalf("CreateSession() answer = %q, want %q", got, answer)
	}
}

func TestDuckAIClientDoesNotIncludeRemoteErrorBodyInError(t *testing.T) {
	const secret = "ephemeral-proof-or-turn-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, secret)
	}))
	defer server.Close()

	client := &DuckAIClient{HTTPClient: server.Client(), BaseURL: server.URL}
	proof := &chat.DynamicHeaders{FeSignals: "signals", FeVersion: "version", VqdHash1: "proof"}
	_, err := client.CreateSession(context.Background(), proof, "v=0\r\n")
	if err == nil {
		t.Fatal("CreateSession() error = nil, want HTTP failure")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("CreateSession() error leaked response body: %v", err)
	}
}

func TestDuckAIClientPreservesRetryAfterForRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "45")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := &DuckAIClient{HTTPClient: server.Client(), BaseURL: server.URL}
	proof := &chat.DynamicHeaders{FeSignals: "signals", FeVersion: "version", VqdHash1: "proof"}
	_, err := client.CreateSession(context.Background(), proof, "v=0\r\n")
	var remote *SignalingHTTPError
	if !errors.As(err, &remote) {
		t.Fatalf("CreateSession() error = %v, want SignalingHTTPError", err)
	}
	if remote.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", remote.StatusCode, http.StatusTooManyRequests)
	}
	if remote.RetryAfter != "45" {
		t.Fatalf("RetryAfter = %q, want %q", remote.RetryAfter, "45")
	}
}

func TestCreateSessionDoesNotForwardProofHeadersAcrossRedirect(t *testing.T) {
	var redirectTargetCalled atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectTargetCalled.Store(true)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	client := &DuckAIClient{HTTPClient: origin.Client(), BaseURL: origin.URL}
	proof := &chat.DynamicHeaders{FeSignals: "signals", FeVersion: "version", VqdHash1: "sensitive-proof"}
	_, err := client.CreateSession(context.Background(), proof, "v=0\r\n")
	if err == nil {
		t.Fatal("CreateSession() error = nil, want redirect response failure")
	}
	if redirectTargetCalled.Load() {
		t.Fatal("redirect destination was contacted with proof headers")
	}
}
