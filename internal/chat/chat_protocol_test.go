package chat

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"

	"github.com/fatih/color"
)

func TestMessageMarshalJSONKeepsTextContentString(t *testing.T) {
	data, err := json.Marshal(Message{Role: "user", Content: "describe this"})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	var content string
	if err := json.Unmarshal(got["content"], &content); err != nil {
		t.Fatalf("text-only content was not a JSON string: %s", got["content"])
	}
	if content != "describe this" {
		t.Fatalf("content = %q, want %q", content, "describe this")
	}
}

func TestMessageMarshalJSONUsesDuckAIMultimodalBlocks(t *testing.T) {
	first := []byte{1, 2, 3}
	second := []byte{4, 5}
	data, err := json.Marshal(Message{
		Role:    "user",
		Content: "What is in these images?",
		Images: []media.ImageAttachment{
			{Name: "logo.png", MIMEType: "image/png", Data: first},
			{Name: "other.webp", MIMEType: "image/webp", Data: second},
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got struct {
		Content []map[string]string `json:"content"`
		Role    string              `json:"role"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Role != "user" || len(got.Content) != 3 {
		t.Fatalf("role/content = %q/%d, want user/3 blocks", got.Role, len(got.Content))
	}
	if got.Content[0]["type"] != "text" || got.Content[0]["text"] != "What is in these images?" {
		t.Fatalf("first block = %#v, want prompt text block", got.Content[0])
	}
	for i, want := range []struct {
		mime string
		data []byte
	}{{"image/png", first}, {"image/webp", second}} {
		block := got.Content[i+1]
		if len(block) != 3 || block["type"] != "image" || block["mimeType"] != want.mime {
			t.Fatalf("image block %d = %#v, want only type/mimeType/image fields", i, block)
		}
		prefix := "data:" + want.mime + ";base64,"
		if !strings.HasPrefix(block["image"], prefix) {
			t.Fatalf("image block %d data URI = %q, want prefix %q", i, block["image"], prefix)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(block["image"], prefix))
		if err != nil || string(decoded) != string(want.data) {
			t.Fatalf("image block %d did not retain original bytes: decoded=%v err=%v", i, decoded, err)
		}
	}
}

func TestRestoreContextPreservesImageAttachmentsThroughOptimization(t *testing.T) {
	original := intelligence.Message{Role: "user", Content: "What is in this picture?", Images: []media.ImageAttachment{{Name: "logo.png", MIMEType: "image/png", Data: []byte{1, 2, 3}}}}
	chat := &Chat{}
	chat.RestoreContext(&persistence.ConversationSession{ID: "session-1", Model: "gpt-5.6-luna", Messages: []intelligence.Message{original}})
	if len(chat.Messages) != 1 || len(chat.Messages[0].Images) != 1 {
		t.Fatalf("RestoreContext() messages = %+v, want restored image attachment", chat.Messages)
	}
	converted := chat.convertMessagesToIntelligence()
	optimizer := intelligence.NewContextOptimizer()
	optimized, _ := optimizer.OptimizeContext(converted)
	restored := chat.convertFromIntelligenceMessages(optimized)
	if len(restored) != 1 || len(restored[0].Images) != 1 {
		t.Fatalf("restored messages = %+v, want one image-bearing message", restored)
	}
	got := restored[0].Images[0]
	if got.Name != original.Images[0].Name || got.MIMEType != original.Images[0].MIMEType || string(got.Data) != string(original.Images[0].Data) {
		t.Fatalf("restored attachment = %+v, want original metadata and bytes", got)
	}
	wire, err := json.Marshal(restored[0])
	if err != nil {
		t.Fatalf("marshal restored outgoing message: %v", err)
	}
	if !strings.Contains(string(wire), "data:image/png;base64,AQID") {
		t.Fatalf("restored outgoing payload lost image bytes: %s", wire)
	}
}

func TestHistoryAndExportShowImageMarkersOnly(t *testing.T) {
	chat := &Chat{Model: "gpt-5.6-luna", Messages: []Message{{
		Role: "user", Content: "What is in this picture?", Images: []media.ImageAttachment{{Name: "logo.png", MIMEType: "image/png", Data: []byte("private-image-bytes")}},
	}, {Role: "assistant", Content: "A duck."}}}
	const marker = "[Image attachment: logo.png (image/png)]"
	markdown := chat.GetMarkdownContent()
	if !strings.Contains(markdown, marker) || strings.Contains(markdown, "private-image-bytes") {
		t.Fatalf("Markdown export does not safely identify attachment: %s", markdown)
	}
	_, searchExport := chat.Export("search_conversation", "picture")
	if !strings.Contains(searchExport, marker) || strings.Contains(searchExport, "private-image-bytes") {
		t.Fatalf("search export does not safely identify attachment: %s", searchExport)
	}

	previousStdout := os.Stdout
	previousColorOutput := color.Output
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	color.Output = writer
	PrintHistory(chat)
	_ = writer.Close()
	os.Stdout = previousStdout
	color.Output = previousColorOutput
	printed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	_ = reader.Close()
	if !strings.Contains(string(printed), marker) || strings.Contains(string(printed), "private-image-bytes") {
		t.Fatalf("printed history does not safely identify attachment: %s", printed)
	}
}

func TestSearchExportIncludesFinalImageContextWithoutAssistantReply(t *testing.T) {
	chat := &Chat{Messages: []Message{{
		Role: "user", Content: "What is in this picture?", Images: []media.ImageAttachment{{Name: "logo.png", MIMEType: "image/png", Data: []byte("private-image-bytes")}},
	}}}
	_, exported := chat.Export("search_conversation", "picture")
	if !strings.Contains(exported, "What is in this picture?") || !strings.Contains(exported, "[Image attachment: logo.png (image/png)]") {
		t.Fatalf("search export omitted final image context: %s", exported)
	}
	if strings.Contains(exported, "private-image-bytes") {
		t.Fatalf("search export exposed image bytes: %s", exported)
	}
}

func TestDebugPayloadRedactsImageBytes(t *testing.T) {
	payload := (&Chat{
		Model: "gpt-5.6-luna",
		Messages: []Message{{Role: "user", Content: "describe", Images: []media.ImageAttachment{{
			Name: "logo.png", MIMEType: "image/png", Data: []byte("private-image-bytes"),
		}}}},
	}).buildPayload(&DurableStream{MessageID: "message", ConversationID: "conversation"})
	debugPayload, err := marshalDebugPayload(payload)
	if err != nil {
		t.Fatalf("marshalDebugPayload() error = %v", err)
	}
	logged := string(debugPayload)
	if strings.Contains(logged, "private-image-bytes") || strings.Contains(logged, "cHJpdmF0ZS1pbWFnZS1ieXRlcw==") || strings.Contains(logged, "data:image/") {
		t.Fatalf("debug payload exposed image data: %s", logged)
	}
	if !strings.Contains(logged, "logo.png") || !strings.Contains(logged, "image/png") {
		t.Fatalf("debug payload omitted safe image metadata: %s", logged)
	}
}

func TestClearDiscardsPendingImageContext(t *testing.T) {
	chat := &Chat{pendingImages: []media.ImageAttachment{{Name: "logo.png", MIMEType: "image/png", Data: []byte{1, 2, 3}}}}
	chat.Clear(&config.Config{})
	if len(chat.pendingImages) != 0 {
		t.Fatalf("Clear() retained %d pending images", len(chat.pendingImages))
	}
}

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

func TestBuildPayloadUsesLowReasoningForTinfoilModels(t *testing.T) {
	for _, model := range []models.Model{models.GPTOSS120B, models.Gemma431B} {
		payload := (&Chat{Model: model}).buildPayload(&DurableStream{})
		if payload.ReasoningEffort != "low" {
			t.Errorf("buildPayload(%q).ReasoningEffort = %q, want low", model, payload.ReasoningEffort)
		}
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
