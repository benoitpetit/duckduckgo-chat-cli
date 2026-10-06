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
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/ui"
)

const maxTextFileSize = 10 << 20

// Keep bulk library imports bounded even when every individual file is valid.
const maxLibraryContextSize = 2 << 20

func HandleFileCommand(c *Chat, input string, cfg *config.Config, chainCtx *chatcontext.Context) error {
	return handleFileCommand(c, input, cfg, chainCtx, ProcessInput)
}

func handleFileCommand(c *Chat, input string, cfg *config.Config, chainCtx *chatcontext.Context, process func(*Chat, string, *config.Config)) error {
	var path, userRequest string
	var err error

	// Handle the case where the command is just "/file" to open the browser
	parsed, parseErr := command.Parse(input)
	if parseErr != nil || len(parsed.Commands) != 1 {
		ui.Errorln("Invalid file command: %v", parseErr)
		if parseErr != nil {
			return fmt.Errorf("invalid file command: %w", parseErr)
		}
		return fmt.Errorf("invalid file command")
	}

	if strings.TrimSpace(parsed.Commands[0].Args) == "" {
		path, err = ui.SelectFile()
		if err != nil {
			ui.Errorln("Error selecting file: %v", err)
			return err
		}
	} else {
		path = strings.TrimSpace(parsed.Commands[0].Args)
		userRequest = parsed.Prompt
	}

	// If no path was selected or provided, exit the command.
	if path == "" {
		ui.Warningln("No file selected or specified.")
		return fmt.Errorf("no file selected or specified")
	}

	if isSupportedImageExtension(path) {
		image, err := media.ReadImage(path)
		if err != nil {
			ui.Errorln("File error: %v", err)
			return err
		}
		if chainCtx != nil {
			chainCtx.AddImage(path, image)
			ui.AIln("Successfully added image to chain context: %s", path)
			return nil
		}

		c.QueueImageAttachments([]media.ImageAttachment{image})
		if c.Analytics != nil {
			c.Analytics.RecordFileProcessed()
		}
		ui.Warningln("Adding image: %s", path)
		ui.AIln("Successfully added image: %s", path)
		if userRequest != "" {
			ui.Systemln("Processing your request about the image...")
			process(c, userRequest, cfg)
		} else {
			ui.Warningln("Image added to context. Your next prompt will include it.")
		}
		return nil
	}

	content, err := readTextContextFile(path)
	if err != nil {
		ui.Errorln("File error: %v", err)
		return err
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
			process(c, userRequest, cfg)
		} else {
			ui.Warningln("File content added to context. You can now ask questions about it.")
		}
	}
	return nil
}

func isSupportedImageExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

func readTextContextFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > maxTextFileSize {
		return nil, fmt.Errorf("%s exceeds the %d MiB limit", path, maxTextFileSize/(1<<20))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !isSupportedTextFile(path, content) {
		return nil, fmt.Errorf("%s is not a supported text file", path)
	}
	return content, nil
}

func (c *Chat) addFileContext(path string, content []byte) {
	contentLength := len(content)
	if contentLength > 500 {
		ui.AIln("Adding %d characters from file", contentLength)
	}

	message := fmt.Sprintf("[File Context]\nFile: %s\n\n%s", filepath.Base(path), string(content))
	c.Messages = append(c.Messages, Message{
		Role:    "user",
		Content: message,
	})
	c.recordContextMessage(message)

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
