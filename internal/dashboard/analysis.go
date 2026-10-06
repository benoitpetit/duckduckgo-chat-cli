package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/models"
	"duckduckgo-chat-cli/internal/persistence"
)

const (
	defaultAnalysisBudget = 8000
	analysisTimeout       = 2 * time.Minute
)

const conversationInstructions = `You are reviewing a local CLI usage archive. Give a concise, practical report with recurring themes, progress, friction points, and a few actionable recommendations. Distinguish observations from inferences. The excerpts are a bounded sample, not a complete transcript. Do not invent facts.

The following session excerpts were explicitly selected by the user for this analysis:`

func BuildConversationReport(sessions []persistence.ConversationSession, tokenBudget int) (prompt string, includedSessions int, estimatedTokens int) {
	if tokenBudget < 1 || tokenBudget > 32000 {
		tokenBudget = defaultAnalysisBudget
	}
	maxBytes := tokenBudget * 4
	if maxBytes <= len(conversationInstructions) {
		return conversationInstructions, 0, estimateInputTokens(conversationInstructions)
	}
	ordered := append([]persistence.ConversationSession(nil), sessions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].StartTime.Equal(ordered[j].StartTime) {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].StartTime.Before(ordered[j].StartTime)
	})
	usable := make([]persistence.ConversationSession, 0, len(ordered))
	for _, session := range ordered {
		if len(session.Messages) == 0 {
			continue
		}
		usable = append(usable, session)
	}
	if len(usable) == 0 {
		return conversationInstructions, 0, estimateInputTokens(conversationInstructions)
	}

	availableBytes := maxBytes - len(conversationInstructions) - 2
	maxSessions := availableBytes / 140
	if maxSessions < 1 {
		maxSessions = 1
	}
	if maxSessions > len(usable) {
		maxSessions = len(usable)
	}
	selected := make([]persistence.ConversationSession, 0, maxSessions)
	for i := 0; i < maxSessions; i++ {
		index := i
		if maxSessions > 1 {
			index = i * (len(usable) - 1) / (maxSessions - 1)
		} else if len(usable) > 1 {
			index = (len(usable) - 1) / 2
		}
		selected = append(selected, usable[index])
	}

	prompt = conversationInstructions
	includedSessions = 0
	for i, session := range selected {
		remainingSlots := len(selected) - i
		remainingBytes := maxBytes - len(prompt)
		perSessionBytes := remainingBytes / remainingSlots
		prefix := fmt.Sprintf("\n\nSession %d — %s — model %s\n", i+1, session.StartTime.Format("2006-01-02"), session.Model)
		if len(prefix) >= perSessionBytes {
			continue
		}
		prompt += prefix
		includedSessions++
		perSessionBytes -= len(prefix)
		for _, message := range session.Messages {
			if message.Role != "user" && message.Role != "assistant" {
				continue
			}
			label := message.Role + ": "
			remainingBytes = maxBytes - len(prompt)
			if remainingBytes <= len(label)+2 {
				break
			}
			allowance := remainingBytes
			if allowance > perSessionBytes {
				allowance = perSessionBytes
			}
			if allowance <= len(label)+1 {
				break
			}
			content := truncateUTF8(message.Content, allowance-len(label)-1)
			if strings.TrimSpace(content) == "" {
				continue
			}
			prompt += label + content + "\n"
			perSessionBytes -= len(label) + len(content) + 1
			if perSessionBytes <= len(label)+1 {
				break
			}
		}
		if len(prompt) >= maxBytes {
			break
		}
	}
	return prompt, includedSessions, estimateInputTokens(prompt)
}

func estimateInputTokens(prompt string) int {
	return (len(prompt) + 3) / 4
}

func truncateUTF8(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	text = text[:maxBytes]
	for len(text) > 0 && !utf8.ValidString(text) {
		_, size := utf8.DecodeLastRuneInString(text)
		if size == 0 {
			break
		}
		text = text[:len(text)-size]
	}
	return text
}

type modelRequest struct {
	Model string `json:"model"`
}

