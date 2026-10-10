package chat

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/persistence"
)

func TestClearResetsLocalStateWithoutCapturingBrowserProof(t *testing.T) {
	factoryCalls := 0
	proof := &fakeProofBrowser{}
	previous := sharedDuckAIBrowser
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) {
		factoryCalls++
		return proof, nil
	})
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	if _, err := sharedDuckAIBrowser.Capture(context.Background()); err != nil {
		t.Fatal(err)
	}
	jar := newDuckAICookieJar()
	site, _ := url.Parse("https://duck.ai/")
	jar.SetCookies(site, []*http.Cookie{{Name: "stale", Value: "session"}})

	chatSession := &Chat{
		Client:                &http.Client{Jar: jar},
		CookieJar:             jar,
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
	if err := chatSession.Clear(&config.Config{ShowMenu: true}); err != nil {
		t.Fatal(err)
	}

	if len(chatSession.Messages) != 0 {
		t.Fatalf("messages after Clear() = %d, want 0", len(chatSession.Messages))
	}
	if chatSession.VqdHash1 != "" || chatSession.FeSignals != "" || chatSession.FeVersion != "" || chatSession.NewVqd != "" || chatSession.OldVqd != "" {
		t.Fatal("Clear() retained proof headers from the previous chat session")
	}
	if chatSession.SessionID == "session_clear_test" {
		t.Fatal("Clear() did not create a new session id")
	}
	if factoryCalls != 1 {
		t.Fatalf("browser factory calls = %d, want no new capture during Clear()", factoryCalls)
	}
	if _, _, closed := proof.stats(); closed != 1 {
		t.Fatalf("proof browser closes = %d, want 1", closed)
	}
	if chatSession.Client.Jar != chatSession.CookieJar || chatSession.CookieJar == jar {
		t.Fatal("Clear() did not replace the HTTP cookie jar")
	}
	for _, cookie := range chatSession.CookieJar.Cookies(site) {
		if cookie.Name == "stale" {
			t.Fatal("Clear() retained a Duck.ai cookie")
		}
	}
}

func TestClearResetsSessionWhenHistoryIsAlreadyEmpty(t *testing.T) {
	previous := sharedDuckAIBrowser
	proof := &fakeProofBrowser{}
	sharedDuckAIBrowser = newBrowserManager(func(context.Context) (proofBrowser, error) { return proof, nil })
	t.Cleanup(func() { sharedDuckAIBrowser = previous })
	if _, err := sharedDuckAIBrowser.Capture(context.Background()); err != nil {
		t.Fatal(err)
	}
	chatSession := &Chat{SessionID: "stale", VqdHash1: "stale"}
	if err := chatSession.Clear(&config.Config{ShowMenu: true}); err != nil {
		t.Fatal(err)
	}
	if chatSession.SessionID == "stale" || chatSession.VqdHash1 != "" {
		t.Fatal("Clear() retained the old session when the message list was empty")
	}
	if _, _, closed := proof.stats(); closed != 1 {
		t.Fatalf("proof browser closes = %d, want 1", closed)
	}
}

func TestNewDuckAICookieJarStartsEmpty(t *testing.T) {
	site, _ := url.Parse("https://duck.ai/")
	if cookies := newDuckAICookieJar().Cookies(site); len(cookies) != 0 {
		t.Fatalf("new Duck.ai session has %d preloaded cookies, want none", len(cookies))
	}
}
