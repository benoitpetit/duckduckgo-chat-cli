package browserrelay

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	defaultWaitTimeout = 8 * time.Second
	responseStartWait  = 60 * time.Second
	defaultLifetime    = 11 * time.Minute
	maxRequestBody     = 32 << 20
	maxControlBody     = 1 << 20
	maxChunkBody       = 128 << 10
)

// Request contains the already-built Duck.ai request. Browser-managed headers
// are removed before the extension receives it; Chrome supplies those itself.
type Request struct {
	URL    string
	Method string
	Header http.Header
	Body   []byte
}

// BrowserOpener transfers the target URL to the user's system browser.
type BrowserOpener func(string) error

type Options struct {
	ListenAddress string
	WaitTimeout   time.Duration
	Lifetime      time.Duration
	OpenBrowser   BrowserOpener
	Listen        func(network, address string) (net.Listener, error)
}

// Relay runs one authenticated request through the user's Duck.ai page. Each
// invocation creates a fresh loopback listener and bearer token.
type Relay struct {
	options Options
	mu      sync.Mutex
	active  bool
}

// New creates a browser relay. With no opener configured it uses the system's
// default-browser launcher.
func New(options Options) *Relay {
	if options.ListenAddress == "" {
		options.ListenAddress = "127.0.0.1:0"
	}
	if options.WaitTimeout <= 0 {
		options.WaitTimeout = defaultWaitTimeout
	}
	if options.Lifetime <= 0 {
		options.Lifetime = defaultLifetime
	}
	if options.OpenBrowser == nil {
		options.OpenBrowser = openDefaultBrowser
	}
	return &Relay{options: options}
}

// Do opens Duck.ai in the default browser, then waits for the extension to
// execute the request in that page and relay its response stream. The returned
// boolean reports whether the browser was opened successfully.
func (r *Relay) Do(ctx context.Context, request Request) (*http.Response, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateRequest(request); err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	r.mu.Lock()
	if r.active {
		r.mu.Unlock()
		return nil, false, errors.New("a browser retry relay is already active")
	}
	r.active = true
	r.mu.Unlock()
	release := func() {
		r.mu.Lock()
		r.active = false
		r.mu.Unlock()
	}
	listen := r.options.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", r.options.ListenAddress)
	if err != nil {
		release()
		return nil, false, fmt.Errorf("start browser retry relay: %w", err)
	}
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.IP == nil || !address.IP.IsLoopback() {
		_ = listener.Close()
		release()
		return nil, false, errors.New("browser retry relay must listen on a loopback address")
	}
	endpoint := "http://" + listener.Addr().String()
	token, err := randomToken()
	if err != nil {
		_ = listener.Close()
		release()
		return nil, false, fmt.Errorf("create browser retry token: %w", err)
	}
	requestID, err := randomToken()
	if err != nil {
		_ = listener.Close()
		release()
		return nil, false, fmt.Errorf("create browser retry request ID: %w", err)
	}

	pipeReader, pipeWriter := io.Pipe()
	session := &relaySession{
		token:        token,
		requestID:    requestID,
		expectedHost: listener.Addr().String(),
		jobs:         make(chan relayJob, 1),
		connected:    make(chan struct{}, 1),
		started:      make(chan relayResponseStart, 1),
		failed:       make(chan error, 1),
		pipeWriter:   pipeWriter,
	}
	server := &http.Server{Handler: session.handler(), ReadHeaderTimeout: 3 * time.Second}
	serveDone := make(chan struct{})
	go func() {
		defer close(serveDone)
		_ = server.Serve(listener)
	}()

	var cleanupOnce sync.Once
	var lifetimeTimer *time.Timer
	cleanup := func(cause error) {
		if cause == nil {
			cause = io.EOF
		}
		cleanupOnce.Do(func() {
			if lifetimeTimer != nil {
				lifetimeTimer.Stop()
			}
			// Closing the writer carries context cancellation or relay errors to
			// any active response reader. Closing the reader here first races with
			// that propagation and replaces the useful cause with io.ErrClosedPipe.
			_ = pipeWriter.CloseWithError(cause)
			_ = server.Close()
			select {
			case <-serveDone:
			case <-time.After(time.Second):
			}
			release()
		})
	}

	session.jobs <- relayJob{
		ID:     requestID,
		URL:    request.URL,
		Method: request.Method,
		Header: FilterRequestHeaders(request.Header),
		Body:   string(request.Body),
	}
	fragment := url.Values{"endpoint": {endpoint}, "token": {token}}.Encode()
	target := endpoint + "/v1/launch#duckchat-relay=" + fragment
	if err := r.options.OpenBrowser(target); err != nil {
		cleanup(err)
		return nil, false, fmt.Errorf("open Duck.ai in the default browser: %w", err)
	}
	lifetimeTimer = time.AfterFunc(r.options.Lifetime, func() {
		cleanup(context.DeadlineExceeded)
	})

	timer := time.NewTimer(r.options.WaitTimeout)
	defer timer.Stop()
	connected := false
	for {
		select {
		case <-session.connected:
			if !connected {
				connected = true
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(responseStartWait)
			}
		case start := <-session.started:
			if start.Status == http.StatusBadGateway {
				select {
				case relayErr := <-session.failed:
					cleanup(relayErr)
					return nil, true, relayErr
				default:
				}
			}
			stopContext := context.AfterFunc(ctx, func() { cleanup(ctx.Err()) })
			response := &http.Response{
				Status:     fmt.Sprintf("%d %s", start.Status, http.StatusText(start.Status)),
				StatusCode: start.Status,
				Header:     filterResponseHeaders(start.Header),
				Body:       &relayResponseBody{reader: pipeReader, cleanup: cleanup, stopContext: stopContext},
				Request: &http.Request{
					Method: request.Method,
					URL:    mustParseURL(request.URL),
					Header: FilterRequestHeaders(request.Header),
				},
			}
			return response, true, nil
		case relayErr := <-session.failed:
			cleanup(relayErr)
			return nil, true, relayErr
		case <-ctx.Done():
			cleanup(ctx.Err())
			return nil, true, ctx.Err()
		case <-timer.C:
			var err error
			if connected {
				err = errors.New("Chrome relay connected but Duck.ai did not start the retry after clearing site data")
			} else {
				err = errors.New("Chrome relay did not connect; check that the DuckChat CLI extension is enabled in the browser opened by the CLI, then reload it in chrome://extensions")
			}
			cleanup(err)
			return nil, true, err
		}
	}
}

