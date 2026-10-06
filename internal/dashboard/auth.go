package dashboard

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/security"
)

const (
	dashboardSessionCookie = "duckchat_dashboard_session"
	loginWindow            = time.Minute
	loginAttemptsPerWindow = 5
	loginBodyLimit         = 8 * 1024
)

type loginFailure struct {
	windowStart time.Time
	attempts    int
}

type loginRequest struct {
	Password string `json:"password"`
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.currentSettings().PasswordHash == "" || s.hasValidSession(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	serveAsset(w, "web/login.html", "text/html; charset=utf-8")
}

func (s *Server) handleLoginStyle(w http.ResponseWriter, _ *http.Request) {
	serveAsset(w, "web/login.css", "text/css; charset=utf-8")
}

func (s *Server) handleLoginScript(w http.ResponseWriter, _ *http.Request) {
	serveAsset(w, "web/login.js", "text/javascript; charset=utf-8")
}

func (s *Server) handleLoginDuck(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	asset := map[string]string{
		"/assets/login-duck-base.png":  "web/login-duck-base.png",
		"/assets/login-duck-hands.png": "web/login-duck-hands.png",
	}[path]
	if asset == "" {
		http.NotFound(w, r)
		return
	}
	serveAsset(w, asset, "image/png")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	settings := s.currentSettings()
	if settings.PasswordHash == "" {
		http.Error(w, "password protection is disabled", http.StatusConflict)
		return
	}
	client := loginClientKey(r.RemoteAddr)
	if !s.allowLoginAttempt(client, time.Now()) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many login attempts; try again later"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, loginBodyLimit)
	var payload loginRequest
	isJSON := strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json")
	if isJSON {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid login request"})
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid login request"})
			return
		}
		payload.Password = r.PostForm.Get("password")
	}
	if !security.VerifyPassword(payload.Password, settings.PasswordSalt, settings.PasswordHash) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		http.Error(w, "could not create dashboard session", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(tokenBytes[:])
	s.mu.Lock()
	if s.settings.PasswordSalt != settings.PasswordSalt || s.settings.PasswordHash != settings.PasswordHash {
		s.mu.Unlock()
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if s.sessions == nil {
		s.sessions = make(map[string]struct{})
	}
	s.sessions[token] = struct{}{}
	delete(s.loginFailures, client)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: dashboardSessionCookie, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	if !isJSON {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(dashboardSessionCookie); err == nil {
		s.mu.Lock()
		delete(s.sessions, cookie.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: dashboardSessionCookie, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) allowLoginAttempt(client string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loginFailures == nil {
		s.loginFailures = make(map[string]loginFailure)
	}
	for key, failure := range s.loginFailures {
		if now.Sub(failure.windowStart) >= loginWindow {
			delete(s.loginFailures, key)
		}
	}
	failure := s.loginFailures[client]
	if failure.windowStart.IsZero() || now.Sub(failure.windowStart) >= loginWindow {
		failure = loginFailure{windowStart: now}
	}
	if failure.attempts >= loginAttemptsPerWindow {
		s.loginFailures[client] = failure
		return false
	}
	failure.attempts++
	s.loginFailures[client] = failure
	return true
}

func loginClientKey(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return strings.TrimSpace(remoteAddr)
}
