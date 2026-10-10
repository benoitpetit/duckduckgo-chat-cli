package voice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"duckduckgo-chat-cli/internal/chat"
)

const (
	testVoiceToken  = "test-voice-token"
	testVoiceOrigin = "http://127.0.0.1:4321"
)

type testProofProvider struct {
	mu     sync.Mutex
	calls  int
	proofs []*chat.DynamicHeaders
	err    error
}

func (p *testProofProvider) Capture(context.Context) (*chat.DynamicHeaders, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	if len(p.proofs) == 0 {
		return testProof("proof-default"), nil
	}
	index := p.calls - 1
	if index >= len(p.proofs) {
		index = len(p.proofs) - 1
	}
	proof := *p.proofs[index]
	return &proof, nil
}

func (p *testProofProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type testSignalingClient struct {
	mu              sync.Mutex
	iceConfig       ICEConfiguration
	iceError        error
	iceCalls        int
	iceJourneyIDs   []string
	sessionErrors   []error
	sessionCalls    int
	sessionOffers   []string
	sessionProofs   []string
	sessionJourneys []string
	sessionResponse string
}

func (s *testSignalingClient) GetICEServers(_ context.Context, proof *chat.DynamicHeaders) (ICEConfiguration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.iceCalls++
	if proof != nil {
		s.iceJourneyIDs = append(s.iceJourneyIDs, proof.JourneyID)
	}
	if s.iceError != nil {
		return ICEConfiguration{}, s.iceError
	}
	return s.iceConfig, nil
}

func (s *testSignalingClient) CreateSession(_ context.Context, proof *chat.DynamicHeaders, offer string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionCalls++
	s.sessionOffers = append(s.sessionOffers, offer)
	if proof != nil {
		s.sessionProofs = append(s.sessionProofs, proof.VqdHash1)
		s.sessionJourneys = append(s.sessionJourneys, proof.JourneyID)
	}
	index := s.sessionCalls - 1
	if index < len(s.sessionErrors) && s.sessionErrors[index] != nil {
		return "", s.sessionErrors[index]
	}
	return s.sessionResponse, nil
}

func (s *testSignalingClient) counts() (ice, session int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iceCalls, s.sessionCalls
}

func (s *testSignalingClient) sessionProofValues() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.sessionProofs...)
}

func (s *testSignalingClient) journeyValues() (ice, session []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.iceJourneyIDs...), append([]string(nil), s.sessionJourneys...)
}

func testProof(vqd string) *chat.DynamicHeaders {
	return &chat.DynamicHeaders{
		FeSignals: "test-signals",
		FeVersion: "test-version",
		VqdHash1:  vqd,
		UserAgent: "test-agent",
	}
}

func newTestHandler(t *testing.T, proof ProofProvider, signaling SignalingClient) http.Handler {
	t.Helper()
	handler, err := NewHandler(Dependencies{Proof: proof, Signaling: signaling}, testVoiceToken, testVoiceOrigin)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	return handler
}

