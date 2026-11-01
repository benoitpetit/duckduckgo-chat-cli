package chat

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/persistence"
)

func TestSetContextPinUpdatesSelectedMessageAndSource(t *testing.T) {
	c := &Chat{
		Messages: []Message{{
			Role: "user", Content: "source content",
			Sources: []chatcontext.SourceRef{{ID: "source-1", Name: "notes.txt"}},
		}},
		PendingSources: []chatcontext.SourceRef{{ID: "source-2", Name: "pending.txt"}},
	}

	if err := setContextPin(c, "1", true); err != nil {
		t.Fatalf("pin message: %v", err)
	}
	if !c.Messages[0].Pinned {
		t.Fatal("selected message was not pinned")
	}
	if err := setContextPin(c, "source-1", true); err != nil {
		t.Fatalf("pin retained source: %v", err)
	}
	if !c.Messages[0].Sources[0].Pinned {
		t.Fatal("selected retained source was not pinned")
	}
	if err := setContextPin(c, "source-2", true); err != nil {
		t.Fatalf("pin pending source: %v", err)
	}
	if !c.PendingSources[0].Pinned {
		t.Fatal("selected pending source was not pinned")
	}
	if err := setContextPin(c, "source-1", false); err != nil {
		t.Fatalf("unpin retained source: %v", err)
	}
	if c.Messages[0].Sources[0].Pinned {
		t.Fatal("selected retained source remained pinned after unpin")
	}
}

func TestCompactConversationRestoresStateAfterSummaryFailureOrCancel(t *testing.T) {
	tests := []struct {
		name       string
		summaryErr error
		confirmed  bool
		wantErr    bool
	}{
		{name: "summary error", summaryErr: errors.New("summary unavailable"), wantErr: true},
		{name: "confirmation canceled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := newCompactionTestChat(t)
			beforeMessages := cloneChatMessages(c.Messages)
			beforeImages := cloneImageAttachments(c.pendingImages)
			beforeSources := cloneSourceRefs(c.PendingSources)
			beforeChanges := append([]intelligence.OptimizationChange(nil), c.LastOptimizationChanges...)
			beforeNotes := append([]string(nil), c.Notes...)
			beforeSessionID, beforeParentID, beforeBranch := c.SessionID, c.ParentSessionID, c.BranchName
			beforeTitle := c.SessionTitle
			beforeTags := append([]string(nil), c.SessionTags...)
			beforeFavorite, beforeStarted, beforeCompacted := c.SessionFavorite, c.ConversationStartTime, c.Compacted
			confirmCalls := 0

			deps := compactionDependencies{
				summarize: func(chat *Chat, _ *config.Config, _ string) (string, error) {
					simulateCompactionRequestMutation(chat)
					return "summary", test.summaryErr
				},
				confirm: func(string) (bool, error) {
					confirmCalls++
					return test.confirmed, nil
				},
				save: func(*Chat) error {
					t.Fatal("unexpected session save before the summary is accepted")
					return nil
				},
			}
			err := compactConversationWithDependencies(c, &config.Config{}, "focus", deps)
			if test.wantErr && err == nil {
				t.Fatal("compactConversationWithDependencies() error = nil, want summary error")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("compactConversationWithDependencies() error = %v", err)
			}
			if test.wantErr && confirmCalls != 0 {
				t.Fatalf("confirmation called %d times after summary error, want 0", confirmCalls)
			}
			if !test.wantErr && confirmCalls != 1 {
				t.Fatalf("confirmation called %d times, want 1", confirmCalls)
			}
			if !reflect.DeepEqual(c.Messages, beforeMessages) || !reflect.DeepEqual(c.pendingImages, beforeImages) || !reflect.DeepEqual(c.PendingSources, beforeSources) || !reflect.DeepEqual(c.LastOptimizationChanges, beforeChanges) {
				t.Fatalf("compaction attempt changed active context:\nmessages: got %+v want %+v\nimages: got %+v want %+v\nsources: got %+v want %+v\nchanges: got %+v want %+v", c.Messages, beforeMessages, c.pendingImages, beforeImages, c.PendingSources, beforeSources, c.LastOptimizationChanges, beforeChanges)
			}
			if !reflect.DeepEqual(c.Notes, beforeNotes) || c.SessionID != beforeSessionID || c.ParentSessionID != beforeParentID || c.BranchName != beforeBranch || c.SessionTitle != beforeTitle || !reflect.DeepEqual(c.SessionTags, beforeTags) || c.SessionFavorite != beforeFavorite || !c.ConversationStartTime.Equal(beforeStarted) || c.Compacted != beforeCompacted {
				t.Fatalf("compaction attempt changed session metadata: %+v", c)
			}
			saved, err := c.HistoryManager.ListSessionSummaries()
			if err != nil {
				t.Fatalf("list sessions after canceled compaction: %v", err)
			}
			if len(saved) != 0 {
				t.Fatalf("compaction attempt saved %d sessions before acceptance", len(saved))
			}
		})
	}
}

