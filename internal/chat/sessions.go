package chat

import (
	"fmt"
	"os"
	"strings"

	"duckduckgo-chat-cli/internal/persistence"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
)

// HandleSessionsCommand opens the saved-session manager, optionally filtered
// by title, tag, model, ID, or first-message text.
func HandleSessionsCommand(c *Chat, args string) error {
	if c == nil || c.HistoryManager == nil {
		return fmt.Errorf("conversation history manager is unavailable")
	}
	query := strings.TrimSpace(args)
	sessions, err := c.HistoryManager.ListSessionSummaries()
	if err != nil {
		return err
	}
	for {
		filtered := filterSessionSummaries(sessions, query)
		if len(filtered) == 0 {
			if query == "" {
				ui.Warningln("No saved sessions found.")
			} else {
				ui.Warningln("No saved sessions match %q.", query)
			}
			return nil
		}

		options, sessionIDs := buildSessionManagerOptions(filtered)
		var selectedOption string
		prompt := &survey.Select{Message: "Choose a saved session", Options: options, PageSize: 10}
		err := ui.AskFramedSelect(prompt, &selectedOption, "Saved sessions", sessionManagerSummary(filtered, query),
			survey.WithStdio(os.Stdin, os.Stdout, os.Stderr), ui.ThemedSurveyIcons())
		if err != nil {
			ui.Warningln("Session manager canceled.")
			return nil
		}
		index := indexOfSessionOption(options, selectedOption)
		if index < 0 || index >= len(sessionIDs) {
			return fmt.Errorf("could not identify the selected session")
		}
		selected := findSessionSummary(filtered, sessionIDs[index])
		if selected == nil {
			return fmt.Errorf("selected session is no longer available")
		}

		action, err := selectSessionManagerAction(*selected)
		if err != nil {
			ui.Warningln("Session action canceled.")
			return nil
		}
		switch action {
		case "Open":
			loadAndRestoreSession(c, selected.ID)
			return nil
		case "Branch":
			if err := branchSavedSession(c, selected.ID, ""); err != nil {
				return err
			}
			return nil
		case "Rename":
			var title string
			err = survey.AskOne(&survey.Input{Message: "Session title", Default: selected.Title}, &title,
				survey.WithStdio(os.Stdin, os.Stdout, os.Stderr), ui.ThemedSurveyIcons())
			if err != nil {
				ui.Warningln("Rename canceled.")
				return nil
			}
			err = renameManagedSession(c.HistoryManager, *selected, title)
		case "Edit tags":
			var tags string
			err = survey.AskOne(&survey.Input{Message: "Comma-separated tags (leave empty to clear)", Default: strings.Join(selected.Tags, ", ")}, &tags,
				survey.WithStdio(os.Stdin, os.Stdout, os.Stderr), ui.ThemedSurveyIcons())
			if err != nil {
				ui.Warningln("Tag edit canceled.")
				return nil
			}
			err = setManagedSessionTags(c.HistoryManager, *selected, tags)
		case "Toggle favorite":
			err = toggleManagedSessionFavorite(c.HistoryManager, *selected)
		case "Delete":
			var confirmed bool
			err = survey.AskOne(&survey.Confirm{Message: sessionDeleteConfirmation(*selected), Default: false}, &confirmed,
				survey.WithStdio(os.Stdin, os.Stdout, os.Stderr), ui.ThemedSurveyIcons())
			if err != nil {
				ui.Warningln("Delete canceled.")
				return nil
			}
			err = deleteManagedSession(c.HistoryManager, *selected, confirmed)
			if err == nil && confirmed {
				ui.AIln("Deleted saved session %s.", selected.ID)
			}
		case "Back":
			return nil
		default:
			return fmt.Errorf("unknown session action %q", action)
		}
		if err != nil {
			return err
		}
		if selected.ID == c.SessionID {
			if updated, updateErr := c.HistoryManager.LoadSession(selected.ID); updateErr == nil {
				c.SessionTitle = updated.Title
				c.SessionTags = append([]string(nil), updated.Tags...)
				c.SessionFavorite = updated.Favorite
			}
		}
		sessions, err = c.HistoryManager.ListSessionSummaries()
		if err != nil {
			return err
		}
	}
}

func filterSessionSummaries(sessions []persistence.SessionSummary, query string) []persistence.SessionSummary {
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]persistence.SessionSummary, 0, len(sessions))
	for _, session := range sessions {
		if query == "" || strings.Contains(strings.ToLower(session.ID), query) ||
			strings.Contains(strings.ToLower(session.Title), query) ||
			strings.Contains(strings.ToLower(session.Model), query) ||
			strings.Contains(strings.ToLower(session.FirstMessage), query) {
			filtered = append(filtered, session)
			continue
		}
		for _, tag := range session.Tags {
			if strings.Contains(strings.ToLower(tag), query) {
				filtered = append(filtered, session)
				break
			}
		}
	}
	return filtered
}

