package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/persistence"
)

func sessionTestServer(t *testing.T, show bool) (*Server, *persistence.HistoryManager) {
	t.Helper()
	history := persistence.NewHistoryManager(filepath.Join(t.TempDir(), "sessions"))
	server := NewServer(Dependencies{
		History:  NewHistoryStore(filepath.Join(t.TempDir(), "stats.jsonl"), 90),
		Sessions: history, Commands: command.GetCommandRegistry,
		Config: config.DashboardConfig{Port: 8765, RefreshIntervalSeconds: 3, RetentionDays: 90, ShowConversations: show, AnalysisTokenBudget: 8000},
	})
	return server, history
}

func TestSessionRoutesAreHiddenByDefault(t *testing.T) {
	server, _ := sessionTestServer(t, false)
	for _, path := range []string{"/api/sessions", "/api/sessions/session_123"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765"+path, nil)
		server.handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, rr.Code)
		}
	}
}

func TestSessionRoutesListSearchAndReadWhenEnabled(t *testing.T) {
	server, history := sessionTestServer(t, true)
	older := &persistence.ConversationSession{ID: "session_old", StartTime: time.Now().Add(-time.Hour), Model: "model-old", Messages: []intelligence.Message{{Role: "user", Content: "old preview"}}}
	newer := &persistence.ConversationSession{ID: "session_new", StartTime: time.Now(), Model: "model-new", Messages: []intelligence.Message{{Role: "user", Content: "needle preview"}, {Role: "assistant", Content: "private transcript"}}}
	if err := history.SaveSession(older); err != nil {
		t.Fatal(err)
	}
	if err := history.SaveSession(newer); err != nil {
		t.Fatal(err)
	}
	list := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/sessions?query=NEEDLE", nil)
	server.handler().ServeHTTP(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", list.Code, list.Body.String())
	}
	var summaries []persistence.SessionSummary
	if err := json.Unmarshal(list.Body.Bytes(), &summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].ID != newer.ID || summaries[0].ResumeCommand != "/load session_new" {
		t.Fatalf("search results = %+v", summaries)
	}
	if strings.Contains(list.Body.String(), "private transcript") {
		t.Fatal("session list exposed transcript contents")
	}

	detail := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/sessions/session_new", nil)
	server.handler().ServeHTTP(detail, req)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "private transcript") {
		t.Fatalf("detail response status=%d body=%s", detail.Code, detail.Body.String())
	}
}

func TestSessionDetailRejectsInvalidIDsBeforeLoading(t *testing.T) {
	server, _ := sessionTestServer(t, true)
	for _, id := range []string{"", "session/a", "../session_1", "session_" + strings.Repeat("a", 100)} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/sessions/session_1", nil)
		req.SetPathValue("id", id)
		server.handleSessionDetail(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("id %q status = %d, want 400", id, rr.Code)
		}
	}
}

func TestSessionListQueryIsLocalAndBounded(t *testing.T) {
	server, _ := sessionTestServer(t, true)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/sessions?query="+url.QueryEscape(strings.Repeat("x", 300)), nil)
	server.handleSessions(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("oversized query status = %d, want 400", rr.Code)
	}
}

func TestSessionDetailRedactsImageBytes(t *testing.T) {
	server, history := sessionTestServer(t, true)
	imageBytes := []byte("private-image-bytes")
	session := &persistence.ConversationSession{
		ID: "session_image", StartTime: time.Now(), Model: "gpt-5.6-luna",
		Messages: []intelligence.Message{{Role: "user", Content: "Describe this", Images: []media.ImageAttachment{{Name: "logo.png", MIMEType: "image/png", Data: imageBytes}}}},
	}
	if err := history.SaveSession(session); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/sessions/session_image", nil)
	server.handler().ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", response.Code, body)
	}
	if !strings.Contains(body, "logo.png") || !strings.Contains(body, "image/png") {
		t.Fatalf("detail response omitted image metadata: %s", body)
	}
	if strings.Contains(body, "private-image-bytes") || strings.Contains(body, "cHJpdmF0ZS1pbWFnZS1ieXRlcw==") || strings.Contains(body, "data:image/") {
		t.Fatalf("detail response exposed image bytes: %s", body)
	}
}
