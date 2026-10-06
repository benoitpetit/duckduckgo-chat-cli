package persistence

import (
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
