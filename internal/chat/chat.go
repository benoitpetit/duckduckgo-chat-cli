package chat

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"duckduckgo-chat-cli/internal/activity"
	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/browserrelay"
	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/command"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
	"duckduckgo-chat-cli/internal/scrape"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"
)

type Chat struct {
	Activity      *activity.Hub
	Display       *ConversationView
	OldVqd        string
	NewVqd        string
	VqdHash1      string // x-vqd-hash-1 header (full VQD hash)
	FeSignals     string // x-fe-signals header
	FeVersion     string // x-fe-version header
	Model         models.Model
	Messages      []Message
	pendingImages []media.ImageAttachment
	Client        *http.Client
	BrowserRetry  BrowserRetryer
	CookieJar     *cookiejar.Jar
	LastHash      string
	RetryCount    int

	// New intelligent features
	Analytics                  *analytics.ChatAnalytics
	ContextOptimizer           *intelligence.ContextOptimizer
	HistoryManager             *persistence.HistoryManager
	SessionID                  string
	ConversationStartTime      time.Time
	suppressSensitiveDebugLogs bool
	suppressDebugPayload       bool

	// Native Duck.ai tools are opt-in because their wire protocol is not
	// public API and may change independently of the chat endpoint.
	NativeToolsEnabled                bool
	NativeWebSearch                   bool
	NativeImageGeneration             bool
	GeneratedImageDir                 string
	requestMu                         sync.Mutex
	requestCancel                     context.CancelFunc
	rateLimitFallbackOpen             time.Time
	rateLimitBrowserOpenedThisAttempt bool
	retryProofTimeout                 time.Duration
	visibleProofBrowserFactory        proofBrowserFactory
	durableStreamMu                   sync.Mutex
	durableConversation               *DurableStream
}

// BrowserRetryer runs a final Duck.ai request through the signed-in page
// session. Keeping this as an interface allows tests to use an isolated fake.
type BrowserRetryer interface {
	Do(context.Context, browserrelay.Request) (*http.Response, bool, error)
}

type Message struct {
	Content   string                  `json:"content"`
	Role      string                  `json:"role"`
	Images    []media.ImageAttachment `json:"-"`
	Timestamp time.Time               `json:"-"`
}

var ErrRateLimited = errors.New("Duck.ai rate limit reached (HTTP 429). Try again later.")
var ErrDuckAIChallenge = errors.New("Duck.ai rejected this automated browser session (HTTP 418, ERR_CHALLENGE). The CLI cannot continue this request.")
var errRetryProofCapture = errors.New("fresh Duck.ai proof capture failed")
var errEmptyResponse = errors.New("Duck.ai returned no assistant response")

const defaultRetryProofTimeout = 15 * time.Second

// RateLimitError keeps safe, actionable information from a 429 response while
// remaining compatible with callers that check errors.Is(err, ErrRateLimited).
type RateLimitError struct {
	Code       string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	message := ErrRateLimited.Error()
	if e.Code != "" {
		message += " Duck.ai error code: " + e.Code + "."
	}
	if e.RetryAfter > 0 {
		seconds := int64(e.RetryAfter / time.Second)
		if e.RetryAfter%time.Second != 0 {
			seconds++
		}
		message += fmt.Sprintf(" Retry after about %d seconds.", seconds)
	}
	return message
}

func (e *RateLimitError) Is(target error) bool {
	return target == ErrRateLimited
}

func recoveryErrorDetail(err error) string {
	if errors.Is(err, ErrRateLimited) {
		return "Duck.ai still reports HTTP 429 in that browser session"
	}
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}

func (m Message) MarshalJSON() ([]byte, error) {
	if len(m.Images) == 0 {
		type textMessage Message
		return json.Marshal(textMessage(m))
	}

	type contentBlock struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		MIMEType string `json:"mimeType,omitempty"`
		Image    string `json:"image,omitempty"`
	}
	type multimodalMessage struct {
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	}
	blocks := make([]contentBlock, 0, len(m.Images)+1)
	blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
	for _, attachment := range m.Images {
		blocks = append(blocks, contentBlock{
			Type:     "image",
			MIMEType: attachment.MIMEType,
			Image:    "data:" + attachment.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(attachment.Data),
		})
	}
	return json.Marshal(multimodalMessage{Role: m.Role, Content: blocks})
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
	CanUseApproxLocation       *bool          `json:"canUseApproxLocation"`
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
	State       string
	Status      string
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
	if model == "" {
		model = models.Default()
		ui.Warningln("Unknown configured model; using %s", model)
	}
	chat := NewChat("", "", "", "", model, cfg)
	ui.Debugln("Chat initialized with model: %s", model)
	setTerminalTitle(fmt.Sprintf("DuckDuckGo Chat - %s", model))
	return chat
}

func setTerminalTitle(title string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("cmd", "/c", fmt.Sprintf("title %s", title)).Run()
	default:
		fmt.Printf("\033]0;%s\007", title)
	}
}

func NewChat(vqd, vqdHash1, feSignals, feVersion string, model models.Model, cfg *config.Config) *Chat {
	jar := newDuckAICookieJar()

	// Generate unique session ID
	sessionID := fmt.Sprintf("session_%d", time.Now().UnixNano())

	// Initialize intelligent features
	analytics := analytics.NewChatAnalytics()
	contextOptimizer := intelligence.NewContextOptimizer()
	historyManager := persistence.NewHistoryManager(cfg.ExportDir)
	if err := historyManager.SetRetentionDays(cfg.Dashboard.RetentionDays); err != nil {
		ui.Warningln("Failed to apply conversation retention: %v", err)
	}

	// Use all headers like the real web browser
	ui.Debugln("Using VQD with all required headers like web browser")

	chat := &Chat{
		OldVqd:    vqd,       // x-vqd-4 value
		NewVqd:    vqd,       // x-vqd-4 value
		VqdHash1:  vqdHash1,  // x-vqd-hash-1 value
		FeSignals: feSignals, // x-fe-signals value
		FeVersion: feVersion, // x-fe-version value
		Model:     model,
		Messages:  []Message{},
		CookieJar: jar,
		// Streaming requests are bounded by their request context rather than a
		// short client-wide timeout. This keeps long responses and image
		// generation cancellable without truncating healthy streams.
		Client: &http.Client{Jar: jar},
		// Keep all automatic recovery inside the isolated headless browser.
		// Legacy rate_limit.open_browser settings are intentionally ignored.
		BrowserRetry:               nil,
		RetryCount:                 0,
		visibleProofBrowserFactory: nil,

		// Initialize new intelligent features
		Analytics:             analytics,
		ContextOptimizer:      contextOptimizer,
		HistoryManager:        historyManager,
		SessionID:             sessionID,
		ConversationStartTime: time.Now(),

		NativeToolsEnabled:    cfg.Tools.Enabled,
		NativeWebSearch:       cfg.Tools.WebSearch,
		NativeImageGeneration: cfg.Tools.ImageGeneration,
		GeneratedImageDir:     filepath.Join(cfg.ExportDir, "images"),
	}

	// Record initial model
	analytics.RecordModelChange(string(model))

	ui.Debugln("Intelligent features enabled: Analytics, Context Optimization, History Management")

	return chat
}

func newDurableStream() (*DurableStream, error) {
	return generateDurableStream(randomRequestID(), randomRequestID())
}

func newDurableConversation() (*DurableStream, error) {
	return generateDurableStream("", randomRequestID())
}

