package chat

import (
	"bufio"
	"bytes"
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
}

type Message struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type ToolChoice struct {
	NewsSearch      bool `json:"NewsSearch"`
	VideosSearch    bool `json:"VideosSearch"`
	LocalSearch     bool `json:"LocalSearch"`
	WeatherForecast bool `json:"WeatherForecast"`
}

type Metadata struct {
	ToolChoice ToolChoice `json:"toolChoice"`
}

type ChatPayload struct {
	Model                models.Model `json:"model"`
	Metadata             Metadata     `json:"metadata"`
	Messages             []Message    `json:"messages"`
	CanUseTools          bool         `json:"canUseTools"`
	CanUseApproxLocation bool         `json:"canUseApproxLocation"`
}

func InitializeSession(cfg *config.Config) *Chat {
	model := models.GetModel(cfg.DefaultModel)
	vqd, vqdHash1, feSignals, feVersion := GetVQD()
	chat := NewChat(vqd, vqdHash1, feSignals, feVersion, model, cfg)
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
	u, _ := url.Parse("https://duckduckgo.com")
	cookies := []*http.Cookie{
		{Name: "5", Value: "1", Domain: ".duckduckgo.com"},
		{Name: "dcm", Value: "3", Domain: ".duckduckgo.com"},
		{Name: "dcs", Value: "1", Domain: ".duckduckgo.com"},
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
	}

	// Record initial model
	analytics.RecordModelChange(string(model))

	ui.AIln("🧠 Intelligent features enabled: Analytics, Context Optimization, History Management")

	return chat
}

