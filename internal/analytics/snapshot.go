package analytics

import "time"

// ModelMetrics summarizes backend interactions observed for one model.
type ModelMetrics struct {
	Interactions        int           `json:"interactions"`
	Successful          int           `json:"successful"`
	Failed              int           `json:"failed"`
	TotalResponseTime   time.Duration `json:"total_response_time"`
	AverageResponseTime time.Duration `json:"average_response_time"`
	Error418            int           `json:"error_418"`
	Error429            int           `json:"error_429"`
	OtherErrors         int           `json:"other_errors"`
}

// Snapshot is an immutable copy of the counters useful to local reporting.
// Its maps are newly allocated for every call to ChatAnalytics.Snapshot.
type Snapshot struct {
	SessionStartTime           time.Time               `json:"session_start_time"`
	SessionEndTime             time.Time               `json:"session_end_time"`
	SessionDuration            time.Duration           `json:"session_duration"`
	ChatInteractionsTotal      int                     `json:"chat_interactions_total"`
	ChatInteractionsSuccessful int                     `json:"chat_interactions_successful"`
	ChatInteractionsFailed     int                     `json:"chat_interactions_failed"`
	TotalChatResponseTime      time.Duration           `json:"total_chat_response_time"`
	AverageChatResponseTime    time.Duration           `json:"average_chat_response_time"`
	APICallsTotal              int                     `json:"api_calls_total"`
	APICallsSuccessful         int                     `json:"api_calls_successful"`
	APICallsFailed             int                     `json:"api_calls_failed"`
	TotalAPIResponseTime       time.Duration           `json:"total_api_response_time"`
	AverageAPIResponseTime     time.Duration           `json:"average_api_response_time"`
	Error418Count              int                     `json:"error_418_count"`
	Error429Count              int                     `json:"error_429_count"`
	OtherErrorsCount           int                     `json:"other_errors_count"`
	VQDRefreshCount            int                     `json:"vqd_refresh_count"`
	HeaderRefreshCount         int                     `json:"header_refresh_count"`
	MessagesTotal              int                     `json:"messages_total"`
	UserMessages               int                     `json:"user_messages"`
	AssistantMessages          int                     `json:"assistant_messages"`
	ContextMessages            int                     `json:"context_messages"`
	TotalTokensEstimate        int                     `json:"total_tokens_estimate"`
	ContextOptimizations       int                     `json:"context_optimizations"`
	ContextCompressions        int                     `json:"context_compressions"`
	BytesSaved                 int64                   `json:"bytes_saved"`
	CommandsUsed               map[string]int          `json:"commands_used"`
	ModelChanges               int                     `json:"model_changes"`
	CurrentModel               string                  `json:"current_model"`
	FilesProcessed             int                     `json:"files_processed"`
	URLsProcessed              int                     `json:"urls_processed"`
	SearchesPerformed          int                     `json:"searches_performed"`
	ByModel                    map[string]ModelMetrics `json:"by_model"`
}

// Snapshot copies the tracker under a read lock and never returns its mutable maps.
func (ca *ChatAnalytics) Snapshot() Snapshot {
	ca.mutex.RLock()
	defer ca.mutex.RUnlock()

	snapshot := Snapshot{
		SessionStartTime: ca.SessionStartTime, SessionEndTime: ca.SessionEndTime, SessionDuration: ca.SessionDuration,
		ChatInteractionsTotal: ca.ChatInteractionsTotal, ChatInteractionsSuccessful: ca.ChatInteractionsSuccessful,
		ChatInteractionsFailed: ca.ChatInteractionsFailed, TotalChatResponseTime: ca.TotalChatResponseTime,
		AverageChatResponseTime: ca.AverageChatResponseTime,
		APICallsTotal:           ca.APICallsTotal, APICallsSuccessful: ca.APICallsSuccessful, APICallsFailed: ca.APICallsFailed,
		TotalAPIResponseTime: ca.TotalAPIResponseTime, AverageAPIResponseTime: ca.AverageAPIResponseTime,
		Error418Count: ca.Error418Count, Error429Count: ca.Error429Count, OtherErrorsCount: ca.OtherErrorsCount,
		VQDRefreshCount: ca.VQDRefreshCount, HeaderRefreshCount: ca.HeaderRefreshCount,
		MessagesTotal: ca.MessagesTotal, UserMessages: ca.UserMessages, AssistantMessages: ca.AssistantMessages,
		ContextMessages: ca.ContextMessages, TotalTokensEstimate: ca.TotalTokensEstimate,
		ContextOptimizations: ca.ContextOptimizations, ContextCompressions: ca.ContextCompressions, BytesSaved: ca.BytesSaved,
		ModelChanges: ca.ModelChanges, CurrentModel: ca.CurrentModel, FilesProcessed: ca.FilesProcessed,
		URLsProcessed: ca.URLsProcessed, SearchesPerformed: ca.SearchesPerformed,
		CommandsUsed: make(map[string]int, len(ca.CommandsUsed)), ByModel: make(map[string]ModelMetrics, len(ca.byModel)),
	}
	for command, count := range ca.CommandsUsed {
		snapshot.CommandsUsed[command] = count
	}
	for model, metrics := range ca.byModel {
		snapshot.ByModel[model] = metrics
	}
	return snapshot
}
