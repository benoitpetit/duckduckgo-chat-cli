package chat

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
	"duckduckgo-chat-cli/internal/scrape"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
)

type Chat struct {
	OldVqd     string
	NewVqd     string
	VqdHash1   string // x-vqd-hash-1 header (full VQD hash)
	FeSignals  string // x-fe-signals header
	FeVersion  string // x-fe-version header
	Model      models.Model
	Messages   []Message
	Client     *http.Client
	CookieJar  *cookiejar.Jar
	LastHash   string
	RetryCount int

	// New intelligent features
	Analytics        *analytics.ChatAnalytics
	ContextOptimizer *intelligence.ContextOptimizer
	HistoryManager   *persistence.HistoryManager
	SessionID        string

	// Native Duck.ai tools are opt-in because their wire protocol is not
	// public API and may change independently of the chat endpoint.
	NativeToolsEnabled    bool
	NativeWebSearch       bool
	NativeImageGeneration bool
	GeneratedImageDir     string
}

type Message struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type ToolChoice struct {
	WebSearch       bool `json:"WebSearch,omitempty"`
	GenerateImage   bool `json:"GenerateImage,omitempty"`
	NewsSearch      bool `json:"NewsSearch"`
	VideosSearch    bool `json:"VideosSearch"`
	LocalSearch     bool `json:"LocalSearch"`
	WeatherForecast bool `json:"WeatherForecast"`
}

type Metadata struct {
	ToolChoice ToolChoice `json:"toolChoice"`
}

type DurableStream struct {
	MessageID      string        `json:"messageId"`
	ConversationID string        `json:"conversationId"`
	PublicKey      *JWKPublicKey `json:"publicKey"`
}

type JWKPublicKey struct {
	Alg    string   `json:"alg"`
	E      string   `json:"e"`
	Ext    bool     `json:"ext"`
	KeyOps []string `json:"key_ops"`
	Kty    string   `json:"kty"`
	N      string   `json:"n"`
	Use    string   `json:"use"`
}

type ChatPayload struct {
	Model                      models.Model   `json:"model"`
	Metadata                   *Metadata      `json:"metadata,omitempty"`
	Messages                   []Message      `json:"messages"`
	CanUseTools                bool           `json:"canUseTools"`
	CanUseApproxLocation       bool           `json:"canUseApproxLocation"`
	CanDelegateImageGeneration *bool          `json:"canDelegateImageGeneration,omitempty"`
	ReasoningEffort            string         `json:"reasoningEffort"`
	DurableStream              *DurableStream `json:"durableStream"`
}

// StreamEvent is a normalized event from Duck.ai's SSE response.
// Message events contain assistant text, source events contain web citations,
// and tool events expose native tool activity without leaking protocol JSON to
// the terminal renderer.
type StreamEvent struct {
	Type        string
	Message     string
	ToolName    string
	ToolCallID  string
	ToolArgs    string
	ToolResult  string
	SourceURL   string
	SourceTitle string
	ImageURL    string
	ImageBase64 string
	ImageFormat string
	ImageWidth  int
	ImageHeight int
	Raw         string
}

func InitializeSession(cfg *config.Config) *Chat {
	model := models.GetModel(cfg.DefaultModel)
	chat := NewChat("", "", "", "", model, cfg)
	ui.AIln("Chat initialized with model: %s", model)
	setTerminalTitle(fmt.Sprintf("DuckDuckGo Chat - %s", model))
	return chat
}

func setTerminalTitle(title string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("cmd", "/c", fmt.Sprintf("title %s", title)).Run()
	default:
		fmt.Printf("\033]0;%s\007", title)
	}
}

func NewChat(vqd, vqdHash1, feSignals, feVersion string, model models.Model, cfg *config.Config) *Chat {
	jar, _ := cookiejar.New(nil)

	// Set required cookies avec les cookies minimum nécessaires
	u, _ := url.Parse("https://duck.ai")
	cookies := []*http.Cookie{
		{Name: "5", Value: "1", Domain: ".duck.ai"},
		{Name: "dcm", Value: "3", Domain: ".duck.ai"},
		{Name: "dcs", Value: "1", Domain: ".duck.ai"},
	}
	jar.SetCookies(u, cookies)

	// Generate unique session ID
	sessionID := fmt.Sprintf("session_%d", time.Now().UnixNano())

	// Initialize intelligent features
	analytics := analytics.NewChatAnalytics()
	contextOptimizer := intelligence.NewContextOptimizer()
	historyManager := persistence.NewHistoryManager(cfg.ExportDir)

	// Use all headers like the real web browser
	ui.AIln("🔍 Using VQD with all required headers like web browser")

	chat := &Chat{
		OldVqd:     vqd,       // x-vqd-4 value
		NewVqd:     vqd,       // x-vqd-4 value
		VqdHash1:   vqdHash1,  // x-vqd-hash-1 value
		FeSignals:  feSignals, // x-fe-signals value
		FeVersion:  feVersion, // x-fe-version value
		Model:      model,
		Messages:   []Message{},
		CookieJar:  jar,
		Client:     &http.Client{Timeout: 30 * time.Second, Jar: jar},
		RetryCount: 0,

		// Initialize new intelligent features
		Analytics:        analytics,
		ContextOptimizer: contextOptimizer,
		HistoryManager:   historyManager,
		SessionID:        sessionID,

		NativeToolsEnabled:    cfg.Tools.Enabled,
		NativeWebSearch:       cfg.Tools.WebSearch,
		NativeImageGeneration: cfg.Tools.ImageGeneration,
		GeneratedImageDir:     filepath.Join(cfg.ExportDir, "images"),
	}

	// Record initial model
	analytics.RecordModelChange(string(model))

	ui.AIln("🧠 Intelligent features enabled: Analytics, Context Optimization, History Management")

	return chat
}

