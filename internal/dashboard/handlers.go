package dashboard

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"
	"time"

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
	if s.deps.Analytics != nil {
		current = s.deps.Analytics.Snapshot()
	}
	history := []any{}
	if s.deps.History != nil {
		entries, err := s.deps.History.List()
		if err != nil {
			http.Error(w, "could not read local statistics history", http.StatusInternalServerError)
			return
		}
		history = make([]any, 0, len(entries))
		for _, entry := range entries {
			history = append(history, entry)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"current": current, "history": history, "updatedAt": time.Now().UTC()})
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
		"allowConversationAnalysis": cfg.AllowConversationAnalysis, "analysisTokenBudget": cfg.AnalysisTokenBudget,
		"currentModel": currentModel, "models": available,
	})
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
