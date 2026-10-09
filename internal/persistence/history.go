package persistence

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/ui"
)

// SessionFormatVersion identifies when per-message timestamps became real
// creation times rather than synthetic save-time ordering values.
const SessionFormatVersion = "1.1"

// ConversationSession represents a complete chat session
type ConversationSession struct {
	ID                string                 `json:"id"`
	StartTime         time.Time              `json:"start_time"`
	EndTime           time.Time              `json:"end_time"`
	Model             string                 `json:"model"`
	Messages          []intelligence.Message `json:"messages"`
	OptimizedMessages []intelligence.Message `json:"optimized_messages,omitempty"`
	Analytics         SessionAnalytics       `json:"analytics"`
	Compressed        bool                   `json:"compressed"`
	Version           string                 `json:"version"`
}

// SessionAnalytics stores session-specific analytics
type SessionAnalytics struct {
	MessageCount      int           `json:"message_count"`
	TotalTokens       int           `json:"total_tokens"`
	SessionDuration   time.Duration `json:"session_duration"`
	APICallsCount     int           `json:"api_calls_count"`
	ErrorCount        int           `json:"error_count"`
	OptimizationsUsed int           `json:"optimizations_used"`
}

// HistoryManager manages conversation persistence
type HistoryManager struct {
	StorageDir       string
	MaxSessions      int
	CompressionLevel int
	RetentionDays    int
	optimizer        *intelligence.ContextOptimizer
	mu               sync.RWMutex
	saveMu           sync.Mutex
}

var resumeSessionIDPattern = regexp.MustCompile(`^session_[A-Za-z0-9_-]{1,80}$`)
var storageSessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// NewHistoryManager creates a new history manager
func NewHistoryManager(storageDir string) *HistoryManager {
	manager := &HistoryManager{
		StorageDir:       storageDir,
		MaxSessions:      100, // Keep last 100 sessions
		CompressionLevel: 6,   // Balanced compression
		RetentionDays:    90,  // Keep sessions for 90 days
		optimizer:        intelligence.NewContextOptimizer(),
	}
	return manager
}

// SetRetentionDays validates and applies retention, pruning expired sessions immediately.
func (hm *HistoryManager) SetRetentionDays(days int) error {
	if days < 1 || days > 3650 {
		return fmt.Errorf("retention days must be between 1 and 3650")
	}
	hm.saveMu.Lock()
	defer hm.saveMu.Unlock()
	hm.mu.Lock()
	hm.RetentionDays = days
	hm.mu.Unlock()
	return hm.cleanupOldSessions()
}

// SaveSession saves a conversation session with optimization
func (hm *HistoryManager) SaveSession(session *ConversationSession) error {
	if session == nil || !storageSessionIDPattern.MatchString(session.ID) {
		return fmt.Errorf("invalid session id")
	}
	hm.saveMu.Lock()
	defer hm.saveMu.Unlock()
	// Ensure storage directory exists
	if err := os.MkdirAll(hm.StorageDir, 0o700); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Set session metadata
	session.EndTime = time.Now()
	session.Version = SessionFormatVersion
	session.Analytics.SessionDuration = session.EndTime.Sub(session.StartTime)
	session.Analytics.MessageCount = len(session.Messages)

	// Optimize messages if needed
	if hm.optimizer.IsOptimizationNeeded(session.Messages) {
		ui.Warningln("Optimizing session before saving...")
		optimized, bytesSaved := hm.optimizer.OptimizeContext(session.Messages)
		session.OptimizedMessages = optimized
		session.Analytics.OptimizationsUsed++
		ui.AIln("Session optimized for storage (saved %d bytes)", bytesSaved)
	}

	filename := fmt.Sprintf("session_%s.json", session.ID)
	fullPath := filepath.Join(hm.StorageDir, filename)

	// Save with compression
	if err := hm.saveCompressed(session, fullPath); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove legacy uncompressed session: %w", err)
	}

	ui.AIln("Session saved: %s", fullPath)

	// Cleanup old sessions
	if err := hm.cleanupOldSessions(); err != nil {
		ui.Warningln("Failed to clean conversation history: %v", err)
	}

	return nil
}

// LoadSession loads a conversation session
func (hm *HistoryManager) LoadSession(sessionID string) (*ConversationSession, error) {
	if !storageSessionIDPattern.MatchString(sessionID) {
		return nil, fmt.Errorf("invalid session id")
	}
	filename := fmt.Sprintf("session_%s.json.gz", sessionID)
	fullPath := filepath.Join(hm.StorageDir, filename)

	// Try compressed first
	if session, err := hm.loadCompressed(fullPath); err == nil {
		return session, nil
	}

	// Fallback to uncompressed
	filename = fmt.Sprintf("session_%s.json", sessionID)
	fullPath = filepath.Join(hm.StorageDir, filename)

	return hm.loadUncompressed(fullPath)
}