func newDurableStream() (*DurableStream, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	messageID := randomRequestID()
	conversationID := randomRequestID()
	publicKey := &privateKey.PublicKey
	return &DurableStream{
		MessageID:      messageID,
		ConversationID: conversationID,
		PublicKey: &JWKPublicKey{
			Alg:    "RSA-OAEP-256",
			E:      base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
			Ext:    true,
			KeyOps: []string{"encrypt"},
			Kty:    "RSA",
			N:      base64.RawURLEncoding.EncodeToString(publicKey.N.Bytes()),
			Use:    "enc",
		},
	}, nil
}

func randomRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func GetVQD() (string, string, string, string) {
	// Kept for compatibility with older callers; current Duck.ai proof is
	// captured from the live frontend rather than read from /status directly.
	headers, err := getCurrentDuckAIHeaders()
	if err != nil {
		ui.Errorln("Error getting Duck.ai chat headers: %v", err)
		return "", "", "", ""
	}
	return "", headers.VqdHash1, headers.FeSignals, headers.FeVersion

}

func (c *Chat) Clear(cfg *config.Config) {
	// Save current session before clearing if it has content
	if len(c.Messages) > 0 {
		c.saveCurrentSession()
	}

	clearTerminal()

	if len(c.Messages) > 0 {
		c.Messages = []Message{}
		newHeaders, err := getCurrentDuckAIHeaders()
		if err != nil {
			ui.Errorln("Error refreshing Duck.ai chat proof: %v", err)
			return
		}
		c.NewVqd = ""
		c.OldVqd = ""
		c.VqdHash1 = newHeaders.VqdHash1
		c.FeSignals = newHeaders.FeSignals
		c.FeVersion = newHeaders.FeVersion
		// Hash will be refreshed on next request if needed
		c.RetryCount = 0

		// Generate new session ID for the fresh start
		c.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())

		ui.AIln("Chat history and context cleared")
	} else {
		ui.Warningln("Chat is already empty")
	}

	if cfg.ShowMenu {
		PrintWelcomeMessage()
	} else {
		PrintCommands()
	}
}

func clearTerminal() {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "cls")
	default:
		cmd = exec.Command("clear")
	}
	cmd.Stdout = color.Output
	cmd.Run()
}

func ProcessInput(c *Chat, input string, cfg *config.Config) {
	if strings.TrimSpace(input) == "" {
		return
	}

	// Track user message
	c.Analytics.RecordMessage("user", len(input))

	isFirstMessage := len(c.Messages) == 0
	actualMessage := input
	if isFirstMessage && cfg.GlobalPrompt != "" {
		actualMessage = cfg.GlobalPrompt + "\n\n" + input
	}

	c.Messages = append(c.Messages, Message{
		Role:    "user",
		Content: actualMessage,
	})

	// Check if context optimization is needed
	if c.ContextOptimizer.IsOptimizationNeeded(c.convertMessagesToIntelligence()) {
		optimizedMessages, bytesSaved := c.ContextOptimizer.OptimizeContext(c.convertMessagesToIntelligence())
		c.Messages = c.convertFromIntelligenceMessages(optimizedMessages)
		c.Analytics.RecordContextOptimization(bytesSaved)
	}

	// Track chat interaction timing
	startTime := time.Now()
	stream, err := c.FetchStream(actualMessage)
	if err != nil {
		c.Analytics.RecordChatInteraction(time.Since(startTime), false, "unknown")
		ui.Errorln("Error: %v", err)
		return
	}

	// Use the new stable streaming renderer
	modelName := shortenModelName(string(c.Model))
	finalResponse := RenderStream(stream, modelName)

	// Track successful chat interaction
	c.Analytics.RecordChatInteraction(time.Since(startTime), true, "")

	// Track assistant message
	c.Analytics.RecordMessage("assistant", len(finalResponse))

	c.Messages = append(c.Messages, Message{
		Role:    "assistant",
		Content: finalResponse,
	})
}

