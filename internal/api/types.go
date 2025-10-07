package api

import (
	"fmt"
	"time"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/models"
)

// API Request Types

// ChatRequest represents a chat message request
// @Description Chat message request payload
type ChatRequest struct {
	Message string `json:"message" binding:"required" example:"Hello, how are you?" minLength:"1" maxLength:"10000"`
	Model   string `json:"model,omitempty" example:"gpt-4o-mini"`
} // @name ChatRequest

// ModelChangeRequest represents a model change request
// @Description Model change request payload
type ModelChangeRequest struct {
	Model string `json:"model" binding:"required" example:"gpt-4o-mini" enum:"gpt-4o-mini,claude-3-haiku,llama,mixtral,o4mini"`
} // @name ModelChangeRequest

// API Response Types

// APIResponse represents the standard API response wrapper
// @Description Standard API response wrapper
type APIResponse struct {
	Success   bool        `json:"success" example:"true"`
	Message   string      `json:"message,omitempty" example:"Request processed successfully"`
	Data      interface{} `json:"data,omitempty"`
	Error     *APIError   `json:"error,omitempty"`
	Timestamp time.Time   `json:"timestamp" example:"2023-01-01T12:00:00Z"`
} // @name APIResponse

// ChatResponse represents a chat response
// @Description Chat response payload
type ChatResponse struct {
	Response  string       `json:"response" example:"Hello! I'm doing well, thank you for asking."`
	Model     string       `json:"model" example:"gpt-4o-mini"`
	MessageID string       `json:"message_id" example:"msg_123456"`
	Metadata  ChatMetadata `json:"metadata"`
} // @name ChatResponse

// ChatMetadata contains additional information about the chat response
// @Description Metadata about the chat response
type ChatMetadata struct {
	ProcessingTime    int64 `json:"processing_time_ms" example:"1500"`
	TokensEstimate    int   `json:"tokens_estimate" example:"45"`
	ConversationCount int   `json:"conversation_count" example:"3"`
} // @name ChatMetadata

// HistoryResponse represents the chat history response
// @Description Chat history response payload
type HistoryResponse struct {
	Messages      []MessageResponse `json:"messages"`
	TotalMessages int               `json:"total_messages" example:"10"`
	SessionID     string            `json:"session_id" example:"session_123456"`
	Model         string            `json:"model" example:"gpt-4o-mini"`
} // @name HistoryResponse

// MessageResponse represents a single message in the history
// @Description Individual message in chat history
type MessageResponse struct {
	ID        string    `json:"id" example:"msg_123456"`
	Role      string    `json:"role" example:"user" enum:"user,assistant"`
	Content   string    `json:"content" example:"Hello, how are you?"`
	Timestamp time.Time `json:"timestamp" example:"2023-01-01T12:00:00Z"`
} // @name MessageResponse

// ModelInfo represents available model information
// @Description Information about an available model
type ModelInfo struct {
	ID          string `json:"id" example:"gpt-4o-mini"`
	Name        string `json:"name" example:"GPT-4o-mini"`
	Description string `json:"description" example:"Fast and efficient model for general conversations"`
	IsDefault   bool   `json:"is_default" example:"true"`
} // @name ModelInfo

// ModelsResponse represents the available models response
// @Description Available models response payload
type ModelsResponse struct {
	Models       []ModelInfo `json:"models"`
	CurrentModel string      `json:"current_model" example:"gpt-4o-mini"`
	TotalModels  int         `json:"total_models" example:"5"`
} // @name ModelsResponse

// APIError represents API error details
// @Description API error information
type APIError struct {
	Code    string `json:"code" example:"VALIDATION_ERROR"`
	Message string `json:"message" example:"Invalid request parameters"`
	Details string `json:"details,omitempty" example:"Field 'message' is required"`
} // @name APIError

// HealthResponse represents the health check response
// @Description Health check response payload
type HealthResponse struct {
	Status    string            `json:"status" example:"healthy"`
	Version   string            `json:"version" example:"1.0.0"`
	Uptime    int64             `json:"uptime_seconds" example:"3600"`
	Services  map[string]string `json:"services"`
	Timestamp time.Time         `json:"timestamp" example:"2023-01-01T12:00:00Z"`
} // @name HealthResponse

// Common Error Codes
const (
	ErrorCodeValidation     = "VALIDATION_ERROR"
	ErrorCodeNotFound       = "NOT_FOUND"
	ErrorCodeInternal       = "INTERNAL_ERROR"
	ErrorCodeRateLimit      = "RATE_LIMIT_EXCEEDED"
	ErrorCodeModelNotFound  = "MODEL_NOT_FOUND"
	ErrorCodeChatError      = "CHAT_ERROR"
	ErrorCodeInvalidSession = "INVALID_SESSION"
)

// Helper functions for creating standardized responses

// NewSuccessResponse creates a successful API response
func NewSuccessResponse(data interface{}, message string) APIResponse {
	return APIResponse{
		Success:   true,
		Message:   message,
		Data:      data,
		Timestamp: time.Now(),
	}
}

// NewErrorResponse creates an error API response
func NewErrorResponse(code, message, details string) APIResponse {
	return APIResponse{
		Success: false,
		Error: &APIError{
			Code:    code,
			Message: message,
			Details: details,
		},
		Timestamp: time.Now(),
	}
}

// ConvertChatMessagesToResponse converts internal chat messages to API response format
func ConvertChatMessagesToResponse(messages []chat.Message, sessionID string, currentModel models.Model) HistoryResponse {
	messageResponses := make([]MessageResponse, len(messages))
	for i, msg := range messages {
		messageResponses[i] = MessageResponse{
			ID:        generateMessageID(i),
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: time.Now().Add(-time.Duration(len(messages)-i) * time.Minute), // Estimate timestamps
		}
	}

	return HistoryResponse{
		Messages:      messageResponses,
		TotalMessages: len(messages),
		SessionID:     sessionID,
		Model:         string(currentModel),
	}
}

// generateMessageID generates a unique message ID
func generateMessageID(index int) string {
	return fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), index)
}

// GetAvailableModels returns information about all available models
func GetAvailableModels() []ModelInfo {
	return []ModelInfo{
		{
			ID:          "gpt-4o-mini",
			Name:        "GPT-4o-mini",
			Description: "Fast and efficient model for general conversations",
			IsDefault:   true,
		},
		{
			ID:          "claude-3-haiku",
			Name:        "Claude-3-haiku",
			Description: "Anthropic's Claude 3 Haiku model for thoughtful responses",
			IsDefault:   false,
		},
		{
			ID:          "llama",
			Name:        "Llama 3.3",
			Description: "Meta's Llama 3.3 70B model for advanced reasoning",
			IsDefault:   false,
		},
		{
			ID:          "mixtral",
			Name:        "Mistral Small 3",
			Description: "Mistral's efficient small model for quick responses",
			IsDefault:   false,
		},
		{
			ID:          "gpt-5",
			Name:        "gpt-5-mini",
			Description: "Compact and efficient model for basic interactions",
			IsDefault:   false,
		},
	}
}
