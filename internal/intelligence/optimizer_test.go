package intelligence

import (
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/media"
)

func TestOptimizerKeepsDistinctImageMessagesAndAttachmentsWhenTruncating(t *testing.T) {
	optimizer := NewContextOptimizer()
	optimizer.MaxContextSize = 1
	messages := []Message{
		{Role: "user", Content: "same", Images: []media.ImageAttachment{{Name: "first.png", MIMEType: "image/png", Data: []byte{1}}}},
		{Role: "user", Content: "same", Images: []media.ImageAttachment{{Name: "second.png", MIMEType: "image/png", Data: []byte{2}}}},
		{Role: "assistant", Content: strings.Repeat("uncompressible", 100)},
	}

	optimized, _ := optimizer.OptimizeContext(messages)
	if len(optimized) != 2 {
		t.Fatalf("optimized message count = %d, want both image messages and no oversized text message: %+v", len(optimized), optimized)
	}
	seen := map[byte]bool{}
	for _, message := range optimized {
		if len(message.Images) != 1 || len(message.Images[0].Data) != 1 {
			t.Fatalf("optimized message lost image bytes: %+v", message)
		}
		seen[message.Images[0].Data[0]] = true
	}
	if !seen[1] || !seen[2] {
		t.Fatalf("optimizer did not retain both distinct images: %+v", optimized)
	}
}
