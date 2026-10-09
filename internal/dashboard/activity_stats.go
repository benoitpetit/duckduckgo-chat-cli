package dashboard

import (
	"time"

	"duckduckgo-chat-cli/internal/analytics"
)

type DailyActivityDay struct {
	Date         string `json:"date"`
	UserMessages int    `json:"userMessages"`
	Available    bool   `json:"available"`
}

func aggregateDailyActivity(history []analytics.Snapshot, current analytics.Snapshot, now time.Time, retentionDays int) []DailyActivityDay {
	if retentionDays <= 0 {
		return []DailyActivityDay{}
	}
	today := now.In(time.Local)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.Local)
	start := today.AddDate(0, 0, -(retentionDays - 1))
	counts := make(map[string]int)
	var availableFrom time.Time

	addSnapshot := func(snapshot analytics.Snapshot) {
		if marker, err := time.ParseInLocation("2006-01-02", snapshot.DailyActivityAvailableFrom, time.Local); err == nil {
			if availableFrom.IsZero() || marker.Before(availableFrom) {
				availableFrom = marker
			}
		}
		for date, count := range snapshot.DailyUserMessages {
			day, err := time.ParseInLocation("2006-01-02", date, time.Local)
			if err != nil {
				continue
			}
			counts[date] += count
			if availableFrom.IsZero() || day.Before(availableFrom) {
				availableFrom = day
			}
		}
	}
	for _, snapshot := range history {
		if !current.SessionStartTime.IsZero() && snapshot.SessionStartTime.Equal(current.SessionStartTime) {
			continue // The active session is already represented by its fresher live snapshot.
		}
		addSnapshot(snapshot)
	}
	addSnapshot(current)

	days := make([]DailyActivityDay, 0, retentionDays)
	for day := start; !day.After(today); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		available := !availableFrom.IsZero() && !day.Before(availableFrom)
		days = append(days, DailyActivityDay{Date: key, UserMessages: counts[key], Available: available})
	}
	return days
}
