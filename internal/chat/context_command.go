package chat

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/ui"
)

// HandleContextCommand reports source counts and size estimates without showing
// the source bodies or their internal URIs.
func HandleContextCommand(c *Chat) {
	if c == nil {
		ui.PrintStyledPanel("Context", []ui.PanelLine{{{Text: "Chat session is unavailable.", Style: ui.PanelMuted}}})
		return
	}
	sources := sourcesForLatestExchange(c)
	lines := make([]ui.PanelLine, 0, len(sources)+8)
	textBytes, imageBytes, imageCount := 0, 0, 0
	type contributor struct {
		label string
		bytes int
	}
	contributors := make([]contributor, 0, len(c.Messages))
	for index, message := range c.Messages {
		messageBytes := len(message.Content)
		textBytes += messageBytes
		for _, image := range message.Images {
			imageBytes += len(image.Data)
			imageCount++
		}
		if messageBytes > 0 {
			label := fmt.Sprintf("Message %d · %s", index+1, message.Role)
			if message.Pinned {
				label += " · pinned"
			}
			contributors = append(contributors, contributor{label: label, bytes: messageBytes})
		}
	}
	for _, image := range c.pendingImages {
		imageBytes += len(image.Data)
		imageCount++
	}
	lines = append(lines,
		ui.PanelLine{{Text: fmt.Sprintf("Conversation text: %s measured across %d messages.", approximateSizeLabel(textBytes), len(c.Messages)), Style: ui.PanelForeground}},
		ui.PanelLine{{Text: fmt.Sprintf("Images: %d attachment(s), %s measured; image model-token cost is unknown.", imageCount, formatByteCount(imageBytes)), Style: ui.PanelMuted}},
	)
	if len(sources) == 0 {
		lines = append(lines, ui.PanelLine{{Text: "No source metadata is available in the current context.", Style: ui.PanelMuted}})
	} else {

		counts := make(map[chatcontext.SourceKind]int)
		sizes := make(map[chatcontext.SourceKind]int)
		totalBytes := 0
		for _, source := range sources {
			kind := source.Kind
			if kind == "" {
				kind = "unknown"
			}
			counts[kind]++
			sizes[kind] += max(source.SizeBytes, 0)
			totalBytes += max(source.SizeBytes, 0)
		}
		lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%d sources; total reported source sizes: %s (separate from conversation text).", len(sources), approximateSizeLabel(totalBytes)), Style: ui.PanelForeground}})
		for _, kind := range []chatcontext.SourceKind{chatcontext.SourceKindFile, chatcontext.SourceKindImage, chatcontext.SourceKindURL, chatcontext.SourceKindSearch, chatcontext.SourceKindLibrary, "unknown"} {
			if counts[kind] == 0 {
				continue
			}
			lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%s: %d source(s) · %s", kind, counts[kind], approximateSizeLabel(sizes[kind])), Style: ui.PanelInfo}})
		}
		lines = append(lines, ui.PanelLine{{Text: "Sources:", Style: ui.PanelAccent}})
		for _, source := range sources {
			state := source.State
			if state == "" {
				state = chatcontext.SourceStateActive
			}
			kind := source.Kind
			if kind == "" {
				kind = "unknown"
			}
			style := ui.PanelInfo
			if state != chatcontext.SourceStateActive {
				style = ui.PanelWarning
			}
			pinLabel := ""
			if source.Pinned {
				pinLabel = " · pinned"
			}
			lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%s · %s · %s · %s · %s%s%s", source.ID, kind, displaySourceName(source), approximateSizeLabel(source.SizeBytes), state, pinLabel, sourceExchangeLabel(source)), Style: style}})
		}
	}
	for _, source := range sources {
		if source.SizeBytes > 0 {
			contributors = append(contributors, contributor{label: "Source · " + source.ID + " · " + displaySourceName(source), bytes: source.SizeBytes})
		}
	}
	sort.SliceStable(contributors, func(i, j int) bool { return contributors[i].bytes > contributors[j].bytes })
	if len(contributors) > 0 {
		lines = append(lines, ui.PanelLine{{Text: "Largest measured contributors:", Style: ui.PanelAccent}})
		lines = append(lines, ui.PanelLine{{Text: "Message and source entries can overlap; these values are not a combined context-token count.", Style: ui.PanelMuted}})
		for _, item := range contributors[:min(5, len(contributors))] {
			lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%s · %s", item.label, formatByteCount(item.bytes)), Style: ui.PanelMuted}})
		}
	}
	if c.ContextOptimizer != nil && c.ContextOptimizer.MaxContextSize > 0 {
		lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("Automatic text budget: %s; optimizer estimates use bytes, not an exact model token count.", formatByteCount(c.ContextOptimizer.MaxContextSize)), Style: ui.PanelMuted}})
		pinnedBytes := pinnedContextBytes(c)
		if pinnedBytes > c.ContextOptimizer.MaxContextSize {
			lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("Pinned text totals %s and exceeds this budget; pinned items will be kept.", formatByteCount(pinnedBytes)), Style: ui.PanelWarning}})
		}
	}
	if c.Compacted {
		lines = append(lines, ui.PanelLine{{Text: "This session uses a compact summary. Full transcript parent: " + c.ParentSessionID, Style: ui.PanelInfo}})
	}
	ui.PrintStyledPanel("Context", lines)
}

