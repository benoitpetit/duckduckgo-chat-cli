package chat

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/persistence"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
)

func HandleBranchCommand(c *Chat, name string) {
	if c == nil || c.HistoryManager == nil {
		ui.Errorln("Conversation history manager is unavailable.")
		return
	}
	if len(c.Messages) == 0 && len(c.Notes) == 0 && len(c.PendingSources) == 0 && len(c.pendingImages) == 0 {
		ui.Warningln("There is no conversation to branch.")
		return
	}
	if c.SessionID == "" {
		c.SessionID = newSessionID()
	}
	if err := branchSavedSession(c, c.SessionID, name); err != nil {
		ui.Errorln("Could not create conversation branch: %v", err)
	}
}

func branchSavedSession(c *Chat, parentID, name string) error {
	if c == nil || c.HistoryManager == nil {
		return fmt.Errorf("conversation history manager is unavailable")
	}
	name = cleanBranchName(name)
	if name == "" {
		name = "branch"
	}
	if len(c.Messages) > 0 || len(c.Notes) > 0 || len(c.PendingSources) > 0 || len(c.pendingImages) > 0 {
		if c.SessionID == "" {
			c.SessionID = newSessionID()
		}
		if err := c.SaveCurrentSession(); err != nil {
			return fmt.Errorf("save active conversation: %w", err)
		}
	}
	parent, err := c.HistoryManager.LoadSession(parentID)
	if err != nil {
		return fmt.Errorf("load parent session %s: %w", parentID, err)
	}
	child := cloneConversationSession(parent)
	child.ID = newSessionID()
	child.ParentSessionID = parent.ID
	child.BranchName = name
	child.StartTime = time.Now()
	child.EndTime = time.Time{}
	title := name
	if name == "branch" && parent.Title != "" {
		title = "Branch: " + parent.Title
	}
	child.Title = limitSessionTitle(title)
	child.Version = persistence.SessionFormatVersion
	if err := c.HistoryManager.SaveSession(child); err != nil {
		return fmt.Errorf("save branch: %w", err)
	}
	c.RestoreContext(child)
	ui.AIln("Created branch %q as session %s, from %s.", name, child.ID, parent.ID)
	return nil
}

func cloneConversationSession(session *persistence.ConversationSession) *persistence.ConversationSession {
	cloned := *session
	cloneMessages := func(messages []intelligence.Message) []intelligence.Message {
		copyOfMessages := make([]intelligence.Message, len(messages))
		for index, message := range messages {
			copyOfMessages[index] = message
			copyOfMessages[index].Images = cloneImageAttachments(message.Images)
			copyOfMessages[index].Sources = cloneSourceRefs(message.Sources)
		}
		return copyOfMessages
	}
	cloned.Messages = cloneMessages(session.Messages)
	cloned.OptimizedMessages = cloneMessages(session.OptimizedMessages)
	cloned.PendingSources = cloneSourceRefs(session.PendingSources)
	cloned.PendingImages = cloneImageAttachments(session.PendingImages)
	cloned.CoordinateReports = cloneCoordinateReports(session.CoordinateReports)
	cloned.Notes = append([]string(nil), session.Notes...)
	cloned.Tags = append([]string(nil), session.Tags...)
	return &cloned
}

func cleanBranchName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
	if len([]rune(value)) > 48 {
		value = string([]rune(value)[:48])
	}
	return strings.TrimSpace(value)
}

func limitSessionTitle(value string) string {
	runes := []rune(value)
	if len(runes) > 100 {
		return string(runes[:99]) + "…"
	}
	return value
}

func newSessionID() string { return "session_" + randomRequestID() }

func HandleNotesCommand(c *Chat, raw string) {
	if c == nil {
		ui.Errorln("Chat session is unavailable.")
		return
	}
	args := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "/notes"))
	parts := strings.SplitN(args, " ", 2)
	action := "show"
	if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
		action = strings.ToLower(strings.TrimSpace(parts[0]))
	}
	value := ""
	if len(parts) == 2 {
		value = strings.TrimSpace(parts[1])
	}
	switch action {
	case "show":
		showSessionNotes(c)
	case "add":
		if value == "" {
			ui.Warningln("Usage: /notes add <text>")
			return
		}
		c.Notes = append(c.Notes, value)
		if err := saveNotes(c); err != nil {
			ui.Warningln("Note is active but could not be saved: %v", err)
			return
		}
		ui.AIln("Added a local note. Notes are not sent to the model unless you include them in a prompt.")
	case "clear":
		confirmed := false
		if err := survey.AskOne(&survey.Confirm{Message: "Clear all local notes for this session?", Default: false}, &confirmed); err != nil || !confirmed {
			ui.Warningln("Note clearing canceled.")
			return
		}
		c.Notes = nil
		if err := saveNotes(c); err != nil {
			ui.Warningln("Notes were cleared in memory but could not be saved: %v", err)
			return
		}
		ui.AIln("Cleared session notes.")
	default:
		ui.Warningln("Usage: /notes [show|add <text>|clear]")
	}
}

func showSessionNotes(c *Chat) {
	lines := make([]ui.PanelLine, 0, len(c.Notes))
	if len(c.Notes) == 0 {
		lines = append(lines, ui.PanelLine{{Text: "No local notes for this session.", Style: ui.PanelMuted}})
	}
	for index, note := range c.Notes {
		lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%d. %s", index+1, note), Style: ui.PanelForeground}})
	}
	lines = append(lines, ui.PanelLine{{Text: "Notes stay local and are not included in chat requests.", Style: ui.PanelMuted}})
	ui.PrintStyledPanel("Session notes", lines)
}

func saveNotes(c *Chat) error {
	if len(c.Messages) == 0 && len(c.Notes) == 0 && len(c.PendingSources) == 0 && len(c.pendingImages) == 0 {
		if c.SessionID == "" {
			return nil
		}
		session, err := c.HistoryManager.LoadSession(c.SessionID)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		session.Notes = nil
		return c.HistoryManager.SaveSession(session)
	}
	if c.SessionID == "" {
		c.SessionID = newSessionID()
	}
	if c.ConversationStartTime.IsZero() {
		c.ConversationStartTime = time.Now()
	}
	return c.SaveCurrentSession()
}