func TestCompactConversationSavesFullParentAndCompactChild(t *testing.T) {
	c := newCompactionTestChat(t)
	originalMessages := cloneChatMessages(c.Messages)
	saveCalls := 0
	deps := compactionDependencies{
		summarize: func(chat *Chat, _ *config.Config, objective string) (string, error) {
			if objective != "focus" {
				t.Fatalf("summary objective = %q, want %q", objective, "focus")
			}
			simulateCompactionRequestMutation(chat)
			return "restart summary", nil
		},
		confirm: func(summary string) (bool, error) {
			if summary != "restart summary" {
				t.Fatalf("confirmation summary = %q", summary)
			}
			return true, nil
		},
		save: func(chat *Chat) error {
			saveCalls++
			return chat.SaveCurrentSession()
		},
	}

	if err := compactConversationWithDependencies(c, &config.Config{}, "focus", deps); err != nil {
		t.Fatalf("compactConversationWithDependencies() error = %v", err)
	}
	if saveCalls != 2 {
		t.Fatalf("session save calls = %d, want parent and child saves", saveCalls)
	}
	parent, err := c.HistoryManager.LoadSession("session_parent")
	if err != nil {
		t.Fatalf("load full parent session: %v", err)
	}
	if len(parent.Messages) != len(originalMessages) {
		t.Fatalf("parent message count = %d, want full transcript count %d", len(parent.Messages), len(originalMessages))
	}
	for index, message := range originalMessages {
		if parent.Messages[index].Content != message.Content || parent.Messages[index].Pinned != message.Pinned {
			t.Fatalf("parent message %d = %+v, want original %+v", index, parent.Messages[index], message)
		}
	}
	if c.SessionID == "session_parent" || c.ParentSessionID != "session_parent" || !c.Compacted || c.BranchName != "compacted" {
		t.Fatalf("active compact session metadata = %+v, want a compact child of session_parent", c)
	}
	child, err := c.HistoryManager.LoadSession(c.SessionID)
	if err != nil {
		t.Fatalf("load compact child session: %v", err)
	}
	if !child.Compacted || child.ParentSessionID != parent.ID || child.BranchName != "compacted" || child.Title != "Compacted: Work session" {
		t.Fatalf("compact child metadata = %+v", child)
	}
	if !reflect.DeepEqual(child.Notes, c.Notes) || !reflect.DeepEqual(child.Tags, c.SessionTags) || child.Favorite != c.SessionFavorite {
		t.Fatalf("compact child did not preserve notes and metadata: %+v", child)
	}
	if len(child.Messages) != 2 || child.Messages[0].Content != "pinned context" || !child.Messages[0].Pinned || !strings.Contains(child.Messages[1].Content, "restart summary") {
		t.Fatalf("compact child messages = %+v, want pinned material and summary", child.Messages)
	}
	if len(child.Messages[1].Images) != 1 || string(child.Messages[1].Images[0].Data) != "pending image" {
		t.Fatalf("compact summary images = %+v, want retained pending image", child.Messages[1].Images)
	}
	if len(child.PendingSources) != 0 || len(child.PendingImages) != 0 {
		t.Fatalf("compact child retained pending context: sources=%+v images=%+v", child.PendingSources, child.PendingImages)
	}
}

