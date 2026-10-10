package chat

import (
	"fmt"
	"strings"
	"sync"

	"duckduckgo-chat-cli/internal/ui"
)

// ProgressUpdate is a concise, normalized status suitable for the CLI row.
type ProgressUpdate struct {
	Stage ui.ProgressStage
	Label string
}

type requestDebugTrace struct {
	mu      sync.Mutex
	entries []string
	sources map[string]struct{}
}

func (t *requestDebugTrace) RecordProgress(update ProgressUpdate) {
	if update.Label == "" {
		return
	}
	entry := fmt.Sprintf("Request progress: stage=%s label=%s", update.Stage, update.Label)
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.entries) > 0 && t.entries[len(t.entries)-1] == entry {
		return
	}
	t.entries = append(t.entries, entry)
}

func (t *requestDebugTrace) RecordEvent(event StreamEvent) {
	details := streamEventDebugDetails(event)
	if len(details) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if event.Type == "source" && event.SourceURL != "" {
		if t.sources == nil {
			t.sources = make(map[string]struct{})
		}
		if _, exists := t.sources[event.SourceURL]; exists {
			return
		}
		t.sources[event.SourceURL] = struct{}{}
	}
	t.entries = append(t.entries, details...)
}

func (t *requestDebugTrace) Snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.entries...)
}

func streamEventDebugDetails(event StreamEvent) []string {
	switch event.Type {
	case "status":
		return []string{"Duck.ai status: " + event.Status}
	case "tool":
		return []string{fmt.Sprintf("Duck.ai tool: %s (%s)", event.ToolName, event.State)}
	case "source":
		title := event.SourceTitle
		if title == "" {
			title = event.SourceURL
		}
		return []string{fmt.Sprintf("Duck.ai source: %s (%s)", title, event.SourceURL)}
	case "image":
		if event.ToolName != "" || event.State != "" {
			return []string{fmt.Sprintf("Duck.ai image tool: %s (%s)", event.ToolName, event.State)}
		}
		return []string{"Duck.ai image event received"}
	case "image_saved":
		return []string{"Generated image saved: " + event.ImageURL}
	case "image_save_failed":
		return []string{"Generated image could not be saved: " + event.ToolResult}
	default:
		return nil
	}
}

func progressUpdateForEvent(event StreamEvent) (ProgressUpdate, bool) {
	if event.Type == "message" {
		return ProgressUpdate{Stage: ui.ProgressResponding, Label: "Writing response"}, event.Message != ""
	}
	if event.Type == "image" {
		return ProgressUpdate{Stage: ui.ProgressImage, Label: "Generating image"}, true
	}
	if event.Type == "status" {
		label := strings.TrimSpace(event.Status)
		if label == "" {
			label = "Preparing response"
		}
		value := strings.ToLower(label)
		switch {
		case strings.Contains(value, "search"):
			return ProgressUpdate{Stage: ui.ProgressSearching, Label: "Searching the web"}, true
		case strings.Contains(value, "generat") && strings.Contains(value, "image"):
			return ProgressUpdate{Stage: ui.ProgressImage, Label: "Generating image"}, true
		case strings.Contains(value, "think"), strings.Contains(value, "reason"), strings.Contains(value, "prepar"):
			return ProgressUpdate{Stage: ui.ProgressPreparing, Label: label}, true
		case strings.Contains(value, "connect"):
			return ProgressUpdate{Stage: ui.ProgressConnecting, Label: label}, true
		case strings.Contains(value, "write"), strings.Contains(value, "respond"), strings.Contains(value, "generat"):
			return ProgressUpdate{Stage: ui.ProgressResponding, Label: "Writing response"}, true
		default:
			return ProgressUpdate{Stage: ui.ProgressPreparing, Label: label}, true
		}
	}
	if event.Type != "tool" {
		return ProgressUpdate{}, false
	}

	name := strings.ToLower(event.ToolName)
	state := strings.ToLower(event.State)
	if strings.Contains(name, "image") {
		return ProgressUpdate{Stage: ui.ProgressImage, Label: "Generating image"}, true
	}
	if strings.Contains(name, "search") || strings.Contains(name, "web") {
		if toolStateComplete(state) {
			return ProgressUpdate{Stage: ui.ProgressResponding, Label: "Writing response"}, true
		}
		return ProgressUpdate{Stage: ui.ProgressSearching, Label: "Searching the web"}, true
	}
	if event.ToolName != "" {
		label := "Using " + event.ToolName
		if toolStateComplete(state) {
			label = event.ToolName + " completed"
		}
		return ProgressUpdate{Stage: ui.ProgressPreparing, Label: label}, true
	}
	return ProgressUpdate{Stage: ui.ProgressPreparing, Label: "Preparing response"}, true
}

func toolStateComplete(state string) bool {
	switch state {
	case "result", "complete", "completed":
		return true
	default:
		return false
	}
}

func sourceProgressUpdate(count int) ProgressUpdate {
	return ProgressUpdate{Stage: ui.ProgressSources, Label: sourceProgressLabel(count)}
}

type sourceProgressTracker struct {
	seen  map[string]struct{}
	count int
}

func (t *sourceProgressTracker) Add(sourceURL string) (count int, added bool) {
	if sourceURL == "" {
		return t.count, false
	}
	if t.seen == nil {
		t.seen = make(map[string]struct{})
	}
	if _, exists := t.seen[sourceURL]; exists {
		return t.count, false
	}
	t.seen[sourceURL] = struct{}{}
	t.count++
	return t.count, true
}

type responseProgressTracker struct {
	started bool
}

func (t *responseProgressTracker) Add(event StreamEvent) (ProgressUpdate, bool) {
	if t.started || event.Type != "message" || event.Message == "" {
		return ProgressUpdate{}, false
	}
	t.started = true
	return ProgressUpdate{Stage: ui.ProgressResponding, Label: "Writing response"}, true
}