func doVoiceRequest(handler http.Handler, method, path, token, origin, contentType, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Host = "127.0.0.1:4321"
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func bootstrapTestICE(handler http.Handler) *httptest.ResponseRecorder {
	return doVoiceRequest(handler, http.MethodGet, "/api/ice-servers", testVoiceToken, testVoiceOrigin, "", "")
}

func TestHandlerRejectsUnauthorizedOrForeignOrigin(t *testing.T) {
	proof := &testProofProvider{}
	signaling := &testSignalingClient{}
	handler := newTestHandler(t, proof, signaling)
	for _, tc := range []struct {
		name   string
		token  string
		origin string
	}{
		{name: "missing token", origin: testVoiceOrigin},
		{name: "wrong token", token: "wrong-token", origin: testVoiceOrigin},
		{name: "foreign origin", token: testVoiceToken, origin: "http://127.0.0.1.evil.test:4321"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := doVoiceRequest(handler, http.MethodGet, "/api/ice-servers", tc.token, tc.origin, "", "")
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
			}
		})
	}
	hostRequest := httptest.NewRequest(http.MethodGet, "/api/ice-servers", nil)
	hostRequest.Host = "attacker.example"
	hostRequest.Header.Set("Authorization", "Bearer "+testVoiceToken)
	hostRequest.Header.Set("Origin", testVoiceOrigin)
	hostResponse := httptest.NewRecorder()
	handler.ServeHTTP(hostResponse, hostRequest)
	if hostResponse.Code != http.StatusForbidden {
		t.Fatalf("mismatched Host status = %d, want %d", hostResponse.Code, http.StatusForbidden)
	}
	if got := proof.callCount(); got != 0 {
		t.Fatalf("proof captures = %d, want 0", got)
	}
	if ice, session := signaling.counts(); ice != 0 || session != 0 {
		t.Fatalf("signaling calls = ICE %d, session %d; want zero", ice, session)
	}
}

func TestHandlerSessionTerminationNotificationKeepsVoiceWindowAlive(t *testing.T) {
	handler := newTestHandler(t, &testProofProvider{}, &testSignalingClient{})
	local := handler.(*localHandler)
	for i := 0; i < 2; i++ {
		response := doVoiceRequest(handler, http.MethodPost, "/api/session-terminated", testVoiceToken, testVoiceOrigin, "", "")
		if response.Code != http.StatusNoContent {
			t.Fatalf("session termination notification status = %d, want %d", response.Code, http.StatusNoContent)
		}
	}
	select {
	case <-local.stop:
		t.Fatal("session termination notification stopped the voice window")
	default:
	}
}

func TestHandlerReportsEverySessionTermination(t *testing.T) {
	notifications := 0
	handler, err := NewHandler(Dependencies{
		Proof:     &testProofProvider{},
		Signaling: &testSignalingClient{},
		OnSessionTerminated: func() {
			notifications++
		},
	}, testVoiceToken, testVoiceOrigin)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	for i := 0; i < 2; i++ {
		response := doVoiceRequest(handler, http.MethodPost, "/api/session-terminated", testVoiceToken, testVoiceOrigin, "", "")
		if response.Code != http.StatusNoContent {
			t.Fatalf("session termination notification status = %d, want %d", response.Code, http.StatusNoContent)
		}
	}
	if notifications != 2 {
		t.Fatalf("CLI notifications = %d, want one per ended voice session", notifications)
	}
}

