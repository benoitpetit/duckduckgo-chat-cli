package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/config"
)

func TestAPIKeyMiddlewareProtectsVersionedAPI(t *testing.T) {
	cfg := &config.Config{API: config.APIConfig{APIKey: "secret", ShowGinLogs: false}}
	router := setupRouter(NewSession(&chat.Chat{}, cfg), cfg)

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("status without key = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	authorized := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("X-API-Key", "secret")
	router.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK {
		t.Fatalf("status with key = %d, want %d", authorized.Code, http.StatusOK)
	}
}

func TestCORSOnlyAllowsConfiguredOrigins(t *testing.T) {
	cfg := &config.Config{API: config.APIConfig{AllowedOrigins: []string{"https://example.test"}, ShowGinLogs: false}}
	router := setupRouter(NewSession(&chat.Chat{}, cfg), cfg)

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", "https://not-allowed.test")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected CORS origin %q", got)
	}
}
