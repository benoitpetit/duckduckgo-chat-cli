package chat

import (
	"testing"

	"duckduckgo-chat-cli/internal/ui"
)

func TestProgressUpdateForStreamEvent(t *testing.T) {
	tests := []struct {
		name  string
		event StreamEvent
		stage ui.ProgressStage
		label string
	}{
		{
			name:  "connecting status",
			event: StreamEvent{Type: "status", Status: "Connecting to Duck.ai"},
			stage: ui.ProgressConnecting,
			label: "Connecting to Duck.ai",
		},
		{
			name:  "preparing status",
			event: StreamEvent{Type: "status", Status: "Preparing response"},
			stage: ui.ProgressPreparing,
			label: "Preparing response",
		},
		{
			name:  "search starts",
			event: StreamEvent{Type: "tool", ToolName: "web_search", State: "call"},
			stage: ui.ProgressSearching,
			label: "Searching the web",
		},
		{
			name:  "search completes",
			event: StreamEvent{Type: "tool", ToolName: "web_search", State: "complete"},
			stage: ui.ProgressResponding,
			label: "Writing response",
		},
		{
			name:  "image tool",
			event: StreamEvent{Type: "tool", ToolName: "generate_image", State: "call"},
			stage: ui.ProgressImage,
			label: "Generating image",
		},
		{
			name:  "assistant message",
			event: StreamEvent{Type: "message", Message: "Here is the answer."},
			stage: ui.ProgressResponding,
			label: "Writing response",
		},
		{
			name:  "unknown tool",
			event: StreamEvent{Type: "tool", ToolName: "custom_tool", State: "call"},
			stage: ui.ProgressPreparing,
			label: "Using custom_tool",
		},
		{
			name:  "unknown completed tool",
			event: StreamEvent{Type: "tool", ToolName: "custom_tool", State: "result"},
			stage: ui.ProgressPreparing,
			label: "custom_tool completed",
		},
		{
			name:  "unknown status",
			event: StreamEvent{Type: "status", Status: "Waiting for a custom phase"},
			stage: ui.ProgressPreparing,
			label: "Waiting for a custom phase",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := progressUpdateForEvent(test.event)
			if !ok {
				t.Fatal("progressUpdateForEvent() returned false, want an update")
			}
			if got.Stage != test.stage || got.Label != test.label {
				t.Fatalf("progressUpdateForEvent() = %+v, want stage=%q label=%q", got, test.stage, test.label)
			}
		})
	}
}

func TestDebugEventDetailsIncludesImageToolNameAndState(t *testing.T) {
	details := streamEventDebugDetails(StreamEvent{Type: "image", ToolName: "GenerateImage", State: "result"})
	if len(details) != 1 || details[0] != "Duck.ai image tool: GenerateImage (result)" {
		t.Fatalf("image debug details = %v, want the image tool name and state", details)
	}
}

func TestRequestDebugTracePreservesProgressAndEventOrder(t *testing.T) {
	var trace requestDebugTrace
	trace.RecordProgress(ProgressUpdate{Stage: ui.ProgressConnecting, Label: "Connecting to Duck.ai"})
	trace.RecordEvent(StreamEvent{Type: "tool", ToolName: "web_search", State: "call"})
	trace.RecordProgress(ProgressUpdate{Stage: ui.ProgressSearching, Label: "Searching the web"})
	trace.RecordEvent(StreamEvent{Type: "source", SourceTitle: "Example", SourceURL: "https://example.com"})
	trace.RecordEvent(StreamEvent{Type: "source", SourceTitle: "Example", SourceURL: "https://example.com"})

	want := []string{
		"Request progress: stage=connecting label=Connecting to Duck.ai",
		"Duck.ai tool: web_search (call)",
		"Request progress: stage=searching label=Searching the web",
		"Duck.ai source: Example (https://example.com)",
	}
	got := trace.Snapshot()
	if len(got) != len(want) {
		t.Fatalf("debug trace entries = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("debug trace entry %d = %q, want %q (all entries: %v)", index, got[index], want[index], got)
		}
	}
}

func TestSourceProgressUpdateShowsDistinctCount(t *testing.T) {
	for _, test := range []struct {
		count int
		want  string
	}{
		{count: 1, want: "Found 1 web source"},
		{count: 3, want: "Found 3 web sources"},
	} {
		got := sourceProgressUpdate(test.count)
		if got.Stage != ui.ProgressSources || got.Label != test.want {
			t.Errorf("sourceProgressUpdate(%d) = %+v, want stage=%q label=%q", test.count, got, ui.ProgressSources, test.want)
		}
	}
}

func TestSourceProgressTrackerDeduplicatesURLs(t *testing.T) {
	var tracker sourceProgressTracker
	for _, test := range []struct {
		url       string
		wantCount int
		wantAdded bool
	}{
		{url: "https://example.com/a", wantCount: 1, wantAdded: true},
		{url: "https://example.com/a", wantCount: 1, wantAdded: false},
		{url: "https://example.com/b", wantCount: 2, wantAdded: true},
	} {
		gotCount, gotAdded := tracker.Add(test.url)
		if gotCount != test.wantCount || gotAdded != test.wantAdded {
			t.Errorf("Add(%q) = (%d, %t), want (%d, %t)", test.url, gotCount, gotAdded, test.wantCount, test.wantAdded)
		}
	}
}

func TestResponseProgressTrackerEmitsOnlyForFirstAssistantMessage(t *testing.T) {
	var tracker responseProgressTracker
	for index, event := range []StreamEvent{
		{Type: "status", Status: "Searching the web"},
		{Type: "message", Message: "First token."},
		{Type: "message", Message: " next token."},
	} {
		update, ok := tracker.Add(event)
		if index == 1 {
			if !ok || update.Stage != ui.ProgressResponding || update.Label != "Writing response" {
				t.Fatalf("first assistant message progress = (%+v, %t), want writing response", update, ok)
			}
		} else if ok {
			t.Fatalf("event %d unexpectedly emitted response progress: %+v", index, update)
		}
	}
}