// renderStreamToString captures the stream output into a single string.
func renderStreamToString(stream <-chan string) string {
	var fullResponse strings.Builder
	var currentLine strings.Builder
	inCodeBlock := false
	var codeBlockLang string

	for chunk := range stream {
		for _, char := range chunk {
			currentLine.WriteRune(char)
			// This is a simplified version of RenderStream.
			// It doesn't handle complex ANSI and formatting, just captures the text.
			if char == '\n' {
				lineStr := currentLine.String()
				if strings.HasPrefix(lineStr, "```") {
					if !inCodeBlock {
						inCodeBlock = true
						codeBlockLang = strings.TrimSpace(strings.TrimPrefix(lineStr, "```"))
						fullResponse.WriteString("```" + codeBlockLang + "\n")
					} else {
						inCodeBlock = false
						fullResponse.WriteString("```\n")
					}
				} else {
					fullResponse.WriteString(lineStr)
				}
				currentLine.Reset()
			}
		}
	}

	if currentLine.Len() > 0 {
		fullResponse.WriteString(currentLine.String())
	}

	return fullResponse.String()
}

func ProcessInputAndReturn(c *Chat, input string, cfg *config.Config) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}

	// Check if this is the first message and if a GlobalPrompt is defined
	isFirstMessage := len(c.Messages) == 0

	// If it's the first message, combine GlobalPrompt and user message
	actualMessage := input
	if isFirstMessage && cfg.GlobalPrompt != "" {
		actualMessage = cfg.GlobalPrompt + "\n\n" + input
	}

	c.Messages = append(c.Messages, Message{
		Role:    "user",
		Content: actualMessage,
	})

	stream, err := c.FetchStream(actualMessage)
	if err != nil {
		return "", fmt.Errorf("error fetching stream: %w", err)
	}

	// Capture the entire response from the stream
	finalResponse := renderStreamToString(stream)

	// Add the assistant's response to the message history
	c.Messages = append(c.Messages, Message{
		Role:    "assistant",
		Content: finalResponse,
	})

	return finalResponse, nil
}

func shortenModelName(model string) string {
	displayNames := map[string]models.ModelAlias{
		"gpt-5.6-luna":     "gpt-5.6-luna",
		"gpt-5.4-mini":     "gpt-5.4-mini",
		"claude-haiku-4-5": "claude-haiku-4-5",
		"mistral-small-4":  "mistral-small-4",
		"gpt-oss-120B":     "gpt-oss-120b",
		"gemma-4-31B":      "gemma-4-31b",
	}

	if shortName, exists := displayNames[model]; exists {
		return string(shortName)
	}

	return "unknown"
}

func (c *Chat) FetchStream(content string) (<-chan string, error) {
	events, err := c.FetchEventStream(content)
	if err != nil {
		return nil, err
	}

	stream := make(chan string)
	go func() {
		defer close(stream)
		var latestImage *StreamEvent
		for event := range events {
			switch event.Type {
			case "message":
				if event.Message != "" {
					stream <- event.Message
				}
			case "source":
				if event.SourceURL != "" {
					stream <- formatSourceEvent(event)
				}
			case "image":
				if event.ImageBase64 != "" {
					imageCopy := event
					latestImage = &imageCopy
				} else if event.ImageURL != "" {
					stream <- fmt.Sprintf("\n\nImage generated: %s\n", event.ImageURL)
				}
			}
		}
		if latestImage != nil {
			path, err := c.saveGeneratedImage(*latestImage)
			if err != nil {
				stream <- fmt.Sprintf("\n\nImage generated but could not be saved: %v\n", err)
			} else {
				stream <- fmt.Sprintf("\n\nImage generated: %s\n", path)
			}
		}
	}()

	return stream, nil
}

func formatSourceEvent(event StreamEvent) string {
	title := event.SourceTitle
	if title == "" {
		title = event.SourceURL
	}
	return fmt.Sprintf("\n\nSource: [%s](%s)\n", title, event.SourceURL)
}

