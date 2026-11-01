package chat

import (
	"reflect"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
)

func TestBranchSavedSessionCopiesMessagesAndPreservesMetadata(t *testing.T) {
	manager := persistence.NewHistoryManager(t.TempDir())
	messageImage := media.ImageAttachment{Name: "message.png", MIMEType: "image/png", Data: []byte{1, 2, 3}}
	pendingImage := media.ImageAttachment{Name: "pending.png", MIMEType: "image/webp", Data: []byte{4, 5, 6}}
	messageSource := chatcontext.SourceRef{ID: "source-message", Kind: chatcontext.SourceKindFile, Name: "message.txt", State: chatcontext.SourceStateActive, Pinned: true}
	pendingSource := chatcontext.SourceRef{ID: "source-pending", Kind: chatcontext.SourceKindURL, Name: "pending", URI: "https://example.test/pending", State: chatcontext.SourceStateActive}
	parent := &persistence.ConversationSession{
		ID: "session_parent", StartTime: time.Now().Add(-time.Hour), Model: string(models.Default()),
		Messages:          []intelligence.Message{{Role: "user", Content: "original prompt", Pinned: true, Images: []media.ImageAttachment{messageImage}, Sources: []chatcontext.SourceRef{messageSource}}},
		OptimizedMessages: []intelligence.Message{{Role: "user", Content: "optimized prompt", Pinned: true, Images: []media.ImageAttachment{messageImage}, Sources: []chatcontext.SourceRef{messageSource}}},
		PendingSources:    []chatcontext.SourceRef{pendingSource}, PendingImages: []media.ImageAttachment{pendingImage},
		Title: "Parent title", Tags: []string{"work", "review"}, Favorite: true,
		ParentSessionID: "session_root", BranchName: "upstream", Notes: []string{"local note"}, Compacted: true,
	}
	if err := manager.SaveSession(parent); err != nil {
		t.Fatalf("SaveSession(parent): %v", err)
	}
	loadedParent, err := manager.LoadSession(parent.ID)
	if err != nil {
		t.Fatalf("LoadSession(parent): %v", err)
	}
	cloned := cloneConversationSession(loadedParent)
	cloned.Messages[0].Images[0].Data[0] = 90
	cloned.Messages[0].Sources[0].ID = "changed-message"
	cloned.OptimizedMessages[0].Images[0].Data[0] = 91
	cloned.OptimizedMessages[0].Sources[0].ID = "changed-optimized"
	cloned.PendingImages[0].Data[0] = 92
	cloned.PendingSources[0].Name = "changed-pending"
	cloned.Notes[0] = "changed note"
	cloned.Tags[0] = "changed tag"
	if loadedParent.Messages[0].Images[0].Data[0] != 1 || loadedParent.Messages[0].Sources[0].ID != messageSource.ID || loadedParent.OptimizedMessages[0].Images[0].Data[0] != 1 || loadedParent.OptimizedMessages[0].Sources[0].ID != messageSource.ID || loadedParent.PendingImages[0].Data[0] != 4 || loadedParent.PendingSources[0].Name != pendingSource.Name || loadedParent.Notes[0] != "local note" || loadedParent.Tags[0] != "work" {
		t.Fatal("cloned session shared mutable message, source, image, or metadata slices with its parent")
	}

	active := &Chat{
		SessionID: "session_active", Model: models.Default(),
		Messages:       []Message{{Role: "user", Content: "active conversation"}},
		HistoryManager: manager,
	}
	if err := branchSavedSession(active, parent.ID, "analysis"); err != nil {
		t.Fatalf("branchSavedSession(): %v", err)
	}
	child, err := manager.LoadSession(active.SessionID)
	if err != nil {
		t.Fatalf("LoadSession(branch): %v", err)
	}
	if child.ID == parent.ID || child.ParentSessionID != parent.ID || child.BranchName != "analysis" || child.Title != "analysis" {
		t.Fatalf("branch identity = %+v, want a named child of %s", child, parent.ID)
	}
	if !reflect.DeepEqual(child.Notes, parent.Notes) || !reflect.DeepEqual(child.Tags, parent.Tags) || child.Favorite != parent.Favorite || child.Compacted != parent.Compacted {
		t.Fatalf("branch metadata = notes:%v tags:%v favorite:%t compacted:%t; want parent metadata", child.Notes, child.Tags, child.Favorite, child.Compacted)
	}
	if len(child.Messages) != 1 || child.Messages[0].Content != parent.Messages[0].Content || !child.Messages[0].Pinned || !reflect.DeepEqual(child.Messages[0].Images, parent.Messages[0].Images) || !reflect.DeepEqual(child.Messages[0].Sources, parent.Messages[0].Sources) {
		t.Fatalf("branch messages = %+v, want copied original transcript", child.Messages)
	}
	if len(child.OptimizedMessages) != 1 || child.OptimizedMessages[0].Content != "optimized prompt" || !reflect.DeepEqual(child.PendingSources, parent.PendingSources) || !reflect.DeepEqual(child.PendingImages, parent.PendingImages) {
		t.Fatalf("branch optimized/pending context not copied: optimized=%+v sources=%+v images=%+v", child.OptimizedMessages, child.PendingSources, child.PendingImages)
	}
	if len(active.Messages) != 1 || active.Messages[0].Content != "optimized prompt" || active.SessionID != child.ID || active.ParentSessionID != parent.ID {
		t.Fatalf("active chat was not restored from the new branch: %+v", active)
	}
}

