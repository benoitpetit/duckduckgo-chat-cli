package analytics

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestSnapshotCopiesSessionAndModelMetrics(t *testing.T) {
	tracker := NewChatAnalytics()
	tracker.RecordMessage("user", 16)
	tracker.RecordCommand("/help")
	tracker.RecordChatInteraction(20*time.Millisecond, true, "")
	tracker.RecordModelInteraction("model-a", 20*time.Millisecond, true, "")
	tracker.RecordChatInteraction(40*time.Millisecond, false, "429")
	tracker.RecordModelInteraction("model-a", 40*time.Millisecond, false, "429")
	tracker.RecordModelInteraction("model-b", 60*time.Millisecond, true, "")

	snapshot := tracker.Snapshot()
	if snapshot.SessionStartTime.IsZero() || snapshot.MessagesTotal != 1 || snapshot.UserMessages != 1 || snapshot.ChatInteractionsTotal != 2 {
		t.Fatalf("snapshot is missing session counters: %+v", snapshot)
	}
	model := snapshot.ByModel["model-a"]
	if model.Interactions != 2 || model.Successful != 1 || model.Failed != 1 || model.TotalResponseTime != 60*time.Millisecond || model.AverageResponseTime != 30*time.Millisecond {
		t.Fatalf("model-a metrics = %+v", model)
	}
	if snapshot.ByModel["model-b"].Successful != 1 {
		t.Fatalf("model-b metrics missing: %+v", snapshot.ByModel["model-b"])
	}

	snapshot.CommandsUsed["/help"] = 99
	snapshot.ByModel["model-a"] = ModelMetrics{}
	if got := tracker.Snapshot(); got.CommandsUsed["/help"] != 1 || got.ByModel["model-a"].Interactions != 2 {
		t.Fatalf("snapshot exposed mutable tracker maps: %+v", got)
	}
}

func TestSnapshotCopiesDailyActivityMap(t *testing.T) {
	tracker := NewChatAnalytics()
	tracker.RecordMessage("user", 12)
	snapshot := tracker.Snapshot()
	snapshot.DailyUserMessages[time.Now().In(time.Local).Format("2006-01-02")] = 99
	if got := tracker.Snapshot().DailyUserMessages[time.Now().In(time.Local).Format("2006-01-02")]; got != 1 {
		t.Fatalf("tracker daily messages = %d after snapshot mutation, want 1", got)
	}
}

func TestOldSnapshotJSONDefaultsNewFields(t *testing.T) {
	var snapshot Snapshot
	if err := json.Unmarshal([]byte(`{"messages_total":2,"total_tokens_estimate":3}`), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.MessagesTotal != 2 || snapshot.TotalTokensEstimate != 3 {
		t.Fatalf("old snapshot counters changed: %+v", snapshot)
	}
	value := reflect.ValueOf(snapshot)
	for _, name := range []string{"UserTokensEstimate", "AssistantTokensEstimate", "ContextTokensEstimate", "DailyActivityAvailableFrom"} {
		if field := value.FieldByName(name); !field.IsValid() || !field.IsZero() {
			t.Errorf("old snapshot field %s should default to zero value", name)
		}
	}
}

func TestSnapshotConcurrentRecording(t *testing.T) {
	tracker := NewChatAnalytics()
	const writers = 8
	const records = 500
	var wg sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for i := 0; i < records; i++ {
				ok := i%2 == 0
				tracker.RecordChatInteraction(time.Millisecond, ok, "other")
				tracker.RecordModelInteraction("shared", time.Millisecond, ok, "other")
				tracker.RecordCommand("/dashboard")
				if i%10 == 0 {
					snapshot := tracker.Snapshot()
					metrics := snapshot.ByModel["shared"]
					if metrics.Successful+metrics.Failed != metrics.Interactions {
						t.Errorf("inconsistent concurrent snapshot: %+v", metrics)
						return
					}
				}
			}
		}(writer)
	}
	wg.Wait()
	snapshot := tracker.Snapshot()
	want := writers * records
	if snapshot.ChatInteractionsTotal != want || snapshot.ByModel["shared"].Interactions != want || snapshot.CommandsUsed["/dashboard"] != want {
		t.Fatalf("lost concurrent updates: chat=%d model=%d commands=%d want=%d", snapshot.ChatInteractionsTotal, snapshot.ByModel["shared"].Interactions, snapshot.CommandsUsed["/dashboard"], want)
	}
}