func generateDurableStream(messageID, conversationID string) (*DurableStream, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

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

// durableStreamForRequest keeps the conversation ID and encryption key stable
// for this local conversation while assigning each HTTP request its own ID.
func (c *Chat) durableStreamForRequest() (*DurableStream, error) {
	c.durableStreamMu.Lock()
	defer c.durableStreamMu.Unlock()

	if c.durableConversation == nil {
		conversation, err := newDurableConversation()
		if err != nil {
			return nil, err
		}
		c.durableConversation = conversation
	}

	return &DurableStream{
		MessageID:      randomRequestID(),
		ConversationID: c.durableConversation.ConversationID,
		PublicKey:      c.durableConversation.PublicKey,
	}, nil
}

func (c *Chat) resetDurableConversation() {
	c.durableStreamMu.Lock()
	c.durableConversation = nil
	c.durableStreamMu.Unlock()
}

// resetRateLimitSession clears Duck.ai site data in the running headless
// browser, then discards HTTP cookies and the durable conversation identity.
func (c *Chat) resetRateLimitSession() error {
	return c.resetRateLimitSessionContext(context.Background())
}

func (c *Chat) resetRateLimitSessionContext(ctx context.Context) error {
	resetCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := sharedDuckAIBrowser.ClearSiteData(resetCtx); err != nil {
		return fmt.Errorf("could not clear Duck.ai browser site data: %w", err)
	}

	jar := newDuckAICookieJar()
	c.CookieJar = jar
	if c.Client != nil {
		c.Client.Jar = jar
	}
	c.NewVqd = ""
	c.OldVqd = ""
	c.VqdHash1 = ""
	c.FeSignals = ""
	c.FeVersion = ""
	c.resetDurableConversation()
	return nil
}

func newDuckAICookieJar() *cookiejar.Jar {
	jar, _ := cookiejar.New(nil)
	return jar
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
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	headers, err := getCurrentDuckAIHeaders(ctx)
	if err != nil {
		ui.Errorln("Error getting Duck.ai chat headers: %v", err)
		return "", "", "", ""
	}
	return "", headers.VqdHash1, headers.FeSignals, headers.FeVersion

}

func (c *Chat) Clear(cfg *config.Config) error {
	// Save current session before clearing if it has content
	if len(c.Messages) > 0 {
		if err := c.SaveCurrentSession(); err != nil {
			ui.Warningln("Failed to save session before clearing: %v", err)
			return err
		}
	}
	if err := c.resetRateLimitSession(); err != nil {
		return err
	}
	c.Messages = []Message{}
	c.pendingImages = nil
	if c.Display != nil {
		c.Display.Clear()
	}
	c.RetryCount = 0
	c.rateLimitFallbackOpen = time.Time{}
	c.rateLimitBrowserOpenedThisAttempt = false
	c.SessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
	c.ConversationStartTime = time.Now()

	clearTerminal()
	ui.AIln("Chat history and Duck.ai session cleared")

	if cfg.ShowMenu {
		PrintWelcomeMessage()
	} else {
		PrintCommands()
	}
	return nil
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
	_ = cmd.Run()
}

func ProcessInput(c *Chat, input string, cfg *config.Config) {
	processInputWithRoleLengths(c, input, cfg, len(input), 0)
}

// ProcessInputWithContext submits command-chain context and the user's prompt
// as one Duck.ai message while keeping their analytics roles separate.
func ProcessInputWithContext(c *Chat, contextContent, prompt string, cfg *config.Config) {
	input := contextContent
	if prompt != "" {
		if input != "" {
			input += "\n\n"
		}
		input += prompt
	}
	processInputWithRoleLengths(c, input, cfg, len(prompt), len(contextContent))
}

func processInputWithRoleLengths(c *Chat, input string, cfg *config.Config, userContentLength, contextContentLength int) {
	if c.Display != nil {
		c.Display.SetHeader("DuckDuckGo AI Chat CLI · " + models.DisplayName(c.Model))
		c.Display.SetFramed(cfg.Appearance.FrameResponses)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.setRequestCancel(cancel)
	defer c.clearRequestCancel()
	modelLabel := func() string { return shortenModelName(string(c.Model)) }
	spinner := ui.StartSpinnerWithModel(ui.AccentColor.Sprint(modelLabel()))
	defer spinner.Stop()
	debugTrace := &requestDebugTrace{}
	if ui.DebugEnabled() {
		debugTrace.RecordProgress(ProgressUpdate{Stage: ui.ProgressConnecting, Label: "Connecting to Duck.ai"})
	}
	recordProgress := func(update ProgressUpdate) {
		spinner.SetProgress(update.Stage, update.Label)
		if ui.DebugEnabled() {
			debugTrace.RecordProgress(update)
		}
	}
	recordEvent := func(event StreamEvent) {
		if !ui.DebugEnabled() {
			return
		}
		debugTrace.RecordEvent(event)
	}
	fetch := func(ctx context.Context, content string) (<-chan string, <-chan error, error) {
		return c.FetchStreamWithErrorsAndProgressEvents(ctx, content, recordProgress, recordEvent)
	}
	response, err := processInputWithTokenRoles(ctx, c, input, cfg, func(stream <-chan string) string {
		return RenderStreamWithView(stream, shortenModelName(string(c.Model)), spinner, cfg.Appearance.FrameResponses, c.Display)
	}, fetch, userContentLength, contextContentLength)
	if err == nil && c.Display != nil {
		c.Display.RecordAssistant(modelLabel(), response)
	}
	spinner.Stop()
	if ui.DebugEnabled() {
		for _, entry := range debugTrace.Snapshot() {
			ui.Debugln("%s", entry)
		}
	}
	if err != nil {
		c.reportChatFailure(ctx, err)
	}
}

func (c *Chat) setRequestCancel(cancel context.CancelFunc) {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	c.requestCancel = cancel
}

func (c *Chat) clearRequestCancel() {
	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	if c.requestCancel != nil {
		c.requestCancel = nil
	}
}

// QueueImageAttachments keeps local image attachments for the next prompt.
func (c *Chat) QueueImageAttachments(images []media.ImageAttachment) {
	c.pendingImages = append(c.pendingImages, cloneImageAttachments(images)...)
}

func cloneImageAttachments(images []media.ImageAttachment) []media.ImageAttachment {
	cloned := make([]media.ImageAttachment, len(images))
	for i, image := range images {
		cloned[i] = image
		cloned[i].Data = append([]byte(nil), image.Data...)
	}
	return cloned
}

// CancelCurrentRequest interrupts the active browser bootstrap or chat stream.
func (c *Chat) CancelCurrentRequest() bool {
	c.requestMu.Lock()
	cancel := c.requestCancel
	c.requestMu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
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
	return processInput(context.Background(), c, input, cfg, renderStreamToString)
}

// ProcessInputContext is the shared conversation pipeline used by HTTP and
// interactive callers. The renderer is injected so the domain flow does not
// depend on terminal output.
func ProcessInputContext(ctx context.Context, c *Chat, input string, cfg *config.Config) (string, error) {
	return processInput(ctx, c, input, cfg, renderStreamToString)
}

func processInput(ctx context.Context, c *Chat, input string, cfg *config.Config, render func(<-chan string) string) (string, error) {
	return processInputWithStreamFetcher(ctx, c, input, cfg, render, c.FetchStreamWithErrors)
}

func processInputWithTokenRoles(ctx context.Context, c *Chat, input string, cfg *config.Config, render func(<-chan string) string, fetch func(context.Context, string) (<-chan string, <-chan error, error), userContentLength, contextContentLength int) (string, error) {
	return processInputWithStreamFetcherAndTokenRoles(ctx, c, input, cfg, render, fetch, userContentLength, contextContentLength)
}

func processInputWithFetcher(ctx context.Context, c *Chat, input string, cfg *config.Config, render func(<-chan string) string, fetch func(context.Context, string) (<-chan string, error)) (string, error) {
	return processInputWithStreamFetcher(ctx, c, input, cfg, render, func(ctx context.Context, content string) (<-chan string, <-chan error, error) {
		stream, err := fetch(ctx, content)
		return stream, nil, err
	})
}

func processInputWithFetcherAndTokenRoles(ctx context.Context, c *Chat, input string, cfg *config.Config, render func(<-chan string) string, fetch func(context.Context, string) (<-chan string, error), userContentLength, contextContentLength int) (string, error) {
	return processInputWithStreamFetcherAndTokenRoles(ctx, c, input, cfg, render, func(ctx context.Context, content string) (<-chan string, <-chan error, error) {
		stream, err := fetch(ctx, content)
		return stream, nil, err
	}, userContentLength, contextContentLength)
}

func processInputWithStreamFetcher(ctx context.Context, c *Chat, input string, cfg *config.Config, render func(<-chan string) string, fetch func(context.Context, string) (<-chan string, <-chan error, error)) (string, error) {
	return processInputWithStreamFetcherAndTokenRoles(ctx, c, input, cfg, render, fetch, len(input), 0)
}

func processInputWithStreamFetcherAndTokenRoles(ctx context.Context, c *Chat, input string, cfg *config.Config, render func(<-chan string) string, fetch func(context.Context, string) (<-chan string, <-chan error, error), userContentLength, contextContentLength int) (string, error) {
	if strings.TrimSpace(input) == "" {
		return "", nil
	}
	if c.Activity != nil {
		c.Activity.Publish(activity.Event{Category: "conversation", Status: "prompt", Summary: "User prompt submitted", Prompt: input})
	}

	originalMessages := append([]Message(nil), c.Messages...)
	originalPendingImages := c.pendingImages
	c.pendingImages = nil
	originalMessageCount := len(originalMessages)
	isFirstMessage := originalMessageCount == 0
	actualMessage := input
	if isFirstMessage && cfg.GlobalPrompt != "" {
		actualMessage = cfg.GlobalPrompt + "\n\n" + input
	}
	if c.Analytics != nil {
		if userContentLength > 0 && contextContentLength > 0 {
			c.Analytics.RecordPromptWithContext(userContentLength, contextContentLength)
		} else if userContentLength > 0 {
			c.Analytics.RecordMessage("user", userContentLength)
		} else if contextContentLength > 0 {
			c.Analytics.RecordMessage("context", contextContentLength)
		}
	}

	c.Messages = append(c.Messages, Message{
		Role:      "user",
		Content:   actualMessage,
		Images:    originalPendingImages,
		Timestamp: time.Now(),
	})

	if c.ContextOptimizer != nil && c.ContextOptimizer.IsOptimizationNeeded(c.convertMessagesToIntelligence()) {
		optimizedMessages, bytesSaved := c.ContextOptimizer.OptimizeContext(c.convertMessagesToIntelligence())
		c.Messages = c.convertFromIntelligenceMessages(optimizedMessages)
		if c.Analytics != nil {
			c.Analytics.RecordContextOptimization(bytesSaved)
			for _, message := range optimizedMessages {
				if message.Compressed {
					c.Analytics.RecordContextCompression()
					break
				}
			}
		}
	}

	requestedModel := c.Model
	if hasImageAttachments(c.Messages) {
		imageModel := models.ImageInputModel(requestedModel)
		if imageModel != requestedModel {
			ui.Warningln("%s cannot receive images through Duck.ai. Using %s for this image conversation.", models.DisplayName(requestedModel), models.DisplayName(imageModel))
			c.Model = imageModel
			defer func() { c.Model = requestedModel }()
		}
	}

	startTime := time.Now()
	usedModel := string(c.Model)
	var requestActivity activity.Event
	if c.Activity != nil {
		requestActivity = c.Activity.Publish(activity.Event{Category: "request", Status: "started", Summary: "Chat request in progress", Model: usedModel})
	}
	stream, streamErrors, err := fetch(ctx, actualMessage)
	if err != nil {
		if c.Activity != nil {
			status, summary := "failed", "Chat request failed"
			if ctx.Err() != nil {
				status, summary = "cancelled", "Chat request cancelled"
			}
			c.Activity.Publish(activity.Event{Category: "request", Status: status, Summary: summary, Model: usedModel, OperationID: requestActivity.OperationID})
		}
		c.Messages = originalMessages
		c.pendingImages = originalPendingImages
		if c.Analytics != nil {
			duration := time.Since(startTime)
			c.Analytics.RecordChatInteraction(duration, false, "unknown")
			c.Analytics.RecordModelInteraction(usedModel, duration, false, "unknown")
		}
		if errors.Is(err, ErrRateLimited) {
			return "", err
		}
		return "", fmt.Errorf("error fetching stream: %w", err)
	}

	finalResponse := render(stream)
	var streamErr error
	if streamErrors != nil {
		for err := range streamErrors {
			if err != nil {
				streamErr = err
			}
		}
	}
	if streamErr == nil && strings.TrimSpace(finalResponse) == "" {
		streamErr = errEmptyResponse
	}
	if streamErr != nil {
		if c.Activity != nil {
			c.Activity.Publish(activity.Event{Category: "request", Status: "failed", Summary: "Chat response stream failed", Model: usedModel, OperationID: requestActivity.OperationID})
		}
		c.Messages = originalMessages
		c.pendingImages = originalPendingImages
		if c.Analytics != nil {
			duration := time.Since(startTime)
			c.Analytics.RecordChatInteraction(duration, false, "stream_error")
			c.Analytics.RecordModelInteraction(usedModel, duration, false, "stream_error")
		}
		return "", fmt.Errorf("error reading response stream: %w", streamErr)
	}
	if err := ctx.Err(); err != nil {
		if c.Activity != nil {
			c.Activity.Publish(activity.Event{Category: "request", Status: "cancelled", Summary: "Chat request cancelled", Model: usedModel, OperationID: requestActivity.OperationID})
		}
		c.Messages = originalMessages
		c.pendingImages = originalPendingImages
		return "", err
	}
	if c.Activity != nil {
		c.Activity.Publish(activity.Event{Category: "request", Status: "completed", Summary: "Chat response received", Model: usedModel, OperationID: requestActivity.OperationID})
		c.Activity.Publish(activity.Event{Category: "conversation", Status: "response", Summary: "Assistant response received", Model: usedModel, Response: finalResponse})
	}
	if c.Analytics != nil {
		duration := time.Since(startTime)
		c.Analytics.RecordChatInteraction(duration, true, "")
		c.Analytics.RecordModelInteraction(usedModel, duration, true, "")
		c.Analytics.RecordMessage("assistant", len(finalResponse))
	}

	// Add the assistant's response to the message history
	c.Messages = append(c.Messages, Message{
		Role:      "assistant",
		Content:   finalResponse,
		Timestamp: time.Now(),
	})

	return finalResponse, nil
}

func hasImageAttachments(messages []Message) bool {
	for _, message := range messages {
		if len(message.Images) > 0 {
			return true
		}
	}
	return false
}

func shortenModelName(model string) string {
	if resolved, ok := models.ResolveModel(model); ok {
		return models.DisplayName(resolved)
	}
	return "unknown"
}

func (c *Chat) FetchStream(content string) (<-chan string, error) {
	return c.FetchStreamContext(context.Background(), content)
}

func (c *Chat) FetchStreamContext(ctx context.Context, content string) (<-chan string, error) {
	stream, _, err := c.FetchStreamWithErrors(ctx, content)
	return stream, err
}

func (c *Chat) FetchStreamWithErrors(ctx context.Context, content string) (<-chan string, <-chan error, error) {
	return c.FetchStreamWithErrorsAndProgress(ctx, content, nil)
}

// FetchStreamWithErrorsAndProgress also reports Duck.ai tool and status events
// to an optional terminal progress callback while preserving response text.
func (c *Chat) FetchStreamWithErrorsAndProgress(ctx context.Context, content string, onProgress func(ProgressUpdate)) (<-chan string, <-chan error, error) {
	return c.fetchStreamWithErrorsAndProgress(ctx, content, onProgress, nil)
}

// FetchStreamWithErrorsAndProgressEvents also exposes structured stream events
// to callers that need diagnostics while keeping response rendering separate.
func (c *Chat) FetchStreamWithErrorsAndProgressEvents(ctx context.Context, content string, onProgress func(ProgressUpdate), onEvent func(StreamEvent)) (<-chan string, <-chan error, error) {
	return c.fetchStreamWithErrorsAndProgress(ctx, content, onProgress, onEvent)
}

func (c *Chat) fetchStreamWithErrorsAndProgress(ctx context.Context, content string, onProgress func(ProgressUpdate), onEvent func(StreamEvent)) (<-chan string, <-chan error, error) {
	events, err := c.fetchEventStreamContextWithProgress(ctx, content, onProgress)
	if err != nil {
		return nil, nil, err
	}
	if onProgress != nil {
		onProgress(ProgressUpdate{Stage: ui.ProgressPreparing, Label: "Preparing response"})
	}

	stream := make(chan string)
	streamErrors := make(chan error, 1)
	go func() {
		defer close(stream)
		defer close(streamErrors)
		var latestImage *StreamEvent
		var seenSources sourceProgressTracker
		var responseProgress responseProgressTracker
		for event := range events {
			if onEvent != nil {
				onEvent(event)
			}
			switch event.Type {
			case "status", "tool":
				if onProgress != nil {
					if update, ok := progressUpdateForEvent(event); ok {
						onProgress(update)
					}
				}
			case "error":
				streamErrors <- errors.New(event.Message)
			case "message":
				if onProgress != nil {
					if update, ok := responseProgress.Add(event); ok {
						onProgress(update)
					}
				}
				if event.Message != "" {
					select {
					case stream <- event.Message:
					case <-ctx.Done():
						return
					}
				}
			case "source":
				if event.SourceURL != "" {
					if count, added := seenSources.Add(event.SourceURL); added && onProgress != nil {
						onProgress(sourceProgressUpdate(count))
					}
					select {
					case stream <- formatSourceEvent(event):
					case <-ctx.Done():
						return
					}
				}
			case "image":
				if onProgress != nil {
					if update, ok := progressUpdateForEvent(event); ok {
						onProgress(update)
					}
				}
				if event.ImageBase64 != "" {
					imageCopy := event
					latestImage = &imageCopy
				} else if event.ImageURL != "" {
					select {
					case stream <- fmt.Sprintf("\n\nImage generated: %s\n", event.ImageURL):
					case <-ctx.Done():
						return
					}
				}
			}
		}
		if latestImage != nil {
			path, err := c.saveGeneratedImage(*latestImage)
			if err != nil {
				if onEvent != nil {
					onEvent(StreamEvent{Type: "image_save_failed", ToolResult: err.Error()})
				}
				select {
				case stream <- fmt.Sprintf("\n\nImage generated but could not be saved: %v\n", err):
				case <-ctx.Done():
					return
				}
			} else {
				if onEvent != nil {
					onEvent(StreamEvent{Type: "image_saved", ImageURL: path})
				}
				select {
				case stream <- fmt.Sprintf("\n\nImage generated: %s\n", path):
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return stream, streamErrors, nil
}

func sourceProgressLabel(count int) string {
	if count == 1 {
		return "Found 1 web source"
	}
	return fmt.Sprintf("Found %d web sources", count)
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
	return c.FetchEventStreamContext(context.Background(), content)
}

func (c *Chat) FetchEventStreamContext(ctx context.Context, content string) (<-chan StreamEvent, error) {
	return c.fetchEventStreamContextWithProgress(ctx, content, nil)
}

func (c *Chat) fetchEventStreamContextWithProgress(ctx context.Context, content string, onProgress func(ProgressUpdate)) (<-chan StreamEvent, error) {
	resp, err := c.fetchContext(ctx, content, 0, onProgress)
	if err != nil {
		return nil, err
	}

	stream := make(chan StreamEvent)
	go func() {
		defer resp.Body.Close()
		defer close(stream)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		eventName := ""
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				eventName = ""
				continue
			}
			if strings.HasPrefix(line, "event:") {
				eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			event, ok := parseSSEEventLine(line)
			if !ok {
				continue
			}
			if status := normalizeStreamStatus(eventName, ""); status != "" && (event.Type == "meta" || event.Type == "raw") {
				event = StreamEvent{Type: "status", Status: status}
			}
			select {
			case stream <- event:
			case <-ctx.Done():
				return
			}
			if event.Type == "done" {
				break
			}
		}

		if err := scanner.Err(); err != nil {
			select {
			case stream <- StreamEvent{Type: "error", Message: err.Error()}:
			case <-ctx.Done():
				return
			}
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
		Status        string          `json:"status"`
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
	if strings.EqualFold(message.Role, "assistant") || strings.EqualFold(message.Role, "status") {
		if status := normalizeStreamStatus(message.State, message.Status); status != "" {
			return StreamEvent{Type: "status", State: message.State, Status: status}, true
		}
	}
	if message.Role == "source" && message.Source != nil {
		return StreamEvent{Type: "source", SourceURL: message.Source.URL, SourceTitle: message.Source.Title}, true
	}
	if message.Role == "tool-invocation" {
		event := StreamEvent{Type: "tool", State: message.State, ToolName: message.ToolName, ToolCallID: message.ToolCallID, Raw: payload}
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

func normalizeStreamStatus(state, status string) string {
	value := strings.ToLower(strings.TrimSpace(status + " " + state))
	switch {
	case strings.Contains(value, "search"):
		return "Searching the web"
	case strings.Contains(value, "think") || strings.Contains(value, "reason"):
		return "Thinking"
	case strings.Contains(value, "generat") || strings.Contains(value, "respond"):
		return "Writing response"
	default:
		return ""
	}
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
	return c.FetchContext(context.Background(), content)
}

func retryProgress(onProgress func(ProgressUpdate), update ProgressUpdate, fallbackMessage string) {
	if onProgress != nil {
		onProgress(update)
		return
	}
	if fallbackMessage != "" {
		ui.Warningln("%s", fallbackMessage)
	}
}

func (c *Chat) FetchContext(ctx context.Context, content string) (*http.Response, error) {
	c.RetryCount = 0
	return c.fetchContext(ctx, content, 0, nil)
}

func (c *Chat) fetchContext(ctx context.Context, content string, retries int, onProgress func(ProgressUpdate)) (*http.Response, error) {
	if retries == 0 {
		c.rateLimitBrowserOpenedThisAttempt = false
	}
	startTime := time.Now()
	// Duck.ai's proof is generated by its frontend and must be captured from
	// a real browser request. It is rotated frequently, so refresh it for
	// every chat request instead of reusing the old DuckDuckGo token.
	proofCtx := ctx
	var cancelProof context.CancelFunc
	if retries > 0 {
		timeout := c.retryProofTimeout
		if timeout <= 0 {
			timeout = defaultRetryProofTimeout
		}
		proofCtx, cancelProof = context.WithTimeout(ctx, timeout)
	}
	headers, err := getCurrentDuckAIHeaders(proofCtx)
	if cancelProof != nil {
		cancelProof()
	}
	if err != nil {
		if retries == 0 && errors.Is(err, ErrRateLimited) && ctx.Err() == nil &&
			c.BrowserRetry == nil && c.visibleProofBrowserFactory == nil {
			retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressPreparing, Label: "Clearing Duck.ai site data"}, "Duck.ai refused this browser session; clearing its site data and retrying once.")
			if resetErr := c.resetRateLimitSessionContext(ctx); resetErr != nil {
				return nil, fmt.Errorf("%w; could not reset local session: %v", err, resetErr)
			}
			c.RetryCount = 1
			return c.fetchContext(ctx, content, 1, onProgress)
		}
		if retries > 0 && errors.Is(err, ErrRateLimited) {
			return nil, err
		}
		if retries > 0 && ctx.Err() == nil {
			return nil, fmt.Errorf("%w: %w", errRetryProofCapture, err)
		}
		if retries == 0 && errors.Is(err, ErrRateLimited) && c.visibleProofBrowserFactory != nil && ctx.Err() == nil {
			return c.fetchViaCleanBrowser(ctx, content, onProgress, err)
		}
		return nil, err
	}
	c.NewVqd = ""
	c.VqdHash1 = headers.VqdHash1
	c.FeSignals = headers.FeSignals
	c.FeVersion = headers.FeVersion
	if c.VqdHash1 == "" {
		return nil, fmt.Errorf("Duck.ai returned an empty X-Vqd-Hash-1 proof")
	}
	if len(headers.BrowserCookies) > 0 {
		if c.CookieJar == nil {
			c.CookieJar = newDuckAICookieJar()
		}
		site, _ := url.Parse(models.ChatURL)
		c.CookieJar.SetCookies(site, headers.BrowserCookies)
		if c.Client != nil {
			c.Client.Jar = c.CookieJar
		}
	}

	durableStream, err := c.durableStreamForRequest()
	if err != nil {
		return nil, fmt.Errorf("failed to create Duck.ai durable stream: %w", err)
	}

	payload := c.buildPayload(durableStream)

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshaling payload: %v", err)
	}

	if shouldLogRequestPayload(c) {
		debugPayload, debugErr := marshalDebugPayload(payload)
		if debugErr != nil {
			color.Yellow("Could not marshal redacted debug payload: %v", debugErr)
		} else {
			color.Cyan("Payload: %s", string(debugPayload))
		}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", models.ChatURL, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}

	// Use the browser values captured with this request's rotating proof.
	req.Header.Set("Accept", "text/event-stream")
	if headers.AcceptLanguage != "" {
		req.Header.Set("Accept-Language", headers.AcceptLanguage)
	} else {
		req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://duck.ai")
	req.Header.Set("Referer", "https://duck.ai/")
	if headers.UserAgent != "" {
		req.Header.Set("User-Agent", headers.UserAgent)
	} else {
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.0.0 Safari/537.36")
	}
	for name, value := range headers.BrowserHeaders {
		req.Header.Set(name, value)
	}

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
	if headers.JourneyID != "" {
		req.Header.Set("x-ddg-journey-id", headers.JourneyID)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending request: %w", err)
	}
	return c.handleFetchResponse(ctx, resp, req, content, jsonPayload, retries, onProgress, startTime)
}

func (c *Chat) fetchViaCleanBrowser(ctx context.Context, content string, onProgress func(ProgressUpdate), initialErr error) (*http.Response, error) {
	durableStream, err := c.durableStreamForRequest()
	if err != nil {
		return nil, fmt.Errorf("%w; could not prepare clean browser request: %v", initialErr, err)
	}
	payload, err := json.Marshal(c.buildPayload(durableStream))
	if err != nil {
		return nil, fmt.Errorf("%w; could not encode clean browser request: %v", initialErr, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, models.ChatURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w; could not create clean browser request: %v", initialErr, err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	if c.BrowserRetry != nil {
		retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressPreparing, Label: "Opening Duck.ai in Chrome"}, "")
		browserResponse, opened, browserErr := c.BrowserRetry.Do(ctx, browserrelay.Request{
			URL: req.URL.String(), Method: req.Method,
			Header: browserrelay.FilterRequestHeaders(req.Header),
			Body:   append([]byte(nil), payload...),
		})
		if opened {
			c.rateLimitBrowserOpenedThisAttempt = true
		}
		if browserErr != nil {
			return nil, fmt.Errorf("%w; Chrome retry failed: %w", initialErr, browserErr)
		}
		if browserResponse == nil {
			return nil, fmt.Errorf("%w; Chrome retry returned no response", initialErr)
		}
		return c.handleFetchResponse(ctx, browserResponse, req, content, payload, 2, onProgress, time.Now())
	}
	response, err := c.tryVisibleBrowserRequest(ctx, req, payload, onProgress)
	if err != nil {
		return nil, fmt.Errorf("%w; clean browser retry failed: %s", initialErr, recoveryErrorDetail(err))
	}
	return c.handleFetchResponse(ctx, response, req, content, payload, 2, onProgress, time.Now())
}

func (c *Chat) handleFetchResponse(ctx context.Context, resp *http.Response, req *http.Request, content string, jsonPayload []byte, retries int, onProgress func(ProgressUpdate), startTime time.Time) (*http.Response, error) {
	// The browser-backed request is the final 429 recovery step. A failed
	// fresh proof capture also reaches it from the first-attempt branch below.
	// Both paths use the same status and SSE handling as a normal response.
	if resp.StatusCode == http.StatusTooManyRequests && retries == 1 && (c.BrowserRetry != nil || c.visibleProofBrowserFactory != nil) {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		rateLimitErr := &RateLimitError{
			Code:       rateLimitErrorCode(body),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		}
		browserResponse, browserErr := c.browserRetryResponse(ctx, req, jsonPayload, rateLimitErr, onProgress, true)
		if browserErr != nil {
			return nil, fmt.Errorf("%w; %w", rateLimitErr, browserErr)
		}
		resp = browserResponse
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		challenge := resp.StatusCode == http.StatusTeapot || rateLimitErrorCode(body) == "ERR_CHALLENGE"
		if shouldLogRequestDetails(c) {
			color.Red("Request Headers: %+v", redactDebugHeaders(req.Header))
			color.Red("Response Headers: %+v", redactDebugHeaders(resp.Header))
			color.Red("Response Status: %d", resp.StatusCode)
			if resp.StatusCode == http.StatusTooManyRequests || challenge {
				if code := rateLimitErrorCode(body); code != "" {
					color.Red("Duck.ai error code: %s", code)
				}
			} else {
				color.Red("Response Body: %s", string(body))
			}
		}

		bodyText := string(body)
		retryableProofFailure := strings.Contains(bodyText, "ERR_INVALID_VQD")
		if c.Analytics != nil && (retryableProofFailure || resp.StatusCode == http.StatusTooManyRequests) {
			errorType := "unknown"
			if challenge {
				errorType = "418"
			} else if resp.StatusCode == http.StatusTooManyRequests {
				errorType = "429"
			}
			duration := time.Since(startTime)
			c.Analytics.RecordChatInteraction(duration, false, errorType)
			c.Analytics.RecordModelInteraction(string(c.Model), duration, false, errorType)
		}
		if challenge {
			c.RetryCount = 0
			return nil, ErrDuckAIChallenge
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			c.RetryCount = 0
			rateLimitErr := &RateLimitError{
				Code:       rateLimitErrorCode(body),
				RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
			}
			if retries < 1 {
				if rateLimitErr.RetryAfter > 0 {
					retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressPreparing, Label: "Waiting for Duck.ai retry window"}, "")
					if err := waitRetryAfter(ctx, rateLimitErr.RetryAfter); err != nil {
						return nil, fmt.Errorf("%w; retry canceled: %w", rateLimitErr, err)
					}
				}
				// The automatic path uses one local reset and one fresh request.
				// A second send with the rejected proof can only add another
				// request to a session Duck.ai already refused.
				if c.BrowserRetry != nil || c.visibleProofBrowserFactory != nil {
					pageResponse, pageErr := c.tryProofBrowserRequest(ctx, req, jsonPayload, onProgress)
					if pageErr == nil {
						return c.handleFetchResponse(ctx, pageResponse, req, content, jsonPayload, retries+2, onProgress, startTime)
					}
					if ctx.Err() != nil {
						return nil, fmt.Errorf("%w; browser retry canceled: %w", rateLimitErr, ctx.Err())
					}
					if !errors.Is(pageErr, errBrowserPageRequestUnavailable) {
						ui.Debugln("Duck.ai in-page retry did not succeed: %v", pageErr)
					}
				}
				retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressPreparing, Label: "Clearing Duck.ai site data"}, "429 received; clearing Duck.ai site data and retrying once.")
				if err := c.resetRateLimitSessionContext(ctx); err != nil {
					return nil, fmt.Errorf("%w; could not clear local session data before retry: %v", rateLimitErr, err)
				}
				c.RetryCount = retries + 1
				if c.Activity != nil {
					c.Activity.Publish(activity.Event{Category: "request", Status: "retrying", Summary: "Duck.ai rate limit; resetting local site data and retrying once", Model: string(c.Model)})
				}
				retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressConnecting, Label: "Retrying after site data reset"}, "")
				response, retryErr := c.fetchContext(ctx, content, retries+1, onProgress)
				if errors.Is(retryErr, errRetryProofCapture) && ctx.Err() == nil && (c.BrowserRetry != nil || c.visibleProofBrowserFactory != nil) {
					browserResponse, browserErr := c.browserRetryResponse(ctx, req, jsonPayload, rateLimitErr, onProgress, false)
					if browserErr != nil {
						return nil, fmt.Errorf("%w; fresh proof unavailable (%s); %w", rateLimitErr, recoveryErrorDetail(retryErr), browserErr)
					}
					return c.handleFetchResponse(ctx, browserResponse, req, content, jsonPayload, retries+2, onProgress, startTime)
				}
				if retryErr != nil && !errors.Is(retryErr, ErrRateLimited) {
					return nil, fmt.Errorf("%w; retry failed: %w", rateLimitErr, retryErr)
				}
				return response, retryErr
			}
			return nil, rateLimitErr
		}

		// A generic 400 can indicate a bad payload (for example an unsupported
		// image format), so only explicit proof or challenge failures are retried.
		if retryableProofFailure && retries < 1 {
			c.RetryCount = retries + 1
			if c.Activity != nil {
				c.Activity.Publish(activity.Event{Category: "request", Status: "retrying", Summary: "Duck.ai proof rejected; refreshing and retrying once", Model: string(c.Model)})
			}
			retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressConnecting, Label: "Refreshing Duck.ai proof"}, fmt.Sprintf("Duck.ai rejected the request proof (HTTP %d); retrying once with a fresh proof...", resp.StatusCode))
			if c.Analytics != nil {
				c.Analytics.RecordVQDRefresh()
			}
			// FetchContext captures a fresh proof at the start of every attempt.
			// Do not perform a separate capture here, which would waste a proof
			// and add another browser bootstrap before the actual retry.
			return c.fetchContext(ctx, content, retries+1, onProgress)
		}
		c.RetryCount = 0
		return nil, fmt.Errorf("%d: Failed to send message. %s. Body: %s", resp.StatusCode, resp.Status, string(body))
	}

	newVqd := resp.Header.Get("x-vqd-4")
	if newVqd != "" {
		c.OldVqd = c.NewVqd
		c.NewVqd = newVqd
	}
	c.RetryCount = 0

	return resp, nil
}