func TestSessionNotesAndPinsSurviveSaveAndRestore(t *testing.T) {
	manager := persistence.NewHistoryManager(t.TempDir())
	started := time.Date(2026, 10, 31, 9, 30, 0, 0, time.UTC)
	source := chatcontext.SourceRef{ID: "source-pinned", Kind: chatcontext.SourceKindFile, Name: "decision.txt", State: chatcontext.SourceStateActive, Pinned: true}
	image := media.ImageAttachment{Name: "reference.png", MIMEType: "image/png", Data: []byte{7, 8, 9}}
	original := &Chat{
		SessionID: "session_pins", Model: models.Default(), HistoryManager: manager, ConversationStartTime: started,
		ParentSessionID: "session_parent", BranchName: "review", Notes: []string{"keep this note"},
		SessionTitle: "Pinned decisions", SessionTags: []string{"design"}, SessionFavorite: true, Compacted: true,
		Messages: []Message{{Role: "user", Content: "keep this decision", Pinned: true, Sources: []chatcontext.SourceRef{source}, Images: []media.ImageAttachment{image}, Timestamp: started}},
	}
	if err := original.SaveCurrentSession(); err != nil {
		t.Fatalf("SaveCurrentSession(): %v", err)
	}
	saved, err := manager.LoadSession(original.SessionID)
	if err != nil {
		t.Fatalf("LoadSession(): %v", err)
	}
	restored := &Chat{}
	restored.RestoreContext(saved)
	if len(restored.Notes) != 1 || restored.Notes[0] != "keep this note" || !restored.Messages[0].Pinned || len(restored.Messages[0].Sources) != 1 || !restored.Messages[0].Sources[0].Pinned || len(restored.Messages[0].Images) != 1 || !reflect.DeepEqual(restored.Messages[0].Images[0], image) {
		t.Fatalf("restored notes or pinned context = notes:%v messages:%+v", restored.Notes, restored.Messages)
	}
	if restored.ParentSessionID != original.ParentSessionID || restored.BranchName != original.BranchName || restored.SessionTitle != original.SessionTitle || !reflect.DeepEqual(restored.SessionTags, original.SessionTags) || restored.SessionFavorite != original.SessionFavorite || restored.Compacted != original.Compacted {
		t.Fatalf("restored session metadata = %+v, want original lineage and metadata", restored)
	}
	if len(restored.Messages) != 1 || !restored.Messages[0].Timestamp.Equal(started) {
		t.Fatalf("restored timestamp = %+v, want format-version timestamp %s", restored.Messages, started)
	}
}
