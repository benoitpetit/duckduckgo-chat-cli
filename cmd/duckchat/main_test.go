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
	"duckduckgo-chat-cli/internal/update"
)

func TestStartupHelpLineShowsOnlyCommonCommands(t *testing.T) {
	if got, want := startupHelpLine(), "/help · /model · /search · /image · /exit"; got != want {
		t.Fatalf("startup help line = %q, want %q", got, want)
	}
}

func TestPromptExecutorDisplaysStartupUpdateOnlyAtSubmission(t *testing.T) {
	updates := make(chan startupUpdateResult, 1)
	updates <- startupUpdateResult{info: &update.UpdateInfo{
		CurrentVersion: "dev",
		LatestVersion:  "1.8.1",
		NeedsUpdate:    true,
	}}

	var order []string
	executePromptInputWithNotice("/help", updates, false,
		func(message string) { order = append(order, "notice: "+message) },
		func(input string) { order = append(order, "execute: "+input) },
	)

	if len(order) != 2 || !strings.HasPrefix(order[0], "notice: Update available: 1.8.1") || order[1] != "execute: /help" {
		t.Fatalf("prompt submission order = %v, want update notice before executing /help", order)
	}
}

func TestPromptExecutorDoesNotWaitForStartupUpdate(t *testing.T) {
	updates := make(chan startupUpdateResult)
	executed := false
	executePromptInputWithNotice("/help", updates, false,
		func(string) { t.Fatal("should not display a notice before the check completes") },
		func(string) { executed = true },
	)
	if !executed {
		t.Fatal("prompt command did not execute while update check was pending")
	}
}

func TestStartupUpdateNoticeKeepsNormalOutputBrief(t *testing.T) {
	result := startupUpdateResult{info: &update.UpdateInfo{
		CurrentVersion: "dev",
		LatestVersion:  "1.8.1",
		NeedsUpdate:    true,
	}}
	if got, want := startupUpdateNotice(result, false), "Update available: 1.8.1. Run /update to update."; got != want {
		t.Fatalf("normal startup notice = %q, want %q", got, want)
	}
	if got := startupUpdateNotice(result, true); !strings.Contains(got, "current: dev") || !strings.Contains(got, "'/update'") {
		t.Fatalf("debug startup notice = %q, want version details and update command", got)
	}
}

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