// ListSessions returns a list of available sessions
func (hm *HistoryManager) ListSessions() ([]ConversationSession, error) {
	files, err := os.ReadDir(hm.StorageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ConversationSession{}, nil
		}
		return nil, fmt.Errorf("failed to read storage directory: %w", err)
	}

	var sessions []ConversationSession

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filename := file.Name()
		if !strings.HasPrefix(filename, "session_") {
			continue
		}

		var session *ConversationSession
		fullPath := filepath.Join(hm.StorageDir, filename)

		if strings.HasSuffix(filename, ".gz") {
			session, err = hm.loadCompressed(fullPath)
		} else if strings.HasSuffix(filename, ".json") {
			session, err = hm.loadUncompressed(fullPath)
		} else {
			continue
		}

		if err != nil {
			ui.Warningln("Failed to load session %s: %v", filename, err)
			continue
		}

		// Only load metadata for listing (not full messages)
		session.Messages = nil
		session.OptimizedMessages = nil
		sessions = append(sessions, *session)
	}

	// Sort by start time (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartTime.After(sessions[j].StartTime)
	})

	return sessions, nil
}

// ListConversationSessions returns retained archives with transcript contents for explicit local analysis.
func (hm *HistoryManager) ListConversationSessions() ([]ConversationSession, error) {
	files, err := os.ReadDir(hm.StorageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ConversationSession{}, nil
		}
		return nil, fmt.Errorf("failed to read storage directory: %w", err)
	}
	sessions := make([]ConversationSession, 0)
	for _, file := range files {
		if file.IsDir() || !strings.HasPrefix(file.Name(), "session_") {
			continue
		}
		path := filepath.Join(hm.StorageDir, file.Name())
		var session *ConversationSession
		switch {
		case strings.HasSuffix(file.Name(), ".json.gz"):
			session, err = hm.loadCompressed(path)
		case strings.HasSuffix(file.Name(), ".json"):
			session, err = hm.loadUncompressed(path)
		default:
			continue
		}
		if err != nil {
			ui.Warningln("Failed to load session %s: %v", file.Name(), err)
			continue
		}
		sessions = append(sessions, *session)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].StartTime.After(sessions[j].StartTime) })
	return sessions, nil
}

// SearchSessions searches for sessions containing specific content
func (hm *HistoryManager) SearchSessions(query string) ([]ConversationSession, error) {
	files, err := os.ReadDir(hm.StorageDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read storage directory: %w", err)
	}

	var matchingSessions []ConversationSession
	query = strings.ToLower(query)

	for _, file := range files {
		if file.IsDir() || !strings.HasPrefix(file.Name(), "session_") {
			continue
		}

		var session *ConversationSession
		fullPath := filepath.Join(hm.StorageDir, file.Name())

		if strings.HasSuffix(file.Name(), ".gz") {
			session, err = hm.loadCompressed(fullPath)
		} else if strings.HasSuffix(file.Name(), ".json") {
			session, err = hm.loadUncompressed(fullPath)
		} else {
			continue
		}

		if err != nil {
			continue
		}

		// Search in session content
		found := false
		for _, msg := range session.Messages {
			if strings.Contains(strings.ToLower(msg.Content), query) {
				found = true
				break
			}
		}

		if found {
			matchingSessions = append(matchingSessions, *session)
		}
	}

	// Sort by relevance (most recent first for now)
	sort.Slice(matchingSessions, func(i, j int) bool {
		return matchingSessions[i].StartTime.After(matchingSessions[j].StartTime)
	})

	return matchingSessions, nil
}

// GetSessionSummary provides a summary of a session without loading full content
func (hm *HistoryManager) GetSessionSummary(sessionID string) (*SessionSummary, error) {
	session, err := hm.LoadSession(sessionID)
	if err != nil {
		return nil, err
	}

	summary := &SessionSummary{
		ID:            session.ID,
		StartTime:     session.StartTime,
		EndTime:       session.EndTime,
		Duration:      session.Analytics.SessionDuration,
		MessageCount:  session.Analytics.MessageCount,
		Model:         session.Model,
		ResumeCommand: "",
		FirstMessage:  "",
		LastMessage:   "",
		KeyTopics:     []string{},
	}
	if resumeSessionIDPattern.MatchString(session.ID) {
		summary.ResumeCommand = "/load " + session.ID
	}

	// Get first and last user messages
	for _, msg := range session.Messages {
		if msg.Role == "user" {
			if summary.FirstMessage == "" {
				summary.FirstMessage = truncateMessage(msg.Content, 100)
			}
			summary.LastMessage = truncateMessage(msg.Content, 100)
		}
	}

	// Extract key topics (simple keyword extraction)
	summary.KeyTopics = hm.extractKeyTopics(session.Messages)

	return summary, nil
}