func (c *Chat) browserRetryResponse(ctx context.Context, req *http.Request, jsonPayload []byte, rateLimitErr *RateLimitError, onProgress func(ProgressUpdate), waitForWindow bool) (*http.Response, error) {
	if waitForWindow && rateLimitErr.RetryAfter > 0 {
		retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressPreparing, Label: "Waiting for Duck.ai retry window"}, "")
		if err := waitRetryAfter(ctx, rateLimitErr.RetryAfter); err != nil {
			return nil, fmt.Errorf("browser retry canceled: %w", err)
		}
	}
	pageResponse, pageErr := c.tryProofBrowserRequest(ctx, req, jsonPayload, onProgress)
	if pageErr == nil {
		return pageResponse, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var visibleErr error
	if c.BrowserRetry == nil && c.visibleProofBrowserFactory != nil {
		visibleResponse, err := c.tryVisibleBrowserRequest(ctx, req, jsonPayload, onProgress)
		if err == nil {
			return visibleResponse, nil
		}
		visibleErr = err
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	if c.BrowserRetry == nil {
		if visibleErr != nil {
			return nil, fmt.Errorf("clean browser retry failed: %s", recoveryErrorDetail(visibleErr))
		}
		return nil, pageErr
	}
	retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressPreparing, Label: "Opening Duck.ai in Chrome"}, "Duck.ai is still rate limiting the CLI; clearing its Chrome site data and retrying once.")
	if c.Activity != nil {
		c.Activity.Publish(activity.Event{Category: "request", Status: "retrying", Summary: "Duck.ai rate limit; trying one request through the browser session", Model: string(c.Model)})
	}
	retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressConnecting, Label: "Retrying with the Chrome Duck.ai session"}, "")
	response, opened, err := c.BrowserRetry.Do(ctx, browserrelay.Request{
		URL: req.URL.String(), Method: req.Method,
		Header: browserrelay.FilterRequestHeaders(req.Header),
		Body:   append([]byte(nil), jsonPayload...),
	})
	if opened {
		c.rateLimitBrowserOpenedThisAttempt = true
	}
	if err != nil {
		if visibleErr != nil {
			return nil, fmt.Errorf("clean browser retry failed (%s); browser session retry failed: %w", recoveryErrorDetail(visibleErr), err)
		}
		if pageErr != nil && !errors.Is(pageErr, errBrowserPageRequestUnavailable) {
			return nil, fmt.Errorf("temporary browser retry failed (%s); browser session retry failed: %w", recoveryErrorDetail(pageErr), err)
		}
		return nil, fmt.Errorf("browser session retry failed: %w", err)
	}
	if response == nil {
		return nil, errors.New("browser session retry returned no response")
	}
	return response, nil
}

