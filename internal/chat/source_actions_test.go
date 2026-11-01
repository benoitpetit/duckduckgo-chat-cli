package chat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/persistence"
	"duckduckgo-chat-cli/internal/scrape"
)

func TestFindSourceContentRejectsDuplicateFileNameMarkers(t *testing.T) {
	conversation := &Chat{Messages: []Message{{
		Role: "user",
		Content: "[File Context]\nFile: README.md\n\nfirst file body" +
			"\n\n[File Context]\nFile: README.md\n\nsecond file body",
		Sources: []chatcontext.SourceRef{
			{ID: "source-1", Kind: chatcontext.SourceKindFile, Name: "README.md", URI: "/project-a/README.md"},
			{ID: "source-2", Kind: chatcontext.SourceKindFile, Name: "README.md", URI: "/project-b/README.md"},
		},
	}}}

	_, _, err := findSourceContent(conversation, "source-1")
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "ambiguous") {
		t.Fatalf("findSourceContent() error = %v, want an ambiguity error", err)
	}
}

func TestSourceBundleSaveLoadAndDeleteRoundTrip(t *testing.T) {
	root := t.TempDir()
	manager := persistence.NewHistoryManager(filepath.Join(root, "history"))
	filePath := filepath.Join(root, "brief.txt")
	if err := os.WriteFile(filePath, []byte("source body"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := chatcontext.SourceRef{ID: "source-original", Kind: chatcontext.SourceKindFile, Name: "brief.txt", URI: filePath, SizeBytes: len("source body"), State: chatcontext.SourceStateActive}
	original := &Chat{HistoryManager: manager, Messages: []Message{{Role: "user", Content: "question", Sources: []chatcontext.SourceRef{source}}}}
	if err := saveSourceBundle(original, "reading-list"); err != nil {
		t.Fatalf("saveSourceBundle(): %v", err)
	}
	bundles, _, err := readSourceBundles(original)
	if err != nil {
		t.Fatalf("readSourceBundles(): %v", err)
	}
	if bundle := bundles["reading-list"]; bundle.Name != "reading-list" || len(bundle.Sources) != 1 || bundle.Sources[0] != source {
		t.Fatalf("saved source bundle = %+v, want source name and URI preserved", bundle)
	}

	restored := &Chat{HistoryManager: manager}
	if err := loadSourceBundle(restored, "reading-list"); err != nil {
		t.Fatalf("loadSourceBundle(): %v", err)
	}
	if len(restored.Messages) != 1 || !strings.Contains(restored.Messages[0].Content, "source body") || len(restored.PendingSources) != 1 || restored.PendingSources[0].Name != source.Name || restored.PendingSources[0].URI != source.URI || restored.PendingSources[0].Kind != source.Kind {
		t.Fatalf("restored source set = messages:%+v pending:%+v", restored.Messages, restored.PendingSources)
	}
	if err := deleteSourceBundle(restored, "reading-list"); err != nil {
		t.Fatalf("deleteSourceBundle(): %v", err)
	}
	bundles, _, err = readSourceBundles(restored)
	if err != nil || len(bundles) != 0 {
		t.Fatalf("source sets after delete = (%+v, %v), want empty", bundles, err)
	}
}

func TestSourceBundleRejectsInvalidNamesAndMalformedJSON(t *testing.T) {
	root := t.TempDir()
	manager := persistence.NewHistoryManager(filepath.Join(root, "history"))
	source := chatcontext.SourceRef{ID: "source-1", Kind: chatcontext.SourceKindFile, Name: "brief.txt", URI: filepath.Join(root, "brief.txt"), State: chatcontext.SourceStateActive}
	c := &Chat{HistoryManager: manager, Messages: []Message{{Role: "user", Content: "question", Sources: []chatcontext.SourceRef{source}}}}
	sentinelPath := filepath.Join(root, "source-bundles.json")
	sentinel := []byte("outside storage sentinel")
	if err := os.WriteFile(sentinelPath, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../escape", "nested/name", "bad name", ""} {
		if err := saveSourceBundle(c, name); err == nil {
			t.Errorf("saveSourceBundle(%q) succeeded, want invalid-name error", name)
		}
		if err := deleteSourceBundle(c, name); err == nil {
			t.Errorf("deleteSourceBundle(%q) succeeded, want invalid-name error", name)
		}
	}
	if data, err := os.ReadFile(sentinelPath); err != nil || string(data) != string(sentinel) {
		t.Fatalf("invalid source-set name changed the path outside storage: data=%q err=%v", data, err)
	}

	storagePath := filepath.Join(manager.StorageDir, "source-bundles.json")
	malformed := []byte(`{"preserve":`)
	if err := os.MkdirAll(manager.StorageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storagePath, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveSourceBundle(c, "valid-name"); err == nil {
		t.Fatal("saveSourceBundle() overwrote malformed source-set data")
	}
	if err := deleteSourceBundle(c, "existing"); err == nil {
		t.Fatal("deleteSourceBundle() accepted malformed source-set data")
	}
	if data, err := os.ReadFile(storagePath); err != nil || string(data) != string(malformed) {
		t.Fatalf("malformed source-set file changed after rejected operations: data=%q err=%v", data, err)
	}
}

func TestLoadSourceBundleContinuesAfterUnavailableSource(t *testing.T) {
	root := t.TempDir()
	manager := persistence.NewHistoryManager(filepath.Join(root, "history"))
	availablePath := filepath.Join(root, "available.txt")
	if err := os.WriteFile(availablePath, []byte("available body"), 0o600); err != nil {
		t.Fatal(err)
	}
	sources := []chatcontext.SourceRef{
		{ID: "source-available", Kind: chatcontext.SourceKindFile, Name: "available.txt", URI: availablePath, State: chatcontext.SourceStateActive},
		{ID: "source-missing", Kind: chatcontext.SourceKindFile, Name: "missing.txt", URI: filepath.Join(root, "missing.txt"), State: chatcontext.SourceStateActive},
		{ID: "source-web", Kind: chatcontext.SourceKindURL, Name: "web source", URI: "https://example.test/unavailable", State: chatcontext.SourceStateActive},
		{ID: "source-empty-web", Kind: chatcontext.SourceKindSearch, Name: "empty web source", URI: "https://example.test/empty", State: chatcontext.SourceStateActive},
	}
	if err := writeSourceBundles(filepath.Join(manager.StorageDir, "source-bundles.json"), map[string]savedSourceBundle{
		"mixed": {Name: "mixed", SavedAt: time.Now(), Sources: sources},
	}); err != nil {
		t.Fatalf("writeSourceBundles(): %v", err)
	}
	fetchCalls := 0
	fetch := func(uri string) (*scrape.WebContentResult, error) {
		fetchCalls++
		switch uri {
		case sources[2].URI:
			return nil, errors.New("offline")
		case sources[3].URI:
			return nil, nil
		default:
			t.Fatalf("unexpected web fetch URI = %q", uri)
		}
		return nil, nil
	}
	c := &Chat{HistoryManager: manager}
	if err := loadSourceBundleWithFetcher(c, "mixed", fetch); err != nil {
		t.Fatalf("loadSourceBundleWithFetcher(): %v, want partial success", err)
	}
	if fetchCalls != 2 || len(c.Messages) != 1 || !strings.Contains(c.Messages[0].Content, "available body") || len(c.PendingSources) != 1 || c.PendingSources[0].Name != sources[0].Name {
		t.Fatalf("partial source load = calls:%d messages:%+v pending:%+v", fetchCalls, c.Messages, c.PendingSources)
	}
}

func TestLoadSourceBundleUsesInjectedWebFetcher(t *testing.T) {
	manager := persistence.NewHistoryManager(t.TempDir())
	source := chatcontext.SourceRef{ID: "source-web", Kind: chatcontext.SourceKindURL, Name: "release notes", URI: "https://example.test/releases", State: chatcontext.SourceStateActive}
	if err := writeSourceBundles(filepath.Join(manager.StorageDir, "source-bundles.json"), map[string]savedSourceBundle{
		"web": {Name: "web", Sources: []chatcontext.SourceRef{source}},
	}); err != nil {
		t.Fatalf("writeSourceBundles(): %v", err)
	}
	c := &Chat{HistoryManager: manager}
	fetchCalls := 0
	fetch := func(uri string) (*scrape.WebContentResult, error) {
		fetchCalls++
		if uri != source.URI {
			t.Fatalf("web fetch URI = %q, want %q", uri, source.URI)
		}
		return &scrape.WebContentResult{URL: uri, Content: "injected page body"}, nil
	}
	if err := loadSourceBundleWithFetcher(c, "web", fetch); err != nil {
		t.Fatalf("loadSourceBundleWithFetcher(): %v", err)
	}
	if fetchCalls != 1 || len(c.Messages) != 1 || !strings.Contains(c.Messages[0].Content, "injected page body") || len(c.PendingSources) != 1 || c.PendingSources[0].Name != source.Name || c.PendingSources[0].URI != source.URI {
		t.Fatalf("injected web source load = calls:%d messages:%+v pending:%+v", fetchCalls, c.Messages, c.PendingSources)
	}
}
