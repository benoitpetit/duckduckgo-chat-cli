package chat

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
)

func HandleContextCommandArgs(c *Chat, args string, cfg *config.Config) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		HandleContextCommand(c)
		return
	}
	switch fields[0] {
	case "pin", "unpin":
		if len(fields) != 2 {
			ui.Warningln("Usage: /context %s <message-number|source-id>", fields[0])
			return
		}
		if err := setContextPin(c, fields[1], fields[0] == "pin"); err != nil {
			ui.Warningln("%v", err)
			return
		}
		verb := "removed"
		if fields[0] == "pin" {
			verb = "pinned"
		}
		ui.AIln("Context item %s %s.", fields[1], verb)
		if fields[0] == "pin" && c.ContextOptimizer != nil && pinnedContextBytes(c) > c.ContextOptimizer.MaxContextSize {
			ui.Warningln("Pinned context is larger than the configured text budget. Pinned content will be retained and may exceed it; unpin items or use /context compact.")
		}
	case "pins":
		showContextPins(c)
	case "compact":
		objective := strings.TrimSpace(strings.TrimPrefix(args, "compact"))
		if err := compactConversation(c, cfg, objective); err != nil {
			ui.Errorln("Could not compact the conversation: %v", err)
		}
	default:
		ui.Warningln("Usage: /context [pin|unpin <message-number|source-id>|pins|compact [objective]]")
	}
}

func setContextPin(c *Chat, selector string, pinned bool) error {
	if c == nil {
		return fmt.Errorf("chat session is unavailable")
	}
	if strings.HasPrefix(selector, "source-") {
		found := false
		for messageIndex := range c.Messages {
			for sourceIndex := range c.Messages[messageIndex].Sources {
				if c.Messages[messageIndex].Sources[sourceIndex].ID == selector {
					c.Messages[messageIndex].Sources[sourceIndex].Pinned = pinned
					found = true
				}
			}
		}
		for sourceIndex := range c.PendingSources {
			if c.PendingSources[sourceIndex].ID == selector {
				c.PendingSources[sourceIndex].Pinned = pinned
				found = true
			}
		}
		if !found {
			return fmt.Errorf("source %q was not found in this conversation", selector)
		}
		return nil
	}
	index, err := strconv.Atoi(selector)
	if err != nil || index < 1 || index > len(c.Messages) {
		return fmt.Errorf("%q is not a message number from 1 to %d or a source ID", selector, len(c.Messages))
	}
	c.Messages[index-1].Pinned = pinned
	return nil
}

func showContextPins(c *Chat) {
	if c == nil {
		return
	}
	lines := make([]ui.PanelLine, 0)
	for index, message := range c.Messages {
		if message.Pinned {
			lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("Message %d · %s", index+1, shortContextText(message.Content, 90)), Style: ui.PanelInfo}})
		}
		for _, source := range message.Sources {
			if source.Pinned {
				lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%s · %s", source.ID, displaySourceName(source)), Style: ui.PanelInfo}})
			}
		}
	}
	for _, source := range c.PendingSources {
		if source.Pinned {
			lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("%s · %s · pending", source.ID, displaySourceName(source)), Style: ui.PanelInfo}})
		}
	}
	if len(lines) == 0 {
		lines = append(lines, ui.PanelLine{{Text: "No pinned context items.", Style: ui.PanelMuted}})
	}
	ui.PrintStyledPanel("Pinned context", lines)
}

func compactConversation(c *Chat, cfg *config.Config, objective string) error {
	return compactConversationWithDependencies(c, cfg, objective, compactionDependencies{
		summarize: summarizeConversationForCompaction,
		confirm:   confirmCompactionSummary,
		save: func(chat *Chat) error {
			return chat.SaveCurrentSession()
		},
	})
}

type compactionDependencies struct {
	summarize func(*Chat, *config.Config, string) (string, error)
	confirm   func(string) (bool, error)
	save      func(*Chat) error
}

