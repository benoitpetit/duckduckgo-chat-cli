package chat

import (
	"fmt"
	"strings"
	"unicode"

	"duckduckgo-chat-cli/internal/persistence"

	"github.com/charmbracelet/x/ansi"
)

const loadSessionPreviewLimit = 43

func buildLoadSessionOptions(summaries []persistence.SessionSummary) ([]string, []string) {
	options := make([]string, 0, len(summaries))
	sessionIDs := make([]string, 0, len(summaries))
	for index, summary := range summaries {
		model := normalizeLoadSessionText(summary.Model, 18)
		if model == "" {
			model = "unknown model"
		}
		preview := normalizeLoadSessionText(summary.FirstMessage, loadSessionPreviewLimit)
		if summary.Compacted {
			preview = "[compact] " + preview
		}
		if preview == "" {
			preview = "No message preview"
		}
		options = append(options, fmt.Sprintf("%d · %s · %s · %d msgs · %s",
			index+1, summary.StartTime.Format("2006-01-02 15:04"), model, summary.MessageCount, preview))
		sessionIDs = append(sessionIDs, summary.ID)
	}
	return options, sessionIDs
}

func normalizeLoadSessionText(value string, maxRunes int) string {
	value = ansi.Strip(value)
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes-1]) + "…"
	}
	return value
}