func TestHandlerServesICEAndNegotiatesSDP(t *testing.T) {
	proof := &testProofProvider{proofs: []*chat.DynamicHeaders{testProof("proof-one")}}
	signaling := &testSignalingClient{
		iceConfig:       ICEConfiguration{ICEServers: []ICEServer{{URLs: []string{"turn:turn.example:3478"}, Username: "temporary", Credential: "temporary"}}},
		sessionResponse: "v=0\r\no=- answer\r\n",
	}
	handler := newTestHandler(t, proof, signaling)

	iceResponse := bootstrapTestICE(handler)
	if iceResponse.Code != http.StatusOK || !strings.Contains(iceResponse.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("ICE response = %d %q, want 200 JSON", iceResponse.Code, iceResponse.Header().Get("Content-Type"))
	}
	var config ICEConfiguration
	if err := json.Unmarshal(iceResponse.Body.Bytes(), &config); err != nil {
		t.Fatalf("decode ICE response: %v", err)
	}
	if len(config.ICEServers) != 1 || config.ICEServers[0].Username != "temporary" {
		t.Fatalf("ICE response = %+v, want the injected configuration", config)
	}

	const offer = "v=0\r\no=- offer\r\n"
	sessionResponse := doVoiceRequest(handler, http.MethodPost, "/api/session", testVoiceToken, testVoiceOrigin, "application/sdp", offer)
	if sessionResponse.Code != http.StatusOK || sessionResponse.Header().Get("Content-Type") != "application/sdp" {
		t.Fatalf("session response = %d %q, want 200 application/sdp", sessionResponse.Code, sessionResponse.Header().Get("Content-Type"))
	}
	answer, err := io.ReadAll(sessionResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(answer) != signaling.sessionResponse {
		t.Fatalf("SDP answer = %q, want %q", answer, signaling.sessionResponse)
	}
	if got := proof.callCount(); got != 1 {
		t.Fatalf("proof captures = %d, want one shared proof", got)
	}
	if ice, session := signaling.counts(); ice != 1 || session != 1 {
		t.Fatalf("signaling calls = ICE %d, session %d; want one each", ice, session)
	}
	if got := signaling.sessionProofValues(); len(got) != 1 || got[0] != "proof-one" {
		t.Fatalf("session proofs = %v, want [proof-one]", got)
	}
	iceJourneys, sessionJourneys := signaling.journeyValues()
	if len(iceJourneys) != 1 || len(sessionJourneys) != 1 || iceJourneys[0] == "" || iceJourneys[0] != sessionJourneys[0] {
		t.Fatalf("ICE/session journey IDs = %v / %v, want one stable non-empty ID", iceJourneys, sessionJourneys)
	}
}

func TestSessionRefreshesRejectedProofOnlyOnce(t *testing.T) {
	t.Run("explicit proof rejection retries once", func(t *testing.T) {
		proof := &testProofProvider{proofs: []*chat.DynamicHeaders{testProof("proof-one"), testProof("proof-two")}}
		signaling := &testSignalingClient{
			sessionErrors: []error{
				&SignalingHTTPError{Endpoint: "session", StatusCode: http.StatusBadRequest, RemoteType: "ERR_CHALLENGE"},
				&SignalingHTTPError{Endpoint: "session", StatusCode: http.StatusBadRequest, RemoteType: "ERR_CHALLENGE"},
			},
		}
		handler := newTestHandler(t, proof, signaling)
		if response := bootstrapTestICE(handler); response.Code != http.StatusOK {
			t.Fatalf("ICE bootstrap status = %d, want 200", response.Code)
		}
		response := doVoiceRequest(handler, http.MethodPost, "/api/session", testVoiceToken, testVoiceOrigin, "application/sdp", "v=0\r\n")
		if response.Code != http.StatusBadGateway {
			t.Fatalf("session status = %d, want %d", response.Code, http.StatusBadGateway)
		}
		if got := proof.callCount(); got != 2 {
			t.Fatalf("proof captures = %d, want one refresh", got)
		}
		if _, got := signaling.counts(); got != 2 {
			t.Fatalf("session attempts = %d, want two", got)
		}
		if got := signaling.sessionProofValues(); len(got) != 2 || got[0] != "proof-one" || got[1] != "proof-two" {
			t.Fatalf("proofs used for session attempts = %v, want [proof-one proof-two]", got)
		}
		iceJourneys, sessionJourneys := signaling.journeyValues()
		if len(iceJourneys) != 1 || len(sessionJourneys) != 2 || sessionJourneys[0] != iceJourneys[0] || sessionJourneys[1] != iceJourneys[0] {
			t.Fatalf("journey IDs after proof refresh = %v / %v, want one stable ID", iceJourneys, sessionJourneys)
		}
	})

	for _, tc := range []struct {
		name           string
		err            error
		wantStatus     int
		wantRetryAfter string
	}{
		{name: "rate limit", err: &SignalingHTTPError{Endpoint: "session", StatusCode: http.StatusTooManyRequests, RetryAfter: "45"}, wantStatus: http.StatusTooManyRequests, wantRetryAfter: "45"},
		{name: "unknown error", err: errors.New("network unavailable"), wantStatus: http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proof := &testProofProvider{proofs: []*chat.DynamicHeaders{testProof("proof-one"), testProof("proof-two")}}
			signaling := &testSignalingClient{sessionErrors: []error{tc.err}}
			handler := newTestHandler(t, proof, signaling)
			if response := bootstrapTestICE(handler); response.Code != http.StatusOK {
				t.Fatalf("ICE bootstrap status = %d, want 200", response.Code)
			}
			response := doVoiceRequest(handler, http.MethodPost, "/api/session", testVoiceToken, testVoiceOrigin, "application/sdp", "v=0\r\n")
			if response.Code != tc.wantStatus {
				t.Fatalf("session status = %d, want %d", response.Code, tc.wantStatus)
			}
			if got := response.Header().Get("Retry-After"); got != tc.wantRetryAfter {
				t.Fatalf("Retry-After = %q, want %q", got, tc.wantRetryAfter)
			}
			if tc.wantRetryAfter != "" && !strings.Contains(response.Body.String(), tc.wantRetryAfter) {
				t.Fatalf("rate-limit response body %q does not include retry delay %q", response.Body.String(), tc.wantRetryAfter)
			}
			if got := proof.callCount(); got != 1 {
				t.Fatalf("proof captures = %d, want no refresh", got)
			}
			if _, got := signaling.counts(); got != 1 {
				t.Fatalf("session attempts = %d, want one", got)
			}
		})
	}
}

