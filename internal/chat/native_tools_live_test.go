package chat

import (
	"os"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
)

// TestNativeToolsAgainstDuckAI is opt-in because it launches Chrome, consumes
// a real Duck.ai request, and depends on the service's undocumented tool wire
// format. Run it with DUCKAI_LIVE_TEST=1 when validating a release candidate.
func TestNativeToolsAgainstDuckAI(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_TEST=1 to run the live Duck.ai integration test")
	}

	cfg := &config.Config{ExportDir: t.TempDir()}
	chat := NewChat("", "", "", "", models.GPT5Luna, cfg)
	chat.NativeToolsEnabled = true
	chat.NativeWebSearch = true
	chat.Messages = append(chat.Messages, Message{
		Role:    "user",
		Content: "Use web search to identify the current Go release. Include at least one source.",
	})

	events, err := chat.FetchEventStream("live native web search test")
	if err != nil {
		t.Fatalf("live Duck.ai request failed: %v", err)
	}

	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	var sawMessage, sawSource bool
	for {
		select {
		case event, ok := <-events:
			if !ok {
				if !sawMessage {
					t.Fatal("Duck.ai returned no assistant message")
				}
				return
			}
			switch event.Type {
			case "message":
				sawMessage = sawMessage || event.Message != ""
			case "source":
				sawSource = sawSource || event.SourceURL != ""
			case "done":
				if !sawMessage {
					t.Fatal("Duck.ai completed without an assistant message")
				}
				if !sawSource {
					t.Log("Duck.ai answered, but did not emit a source event")
				}
				return
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for Duck.ai native tool response")
		}
	}
}

func TestNativeImageGenerationAgainstDuckAI(t *testing.T) {
	if os.Getenv("DUCKAI_LIVE_TEST") != "1" {
		t.Skip("set DUCKAI_LIVE_TEST=1 to run the live Duck.ai image test")
	}

	cfg := &config.Config{ExportDir: t.TempDir()}
	chat := NewChat("", "", "", "", models.GPT5Luna, cfg)
	chat.NativeToolsEnabled = true
	chat.NativeImageGeneration = true
	chat.Messages = append(chat.Messages, Message{
		Role:    "user",
		Content: "Generate a simple image of a blue circle on a white background.",
	})

	events, err := chat.FetchEventStream("live native image generation test")
	if err != nil {
		t.Fatalf("live Duck.ai image request failed: %v", err)
	}

	deadline := time.NewTimer(120 * time.Second)
	defer deadline.Stop()
	var sawImage bool
	for {
		select {
		case event, ok := <-events:
			if !ok {
				if !sawImage {
					t.Fatal("Duck.ai returned no generated image event")
				}
				return
			}
			if event.Type == "image" {
				if event.ImageBase64 != "" {
					t.Logf("received image data: format=%s width=%d height=%d bytes(base64)=%d", event.ImageFormat, event.ImageWidth, event.ImageHeight, len(event.ImageBase64))
					path, saveErr := chat.saveGeneratedImage(event)
					if saveErr != nil {
						t.Fatalf("save generated image: %v", saveErr)
					}
					info, statErr := os.Stat(path)
					if statErr != nil || info.Size() == 0 {
						t.Fatalf("saved generated image is missing or empty: path=%s err=%v", path, statErr)
					}
					sawImage = true
				} else {
					sawImage = event.ImageURL != ""
				}
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for Duck.ai image generation")
		}
	}
}
