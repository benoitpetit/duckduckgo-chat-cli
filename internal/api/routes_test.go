package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"duckduckgo-chat-cli/internal/chat"
	"duckduckgo-chat-cli/internal/config"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"

	"github.com/gin-gonic/gin"
)

func newRouteTestServer(t *testing.T) (*gin.Engine, *chat.Chat) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{API: config.APIConfig{ShowGinLogs: false}}
	conversation := &chat.Chat{
		Model:          models.Default(),
		SessionID:      "session-test",
		HistoryManager: persistence.NewHistoryManager(t.TempDir()),
		Messages: []chat.Message{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "reply"},
			{Role: "user", Content: "second"},
		},
	}
	return setupRouter(NewSession(conversation, cfg), cfg), conversation
}

func performJSONRequest(router http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	var body bytes.Buffer
	if payload != nil {
		_ = json.NewEncoder(&body).Encode(payload)
	}
	req := httptest.NewRequest(method, path, &body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func decodeAPIResponse(t *testing.T, response *httptest.ResponseRecorder) APIResponse {
	t.Helper()
	var result APIResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode API response: %v; body=%s", err, response.Body.String())
	}
	return result
}

func TestAPIRoutesModelSessionHistoryAndClearFlow(t *testing.T) {
	router, conversation := newRouteTestServer(t)

	modelsResponse := performJSONRequest(router, http.MethodGet, "/api/v1/models", nil)
	if modelsResponse.Code != http.StatusOK {
		t.Fatalf("GET /models status = %d, body=%s", modelsResponse.Code, modelsResponse.Body.String())
	}
	var modelEnvelope struct {
		Data ModelsResponse `json:"data"`
	}
	if err := json.Unmarshal(modelsResponse.Body.Bytes(), &modelEnvelope); err != nil {
		t.Fatal(err)
	}
	if modelEnvelope.Data.TotalModels != len(models.Available()) || len(modelEnvelope.Data.Models) != len(models.Available()) {
		t.Fatalf("model count = total:%d listed:%d, want %d", modelEnvelope.Data.TotalModels, len(modelEnvelope.Data.Models), len(models.Available()))
	}
	if modelEnvelope.Data.CurrentModel != string(models.Default()) {
		t.Fatalf("current model = %q, want %q", modelEnvelope.Data.CurrentModel, models.Default())
	}

	changeResponse := performJSONRequest(router, http.MethodPost, "/api/v1/models", ModelChangeRequest{Model: string(models.GPT5Luna)})
	if changeResponse.Code != http.StatusOK || conversation.Model != models.GPT5Luna {
		t.Fatalf("POST /models status=%d current model=%q body=%s", changeResponse.Code, conversation.Model, changeResponse.Body.String())
	}

	sessionResponse := performJSONRequest(router, http.MethodGet, "/api/v1/session", nil)
	var sessionEnvelope struct {
		Data struct {
			SessionID    string `json:"session_id"`
			CurrentModel string `json:"current_model"`
			MessageCount int    `json:"message_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(sessionResponse.Body.Bytes(), &sessionEnvelope); err != nil {
		t.Fatal(err)
	}
	if sessionResponse.Code != http.StatusOK || sessionEnvelope.Data.SessionID != "session-test" || sessionEnvelope.Data.CurrentModel != string(models.GPT5Luna) || sessionEnvelope.Data.MessageCount != 3 {
		t.Fatalf("GET /session status=%d data=%+v body=%s", sessionResponse.Code, sessionEnvelope.Data, sessionResponse.Body.String())
	}

	historyResponse := performJSONRequest(router, http.MethodGet, "/api/v1/history?limit=1&offset=1", nil)
	var historyEnvelope struct {
		Data HistoryResponse `json:"data"`
	}
	if err := json.Unmarshal(historyResponse.Body.Bytes(), &historyEnvelope); err != nil {
		t.Fatal(err)
	}
	if historyResponse.Code != http.StatusOK || historyEnvelope.Data.TotalMessages != 3 || len(historyEnvelope.Data.Messages) != 1 || historyEnvelope.Data.Messages[0].Content != "reply" {
		t.Fatalf("GET /history pagination status=%d data=%+v body=%s", historyResponse.Code, historyEnvelope.Data, historyResponse.Body.String())
	}

	clearResponse := performJSONRequest(router, http.MethodDelete, "/api/v1/history", nil)
	if clearResponse.Code != http.StatusOK {
		t.Fatalf("DELETE /history status = %d, body=%s", clearResponse.Code, clearResponse.Body.String())
	}
	if len(conversation.Messages) != 0 {
		t.Fatalf("DELETE /history left %d messages", len(conversation.Messages))
	}
}

func TestAPIRejectsInvalidHistoryPagination(t *testing.T) {
	router, _ := newRouteTestServer(t)
	for _, query := range []string{"limit=invalid", "limit=-2", "offset=invalid", "offset=-1", "offset=999999999999999999999999999999"} {
		t.Run(query, func(t *testing.T) {
			response := performJSONRequest(router, http.MethodGet, "/api/v1/history?"+query, nil)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("GET /history?%s status = %d, want %d; body=%s", query, response.Code, http.StatusBadRequest, response.Body.String())
			}
			if envelope := decodeAPIResponse(t, response); envelope.Success || envelope.Error == nil || envelope.Error.Code != ErrorCodeValidation {
				t.Fatalf("GET /history?%s error envelope = %+v", query, envelope)
			}
		})
	}
}

func TestAPIHistoryTreatsZeroLimitAsDefault(t *testing.T) {
	router, _ := newRouteTestServer(t)
	response := performJSONRequest(router, http.MethodGet, "/api/v1/history?limit=0", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /history?limit=0 status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
}

func TestAPIRejectsInvalidChatRequestsWithoutChangingModel(t *testing.T) {
	router, conversation := newRouteTestServer(t)

	for name, payload := range map[string]any{
		"empty message":     map[string]string{"message": ""},
		"unknown model":     map[string]string{"message": "hello", "model": "not-a-model"},
		"oversized message": map[string]string{"message": string(bytes.Repeat([]byte("x"), 10001))},
	} {
		t.Run(name, func(t *testing.T) {
			response := performJSONRequest(router, http.MethodPost, "/api/v1/chat", payload)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("POST /chat status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}

	if conversation.Model != models.Default() {
		t.Fatalf("invalid chat request changed model to %q", conversation.Model)
	}
}

func TestAPIDeleteHistoryReportsPersistenceFailure(t *testing.T) {
	router, conversation := newRouteTestServer(t)
	conversation.HistoryManager = nil

	response := performJSONRequest(router, http.MethodDelete, "/api/v1/history", nil)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("DELETE /history status = %d, want %d; body=%s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
	if len(conversation.Messages) == 0 {
		t.Fatal("DELETE /history discarded messages after the archive failed")
	}
	if envelope := decodeAPIResponse(t, response); envelope.Success || envelope.Error == nil || envelope.Error.Code != ErrorCodeInternal {
		t.Fatalf("DELETE /history error envelope = %+v", envelope)
	}
}

func TestAPIKeyMiddlewareAcceptsBearerAuthorization(t *testing.T) {
	cfg := &config.Config{API: config.APIConfig{APIKey: "secret", ShowGinLogs: false}}
	router := setupRouter(NewSession(&chat.Chat{}, cfg), cfg)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status with bearer key = %d, want %d", response.Code, http.StatusOK)
	}
}
