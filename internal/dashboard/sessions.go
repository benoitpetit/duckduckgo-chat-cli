package dashboard

import (
	"net/http"
	"regexp"
	"strings"
)

var dashboardSessionID = regexp.MustCompile(`^session_[A-Za-z0-9_-]{1,80}$`)

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if !s.currentSettings().ShowConversations {
		http.NotFound(w, r)
		return
	}
	if s.deps.Sessions == nil {
		http.Error(w, "conversation history unavailable", http.StatusServiceUnavailable)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if len(query) > 200 {
		http.Error(w, "search query is too long", http.StatusBadRequest)
		return
	}
	summaries, err := s.deps.Sessions.ListSessionSummaries()
	if err != nil {
		http.Error(w, "could not read conversation history", http.StatusInternalServerError)
		return
	}
	query = strings.ToLower(query)
	filtered := make([]any, 0, len(summaries))
	for _, summary := range summaries {
		if !dashboardSessionID.MatchString(summary.ID) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(summary.ID+" "+summary.Model+" "+summary.FirstMessage), query) {
			continue
		}
		filtered = append(filtered, summary)
	}
	writeJSON(w, http.StatusOK, filtered)
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	if !s.currentSettings().ShowConversations {
		http.NotFound(w, r)
		return
	}
	id := r.PathValue("id")
	if !dashboardSessionID.MatchString(id) {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}
	if s.deps.Sessions == nil {
		http.Error(w, "conversation history unavailable", http.StatusServiceUnavailable)
		return
	}
	session, err := s.deps.Sessions.LoadSession(id)
	if err != nil || session.ID != id {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, session)
}
