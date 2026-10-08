package voice

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"duckduckgo-chat-cli/internal/chat"
)

const maxSDPOfferBytes = 1 << 20

type Dependencies struct {
	Proof     ProofProvider
	Signaling SignalingClient
}

type localHandler struct {
	deps          Dependencies
	allowedHost   string
	allowedOrigin string
	journeyID     string

	mu    sync.Mutex
	token string
	proof *chat.DynamicHeaders
	stop  chan struct{}
	ended sync.Once
}

func NewHandler(deps Dependencies, token, allowedOrigin string) (http.Handler, error) {
	if deps.Proof == nil || deps.Signaling == nil {
		return nil, fmt.Errorf("voice proof and signaling dependencies are required")
	}
	if token == "" {
		return nil, fmt.Errorf("voice access token is required")
	}
	origin, err := url.Parse(allowedOrigin)
	if err != nil || origin.Scheme != "http" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, fmt.Errorf("voice origin must be an absolute local HTTP origin")
	}
	if origin.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("voice origin must use 127.0.0.1")
	}
	journeyID, err := newJourneyID()
	if err != nil {
		return nil, fmt.Errorf("create voice journey ID: %w", err)
	}
	return &localHandler{
		deps:          deps,
		allowedHost:   origin.Host,
		allowedOrigin: origin.Scheme + "://" + origin.Host,
		journeyID:     journeyID,
		token:         token,
		stop:          make(chan struct{}),
	}, nil
}

func (h *localHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	switch r.URL.Path {
	case "/":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if r.Host != h.allowedHost {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.serveAsset(w, "webui/index.html", "text/html; charset=utf-8")
	case "/app.js":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if r.Host != h.allowedHost {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.serveAsset(w, "webui/app.js", "text/javascript; charset=utf-8")
	case "/style.css":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if r.Host != h.allowedHost {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.serveAsset(w, "webui/style.css", "text/css; charset=utf-8")
	case "/logo.png":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if r.Host != h.allowedHost {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.serveAsset(w, "webui/logo.png", "image/png")
	case "/api/ice-servers":
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		if !h.authorize(w, r) {
			return
		}
		h.handleICEServers(w, r)
	case "/api/session":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !h.authorize(w, r) {
			return
		}
		h.handleSession(w, r)
	case "/api/stop":
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		if !h.authorize(w, r) {
			return
		}
		h.ended.Do(func() { close(h.stop) })
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (h *localHandler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if r.Host != h.allowedHost {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	origin := r.Header.Get("Origin")
	if origin != "" && origin != h.allowedOrigin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	h.mu.Lock()
	token := h.token
	h.mu.Unlock()
	if token == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (h *localHandler) handleICEServers(w http.ResponseWriter, r *http.Request) {
	proof, err := h.captureProof(r.Context())
	if err != nil {
		writeVoiceError(w, http.StatusBadGateway, "Duck.ai proof capture failed")
		return
	}
	configuration, err := h.deps.Signaling.GetICEServers(r.Context(), proof)
	if isExplicitProofRejection(err) {
		proof, err = h.captureProof(r.Context())
		if err == nil {
			configuration, err = h.deps.Signaling.GetICEServers(r.Context(), proof)
		}
	}
	if err != nil {
		writeSignalingError(w, err)
		return
	}
	h.mu.Lock()
	h.proof = cloneDynamicHeaders(proof)
	h.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(configuration); err != nil {
		return
	}
}

func (h *localHandler) handleSession(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/sdp") {
		writeVoiceError(w, http.StatusBadRequest, "Expected an application/sdp offer")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSDPOfferBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeVoiceError(w, http.StatusRequestEntityTooLarge, "SDP offer exceeds the 1 MiB limit")
		} else {
			writeVoiceError(w, http.StatusBadRequest, "Could not read SDP offer")
		}
		return
	}
	if !validSDPOffer(body) {
		writeVoiceError(w, http.StatusBadRequest, "SDP offer must begin with v=0")
		return
	}
	h.mu.Lock()
	proof := cloneDynamicHeaders(h.proof)
	h.mu.Unlock()
	if proof == nil {
		writeVoiceError(w, http.StatusConflict, "Fetch ICE configuration before creating a session")
		return
	}
	answer, err := h.deps.Signaling.CreateSession(r.Context(), proof, string(body))
	if isExplicitProofRejection(err) {
		proof, err = h.captureProof(r.Context())
		if err == nil {
			h.mu.Lock()
			h.proof = cloneDynamicHeaders(proof)
			h.mu.Unlock()
			answer, err = h.deps.Signaling.CreateSession(r.Context(), proof, string(body))
		}
	}
	if err != nil {
		writeSignalingError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/sdp")
	_, _ = io.WriteString(w, answer)
}

func (h *localHandler) captureProof(ctx context.Context) (*chat.DynamicHeaders, error) {
	proof, err := h.deps.Proof.Capture(ctx)
	if err != nil {
		return nil, err
	}
	if proof == nil {
		return nil, fmt.Errorf("Duck.ai proof provider returned no proof")
	}
	proof = cloneDynamicHeaders(proof)
	proof.JourneyID = h.journeyID
	return proof, nil
}

func (h *localHandler) invalidate() {
	h.mu.Lock()
	h.token = ""
	h.proof = nil
	h.mu.Unlock()
}

func cloneDynamicHeaders(headers *chat.DynamicHeaders) *chat.DynamicHeaders {
	if headers == nil {
		return nil
	}
	copy := *headers
	return &copy
}

func validSDPOffer(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	firstLine := strings.SplitN(string(body), "\n", 2)[0]
	firstLine = strings.TrimSuffix(firstLine, "\r")
	return firstLine == "v=0"
}

func isExplicitProofRejection(err error) bool {
	var remote *SignalingHTTPError
	if !errors.As(err, &remote) {
		return false
	}
	// Preserve the existing text client's legacy Duck.ai challenge handling:
	// it treats HTTP 418 as a rotating-proof rejection. Voice responses may
	// also identify a proof rejection with an explicit machine-readable type.
	return remote.StatusCode == http.StatusTeapot || remote.RemoteType == "ERR_INVALID_VQD" || remote.RemoteType == "ERR_CHALLENGE"
}

func writeSignalingError(w http.ResponseWriter, err error) {
	var remote *SignalingHTTPError
	if errors.As(err, &remote) {
		if remote.StatusCode == http.StatusTooManyRequests {
			if remote.RetryAfter != "" {
				w.Header().Set("Retry-After", remote.RetryAfter)
			}
			writeVoiceError(w, http.StatusTooManyRequests, remote.Error())
			return
		}
		writeVoiceError(w, http.StatusBadGateway, remote.Error())
		return
	}
	writeVoiceError(w, http.StatusBadGateway, "Duck.ai signaling request failed")
}

func writeVoiceError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	http.Error(w, message, status)
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, POST")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (h *localHandler) serveAsset(w http.ResponseWriter, name, contentType string) {
	data, err := webAssets.ReadFile(name)
	if err != nil {
		http.NotFound(w, nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}
