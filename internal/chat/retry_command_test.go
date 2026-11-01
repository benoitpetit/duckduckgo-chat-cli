package chat

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/models"
)

func TestRetryLastAnswerReplacesOnlyTheLatestCompletedAnswer(t *testing.T) {
	source := chatcontext.SourceRef{ID: "source-1", Kind: chatcontext.SourceKindURL, Name: "notes", URI: "https://example.test/notes", State: chatcontext.SourceStateActive, ExchangeID: "exchange-1"}
	c := &Chat{
		Model: models.Default(),
		Messages: []Message{
			{Role: "user", Content: "question", Sources: []chatcontext.SourceRef{source}},
			{Role: "assistant", Content: "old answer"},
		},
	}
	cfg := &config.Config{GlobalPrompt: "global preamble"}
	var requested string
	deps := retryTestDependencies("replacement answer", nil, func(content string) { requested = content })

	response, err := retryLastAnswerWithDependencies(context.Background(), c, cfg, "be more specific", deps)
	if err != nil {
		t.Fatalf("retryLastAnswerWithDependencies() error = %v", err)
	}
	if response != "replacement answer" {
		t.Fatalf("retry response = %q, want %q", response, "replacement answer")
	}

	wantRequest := "question\n\n[Retry instruction]\nbe more specific"
	if requested != wantRequest {
		t.Fatalf("retry request = %q, want %q", requested, wantRequest)
	}
	if strings.Contains(requested, cfg.GlobalPrompt) {
		t.Fatalf("retry request duplicated the global prompt: %q", requested)
	}
	if len(c.Messages) != 2 || c.Messages[0].Role != "user" || c.Messages[0].Content != wantRequest || !reflect.DeepEqual(c.Messages[0].Sources, []chatcontext.SourceRef{source}) || c.Messages[1].Role != "assistant" || c.Messages[1].Content != "replacement answer" {
		t.Fatalf("messages after retry = %+v, want the original user context and replacement answer", c.Messages)
	}
}

func TestRetryLastAnswerPreservesPendingContextAfterFetchFailure(t *testing.T) {
	chatSource := chatcontext.SourceRef{ID: "source-chat", Kind: chatcontext.SourceKindFile, Name: "attached.txt", State: chatcontext.SourceStateActive, ExchangeID: "exchange-1"}
	pendingSource := chatcontext.SourceRef{ID: "source-pending", Kind: chatcontext.SourceKindURL, Name: "pending", URI: "https://example.test/pending", State: chatcontext.SourceStateActive}
	image := media.ImageAttachment{Name: "pending.png", MIMEType: "image/png", Data: []byte{1, 2, 3}}
	c := &Chat{
		Model:                   models.Default(),
		Messages:                []Message{{Role: "user", Content: "question", Sources: []chatcontext.SourceRef{chatSource}}, {Role: "assistant", Content: "old answer"}},
		pendingImages:           []media.ImageAttachment{image},
		PendingSources:          []chatcontext.SourceRef{pendingSource},
		LastOptimizationChanges: []intelligence.OptimizationChange{{SourceID: "source-chat", State: chatcontext.SourceStateOmitted, BytesBefore: 140, BytesAfter: 0}},
	}
	beforeMessages := cloneChatMessages(c.Messages)
	beforeImages := cloneImageAttachments(c.pendingImages)
	beforeSources := cloneSourceRefs(c.PendingSources)
	beforeChanges := append([]intelligence.OptimizationChange(nil), c.LastOptimizationChanges...)
	wantErr := errors.New("offline")
	deps := retryTestDependencies("", wantErr, nil)

	if _, err := retryLastAnswerWithDependencies(context.Background(), c, &config.Config{}, "", deps); err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("retry error = %v, want an error containing %q", err, wantErr)
	}
	if !reflect.DeepEqual(c.Messages, beforeMessages) || !reflect.DeepEqual(c.pendingImages, beforeImages) || !reflect.DeepEqual(c.PendingSources, beforeSources) || !reflect.DeepEqual(c.LastOptimizationChanges, beforeChanges) {
		t.Fatalf("failed retry changed chat state:\nmessages: got %+v want %+v\nimages: got %+v want %+v\nsources: got %+v want %+v\nchanges: got %+v want %+v", c.Messages, beforeMessages, c.pendingImages, beforeImages, c.PendingSources, beforeSources, c.LastOptimizationChanges, beforeChanges)
	}
}

func TestRetryLastAnswerPreservesMessageOrderAndPendingContextAfterSuccess(t *testing.T) {
	latestSource := chatcontext.SourceRef{ID: "source-latest", Kind: chatcontext.SourceKindFile, Name: "latest.txt", State: chatcontext.SourceStateActive, ExchangeID: "exchange-latest"}
	pendingSource := chatcontext.SourceRef{ID: "source-pending", Kind: chatcontext.SourceKindURL, Name: "pending", URI: "https://example.test/pending", State: chatcontext.SourceStateActive}
	pendingImage := media.ImageAttachment{Name: "pending.png", MIMEType: "image/png", Data: []byte{4, 5, 6}}
	c := &Chat{
		Model: models.Default(),
		Messages: []Message{
			{Role: "user", Content: "first question"},
			{Role: "assistant", Content: "first answer"},
			{Role: "user", Content: "latest question", Sources: []chatcontext.SourceRef{latestSource}},
			{Role: "assistant", Content: "latest old answer"},
		},
		pendingImages:           []media.ImageAttachment{pendingImage},
		PendingSources:          []chatcontext.SourceRef{pendingSource},
		LastOptimizationChanges: []intelligence.OptimizationChange{{SourceID: "stale-source", State: chatcontext.SourceStateOmitted, BytesBefore: 500, BytesAfter: 0}},
	}
	beforeImages := cloneImageAttachments(c.pendingImages)
	beforeSources := cloneSourceRefs(c.PendingSources)
	deps := retryTestDependencies("new latest answer", nil, nil)

	if _, err := retryLastAnswerWithDependencies(context.Background(), c, &config.Config{}, "", deps); err != nil {
		t.Fatalf("retryLastAnswerWithDependencies() error = %v", err)
	}
	if len(c.Messages) != 4 || c.Messages[0].Content != "first question" || c.Messages[1].Content != "first answer" || c.Messages[2].Content != "latest question" || c.Messages[2].Role != "user" || !reflect.DeepEqual(c.Messages[2].Sources, []chatcontext.SourceRef{latestSource}) || c.Messages[3].Content != "new latest answer" || c.Messages[3].Role != "assistant" {
		t.Fatalf("messages after retry = %+v, want previous exchange followed by the replaced latest answer", c.Messages)
	}
	if !reflect.DeepEqual(c.pendingImages, beforeImages) || !reflect.DeepEqual(c.PendingSources, beforeSources) {
		t.Fatalf("successful retry changed pending context: images=%+v sources=%+v", c.pendingImages, c.PendingSources)
	}
	if len(c.LastOptimizationChanges) != 0 {
		t.Fatalf("successful retry retained stale optimization metadata: %+v", c.LastOptimizationChanges)
	}
}

func retryTestDependencies(response string, fetchErr error, onFetch func(string)) retryCommandDependencies {
	return retryCommandDependencies{
		fetch: func(_ context.Context, content string) (<-chan string, <-chan error, error) {
			if onFetch != nil {
				onFetch(content)
			}
			if fetchErr != nil {
				return nil, nil, fetchErr
			}
			stream := make(chan string, 1)
			stream <- response
			close(stream)
			streamErrors := make(chan error)
			close(streamErrors)
			return stream, streamErrors, nil
		},
		render: renderStreamToString,
	}
}
