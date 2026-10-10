package browserrelay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRelayStreamsOneAuthenticatedBrowserResponse(t *testing.T) {
	var relayEndpoint, relayToken string
	simulationDone := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-simulationDone:
		case <-time.After(2 * time.Second):
			t.Error("simulated browser did not finish before test cleanup")
		}
	})
	listener := newPipeListener()
	client := pipeHTTPClient(listener)
	relay := New(Options{
		ListenAddress: "127.0.0.1:0",
		WaitTimeout:   time.Second,
		Listen:        func(string, string) (net.Listener, error) { return listener, nil },
		OpenBrowser: func(target string) error {
			values, err := url.ParseQuery(strings.TrimPrefix(target[strings.Index(target, "#")+1:], "duckchat-relay="))
			if err != nil {
				return err
			}
			relayEndpoint = values.Get("endpoint")
			relayToken = values.Get("token")
			go func() {
				defer close(simulationDone)
				simulateExtension(t, client, relayEndpoint, relayToken, func(job relayJob) {
					if job.URL != "https://duck.ai/duckchat/v1/chat" || job.Method != http.MethodPost {
						t.Errorf("relayed request target = %s %s", job.Method, job.URL)
					}
					if job.Body != `{"message":"hello"}` {
						t.Errorf("relayed request body = %q", job.Body)
					}
					if job.Header.Get("x-vqd-hash-1") != "fresh-proof" {
						t.Errorf("proof header = %q", job.Header.Get("x-vqd-hash-1"))
					}
					for _, forbidden := range []string{"Cookie", "Origin", "User-Agent", "Sec-Fetch-Site"} {
						if value := job.Header.Get(forbidden); value != "" {
							t.Errorf("browser-owned header %s was relayed: %q", forbidden, value)
						}
					}
				})
			}()
			return nil
		},
	})

	response, opened, err := relay.Do(context.Background(), Request{
		URL:    "https://duck.ai/duckchat/v1/chat",
		Method: http.MethodPost,
		Header: http.Header{
			"Content-Type":   {"application/json"},
			"Accept":         {"text/event-stream"},
			"X-Vqd-Hash-1":   {"fresh-proof"},
			"Origin":         {"https://duck.ai"},
			"User-Agent":     {"CLI test agent"},
			"Cookie":         {"session=must-not-leave-Chrome"},
			"Sec-Fetch-Site": {"same-origin"},
		},
		Body: []byte(`{"message":"hello"}`),
	})
	if err != nil {
		t.Fatalf("Relay.Do() error = %v", err)
	}
	if !opened {
		t.Fatal("Relay.Do() opened = false, want true")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("response status = %d, want 200", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("response content type = %q", got)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read relayed stream: %v", err)
	}
	if got, want := string(body), "data: first\n\ndata: second\n\n"; got != want {
		t.Fatalf("relayed body = %q, want %q", got, want)
	}
	if relayEndpoint == "" || relayToken == "" {
		t.Fatal("browser opener did not receive relay endpoint and one-use token")
	}
}

func TestAuthenticatedExtensionGETMayOmitOrigin(t *testing.T) {
	session := &relaySession{
		token: "local-secret", expectedHost: "127.0.0.1:8765",
		jobs: make(chan relayJob, 1), connected: make(chan struct{}, 1),
	}
	session.jobs <- relayJob{ID: "job"}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/next", nil)
	request.Header.Set("Authorization", "Bearer local-secret")
	response := httptest.NewRecorder()
	session.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated extension GET without Origin = %d, want 200", response.Code)
	}
	request = httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:8765/v1/next", nil)
	response = httptest.NewRecorder()
	session.handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("preflight without Origin = %d, want 403", response.Code)
	}
}

func TestLaunchPageRedirectsToDuckAIWithoutExtensionID(t *testing.T) {
	session := &relaySession{expectedHost: "127.0.0.1:8765"}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/v1/launch", nil)
	response := httptest.NewRecorder()
	session.handler().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("launch page = %d, want 200", response.Code)
	}
	if strings.Contains(response.Body.String(), "chrome-extension://") {
		t.Fatal("launch page still probes extension IDs")
	}
	if !strings.Contains(response.Body.String(), `location.replace("https://duck.ai/" + location.hash)`) {
		t.Fatal("launch page does not pass relay fragment to Duck.ai")
	}
}