// ListSessionSummaries returns a newest-first list containing metadata and a short first-user-message preview only.
func (hm *HistoryManager) ListSessionSummaries() ([]SessionSummary, error) {
	files, err := os.ReadDir(hm.StorageDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionSummary{}, nil
		}
		return nil, fmt.Errorf("failed to read storage directory: %w", err)
	}
	summaries := make([]SessionSummary, 0)
	for _, file := range files {
		if file.IsDir() || !strings.HasPrefix(file.Name(), "session_") {
			continue
		}
		path := filepath.Join(hm.StorageDir, file.Name())
		var session *ConversationSession
		switch {
		case strings.HasSuffix(file.Name(), ".json.gz"):
			session, err = hm.loadCompressed(path)
		case strings.HasSuffix(file.Name(), ".json"):
			session, err = hm.loadUncompressed(path)
		default:
			continue
		}
		if err != nil {
			ui.Warningln("Failed to load session %s: %v", file.Name(), err)
			continue
		}
		summary := SessionSummary{
			ID: session.ID, StartTime: session.StartTime, EndTime: session.EndTime,
			Duration: session.Analytics.SessionDuration, MessageCount: session.Analytics.MessageCount,
			Model: session.Model, KeyTopics: []string{},
		}
		for _, message := range session.Messages {
			if message.Role == "user" {
				summary.FirstMessage = truncateMessage(message.Content, 100)
				break
			}
		}
		if resumeSessionIDPattern.MatchString(session.ID) {
			summary.ResumeCommand = "/load " + session.ID
		}
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].StartTime.After(summaries[j].StartTime) })
	return summaries, nil
}

// RestoreSession restores messages from a saved session
func (hm *HistoryManager) RestoreSession(sessionID string) ([]intelligence.Message, error) {
	session, err := hm.LoadSession(sessionID)
	if err != nil {
		return nil, err
	}

	// Use optimized messages if available, otherwise use original
	if len(session.OptimizedMessages) > 0 {
		ui.AIln("Restored optimized session with %d messages", len(session.OptimizedMessages))
		return session.OptimizedMessages, nil
	}

	ui.AIln("Restored session with %d messages", len(session.Messages))
	return session.Messages, nil
}

// GetStorageStats returns storage statistics
func (hm *HistoryManager) GetStorageStats() (*StorageStats, error) {
	sessions, err := hm.ListSessions()
	if err != nil {
		return nil, err
	}

	stats := &StorageStats{
		TotalSessions:      len(sessions),
		CompressedSessions: 0,
		TotalSizeBytes:     0,
		OldestSession:      time.Now(),
		NewestSession:      time.Time{},
	}

	// Calculate storage statistics
	files, err := os.ReadDir(hm.StorageDir)
	if err == nil {
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			fileInfo, err := file.Info()
			if err != nil {
				continue
			}

			stats.TotalSizeBytes += fileInfo.Size()

			if strings.HasSuffix(file.Name(), ".gz") {
				stats.CompressedSessions++
			}
		}
	}

	// Find oldest and newest sessions
	for _, session := range sessions {
		if session.StartTime.Before(stats.OldestSession) {
			stats.OldestSession = session.StartTime
		}
		if session.StartTime.After(stats.NewestSession) {
			stats.NewestSession = session.StartTime
		}
	}

	return stats, nil
}

// Private helper methods

