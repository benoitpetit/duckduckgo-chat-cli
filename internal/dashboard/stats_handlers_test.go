package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
)

func TestStatsResponseIncludesDailyActivity(t *testing.T) {
	server := testServer(t, 8765)
	oldSession := analytics.Snapshot{
		SessionStartTime: time.Now().Add(-24 * time.Hour), MessagesTotal: 7, ChatInteractionsTotal: 2,
	}
	if err := server.deps.History.Save(oldSession); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8765/api/stats", nil)
	rr := httptest.NewRecorder()
	server.handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/stats status = %d, want 200", rr.Code)
	}
	var response struct {
		DailyActivity []DailyActivityDay `json:"dailyActivity"`
		SessionCount  int                `json:"sessionCount"`
		AllSessions   analytics.Snapshot `json:"allSessions"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.DailyActivity) != 90 {
		t.Fatalf("dailyActivity has %d entries, want 90", len(response.DailyActivity))
	}
	if response.SessionCount != 2 || response.AllSessions.MessagesTotal != 7 || response.AllSessions.ChatInteractionsTotal != 2 {
		t.Fatalf("all-session stats = %+v across %d sessions, want saved data plus active session", response.AllSessions, response.SessionCount)
	}
	last := response.DailyActivity[len(response.DailyActivity)-1]
	if last.Date != time.Now().In(time.Local).Format("2006-01-02") || !last.Available || last.UserMessages != 0 {
		t.Fatalf("current day = %+v, want available zero-count today", last)
	}
}

func TestAggregateSessionStatsMergesRetainedSessionsAndLiveSession(t *testing.T) {
	start := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.Local)
	activeStart := time.Date(2026, time.October, 6, 9, 0, 0, 0, time.Local)
	history := []analytics.Snapshot{
		{SessionStartTime: start, SessionEndTime: start.Add(time.Hour), SessionDuration: time.Hour,
			ChatInteractionsTotal: 2, ChatInteractionsSuccessful: 1, ChatInteractionsFailed: 1,
			TotalChatResponseTime: 4 * time.Second, MessagesTotal: 3, UserMessages: 2, UserTokensEstimate: 10,
			CommandsUsed: map[string]int{"/help": 2}, ByModel: map[string]analytics.ModelMetrics{"m1": {Interactions: 2, Successful: 1, Failed: 1, TotalResponseTime: 4 * time.Second}}},
		{SessionStartTime: activeStart, ChatInteractionsTotal: 2, MessagesTotal: 2, UserMessages: 1,
			UserTokensEstimate: 5, CommandsUsed: map[string]int{"/help": 1, "/config": 1},
			ByModel: map[string]analytics.ModelMetrics{"m1": {Interactions: 2, Successful: 2, TotalResponseTime: 2 * time.Second}}},
	}
	current := analytics.Snapshot{SessionStartTime: activeStart, SessionDuration: 3 * time.Minute,
		ChatInteractionsTotal: 3, ChatInteractionsSuccessful: 3, TotalChatResponseTime: 6 * time.Second,
		MessagesTotal: 4, UserMessages: 2, UserTokensEstimate: 9, CurrentModel: "m2",
		CommandsUsed: map[string]int{"/help": 3, "/config": 1},
		ByModel:      map[string]analytics.ModelMetrics{"m2": {Interactions: 3, Successful: 3, TotalResponseTime: 6 * time.Second}}}

	got, sessions := aggregateSessionStats(history, current)
	if sessions != 2 || got.ChatInteractionsTotal != 5 || got.ChatInteractionsSuccessful != 4 || got.ChatInteractionsFailed != 1 {
		t.Fatalf("aggregate interactions = %+v over %d sessions", got, sessions)
	}
	if got.MessagesTotal != 7 || got.UserMessages != 4 || got.UserTokensEstimate != 19 {
		t.Fatalf("aggregate message/token counts = %+v", got)
	}
	if got.AverageChatResponseTime != 2*time.Second {
		t.Fatalf("aggregate average latency = %s, want 2s", got.AverageChatResponseTime)
	}
	if got.CommandsUsed["/help"] != 5 || got.CommandsUsed["/config"] != 1 {
		t.Fatalf("aggregate commands = %v", got.CommandsUsed)
	}
	if got.ByModel["m1"].Interactions != 2 || got.ByModel["m2"].Interactions != 3 {
		t.Fatalf("aggregate models = %v", got.ByModel)
	}
}