func buildSessionManagerOptions(summaries []persistence.SessionSummary) ([]string, []string) {
	options := make([]string, 0, len(summaries))
	ids := make([]string, 0, len(summaries))
	for index, summary := range summaries {
		title := normalizeLoadSessionText(summary.Title, 32)
		if title == "" {
			title = "Untitled conversation"
		}
		favorite := ""
		if summary.Favorite {
			favorite = "★ "
		}
		if summary.Compacted {
			favorite += "[compact] "
		}
		tags := ""
		if len(summary.Tags) > 0 {
			tags = " · #" + strings.Join(summary.Tags, " #")
		}
		preview := normalizeLoadSessionText(summary.FirstMessage, 28)
		if preview == "" {
			preview = "No message preview"
		}
		options = append(options, fmt.Sprintf("%d · %s%s · %s · %s · %d msgs · %s%s",
			index+1, favorite, title, summary.StartTime.Format("2006-01-02 15:04"),
			normalizeLoadSessionText(summary.Model, 16), summary.MessageCount, preview, tags))
		ids = append(ids, summary.ID)
	}
	return options, ids
}

func sessionManagerSummary(summaries []persistence.SessionSummary, query string) []ui.PanelLine {
	lines := []ui.PanelLine{{{Text: fmt.Sprintf("Matching sessions: %d", len(summaries)), Style: ui.PanelAccent}}}
	if query != "" {
		lines = append(lines, ui.PanelLine{{Text: "Filter: " + normalizeLoadSessionText(query, 72), Style: ui.PanelMuted}})
	}
	lines = append(lines, ui.PanelLine{{Text: "Filter by title, tag, model, ID, or opening message.", Style: ui.PanelMuted}})
	return lines
}

func indexOfSessionOption(options []string, selected string) int {
	for index, option := range options {
		if option == selected {
			return index
		}
	}
	return -1
}

func findSessionSummary(summaries []persistence.SessionSummary, id string) *persistence.SessionSummary {
	for index := range summaries {
		if summaries[index].ID == id {
			return &summaries[index]
		}
	}
	return nil
}

func selectSessionManagerAction(summary persistence.SessionSummary) (string, error) {
	var action string
	options := []string{"Open", "Branch", "Rename", "Edit tags", "Toggle favorite", "Delete", "Back"}
	prompt := &survey.Select{Message: "Choose an action", Options: options, PageSize: len(options)}
	lines := []ui.PanelLine{
		{{Text: normalizeLoadSessionText(summary.Title, 72), Style: ui.PanelAccent}},
		{{Text: fmt.Sprintf("%s · %d messages · %s", summary.ID, summary.MessageCount, summary.StartTime.Format("2006-01-02 15:04")), Style: ui.PanelMuted}},
	}
	if summary.ParentSessionID != "" {
		label := summary.ParentSessionID
		if summary.BranchName != "" {
			label += " · " + summary.BranchName
		}
		lines = append(lines, ui.PanelLine{{Text: "Branch of " + label, Style: ui.PanelMuted}})
	}
	err := ui.AskFramedSelect(prompt, &action, "Session actions", lines,
		survey.WithStdio(os.Stdin, os.Stdout, os.Stderr), ui.ThemedSurveyIcons())
	return action, err
}

func renameManagedSession(manager *persistence.HistoryManager, selected persistence.SessionSummary, title string) error {
	return manager.UpdateSessionMetadata(selected.ID, persistence.SessionMetadata{
		Title: title, Tags: selected.Tags, Favorite: selected.Favorite,
	})
}

func setManagedSessionTags(manager *persistence.HistoryManager, selected persistence.SessionSummary, value string) error {
	var tags []string
	for _, tag := range strings.Split(value, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return manager.UpdateSessionMetadata(selected.ID, persistence.SessionMetadata{
		Title: selected.Title, Tags: tags, Favorite: selected.Favorite,
	})
}

func toggleManagedSessionFavorite(manager *persistence.HistoryManager, selected persistence.SessionSummary) error {
	return manager.UpdateSessionMetadata(selected.ID, persistence.SessionMetadata{
		Title: selected.Title, Tags: selected.Tags, Favorite: !selected.Favorite,
	})
}

func sessionDeleteConfirmation(selected persistence.SessionSummary) string {
	title := normalizeLoadSessionText(selected.Title, 72)
	if title == "" {
		title = "Untitled conversation"
	}
	return fmt.Sprintf("Delete %q (%s)? This cannot be undone.", title, selected.ID)
}

func deleteManagedSession(manager *persistence.HistoryManager, selected persistence.SessionSummary, confirmed bool) error {
	if !confirmed {
		return nil
	}
	return manager.DeleteSession(selected.ID)
}