func (c *Chat) tryProofBrowserRequest(ctx context.Context, req *http.Request, jsonPayload []byte, onProgress func(ProgressUpdate)) (*http.Response, error) {
	retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressConnecting, Label: "Retrying in the Duck.ai browser session"}, "")
	response, err := sharedDuckAIBrowser.PageRequest(ctx, browserrelay.Request{
		URL: req.URL.String(), Method: req.Method,
		Header: browserrelay.FilterRequestHeaders(req.Header),
		Body:   append([]byte(nil), jsonPayload...),
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("Duck.ai browser page returned no response")
	}
	if response.StatusCode == http.StatusOK {
		return response, nil
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{Code: rateLimitErrorCode(body)}
	}
	return nil, fmt.Errorf("Duck.ai browser page returned HTTP %d", response.StatusCode)
}

func (c *Chat) tryVisibleBrowserRequest(ctx context.Context, req *http.Request, jsonPayload []byte, onProgress func(ProgressUpdate)) (*http.Response, error) {
	retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressPreparing, Label: "Opening a clean Duck.ai browser"}, "")
	bootstrapCtx, cancelBootstrap := context.WithTimeout(ctx, defaultRetryProofTimeout)
	defer cancelBootstrap()
	browser, err := c.visibleProofBrowserFactory(bootstrapCtx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := browser.Close(); closeErr != nil {
			ui.Debugln("Could not close temporary Duck.ai browser: %v", closeErr)
		}
	}()
	headers, err := browser.Capture(bootstrapCtx)
	if err != nil {
		return nil, fmt.Errorf("clean Duck.ai browser could not capture a proof: %w", err)
	}
	if headers == nil || headers.VqdHash1 == "" {
		return nil, errors.New("clean Duck.ai browser returned no request proof")
	}
	requester, ok := browser.(browserPageRequester)
	if !ok {
		return nil, errBrowserPageRequestUnavailable
	}
	requestHeaders := browserrelay.FilterRequestHeaders(req.Header)
	requestHeaders.Set("x-vqd-hash-1", headers.VqdHash1)
	if headers.FeSignals != "" {
		requestHeaders.Set("x-fe-signals", headers.FeSignals)
	} else {
		requestHeaders.Del("x-fe-signals")
	}
	if headers.FeVersion != "" {
		requestHeaders.Set("x-fe-version", headers.FeVersion)
	} else {
		requestHeaders.Del("x-fe-version")
	}
	if headers.JourneyID != "" {
		requestHeaders.Set("x-ddg-journey-id", headers.JourneyID)
	} else {
		requestHeaders.Del("x-ddg-journey-id")
	}
	retryProgress(onProgress, ProgressUpdate{Stage: ui.ProgressConnecting, Label: "Retrying in a clean Duck.ai browser"}, "")
	response, err := requester.Request(ctx, browserrelay.Request{
		URL: req.URL.String(), Method: req.Method,
		Header: requestHeaders, Body: append([]byte(nil), jsonPayload...),
	})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("clean Duck.ai browser returned no response")
	}
	if response.StatusCode == http.StatusOK {
		return response, nil
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests {
		return nil, &RateLimitError{Code: rateLimitErrorCode(body)}
	}
	return nil, fmt.Errorf("clean Duck.ai browser returned HTTP %d", response.StatusCode)
}

