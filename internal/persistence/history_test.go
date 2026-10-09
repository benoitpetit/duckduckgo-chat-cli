package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/media"
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

func TestHistoryManagerRestartAppliesLongerRetentionBeforePruning(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	if err := manager.SetRetentionDays(365); err != nil {
		t.Fatal(err)
	}
	archived := &ConversationSession{ID: "session_120_days", StartTime: time.Now().Add(-120 * 24 * time.Hour), Messages: []intelligence.Message{{Role: "user", Content: "keep me"}}}
	if err := manager.SaveSession(archived); err != nil {
		t.Fatal(err)
	}

	// NewHistoryManager must not apply its 90-day fallback before the caller
	// can restore the configured 365-day retention.
	restarted := NewHistoryManager(dir)
	if err := restarted.SetRetentionDays(365); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.LoadSession(archived.ID)
	if err != nil || got.ID != archived.ID {
		t.Fatalf("120-day archive after restart = (%v, %v), want retained archive", got, err)
	}
}

func TestHistoryListingsTreatMissingStorageAsEmpty(t *testing.T) {
	manager := NewHistoryManager(filepath.Join(t.TempDir(), "not-created"))
	if summaries, err := manager.ListSessionSummaries(); err != nil || len(summaries) != 0 {
		t.Fatalf("ListSessionSummaries() = (%v, %v), want empty", summaries, err)
	}
	if sessions, err := manager.ListConversationSessions(); err != nil || len(sessions) != 0 {
		t.Fatalf("ListConversationSessions() = (%v, %v), want empty", sessions, err)
	}
}

func TestHistoryManagerRejectsUnsafeSessionIDs(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	for _, id := range []string{"../outside", "..\\outside", "", ".", "session/../../outside"} {
		if _, err := manager.LoadSession(id); err == nil {
			t.Errorf("LoadSession(%q) succeeded, want invalid ID error", id)
		}
		if err := manager.SaveSession(&ConversationSession{ID: id}); err == nil {
			t.Errorf("SaveSession(%q) succeeded, want invalid ID error", id)
		}
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

func TestSessionRoundTripPreservesImageAttachments(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	image := media.ImageAttachment{Name: "logo.png", MIMEType: "image/png", Data: []byte{1, 2, 3, 4}}
	session := &ConversationSession{
		ID: "session_with_image", StartTime: time.Now(), Model: "gpt-5.6-luna",
		Messages: []intelligence.Message{{Role: "user", Content: "Describe this", Images: []media.ImageAttachment{image}}},
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	loaded, err := manager.LoadSession(session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(loaded.Messages) != 1 || len(loaded.Messages[0].Images) != 1 {
		t.Fatalf("loaded messages = %+v, want one image-bearing message", loaded.Messages)
	}
	got := loaded.Messages[0].Images[0]
	if got.Name != image.Name || got.MIMEType != image.MIMEType || string(got.Data) != string(image.Data) {
		t.Fatalf("loaded attachment = %+v, want original metadata and bytes", got)
	}
}

func TestSaveSessionReplacesLegacyUncompressedArchive(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	legacy := ConversationSession{
		ID: "session_duplicate", StartTime: time.Now().Add(-time.Hour),
		Messages: []intelligence.Message{{Role: "user", Content: "old"}},
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, "session_session_duplicate.json")
	if err := os.WriteFile(legacyPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	updated := &ConversationSession{
		ID: "session_duplicate", StartTime: time.Now(),
		Messages: []intelligence.Message{{Role: "user", Content: "new"}},
	}
	if err := manager.SaveSession(updated); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy archive still exists, stat error = %v", err)
	}
	sessions, err := manager.ListSessions()
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions() = %d sessions, error = %v; want one", len(sessions), err)
	}
	loaded, err := manager.LoadSession(updated.ID)
	if err != nil || loaded.Messages[0].Content != "new" {
		t.Fatalf("LoadSession() = (%+v, %v), want updated session", loaded, err)
	}
}
