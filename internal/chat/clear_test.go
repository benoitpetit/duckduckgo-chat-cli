package chat

import (
	"context"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/persistence"
)

func TestClearResetsLocalStateWithoutCapturingBrowserProof(t *testing.T) {
	factoryCalls := 0
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		factoryCalls++
		return &fakeProofBrowser{}, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })

	chatSession := &Chat{
		VqdHash1:              "stale-proof",
		FeSignals:             "old-signals",
		FeVersion:             "old-version",
		NewVqd:                "old-vqd",
		OldVqd:                "older-vqd",
		Messages:              []Message{{Role: "user", Content: "hello"}},
		SessionID:             "session_clear_test",
		ConversationStartTime: time.Now(),
		HistoryManager:        persistence.NewHistoryManager(t.TempDir()),
	}
	chatSession.Clear(&config.Config{ShowMenu: true})

	if len(chatSession.Messages) != 0 {
		t.Fatalf("messages after Clear() = %d, want 0", len(chatSession.Messages))
	}
	if chatSession.VqdHash1 != "" || chatSession.FeSignals != "" || chatSession.FeVersion != "" || chatSession.NewVqd != "" || chatSession.OldVqd != "" {
		t.Fatal("Clear() retained proof headers from the previous chat session")
	}
	if chatSession.SessionID == "session_clear_test" {
		t.Fatal("Clear() did not create a new session id")
	}
	if factoryCalls != 0 {
		t.Fatalf("browser factory calls during Clear() = %d, want 0", factoryCalls)
	}
}
