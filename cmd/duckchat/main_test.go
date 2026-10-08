package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
)

func TestShutdownFinalizesSessionBeforeExit(t *testing.T) {
	var calls []string
	finalize := newShutdownFinalizer(
		func() { calls = append(calls, "stop-snapshots") },
		func() error { calls = append(calls, "save-conversation"); return nil },
		func() error { calls = append(calls, "save-analytics"); return nil },
		func() error { calls = append(calls, "stop-api"); return nil },
		func() error { calls = append(calls, "stop-dashboard"); return nil },
		func() error { calls = append(calls, "stop-voice"); return nil },
		func() error { calls = append(calls, "shutdown-browser"); return nil },
		func() error { calls = append(calls, "restore-terminal"); return nil },
	)
	finalize()
	finalize()
	want := []string{"stop-snapshots", "stop-api", "stop-dashboard", "stop-voice", "save-conversation", "save-analytics", "shutdown-browser", "restore-terminal"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("shutdown order = %v, want %v", calls, want)
	}
}

func TestCommandChainAbortsPromptWhenImagePreparationFails(t *testing.T) {
	previousActivity := dashboardActivity
	dashboardActivity = nil
	t.Cleanup(func() { dashboardActivity = previousActivity })

	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "signature mismatch", data: []byte("not a png")},
		{name: "oversized", data: append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, (10<<20)+1)...)},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.png")
			if err := os.WriteFile(path, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			chatSession := &chat.Chat{}
			requestSent := false
			chain := &command.ChainedCommand{
				Commands: []*command.Command{{Type: "/file", Raw: "/file " + path + " -- describe image"}},
				Prompt:   "describe image",
			}
			handleCommandChainWithProcessor(chatSession, &config.Config{}, chain, func(*chat.Chat, string, *config.Config) {
				requestSent = true
			})
			if requestSent {
				t.Fatal("command chain sent the prompt after image preparation failed")
			}
			if len(chatSession.Messages) != 0 {
				t.Fatalf("failed chain changed chat messages: %+v", chatSession.Messages)
			}
		})
	}
}

func TestCommandChainAddsValidImageToProcessorContext(t *testing.T) {
	previousActivity := dashboardActivity
	dashboardActivity = nil
	t.Cleanup(func() { dashboardActivity = previousActivity })

	path := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\nimage"), 0600); err != nil {
		t.Fatal(err)
	}
	chatSession := &chat.Chat{}
	var gotPrompt string
	handleCommandChainWithProcessor(chatSession, &config.Config{}, &command.ChainedCommand{
		Commands: []*command.Command{{Type: "/file", Raw: "/file " + path}},
		Prompt:   "describe logo",
	}, func(session *chat.Chat, prompt string, _ *config.Config) {
		gotPrompt = prompt
	})
	if !strings.Contains(gotPrompt, "describe logo") {
		t.Fatalf("processor prompt = %q, want requested prompt", gotPrompt)
	}
}

func TestCommandChainPassesContextAndPromptAsSeparateAnalyticsRoles(t *testing.T) {
	previousActivity := dashboardActivity
	dashboardActivity = nil
	t.Cleanup(func() { dashboardActivity = previousActivity })

	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("project notes for the report"), 0600); err != nil {
		t.Fatal(err)
	}
	var gotContext, gotPrompt string
	handleCommandChainWithRoleProcessor(&chat.Chat{}, &config.Config{}, &command.ChainedCommand{
		Commands: []*command.Command{{Type: "/file", Raw: "/file " + path}},
		Prompt:   "summarize these notes",
	}, func(_ *chat.Chat, contextContent, prompt string, _ *config.Config) {
		gotContext, gotPrompt = contextContent, prompt
	})
	if !strings.Contains(gotContext, "project notes for the report") || gotPrompt != "summarize these notes" {
		t.Fatalf("processor context=%q prompt=%q, want separate file context and user prompt", gotContext, gotPrompt)
	}
}
