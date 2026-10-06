package persistence

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/intelligence"
)

func TestHistoryManagerUsesConfigurableRetention(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	if manager.RetentionDays != 90 {
		t.Fatalf("default retention = %d, want 90", manager.RetentionDays)
	}
	old := &ConversationSession{ID: "old", StartTime: time.Now().Add(-60 * 24 * time.Hour), Messages: []intelligence.Message{{Role: "user", Content: "old"}}}
	recent := &ConversationSession{ID: "recent", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: "recent"}}}
	if err := manager.SaveSession(old); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveSession(recent); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRetentionDays(30); err != nil {
		t.Fatal(err)
	}
	sessions, err := manager.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "recent" {
		t.Fatalf("sessions after retention update = %+v, want only recent", sessions)
	}
}

func TestHistoryManagerRejectsInvalidRetention(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	if err := manager.SetRetentionDays(0); err == nil {
		t.Fatal("SetRetentionDays(0) succeeded")
	}
}

func TestListSessionSummariesAreNewestFirstWithPreviewOnly(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	older := &ConversationSession{ID: "session_older", StartTime: time.Now().Add(-time.Hour), Model: "model-old", Messages: []intelligence.Message{{Role: "user", Content: "older conversation"}, {Role: "assistant", Content: "answer"}}}
	newer := &ConversationSession{ID: "session_newer", StartTime: time.Now(), Model: "model-new", Messages: []intelligence.Message{{Role: "user", Content: "newer conversation"}, {Role: "assistant", Content: "secret transcript"}}}
	if err := manager.SaveSession(older); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveSession(newer); err != nil {
		t.Fatal(err)
	}
	summaries, err := manager.ListSessionSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].ID != newer.ID || summaries[1].ID != older.ID {
		t.Fatalf("summary ordering = %+v", summaries)
	}
	if summaries[0].FirstMessage != "newer conversation" || summaries[0].ResumeCommand != "/load session_newer" {
		t.Fatalf("summary preview/resume command = %+v", summaries[0])
	}
	encoded, err := json.Marshal(summaries)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret transcript") {
		t.Fatalf("summary output contains transcript content: %s", encoded)
	}
}