func TestRelayRejectsWrongTokenAndRequestID(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opened := make(chan string, 1)
	listener := newPipeListener()
	client := pipeHTTPClient(listener)
	relay := New(Options{
		ListenAddress: "127.0.0.1:0",
		WaitTimeout:   time.Second,
		Listen:        func(string, string) (net.Listener, error) { return listener, nil },
		OpenBrowser: func(target string) error {
			opened <- target
			return nil
		},
	})
	go func() {
		_, _, _ = relay.Do(ctx, Request{URL: "https://duck.ai/duckchat/v1/chat", Method: http.MethodPost, Body: []byte("{}")})
	}()

	var target string
	select {
	case target = <-opened:
	case <-time.After(time.Second):
		t.Fatal("browser opener was not called")
	}
	values, err := url.ParseQuery(strings.TrimPrefix(target[strings.Index(target, "#")+1:], "duckchat-relay="))
	if err != nil {
		t.Fatal(err)
	}
	endpoint, token := values.Get("endpoint"), values.Get("token")
	wrongTokenResponse := relayRequestWithClient(t, client, http.MethodGet, endpoint+"/v1/next", "wrong-token", nil)
	defer wrongTokenResponse.Body.Close()
	if wrongTokenResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-token status = %d, want 403", wrongTokenResponse.StatusCode)
	}
	wrongID := relayResponseStart{ID: "not-the-active-request", Status: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}}
	wrongIDResponse := relayJSONRequestWithClient(t, client, endpoint+"/v1/start", token, wrongID)
	defer wrongIDResponse.Body.Close()
	if wrongIDResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong-ID status = %d, want 404", wrongIDResponse.StatusCode)
	}
	cancel()
}

func TestRelayCancellationStopsWaitingForExtension(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	opened := make(chan struct{}, 1)
	listener := newPipeListener()
	relay := New(Options{
		ListenAddress: "127.0.0.1:0",
		WaitTimeout:   time.Minute,
		Listen:        func(string, string) (net.Listener, error) { return listener, nil },
		OpenBrowser: func(string) error {
			opened <- struct{}{}
			return nil
		},
	})
	result := make(chan error, 1)
	go func() {
		_, _, err := relay.Do(ctx, Request{URL: "https://duck.ai/duckchat/v1/chat", Method: http.MethodPost, Body: []byte("{}")})
		result <- err
	}()
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("browser opener was not called")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Relay.Do() error = nil after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("Relay.Do() did not stop after context cancellation")
	}
}

func TestRelayWaitsLongerAfterExtensionConnects(t *testing.T) {
	listener := newPipeListener()
	client := pipeHTTPClient(listener)
	relay := New(Options{
		ListenAddress: "127.0.0.1:0",
		WaitTimeout:   20 * time.Millisecond,
		Listen:        func(string, string) (net.Listener, error) { return listener, nil },
		OpenBrowser: func(target string) error {
			values, err := url.ParseQuery(strings.TrimPrefix(target[strings.Index(target, "#")+1:], "duckchat-relay="))
			if err != nil {
				return err
			}
			go func() {
				endpoint, token := values.Get("endpoint"), values.Get("token")
				response := relayRequestWithClient(t, client, http.MethodGet, endpoint+"/v1/next", token, nil)
				var job relayJob
				if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&job) != nil {
					t.Errorf("extension could not receive retry job: %d", response.StatusCode)
					_ = response.Body.Close()
					return
				}
				_ = response.Body.Close()
				time.Sleep(60 * time.Millisecond)
				response = relayJSONRequestWithClient(t, client, endpoint+"/v1/start", token, relayResponseStart{
					ID: job.ID, Status: http.StatusOK, Header: make(http.Header),
				})
				_ = response.Body.Close()
			}()
			return nil
		},
	})
	response, _, err := relay.Do(context.Background(), Request{URL: "https://duck.ai/duckchat/v1/chat", Method: http.MethodPost, Body: []byte("{}")})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
}

