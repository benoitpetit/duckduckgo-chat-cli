package chat

import (
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
)

func TestBuildLoadSessionOptionsShowsFormattedSessionDetails(t *testing.T) {
	started := time.Date(2026, time.October, 11, 9, 7, 0, 0, time.UTC)
	summaries := []persistence.SessionSummary{
		{ID: "session_new", StartTime: started, MessageCount: 12, Model: "gpt-5.6-luna", FirstMessage: "Help me plan a quiet weekend by the sea"},
		{ID: "session_old", StartTime: started.Add(-24 * time.Hour), MessageCount: 3, Model: "gpt-5.6-sol", FirstMessage: "Explain how tides work"},
	}
	options, sessionIDs := buildLoadSessionOptions(summaries)
	if len(options) != 2 || len(sessionIDs) != 2 {
		t.Fatalf("got %d options and %d IDs, want two of each", len(options), len(sessionIDs))
	}
	for _, want := range []string{"2026-10-11 09:07", "gpt-5.6-luna", "12 msgs", "Help me plan a quiet weekend by the sea"} {
		if !strings.Contains(options[0], want) {
			t.Errorf("option %q does not contain %q", options[0], want)
		}
	}
	if sessionIDs[0] != "session_new" || sessionIDs[1] != "session_old" {
		t.Fatalf("session IDs = %v, want IDs in option order", sessionIDs)
	}
	if options[0] == options[1] {
		t.Fatalf("options are not unique: %v", options)
	}
}

func TestBuildLoadSessionOptionsSanitizesAndTruncatesPreview(t *testing.T) {
	summaries := []persistence.SessionSummary{{
		ID: "session_control", StartTime: time.Date(2026, 10, 11, 9, 7, 0, 0, time.UTC),
		MessageCount: 1, Model: "model", FirstMessage: "hello\n\x1b[31mworld\x1b[0m " + strings.Repeat("long ", 20),
	}}
	options, _ := buildLoadSessionOptions(summaries)
	if strings.Contains(options[0], "\x1b") || strings.Contains(options[0], "\n") {
		t.Fatalf("session option contains terminal controls or line breaks: %q", options[0])
	}
	if len([]rune(options[0])) > 112 {
		t.Fatalf("session option is too long for the framed selector: %d runes: %q", len([]rune(options[0])), options[0])
	}
	if !strings.Contains(options[0], "hello world") || !strings.Contains(options[0], "…") {
		t.Fatalf("session preview was not normalized and truncated: %q", options[0])
	}
}

func TestLoadCommandCanRestoreOriginalTranscriptWhenOptimizedCopyExists(t *testing.T) {
	manager := persistence.NewHistoryManager(t.TempDir())
	session := &persistence.ConversationSession{
		ID:        "session_full_transcript",
		Model:     string(models.Default()),
		StartTime: time.Now(),
		Messages: []intelligence.Message{
			{Role: "user", Content: "original question"},
			{Role: "assistant", Content: "original answer"},
		},
		OptimizedMessages: []intelligence.Message{
			{Role: "user", Content: "optimized summary"},
		},
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	conversation := &Chat{HistoryManager: manager}
	HandleLoadCommand(conversation, session.ID+" --full")
	if len(conversation.Messages) != 2 || conversation.Messages[0].Content != "original question" || conversation.Messages[1].Content != "original answer" {
		t.Fatalf("loaded messages = %+v, want the original two-message transcript", conversation.Messages)
	}
}
