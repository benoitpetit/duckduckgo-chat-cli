package dashboard

import (
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
)

func TestAggregateDailyActivityMergesSnapshots(t *testing.T) {
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.Local)
	history := []analytics.Snapshot{
		{DailyActivityAvailableFrom: "2026-10-02", DailyUserMessages: map[string]int{"2026-10-02": 2, "2026-10-04": 3}},
		{DailyActivityAvailableFrom: "2026-10-01", DailyUserMessages: map[string]int{"2026-10-02": 1}},
	}
	got := aggregateDailyActivity(history, analytics.Snapshot{}, now, 5)
	if len(got) != 5 || got[0].Date != "2026-10-02" || got[4].Date != "2026-10-06" {
		t.Fatalf("daily range = %+v, want Oct 2 through Oct 6", got)
	}
	if got[0].UserMessages != 3 || got[2].UserMessages != 3 || !got[3].Available || got[3].UserMessages != 0 {
		t.Fatalf("daily values = %+v, want merged counts and tracked zero days", got)
	}
}

func TestAggregateDailyActivityUsesCurrentSession(t *testing.T) {
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.Local)
	history := []analytics.Snapshot{{DailyActivityAvailableFrom: "2026-10-05", DailyUserMessages: map[string]int{"2026-10-05": 1}}}
	current := analytics.Snapshot{DailyActivityAvailableFrom: "2026-10-05", DailyUserMessages: map[string]int{"2026-10-05": 2, "2026-10-06": 4}}
	got := aggregateDailyActivity(history, current, now, 2)
	if len(got) != 2 || got[0].UserMessages != 3 || got[1].UserMessages != 4 {
		t.Fatalf("daily history plus current session = %+v, want counts [3 4]", got)
	}
}

func TestAggregateDailyActivityDoesNotDoubleCountPersistedCurrentSession(t *testing.T) {
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.Local)
	sessionStart := time.Date(2026, time.October, 6, 9, 0, 0, 0, time.Local)
	history := []analytics.Snapshot{{
		SessionStartTime: sessionStart, DailyActivityAvailableFrom: "2026-10-06", DailyUserMessages: map[string]int{"2026-10-06": 2},
	}}
	current := analytics.Snapshot{
		SessionStartTime: sessionStart, DailyActivityAvailableFrom: "2026-10-06", DailyUserMessages: map[string]int{"2026-10-06": 5},
	}
	got := aggregateDailyActivity(history, current, now, 1)
	if got[0].UserMessages != 5 {
		t.Fatalf("activity count = %d, want live session snapshot count 5 without persisted duplicate", got[0].UserMessages)
	}
}

func TestAggregateDailyActivityMarksPreTrackingDatesUnavailable(t *testing.T) {
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.Local)
	current := analytics.Snapshot{DailyActivityAvailableFrom: "2026-10-04"}
	got := aggregateDailyActivity(nil, current, now, 5)
	if got[0].Available || got[1].Available || !got[2].Available || !got[4].Available {
		t.Fatalf("tracking availability = %+v, want Oct 2-3 unavailable and Oct 4-6 available", got)
	}
	if got[2].UserMessages != 0 {
		t.Fatalf("tracked date without activity = %+v, want zero user messages", got[2])
	}
}

func TestAggregateDailyActivityHonorsRetention(t *testing.T) {
	now := time.Date(2026, time.October, 6, 12, 0, 0, 0, time.Local)
	got := aggregateDailyActivity(nil, analytics.Snapshot{}, now, 3)
	if len(got) != 3 || got[0].Date != "2026-10-04" || got[2].Date != "2026-10-06" {
		t.Fatalf("retained daily range = %+v, want 3 days ending Oct 6", got)
	}
}