func TestCompactConversationRestoresActiveStateWhenChildSaveFails(t *testing.T) {
	c := newCompactionTestChat(t)
	beforeMessages := cloneChatMessages(c.Messages)
	beforeImages := cloneImageAttachments(c.pendingImages)
	beforeSources := cloneSourceRefs(c.PendingSources)
	beforeChanges := append([]intelligence.OptimizationChange(nil), c.LastOptimizationChanges...)
	beforeSessionID, beforeParentID, beforeBranch := c.SessionID, c.ParentSessionID, c.BranchName
	beforeTitle := c.SessionTitle
	beforeTags := append([]string(nil), c.SessionTags...)
	beforeFavorite, beforeStarted, beforeCompacted := c.SessionFavorite, c.ConversationStartTime, c.Compacted
	saveCalls := 0
	deps := compactionDependencies{
		summarize: func(chat *Chat, _ *config.Config, _ string) (string, error) {
			simulateCompactionRequestMutation(chat)
			return "restart summary", nil
		},
		confirm: func(string) (bool, error) { return true, nil },
		save: func(chat *Chat) error {
			saveCalls++
			if saveCalls == 2 {
				return errors.New("child archive unavailable")
			}
			return chat.SaveCurrentSession()
		},
	}

	err := compactConversationWithDependencies(c, &config.Config{}, "", deps)
	if err == nil || !strings.Contains(err.Error(), "save compacted session") {
		t.Fatalf("compactConversationWithDependencies() error = %v, want compact-child save error", err)
	}
	if saveCalls != 2 {
		t.Fatalf("session save calls = %d, want parent save and failing child save", saveCalls)
	}
	if !reflect.DeepEqual(c.Messages, beforeMessages) || !reflect.DeepEqual(c.pendingImages, beforeImages) || !reflect.DeepEqual(c.PendingSources, beforeSources) || !reflect.DeepEqual(c.LastOptimizationChanges, beforeChanges) {
		t.Fatalf("failed child save changed active context:\nmessages: got %+v want %+v\nimages: got %+v want %+v\nsources: got %+v want %+v\nchanges: got %+v want %+v", c.Messages, beforeMessages, c.pendingImages, beforeImages, c.PendingSources, beforeSources, c.LastOptimizationChanges, beforeChanges)
	}
	if c.SessionID != beforeSessionID || c.ParentSessionID != beforeParentID || c.BranchName != beforeBranch || c.SessionTitle != beforeTitle || !reflect.DeepEqual(c.SessionTags, beforeTags) || c.SessionFavorite != beforeFavorite || !c.ConversationStartTime.Equal(beforeStarted) || c.Compacted != beforeCompacted {
		t.Fatalf("failed child save changed session metadata: %+v", c)
	}
	parent, err := c.HistoryManager.LoadSession(beforeSessionID)
	if err != nil {
		t.Fatalf("full parent was lost after child-save failure: %v", err)
	}
	if len(parent.Messages) != len(beforeMessages) {
		t.Fatalf("remaining parent has %d messages, want full transcript count %d", len(parent.Messages), len(beforeMessages))
	}
	summaries, err := c.HistoryManager.ListSessionSummaries()
	if err != nil {
		t.Fatalf("list sessions after child-save failure: %v", err)
	}
	if len(summaries) != 1 || summaries[0].ID != beforeSessionID {
		t.Fatalf("saved sessions after child-save failure = %+v, want only original parent", summaries)
	}
}

func TestCompactConversationRestoresSessionIDWhenParentSaveFails(t *testing.T) {
	c := newCompactionTestChat(t)
	c.SessionID = ""
	deps := compactionDependencies{
		summarize: func(chat *Chat, _ *config.Config, _ string) (string, error) {
			simulateCompactionRequestMutation(chat)
			return "restart summary", nil
		},
		confirm: func(string) (bool, error) { return true, nil },
		save:    func(*Chat) error { return errors.New("parent archive unavailable") },
	}

	err := compactConversationWithDependencies(c, &config.Config{}, "", deps)
	if err == nil || !strings.Contains(err.Error(), "save original conversation") {
		t.Fatalf("compactConversationWithDependencies() error = %v, want parent-save error", err)
	}
	if c.SessionID != "" {
		t.Fatalf("failed parent save left session ID %q in active chat", c.SessionID)
	}
}

func newCompactionTestChat(t *testing.T) *Chat {
	t.Helper()
	return &Chat{
		HistoryManager: persistence.NewHistoryManager(t.TempDir()),
		SessionID:      "session_parent", ParentSessionID: "session_grandparent", BranchName: "work",
		ConversationStartTime: time.Now().Add(-time.Hour).UTC().Truncate(time.Second),
		SessionTitle:          "Work session", SessionTags: []string{"review", "context"}, SessionFavorite: true,
		Notes: []string{"keep this local note"},
		Messages: []Message{
			{Role: "user", Content: "pinned context", Pinned: true, Timestamp: time.Now(), Sources: []chatcontext.SourceRef{{ID: "source-1", Kind: chatcontext.SourceKindFile, Name: "notes.txt", SizeBytes: 32, State: chatcontext.SourceStateActive, Pinned: true}}},
			{Role: "assistant", Content: "old answer", Timestamp: time.Now()},
			{Role: "user", Content: "current question", Timestamp: time.Now()},
		},
		PendingSources:          []chatcontext.SourceRef{{ID: "source-2", Kind: chatcontext.SourceKindURL, Name: "pending source", URI: "https://example.test", SizeBytes: 12, State: chatcontext.SourceStateActive}},
		pendingImages:           []media.ImageAttachment{{Name: "pending.png", MIMEType: "image/png", Data: []byte("pending image")}},
		LastOptimizationChanges: []intelligence.OptimizationChange{{SourceID: "source-1", State: chatcontext.SourceStateCompressed, BytesBefore: 32, BytesAfter: 16}},
	}
}

func simulateCompactionRequestMutation(c *Chat) {
	c.Messages = append(c.Messages, Message{Role: "user", Content: "generated summary request"}, Message{Role: "assistant", Content: "generated response"})
	c.pendingImages = nil
	c.PendingSources = nil
	c.LastOptimizationChanges = nil
}
