package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/ui"
)

func HandleRetryCommand(c *Chat, instruction string, cfg *config.Config) {
	if err := RetryLastAnswer(c, cfg, instruction); err != nil {
		ui.Errorln("Retry failed: %v", err)
	}
}

func RetryLastAnswer(c *Chat, cfg *config.Config, instruction string) error {
	if c == nil || cfg == nil {
		return fmt.Errorf("chat session or configuration is unavailable")
	}
	if _, _, err := latestCompletedExchange(c.Messages); err != nil {
		return err
	}

	modelLabel := shortenModelName(string(c.Model))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.setRequestCancel(cancel)
	defer c.clearRequestCancel()
	spinner := ui.StartSpinnerWithModel(ui.AccentColor.Sprint(modelLabel))
	defer spinner.Stop()
	deps := retryCommandDependencies{
		fetch: func(ctx context.Context, content string) (<-chan string, <-chan error, error) {
			return c.FetchStreamWithErrorsAndProgressEvents(ctx, content, func(update ProgressUpdate) {
				spinner.SetProgress(update.Stage, update.Label)
			}, nil)
		},
		render: func(stream <-chan string) string {
			return RenderStreamWithView(stream, modelLabel, spinner, cfg.Appearance.FrameResponses, c.Display)
		},
	}
	response, err := retryLastAnswerWithDependencies(ctx, c, cfg, instruction, deps)
	spinner.Stop()
	if err != nil {
		c.reportChatFailure(ctx, err)
		return err
	}
	if c.Display != nil {
		c.Display.RecordAssistant(modelLabel, response)
	}
	ui.AIln("Replaced the previous answer using %s.", models.DisplayName(c.Model))
	return nil
}

type retryCommandDependencies struct {
	fetch  func(context.Context, string) (<-chan string, <-chan error, error)
	render func(<-chan string) string
}

func retryLastAnswerWithDependencies(ctx context.Context, c *Chat, cfg *config.Config, instruction string, deps retryCommandDependencies) (string, error) {
	if c == nil || cfg == nil {
		return "", fmt.Errorf("chat session or configuration is unavailable")
	}
	if deps.fetch == nil || deps.render == nil {
		return "", fmt.Errorf("retry dependencies are incomplete")
	}
	assistantIndex, userIndex, err := latestCompletedExchange(c.Messages)
	if err != nil {
		return "", err
	}

	original := cloneChatMessages(c.Messages)
	pendingImages := cloneImageAttachments(c.pendingImages)
	pendingSources := cloneSourceRefs(c.PendingSources)
	previousOptimization := append([]intelligence.OptimizationChange(nil), c.LastOptimizationChanges...)

	prefix := cloneChatMessages(original[:userIndex])
	tail := cloneChatMessages(original[assistantIndex+1:])
	userMessage := original[userIndex]
	if instruction = strings.TrimSpace(instruction); instruction != "" {
		userMessage.Content += "\n\n[Retry instruction]\n" + instruction
	}
	userMessage.Timestamp = time.Now()
	c.Messages = prefix
	input := userMessage.Content
	response, err := processInputWithStreamFetcherAndTokenRolesOptions(ctx, c, input, cfg,
		deps.render, deps.fetch, len(input), 0,
		pipelineOptions{SkipGlobalPrompt: true, SkipSourceLabels: true, PreservePending: true, UserMessage: &userMessage})
	if err != nil {
		c.Messages = original
		c.pendingImages = pendingImages
		c.PendingSources = pendingSources
		c.LastOptimizationChanges = previousOptimization
		return "", err
	}
	assistant := Message{Role: "assistant", Content: response, Timestamp: time.Now()}
	accepted := append(prefix, userMessage, assistant)
	accepted = append(accepted, tail...)
	c.Messages = accepted
	c.pendingImages = pendingImages
	c.PendingSources = pendingSources
	return response, nil
}

func latestCompletedExchange(messages []Message) (assistantIndex, userIndex int, err error) {
	assistantIndex, userIndex = -1, -1
	for index := len(messages) - 1; index >= 0; index-- {
		if assistantIndex < 0 && messages[index].Role == "assistant" {
			assistantIndex = index
			continue
		}
		if assistantIndex >= 0 && messages[index].Role == "user" {
			userIndex = index
			break
		}
	}
	if assistantIndex < 0 || userIndex < 0 {
		return -1, -1, fmt.Errorf("there is no completed chat exchange to retry")
	}
	return assistantIndex, userIndex, nil
}
