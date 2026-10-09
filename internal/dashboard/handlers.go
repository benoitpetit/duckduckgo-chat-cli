package dashboard

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"time"

	"duckduckgo-chat-cli/internal/activity"
	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/models"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	serveAsset(w, "web/index.html", "text/html; charset=utf-8")
}

func (s *Server) handleStyle(w http.ResponseWriter, _ *http.Request) {
	serveAsset(w, "web/style.css", "text/css; charset=utf-8")
}

func (s *Server) handleAppScript(w http.ResponseWriter, _ *http.Request) {
	serveAsset(w, "web/app.js", "text/javascript; charset=utf-8")
}

func (s *Server) handleLogo(w http.ResponseWriter, _ *http.Request) {
	serveAsset(w, "web/logo.png", "image/png")
}

func serveAsset(w http.ResponseWriter, path, contentType string) {
	data, err := fs.ReadFile(webFiles, path)
	if err != nil {
		http.Error(w, "dashboard asset unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	var current any = map[string]any{}
	currentSnapshot := analytics.Snapshot{}
	if s.deps.Analytics != nil {
		currentSnapshot = s.deps.Analytics.Snapshot()
		current = currentSnapshot
	}
	history := []any{}
	historySnapshots := []analytics.Snapshot{}
	if s.deps.History != nil {
		entries, err := s.deps.History.List()
		if err != nil {
			http.Error(w, "could not read local statistics history", http.StatusInternalServerError)
			return
		}
		historySnapshots = entries
		history = make([]any, 0, len(entries))
		for _, entry := range entries {
			history = append(history, entry)
		}
	}
	dailyActivity := aggregateDailyActivity(historySnapshots, currentSnapshot, time.Now(), s.currentSettings().RetentionDays)
	allSessions, sessionCount := aggregateSessionStats(historySnapshots, currentSnapshot)
	writeJSON(w, http.StatusOK, map[string]any{
		"current": current, "allSessions": allSessions, "sessionCount": sessionCount,
		"history": history, "dailyActivity": dailyActivity, "updatedAt": time.Now().UTC(),
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, _ *http.Request) {
	cfg := s.currentSettings()
	currentModel := ""
	if s.deps.Analytics != nil {
		currentModel = s.deps.Analytics.Snapshot().CurrentModel
	}
	type modelInfo struct {
		ID          string `json:"id"`
		Alias       string `json:"alias"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	available := make([]modelInfo, 0, len(models.Available()))
	for _, item := range models.Available() {
		available = append(available, modelInfo{ID: string(item.ID), Alias: string(item.Alias), Name: item.Name, Description: item.Description})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"autostart": cfg.Autostart, "port": cfg.Port, "refreshIntervalSeconds": cfg.RefreshIntervalSeconds,
		"retentionDays": cfg.RetentionDays, "showConversations": cfg.ShowConversations,
		"showConversationContent":   cfg.ShowConversationContent,
		"allowConversationAnalysis": cfg.AllowConversationAnalysis, "analysisTokenBudget": cfg.AnalysisTokenBudget,
		"currentModel": currentModel, "models": available,
	})
}

func (s *Server) handleActivityStream(w http.ResponseWriter, r *http.Request) {
	if s.deps.Activity == nil {
		http.Error(w, "activity feed unavailable", http.StatusServiceUnavailable)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming is unavailable", http.StatusInternalServerError)
		return
	}
	cursor := uint64(0)
	lastID := r.Header.Get("Last-Event-ID")
	if lastID != "" {
		parsed, err := strconv.ParseUint(lastID, 10, 64)
		if err != nil {
			http.Error(w, "invalid activity cursor", http.StatusBadRequest)
			return
		}
		cursor = parsed
	}
	replay, ids, cancel := s.deps.Activity.Subscribe(cursor)
	defer cancel()
	if !s.activityStreamAuthorized(r) {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher := w.(http.Flusher)
	for _, event := range replay {
		if !s.activityStreamAuthorized(r) {
			return
		}
		if !s.writeActivityEvent(w, event) {
			return
		}
		cursor = event.ID
	}
	_, current := s.deps.Activity.Snapshot()
	if !s.activityStreamAuthorized(r) {
		return
	}
	if !writeActivityState(w, current) {
		return
	}
	flusher.Flush()
	heartbeat := time.NewTicker(2 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case id, ok := <-ids:
			if !ok {
				return
			}
			_ = id // EventsAfter uses the cursor to include every event in order.
			for _, event := range s.deps.Activity.EventsAfter(cursor) {
				if !s.activityStreamAuthorized(r) {
					return
				}
				if !s.writeActivityEvent(w, event) {
					return
				}
				cursor = event.ID
			}
			flusher.Flush()
		case <-heartbeat.C:
			if !s.activityStreamAuthorized(r) {
				return
			}
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) writeActivityEvent(w http.ResponseWriter, event activity.Event) bool {
	if !s.currentSettings().ShowConversationContent {
		event.Prompt, event.Response = "", ""
		if event.Category == "conversation" {
			if event.Status == "prompt" {
				event.Summary = "User prompt submitted (hidden)"
			} else {
				event.Summary = "Assistant response received (hidden)"
			}
		}
	}
	data, err := json.Marshal(event)
	if err != nil {
		return false
	}
	if _, err = fmt.Fprintf(w, "id: %d\nevent: activity\ndata: %s\n\n", event.ID, data); err != nil {
		return false
	}
	return true
}

func writeActivityState(w http.ResponseWriter, current string) bool {
	data, err := json.Marshal(map[string]string{"current": current})
	if err != nil {
		return false
	}
	_, err = fmt.Fprintf(w, "event: state\ndata: %s\n\n", data)
	return err == nil
}

func (s *Server) handleCommands(w http.ResponseWriter, _ *http.Request) {
	commands := []any{}
	if s.deps.Commands != nil {
		if registry := s.deps.Commands(); registry != nil {
			items := make([]struct {
				key   string
				value any
			}, 0, len(registry.Commands))
			for key, value := range registry.Commands {
				items = append(items, struct {
					key   string
					value any
				}{key, value})
			}
			sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
			for _, item := range items {
				commands = append(commands, item.value)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"commands": commands})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
