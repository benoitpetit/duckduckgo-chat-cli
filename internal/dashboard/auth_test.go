package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/security"
)

const authTestPassword = "local dashboard test password"
const authTestCookieName = "duckchat_dashboard_session"

func authEnabledServer(t *testing.T) *Server {
	t.Helper()
	server := testServer(t, 8765)
	salt, hash, err := security.HashPassword(authTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	settings := server.currentSettings()
	settings.PasswordSalt = salt
	settings.PasswordHash = hash
	server.UpdateSettings(settings)
	return server
}

func authRequest(server *Server, method, path string, body []byte, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+server.expectedHost()+path, bytes.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	if method == http.MethodPost {
		req.Header.Set("Origin", "http://"+server.expectedHost())
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	server.handler().ServeHTTP(rr, req)
	return rr
}

func loginCookie(t *testing.T, server *Server) *http.Cookie {
	t.Helper()
	body, err := json.Marshal(map[string]string{"password": authTestPassword})
	if err != nil {
		t.Fatal(err)
	}
	rr := authRequest(server, http.MethodPost, "/auth/login", body, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%q", rr.Code, rr.Body.String())
	}
	for _, cookie := range rr.Result().Cookies() {
		if cookie.Name == authTestCookieName {
			return cookie
		}
	}
	t.Fatal("login response did not set a session cookie")
	return nil
}

func TestDashboardAuthProtectsEveryRoute(t *testing.T) {
	server := authEnabledServer(t)
	for _, path := range []string{
		"/", "/assets/style.css", "/assets/app.js", "/assets/logo.png",
		"/api/stats", "/api/settings", "/api/sessions", "/api/activity/stream",
	} {
		rr := authRequest(server, http.MethodGet, path, nil, nil)
		if strings.HasPrefix(path, "/api/") {
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("GET %s status = %d, want 401", path, rr.Code)
			}
		} else if rr.Code != http.StatusUnauthorized && rr.Code != http.StatusFound {
			t.Errorf("GET %s status = %d, want protected status", path, rr.Code)
		}
	}
	for _, path := range []string{
		"/assets/login.css", "/assets/login.js", "/assets/login-duck-base.png",
		"/assets/login-duck-hands.png", "/favicon.ico",
	} {
		rr := authRequest(server, http.MethodGet, path, nil, nil)
		if rr.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, rr.Code)
		}
	}
}

func TestDashboardAuthLoginSetsSessionCookie(t *testing.T) {
	server := authEnabledServer(t)
	cookie := loginCookie(t, server)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Expires.IsZero() == false || cookie.MaxAge != 0 {
		t.Fatalf("unexpected session cookie settings: %+v", cookie)
	}
	if rr := authRequest(server, http.MethodGet, "/", nil, cookie); rr.Code != http.StatusOK {
		t.Fatalf("authenticated root status = %d, want 200", rr.Code)
	}
	rr := authRequest(server, http.MethodGet, "/api/settings", nil, cookie)
	if rr.Code != http.StatusOK {
		t.Fatalf("authenticated settings status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	settings := server.currentSettings()
	if strings.Contains(body, settings.PasswordSalt) || strings.Contains(body, settings.PasswordHash) {
		t.Fatal("settings response exposed the password salt or verifier")
	}
}

func TestDashboardAuthFormLoginUsesPOSTAndDoesNotExposePasswordInURL(t *testing.T) {
	server := authEnabledServer(t)
	body := strings.NewReader("password=local+dashboard+test+password")
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765/auth/login", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	rr := httptest.NewRecorder()
	server.handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/" || rr.Header().Get("Set-Cookie") == "" {
		t.Fatalf("POST form login status=%d location=%q cookie=%q body=%q", rr.Code, rr.Header().Get("Location"), rr.Header().Get("Set-Cookie"), rr.Body.String())
	}
	if strings.Contains(rr.Header().Get("Location"), "password") || strings.Contains(rr.Header().Get("Set-Cookie"), authTestPassword) {
		t.Fatal("form login exposed password in URL or cookie")
	}
}

func TestDashboardAuthLogoutInvalidatesSession(t *testing.T) {
	server := authEnabledServer(t)
	cookie := loginCookie(t, server)
	rr := authRequest(server, http.MethodPost, "/auth/logout", nil, cookie)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", rr.Code)
	}
	if rr := authRequest(server, http.MethodGet, "/", nil, cookie); rr.Code != http.StatusFound {
		t.Fatalf("root after logout status = %d, want 302", rr.Code)
	}
}

func TestDashboardAuthSettingsChangeInvalidatesSessions(t *testing.T) {
	server := authEnabledServer(t)
	cookie := loginCookie(t, server)
	settings := server.currentSettings()
	settings.PasswordHash = strings.Repeat("0", 64)
	server.UpdateSettings(settings)
	if rr := authRequest(server, http.MethodGet, "/", nil, cookie); rr.Code != http.StatusFound {
		t.Fatalf("root after password update status = %d, want 302", rr.Code)
	}
}

func TestDashboardAuthRemovingPasswordAllowsAccess(t *testing.T) {
	server := authEnabledServer(t)
	cookie := loginCookie(t, server)
	settings := server.currentSettings()
	settings.PasswordSalt = ""
	settings.PasswordHash = ""
	server.UpdateSettings(settings)
	if rr := authRequest(server, http.MethodGet, "/", nil, nil); rr.Code != http.StatusOK {
		t.Fatalf("root after password removal status = %d, want 200", rr.Code)
	}
	if rr := authRequest(server, http.MethodGet, "/", nil, cookie); rr.Code != http.StatusOK {
		t.Fatalf("root after password removal with prior cookie status = %d, want 200", rr.Code)
	}
}

func TestDashboardAuthRateLimitsFailures(t *testing.T) {
	server := authEnabledServer(t)
	body := []byte(`{"password":"wrong password"}`)
	for attempt := 1; attempt <= 6; attempt++ {
		rr := authRequest(server, http.MethodPost, "/auth/login", body, nil)
		want := http.StatusUnauthorized
		if attempt == 6 {
			want = http.StatusTooManyRequests
		}
		if rr.Code != want {
			t.Fatalf("failed login %d status = %d, want %d", attempt, rr.Code, want)
		}
		if strings.Contains(rr.Body.String(), "wrong password") {
			t.Fatal("login failure response leaked the supplied password")
		}
	}
}

func TestDashboardAuthDisabledPreservesCurrentAccess(t *testing.T) {
	server := testServer(t, 8765)
	if rr := authRequest(server, http.MethodGet, "/", nil, nil); rr.Code != http.StatusOK {
		t.Fatalf("root without password status = %d, want 200", rr.Code)
	}
	if rr := authRequest(server, http.MethodGet, "/login", nil, nil); rr.Code != http.StatusFound || rr.Header().Get("Location") != "/" {
		t.Fatalf("login without password status=%d location=%q, want redirect to /", rr.Code, rr.Header().Get("Location"))
	}
}

func TestActivityStreamAuthorizationTracksSessionRevocation(t *testing.T) {
	server := testServer(t, 8765)
	server.settings.PasswordHash = "configured"
	server.sessions["stream-session"] = struct{}{}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/activity/stream", nil)
	req.AddCookie(&http.Cookie{Name: dashboardSessionCookie, Value: "stream-session"})
	if !server.activityStreamAuthorized(req) {
		t.Fatal("valid stream session was rejected")
	}
	server.mu.Lock()
	delete(server.sessions, "stream-session")
	server.mu.Unlock()
	if server.activityStreamAuthorized(req) {
		t.Fatal("revoked stream session was still authorized")
	}
}
