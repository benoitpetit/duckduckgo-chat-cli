package dashboard

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/media"
	"duckduckgo-chat-cli/internal/persistence"
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
		if query != "" && !sessionSummaryMatchesQuery(summary, query) {
			continue
		}
		filtered = append(filtered, summary)
	}
	writeJSON(w, http.StatusOK, filtered)
}

func sessionSummaryMatchesQuery(summary persistence.SessionSummary, query string) bool {
	if strings.Contains(strings.ToLower(summary.ID+" "+summary.Title+" "+summary.Model+" "+summary.FirstMessage), query) {
		return true
	}
	for _, tag := range summary.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
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
	writeJSON(w, http.StatusOK, sessionWithoutImageData(session))
}

func (s *Server) handleSessionMetadataUpdate(w http.ResponseWriter, r *http.Request) {
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
	metadata, ok := decodeSessionMetadata(w, r)
	if !ok {
		return
	}
	if err := s.deps.Sessions.UpdateSessionMetadata(id, metadata); err != nil {
		switch {
		case errors.Is(err, persistence.ErrInvalidSessionMetadata):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, os.ErrNotExist):
			http.Error(w, "session not found", http.StatusNotFound)
		default:
			http.Error(w, "could not update session metadata", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
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
	if err := s.deps.Sessions.DeleteSession(id); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "session not found", http.StatusNotFound)
		} else {
			http.Error(w, "could not delete session", http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func decodeSessionMetadata(w http.ResponseWriter, r *http.Request) (persistence.SessionMetadata, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var metadata persistence.SessionMetadata
	if err := decoder.Decode(&metadata); err != nil {
		http.Error(w, "invalid session metadata", http.StatusBadRequest)
		return persistence.SessionMetadata{}, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "invalid session metadata", http.StatusBadRequest)
		return persistence.SessionMetadata{}, false
	}
	return metadata, true
}

func sessionWithoutImageData(session *persistence.ConversationSession) *persistence.ConversationSession {
	copy := *session
	copy.Messages = messagesWithoutImageData(session.Messages)
	copy.OptimizedMessages = messagesWithoutImageData(session.OptimizedMessages)
	copy.PendingImages = nil
	copy.Notes = nil
	return &copy
}

func messagesWithoutImageData(messages []intelligence.Message) []intelligence.Message {
	copy := append([]intelligence.Message(nil), messages...)
	for i := range copy {
		copy[i].Images = append([]media.ImageAttachment(nil), messages[i].Images...)
		for j := range copy[i].Images {
			copy[i].Images[j].Data = nil
		}
	}
	return copy
}