func (hm *HistoryManager) saveCompressed(session *ConversationSession, filePath string) error {
	session.Compressed = true
	gzFilepath := filePath + ".gz"
	tmp, err := os.CreateTemp(hm.StorageDir, ".session-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}

	gzWriter, err := gzip.NewWriterLevel(tmp, hm.CompressionLevel)
	if err != nil {
		tmp.Close()
		return err
	}
	encoder := json.NewEncoder(gzWriter)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(session); err != nil {
		gzWriter.Close()
		tmp.Close()
		return err
	}
	if err := gzWriter.Close(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, gzFilepath)
}

func (hm *HistoryManager) loadCompressed(filePath string) (*ConversationSession, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gzReader.Close()

	var session ConversationSession
	decoder := json.NewDecoder(gzReader)

	if err := decoder.Decode(&session); err != nil {
		return nil, err
	}

	return &session, nil
}

func (hm *HistoryManager) loadUncompressed(filePath string) (*ConversationSession, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var session ConversationSession
	decoder := json.NewDecoder(file)

	if err := decoder.Decode(&session); err != nil {
		return nil, err
	}

	return &session, nil
}

func (hm *HistoryManager) cleanupOldSessions() error {
	sessions, err := hm.ListSessions()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	hm.mu.RLock()
	retentionDays, maxSessions := hm.RetentionDays, hm.MaxSessions
	hm.mu.RUnlock()

	now := time.Now()
	removed := 0

	// Remove sessions older than retention period
	for _, session := range sessions {
		if !storageSessionIDPattern.MatchString(session.ID) {
			continue
		}
		if now.Sub(session.StartTime) > time.Duration(retentionDays)*24*time.Hour {
			filename := fmt.Sprintf("session_%s.json", session.ID)
			gzFilename := fmt.Sprintf("session_%s.json.gz", session.ID)

			os.Remove(filepath.Join(hm.StorageDir, filename))
			os.Remove(filepath.Join(hm.StorageDir, gzFilename))
			removed++
		}
	}

	// Remove excess sessions if over limit
	if len(sessions) > maxSessions {
		excess := sessions[maxSessions:]
		for _, session := range excess {
			if !storageSessionIDPattern.MatchString(session.ID) {
				continue
			}
			filename := fmt.Sprintf("session_%s.json", session.ID)
			gzFilename := fmt.Sprintf("session_%s.json.gz", session.ID)

			os.Remove(filepath.Join(hm.StorageDir, filename))
			os.Remove(filepath.Join(hm.StorageDir, gzFilename))
			removed++
		}
	}

	if removed > 0 {
		ui.AIln("Cleaned up %d old sessions", removed)
	}
	return nil
}

func (hm *HistoryManager) extractKeyTopics(messages []intelligence.Message) []string {
	topicCounts := make(map[string]int)

	// Simple keyword extraction
	keywords := []string{
		"code", "function", "api", "database", "error", "bug", "fix",
		"performance", "security", "optimization", "algorithm", "data",
		"server", "client", "frontend", "backend", "deploy", "test",
		"python", "go", "javascript", "typescript", "sql", "json",
		"docker", "kubernetes", "aws", "cloud", "web", "mobile",
	}

	for _, msg := range messages {
		content := strings.ToLower(msg.Content)
		for _, keyword := range keywords {
			if strings.Contains(content, keyword) {
				topicCounts[keyword]++
			}
		}
	}

	// Sort by frequency and return top topics
	type topicFreq struct {
		topic string
		count int
	}

	var topics []topicFreq
	for topic, count := range topicCounts {
		if count >= 2 { // Minimum frequency
			topics = append(topics, topicFreq{topic, count})
		}
	}

	sort.Slice(topics, func(i, j int) bool {
		return topics[i].count > topics[j].count
	})

	var result []string
	for i, topic := range topics {
		if i >= 5 { // Limit to top 5 topics
			break
		}
		result = append(result, topic.topic)
	}

	return result
}

func truncateMessage(message string, maxLength int) string {
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength] + "..."
}

// Supporting types

type SessionSummary struct {
	ID            string        `json:"id"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	Duration      time.Duration `json:"duration"`
	MessageCount  int           `json:"message_count"`
	Model         string        `json:"model"`
	ResumeCommand string        `json:"resume_command"`
	FirstMessage  string        `json:"first_message"`
	LastMessage   string        `json:"last_message"`
	KeyTopics     []string      `json:"key_topics"`
}

type StorageStats struct {
	TotalSessions      int       `json:"total_sessions"`
	CompressedSessions int       `json:"compressed_sessions"`
	TotalSizeBytes     int64     `json:"total_size_bytes"`
	OldestSession      time.Time `json:"oldest_session"`
	NewestSession      time.Time `json:"newest_session"`
}

// ConvertToIntelligenceMessage converts chat.Message to intelligence.Message
func ConvertToIntelligenceMessage(content, role string) intelligence.Message {
	return intelligence.Message{
		Content:   content,
		Role:      role,
		Timestamp: time.Now(),
	}
}

// ConvertFromIntelligenceMessage converts intelligence.Message to basic content/role
func ConvertFromIntelligenceMessage(msg intelligence.Message) (string, string) {
	return msg.Content, msg.Role
}