func (c *Chat) buildPayload(durableStream *DurableStream) ChatPayload {
	reasoningEffort := "none"
	if c.Model == models.GPTOSS120B || c.Model == models.Gemma431B {
		reasoningEffort = "low"
	}
	payload := ChatPayload{
		Model:           c.Model,
		Messages:        c.Messages,
		CanUseTools:     c.NativeToolsEnabled && (c.NativeWebSearch || c.NativeImageGeneration),
		ReasoningEffort: reasoningEffort,
		DurableStream:   durableStream,
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

func redactDebugHeaders(headers http.Header) http.Header {
	redacted := headers.Clone()
	for _, name := range []string{
		"Authorization", "Cookie", "Set-Cookie", "X-Vqd-4", "X-Vqd-Hash-1", "X-Fe-Signals",
	} {
		if _, exists := redacted[http.CanonicalHeaderKey(name)]; exists {
			redacted.Set(name, "[REDACTED]")
		}
	}
	return redacted
}

func rateLimitErrorCode(body []byte) string {
	var fields map[string]any
	if json.Unmarshal(body, &fields) == nil {
		for _, key := range []string{"type", "code", "error"} {
			if value, ok := fields[key].(string); ok {
				if safe := safeErrorCode(value); safe != "" {
					return safe
				}
			}
		}
	}
	return safeErrorCode(strings.TrimSpace(string(body)))
}

func safeErrorCode(value string) string {
	if value == "" || len(value) > 80 {
		return ""
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' && char != '.' {
			return ""
		}
	}
	return value
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds <= 0 {
			return 0
		}
		maxSeconds := int64((time.Duration(1<<63 - 1)) / time.Second)
		if seconds > maxSeconds {
			return time.Duration(1<<63 - 1)
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

func shouldLogRequestDetails(c *Chat) bool {
	return os.Getenv("DEBUG") == "true" && (c == nil || !c.suppressSensitiveDebugLogs)
}

func shouldLogRequestPayload(c *Chat) bool {
	return shouldLogRequestDetails(c) && (c == nil || !c.suppressDebugPayload)
}

func marshalDebugPayload(payload ChatPayload) ([]byte, error) {
	redacted := payload
	redacted.Messages = append([]Message(nil), payload.Messages...)
	for i := range redacted.Messages {
		message := &redacted.Messages[i]
		if len(message.Images) == 0 {
			continue
		}
		for _, image := range message.Images {
			message.Content += fmt.Sprintf("\n[Image attachment: %s (%s); bytes redacted]", image.Name, image.MIMEType)
		}
		message.Images = nil
	}
	return json.Marshal(redacted)
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

	message := fmt.Sprintf("[URL Context]\nURL: %s\n\n%s", url, content.Content)
	c.Messages = append(c.Messages, Message{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	})
	c.recordContextMessage(message)

	if c.Analytics != nil {
		c.Analytics.RecordURLProcessed()
	}

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
	// Everything is laid out for the width PrintLogoBeside will actually give
	// it on this writer, so the layout cannot disagree with what gets drawn.
	textWidth := ui.TextWidth(color.Output)

	// Build the whole help as text lines so it can sit beside the logo; the
	// lines that do not fit fall below the image automatically.
	separatorWidth := textWidth
	if separatorWidth > 33 {
		separatorWidth = 33
	}

	lines := wrapHelpText("DuckDuckGo AI Chat CLI - Help", textWidth)
	lines = append(lines, strings.Repeat("-", separatorWidth))

	// Get commands from centralized registry
	commandsByCategory := command.GetCommandsByCategory()

	sections := []struct {
		title    string
		commands []CommandHelp
	}{
		{title: "Core Commands:", commands: helpRows(commandsByCategory["core"])},
		{title: "Context Commands:", commands: helpRows(commandsByCategory["context"])},
		{title: "Productivity Commands:", commands: helpRows(commandsByCategory["productivity"])},
		{
			title: "API Documentation:",
			commands: []CommandHelp{
				{Command: "GET /", Description: "Shows API documentation"},
				{Command: "POST /chat", Description: "Sends a message to the chat"},
				{Command: "GET /history", Description: "Retrieves the chat session history"},
			},
		},
	}

	for _, section := range sections {
		lines = append(lines, "", ui.AIColor.Sprint(section.title))
		lines = append(lines, renderCommandsTable(section.commands, textWidth)...)
	}

	lines = append(lines, "")
	for _, line := range wrapHelpText("Note: You can add '-- <your request>' after /search, /file, /url, or /library load to make an immediate request about the context.", textWidth) {
		lines = append(lines, ui.WarningColor.Sprint(line))
	}

	_ = ui.PrintLogoBeside(color.Output, lines)
}

// helpRows copies a command category into table rows, falling back to the
// command name when the registry carries no separate usage string.
func helpRows(commands []command.CommandInfo) []CommandHelp {
	rows := make([]CommandHelp, 0, len(commands))
	for _, cmd := range commands {
		usage := cmd.Usage
		if usage == "" {
			usage = cmd.Name
		}
		rows = append(rows, CommandHelp{Command: usage, Description: cmd.Description})
	}
	return rows
}

// printCommandsTable formats and prints a list of commands.
func printCommandsTable(commands []CommandHelp) {
	for _, line := range renderCommandsTable(commands, getTerminalWidthSafe()) {
		fmt.Println(line)
	}
}

// renderCommandsTable formats commands into lines no wider than width, keeping
// the themed command and description colours as ANSI sequences so the caller
// can place them next to the logo or print them on their own.
func renderCommandsTable(commands []CommandHelp, width int) []string {
	const indent = 2
	const columnGap = 2
	const minDescriptionWidth = 24

	// A command is laid out in columns when it leaves enough room for a
	// readable description beside it. Commands that do not are stacked one by
	// one instead, so a single long usage string cannot flatten the whole
	// section into the stacked layout.
	fitsColumn := func(command string) bool {
		return width-indent-len(command)-columnGap >= minDescriptionWidth
	}

	// The description column is sized by the commands that use it; the stacked
	// ones must not reserve space they never occupy.
	maxLength := 0
	anyColumn := false
	for _, cmd := range commands {
		if !fitsColumn(cmd.Command) {
			continue
		}
		anyColumn = true
		if len(cmd.Command) > maxLength {
			maxLength = len(cmd.Command)
		}
	}

	descriptionWidth := width - indent - 2
	if anyColumn {
		descriptionWidth = width - indent - maxLength - columnGap
	}

	var lines []string
	for _, cmd := range commands {
		if !anyColumn || !fitsColumn(cmd.Command) {
			for _, line := range wrapHelpText(cmd.Command, width-indent) {
				lines = append(lines, ui.AccentColor.Sprint("  "+line))
			}
			for _, line := range wrapHelpText(cmd.Description, descriptionWidth) {
				lines = append(lines, ui.WhiteColor.Sprint("    "+line))
			}
			continue
		}

		wrapped := wrapHelpText(cmd.Description, descriptionWidth)
		if len(wrapped) == 0 {
			wrapped = []string{""}
		}

		head := ui.AccentColor.Sprint(fmt.Sprintf("  %-*s", maxLength, cmd.Command)) +
			ui.WhiteColor.Sprint("  "+wrapped[0])
		lines = append(lines, head)

		indentPad := strings.Repeat(" ", indent+maxLength+columnGap)
		for _, line := range wrapped[1:] {
			lines = append(lines, ui.WhiteColor.Sprint(indentPad+line))
		}
	}

	return lines
}

func wrapHelpText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	lines := make([]string, 0, 1)
	line := ""
	for _, word := range words {
		wordRunes := []rune(word)
		for len(wordRunes) > width {
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			lines = append(lines, string(wordRunes[:width]))
			wordRunes = wordRunes[width:]
		}
		word = string(wordRunes)
		if line == "" {
			line = word
			continue
		}
		if len([]rune(line))+1+len([]rune(word)) <= width {
			line += " " + word
			continue
		}
		lines = append(lines, line)
		line = word
	}
	lines = append(lines, line)
	return lines
}

func HandleURLCommand(c *Chat, input string, cfg *config.Config, chainCtx *chatcontext.Context) {
	parsed, parseErr := command.Parse(input)
	if parseErr != nil || len(parsed.Commands) != 1 {
		ui.Errorln("Invalid URL command: %v", parseErr)
		return
	}
	urlStr := strings.TrimSpace(parsed.Commands[0].Args)
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
		if parsed.Prompt != "" {
			ui.Systemln("Processing your request about the URL...")
			ProcessInput(c, parsed.Prompt, cfg)
		} else {
			ui.Warningln("You can now ask questions about the URL content.")
		}
	}
}

func (c *Chat) addURLContext(url string, content string) {
	contentLength := len(content)
	if contentLength > 500 {
		ui.AIln("Adding %d characters from URL", contentLength)
	}

	message := fmt.Sprintf("[URL Context]\nURL: %s\n\n%s", url, content)
	c.Messages = append(c.Messages, Message{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	})
	c.recordContextMessage(message)
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
		if err := survey.AskOne(searchPrompt, &searchText, survey.WithStdio(os.Stdin, os.Stdout, os.Stderr)); err != nil {
			ui.Warningln("Export search canceled.")
			return
		}
		if searchText == "" {
			ui.Warningln("⚠️ Search text cannot be empty")
			return
		}
		filename, content = c.Export("search_conversation", searchText)
	default:
		ui.Warningln("Export canceled.")
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
			options[i] = fmt.Sprintf("%s (Started: %s, Messages: %d)", s.ID, s.StartTime.Format("2006-01-02 15:04"), s.Analytics.MessageCount)
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
		if err := c.SaveCurrentSession(); err != nil {
			ui.Warningln("Failed to save session before loading another: %v", err)
			return
		}
	}

	c.RestoreContext(session)
	ui.AIln("Session %s loaded successfully. Context restored.", sessionID)
}

