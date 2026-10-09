package chat

import (
	"fmt"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/models"
)

type ExportMetadata struct {
	Date        time.Time
	Model       string
	ContextSize int
	Type        string
}

func (c *Chat) Export(exportType string, query string) (string, string) {
	metadata := ExportMetadata{
		Date:        time.Now(),
		Model:       string(c.Model),
		ContextSize: len(c.Messages),
		Type:        exportType,
	}

	var content string
	filename := fmt.Sprintf("%s_%s.md", sanitizeFilename(exportType), time.Now().Format("20060102_150405"))

	switch exportType {
	case "conversation":
		content = c.formatConversation(metadata)
	case "last_response":
		content = c.formatLastResponse(metadata)
	case "code_block":
		content = c.formatCodeBlock(metadata)
	case "search_results":
		content = c.formatSearchResults(metadata, query)
	case "search_conversation": // add new case
		content = c.formatSearchInConversation(metadata, query)
	}

	return filename, content
}

func (c *Chat) formatConversation(metadata ExportMetadata) string {
	var sb strings.Builder
	writeMetadataHeader(&sb, metadata)

	for _, msg := range c.Messages {
		timestamp := formatMessageTimestamp(msg.Timestamp)

		switch {
		case strings.Contains(msg.Content, "[Search Context]"):
			writeSection(&sb, "Search Results", timestamp,
				strings.TrimPrefix(msg.Content, "[Search Context]\n"))
		case strings.Contains(msg.Content, "[File Context]"):
			writeSection(&sb, "File Content", timestamp,
				strings.TrimPrefix(msg.Content, "[File Context]\n"))
		case strings.Contains(msg.Content, "[URL Context]"):
			writeSection(&sb, "Web Content", timestamp,
				strings.TrimPrefix(msg.Content, "[URL Context]\n"))
		case msg.Role == "user":
			writeSection(&sb, "User Query", timestamp, msg.Content)
		case msg.Role == "assistant":
			title := fmt.Sprintf("%s Response", formatModelName(string(c.Model)))
			writeSection(&sb, title, timestamp, msg.Content)
		}
		for _, image := range msg.Images {
			writeSection(&sb, "Image Attachment", timestamp, imageAttachmentMarker(image))
		}
	}

	return sb.String()
}

func (c *Chat) formatLastResponse(metadata ExportMetadata) string {
	var sb strings.Builder
	metadata.Type = "Last AI Response"
	writeMetadataHeader(&sb, metadata)

	lastMsg := findLastAssistantMessage(c.Messages)
	if lastMsg != nil {
		title := fmt.Sprintf("%s Response", formatModelName(string(c.Model)))
		writeSection(&sb, title, formatMessageTimestamp(lastMsg.Timestamp), lastMsg.Content)
	}

	return sb.String()
}

func (c *Chat) formatCodeBlock(metadata ExportMetadata) string {
	var sb strings.Builder
	metadata.Type = "Code Block"
	writeMetadataHeader(&sb, metadata)

	if code, err := c.copyLargestCodeBlock(); err == nil {
		timestamp := "unknown"
		if message := findLastAssistantMessage(c.Messages); message != nil {
			timestamp = formatMessageTimestamp(message.Timestamp)
		}
		writeSection(&sb, "Code Block", timestamp,
			fmt.Sprintf("```\n%s\n```", code))
	}

	return sb.String()
}

func (c *Chat) formatSearchResults(metadata ExportMetadata, query string) string {
	var sb strings.Builder
	metadata.Type = "Search Results"
	writeMetadataHeader(&sb, metadata)

	writeSection(&sb, "Search Query", time.Now().Format("15:04"), query)

	for _, msg := range c.Messages {
		if strings.Contains(msg.Content, "[Search Context]") {
			writeSection(&sb, "Results", formatMessageTimestamp(msg.Timestamp),
				strings.TrimPrefix(msg.Content, "[Search Context]\n"))
			break
		}
	}

	return sb.String()
}

func (c *Chat) formatSearchInConversation(metadata ExportMetadata, searchText string) string {
	var sb strings.Builder
	metadata.Type = "Search Results"
	writeMetadataHeader(&sb, metadata)

	writeSection(&sb, "Search Query", time.Now().Format("15:04"), searchText)

	// search for the text in the conversation
	foundResults := false
	for i := 0; i < len(c.Messages); i++ {
		msg := c.Messages[i]
		if strings.Contains(strings.ToLower(msg.Content), strings.ToLower(searchText)) {
			foundResults = true

			// Ajouter le contexte (question et réponse)
			if msg.Role == "user" {
				writeSection(&sb, "User Message", formatMessageTimestamp(msg.Timestamp), exportMessageContent(msg))
				if i+1 < len(c.Messages) && c.Messages[i+1].Role == "assistant" {
					title := fmt.Sprintf("%s Response", formatModelName(string(c.Model)))
					writeSection(&sb, title, formatMessageTimestamp(c.Messages[i+1].Timestamp), c.Messages[i+1].Content)
				}
			} else if msg.Role == "assistant" && i > 0 {
				writeSection(&sb, "User Message", formatMessageTimestamp(c.Messages[i-1].Timestamp), exportMessageContent(c.Messages[i-1]))
				title := fmt.Sprintf("%s Response", formatModelName(string(c.Model)))
				writeSection(&sb, title, formatMessageTimestamp(msg.Timestamp), msg.Content)
			}
		}
	}

	if !foundResults {
		return ""
	}

	return sb.String()
}

func exportMessageContent(msg Message) string {
	var content strings.Builder
	content.WriteString(msg.Content)
	for _, image := range msg.Images {
		content.WriteString("\n\n")
		content.WriteString(imageAttachmentMarker(image))
	}
	return content.String()
}

func writeMetadataHeader(sb *strings.Builder, metadata ExportMetadata) {
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("date: %s\n", metadata.Date.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("model: %s\n", metadata.Model))
	sb.WriteString(fmt.Sprintf("type: %s\n", metadata.Type))
	sb.WriteString(fmt.Sprintf("context_size: %d\n", metadata.ContextSize))
	sb.WriteString("---\n\n")
	sb.WriteString(fmt.Sprintf("# %s Export\n\n", metadata.Type))
}

func writeSection(sb *strings.Builder, title, timestamp, content string) {
	sb.WriteString(fmt.Sprintf("## %s (%s)\n\n", title, timestamp))
	sb.WriteString(content)
	sb.WriteString("\n\n---\n\n")
}

func formatMessageTimestamp(timestamp time.Time) string {
	if timestamp.IsZero() {
		return "unknown"
	}
	return timestamp.Local().Format("2006-01-02 15:04:05")
}

func findLastAssistantMessage(messages []Message) *Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return &messages[i]
		}
	}
	return nil
}

func sanitizeFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, strings.ToLower(name))
}

func formatModelName(modelName string) string {
	if resolved, ok := models.ResolveModel(modelName); ok {
		return models.DisplayName(resolved)
	}
	return modelName
}