func (s *Server) handleMetricsAnalysis(w http.ResponseWriter, r *http.Request) {
	_, model, ok := s.decodeModelRequest(w, r)
	if !ok {
		return
	}
	var current analytics.Snapshot
	if s.deps.Analytics != nil {
		current = s.deps.Analytics.Snapshot()
	}
	var history []analytics.Snapshot
	if s.deps.History != nil {
		var err error
		history, err = s.deps.History.List()
		if err != nil {
			http.Error(w, "could not read local statistics history", http.StatusInternalServerError)
			return
		}
	}
	catalog := make([]map[string]string, 0, len(models.Available()))
	for _, item := range models.Available() {
		catalog = append(catalog, map[string]string{"id": string(item.ID), "name": item.Name, "description": item.Description})
	}
	data, _ := json.MarshalIndent(map[string]any{"current_session_metrics": current, "historical_session_metrics": history, "available_cli_models": catalog}, "", "  ")
	prompt := "Analyze these local aggregate usage measurements. They contain metrics only and no conversation text. Separate observed facts from recommendations. Mark token counts as estimates and do not claim costs or live provider availability. Recommend a CLI model only based on observed per-model performance and the supplied catalog.\n\n" + string(data)
	s.runAnalysis(w, r, model, prompt, map[string]any{"kind": "metrics"})
}

func (s *Server) handleConversationPreview(w http.ResponseWriter, r *http.Request) {
	if !s.currentSettings().AllowConversationAnalysis {
		http.Error(w, "conversation analysis is disabled in /config", http.StatusForbidden)
		return
	}
	sessions, err := s.listConversationArchives()
	if err != nil {
		http.Error(w, "could not read conversation archives", http.StatusInternalServerError)
		return
	}
	cfg := s.currentSettings()
	_, included, estimated := BuildConversationReport(sessions, cfg.AnalysisTokenBudget)
	writeJSON(w, http.StatusOK, map[string]any{"includedSessions": included, "estimatedTokens": estimated, "tokenBudget": cfg.AnalysisTokenBudget, "estimateNote": "Approximate estimate using four characters per token; provider usage may differ."})
}

func (s *Server) handleConversationAnalysis(w http.ResponseWriter, r *http.Request) {
	if !s.currentSettings().AllowConversationAnalysis {
		http.Error(w, "conversation analysis is disabled in /config", http.StatusForbidden)
		return
	}
	_, model, ok := s.decodeModelRequest(w, r)
	if !ok {
		return
	}
	sessions, err := s.listConversationArchives()
	if err != nil {
		http.Error(w, "could not read conversation archives", http.StatusInternalServerError)
		return
	}
	cfg := s.currentSettings()
	prompt, included, estimated := BuildConversationReport(sessions, cfg.AnalysisTokenBudget)
	s.runAnalysis(w, r, model, prompt, map[string]any{
		"kind": "conversations", "includedSessions": included, "estimatedTokens": estimated,
		"tokenBudget": cfg.AnalysisTokenBudget,
	})
}

func (s *Server) listConversationArchives() ([]persistence.ConversationSession, error) {
	if s.deps.Sessions == nil {
		return nil, fmt.Errorf("conversation history unavailable")
	}
	return s.deps.Sessions.ListConversationSessions()
}

func (s *Server) decodeModelRequest(w http.ResponseWriter, r *http.Request) (modelRequest, models.Model, bool) {
	var request modelRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid analysis request", http.StatusBadRequest)
		return request, "", false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "invalid analysis request", http.StatusBadRequest)
		return request, "", false
	}
	for _, available := range models.Available() {
		if string(available.ID) == request.Model {
			return request, available.ID, true
		}
	}
	http.Error(w, "model must be selected from the CLI model catalog", http.StatusBadRequest)
	return request, "", false
}

func (s *Server) runAnalysis(w http.ResponseWriter, r *http.Request, model models.Model, prompt string, metadata map[string]any) {
	if s.deps.Analyze == nil {
		http.Error(w, "AI analysis is unavailable", http.StatusServiceUnavailable)
		return
	}
	if !s.analysis.TryLock() {
		http.Error(w, "another dashboard analysis is already running", http.StatusConflict)
		return
	}
	defer s.analysis.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), analysisTimeout)
	defer cancel()
	result, err := s.deps.Analyze(ctx, model, prompt)
	if err != nil {
		http.Error(w, fmt.Sprintf("AI analysis failed: %v", err), http.StatusBadGateway)
		return
	}
	metadata["result"] = result
	writeJSON(w, http.StatusOK, metadata)
}
