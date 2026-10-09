package chat

import (
	"fmt"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/media"

	"github.com/fatih/color"
)

func PrintHistory(c *Chat) {
	if len(c.Messages) == 0 {
		color.Yellow("No messages in history yet")
		return
	}

	dimWhite := color.New(color.FgHiWhite, color.Faint)
	dimBlue := color.New(color.FgBlue, color.Faint)
	dimGreen := color.New(color.FgHiGreen, color.Faint)
	dimYellow := color.New(color.FgYellow, color.Faint)

	// add a newline before printing the history
	fmt.Println()
	for i, msg := range c.Messages {
		switch {
		case strings.HasPrefix(msg.Content, "[Search Context]"):
			dimYellow.Print("Search Context: ")
			dimWhite.Println(strings.TrimSpace(strings.TrimPrefix(msg.Content, "[Search Context]")))
		case strings.HasPrefix(msg.Content, "[File Context]"):
			dimYellow.Print("File Context: ")
			dimWhite.Println(strings.TrimSpace(strings.TrimPrefix(msg.Content, "[File Context]")))
		case strings.HasPrefix(msg.Content, "[URL Context]"):
			dimYellow.Print("URL Context: ")
			dimWhite.Println(strings.TrimSpace(strings.TrimPrefix(msg.Content, "[URL Context]")))
		case msg.Role == "user":
			dimBlue.Print("You: ")
			dimWhite.Println(msg.Content)
		case msg.Role == "assistant":
			dimGreen.Print("\nAI: ")
			dimWhite.Println(msg.Content)
		default:
			dimWhite.Println(msg.Content)
		}
		for _, image := range msg.Images {
			dimYellow.Print("  ")
			dimWhite.Println(imageAttachmentMarker(image))
		}

		// add a newline between messages
		if i < len(c.Messages)-1 {
			fmt.Println()
		}
	}
	fmt.Println()
}

// GetMarkdownContent exports the chat history in markdown format
func (c *Chat) GetMarkdownContent() string {
	var md strings.Builder

	md.WriteString("---\n")
	md.WriteString(fmt.Sprintf("date: %s\n", time.Now().Format("2006-01-02 15:04:05")))
	md.WriteString(fmt.Sprintf("model: %s\n", c.Model))
	md.WriteString(fmt.Sprintf("context_size: %d\n", len(c.Messages)))
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