func TestSessionRefreshesProofForLegacyVQDRejection(t *testing.T) {
	proof := &testProofProvider{proofs: []*chat.DynamicHeaders{testProof("proof-one"), testProof("proof-two")}}
	signaling := &testSignalingClient{sessionErrors: []error{
		&SignalingHTTPError{Endpoint: "session", StatusCode: http.StatusTeapot},
		nil,
	}}
	handler := newTestHandler(t, proof, signaling)
	if response := bootstrapTestICE(handler); response.Code != http.StatusOK {
		t.Fatalf("ICE bootstrap status = %d, want 200", response.Code)
	}
	response := doVoiceRequest(handler, http.MethodPost, "/api/session", testVoiceToken, testVoiceOrigin, "application/sdp", "v=0\r\n")
	if response.Code != http.StatusOK {
		t.Fatalf("session status = %d, want 200 after one proof refresh", response.Code)
	}
	if got := proof.callCount(); got != 2 {
		t.Fatalf("proof captures = %d, want two", got)
	}
	if _, got := signaling.counts(); got != 2 {
		t.Fatalf("session attempts = %d, want two", got)
	}
}

func TestSessionRejectsInvalidOrOversizedSDP(t *testing.T) {
	proof := &testProofProvider{}
	signaling := &testSignalingClient{}
	handler := newTestHandler(t, proof, signaling)
	if response := bootstrapTestICE(handler); response.Code != http.StatusOK {
		t.Fatalf("ICE bootstrap status = %d, want 200", response.Code)
	}
	for _, tc := range []struct {
		name        string
		contentType string
		body        string
		wantStatus  int
	}{
		{name: "wrong content type", contentType: "application/json", body: `{"sdp":"v=0"}`, wantStatus: http.StatusBadRequest},
		{name: "missing SDP version", contentType: "application/sdp", body: "o=- invalid\r\n", wantStatus: http.StatusBadRequest},
		{name: "empty SDP", contentType: "application/sdp", body: "", wantStatus: http.StatusBadRequest},
		{name: "oversized SDP", contentType: "application/sdp", body: "v=0\r\n" + strings.Repeat("x", 1<<20), wantStatus: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := doVoiceRequest(handler, http.MethodPost, "/api/session", testVoiceToken, testVoiceOrigin, tc.contentType, tc.body)
			if response.Code != tc.wantStatus {
				t.Fatalf("session status = %d, want %d", response.Code, tc.wantStatus)
			}
		})
	}
	if _, got := signaling.counts(); got != 0 {
		t.Fatalf("CreateSession calls = %d, want 0 for invalid SDP", got)
	}
}
