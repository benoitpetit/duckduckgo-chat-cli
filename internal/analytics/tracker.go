package analytics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"duckduckgo-chat-cli/internal/ui"
)

// ChatAnalytics tracks various metrics during a chat session
type ChatAnalytics struct {
	// Session info
	SessionStartTime time.Time     `json:"session_start_time"`
	SessionEndTime   time.Time     `json:"session_end_time"`
	SessionDuration  time.Duration `json:"session_duration"`

	// Chat Interactions (CLI usage)
	ChatInteractionsTotal      int           `json:"chat_interactions_total"`
	ChatInteractionsSuccessful int           `json:"chat_interactions_successful"`
	ChatInteractionsFailed     int           `json:"chat_interactions_failed"`
	TotalChatResponseTime      time.Duration `json:"total_chat_response_time"`
	AverageChatResponseTime    time.Duration `json:"average_chat_response_time"`

	// REST API Metrics (HTTP calls)
	APICallsTotal          int           `json:"api_calls_total"`
	APICallsSuccessful     int           `json:"api_calls_successful"`
	APICallsFailed         int           `json:"api_calls_failed"`
	TotalAPIResponseTime   time.Duration `json:"total_api_response_time"`
	AverageAPIResponseTime time.Duration `json:"average_api_response_time"`

	// Error Tracking
	Error418Count      int `json:"error_418_count"`
	Error429Count      int `json:"error_429_count"`
	OtherErrorsCount   int `json:"other_errors_count"`
	VQDRefreshCount    int `json:"vqd_refresh_count"`
	HeaderRefreshCount int `json:"header_refresh_count"`

	// Content Metrics
	MessagesTotal       int `json:"messages_total"`
	UserMessages        int `json:"user_messages"`
	AssistantMessages   int `json:"assistant_messages"`
	ContextMessages     int `json:"context_messages"`
	TotalTokensEstimate int `json:"total_tokens_estimate"`

	// Context Optimization
	ContextOptimizations int   `json:"context_optimizations"`
	ContextCompressions  int   `json:"context_compressions"`
	BytesSaved           int64 `json:"bytes_saved"`

	// Commands Usage
	CommandsUsed map[string]int `json:"commands_used"`

	// Model Changes
	ModelChanges int    `json:"model_changes"`
	CurrentModel string `json:"current_model"`

	// Files and URLs
	FilesProcessed    int `json:"files_processed"`
	URLsProcessed     int `json:"urls_processed"`
	SearchesPerformed int `json:"searches_performed"`

	byModel map[string]ModelMetrics

	mutex sync.RWMutex
}

// NewChatAnalytics creates a new analytics tracker
func NewChatAnalytics() *ChatAnalytics {
	return &ChatAnalytics{
		SessionStartTime: time.Now(),
		CommandsUsed:     make(map[string]int),
		byModel:          make(map[string]ModelMetrics),
		mutex:            sync.RWMutex{},
	}
}

// RecordModelInteraction records one response attempt against its selected model.
func (ca *ChatAnalytics) RecordModelInteraction(model string, duration time.Duration, success bool, errorType string) {
	if model == "" {
		return
	}
	ca.mutex.Lock()
	defer ca.mutex.Unlock()
	if ca.byModel == nil {
		ca.byModel = make(map[string]ModelMetrics)
	}
	metrics := ca.byModel[model]
	metrics.Interactions++
	metrics.TotalResponseTime += duration
	metrics.AverageResponseTime = metrics.TotalResponseTime / time.Duration(metrics.Interactions)
	if success {
		metrics.Successful++
	} else {
		metrics.Failed++
		switch errorType {
		case "418":
			metrics.Error418++
		case "429":
			metrics.Error429++
		default:
			metrics.OtherErrors++
		}
	}
	ca.byModel[model] = metrics
}

// Chat Interaction Tracking (for CLI usage)
func (ca *ChatAnalytics) RecordChatInteraction(duration time.Duration, success bool, errorType string) {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.ChatInteractionsTotal++
	ca.TotalChatResponseTime += duration
	ca.AverageChatResponseTime = ca.TotalChatResponseTime / time.Duration(ca.ChatInteractionsTotal)

	if success {
		ca.ChatInteractionsSuccessful++
	} else {
		ca.ChatInteractionsFailed++
		switch errorType {
		case "418":
			ca.Error418Count++
		case "429":
			ca.Error429Count++
		default:
			ca.OtherErrorsCount++
		}
	}
}

