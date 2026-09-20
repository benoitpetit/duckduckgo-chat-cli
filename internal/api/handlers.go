package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/ui"

	"github.com/gin-gonic/gin"
)

var startTime = time.Now()

// ChatHandler handles chat requests
// @Summary      Send a chat message
// @Description  Send a message to the AI and receive a response
// @Tags         Chat
// @Accept       json
// @Produce      json
// @Param        request body ChatRequest true "Chat message request"
// @Success      200 {object} APIResponse{data=ChatResponse} "Successful chat response"
// @Failure      400 {object} APIResponse{error=APIError} "Invalid request"
// @Failure      500 {object} APIResponse{error=APIError} "Internal server error"
// @Router       /chat [post]
func ChatHandler(chatSession *chat.Chat, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req ChatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response := NewErrorResponse(ErrorCodeValidation, "Invalid request payload", err.Error())
			c.JSON(http.StatusBadRequest, response)
			return
		}

		if req.Message == "" {
			response := NewErrorResponse(ErrorCodeValidation, "Message cannot be empty", "Field 'message' is required and cannot be empty")
			c.JSON(http.StatusBadRequest, response)
			return
		}

		// Change model if specified
		if req.Model != "" {
			if newModel := models.GetModel(req.Model); newModel != chatSession.Model {
				chatSession.ChangeModel(newModel)
			}
		}

		// Log the request if enabled
		if cfg.API.LogRequests {
			ui.APILog("Received chat request from %s: '%s'", c.ClientIP(), req.Message)
		}

		// Process the chat message
		startTime := time.Now()
		response, err := chat.ProcessInputAndReturn(chatSession, req.Message, cfg)
		processingTime := time.Since(startTime)

		// Track API call in analytics
		if chatSession.Analytics != nil {
			chatSession.Analytics.RecordAPICall(processingTime, err == nil, "")
		}

		if err != nil {
			errorResponse := NewErrorResponse(ErrorCodeChatError, "Error processing chat message", err.Error())
			c.JSON(http.StatusInternalServerError, errorResponse)
			return
		}

		// Create response with metadata
		chatResponse := ChatResponse{
			Response:  response,
			Model:     string(chatSession.Model),
			MessageID: generateMessageID(len(chatSession.Messages)),
			Metadata: ChatMetadata{
				ProcessingTime:    processingTime.Milliseconds(),
				TokensEstimate:    estimateTokens(response),
				ConversationCount: len(chatSession.Messages),
			},
		}

		// Log the response if enabled
		if cfg.API.LogRequests {
			responseSnippet := response
			if len(responseSnippet) > 100 {
				responseSnippet = responseSnippet[:100] + "..."
			}
			ui.APILog("Sending response snippet to %s: '%s'", c.ClientIP(), responseSnippet)
		}

		successResponse := NewSuccessResponse(chatResponse, "Chat message processed successfully")
		c.JSON(http.StatusOK, successResponse)
	}
}

// HistoryHandler handles chat history requests
// @Summary      Get chat history
// @Description  Retrieve the complete chat session history
// @Tags         Chat
// @Produce      json
// @Param        limit query int false "Maximum number of messages to return" default(50)
// @Param        offset query int false "Number of messages to skip" default(0)
// @Success      200 {object} APIResponse{data=HistoryResponse} "Chat history retrieved successfully"
// @Failure      400 {object} APIResponse{error=APIError} "Invalid query parameters"
// @Router       /history [get]
func HistoryHandler(chatSession *chat.Chat) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// Parse query parameters
		limit := 50 // default
		offset := 0 // default

		if limitParam := c.Query("limit"); limitParam != "" {
			if parsedLimit, err := strconv.Atoi(limitParam); err == nil && parsedLimit > 0 {
				if parsedLimit > 1000 { // Maximum limit
					limit = 1000
				} else {
					limit = parsedLimit
				}
			}
		}

		if offsetParam := c.Query("offset"); offsetParam != "" {
			if parsedOffset, err := strconv.Atoi(offsetParam); err == nil && parsedOffset >= 0 {
				offset = parsedOffset
			}
		}

		// Apply pagination
		messages := chatSession.Messages
		totalMessages := len(messages)

		if offset >= totalMessages {
			messages = []chat.Message{}
		} else {
			end := offset + limit
			if end > totalMessages {
				end = totalMessages
			}
			messages = messages[offset:end]
		}

		// Convert to response format
		historyResponse := ConvertChatMessagesToResponse(messages, chatSession.SessionID, chatSession.Model)
		historyResponse.TotalMessages = totalMessages // Set the actual total

		// Track API call in analytics
		if chatSession.Analytics != nil {
			chatSession.Analytics.RecordAPICall(time.Since(startTime), true, "")
		}

		successResponse := NewSuccessResponse(historyResponse, "Chat history retrieved successfully")
		c.JSON(http.StatusOK, successResponse)
	}
}