// FetchEventStream returns normalized Duck.ai SSE events. It is kept separate
// from FetchStream so callers that need citations or generated image URLs can
// consume structured events instead of scraping rendered text.
func (c *Chat) FetchEventStream(content string) (<-chan StreamEvent, error) {
	resp, err := c.Fetch(content)
	if err != nil {
		return nil, err
	}

	stream := make(chan StreamEvent)
	go func() {
		defer resp.Body.Close()
		defer close(stream)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			event, ok := parseSSEEventLine(scanner.Text())
			if !ok {
				continue
			}
			stream <- event
			if event.Type == "done" {
				break
			}
		}

		if err := scanner.Err(); err != nil {
			log.Printf("Error reading response body: %v\n", err)
		}

		if newVqd := resp.Header.Get("x-vqd-4"); newVqd != "" {
			c.OldVqd = c.NewVqd
			c.NewVqd = newVqd
		}
		c.RetryCount = 0
	}()

	return stream, nil
}

func parseSSEEventLine(line string) (StreamEvent, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return StreamEvent{}, false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" {
		return StreamEvent{}, false
	}
	if payload == "[DONE]" {
		return StreamEvent{Type: "done"}, true
	}
	if payload == "[PING]" {
		return StreamEvent{Type: "ping"}, true
	}
	if strings.HasPrefix(payload, "[CHAT_TITLE:") {
		return StreamEvent{Type: "title", Message: strings.TrimSuffix(strings.TrimPrefix(payload, "[CHAT_TITLE:"), "]")}, true
	}

	var message struct {
		Role          string          `json:"role"`
		Name          string          `json:"name"`
		Message       string          `json:"message"`
		State         string          `json:"state"`
		ToolName      string          `json:"toolName"`
		ToolCallID    string          `json:"toolCallId"`
		ToolArguments json.RawMessage `json:"toolArguments"`
		Result        json.RawMessage `json:"result"`
		Source        *struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		} `json:"source"`
		ImageURL    string          `json:"imageUrl"`
		ImageURLAlt string          `json:"image_url"`
		URL         string          `json:"url"`
		Data        json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &message); err != nil {
		return StreamEvent{Type: "raw", Raw: payload}, true
	}

	if message.Role == "assistant" && message.Message != "" {
		return StreamEvent{Type: "message", Message: message.Message}, true
	}
	if message.Role == "source" && message.Source != nil {
		return StreamEvent{Type: "source", SourceURL: message.Source.URL, SourceTitle: message.Source.Title}, true
	}
	if message.Role == "tool-invocation" {
		event := StreamEvent{Type: "tool", ToolName: message.ToolName, ToolCallID: message.ToolCallID, Raw: payload}
		if len(message.ToolArguments) > 0 && string(message.ToolArguments) != "null" {
			event.ToolArgs = string(message.ToolArguments)
		}
		if len(message.Result) > 0 && string(message.Result) != "null" {
			event.ToolResult = string(message.Result)
		}
		if strings.Contains(strings.ToLower(message.ToolName), "image") {
			event.Type = "image"
			event.ImageURL = firstImageURL(message.ImageURL, message.ImageURLAlt, message.URL, string(message.Result), string(message.Data))
		}
		return event, true
	}
	if message.Role == "ui-component" && strings.EqualFold(message.Name, "generate-image") {
		var imageData struct {
			Type                  string `json:"type"`
			B64Image              string `json:"b64Image"`
			Width                 int    `json:"width"`
			Height                int    `json:"height"`
			Format                string `json:"format"`
			ImageModelDisplayName string `json:"imageModelDisplayName"`
		}
		if err := json.Unmarshal(message.Data, &imageData); err == nil && imageData.B64Image != "" {
			return StreamEvent{
				Type:        "image",
				ImageBase64: imageData.B64Image,
				ImageFormat: imageData.Format,
				ImageWidth:  imageData.Width,
				ImageHeight: imageData.Height,
				Raw:         payload,
			}, true
		}
		return StreamEvent{Type: "meta", Raw: payload}, true
	}
	if message.ImageURL != "" || message.ImageURLAlt != "" {
		return StreamEvent{Type: "image", ImageURL: firstImageURL(message.ImageURL, message.ImageURLAlt)}, true
	}

	return StreamEvent{Type: "meta", Raw: payload}, true
}

func (c *Chat) saveGeneratedImage(event StreamEvent) (string, error) {
	if event.ImageBase64 == "" {
		return "", fmt.Errorf("image event did not contain image data")
	}
	data, err := base64.StdEncoding.DecodeString(event.ImageBase64)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(event.ImageBase64)
		if err != nil {
			return "", fmt.Errorf("decode generated image: %w", err)
		}
	}

	dir := c.GeneratedImageDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "duckduckgo-chat-cli", "images")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create image directory: %w", err)
	}
	ext := strings.ToLower(strings.TrimSpace(event.ImageFormat))
	if ext == "" {
		ext = "jpeg"
	}
	if ext == "jpg" {
		ext = "jpeg"
	}
	path := filepath.Join(dir, fmt.Sprintf("duckai-%d.%s", time.Now().UnixNano(), ext))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write generated image: %w", err)
	}
	return path, nil
}