// REST API Call Tracking (for HTTP requests only)
func (ca *ChatAnalytics) RecordAPICall(duration time.Duration, success bool, errorType string) {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.APICallsTotal++
	ca.TotalAPIResponseTime += duration
	ca.AverageAPIResponseTime = ca.TotalAPIResponseTime / time.Duration(ca.APICallsTotal)

	if success {
		ca.APICallsSuccessful++
	} else {
		ca.APICallsFailed++
		switch errorType {
		case "validation":
		case "model_not_found":
		case "chat_error":
		default:
			// For API errors, we don't track 418/429 since those are DuckDuckGo backend errors
		}
	}
}

// Message Tracking
func (ca *ChatAnalytics) RecordMessage(role string, contentLength int) {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.MessagesTotal++
	ca.TotalTokensEstimate += estimateTokens(contentLength)

	switch role {
	case "user":
		ca.UserMessages++
	case "assistant":
		ca.AssistantMessages++
	default:
		ca.ContextMessages++
	}
}

// Command Usage Tracking
func (ca *ChatAnalytics) RecordCommand(command string) {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	if ca.CommandsUsed == nil {
		ca.CommandsUsed = make(map[string]int)
	}
	ca.CommandsUsed[command]++
}

// Context Operations Tracking
func (ca *ChatAnalytics) RecordContextOptimization(bytesSaved int64) {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.ContextOptimizations++
	ca.BytesSaved += bytesSaved
}

func (ca *ChatAnalytics) RecordContextCompression() {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.ContextCompressions++
}

// Error Recovery Tracking
func (ca *ChatAnalytics) RecordVQDRefresh() {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.VQDRefreshCount++
}

func (ca *ChatAnalytics) RecordHeaderRefresh() {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.HeaderRefreshCount++
}

// Content Processing Tracking
func (ca *ChatAnalytics) RecordFileProcessed() {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.FilesProcessed++
}

func (ca *ChatAnalytics) RecordURLProcessed() {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.URLsProcessed++
}

func (ca *ChatAnalytics) RecordSearchPerformed() {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.SearchesPerformed++
}

func (ca *ChatAnalytics) RecordModelChange(newModel string) {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.ModelChanges++
	ca.CurrentModel = newModel
}

// Session Management
func (ca *ChatAnalytics) EndSession() {
	ca.mutex.Lock()
	defer ca.mutex.Unlock()

	ca.SessionEndTime = time.Now()
	ca.SessionDuration = ca.SessionEndTime.Sub(ca.SessionStartTime)
}

// Display comprehensive statistics
func (ca *ChatAnalytics) DisplayStatistics() {
	ca.EndSession()

	ca.mutex.RLock()
	defer ca.mutex.RUnlock()

	ui.Systemln("\n" + strings.Repeat("-", 50))
	ui.Systemln("SESSION ANALYTICS SUMMARY")
	ui.Systemln(strings.Repeat("-", 50))

	// Session Overview
	ui.AIln("Session Overview:")
	ui.Whiteln("  Duration: %s", formatDuration(ca.SessionDuration))
	ui.Whiteln("  Messages: %d total (%d user, %d AI)", ca.MessagesTotal, ca.UserMessages, ca.AssistantMessages)
	ui.Whiteln("  Estimated Tokens: ~%d", ca.TotalTokensEstimate)

	// Chat Performance (CLI interactions)
	if ca.ChatInteractionsTotal > 0 {
		ui.AIln("\nChat Performance:")
		ui.Whiteln("  Interactions: %d total (%d successful, %d failed)", ca.ChatInteractionsTotal, ca.ChatInteractionsSuccessful, ca.ChatInteractionsFailed)
		ui.Whiteln("  Success Rate: %.1f%%", ca.getChatSuccessRate())
		ui.Whiteln("  Average Response Time: %s", formatDuration(ca.AverageChatResponseTime))

		// Error Details (only if there are errors)
		if ca.ChatInteractionsFailed > 0 {
			ui.Warningln("  Errors: 418=%d, 429=%d, Other=%d", ca.Error418Count, ca.Error429Count, ca.OtherErrorsCount)
		}
	}

	// REST API Usage (only if there were actual API calls)
	if ca.APICallsTotal > 0 {
		ui.AIln("\nREST API Usage:")
		ui.Whiteln("  Calls: %d total (%d successful, %d failed)", ca.APICallsTotal, ca.APICallsSuccessful, ca.APICallsFailed)
		ui.Whiteln("  Success Rate: %.1f%%", ca.getAPISuccessRate())
		ui.Whiteln("  Average Response Time: %s", formatDuration(ca.AverageAPIResponseTime))
	}

	// Content Processing
	if ca.FilesProcessed > 0 || ca.URLsProcessed > 0 || ca.SearchesPerformed > 0 {
		ui.AIln("\nContent Processing:")
		if ca.FilesProcessed > 0 {
			ui.Whiteln("  Files: %d", ca.FilesProcessed)
		}
		if ca.URLsProcessed > 0 {
			ui.Whiteln("  URLs: %d", ca.URLsProcessed)
		}
		if ca.SearchesPerformed > 0 {
			ui.Whiteln("  Searches: %d", ca.SearchesPerformed)
		}
	}

	// Context Optimization
	if ca.ContextOptimizations > 0 || ca.ContextCompressions > 0 {
		ui.AIln("\nContext Intelligence:")
		ui.Whiteln("  Optimizations: %d", ca.ContextOptimizations)
		ui.Whiteln("  Compressions: %d", ca.ContextCompressions)
		ui.Whiteln("  Bytes Saved: %s", formatBytes(ca.BytesSaved))
	}

	// Commands Usage
	if len(ca.CommandsUsed) > 0 {
		ui.AIln("\nCommands Used:")
		for cmd, count := range ca.CommandsUsed {
			ui.Whiteln("  %s: %d", cmd, count)
		}
	}

	// Model Info
	if ca.CurrentModel != "" {
		ui.AIln("\nModel: %s", ca.CurrentModel)
		if ca.ModelChanges > 0 {
			ui.Whiteln("  Changes: %d", ca.ModelChanges)
		}
	}

	// Performance Summary
	ui.AIln("\nPerformance Score: %.1f%% | Messages/min: %.1f", ca.getEfficiencyScore(), ca.getMessagesPerMinute())

	ui.Systemln(strings.Repeat("-", 50) + "\n")
}

