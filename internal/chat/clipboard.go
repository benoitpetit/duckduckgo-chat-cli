package chat

import (
	"fmt"
	"os"
	"regexp"

	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	"github.com/atotto/clipboard"
)

func HandleCopyCommand(c *Chat) {
	var choice string
	prompt := &survey.Select{
		Message: "Choose what to copy:",
		Options: []string{
			"Last Q&A exchange",
			"Largest code block",
			"Cancel",
		},
		Default: "Last Q&A exchange",
	}
	err := survey.AskOne(prompt, &choice, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		ui.Warningln("\nCopy canceled.")
		return
	}

	var content string

	switch choice {
	case "Last Q&A exchange":
		content, err = c.copyLastExchange()
	case "Largest code block":
		content, err = c.copyLargestCodeBlock()
	default:
		ui.Warningln("Copy canceled.")
		return
	}

	if err != nil {
		ui.Errorln("Error: %v", err)
		return
	}

	if err := clipboard.WriteAll(content); err != nil {
		ui.Errorln("Failed to copy to clipboard: %v", err)
		return
	}
	ui.AIln("Content copied to clipboard")
}

func (c *Chat) copyLastExchange() (string, error) {
	if len(c.Messages) < 2 {
		return "", fmt.Errorf("no complete exchange found")
	}

	var lastQuestion, lastAnswer string
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == "assistant" {
			lastAnswer = c.Messages[i].Content
			if i > 0 && c.Messages[i-1].Role == "user" {
				lastQuestion = c.Messages[i-1].Content
				break
			}
		}
	}

	if lastQuestion == "" || lastAnswer == "" {
		return "", fmt.Errorf("no complete exchange found")
	}

	return fmt.Sprintf("Q: %s\n\nA: %s", lastQuestion, lastAnswer), nil
}

func (c *Chat) copyLargestCodeBlock() (string, error) {
	var lastMessage Message
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == "assistant" {
			lastMessage = c.Messages[i]
			break
		}
	}

	if lastMessage.Content == "" {
		return "", fmt.Errorf("no AI response found")
	}

	codeBlockRegex := regexp.MustCompile("```(?:.*?)\n([\\s\\S]*?)```")
	matches := codeBlockRegex.FindAllStringSubmatch(lastMessage.Content, -1)

	if len(matches) == 0 {
		indentRegex := regexp.MustCompile("(?m)^( {4}|\\t).*(?:\n(?:(?:( {4}|\\t).*)|(?:\n))*)")
		matches = indentRegex.FindAllStringSubmatch(lastMessage.Content, -1)
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no code blocks found")
	}

	var largestBlock string
	maxLen := 0
	for _, match := range matches {
		block := match[1]
		if len(block) > maxLen {
			maxLen = len(block)
			largestBlock = block
		}
	}

	return largestBlock, nil
}