func firstImageURL(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, `"`) {
			var decoded string
			if err := json.Unmarshal([]byte(value), &decoded); err == nil {
				value = strings.TrimSpace(decoded)
			}
		}
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			return value
		}
	}
	return ""
}

func (c *Chat) Fetch(content string) (*http.Response, error) {
	startTime := time.Now()
	// Duck.ai's proof is generated by its frontend and must be captured from
	// a real browser request. It is rotated frequently, so refresh it for
	// every chat request instead of reusing the old DuckDuckGo token.
	headers, err := getCurrentDuckAIHeaders()
	if err != nil {
		return nil, err
	}
	c.NewVqd = ""
	c.VqdHash1 = headers.VqdHash1
	c.FeSignals = headers.FeSignals
	c.FeVersion = headers.FeVersion
	if c.VqdHash1 == "" {
		return nil, fmt.Errorf("Duck.ai returned an empty X-Vqd-Hash-1 proof")
	}

	durableStream, err := newDurableStream()
	if err != nil {
		return nil, fmt.Errorf("failed to create Duck.ai durable stream: %w", err)
	}

	payload := c.buildPayload(durableStream)

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling payload: %v", err)
	}

	if os.Getenv("DEBUG") == "true" {
		color.Cyan("Payload: %s", string(jsonPayload))
	}

	req, err := http.NewRequest("POST", models.ChatURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	// Set ALL required headers EXACTLY like the real web browser request
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://duck.ai")
	req.Header.Set("Referer", "https://duck.ai/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36")

	// Current Duck.ai request headers.
	if c.FeSignals != "" {
		req.Header.Set("x-fe-signals", c.FeSignals)
	}
	if c.FeVersion != "" {
		req.Header.Set("x-fe-version", c.FeVersion)
	}
	if c.VqdHash1 != "" {
		req.Header.Set("X-Vqd-Hash-1", c.VqdHash1)
	}
	req.Header.Set("x-ddg-journey-id", durableStream.ConversationID)

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if os.Getenv("DEBUG") == "true" {
			color.Red("Request Headers: %+v", req.Header)
			color.Red("Response Headers: %+v", resp.Header)
			color.Red("Response Status: %d", resp.StatusCode)
			color.Red("Response Body: %s", string(body))
		}

		// Handle various error conditions including 418 (I'm a teapot)
		bodyText := string(body)
		if resp.StatusCode == 400 || resp.StatusCode == 418 || resp.StatusCode == 429 || strings.Contains(bodyText, "ERR_INVALID_VQD") || strings.Contains(bodyText, "ERR_CHALLENGE") {
			// Track specific error types
			errorType := "unknown"
			switch resp.StatusCode {
			case 418:
				errorType = "418"
			case 429:
				errorType = "429"
			}
			c.Analytics.RecordChatInteraction(time.Since(startTime), false, errorType)

			time.Sleep(2 * time.Second)

			// Refresh ONLY VQD on errors, like the PowerShell script
			ui.Warningln("🔄 Error %d detected, refreshing VQD...", resp.StatusCode)
			newHeaders, refreshErr := getCurrentDuckAIHeaders()
			if refreshErr == nil && newHeaders.VqdHash1 != "" {
				c.NewVqd = ""
				c.VqdHash1 = newHeaders.VqdHash1
				c.FeSignals = newHeaders.FeSignals
				c.FeVersion = newHeaders.FeVersion
				ui.AIln("✅ Refreshed Duck.ai chat proof")
			}
			c.Analytics.RecordVQDRefresh()

			if c.RetryCount < 2 {
				c.RetryCount++
				ui.Warningln("Retrying request (attempt %d/3)...", c.RetryCount)
				return c.Fetch(content)
			}
		}
		return nil, fmt.Errorf("%d: Failed to send message. %s. Body: %s", resp.StatusCode, resp.Status, string(body))
	}

	newVqd := resp.Header.Get("x-vqd-4")
	if newVqd != "" {
		c.OldVqd = c.NewVqd
		c.NewVqd = newVqd
	}

	return resp, nil
}

func (c *Chat) buildPayload(durableStream *DurableStream) ChatPayload {
	payload := ChatPayload{
		Model:                c.Model,
		Messages:             c.Messages,
		CanUseTools:          c.NativeToolsEnabled && (c.NativeWebSearch || c.NativeImageGeneration),
		CanUseApproxLocation: true,
		ReasoningEffort:      "none",
		DurableStream:        durableStream,
	}
	if payload.CanUseTools {
		payload.Metadata = &Metadata{ToolChoice: ToolChoice{
			WebSearch:     c.NativeWebSearch,
			GenerateImage: c.NativeImageGeneration,
		}}
		if c.NativeImageGeneration {
			canDelegate := true
			payload.CanDelegateImageGeneration = &canDelegate
		}
	}
	return payload
}

