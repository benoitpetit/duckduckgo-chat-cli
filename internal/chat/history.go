package chat

import (
	"fmt"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/ui"
)

func PrintHistory(c *Chat) {
	if c == nil || len(c.Messages) == 0 {
		ui.PrintPanel("Chat History", []string{"No messages in history yet."})
		return
	}

	lines := make([]ui.PanelLine, 0, len(c.Messages)*2)
	if c.Compacted {
		lines = append(lines, ui.PanelLine{{Text: "Compact summary session; full transcript parent: " + c.ParentSessionID, Style: ui.PanelInfo}})
	}
	for _, msg := range c.Messages {
		label := "Message"
		labelStyle := ui.PanelAssistant
		content := msg.Content
		switch {
		case strings.HasPrefix(msg.Content, "[Search Context]"):
			label = "Search Context"
			labelStyle = ui.PanelInfo
			content = strings.TrimSpace(strings.TrimPrefix(msg.Content, "[Search Context]"))
		case strings.HasPrefix(msg.Content, "[File Context]"):
			label = "File Context"
			labelStyle = ui.PanelInfo
			content = strings.TrimSpace(strings.TrimPrefix(msg.Content, "[File Context]"))
		case strings.HasPrefix(msg.Content, "[URL Context]"):
			label = "URL Context"
			labelStyle = ui.PanelInfo
			content = strings.TrimSpace(strings.TrimPrefix(msg.Content, "[URL Context]"))
		case msg.Role == "user":
			label = "You"
			labelStyle = ui.PanelUser
		}
		if msg.Pinned {
			label += " · pinned"
		}
		lines = append(lines, ui.PanelLine{{Text: label, Style: labelStyle}})
		for _, contentLine := range strings.Split(content, "\n") {
			lines = append(lines, ui.PanelLine{{Text: "  " + contentLine, Style: ui.PanelForeground}})
		}
		for _, image := range msg.Images {
			lines = append(lines, ui.PanelLine{{Text: "  " + imageAttachmentMarker(image), Style: ui.PanelMuted}})
		}
		for _, source := range msg.Sources {
			state := source.State
			if state == "" {
				state = chatcontext.SourceStateActive
			}
			lines = append(lines, ui.PanelLine{{Text: fmt.Sprintf("  Source: %s (%s)", displaySourceName(source), state), Style: ui.PanelMuted}})
		}
		lines = append(lines, ui.PanelLine{{Text: "", Style: ui.PanelForeground}})
	}
	ui.PrintStyledPanel(fmt.Sprintf("Chat History · %d messages", len(c.Messages)), lines)
}

// GetMarkdownContent exports the chat history in markdown format
func (c *Chat) GetMarkdownContent() string {
	var md strings.Builder

	md.WriteString("---\n")
	md.WriteString(fmt.Sprintf("date: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	md.WriteString(fmt.Sprintf("model: %s\n", c.Model))
	md.WriteString(fmt.Sprintf("context_size: %d\n", len(c.Messages)))
	if c.Compacted {
		md.WriteString("compacted: true\n")
		md.WriteString(fmt.Sprintf("parent_session: %s\n", c.ParentSessionID))
	}
	md.WriteString("---\n\n")
	md.WriteString("# DuckDuckGo AI Chat Export\n\n")

	for _, msg := range c.Messages {
		timestamp := formatMessageTimestamp(msg.Timestamp)
		switch {
		case strings.Contains(msg.Content, "[Search Context]"):
			md.WriteString(fmt.Sprintf("### Search Context (%s)\n\n", timestamp))
			md.WriteString("```\n" + strings.TrimPrefix(msg.Content, "[Search Context]\n") + "\n```\n")
		case strings.Contains(msg.Content, "[File Context]"):
			md.WriteString(fmt.Sprintf("### File Content (%s)\n\n", timestamp))
			md.WriteString("```\n" + strings.TrimPrefix(msg.Content, "[File Context]\n") + "\n```\n")
		case strings.Contains(msg.Content, "[URL Context]"):
			md.WriteString(fmt.Sprintf("### Web Content (%s)\n\n", timestamp))
			md.WriteString("```\n" + strings.TrimPrefix(msg.Content, "[URL Context]\n") + "\n```\n")
		default:
			if msg.Role == "user" {
				md.WriteString(fmt.Sprintf("### User Query (%s)\n\n", timestamp))
				md.WriteString(msg.Content + "\n")
			} else {
				md.WriteString(fmt.Sprintf("### AI Response (%s)\n\n", timestamp))
				md.WriteString(msg.Content + "\n")
			}
		}
		for _, image := range msg.Images {
			md.WriteString("\n" + imageAttachmentMarker(image) + "\n")
		}
		md.WriteString("\n---\n\n")
	}
	if len(c.Notes) > 0 {
		md.WriteString("## Local Session Notes (not sent to the model)\n\n")
		md.WriteString(strings.Join(c.Notes, "\n\n"))
		md.WriteString("\n")
	}

	return md.String()
}

func imageAttachmentMarker(image media.ImageAttachment) string {
	return fmt.Sprintf("[Image attachment: %s (%s)]", image.Name, image.MIMEType)
}

// ExtractLastMessage extracts the last AI response
func (c *Chat) ExtractLastMessage() (string, string) {
	var lastMessage Message
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == "assistant" {
			lastMessage = c.Messages[i]
			break
		}
	}

	if lastMessage.Content == "" {
		return "", ""
	}

	title := FormatMessageTitle(lastMessage.Content)
	filename := fmt.Sprintf("%s_%s.md", title, time.Now().Format("20060102"))
	content := fmt.Sprintf("# Extracted Message\n\n%s\n", lastMessage.Content)

	return filename, content
}

// FindMessageByText searches for a message containing the given text
func (c *Chat) FindMessageByText(substring string) (string, string) {
	var foundMessage Message
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if strings.Contains(strings.ToLower(c.Messages[i].Content), strings.ToLower(substring)) {
			foundMessage = c.Messages[i]
			break
		}
	}

	if foundMessage.Content == "" {
		return "", ""
	}

	filename := fmt.Sprintf("message_%s.md", time.Now().Format("20060102_150405"))
	content := fmt.Sprintf("# Message containing: %s\n\n%s\n", substring, foundMessage.Content)

	return filename, content
}

// FormatMessageTitle formats a message title for use in filenames
func FormatMessageTitle(content string) string {
	words := strings.Fields(content)
	var title string
	if len(words) > 5 {
		title = strings.Join(words[:5], "_")
	} else {
		title = strings.Join(words, "_")
	}

	title = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, title)

	if len(title) > 50 {
		title = title[:50]
	}

	return title
}