func (c *Chat) ChangeModel(model models.Model) {
	c.Model = model
	if c.Analytics != nil {
		c.Analytics.RecordModelChange(string(model))
	}
	setTerminalTitle(fmt.Sprintf("DuckDuckGo Chat - %s", model))
	ui.AIln("Model changed to %s", model)
}

func (c *Chat) AddContextMessage(content string) {
	c.Messages = append(c.Messages, Message{
		Role:      "user",
		Content:   content,
		Timestamp: time.Now(),
	})
	c.recordContextMessage(content)
}

// AddContextMessageWithImages stores command-chain context and its attachments.
func (c *Chat) AddContextMessageWithImages(content string, images []media.ImageAttachment) {
	c.Messages = append(c.Messages, Message{
		Role:      "user",
		Content:   content,
		Images:    cloneImageAttachments(images),
		Timestamp: time.Now(),
	})
	c.recordContextMessage(content)
}

func (c *Chat) recordContextMessage(content string) {
	if c.Analytics != nil {
		c.Analytics.RecordMessage("context", len(content))
	}
}

// RestoreContext restores the chat context from a given conversation session.
func (c *Chat) RestoreContext(session *persistence.ConversationSession) {
	// Saved CLI history is restored locally. Start a fresh Duck.ai durable
	// conversation identity and send this transcript on the next chat request.
	c.resetDurableConversation()
	messages := session.Messages
	if len(session.OptimizedMessages) > 0 {
		messages = session.OptimizedMessages
	}
	// Convert persistence.Message to chat.Message
	c.Messages = make([]Message, len(messages))
	for i, msg := range messages {
		timestamp := time.Time{}
		if session.Version == persistence.SessionFormatVersion {
			timestamp = msg.Timestamp
		}
		c.Messages[i] = Message{
			Content:   msg.Content,
			Role:      msg.Role,
			Images:    cloneImageAttachments(msg.Images),
			Timestamp: timestamp,
		}
	}
	c.SessionID = session.ID
	c.ConversationStartTime = session.StartTime
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
			Images:    cloneImageAttachments(msg.Images),
			Timestamp: msg.Timestamp,
		}
	}
	return result
}

