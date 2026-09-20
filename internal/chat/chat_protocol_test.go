package chat

import (
	"os"
	"testing"
)

func TestNewDurableStreamBuildsDuckAIJWK(t *testing.T) {
	stream, err := newDurableStream()
	if err != nil {
		t.Fatalf("newDurableStream() error = %v", err)
	}
	if stream.MessageID == "" || stream.ConversationID == "" {
		t.Fatal("durable stream IDs must not be empty")
	}
	if stream.PublicKey == nil {
		t.Fatal("durable stream public key is nil")
	}
	if stream.PublicKey.Alg != "RSA-OAEP-256" || stream.PublicKey.Kty != "RSA" {
		t.Fatalf("unexpected JWK type: alg=%q kty=%q", stream.PublicKey.Alg, stream.PublicKey.Kty)
	}
	if stream.PublicKey.E != "AQAB" || stream.PublicKey.N == "" {
		t.Fatalf("invalid RSA JWK exponent/modulus: e=%q n-empty=%t", stream.PublicKey.E, stream.PublicKey.N == "")
	}
}

func TestBuildPayloadEnablesConfiguredNativeTools(t *testing.T) {
	durableStream := &DurableStream{MessageID: "message", ConversationID: "conversation"}
	chat := &Chat{
		Model:                 "gpt-5.6-luna",
		Messages:              []Message{{Role: "user", Content: "latest Go release"}},
		NativeToolsEnabled:    true,
		NativeWebSearch:       true,
		NativeImageGeneration: true,
	}

	payload := chat.buildPayload(durableStream)
	if !payload.CanUseTools {
		t.Fatal("native tools should be enabled")
	}
	if payload.Metadata == nil {
		t.Fatal("native tool metadata is missing")
	}
	if !payload.Metadata.ToolChoice.WebSearch || !payload.Metadata.ToolChoice.GenerateImage {
		t.Fatalf("unexpected tool choice: %+v", payload.Metadata.ToolChoice)
	}
	if payload.CanDelegateImageGeneration == nil || !*payload.CanDelegateImageGeneration {
		t.Fatal("image generation delegation should be enabled")
	}
}

func TestBuildPayloadKeepsNativeToolsDisabledByDefault(t *testing.T) {
	payload := (&Chat{Model: "gpt-5.6-luna"}).buildPayload(&DurableStream{})
	if payload.CanUseTools {
		t.Fatal("native tools must remain disabled unless explicitly configured")
	}
	if payload.Metadata != nil {
		t.Fatal("metadata should be omitted when native tools are disabled")
	}
}

func TestParseSSEEventLine(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		typ       string
		message   string
		sourceURL string
		imageURL  string
	}{
		{
			name:    "assistant message",
			line:    `data: {"role":"assistant","message":"hello"}`,
			typ:     "message",
			message: "hello",
		},
		{
			name:      "source citation",
			line:      `data: {"role":"source","source":{"url":"https://example.com","title":"Example"}}`,
			typ:       "source",
			sourceURL: "https://example.com",
		},
		{
			name:     "generated image",
			line:     `data: {"role":"tool-invocation","state":"result","toolName":"GenerateImage","result":"https://cdn.example/image.png"}`,
			typ:      "image",
			imageURL: "https://cdn.example/image.png",
		},
		{
			name: "generated image data",
			line: `data: {"role":"ui-component","state":"data","name":"generate-image","data":{"type":"image-partial","b64Image":"AQID","width":2,"height":2,"format":"png"}}`,
			typ:  "image",
		},
		{
			name: "done marker",
			line: "data: [DONE]",
			typ:  "done",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, ok := parseSSEEventLine(tt.line)
			if !ok {
				t.Fatal("event line was not parsed")
			}
			if event.Type != tt.typ {
				t.Fatalf("event type = %q, want %q", event.Type, tt.typ)
			}
			if event.Message != tt.message {
				t.Fatalf("event message = %q, want %q", event.Message, tt.message)
			}
			if event.SourceURL != tt.sourceURL {
				t.Fatalf("source URL = %q, want %q", event.SourceURL, tt.sourceURL)
			}
			if event.ImageURL != tt.imageURL {
				t.Fatalf("image URL = %q, want %q", event.ImageURL, tt.imageURL)
			}
			if tt.name == "generated image data" && event.ImageBase64 != "AQID" {
				t.Fatalf("image data = %q, want AQID", event.ImageBase64)
			}
		})
	}
}

func TestSaveGeneratedImage(t *testing.T) {
	chat := &Chat{GeneratedImageDir: t.TempDir()}
	path, err := chat.saveGeneratedImage(StreamEvent{ImageBase64: "AQID", ImageFormat: "png"})
	if err != nil {
		t.Fatalf("saveGeneratedImage() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading saved image: %v", err)
	}
	if string(data) != string([]byte{1, 2, 3}) {
		t.Fatalf("saved image bytes = %v, want [1 2 3]", data)
	}
}

func TestFetchStreamFormatsNativeSources(t *testing.T) {
	event := StreamEvent{Type: "source", SourceTitle: "Example", SourceURL: "https://example.com"}
	formatted := formatSourceEvent(event)
	if formatted != "\n\nSource: [Example](https://example.com)\n" {
		t.Fatal("source formatting lost the citation URL")
	}
}
