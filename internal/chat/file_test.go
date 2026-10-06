package chat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/models"
)

var testPNG = []byte("\x89PNG\r\n\x1a\nimage")

func writeTestPNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "logo.png")
	if err := os.WriteFile(path, testPNG, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAddedFileAndChainContextUpdateContextTokenEstimate(t *testing.T) {
	tracker := analytics.NewChatAnalytics()
	c := &Chat{Analytics: tracker}
	c.addFileContext("notes.txt", []byte("file information"))
	c.AddContextMessage("[URL Context]\nhttps://example.test\n\npage information")
	snapshot := tracker.Snapshot()
	if snapshot.ContextMessages != 2 || snapshot.ContextTokensEstimate == 0 || snapshot.UserTokensEstimate != 0 {
		t.Fatalf("context estimates = messages:%d tokens:%d user tokens:%d, want two tracked context messages and no user tokens", snapshot.ContextMessages, snapshot.ContextTokensEstimate, snapshot.UserTokensEstimate)
	}
	if snapshot.TotalTokensEstimate != snapshot.ContextTokensEstimate {
		t.Fatalf("total token estimate = %d, context estimate = %d, want context represented in total", snapshot.TotalTokensEstimate, snapshot.ContextTokensEstimate)
	}
}

func TestPromptWithInsertedContextSplitsRoleTokenEstimates(t *testing.T) {
	tracker := analytics.NewChatAnalytics()
	c := &Chat{Analytics: tracker}
	contextText := strings.Repeat("c", 80)
	prompt := strings.Repeat("p", 40)
	fetch := func(context.Context, string) (<-chan string, error) {
		stream := make(chan string, 1)
		stream <- "answer"
		close(stream)
		return stream, nil
	}
	_, err := processInputWithFetcherAndTokenRoles(context.Background(), c, contextText+"\n\n"+prompt, &config.Config{}, renderStreamToString, fetch, len(prompt), len(contextText))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := tracker.Snapshot()
	if snapshot.UserTokensEstimate == 0 || snapshot.ContextTokensEstimate == 0 {
		t.Fatalf("role token estimates = user:%d context:%d, want both nonzero", snapshot.UserTokensEstimate, snapshot.ContextTokensEstimate)
	}
	if snapshot.TotalTokensEstimate != snapshot.UserTokensEstimate+snapshot.ContextTokensEstimate+snapshot.AssistantTokensEstimate {
		t.Fatalf("total estimate %d does not equal role estimates user:%d context:%d assistant:%d", snapshot.TotalTokensEstimate, snapshot.UserTokensEstimate, snapshot.ContextTokensEstimate, snapshot.AssistantTokensEstimate)
	}
}

func TestImageFileContextQueuesImageForNextPrompt(t *testing.T) {
	path := writeTestPNG(t)
	c := &Chat{}
	handleFileCommand(c, "/file "+path, &config.Config{}, nil, func(*Chat, string, *config.Config) {})

	if len(c.pendingImages) != 1 {
		t.Fatalf("pending image count = %d, want 1", len(c.pendingImages))
	}
	if c.pendingImages[0].Name != "logo.png" || string(c.pendingImages[0].Data) != string(testPNG) {
		t.Fatalf("pending image = %+v, want logo.png and original bytes", c.pendingImages[0])
	}
	if len(c.Messages) != 0 {
		t.Fatalf("queued image was inserted as a separate message: %+v", c.Messages)
	}
}

func TestImageFilePromptQueuesAttachmentWithPrompt(t *testing.T) {
	path := writeTestPNG(t)
	c := &Chat{}
	var requested string
	var message Message
	handleFileCommand(c, "/file "+path+" -- describe this duck", &config.Config{}, nil, func(chat *Chat, prompt string, cfg *config.Config) {
		requested = prompt
		_, err := processInputWithFetcher(context.Background(), chat, prompt, cfg, func(stream <-chan string) string {
			for range stream {
			}
			return "answer"
		}, func(_ context.Context, _ string) (<-chan string, error) {
			message = chat.Messages[len(chat.Messages)-1]
			stream := make(chan string, 1)
			stream <- "answer"
			close(stream)
			return stream, nil
		})
		if err != nil {
			t.Errorf("processInputWithFetcher() error = %v", err)
		}
	})

	if requested != "describe this duck" {
		t.Fatalf("requested prompt = %q, want %q", requested, "describe this duck")
	}
	if message.Content != requested || len(message.Images) != 1 || string(message.Images[0].Data) != string(testPNG) {
		t.Fatalf("submitted user message = %+v, want prompt and image in one message", message)
	}
}

func TestPendingImagesRestoreAfterFailedInputAndConsumeAfterSuccess(t *testing.T) {
	image := media.ImageAttachment{Name: "logo.png", MIMEType: "image/png", Data: testPNG}
	c := &Chat{pendingImages: []media.ImageAttachment{image}}
	render := func(stream <-chan string) string {
		for range stream {
		}
		return "answer"
	}
	_, err := processInputWithFetcher(context.Background(), c, "retry me", &config.Config{}, render, func(context.Context, string) (<-chan string, error) {
		return nil, errors.New("temporary failure")
	})
	if err == nil {
		t.Fatal("failed input unexpectedly succeeded")
	}
	if len(c.pendingImages) != 1 || c.Messages != nil {
		t.Fatalf("failed input did not restore state: pending=%d messages=%+v", len(c.pendingImages), c.Messages)
	}

	_, err = processInputWithFetcher(context.Background(), c, "retry me", &config.Config{}, render, func(context.Context, string) (<-chan string, error) {
		stream := make(chan string, 1)
		stream <- "answer"
		close(stream)
		return stream, nil
	})
	if err != nil {
		t.Fatalf("successful retry error = %v", err)
	}
	if len(c.pendingImages) != 0 {
		t.Fatalf("successful input left %d pending images, want 0", len(c.pendingImages))
	}
	if len(c.Messages) != 2 || len(c.Messages[0].Images) != 1 || string(c.Messages[0].Images[0].Data) != string(testPNG) {
		t.Fatalf("successful retry messages = %+v, want attached user message and assistant response", c.Messages)
	}
}

func TestPendingImagesRestoreAfterMidStreamFailure(t *testing.T) {
	image := media.ImageAttachment{Name: "logo.png", MIMEType: "image/png", Data: testPNG}
	c := &Chat{pendingImages: []media.ImageAttachment{image}}
	streamErrs := make(chan error, 1)
	streamErrs <- errors.New("truncated response")
	close(streamErrs)
	_, err := processInputWithStreamFetcher(context.Background(), c, "retry me", &config.Config{}, func(stream <-chan string) string {
		for range stream {
		}
		return "partial answer"
	}, func(context.Context, string) (<-chan string, <-chan error, error) {
		stream := make(chan string)
		close(stream)
		return stream, streamErrs, nil
	})
	if err == nil {
		t.Fatal("mid-stream failure unexpectedly succeeded")
	}
	if len(c.pendingImages) != 1 || len(c.Messages) != 0 {
		t.Fatalf("mid-stream failure did not roll back state: pending=%d messages=%+v", len(c.pendingImages), c.Messages)
	}
}

func TestInvalidImageFileDoesNotInvokeProcessor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.png")
	if err := os.WriteFile(path, []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &Chat{}
	called := false
	err := handleFileCommand(c, "/file "+path+" -- describe this", &config.Config{}, nil, func(*Chat, string, *config.Config) {
		called = true
	})
	if err == nil {
		t.Fatal("invalid image unexpectedly succeeded")
	}
	if called || len(c.pendingImages) != 0 || len(c.Messages) != 0 {
		t.Fatalf("invalid image had side effects: processor called=%t pending=%d messages=%+v", called, len(c.pendingImages), c.Messages)
	}
}

func TestImageRequestsUseVisionCapableDuckAIModel(t *testing.T) {
	for _, selectedModel := range []models.Model{models.MistralSmall, models.GPTOSS120B} {
		t.Run(string(selectedModel), func(t *testing.T) {
			c := &Chat{Model: selectedModel, pendingImages: []media.ImageAttachment{{Name: "logo.png", MIMEType: "image/png", Data: testPNG}}}
			var requestModel models.Model
			_, err := processInputWithStreamFetcher(context.Background(), c, "What is in this image?", &config.Config{}, func(stream <-chan string) string {
				for range stream {
				}
				return "A duck."
			}, func(context.Context, string) (<-chan string, <-chan error, error) {
				requestModel = c.Model
				stream := make(chan string)
				close(stream)
				return stream, nil, nil
			})
			if err != nil {
				t.Fatalf("processInputWithStreamFetcher() error = %v", err)
			}
			if requestModel != models.GPT54Mini {
				t.Fatalf("request model = %q, want vision-capable fallback %q", requestModel, models.GPT54Mini)
			}
			if c.Model != selectedModel {
				t.Fatalf("selected model changed to %q after image request, want %q", c.Model, selectedModel)
			}
		})
	}
}