func compactConversationWithDependencies(c *Chat, cfg *config.Config, objective string, deps compactionDependencies) error {
	if c == nil || cfg == nil || c.HistoryManager == nil {
		return fmt.Errorf("chat session or history manager is unavailable")
	}
	if len(c.Messages) == 0 {
		return fmt.Errorf("there is no conversation to compact")
	}
	if deps.summarize == nil || deps.confirm == nil || deps.save == nil {
		return fmt.Errorf("compaction dependencies are incomplete")
	}
	if anyPinnedMessage(c.Messages) {
		ui.Warningln("Pinned messages will be included in the summary request. Review their size in /context before continuing.")
	}

	originalMessages := cloneChatMessages(c.Messages)
	originalPendingImages := cloneImageAttachments(c.pendingImages)
	originalPendingSources := cloneSourceRefs(c.PendingSources)
	originalChanges := append(c.LastOptimizationChanges[:0:0], c.LastOptimizationChanges...)
	response, err := deps.summarize(c, cfg, objective)
	c.Messages = originalMessages
	c.pendingImages = originalPendingImages
	c.PendingSources = originalPendingSources
	c.LastOptimizationChanges = originalChanges
	c.resetDurableConversation()
	if err != nil {
		return err
	}
	if strings.TrimSpace(response) == "" {
		return fmt.Errorf("the model returned an empty summary")
	}

	confirmed, confirmErr := deps.confirm(response)
	if confirmErr != nil || !confirmed {
		ui.Warningln("Compaction canceled; the active conversation is unchanged.")
		return nil
	}

	originalSessionID := c.SessionID
	parentID := originalSessionID
	if parentID == "" {
		parentID = newSessionID()
	}
	if c.SessionID == "" {
		c.SessionID = parentID
	}
	if err := deps.save(c); err != nil {
		c.SessionID = originalSessionID
		return fmt.Errorf("save original conversation: %w", err)
	}
	oldSessionID, oldParentID, oldBranchName := c.SessionID, c.ParentSessionID, c.BranchName
	oldCompacted, oldTitle := c.Compacted, c.SessionTitle
	oldTags := append([]string(nil), c.SessionTags...)
	oldFavorite, oldStarted := c.SessionFavorite, c.ConversationStartTime
	c.SessionID = newSessionID()
	c.ParentSessionID = parentID
	c.BranchName = "compacted"
	c.ConversationStartTime = time.Now()
	title := "Compacted session"
	if oldTitle != "" {
		title = "Compacted: " + oldTitle
	}
	if runes := []rune(title); len(runes) > 100 {
		title = string(runes[:99]) + "…"
	}
	c.SessionTitle = title
	compactRefs := compactedSourceRefs(sourcesForLatestExchange(c))
	compactedMessages := make([]Message, 0, len(originalMessages)+1)
	for _, message := range originalMessages {
		if message.Pinned || hasPinnedSourceRef(message.Sources) {
			compactedMessages = append(compactedMessages, message)
		}
	}
	compactedMessages = append(compactedMessages, Message{
		Role: "user", Content: "[Compact conversation summary]\n" + strings.TrimSpace(response),
		Images: cloneImageAttachments(originalPendingImages), Sources: compactRefs,
		Timestamp: time.Now(),
	})
	c.Messages = compactedMessages
	c.PendingSources = nil
	c.pendingImages = nil
	c.Compacted = true
	c.LastOptimizationChanges = nil
	c.resetDurableConversation()
	if err := deps.save(c); err != nil {
		c.SessionID, c.ParentSessionID, c.BranchName = oldSessionID, oldParentID, oldBranchName
		c.Compacted, c.SessionTitle = oldCompacted, oldTitle
		c.SessionTags, c.SessionFavorite, c.ConversationStartTime = oldTags, oldFavorite, oldStarted
		c.Messages = originalMessages
		c.PendingSources = originalPendingSources
		c.pendingImages = originalPendingImages
		c.LastOptimizationChanges = originalChanges
		c.resetDurableConversation()
		return fmt.Errorf("save compacted session: %w", err)
	}
	ui.AIln("Conversation compacted into session %s. The full transcript remains in parent session %s.", c.SessionID, parentID)
	return nil
}

func summarizeConversationForCompaction(c *Chat, cfg *config.Config, objective string) (string, error) {
	request := "Create a concise restart summary of the conversation so far. Preserve the user's goal, decisions, constraints, important facts, unresolved questions, and useful source names. Clearly mark uncertainty. State that full source contents are not retained after compaction. Do not claim that any source or answer is verified."
	if objective != "" {
		request += "\nFocus on this objective: " + objective
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.setRequestCancel(cancel)
	defer c.clearRequestCancel()
	return processInput(ctx, c, request, cfg, renderStreamToString)
}

func confirmCompactionSummary(response string) (bool, error) {
	lines := []ui.PanelLine{{{Text: "Review the proposed summary. The current transcript stays unchanged until you accept it.", Style: ui.PanelMuted}}}
	for _, line := range strings.Split(strings.TrimSpace(response), "\n") {
		lines = append(lines, ui.PanelLine{{Text: line, Style: ui.PanelForeground}})
	}
	ui.PrintStyledPanel("Conversation compact summary", lines)
	confirmed := false
	err := survey.AskOne(&survey.Confirm{Message: "Use this summary as the active context? The full conversation will remain saved as its parent session.", Default: false}, &confirmed)
	return confirmed, err
}

func anyPinnedMessage(messages []Message) bool {
	for _, message := range messages {
		if message.Pinned || hasPinnedSourceRef(message.Sources) {
			return true
		}
	}
	return false
}

func hasPinnedSourceRef(sources []chatcontext.SourceRef) bool {
	for _, source := range sources {
		if source.Pinned {
			return true
		}
	}
	return false
}

func compactedSourceRefs(sources []chatcontext.SourceRef) []chatcontext.SourceRef {
	compacted := cloneSourceRefs(sources)
	for index := range compacted {
		if compacted[index].State == "" || compacted[index].State == chatcontext.SourceStateActive {
			compacted[index].State = chatcontext.SourceStateCompressed
		}
	}
	return compacted
}

func pinnedContextBytes(c *Chat) int {
	total := 0
	seenSources := make(map[string]bool)
	for _, message := range c.Messages {
		pinned := message.Pinned
		for _, source := range message.Sources {
			if source.Pinned {
				pinned = true
				seenSources[source.ID] = true
			}
		}
		if pinned {
			total += len(message.Content)
		}
	}
	for _, source := range c.PendingSources {
		if source.Pinned && !seenSources[source.ID] {
			total += max(source.SizeBytes, 0)
		}
	}
	return total
}

func cloneChatMessages(messages []Message) []Message {
	cloned := make([]Message, len(messages))
	for i, message := range messages {
		cloned[i] = message
		cloned[i].Images = cloneImageAttachments(message.Images)
		cloned[i].Sources = cloneSourceRefs(message.Sources)
	}
	return cloned
}

func shortContextText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > limit {
		return value[:limit-1] + "…"
	}
	return value
}

func formatByteCount(value int) string {
	if value < 0 {
		value = 0
	}
	return fmt.Sprintf("%d bytes", value)
}