// Save analytics to file for future analysis
func (ca *ChatAnalytics) SaveToFile(exportDir string) error {
	ca.EndSession()

	filename := fmt.Sprintf("analytics_%s.json", time.Now().Format("20060102_150405"))
	filepath := filepath.Join(exportDir, filename)

	data, err := json.MarshalIndent(ca, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return err
	}

	return os.WriteFile(filepath, data, 0644)
}

// Helper functions
func (ca *ChatAnalytics) getChatSuccessRate() float64 {
	if ca.ChatInteractionsTotal == 0 {
		return 0.0
	}
	return (float64(ca.ChatInteractionsSuccessful) / float64(ca.ChatInteractionsTotal)) * 100
}

func (ca *ChatAnalytics) getAPISuccessRate() float64 {
	if ca.APICallsTotal == 0 {
		return 0.0
	}
	return (float64(ca.APICallsSuccessful) / float64(ca.APICallsTotal)) * 100
}

func (ca *ChatAnalytics) getSuccessRate() float64 {
	// For backwards compatibility, prioritize chat interactions for efficiency score
	if ca.ChatInteractionsTotal > 0 {
		return ca.getChatSuccessRate()
	}
	return ca.getAPISuccessRate()
}

func (ca *ChatAnalytics) getEfficiencyScore() float64 {
	score := 0.0
	factors := 0

	// Success rate factor (40% of score) - prioritize chat interactions
	successRate := ca.getSuccessRate()
	if successRate > 0 {
		score += successRate * 0.4
		factors++
	}

	// Response time factor (30% of score) - use chat response time primarily
	var avgResponseTime time.Duration
	if ca.ChatInteractionsTotal > 0 {
		avgResponseTime = ca.AverageChatResponseTime
	} else if ca.APICallsTotal > 0 {
		avgResponseTime = ca.AverageAPIResponseTime
	}

	if avgResponseTime > 0 {
		// Good response time is under 3 seconds
		timeScore := 100.0
		if avgResponseTime > 3*time.Second {
			timeScore = 100.0 * (3000.0 / float64(avgResponseTime.Milliseconds()))
		}
		if timeScore > 100 {
			timeScore = 100
		}
		score += timeScore * 0.3
		factors++
	}

	// Optimization factor (30% of score)
	if ca.MessagesTotal > 0 {
		optimizationScore := 50.0 // Base score
		if ca.ContextOptimizations > 0 {
			optimizationScore += 50.0 * (float64(ca.ContextOptimizations) / float64(ca.MessagesTotal))
		}
		if optimizationScore > 100 {
			optimizationScore = 100
		}
		score += optimizationScore * 0.3
		factors++
	}

	if factors == 0 {
		return 0.0
	}
	return score
}

func (ca *ChatAnalytics) getMessagesPerMinute() float64 {
	if ca.SessionDuration == 0 {
		return 0.0
	}
	minutes := ca.SessionDuration.Minutes()
	if minutes == 0 {
		return 0.0
	}
	return float64(ca.UserMessages) / minutes
}

func estimateTokens(contentLength int) int {
	// Rough estimation: 4 characters per token on average
	return contentLength / 4
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}

func formatBytes(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}
