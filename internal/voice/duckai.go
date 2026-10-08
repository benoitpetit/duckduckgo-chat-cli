package voice

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"duckduckgo-chat-cli/internal/chat"
)

const (
	defaultDuckAIBaseURL = "https://duck.ai"
	maxSignalingResponse = 2 << 20
	maxErrorResponse     = 64 << 10
)

type ICEConfiguration struct {
	ICEServers []ICEServer `json:"iceServers"`
}

type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

type ProofProvider interface {
	Capture(context.Context) (*chat.DynamicHeaders, error)
}

type CurrentProofProvider struct{}

func (CurrentProofProvider) Capture(ctx context.Context) (*chat.DynamicHeaders, error) {
	return chat.CaptureDynamicHeaders(ctx)
}

type SignalingClient interface {
	GetICEServers(context.Context, *chat.DynamicHeaders) (ICEConfiguration, error)
	CreateSession(context.Context, *chat.DynamicHeaders, string) (string, error)
}

type DuckAIClient struct {
	HTTPClient *http.Client
	BaseURL    string
}

// SignalingHTTPError carries a safe status, an optional machine code, and a
// validated Retry-After value. The response body, proof, and TURN credentials
// are never included in Error().
type SignalingHTTPError struct {
	Endpoint   string
	StatusCode int
	RemoteType string
	RetryAfter string
}

func (e *SignalingHTTPError) Error() string {
	if e.StatusCode == http.StatusTooManyRequests && e.RetryAfter != "" {
		return fmt.Sprintf("Duck.ai %s request failed with HTTP %d; Retry-After: %s", e.Endpoint, e.StatusCode, e.RetryAfter)
	}
	return fmt.Sprintf("Duck.ai %s request failed with HTTP %d", e.Endpoint, e.StatusCode)
}

func (c *DuckAIClient) GetICEServers(ctx context.Context, proof *chat.DynamicHeaders) (ICEConfiguration, error) {
	if err := validateProof(proof, false); err != nil {
		return ICEConfiguration{}, err
	}
	response, err := c.do(ctx, http.MethodGet, "/duckchat/v1/ice-servers", proof, false, "")
	if err != nil {
		return ICEConfiguration{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ICEConfiguration{}, signalingStatusError("ICE server", response)
	}
	body, err := readBounded(response.Body, maxSignalingResponse)
	if err != nil {
		return ICEConfiguration{}, fmt.Errorf("read Duck.ai ICE server response: %w", err)
	}
	var configuration ICEConfiguration
	if err := json.Unmarshal(body, &configuration); err != nil {
		return ICEConfiguration{}, fmt.Errorf("decode Duck.ai ICE server response: %w", err)
	}
	if len(configuration.ICEServers) == 0 {
		return ICEConfiguration{}, fmt.Errorf("Duck.ai ICE server response contained no servers")
	}
	return configuration, nil
}

func (c *DuckAIClient) CreateSession(ctx context.Context, proof *chat.DynamicHeaders, offer string) (string, error) {
	if err := validateProof(proof, true); err != nil {
		return "", err
	}
	response, err := c.do(ctx, http.MethodPost, "/duckchat/v1/session", proof, true, offer)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", signalingStatusError("session", response)
	}
	body, err := readBounded(response.Body, maxSignalingResponse)
	if err != nil {
		return "", fmt.Errorf("read Duck.ai session response: %w", err)
	}
	return string(body), nil
}

func validateProof(proof *chat.DynamicHeaders, requireVQD bool) error {
	if proof == nil || proof.FeSignals == "" || proof.FeVersion == "" {
		return fmt.Errorf("Duck.ai proof headers are incomplete")
	}
	if requireVQD && proof.VqdHash1 == "" {
		return fmt.Errorf("Duck.ai session proof is unavailable")
	}
	return nil
}

func (c *DuckAIClient) do(ctx context.Context, method, path string, proof *chat.DynamicHeaders, requireVQD bool, body string) (*http.Response, error) {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = defaultDuckAIBaseURL
	}
	parsedBase, err := url.Parse(base)
	if err != nil || parsedBase.Scheme == "" || parsedBase.Host == "" {
		return nil, fmt.Errorf("invalid Duck.ai base URL")
	}
	endpoint := base + path
	var requestBody io.Reader
	if method == http.MethodPost {
		requestBody = strings.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, requestBody)
	if err != nil {
		return nil, fmt.Errorf("create Duck.ai signaling request: %w", err)
	}
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Referer", parsedBase.Scheme+"://"+parsedBase.Host+"/")
	request.Header.Set("X-Fe-Signals", proof.FeSignals)
	request.Header.Set("X-Fe-Version", proof.FeVersion)
	if proof.UserAgent != "" {
		request.Header.Set("User-Agent", proof.UserAgent)
	}
	if proof.JourneyID != "" {
		request.Header.Set("X-DDG-Journey-ID", proof.JourneyID)
	}
	if requireVQD {
		request.Header.Set("Origin", parsedBase.Scheme+"://"+parsedBase.Host)
		request.Header.Set("X-Vqd-Hash-1", proof.VqdHash1)
		request.Header.Set("Content-Type", "application/sdp")
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	// Never forward a live proof header to a redirect destination. Retain the
	// injected transport and timeout while refusing redirects for this request.
	safeClient := &http.Client{
		Transport:     client.Transport,
		Jar:           client.Jar,
		Timeout:       client.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return safeClient.Do(request)
}

func signalingStatusError(endpoint string, response *http.Response) error {
	var payload struct {
		Type string `json:"type"`
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorResponse))
	_ = json.Unmarshal(body, &payload)
	if payload.Type == "" {
		for _, proofType := range []string{"ERR_INVALID_VQD", "ERR_CHALLENGE"} {
			if strings.Contains(string(body), proofType) {
				payload.Type = proofType
				break
			}
		}
	}
	return &SignalingHTTPError{
		Endpoint:   endpoint,
		StatusCode: response.StatusCode,
		RemoteType: payload.Type,
		RetryAfter: normalizeRetryAfter(response.Header.Get("Retry-After")),
	}
}

func normalizeRetryAfter(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if digitsOnly(value) {
		if _, err := strconv.ParseUint(value, 10, 64); err == nil {
			return value
		}
		return ""
	}
	retryAt, err := http.ParseTime(value)
	if err != nil {
		return ""
	}
	return retryAt.UTC().Format(http.TimeFormat)
}

func digitsOnly(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return len(value) > 0
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, nil
}
