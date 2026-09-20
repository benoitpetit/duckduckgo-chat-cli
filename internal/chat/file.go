package chat

import (
	"bytes"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/ui"
)

const maxTextFileSize = 10 << 20

func HandleFileCommand(c *Chat, input string, cfg *config.Config, chainCtx *chatcontext.Context) {
	var path, userRequest string
	var err error

	// Handle the case where the command is just "/file" to open the browser
	parsed, parseErr := command.Parse(input)
	if parseErr != nil || len(parsed.Commands) != 1 {
		ui.Errorln("Invalid file command: %v", parseErr)
		return
	}

	if strings.TrimSpace(parsed.Commands[0].Args) == "" {
		path, err = ui.SelectFile()
		if err != nil {
			ui.Errorln("Error selecting file: %v", err)
			return
		}
	} else {
		path = strings.TrimSpace(parsed.Commands[0].Args)
		userRequest = parsed.Prompt
	}

	// If no path was selected or provided, exit the command.
	if path == "" {
		ui.Warningln("No file selected or specified.")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		ui.Errorln("File error: %v", err)
		return
	}
	if info.IsDir() {
		ui.Errorln("File error: %s is a directory", path)
		return
	}
	if info.Size() > maxTextFileSize {
		ui.Errorln("File error: %s exceeds the %d MiB limit", path, maxTextFileSize/(1<<20))
		return
	}
	content, err := os.ReadFile(path)
	if err != nil {
		ui.Errorln("File error: %v", err)
		return
	}
	if !isSupportedTextFile(path, content) {
		ui.Errorln("File error: %s is not a supported text file", path)
		return
	}

	if chainCtx != nil {
		chainCtx.AddFile(path, content)
		ui.AIln("Successfully added content from file to chain context: %s", path)
	} else {
		ui.Warningln("Adding file content: %s", path)
		c.addFileContext(path, content)
		ui.AIln("Successfully added content from file: %s", path)
		// If user provided a specific request, process it with the file context
		if userRequest != "" {
			ui.Systemln("Processing your request about the file...")
			ProcessInput(c, userRequest, cfg)
		} else {
			ui.Warningln("File content added to context. You can now ask questions about it.")
		}
	}
}

func (c *Chat) addFileContext(path string, content []byte) {
	contentLength := len(content)
	if contentLength > 500 {
		ui.AIln("Adding %d characters from file", contentLength)
	}

	c.Messages = append(c.Messages, Message{
		Role:    "user",
		Content: fmt.Sprintf("[File Context]\nFile: %s\n\n%s", filepath.Base(path), string(content)),
	})

	if c.Analytics != nil {
		c.Analytics.RecordFileProcessed()
	}
}

func isSupportedTextFile(path string, content []byte) bool {
	if bytes.IndexByte(content, 0) >= 0 {
		return false
	}
	if contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); contentType != "" {
		return strings.HasPrefix(contentType, "text/") || strings.Contains(contentType, "json") || strings.Contains(contentType, "xml") || strings.Contains(contentType, "javascript")
	}
	return true
}
