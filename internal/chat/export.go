package chat

import (
	"fmt"
	"strings"
	"time"
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

	for i, msg := range c.Messages {
		timestamp := time.Now().Add(time.Duration(-len(c.Messages)+i) * time.Minute).Format("15:04")

		switch {
		case strings.Contains(msg.Content, "[Search Context]"):
			writeSection(&sb, "🔍 Search Results", timestamp,
				strings.TrimPrefix(msg.Content, "[Search Context]\n"))
		case strings.Contains(msg.Content, "[File Context]"):
			writeSection(&sb, "📄 File Content", timestamp,
				strings.TrimPrefix(msg.Content, "[File Context]\n"))
		case strings.Contains(msg.Content, "[URL Context]"):
			writeSection(&sb, "🌐 Web Content", timestamp,
				strings.TrimPrefix(msg.Content, "[URL Context]\n"))
		case msg.Role == "user":
			writeSection(&sb, "🧑 User Query", timestamp, msg.Content)
		case msg.Role == "assistant":
			title := fmt.Sprintf("🤖 %s Response", formatModelName(string(c.Model)))
			writeSection(&sb, title, timestamp, msg.Content)
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
		title := fmt.Sprintf("🤖 %s Response", formatModelName(string(c.Model)))
		writeSection(&sb, title, time.Now().Format("15:04"), lastMsg.Content)
	}

	return sb.String()
}

func (c *Chat) formatCodeBlock(metadata ExportMetadata) string {
	var sb strings.Builder
	metadata.Type = "Code Block"
	writeMetadataHeader(&sb, metadata)

	if code, err := c.copyLargestCodeBlock(); err == nil {
		writeSection(&sb, "💻 Code Block", time.Now().Format("15:04"),
			fmt.Sprintf("```\n%s\n```", code))
	}

	return sb.String()
}

func (c *Chat) formatSearchResults(metadata ExportMetadata, query string) string {
	var sb strings.Builder
	metadata.Type = "Search Results"
	writeMetadataHeader(&sb, metadata)

	writeSection(&sb, "🔍 Search Query", time.Now().Format("15:04"), query)

	for _, msg := range c.Messages {
		if strings.Contains(msg.Content, "[Search Context]") {
			writeSection(&sb, "📊 Results", time.Now().Format("15:04"),
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

	writeSection(&sb, "🔍 Search Query", time.Now().Format("15:04"), searchText)

	// search for the text in the conversation
	foundResults := false
	for i := 0; i < len(c.Messages); i++ {
		msg := c.Messages[i]
		if strings.Contains(strings.ToLower(msg.Content), strings.ToLower(searchText)) {
			foundResults = true

			// Ajouter le contexte (question et réponse)
			if msg.Role == "user" && i+1 < len(c.Messages) {
				writeSection(&sb, "🧑 User Message", time.Now().Format("15:04"), msg.Content)
				if c.Messages[i+1].Role == "assistant" {
					title := fmt.Sprintf("🤖 %s Response", formatModelName(string(c.Model)))
					writeSection(&sb, title, time.Now().Format("15:04"), c.Messages[i+1].Content)
				}
			} else if msg.Role == "assistant" && i > 0 {
				writeSection(&sb, "🧑 User Message", time.Now().Format("15:04"), c.Messages[i-1].Content)
				title := fmt.Sprintf("🤖 %s Response", formatModelName(string(c.Model)))
				writeSection(&sb, title, time.Now().Format("15:04"), msg.Content)
			}
		}
	}

	if !foundResults {
		return ""
	}

	return sb.String()
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
	displayNames := map[string]string{
		"gpt-4o-mini":                             "GPT-4o mini",
		"claude-3-5-haiku-latest":                 "Claude 3 Haiku",
		"meta-llama/Llama-4-Scout-17B-16E-Instruct": "Llama 3.3 70B",
		"mistralai/Mixtral-8x7B-Instruct-v0.1":    "Mistral 8x7B",
		"gpt-5-mini":                                 "gpt-5",	
		"openai/gpt-oss-120b": 						"gpt-oss",
	}

	if shortName, exists := displayNames[modelName]; exists {
		return shortName
	}
	return modelName
}