func GetVQD() (string, string, string, string) {
	// Simple approach like the working PowerShell script
	// Use static headers that work, no complex challenges
	ui.Warningln("⌛ Getting VQD from status API (simple approach like working PS1 script)...")

	client := &http.Client{Timeout: 10 * time.Second}

	// Set up cookies avec les cookies minimum nécessaires (comme dans le script PS1)
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse("https://duckduckgo.com")
	cookies := []*http.Cookie{
		{Name: "5", Value: "1", Domain: ".duckduckgo.com"},
		{Name: "dcm", Value: "3", Domain: ".duckduckgo.com"},
		{Name: "dcs", Value: "1", Domain: ".duckduckgo.com"},
	}
	jar.SetCookies(u, cookies)
	client.Jar = jar

	// Direct GET to /status with exact headers from working PS1 script
	req, _ := http.NewRequest("GET", models.StatusURL, nil)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	req.Header.Set("Accept-Language", "fr-FR,fr;q=0.6")
	req.Header.Set("Authority", "duckduckgo.com")
	req.Header.Set("Cache-Control", "no-store")
	req.Header.Set("DNT", "1")
	req.Header.Set("Method", "GET")
	req.Header.Set("Path", "/duckchat/v1/status")
	req.Header.Set("Priority", "u=1, i")
	req.Header.Set("Referer", "https://duckduckgo.com/")
	req.Header.Set("Scheme", "https")
	req.Header.Set("Sec-CH-UA", `"Not)A;Brand";v="8", "Chromium";v="138", "Brave";v="138"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-GPC", "1")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")
	req.Header.Set("x-vqd-accept", "1")

	resp, err := client.Do(req)
	if err != nil {
		ui.Errorln("Error fetching VQD: %v", err)
		return "", "", "", ""
	}
	defer resp.Body.Close()

	// Le VQD header de la status API pour x-vqd-4
	vqdHeader := resp.Header.Get("x-vqd-hash-1")
	if vqdHeader == "" {
		ui.Errorln("No VQD header found in response")
		return "", "", "", ""
	}

	// Return the VQD and static headers that work in PowerShell script
	vqd := vqdHeader
	vqdHash1 := "eyJzZXJ2ZXJfaGFzaGVzIjpbImRQSlJJTWczZnFYQXIvaStaa3c2cEpFVzEwckdTdmxJVlVkNlFsOVRGWXc9IiwiMUN3Qzg3N0Q3WXE1dzlEeTc4UjhBVi9qZVZWaUlYbmV0Q0xvckx3c01QZz0iLCJQSzc3TGc2L25weDdWQ2J2UWxsTEhBR3cyenJIVmEvQUFBRFBhQTl1ekVRPSJdLCJjbGllbnRfaGFzaGVzIjpbImxWblI0MStCMVFWZ0o4d0hhMUdBNmdxR0JoSjlWdjN5K0dISkdGekJmTGM9IiwiVS9RRUc2RE1qdEU4V2hHU1FxOUU1Z0VGNmw1SWJrNk9NVlBuY01DU1licz0iLCJ6SURsYUNvZG9JUjNwbTNSVTlWOUJXaUJkZDJqenRMODAyN0VYTHhkWll3PSJdLCJzaWduYWxzIjp7fSwibWV0YSI6eyJ2IjoiNCIsImNoYWxsZW5nZV9pZCI6ImM4M2Q0ZTc5NTU2MjJmZjU3Mzc0ZDUzOTk2ZjliMmJhZGE2ZDQxZTMzNDM1ZjVlNzMyYjFmNmZjNmQ0ZTE1NzVoOGpidCIsInRpbWVzdGFtcCI6IjE3NTIxNTU3Nzc4NjYiLCJvcmlnaW4iOiJodHRwczovL2R1Y2tkdWNrZ28uY29tIiwic3RhY2siOiJFcnJvclxuYXQgRSAoaHR0cHM6Ly9kdWNrZHVja2dvLmNvbS9kaXN0L3dwbS5jaGF0LjcwZWFjYTZhZWEyOTQ4YjBiYjYwLmpzOjE6MTQ4MjUpXG5hdCBhc3luYyBodHRwczovL2R1Y2tkdWNrZ28uY29tL2Rpc3Qvd3BtLmNoYXQuNzBlYWNhNmFlYTI5NDhiMGJiNjAuanM6MToxNjk4NSIsImR1cmF0aW9uIjoiNTgifX0="
	feSignals := "eyJzdGFydCI6MTc1MjE1NTc3NzQ4MCwiZXZlbnRzIjpbeyJuYW1lIjoic3RhcnROZXdDaGF0IiwiZGVsdGEiOjc1fSx7Im5hbWUiOiJyZWNlbnRDaGF0c0xpc3RJbXByZXNzaW9uIiwiZGVsdGEiOjEyNH1dLCJlbmQiOjQzNDN9"
	feVersion := "serp_20250710_090702_ET-70eaca6aea2948b0bb60"

	ui.AIln("✅ Successfully got VQD and all required headers")
	return vqd, vqdHash1, feSignals, feVersion
}

func (c *Chat) Clear(cfg *config.Config) {
	// Save current session before clearing if it has content
	if len(c.Messages) > 0 {
		c.saveCurrentSession()
	}

	clearTerminal()

	if len(c.Messages) > 0 {
		c.Messages = []Message{}
		newVqd, newVqdHash1, newFeSignals, newFeVersion := GetVQD()
		c.NewVqd = newVqd
		c.OldVqd = c.NewVqd
		c.VqdHash1 = newVqdHash1
		c.FeSignals = newFeSignals
		c.FeVersion = newFeVersion
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
		"gpt-4o-mini":                               "gpt-4o-mini",
		"claude-3-5-haiku-latest":                   "claude-3-haiku",
		"meta-llama/Llama-4-Scout-17B-16E-Instruct":   "llama",
		"mistralai/Mistral-Small-24B-Instruct-2501": "mixtral",
		"gpt-5-mini": "gpt-5",
		"openai/gpt-oss-120b": "gpt-oss",
	}

	if shortName, exists := displayNames[model]; exists {
		return string(shortName)
	}

	return "unknown"
}

func (c *Chat) FetchStream(content string) (<-chan string, error) {
	resp, err := c.Fetch(content)
	if err != nil {
		return nil, err
	}

	stream := make(chan string)
	go func() {
		defer resp.Body.Close()
		defer close(stream)

		scanner := bufio.NewScanner(resp.Body)

		for scanner.Scan() {
			line := scanner.Text()

			if line == "data: [DONE]" {
				break
			}

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				var messageData struct {
					Role    string `json:"role"`
					Message string `json:"message"`
					Created int64  `json:"created"`
					ID      string `json:"id"`
					Action  string `json:"action"`
					Model   string `json:"model"`
				}
				if err := json.Unmarshal([]byte(data), &messageData); err != nil {
					log.Printf("Error unmarshaling data: %v\n", err)
					continue
				}

				if messageData.Message != "" {
					stream <- messageData.Message
				}
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

func (c *Chat) Fetch(content string) (*http.Response, error) {
	startTime := time.Now()
	if c.NewVqd == "" {
		newVqd, newVqdHash1, newFeSignals, newFeVersion := GetVQD()
		c.NewVqd = newVqd
		c.VqdHash1 = newVqdHash1
		c.FeSignals = newFeSignals
		c.FeVersion = newFeVersion
		if c.NewVqd == "" {
			return nil, fmt.Errorf("failed to get VQD")
		}
	}

	// VQD hash is now initialized during chat creation

	payload := ChatPayload{
		Model: c.Model,
		Metadata: Metadata{
			ToolChoice: ToolChoice{
				NewsSearch:      false,
				VideosSearch:    false,
				LocalSearch:     false,
				WeatherForecast: false,
			},
		},
		Messages:             c.Messages,
		CanUseTools:          true,
		CanUseApproxLocation: true,
	}

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
	req.Header.Set("Accept-Language", "fr-FR,fr;q=0.6")
	req.Header.Set("Authority", "duckduckgo.com")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DNT", "1")
	req.Header.Set("Method", "POST")
	req.Header.Set("Origin", "https://duckduckgo.com")
	req.Header.Set("Path", "/duckchat/v1/chat")
	req.Header.Set("Priority", "u=1, i")
	req.Header.Set("Referer", "https://duckduckgo.com/")
	req.Header.Set("Scheme", "https")
	req.Header.Set("Sec-CH-UA", `"Not)A;Brand";v="8", "Chromium";v="138", "Brave";v="138"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Sec-GPC", "1")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36")

	// ALL VQD-related headers from the real web browser request
	req.Header.Set("x-vqd-4", c.NewVqd)
	if c.FeSignals != "" {
		req.Header.Set("x-fe-signals", c.FeSignals)
	}
	if c.FeVersion != "" {
		req.Header.Set("x-fe-version", c.FeVersion)
	}
	if c.VqdHash1 != "" {
		req.Header.Set("x-vqd-hash-1", c.VqdHash1)
	}

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
		if resp.StatusCode == 418 || resp.StatusCode == 429 || strings.Contains(string(body), "ERR_INVALID_VQD") {
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
			newVqd, newVqdHash1, newFeSignals, newFeVersion := GetVQD()
			if newVqd != "" {
				c.NewVqd = newVqd
				c.VqdHash1 = newVqdHash1
				c.FeSignals = newFeSignals
				c.FeVersion = newFeVersion
				ui.AIln("✅ Refreshed VQD: %s...", c.NewVqd[:50])
			}
			c.Analytics.RecordVQDRefresh()

			if c.NewVqd != "" && c.RetryCount < 3 {
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