// HandleSourcesCommand lists source references present in the retained request
// context for the most recent exchange.
func HandleSourcesCommand(c *Chat) {
	sources := sourcesForLatestExchange(c)
	lines := make([]ui.PanelLine, 0, len(sources))
	if len(sources) == 0 {
		lines = append(lines, ui.PanelLine{{Text: "No source metadata is available for the latest exchange.", Style: ui.PanelMuted}})
	} else {
		lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%d unique sources in the retained request context:", len(sources)), Style: ui.PanelForeground}})
		for _, source := range sources {
			state := source.State
			if state == "" {
				state = chatcontext.SourceStateActive
			}
			kind := source.Kind
			if kind == "" {
				kind = "unknown"
			}
			lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%s · %s · %s · %s%s", source.ID, kind, displaySourceName(source), state, sourceExchangeLabel(source)), Style: ui.PanelInfo}})
			lines = append(lines, ui.PanelLine{{Text: "  " + approximateSizeLabel(source.SizeBytes), Style: ui.PanelMuted}})
		}
	}
	ui.PrintStyledPanel("Sources", lines)
}

func sourcesForLatestExchange(c *Chat) []chatcontext.SourceRef {
	if c == nil {
		return nil
	}
	ordered := make([]chatcontext.SourceRef, 0)
	indices := make(map[string]int)
	add := func(source chatcontext.SourceRef) {
		if source.ID == "" {
			return
		}
		if source.State == "" {
			source.State = chatcontext.SourceStateActive
		}
		if index, ok := indices[source.ID]; ok {
			current := ordered[index]
			if current.Name == "" && source.Name != "" {
				current.Name = source.Name
			}
			if current.Kind == "" {
				current.Kind = source.Kind
			}
			if current.SizeBytes == 0 {
				current.SizeBytes = source.SizeBytes
			}
			if current.ExchangeID == "" {
				current.ExchangeID = source.ExchangeID
			}
			if sourceStatePriority(source.State) > sourceStatePriority(current.State) {
				current.State = source.State
			}
			ordered[index] = current
			return
		}
		indices[source.ID] = len(ordered)
		ordered = append(ordered, source)
	}

	for _, message := range c.Messages {
		for _, source := range message.Sources {
			add(source)
		}
	}
	for _, source := range c.PendingSources {
		add(source)
	}
	for _, change := range c.LastOptimizationChanges {
		index, ok := indices[change.SourceID]
		if !ok {
			add(chatcontext.SourceRef{
				ID: change.SourceID, Name: "unknown source", SizeBytes: change.BytesBefore, State: change.State,
			})
			continue
		}
		ordered[index].State = change.State
	}
	return ordered
}

func contextEstimateTokens(byteCount int) int {
	if byteCount <= 0 {
		return 0
	}
	tokens := byteCount / 4
	if byteCount%4 != 0 {
		tokens++
	}
	return tokens
}

func approximateSizeLabel(byteCount int) string {
	byteCount = max(byteCount, 0)
	return fmt.Sprintf("~%d bytes (~%d tokens; approximate)", byteCount, contextEstimateTokens(byteCount))
}

func displaySourceName(source chatcontext.SourceRef) string {
	name := strings.TrimSpace(source.Name)
	if name == "" {
		return "unknown source"
	}
	switch source.Kind {
	case chatcontext.SourceKindFile, chatcontext.SourceKindImage, chatcontext.SourceKindLibrary:
		name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	}
	if name == "." || name == "/" {
		return "unknown source"
	}
	return name
}

func sourceExchangeLabel(source chatcontext.SourceRef) string {
	if source.ExchangeID == "" {
		return ""
	}
	shortID := source.ExchangeID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return " · exchange " + shortID
}

func sourceStatePriority(state chatcontext.SourceState) int {
	switch state {
	case chatcontext.SourceStateOmitted:
		return 3
	case chatcontext.SourceStateTruncated:
		return 2
	case chatcontext.SourceStateCompressed:
		return 1
	default:
		return 0
	}
}

func sourcePromptLabels(sources []chatcontext.SourceRef) string {
	lines := []string{}
	for _, source := range sources {
		if source.ID == "" || source.State == chatcontext.SourceStateOmitted {
			continue
		}
		kind := source.Kind
		if kind == "" {
			kind = "unknown"
		}
		state := source.State
		if state == "" {
			state = chatcontext.SourceStateActive
		}
		lines = append(lines, fmt.Sprintf("[%d] %s · %s · %s · %s", len(lines)+1, source.ID, kind, displaySourceName(source), state))
	}
	if len(lines) == 0 {
		return ""
	}
	return "[Source references]\nLabels identify supplied context; they are not verified citations.\n" + strings.Join(lines, "\n")
}
