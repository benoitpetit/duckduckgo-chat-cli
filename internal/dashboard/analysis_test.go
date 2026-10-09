package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
)

func TestBuildConversationReportSpreadsExcerptsAndHonorsEstimatedBudget(t *testing.T) {
	sessions := make([]persistence.ConversationSession, 24)
	for i := range sessions {
		sessions[i] = persistence.ConversationSession{
			ID: "session_" + strings.Repeat("x", i+1),
			Messages: []intelligence.Message{
				{Role: "user", Content: "PRIVATE-SESSION-" + string(rune('A'+i)) + strings.Repeat("u", 2500)},
				{Role: "assistant", Content: strings.Repeat("assistant answer ", 200)},
			},
		}
	}
	prompt, included, estimated := BuildConversationReport(sessions, 1000)
	if included < 5 || included > len(sessions) {
		t.Fatalf("included sessions = %d, want a representative multi-session sample", included)
	}
	if estimated > 1000 || estimated < 100 {
		t.Fatalf("estimated tokens = %d, expected overhead included and within budget", estimated)
	}
	if !strings.Contains(prompt, "local CLI usage archive") || !strings.Contains(prompt, "PRIVATE-SESSION-") {
		t.Fatalf("report prompt missing instructions or excerpts: %q", prompt[:min(300, len(prompt))])
	}
	if _, n, _ := BuildConversationReport(sessions, 1000); n != included {
		t.Fatalf("report selection was not deterministic: %d vs %d", included, n)
	}
}

func TestMetricsAnalysisOmitsConversationTextAndValidatesModel(t *testing.T) {
	secret := "TRANSCRIPT-MUST-NOT-LEAVE-METRICS-ROUTE"
	sessions := persistence.NewHistoryManager(t.TempDir())
	if err := sessions.SaveSession(&persistence.ConversationSession{ID: "session_metrics", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: secret}}}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var received string
	server := NewServer(Dependencies{
		Analytics: analyticsWithSecret(t, secret),
		Sessions:  sessions,
		Config:    config.DashboardConfig{Port: 8765, RefreshIntervalSeconds: 3, RetentionDays: 90},
		Analyze: func(_ context.Context, _ models.Model, prompt string) (string, error) {
			calls.Add(1)
			received = prompt
			return "summary", nil
		},
	})
	response := postAnalysis(server, "/api/analysis/metrics", `{"model":"`+string(models.Default())+`"}`)
	if response.Code != http.StatusOK || calls.Load() != 1 || strings.Contains(received, secret) {
		t.Fatalf("metrics analysis response=%d calls=%d prompt=%q", response.Code, calls.Load(), received)
	}
	response = postAnalysis(server, "/api/analysis/metrics", `{"model":"unlisted-model"}`)
	if response.Code != http.StatusBadRequest || calls.Load() != 1 {
		t.Fatalf("invalid model was not refused: status=%d calls=%d", response.Code, calls.Load())
	}
}

func TestConversationAnalysisRequiresOptInAndPreviewDoesNotCallProvider(t *testing.T) {
	var calls atomic.Int32
	server, history := sessionTestServer(t, false)
	server.deps.Analyze = func(context.Context, models.Model, string) (string, error) {
		calls.Add(1)
		return "report", nil
	}
	settings := server.currentSettings()
	settings.AllowConversationAnalysis = false
	server.UpdateSettings(settings)
	_ = history.SaveSession(&persistence.ConversationSession{ID: "session_one", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: "hello"}}})
	response := postAnalysis(server, "/api/analysis/conversations", `{"model":"`+string(models.Default())+`"}`)
	if response.Code != http.StatusForbidden || calls.Load() != 0 {
		t.Fatalf("disabled conversation analysis status=%d calls=%d", response.Code, calls.Load())
	}
	settings.AllowConversationAnalysis = true
	server.UpdateSettings(settings)
	preview := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/analysis/conversations/preview", nil)
	server.handler().ServeHTTP(preview, req)
	if preview.Code != http.StatusOK || calls.Load() != 0 || !strings.Contains(preview.Body.String(), "estimatedTokens") {
		t.Fatalf("preview status=%d calls=%d body=%s", preview.Code, calls.Load(), preview.Body.String())
	}
	var estimates map[string]any
	if err := json.Unmarshal(preview.Body.Bytes(), &estimates); err != nil {
		t.Fatal(err)
	}
	if estimates["includedSessions"] != float64(1) || estimates["estimatedTokens"].(float64) > estimates["tokenBudget"].(float64) {
		t.Fatalf("preview budget is incorrect: %+v", estimates)
	}
	submitted := postAnalysis(server, "/api/analysis/conversations", `{"model":"`+string(models.Default())+`"}`)
	if submitted.Code != http.StatusOK || calls.Load() != 1 || !strings.Contains(submitted.Body.String(), "report") {
		t.Fatalf("explicit report request status=%d calls=%d body=%s", submitted.Code, calls.Load(), submitted.Body.String())
	}
}

func TestAnalysisRejectsConcurrentRequests(t *testing.T) {
	started := make(chan struct{})
	continueRequest := make(chan struct{})
	server := testServer(t, 8765)
	server.deps.Analyze = func(context.Context, models.Model, string) (string, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-continueRequest
		return "done", nil
	}
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_ = postAnalysis(server, "/api/analysis/metrics", `{"model":"`+string(models.Default())+`"}`)
	}()
	<-started
	second := postAnalysis(server, "/api/analysis/metrics", `{"model":"`+string(models.Default())+`"}`)
	if second.Code != http.StatusConflict {
		t.Fatalf("concurrent request status = %d, want 409", second.Code)
	}
	close(continueRequest)
	<-firstDone
}

func analyticsWithSecret(t *testing.T, secret string) *analytics.ChatAnalytics {
	t.Helper()
	tracker := analytics.NewChatAnalytics()
	tracker.RecordMessage("user", len(secret))
	return tracker
}

func postAnalysis(server *Server, path, body string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8765"+path, bytes.NewBufferString(body))
	req.Header.Set("Origin", "http://127.0.0.1:8765")
	server.handler().ServeHTTP(rr, req)
	return rr
}