// ModelsHandler handles available models requests
// @Summary      Get available models
// @Description  Retrieve list of all available AI models
// @Tags         Models
// @Produce      json
// @Success      200 {object} APIResponse{data=ModelsResponse} "Available models retrieved successfully"
// @Router       /models [get]
func ModelsHandler(chatSession *chat.Chat) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		availableModels := GetAvailableModels()

		modelsResponse := ModelsResponse{
			Models:       availableModels,
			CurrentModel: string(chatSession.Model),
			TotalModels:  len(availableModels),
		}

		// Track API call in analytics
		if chatSession.Analytics != nil {
			chatSession.Analytics.RecordAPICall(time.Since(startTime), true, "")
		}

		successResponse := NewSuccessResponse(modelsResponse, "Available models retrieved successfully")
		c.JSON(http.StatusOK, successResponse)
	}
}

// ModelChangeHandler handles model change requests
// @Summary      Change AI model
// @Description  Change the current AI model for the chat session
// @Tags         Models
// @Accept       json
// @Produce      json
// @Param        request body ModelChangeRequest true "Model change request"
// @Success      200 {object} APIResponse{data=ModelInfo} "Model changed successfully"
// @Failure      400 {object} APIResponse{error=APIError} "Invalid request or model not found"
// @Router       /models [post]
func ModelChangeHandler(chatSession *chat.Chat) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		var req ModelChangeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			// Track failed API call
			if chatSession.Analytics != nil {
				chatSession.Analytics.RecordAPICall(time.Since(startTime), false, "validation")
			}
			response := NewErrorResponse(ErrorCodeValidation, "Invalid request payload", err.Error())
			c.JSON(http.StatusBadRequest, response)
			return
		}

		// Validate model exists
		newModel := models.GetModel(req.Model)
		availableModels := GetAvailableModels()

		var modelInfo *ModelInfo
		for _, model := range availableModels {
			if model.ID == req.Model {
				modelInfo = &model
				break
			}
		}

		if modelInfo == nil {
			// Track failed API call
			if chatSession.Analytics != nil {
				chatSession.Analytics.RecordAPICall(time.Since(startTime), false, "model_not_found")
			}
			response := NewErrorResponse(ErrorCodeModelNotFound, "Model not found", fmt.Sprintf("Model '%s' is not available", req.Model))
			c.JSON(http.StatusBadRequest, response)
			return
		}

		// Change the model
		chatSession.ChangeModel(newModel)

		// Track successful API call
		if chatSession.Analytics != nil {
			chatSession.Analytics.RecordAPICall(time.Since(startTime), true, "")
		}

		successResponse := NewSuccessResponse(*modelInfo, fmt.Sprintf("Model changed to %s successfully", modelInfo.Name))
		c.JSON(http.StatusOK, successResponse)
	}
}

// HealthHandler handles health check requests
// @Summary      Health check
// @Description  Check the health status of the API server
// @Tags         Health
// @Produce      json
// @Success      200 {object} APIResponse{data=HealthResponse} "Service is healthy"
// @Router       /health [get]
func HealthHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		uptime := time.Since(startTime)

		services := map[string]string{
			"api":    "healthy",
			"chat":   "healthy",
			"models": "healthy",
		}

		healthResponse := HealthResponse{
			Status:    "healthy",
			Version:   "1.3.0",
			Uptime:    int64(uptime.Seconds()),
			Services:  services,
			Timestamp: time.Now(),
		}

		successResponse := NewSuccessResponse(healthResponse, "Service is healthy")
		c.JSON(http.StatusOK, successResponse)
	}
}

// ClearHistoryHandler handles clearing chat history
// @Summary      Clear chat history
// @Description  Clear the current chat session history
// @Tags         Chat
// @Produce      json
// @Success      200 {object} APIResponse "Chat history cleared successfully"
// @Router       /history [delete]
func ClearHistoryHandler(chatSession *chat.Chat, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestStartTime := time.Now()
		chatSession.Clear(cfg)

		// Track API call in analytics
		if chatSession.Analytics != nil {
			chatSession.Analytics.RecordAPICall(time.Since(requestStartTime), true, "")
		}

		successResponse := NewSuccessResponse(nil, "Chat history cleared successfully")
		c.JSON(http.StatusOK, successResponse)
	}
}

// SessionInfoHandler handles session information requests
// @Summary      Get session information
// @Description  Get information about the current chat session
// @Tags         Session
// @Produce      json
// @Success      200 {object} APIResponse "Session information retrieved successfully"
// @Router       /session [get]
func SessionInfoHandler(chatSession *chat.Chat) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestStartTime := time.Now()
		sessionInfo := map[string]interface{}{
			"session_id":    chatSession.SessionID,
			"current_model": string(chatSession.Model),
			"message_count": len(chatSession.Messages),
			"last_vqd":      chatSession.NewVqd,
			"retry_count":   chatSession.RetryCount,
		}

		// Track API call in analytics
		if chatSession.Analytics != nil {
			chatSession.Analytics.RecordAPICall(time.Since(requestStartTime), true, "")
		}

		successResponse := NewSuccessResponse(sessionInfo, "Session information retrieved successfully")
		c.JSON(http.StatusOK, successResponse)
	}
}

// Helper functions

// estimateTokens provides a rough token count estimate
func estimateTokens(text string) int {
	// Rough estimation: ~4 characters per token
	return len(text) / 4
}