type relaySession struct {
	token        string
	requestID    string
	expectedHost string
	jobs         chan relayJob
	connected    chan struct{}
	started      chan relayResponseStart
	failed       chan error
	pipeWriter   *io.PipeWriter

	mu      sync.Mutex
	start   bool
	closed  bool
	writeMu sync.Mutex
}

func (s *relaySession) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/next", s.handleNext)
	mux.HandleFunc("GET /v1/ping", s.handlePing)
	mux.HandleFunc("POST /v1/start", s.handleStart)
	mux.HandleFunc("POST /v1/chunk", s.handleChunk)
	mux.HandleFunc("POST /v1/finish", s.handleFinish)
	mux.HandleFunc("POST /v1/error", s.handleFailure)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/launch" {
			if r.Host != s.expectedHost {
				http.Error(w, "invalid relay host", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'")
			_, _ = io.WriteString(w, `<!doctype html><html lang="en"><meta charset="utf-8"><title>DuckChat CLI retry</title><body>Opening Duck.ai…</body><script>
if (location.hash.startsWith("#duckchat-relay=")) {
  location.replace("https://duck.ai/" + location.hash);
} else {
  document.body.textContent = "Invalid DuckChat CLI retry link.";
}
</script></html>`)
			return
		}
		origin := r.Header.Get("Origin")
		// Chromium extension service workers omit Origin on same-extension GETs.
		// The bearer token still authenticates these loopback requests. A
		// supplied Origin must be the extension scheme, and preflights need it.
		if origin != "" {
			parsedOrigin, originErr := url.Parse(origin)
			if originErr != nil || parsedOrigin.Scheme != "chrome-extension" || parsedOrigin.Hostname() == "" ||
				parsedOrigin.Port() != "" || parsedOrigin.User != nil || parsedOrigin.Path != "" ||
				parsedOrigin.RawQuery != "" || parsedOrigin.Fragment != "" {
				if os.Getenv("DEBUG") == "true" {
					log.Printf("Duck.ai Chrome relay rejected request with Origin %q", origin)
				}
				http.Error(w, "invalid relay origin", http.StatusForbidden)
				return
			}
		} else if r.Method == http.MethodOptions {
			http.Error(w, "invalid relay preflight", http.StatusForbidden)
			return
		}
		if r.Host != s.expectedHost {
			http.Error(w, "invalid relay host", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Private-Network", "true")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !s.authorized(r) {
			if os.Getenv("DEBUG") == "true" {
				log.Printf("Duck.ai Chrome relay rejected unauthenticated request to %s", r.URL.Path)
			}
			http.Error(w, "unauthorized relay session", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *relaySession) authorized(r *http.Request) bool {
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if provided == "" || len(provided) != len(s.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) == 1
}

func (s *relaySession) handleNext(w http.ResponseWriter, _ *http.Request) {
	select {
	case s.connected <- struct{}{}:
	default:
	}
	select {
	case job := <-s.jobs:
		writeRelayJSON(w, http.StatusOK, job)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *relaySession) handlePing(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	active := !s.closed
	s.mu.Unlock()
	if !active {
		http.Error(w, "relay session is closed", http.StatusGone)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *relaySession) handleStart(w http.ResponseWriter, r *http.Request) {
	var start relayResponseStart
	if !decodeRelayJSON(w, r, maxControlBody, &start) {
		return
	}
	if start.ID != s.requestID || start.Status < 100 || start.Status > 599 {
		http.Error(w, "unknown relay request", http.StatusNotFound)
		return
	}
	s.mu.Lock()
	if s.start || s.closed {
		s.mu.Unlock()
		http.Error(w, "relay response already started", http.StatusConflict)
		return
	}
	s.start = true
	s.mu.Unlock()
	start.Header = filterResponseHeaders(start.Header)
	s.started <- start
	w.WriteHeader(http.StatusNoContent)
}

func (s *relaySession) handleChunk(w http.ResponseWriter, r *http.Request) {
	var chunk relayResponseChunk
	if !decodeRelayJSON(w, r, maxChunkBody, &chunk) {
		return
	}
	if chunk.ID != s.requestID {
		http.Error(w, "unknown relay request", http.StatusNotFound)
		return
	}
	data, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil || len(data) > 64<<10 {
		http.Error(w, "invalid relay chunk", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	ready := s.start && !s.closed
	s.mu.Unlock()
	if !ready {
		http.Error(w, "relay response has not started", http.StatusConflict)
		return
	}
	s.writeMu.Lock()
	_, err = s.pipeWriter.Write(data)
	s.writeMu.Unlock()
	if err != nil {
		http.Error(w, "relay response closed", http.StatusGone)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *relaySession) handleFinish(w http.ResponseWriter, r *http.Request) {
	var finish relayResponseFinish
	if !decodeRelayJSON(w, r, maxControlBody, &finish) {
		return
	}
	if finish.ID != s.requestID {
		http.Error(w, "unknown relay request", http.StatusNotFound)
		return
	}
	s.mu.Lock()
	if !s.start || s.closed {
		s.mu.Unlock()
		http.Error(w, "relay response is not active", http.StatusConflict)
		return
	}
	s.closed = true
	s.mu.Unlock()
	_ = s.pipeWriter.Close()
	w.WriteHeader(http.StatusNoContent)
}

func (s *relaySession) handleFailure(w http.ResponseWriter, r *http.Request) {
	var failure relayResponseFailure
	if !decodeRelayJSON(w, r, maxControlBody, &failure) {
		return
	}
	if failure.ID != s.requestID {
		http.Error(w, "unknown relay request", http.StatusNotFound)
		return
	}
	message := strings.TrimSpace(failure.Message)
	if message == "" || len(message) > 256 {
		message = "Chrome could not complete the browser-backed retry"
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		http.Error(w, "relay response is closed", http.StatusConflict)
		return
	}
	s.closed = true
	started := s.start
	s.mu.Unlock()
	err := fmt.Errorf("browser relay: %s", message)
	if !started {
		select {
		case s.failed <- err:
		default:
		}
	}
	_ = s.pipeWriter.CloseWithError(err)
	w.WriteHeader(http.StatusNoContent)
}

func decodeRelayJSON(w http.ResponseWriter, r *http.Request, limit int64, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer r.Body.Close()
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		http.Error(w, "JSON required", http.StatusUnsupportedMediaType)
		return false
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		http.Error(w, "invalid relay message", http.StatusBadRequest)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "multiple relay messages", http.StatusBadRequest)
		return false
	}
	return true
}

func writeRelayJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type relayResponseBody struct {
	reader      *io.PipeReader
	once        sync.Once
	cleanup     func(error)
	stopContext func() bool
}

func (b *relayResponseBody) Read(p []byte) (int, error) { return b.reader.Read(p) }

func (b *relayResponseBody) Close() error {
	var err error
	b.once.Do(func() {
		if b.stopContext != nil {
			b.stopContext()
		}
		err = b.reader.Close()
		b.cleanup(io.EOF)
	})
	return err
}

func validateRequest(request Request) error {
	parsed, err := url.Parse(request.URL)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "duck.ai") ||
		parsed.User != nil || (parsed.Port() != "" && parsed.Port() != "443") {
		return errors.New("browser retry only accepts HTTPS Duck.ai requests")
	}
	if request.Method != http.MethodPost {
		return errors.New("browser retry only accepts POST requests")
	}
	if len(request.Body) > maxRequestBody {
		return fmt.Errorf("browser retry request is too large (%d bytes)", len(request.Body))
	}
	return nil
}

// FilterRequestHeaders removes browser-owned and credential headers before a
// request is handed to the extension. Chrome supplies those in the page.
func FilterRequestHeaders(headers http.Header) http.Header {
	allowed := map[string]struct{}{
		"accept": {}, "content-type": {}, "x-vqd-hash-1": {}, "x-fe-signals": {},
		"x-fe-version": {}, "x-ddg-journey-id": {},
	}
	filtered := make(http.Header)
	for name, values := range headers {
		if _, ok := allowed[strings.ToLower(name)]; !ok {
			continue
		}
		for _, value := range values {
			filtered.Add(name, value)
		}
	}
	return filtered
}

func filterResponseHeaders(headers http.Header) http.Header {
	allowed := map[string]struct{}{
		"content-type": {}, "retry-after": {}, "x-vqd-4": {}, "cache-control": {},
	}
	filtered := make(http.Header)
	for name, values := range headers {
		if _, ok := allowed[strings.ToLower(name)]; !ok {
			continue
		}
		for _, value := range values {
			filtered.Add(name, value)
		}
	}
	return filtered
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}

func mustParseURL(raw string) *url.URL {
	parsed, _ := url.Parse(raw)
	return parsed
}

func openDefaultBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		command = exec.Command("open", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}