// convertFromIntelligenceMessages converts intelligence.Message back to Chat messages
func (c *Chat) convertFromIntelligenceMessages(messages []intelligence.Message) []Message {
	result := make([]Message, len(messages))
	for i, msg := range messages {
		result[i] = Message{
			Content:   msg.Content,
			Role:      msg.Role,
			Images:    cloneImageAttachments(msg.Images),
			Timestamp: msg.Timestamp,
		}
	}
	return result
}

// SaveCurrentSession synchronously archives the current conversation before a lifecycle transition.
func (c *Chat) SaveCurrentSession() error {
	if len(c.Messages) == 0 {
		return nil
	}
	if c.HistoryManager == nil {
		return fmt.Errorf("conversation history manager is unavailable")
	}

	// Convert messages to intelligence format
	intelligenceMessages := c.convertMessagesToIntelligence()

	// Create session object
	started := c.ConversationStartTime
	var sessionAnalytics analytics.Snapshot
	if c.Analytics != nil {
		sessionAnalytics = c.Analytics.Snapshot()
		if started.IsZero() && !sessionAnalytics.SessionStartTime.IsZero() {
			started = sessionAnalytics.SessionStartTime
		}
	}
	if started.IsZero() {
		started = time.Now()
	}
	session := &persistence.ConversationSession{
		ID:        c.SessionID,
		StartTime: started,
		Model:     string(c.Model),
		Messages:  intelligenceMessages,
		Analytics: persistence.SessionAnalytics{
			MessageCount:      len(c.Messages),
			TotalTokens:       sessionAnalytics.TotalTokensEstimate,
			APICallsCount:     sessionAnalytics.APICallsTotal,
			ErrorCount:        sessionAnalytics.APICallsFailed,
			OptimizationsUsed: sessionAnalytics.ContextOptimizations,
		},
	}
	return c.HistoryManager.SaveSession(session)
}

// ShowSessionStats displays analytics at the end of the session
func (c *Chat) ShowSessionStats() {
	if c.Analytics != nil {
		c.Analytics.DisplayStatistics()
	}
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
