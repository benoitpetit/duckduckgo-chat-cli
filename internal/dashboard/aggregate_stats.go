package dashboard

import (
	"time"

	"duckduckgo-chat-cli/internal/analytics"
)

// aggregateSessionStats combines retained session snapshots and replaces any
// persisted copy of the active session with its fresher live snapshot.
func aggregateSessionStats(history []analytics.Snapshot, current analytics.Snapshot) (analytics.Snapshot, int) {
	entries := make([]analytics.Snapshot, 0, len(history)+1)
	for _, snapshot := range history {
		if !current.SessionStartTime.IsZero() && snapshot.SessionStartTime.Equal(current.SessionStartTime) {
			continue
		}
		entries = append(entries, snapshot)
	}
	if !current.SessionStartTime.IsZero() || hasSnapshotActivity(current) {
		entries = append(entries, current)
	}

	var total analytics.Snapshot
	var sessions int
	var modelTime time.Time
	for _, snapshot := range entries {
		if snapshot.SessionStartTime.IsZero() && !hasSnapshotActivity(snapshot) {
			continue
		}
		sessions++
		if total.SessionStartTime.IsZero() || (!snapshot.SessionStartTime.IsZero() && snapshot.SessionStartTime.Before(total.SessionStartTime)) {
			total.SessionStartTime = snapshot.SessionStartTime
		}
		if snapshot.SessionEndTime.After(total.SessionEndTime) {
			total.SessionEndTime = snapshot.SessionEndTime
		}
		total.SessionDuration += snapshot.SessionDuration
		total.ChatInteractionsTotal += snapshot.ChatInteractionsTotal
		total.ChatInteractionsSuccessful += snapshot.ChatInteractionsSuccessful
		total.ChatInteractionsFailed += snapshot.ChatInteractionsFailed
		total.TotalChatResponseTime += snapshot.TotalChatResponseTime
		total.APICallsTotal += snapshot.APICallsTotal
		total.APICallsSuccessful += snapshot.APICallsSuccessful
		total.APICallsFailed += snapshot.APICallsFailed
		total.TotalAPIResponseTime += snapshot.TotalAPIResponseTime
		total.Error418Count += snapshot.Error418Count
		total.Error429Count += snapshot.Error429Count
		total.OtherErrorsCount += snapshot.OtherErrorsCount
		total.VQDRefreshCount += snapshot.VQDRefreshCount
		total.HeaderRefreshCount += snapshot.HeaderRefreshCount
		total.MessagesTotal += snapshot.MessagesTotal
		total.UserMessages += snapshot.UserMessages
		total.AssistantMessages += snapshot.AssistantMessages
		total.ContextMessages += snapshot.ContextMessages
		total.TotalTokensEstimate += snapshot.TotalTokensEstimate
		total.UserTokensEstimate += snapshot.UserTokensEstimate
		total.AssistantTokensEstimate += snapshot.AssistantTokensEstimate
		total.ContextTokensEstimate += snapshot.ContextTokensEstimate
		total.ContextOptimizations += snapshot.ContextOptimizations
		total.ContextCompressions += snapshot.ContextCompressions
		total.BytesSaved += snapshot.BytesSaved
		total.ModelChanges += snapshot.ModelChanges
		total.FilesProcessed += snapshot.FilesProcessed
		total.URLsProcessed += snapshot.URLsProcessed
		total.SearchesPerformed += snapshot.SearchesPerformed
		mergeCounts(&total.CommandsUsed, snapshot.CommandsUsed)
		mergeCounts(&total.DailyUserMessages, snapshot.DailyUserMessages)
		for model, metrics := range snapshot.ByModel {
			if total.ByModel == nil {
				total.ByModel = make(map[string]analytics.ModelMetrics)
			}
			merged := total.ByModel[model]
			merged.Interactions += metrics.Interactions
			merged.Successful += metrics.Successful
			merged.Failed += metrics.Failed
			merged.TotalResponseTime += metrics.TotalResponseTime
			merged.Error418 += metrics.Error418
			merged.Error429 += metrics.Error429
			merged.OtherErrors += metrics.OtherErrors
			total.ByModel[model] = merged
		}
		if modelTime.IsZero() || snapshot.SessionStartTime.After(modelTime) {
			total.CurrentModel = snapshot.CurrentModel
			modelTime = snapshot.SessionStartTime
		}
		if snapshot.DailyActivityAvailableFrom != "" {
			if total.DailyActivityAvailableFrom == "" || snapshot.DailyActivityAvailableFrom < total.DailyActivityAvailableFrom {
				total.DailyActivityAvailableFrom = snapshot.DailyActivityAvailableFrom
			}
		}
	}
	if total.ChatInteractionsTotal > 0 {
		total.AverageChatResponseTime = total.TotalChatResponseTime / time.Duration(total.ChatInteractionsTotal)
	}
	if total.APICallsTotal > 0 {
		total.AverageAPIResponseTime = total.TotalAPIResponseTime / time.Duration(total.APICallsTotal)
	}
	for model, metrics := range total.ByModel {
		if metrics.Interactions > 0 {
			metrics.AverageResponseTime = metrics.TotalResponseTime / time.Duration(metrics.Interactions)
			total.ByModel[model] = metrics
		}
	}
	return total, sessions
}

func hasSnapshotActivity(snapshot analytics.Snapshot) bool {
	return snapshot.MessagesTotal != 0 || snapshot.ChatInteractionsTotal != 0 || snapshot.APICallsTotal != 0 || snapshot.UserMessages != 0 || snapshot.TotalTokensEstimate != 0 || len(snapshot.CommandsUsed) != 0 || len(snapshot.ByModel) != 0
}

func mergeCounts(target *map[string]int, source map[string]int) {
	if len(source) == 0 {
		return
	}
	if *target == nil {
		*target = make(map[string]int, len(source))
	}
	for key, count := range source {
		(*target)[key] += count
	}
}
