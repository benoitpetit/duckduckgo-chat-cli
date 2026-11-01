package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/chatcontext"
)

func TestConversationExportIncludesCompactSourceReferences(t *testing.T) {
	chat := &Chat{Messages: []Message{
		{Role: "user", Content: "[File Context]\nprivate body", Sources: []chatcontext.SourceRef{{ID: "source-1", Kind: chatcontext.SourceKindFile, Name: "notes.md", URI: "/home/alice/private/notes.md", SizeBytes: 42, State: chatcontext.SourceStateCompressed}}},
		{Role: "user", Content: "Summarize this", Sources: []chatcontext.SourceRef{{ID: "source-1", Kind: chatcontext.SourceKindFile, Name: "notes.md", URI: "/home/alice/private/notes.md", SizeBytes: 42, State: chatcontext.SourceStateCompressed}}},
		{Role: "assistant", Content: "Summary."},
	}}
	_, exported := chat.Export("conversation", "")
	for _, want := range []string{"source-1", "notes.md", "/home/alice/private/notes.md", "42 bytes", "compressed"} {
		if !strings.Contains(exported, want) {
			t.Errorf("conversation export lacks %q: %s", want, exported)
		}
	}
	if strings.Count(exported, "private body") != 1 {
		t.Fatalf("conversation export duplicated source body: %s", exported)
	}
}

func TestWriteExportUsesPrivateDirectoryAndFile(t *testing.T) {
	root := t.TempDir()
	exportDir := filepath.Join(root, "new", "exports")
	path, err := writeExportFile(exportDir, "conversation.md", "private transcript")
	if err != nil {
		t.Fatal(err)
	}
	directoryInfo, err := os.Stat(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("new export directory mode = %#o, want 0700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("export file mode = %#o, want 0600", got)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "private transcript" {
		t.Fatalf("export contents = %q, %v", got, err)
	}
}

func TestWriteExportPreservesExistingDirectoryMode(t *testing.T) {
	exportDir := filepath.Join(t.TempDir(), "shared-exports")
	if err := os.Mkdir(exportDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := writeExportFile(exportDir, "conversation.md", "transcript"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(exportDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("existing export directory mode = %#o, want unchanged 0750", got)
	}
}

func TestConversationExportWithoutSourceMetadataKeepsExistingSections(t *testing.T) {
	chat := &Chat{Messages: []Message{{Role: "user", Content: "Plain question"}, {Role: "assistant", Content: "Plain answer"}}}
	_, exported := chat.Export("conversation", "")
	if !strings.Contains(exported, "Plain question") || !strings.Contains(exported, "Plain answer") {
		t.Fatalf("legacy conversation export lost messages: %s", exported)
	}
	if strings.Contains(exported, "Source references") || strings.Contains(exported, "source-") {
		t.Fatalf("conversation without metadata gained source references: %s", exported)
	}
}
