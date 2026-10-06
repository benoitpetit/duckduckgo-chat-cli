package chat

import (
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
)

func TestSaveCurrentSessionSynchronouslyPreservesSessionMetadata(t *testing.T) {
	manager := persistence.NewHistoryManager(t.TempDir())
	tracker := analytics.NewChatAnalytics()
	started := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	tracker.SessionStartTime = started
	tracker.RecordMessage("user", len("private preview"))
	chat := &Chat{
		SessionID:      "session-final",
		Model:          models.Default(),
		Messages:       []Message{{Role: "user", Content: "private preview"}},
		Analytics:      tracker,
		HistoryManager: manager,
	}

	if err := chat.SaveCurrentSession(); err != nil {
		t.Fatal(err)
	}
	saved, err := manager.LoadSession(chat.SessionID)
	if err != nil {
		t.Fatalf("final save returned before session was readable: %v", err)
	}
	if !saved.StartTime.Equal(started) || saved.Analytics.MessageCount != 1 || saved.Model != string(models.Default()) {
		t.Fatalf("saved metadata = %+v, want start=%s, messages=1, model=%s", saved, started, models.Default())
	}
}

func TestDashboardAnalysisDisablesSensitiveDebugPayloads(t *testing.T) {
	t.Setenv("DEBUG", "true")
	regular := &Chat{}
	private := &Chat{suppressSensitiveDebugLogs: true}
	if !shouldLogRequestDetails(regular) {
		t.Fatal("ordinary CLI requests should retain DEBUG diagnostics")
	}
	if shouldLogRequestDetails(private) {
		t.Fatal("dashboard analysis must never print transcript-bearing request or error payloads")
	}
}

func TestSaveCurrentSessionUsesConversationStartTime(t *testing.T) {
	manager := persistence.NewHistoryManager(t.TempDir())
	tracker := analytics.NewChatAnalytics()
	processStart := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	tracker.SessionStartTime = processStart
	conversationStart := time.Now().Add(-20 * time.Minute).UTC().Truncate(time.Second)
	chat := &Chat{
		SessionID: "session-conversation-time", Model: models.Default(),
		Messages:  []Message{{Role: "user", Content: "separate conversation timing"}},
		Analytics: tracker, HistoryManager: manager, ConversationStartTime: conversationStart,
	}
	if err := chat.SaveCurrentSession(); err != nil {
		t.Fatal(err)
	}
	saved, err := manager.LoadSession(chat.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.StartTime.Equal(conversationStart) {
		t.Fatalf("saved conversation start = %s, want %s (process start %s)", saved.StartTime, conversationStart, processStart)
	}
}