func (c *Chat) SetNativeTools(enabled, webSearch, imageGeneration bool) {
	c.NativeToolsEnabled = enabled
	c.NativeWebSearch = webSearch
	c.NativeImageGeneration = imageGeneration
}

func (c *Chat) AddURLContext(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	ui.Warningln("⌛ Retrieving webpage content...")
	content, err := scrape.WebContent(url)
	if err != nil {
		return err
	}

	contentLength := len(content.Content)
	if contentLength > 500 {
		ui.AIln("Retrieved %d characters of content", contentLength)
	}

	c.Messages = append(c.Messages, Message{
		Role:    "user",
		Content: fmt.Sprintf("[URL Context]\nURL: %s\n\n%s", url, content.Content),
	})

	c.Analytics.RecordURLProcessed()

	return nil
}

func PrintCommands() {
	ui.Warningln("Type /help to show these commands again")
}

// CommandHelp holds information about a CLI command for the help message.
type CommandHelp struct {
	Command     string
	Description string
}

func PrintWelcomeMessage() {
	ui.Systemln("\nDuckDuckGo AI Chat CLI - Help")
	ui.Mutedln("---------------------------------")

	// Get commands from centralized registry
	commandsByCategory := command.GetCommandsByCategory()

	// Core commands
	coreCommands := []CommandHelp{}
	for _, cmd := range commandsByCategory["core"] {
		coreCommands = append(coreCommands, CommandHelp{
			Command:     cmd.Name,
			Description: cmd.Description,
		})
	}

	// Context commands
	contextCommands := []CommandHelp{}
	for _, cmd := range commandsByCategory["context"] {
		usage := cmd.Usage
		if usage == cmd.Name {
			usage = cmd.Name // Use simple name if no special usage
		}
		contextCommands = append(contextCommands, CommandHelp{
			Command:     usage,
			Description: cmd.Description,
		})
	}

	// Productivity commands
	productivityCommands := []CommandHelp{}
	for _, cmd := range commandsByCategory["productivity"] {
		productivityCommands = append(productivityCommands, CommandHelp{
			Command:     cmd.Name,
			Description: cmd.Description,
		})
	}

	// API documentation (static)
	apiCommands := []CommandHelp{
		{"GET /", "Shows API documentation"},
		{"POST /chat", "Sends a message to the chat"},
		{"GET /history", "Retrieves the chat session history"},
	}

	ui.AIln("\nCore Commands:")
	printCommandsTable(coreCommands)

	ui.AIln("\nContext Commands:")
	printCommandsTable(contextCommands)

	ui.AIln("\nProductivity Commands:")
	printCommandsTable(productivityCommands)

	ui.AIln("\nAPI Documentation:")
	printCommandsTable(apiCommands)

	ui.Warningln("\nNote: You can add '-- <your request>' after /search, /file, or /url to make an immediate request about the context.")
}

// printCommandsTable formats and prints a list of commands.
func printCommandsTable(commands []CommandHelp) {
	// Find the longest command to align descriptions
	maxLength := 0
	for _, cmd := range commands {
		if len(cmd.Command) > maxLength {
			maxLength = len(cmd.Command)
		}
	}

	for _, cmd := range commands {
		// Use UserColor for the command and default white for the description
		ui.UserColor.Printf("  %-*s", maxLength+4, cmd.Command)
		ui.Whiteln("- %s", cmd.Description)
	}
}

func HandleURLCommand(c *Chat, input string, cfg *config.Config, chainCtx *chatcontext.Context) {
	urlStr := strings.TrimSpace(strings.TrimPrefix(input, "/url"))
	if urlStr == "" {
		ui.Errorln("URL cannot be empty.")
		return
	}

	result, err := scrape.WebContent(urlStr)
	if err != nil {
		ui.Errorln("URL error: %v", err)
		return
	}

	if chainCtx != nil {
		chainCtx.AddURL(urlStr, result.Content)
		ui.AIln("Successfully added content from URL to chain context: %s", urlStr)
	} else {
		ui.Warningln("Adding URL content: %s", urlStr)
		c.addURLContext(urlStr, result.Content)
		ui.AIln("Successfully added content from URL: %s", urlStr)
		ui.Warningln("You can now ask questions about the URL content.")
	}
}

func (c *Chat) addURLContext(url string, content string) {
	contentLength := len(content)
	if contentLength > 500 {
		ui.AIln("Adding %d characters from URL", contentLength)
	}

	c.Messages = append(c.Messages, Message{
		Role:    "user",
		Content: fmt.Sprintf("[URL Context]\nURL: %s\n\n%s", url, content),
	})
}