func TestRelayCancellationClosesStartedResponseStream(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener := newPipeListener()
	client := pipeHTTPClient(listener)
	relay := New(Options{
		ListenAddress: "127.0.0.1:0",
		WaitTimeout:   time.Second,
		Listen:        func(string, string) (net.Listener, error) { return listener, nil },
		OpenBrowser: func(target string) error {
			values, err := url.ParseQuery(strings.TrimPrefix(target[strings.Index(target, "#")+1:], "duckchat-relay="))
			if err != nil {
				return err
			}
			endpoint, token := values.Get("endpoint"), values.Get("token")
			go func() {
				response := relayRequestWithClient(t, client, http.MethodGet, endpoint+"/v1/next", token, nil)
				var job relayJob
				if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&job) != nil {
					_ = response.Body.Close()
					return
				}
				_ = response.Body.Close()
				response = relayJSONRequestWithClient(t, client, endpoint+"/v1/start", token, relayResponseStart{
					ID: job.ID, Status: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
				})
				_ = response.Body.Close()
			}()
			return nil
		},
	})

	response, opened, err := relay.Do(ctx, Request{URL: "https://duck.ai/duckchat/v1/chat", Method: http.MethodPost, Body: []byte("{}")})
	if err != nil {
		t.Fatalf("Relay.Do() error = %v", err)
	}
	if !opened {
		t.Fatal("Relay.Do() opened = false, want true")
	}
	cancel()
	_, err = io.ReadAll(response.Body)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("reading canceled stream returned %v, want context.Canceled", err)
	}
	_ = response.Body.Close()
}

func simulateExtension(t *testing.T, client *http.Client, endpoint, token string, inspect func(relayJob)) {
	t.Helper()
	response := relayRequestWithClient(t, client, http.MethodGet, endpoint+"/v1/next", token, nil)
	if response.StatusCode != http.StatusOK {
		t.Errorf("extension poll status = %d, want 200", response.StatusCode)
		return
	}
	var job relayJob
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		_ = response.Body.Close()
		t.Errorf("decode browser job: %v", err)
		return
	}
	_ = response.Body.Close()
	inspect(job)
	ping := relayRequestWithClient(t, client, http.MethodGet, endpoint+"/v1/ping", token, nil)
	if ping.StatusCode != http.StatusNoContent {
		t.Errorf("relay heartbeat status = %d, want 204", ping.StatusCode)
	}
	_ = ping.Body.Close()
	duplicate := relayRequestWithClient(t, client, http.MethodGet, endpoint+"/v1/next", token, nil)
	if duplicate.StatusCode != http.StatusNoContent {
		t.Errorf("second extension poll status = %d, want 204 after one-use job delivery", duplicate.StatusCode)
	}
	_ = duplicate.Body.Close()

	started := relayResponseStart{ID: job.ID, Status: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Vqd-4": {"next-vqd"}}}
	response = relayJSONRequestWithClient(t, client, endpoint+"/v1/start", token, started)
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("start status = %d, want 204", response.StatusCode)
	}
	_ = response.Body.Close()
	for _, data := range []string{"data: first\n\n", "data: second\n\n"} {
		response = relayJSONRequestWithClient(t, client, endpoint+"/v1/chunk", token, relayResponseChunk{ID: job.ID, Data: base64.StdEncoding.EncodeToString([]byte(data))})
		if response.StatusCode != http.StatusNoContent {
			t.Errorf("chunk status = %d, want 204", response.StatusCode)
		}
		_ = response.Body.Close()
	}
	response = relayJSONRequestWithClient(t, client, endpoint+"/v1/finish", token, relayResponseFinish{ID: job.ID})
	if response.StatusCode != http.StatusNoContent {
		t.Errorf("finish status = %d, want 204", response.StatusCode)
	}
	_ = response.Body.Close()
}

func relayJSONRequest(t *testing.T, target, token string, payload any) *http.Response {
	t.Helper()
	return relayJSONRequestWithClient(t, &http.Client{Timeout: time.Second}, target, token, payload)
}

func relayJSONRequestWithClient(t *testing.T, client *http.Client, target, token string, payload any) *http.Response {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return relayRequestWithClient(t, client, http.MethodPost, target, token, strings.NewReader(string(data)))
}

func relayRequest(t *testing.T, method, target, token string, body io.Reader) *http.Response {
	t.Helper()
	return relayRequestWithClient(t, &http.Client{Timeout: time.Second}, method, target, token, body)
}

func relayRequestWithClient(t *testing.T, client *http.Client, method, target, token string, body io.Reader) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Origin", "chrome-extension://test-extension")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
	address     *net.TCPAddr
}

func newPipeListener() *pipeListener {
	return &pipeListener{
		connections: make(chan net.Conn),
		closed:      make(chan struct{}),
		address:     &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 43210},
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return l.address }

func (l *pipeListener) Dial() (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.connections <- server:
		return client, nil
	case <-l.closed:
		_ = client.Close()
		_ = server.Close()
		return nil, net.ErrClosed
	}
}

func pipeHTTPClient(listener *pipeListener) *http.Client {
	return &http.Client{
		Timeout: time.Second,
		Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
			return listener.Dial()
		}},
	}
}
