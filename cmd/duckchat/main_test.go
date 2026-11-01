package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/activity"
	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/doctor"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
	"duckduckgo-chat-cli/internal/tray"
	"duckduckgo-chat-cli/internal/ui"
	"duckduckgo-chat-cli/internal/update"
	"duckduckgo-chat-cli/internal/voice"

	"github.com/c-bata/go-prompt"
)

func TestTrackActivityWithErrorPublishesFailureWithoutErrorDetails(t *testing.T) {
	previous := dashboardActivity
	hub := activity.NewHub()
	dashboardActivity = hub
	t.Cleanup(func() { dashboardActivity = previous })
	operationErr := errors.New("private file path failed")

	err := trackActivityWithError("file", "File operation", func() error { return operationErr })
	if !errors.Is(err, operationErr) {
		t.Fatalf("trackActivityWithError() error = %v, want original error", err)
	}
	events := hub.EventsAfter(0)
	if len(events) != 2 || events[0].Status != "started" || events[1].Status != "failed" {
		t.Fatalf("activity events = %+v, want started then failed", events)
	}
	if strings.Contains(events[1].Summary, operationErr.Error()) {
		t.Fatalf("failure activity exposed the detailed error: %q", events[1].Summary)
	}
}

func TestVoiceContextUsesOnlyUserAndAssistantText(t *testing.T) {
	messages := []chat.Message{
		{Role: "system", Content: "private system instruction"},
		{Role: "user", Content: "What is the result?"},
		{Role: "assistant", Content: "The result is 42."},
	}
	got := voiceContextForChat(messages)
	want := []tray.ConversationMessage{
		{Role: "user", Content: "What is the result?"},
		{Role: "assistant", Content: "The result is 42."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("voice context = %+v, want %+v", got, want)
	}
}

func TestVoiceTranscriptIDsRoundTripThroughDependencyAdapter(t *testing.T) {
	want := tray.ConversationMessage{ID: "realtime-item-1", Role: "assistant", Content: "Voice reply"}
	var got tray.ConversationMessage
	dependencies := voiceDependenciesWithContext(config.DefaultSpeakConfig(), nil, func(message tray.ConversationMessage) {
		got = message
	})
	dependencies.OnTranscript(voice.ConversationMessage{ID: want.ID, Role: want.Role, Content: want.Content})
	if got != want {
		t.Fatalf("adapted transcript = %+v, want %+v", got, want)
	}
}

func TestVoiceTranscriptOmissionMarkerSurvivesDependencyAdapter(t *testing.T) {
	want := tray.ConversationMessage{ID: "omitted-voice-item", Role: "user", Omitted: true}
	var got tray.ConversationMessage
	dependencies := voiceDependenciesWithContext(config.DefaultSpeakConfig(), nil, func(message tray.ConversationMessage) {
		got = message
	})
	dependencies.OnTranscript(voice.ConversationMessage{ID: want.ID, Role: want.Role, Omitted: true})
	if got != want {
		t.Fatalf("adapted omission marker = %+v, want %+v", got, want)
	}
}

func TestVoiceStatusReturnsFinalizedMessagesAfterTheSessionEnds(t *testing.T) {
	voiceMessages := []tray.ConversationMessage{
		{Role: "user", Content: "Voice question"},
		{Role: "assistant", Content: "Voice answer"},
	}
	service := &trayServiceRuntime{voiceStarting: true, voiceOwnerID: "cli-owner", voiceMessages: voiceMessages}
	active := service.voiceStatus("cli-owner")
	if !active.VoiceActive || len(active.VoiceMessages) != 0 {
		t.Fatalf("active voice status = %+v, want active with no final transcript", active)
	}
	service.voiceStarting = false
	service.voiceResults = map[string]voiceTranscriptResult{
		"cli-owner": {messages: voiceMessages, truncated: true},
	}
	ended := service.voiceStatus("cli-owner")
	if ended.VoiceActive || !reflect.DeepEqual(ended.VoiceMessages, voiceMessages) || !ended.VoiceMessagesTruncated {
		t.Fatalf("ended voice status = %+v, want finalized messages %+v", ended, voiceMessages)
	}
	otherOwner := service.voiceStatus("another-cli")
	if len(otherOwner.VoiceMessages) != 0 {
		t.Fatalf("unrelated CLI received %d voice messages, want none", len(otherOwner.VoiceMessages))
	}
}

func TestVoiceTranscriptResultRemainsScopedWhenANewSessionStarts(t *testing.T) {
	oldMessages := []tray.ConversationMessage{{Role: "user", Content: "belongs to old CLI"}}
	service := &trayServiceRuntime{
		voiceStarting: true,
		voiceOwnerID:  "new-owner",
		voiceResults: map[string]voiceTranscriptResult{
			"old-owner": {messages: oldMessages},
		},
	}
	status := service.voiceStatus("old-owner")
	if status.VoiceActive || !reflect.DeepEqual(status.VoiceMessages, oldMessages) {
		t.Fatalf("old owner's status = %+v, want its ended transcript independent of the new active session", status)
	}
	service.acknowledgeVoiceTranscript("old-owner")
	if _, exists := service.voiceResults["old-owner"]; exists {
		t.Fatal("acknowledged transcript remains pending")
	}
}

func TestStoreVoiceTranscriptBoundsUnacknowledgedResults(t *testing.T) {
	service := &trayServiceRuntime{voiceResults: make(map[string]voiceTranscriptResult)}
	oldest := time.Now().Add(-time.Hour)
	for index := 0; index < 8; index++ {
		service.voiceResults[fmt.Sprintf("owner-%d", index)] = voiceTranscriptResult{finished: oldest.Add(time.Duration(index) * time.Minute)}
	}
	service.mu.Lock()
	service.storeVoiceTranscriptLocked("owner-new", voiceTranscriptResult{})
	service.mu.Unlock()
	if len(service.voiceResults) != 8 {
		t.Fatalf("pending transcript count = %d, want maximum of 8", len(service.voiceResults))
	}
	if _, exists := service.voiceResults["owner-0"]; exists {
		t.Fatal("oldest unacknowledged transcript was retained beyond the limit")
	}
	if _, exists := service.voiceResults["owner-new"]; !exists {
		t.Fatal("new transcript was dropped instead of the oldest pending result")
	}
}

func TestNewVoiceOwnerIDIsUniqueHex(t *testing.T) {
	first, err := newVoiceOwnerID()
	if err != nil {
		t.Fatalf("newVoiceOwnerID() error = %v", err)
	}
	second, err := newVoiceOwnerID()
	if err != nil {
		t.Fatalf("newVoiceOwnerID() second error = %v", err)
	}
	if len(first) != 32 || first == second {
		t.Fatalf("voice owner IDs = %q and %q; want unique 32-character IDs", first, second)
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("voice owner ID %q is not hexadecimal: %v", first, err)
	}
}

func TestMergeVoiceTranscriptPreservesCompletedTurnsBeyondSeedContextBounds(t *testing.T) {
	const messageCount = 25
	manager := persistence.NewHistoryManager(t.TempDir())
	session := &chat.Chat{SessionID: "voice-long-continuity", Model: models.Default(), HistoryManager: manager}
	voiceMessages := make([]tray.ConversationMessage, 0, messageCount)
	for index := 0; index < messageCount; index++ {
		content := fmt.Sprintf("voice turn %02d", index)
		if index == 0 {
			content = strings.Repeat("é", 4500) // More than the per-item outbound context limit in UTF-8 bytes.
		}
		voiceMessages = append(voiceMessages, tray.ConversationMessage{Role: "user", Content: content})
	}
	if err := mergeVoiceTranscript(session, voiceMessages); err != nil {
		t.Fatalf("mergeVoiceTranscript() error = %v", err)
	}
	if len(session.Messages) != messageCount || session.Messages[0].Content != voiceMessages[0].Content {
		t.Fatalf("merged %d messages with first content length %d; want all %d complete turns", len(session.Messages), len(session.Messages[0].Content), messageCount)
	}
}

func TestMergeVoiceTranscriptAppendsAndSavesMessagesInOrder(t *testing.T) {
	manager := persistence.NewHistoryManager(t.TempDir())
	session := &chat.Chat{
		SessionID:      "voice-continuity",
		Model:          models.Default(),
		HistoryManager: manager,
		Messages: []chat.Message{
			{Role: "user", Content: "Text question"},
			{Role: "assistant", Content: "Text answer"},
		},
	}
	voiceMessages := []tray.ConversationMessage{
		{Role: "user", Content: "Voice follow-up"},
		{Role: "assistant", Content: "Voice reply"},
		{Role: "system", Content: "must not enter the chat"},
	}
	if err := mergeVoiceTranscript(session, voiceMessages); err != nil {
		t.Fatalf("mergeVoiceTranscript() error = %v", err)
	}
	want := []chat.Message{
		{Role: "user", Content: "Text question"},
		{Role: "assistant", Content: "Text answer"},
		{Role: "user", Content: "Voice follow-up"},
		{Role: "assistant", Content: "Voice reply"},
	}
	if len(session.Messages) != len(want) {
		t.Fatalf("active chat messages = %+v, want %+v", session.Messages, want)
	}
	for index, message := range session.Messages {
		if message.Role != want[index].Role || message.Content != want[index].Content {
			t.Fatalf("active message %d = %+v, want %+v", index, message, want[index])
		}
	}
	saved, err := manager.LoadSession(session.SessionID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(saved.Messages) != len(want) || saved.Messages[2].Content != "Voice follow-up" || saved.Messages[3].Content != "Voice reply" {
		t.Fatalf("saved messages = %+v, want voice turns appended in order", saved.Messages)
	}
}

func TestPromptCompletionTextHasReadableContrastForEveryTheme(t *testing.T) {
	for _, theme := range ui.Themes() {
		t.Run(string(theme.ID), func(t *testing.T) {
			foreground := promptColorHex(theme, prompt.LightGray, false)
			background := promptColorHex(theme, prompt.DarkGray, true)
			if background != theme.Colors.Surface {
				t.Fatalf("completion background = %s, want theme surface %s", background, theme.Colors.Surface)
			}
			if ratio := contrastRatio(foreground, background); ratio < 4.5 {
				t.Fatalf("completion contrast = %.2f:1, want at least 4.5:1 (foreground %s, background %s)", ratio, foreground, background)
			}
			selectedText := promptColorHex(theme, prompt.Black, false)
			selectedBackground := promptColorHex(theme, prompt.Blue, true)
			if ratio := contrastRatio(selectedText, selectedBackground); ratio < 4.5 {
				t.Fatalf("selected completion contrast = %.2f:1, want at least 4.5:1 (foreground %s, background %s)", ratio, selectedText, selectedBackground)
			}
		})
	}
}

func TestCoordinatePromptPrefixUsesDistinctRed(t *testing.T) {
	theme, ok := ui.ResolveTheme("official")
	if !ok {
		t.Fatal("official theme is unavailable")
	}
	got := promptPrefixForeground(theme, prompt.Blue, prompt.DarkGray, true)
	if got != "#FF5555" {
		t.Fatalf("coordinate prefix foreground = %s, want distinct red #FF5555", got)
	}
	if got == promptPrefixForeground(theme, prompt.Blue, prompt.DarkGray, false) {
		t.Fatal("coordinate prefix color should differ from the normal prompt color")
	}
}

func TestCoordinatePromptPrefixAddsIndicatorOnlyWhilePending(t *testing.T) {
	got, live := coordinatePromptPrefix(true)
	if !live || got != "◆ You: " {
		t.Fatalf("coordinate prefix = (%q, %v), want active indicator", got, live)
	}
	got, live = coordinatePromptPrefix(false)
	if live || got != "" {
		t.Fatalf("normal prefix override = (%q, %v), want static You: prefix", got, live)
	}
}

func TestPromptScrollbarUsesThemeAccentAndSurface(t *testing.T) {
	thumb, track := promptScrollbarColors()
	if thumb != prompt.Blue || track != prompt.DarkGray {
		t.Fatalf("scrollbar colors = (%v, %v), want theme accent and surface", thumb, track)
	}
	for _, theme := range ui.Themes() {
		if got := promptColorHex(theme, thumb, true); got != theme.Colors.Accent {
			t.Errorf("%s scrollbar thumb = %s, want accent %s", theme.ID, got, theme.Colors.Accent)
		}
		if got := promptColorHex(theme, track, true); got != theme.Colors.Surface {
			t.Errorf("%s scrollbar track = %s, want surface %s", theme.ID, got, theme.Colors.Surface)
		}
	}
}

func TestParseCLIOptionsAcceptsRepeatedOneShotContext(t *testing.T) {
	options, err := parseCLIOptions([]string{
		"--prompt", "summarize these sources",
		"--file", "first.txt", "--file", "second.pdf",
		"--url", "https://example.com/a", "--url", "http://example.org/b",
		"--search", "first query", "--search", "second query",
	})
	if err != nil {
		t.Fatalf("parse context options: %v", err)
	}
	if !reflect.DeepEqual(options.files, []string{"first.txt", "second.pdf"}) {
		t.Fatalf("files = %v", options.files)
	}
	if !reflect.DeepEqual(options.urls, []string{"https://example.com/a", "http://example.org/b"}) {
		t.Fatalf("URLs = %v", options.urls)
	}
	if !reflect.DeepEqual(options.searches, []string{"first query", "second query"}) {
		t.Fatalf("searches = %v", options.searches)
	}
}

func TestParseCLIOptionsRejectsInvalidOneShotContext(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "file requires prompt", args: []string{"--file", "notes.txt"}, want: "--prompt"},
		{name: "URL requires prompt", args: []string{"--url", "https://example.com"}, want: "--prompt"},
		{name: "search requires prompt", args: []string{"--search", "query"}, want: "--prompt"},
		{name: "empty file", args: []string{"--prompt", "question", "--file", ""}, want: "--file"},
		{name: "empty search", args: []string{"--prompt", "question", "--search", "  "}, want: "--search"},
		{name: "invalid URL scheme", args: []string{"--prompt", "question", "--url", "ftp://example.com"}, want: "--url"},
		{name: "URL without host", args: []string{"--prompt", "question", "--url", "https:///page"}, want: "--url"},
		{name: "URL with credentials", args: []string{"--prompt", "question", "--url", "https://user:pass@example.com"}, want: "--url"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseCLIOptions(test.args)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("parse error = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestParseCLIOptionsContextCanReadPromptFromStdin(t *testing.T) {
	options, err := parseCLIOptions([]string{"--prompt", "-", "--file", "notes.txt"})
	if err != nil {
		t.Fatalf("parse stdin prompt with file context: %v", err)
	}
	if options.prompt != "-" || len(options.files) != 1 {
		t.Fatalf("parsed options = %#v", options)
	}
}

func contrastRatio(foreground, background string) float64 {
	firstLum := relativeLuminance(foreground)
	secondLum := relativeLuminance(background)
	if firstLum < secondLum {
		firstLum, secondLum = secondLum, firstLum
	}
	return (firstLum + 0.05) / (secondLum + 0.05)
}

func relativeLuminance(value string) float64 {
	rgb := hexRGB(value)
	return 0.2126*linearize(rgb[0]) + 0.7152*linearize(rgb[1]) + 0.0722*linearize(rgb[2])
}

func hexRGB(value string) [3]float64 {
	value = strings.TrimPrefix(value, "#")
	rgb, _ := strconv.ParseUint(value, 16, 24)
	return [3]float64{float64((rgb >> 16) & 0xff), float64((rgb >> 8) & 0xff), float64(rgb & 0xff)}
}

func linearize(channel float64) float64 {
	channel /= 255
	if channel <= 0.04045 {
		return channel / 12.92
	}
	return math.Pow((channel+0.055)/1.055, 2.4)
}

func TestStartupHelpLineShowsOnlyCommonCommands(t *testing.T) {
	if got, want := startupHelpLine(), "/help · /model · /search · /image · /issue · /exit"; got != want {
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
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pngData.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	chatSession := &chat.Chat{}
	var gotPrompt string
	handleCommandChainWithProcessor(chatSession, &config.Config{}, &command.ChainedCommand{
		Commands: []*command.Command{{Type: "/file", Raw: "/file " + path}},
		Prompt:   "describe logo",
	}, func(session *chat.Chat, prompt string, _ *config.Config) {
		gotPrompt = prompt
		if len(session.PendingSources) != 1 || session.PendingSources[0].Kind != chatcontext.SourceKindImage {
			t.Errorf("pending image sources = %+v, want image provenance", session.PendingSources)
		}
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

func doctorTestDependencies(t *testing.T, accepted bool) doctor.Dependencies {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	exportDir := filepath.Join(root, "exports")
	if err := os.Mkdir(exportDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return doctor.Dependencies{
		ReadConfig: func() (config.Config, string, error) {
			return config.Config{TOSAccepted: accepted, ExportDir: exportDir}, configPath, nil
		},
		BrowserPath: func() (string, error) { return "/usr/bin/chromium", nil },
		BrowserVersion: func(context.Context, string) (string, error) {
			return "Chromium 130.0.0.0", nil
		},
		NetworkProbe: func(context.Context) error { return nil },
	}
}

func TestDoctorLocalInvocationSkipsNetwork(t *testing.T) {
	deps := doctorTestDependencies(t, true)
	called := false
	deps.NetworkProbe = func(context.Context) error {
		called = true
		return nil
	}
	var stdout, stderr bytes.Buffer

	code := runDoctorWithDependencies(nil, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("runDoctorWithDependencies() = %d, want 0; stderr: %s", code, stderr.String())
	}
	if called {
		t.Fatal("local invocation called the network probe")
	}
	if !strings.Contains(stdout.String(), "network [skipped]") {
		t.Fatalf("human output omitted skipped network check: %s", stdout.String())
	}
}

func TestDoctorNetworkFlagRunsProbe(t *testing.T) {
	deps := doctorTestDependencies(t, true)
	called := false
	deps.NetworkProbe = func(ctx context.Context) error {
		called = true
		if _, ok := ctx.Deadline(); !ok {
			t.Error("network probe has no deadline")
		}
		return nil
	}
	var stdout, stderr bytes.Buffer

	code := runDoctorWithDependencies([]string{"--network"}, &stdout, &stderr, deps)
	if code != 0 || !called {
		t.Fatalf("runDoctorWithDependencies() = %d, called %v; want 0 and a probe; stderr: %s", code, called, stderr.String())
	}
}

func TestDoctorJSONHasStableChecks(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDoctorWithDependencies([]string{"--json"}, &stdout, &stderr, doctorTestDependencies(t, true))
	if code != 0 {
		t.Fatalf("runDoctorWithDependencies() = %d; stderr: %s", code, stderr.String())
	}
	var result doctor.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("JSON output is invalid: %v: %s", err, stdout.String())
	}
	ids := make([]string, len(result.Checks))
	for i, check := range result.Checks {
		ids[i] = check.ID
	}
	if got, want := strings.Join(ids, ","), "platform,config_file,terms,browser,browser_version,export_directory,network"; got != want {
		t.Fatalf("JSON check order = %q, want %q", got, want)
	}
}

func TestDoctorRejectsUnknownFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDoctorWithDependencies([]string{"--unknown"}, &stdout, &stderr, doctorTestDependencies(t, true))
	if code != 2 {
		t.Fatalf("runDoctorWithDependencies() = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown") {
		t.Fatalf("unknown flag error missing from stderr: %s", stderr.String())
	}
}

func TestDoctorReturnsNonZeroForRequiredFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDoctorWithDependencies(nil, &stdout, &stderr, doctorTestDependencies(t, false))
	if code != 1 {
		t.Fatalf("runDoctorWithDependencies() = %d, want 1", code)
	}
}

func TestDoctorOutputRedactsSensitiveProbeErrors(t *testing.T) {
	for _, args := range [][]string{{"--network"}, {"--network", "--json"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			deps := doctorTestDependencies(t, true)
			deps.NetworkProbe = func(context.Context) error {
				return errors.New("captured cookie=private-cookie VQD=private-vqd")
			}
			var stdout, stderr bytes.Buffer
			runDoctorWithDependencies(args, &stdout, &stderr, deps)
			for _, secret := range []string{"private-cookie", "private-vqd"} {
				if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
					t.Fatalf("diagnostic output leaked %q", secret)
				}
			}
		})
	}
}

func TestDoctorIsListedInCLIHelp(t *testing.T) {
	var output bytes.Buffer
	printCLIUsage(&output)
	for _, text := range []string{"duckchat doctor", "--network", "--json"} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("CLI help omitted %q", text)
		}
	}
}

func TestCommandChainPassesSourceMetadataToSubmittedPrompt(t *testing.T) {
	previousActivity := dashboardActivity
	dashboardActivity = nil
	t.Cleanup(func() { dashboardActivity = previousActivity })

	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("project notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	chatSession := &chat.Chat{}
	var gotSources int
	handleCommandChainWithRoleProcessor(chatSession, &config.Config{}, &command.ChainedCommand{
		Commands: []*command.Command{{Type: "/file", Raw: "/file " + path}},
		Prompt:   "summarize notes",
	}, func(session *chat.Chat, _, _ string, _ *config.Config) {
		gotSources = len(session.PendingSources)
		if gotSources == 1 {
			source := session.PendingSources[0]
			if source.Name != "notes.txt" || source.URI != path {
				t.Errorf("pending source = %+v, want file name and source URI", source)
			}
		}
	})
	if gotSources != 1 {
		t.Fatalf("pending source count = %d, want one source for the chained file", gotSources)
	}
}

func TestTraySettingsChangedIncludesSpeakPreferences(t *testing.T) {
	base := config.Config{
		Tray:  config.TrayConfig{TextShortcut: "Ctrl+Alt+D", VoiceShortcut: "Ctrl+Alt+V"},
		Speak: config.SpeakConfig{Voice: "voice-1", Volume: 100, AutoStart: true},
	}
	if traySettingsChanged(base.Tray, base.Speak, base) {
		t.Fatal("unchanged Tray and Speak settings should not require a tray reload")
	}

	voiceChanged := base
	voiceChanged.Speak.Voice = "voice-4"
	if !traySettingsChanged(base.Tray, base.Speak, voiceChanged) {
		t.Fatal("a Speak voice change must reload the tray service")
	}

	speakPlaybackChanged := base
	speakPlaybackChanged.Speak.AutoStart = false
	if !traySettingsChanged(base.Tray, base.Speak, speakPlaybackChanged) {
		t.Fatal("a Speak playback change must reload the tray service")
	}

	shortcutChanged := base
	shortcutChanged.Tray.VoiceShortcut = "Ctrl+Alt+B"
	if !traySettingsChanged(base.Tray, base.Speak, shortcutChanged) {
		t.Fatal("a tray shortcut change must reload the tray service")
	}
}

func TestCommandChainKeepsSourceWhenPromptIsDeferred(t *testing.T) {
	previousActivity := dashboardActivity
	dashboardActivity = nil
	t.Cleanup(func() { dashboardActivity = previousActivity })

	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("project notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	chatSession := &chat.Chat{}
	handleCommandChainWithRoleProcessor(chatSession, &config.Config{}, &command.ChainedCommand{
		Commands: []*command.Command{{Type: "/file", Raw: "/file " + path}},
	}, func(*chat.Chat, string, string, *config.Config) {
		t.Fatal("deferred prompt should not run")
	})
	if len(chatSession.PendingSources) != 1 || chatSession.PendingSources[0].Name != "notes.txt" {
		t.Fatalf("deferred chain sources = %+v, want source retained until next turn", chatSession.PendingSources)
	}
	if len(chatSession.Messages) != 1 || len(chatSession.Messages[0].Sources) != 1 || chatSession.Messages[0].Sources[0] != chatSession.PendingSources[0] {
		t.Fatalf("deferred context message sources = %+v, want same source reference as the pending turn", chatSession.Messages)
	}
}