func HandleExportCommand(c *Chat, cfg *config.Config) {
	var choice string
	prompt := &survey.Select{
		Message: "Choose what to export:",
		Options: []string{
			"Full conversation",
			"Last AI response",
			"Largest code block",
			"Search in conversation",
			"Cancel",
		},
		Default: "Full conversation",
	}
	err := survey.AskOne(prompt, &choice, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
	if err != nil {
		ui.Warningln("\nExport canceled.")
		return
	}

	var filename, content string

	switch choice {
	case "Full conversation":
		filename, content = c.Export("conversation", "")
	case "Last AI response":
		filename, content = c.Export("last_response", "")
	case "Largest code block":
		filename, content = c.Export("code_block", "")
	case "Search in conversation":
		var searchText string
		searchPrompt := &survey.Input{Message: "Enter text to search for:"}
		survey.AskOne(searchPrompt, &searchText, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
		if searchText == "" {
			ui.Warningln("⚠️ Search text cannot be empty")
			return
		}
		filename, content = c.Export("search_conversation", searchText)
	default:
		ui.Warningln("💡 Export canceled.")
		return
	}

	if filename == "" || content == "" {
		ui.Errorln("❌ Nothing to export")
		return
	}

	fullPath := filepath.Join(cfg.ExportDir, filename)
	if err := os.MkdirAll(cfg.ExportDir, 0755); err != nil {
		ui.Errorln("❌ Cannot create export directory: %v", err)
		return
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		ui.Errorln("❌ Error saving file: %v", err)
		return
	}

	ui.AIln("✅ Saved to: %s", fullPath)
}

func HandleLoadCommand(c *Chat, args string) {
	if args == "" {
		// Interactive mode: list sessions and let user choose
		sessions, err := c.HistoryManager.ListSessions()
		if err != nil {
			ui.Errorln("Error listing sessions: %v", err)
			return
		}

		if len(sessions) == 0 {
			ui.Warningln("No saved sessions found.")
			return
		}

		options := make([]string, len(sessions))
		for i, s := range sessions {
			options[i] = fmt.Sprintf("%s (Started: %s, Messages: %d)", s.ID, s.StartTime.Format("2006-01-02 15:04"), len(s.Messages))
		}

		var selectedOption string
		prompt := &survey.Select{
			Message:  "Select a session to load:",
			Options:  options,
			PageSize: 10,
		}
		err = survey.AskOne(prompt, &selectedOption, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr))
		if err != nil {
			ui.Warningln("\nSession load canceled.")
			return
		}

		sessionID := strings.Split(selectedOption, " ")[0]
		loadAndRestoreSession(c, sessionID)

	} else {
		// Direct load mode: load session by ID
		loadAndRestoreSession(c, args)
	}
}

func loadAndRestoreSession(c *Chat, sessionID string) {
	session, err := c.HistoryManager.LoadSession(sessionID)
	if err != nil {
		ui.Errorln("Error loading session %s: %v", sessionID, err)
		return
	}

	// Save current session before loading a new one
	if len(c.Messages) > 0 {
		c.saveCurrentSession()
	}

	c.RestoreContext(session)
	ui.AIln("Session %s loaded successfully. Context restored.", sessionID)
}

func (c *Chat) ChangeModel(model models.Model) {
	c.Model = model
	c.Analytics.RecordModelChange(string(model))
	setTerminalTitle(fmt.Sprintf("DuckDuckGo Chat - %s", model))
	ui.AIln("Model changed to %s", model)
}

func (c *Chat) AddContextMessage(content string) {
	c.Messages = append(c.Messages, Message{
		Role:    "user",
		Content: content,
	})
}

// RestoreContext restores the chat context from a given conversation session.
func (c *Chat) RestoreContext(session *persistence.ConversationSession) {
	// Convert persistence.Message to chat.Message
	c.Messages = make([]Message, len(session.Messages))
	for i, msg := range session.Messages {
		c.Messages[i] = Message{
			Content: msg.Content,
			Role:    msg.Role,
		}
	}
	c.SessionID = session.ID
	c.Model = models.Model(session.Model) // Restore the model used in that session
	ui.AIln("Context restored from session %s. Model set to %s.", session.ID, session.Model)
}

// Helper methods for intelligent features

// convertMessagesToIntelligence converts Chat messages to intelligence.Message format
func (c *Chat) convertMessagesToIntelligence() []intelligence.Message {
	result := make([]intelligence.Message, len(c.Messages))
	for i, msg := range c.Messages {
		result[i] = intelligence.Message{
			Content:   msg.Content,
			Role:      msg.Role,
			Timestamp: time.Now(), // We don't have timestamps in current messages
		}
	}
	return result
}

// convertFromIntelligenceMessages converts intelligence.Message back to Chat messages
func (c *Chat) convertFromIntelligenceMessages(messages []intelligence.Message) []Message {
	result := make([]Message, len(messages))
	for i, msg := range messages {
		result[i] = Message{
			Content: msg.Content,
			Role:    msg.Role,
		}
	}
	return result
}

// saveCurrentSession saves the current conversation session to persistent storage
func (c *Chat) saveCurrentSession() {
	if len(c.Messages) == 0 {
		return // No content to save
	}

	// Convert messages to intelligence format
	intelligenceMessages := c.convertMessagesToIntelligence()

	// Create session object
	session := &persistence.ConversationSession{
		ID:        c.SessionID,
		StartTime: c.Analytics.SessionStartTime,
		Model:     string(c.Model),
		Messages:  intelligenceMessages,
		Analytics: persistence.SessionAnalytics{
			MessageCount:      len(c.Messages),
			TotalTokens:       c.Analytics.TotalTokensEstimate,
			APICallsCount:     c.Analytics.APICallsTotal,
			ErrorCount:        c.Analytics.APICallsFailed,
			OptimizationsUsed: c.Analytics.ContextOptimizations,
		},
	}

	// Save session asynchronously
	go func() {
		if err := c.HistoryManager.SaveSession(session); err != nil {
			ui.Warningln("Failed to save session: %v", err)
		}
	}()
}

// ShowSessionStats displays analytics at the end of the session
func (c *Chat) ShowSessionStats() {
	c.Analytics.DisplayStatistics()
}

// HandlePromptCommand processes the /prompt command for prompt management and loading
func HandlePromptCommand(c *Chat, input string, cfg *config.Config) {
	args := strings.TrimPrefix(input, "/prompt")
	args = strings.TrimSpace(args)
	if args == "" || args == "help" {
		// Launch interactive prompt management menu for consistency
		config.HandlePromptManagement(cfg)
		return
	}
	fields := strings.Fields(args)
	if len(fields) == 0 {
		ui.Errorln("Invalid /prompt usage. Type /prompt help for usage.")
		return
	}
	subcmd := fields[0]
	switch subcmd {
	case "list":
		names := config.ListPrompts(cfg)
		if len(names) == 0 {
			ui.AIln("No prompts saved.")
			return
		}
		ui.AIln("Saved prompts:")
		for _, name := range names {
			content, _ := config.GetPrompt(cfg, name)
			preview := content
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			ui.AIln("- %s: %s", name, preview)
		}
	case "add":
		if len(fields) < 2 {
			ui.Errorln("Usage: /prompt add <name> -- <prompt text>")
			return
		}
		name := fields[1]
		promptText := ""
		if strings.Contains(args, "--") {
			parts := strings.SplitN(args, "--", 2)
			promptText = strings.TrimSpace(parts[1])
		}
		if promptText == "" {
			ui.Errorln("Prompt text required after --")
			return
		}
		err := config.AddPrompt(cfg, name, promptText)
		if err != nil {
			ui.Errorln("%v", err)
		} else {
			ui.AIln("Prompt '%s' added.", name)
		}
	case "edit":
		if len(fields) < 2 {
			ui.Errorln("Usage: /prompt edit <name> -- <prompt text>")
			return
		}
		name := fields[1]
		promptText := ""
		if strings.Contains(args, "--") {
			parts := strings.SplitN(args, "--", 2)
			promptText = strings.TrimSpace(parts[1])
		}
		if promptText == "" {
			ui.Errorln("Prompt text required after --")
			return
		}
		err := config.EditPrompt(cfg, name, promptText)
		if err != nil {
			ui.Errorln("%v", err)
		} else {
			ui.AIln("Prompt '%s' updated.", name)
		}
	case "remove":
		if len(fields) < 2 {
			ui.Errorln("Usage: /prompt remove <name>")
			return
		}
		name := fields[1]
		err := config.RemovePrompt(cfg, name)
		if err != nil {
			ui.Errorln("%v", err)
		} else {
			ui.AIln("Prompt '%s' removed.", name)
		}
	case "load":
		if len(fields) < 2 {
			ui.Errorln("Usage: /prompt load <name>")
			return
		}
		name := fields[1]
		promptText, err := config.GetPrompt(cfg, name)
		if err != nil {
			ui.Errorln("%v", err)
			return
		}
		// Aperçu du prompt (80 premiers caractères)
		preview := promptText
		if len(preview) > 80 {
			preview = preview[:80] + "..."
		}
		ui.AIln("[Prompt: %s] Preview: %s", name, preview)
		ProcessInput(c, promptText, cfg)
	default:
		ui.Errorln("Unknown /prompt subcommand: %s", subcmd)
	}
}
