package analytics

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
)

func TestRecordMessageEstimatesTokensByRole(t *testing.T) {
	tracker := NewChatAnalytics()
	tracker.RecordMessage("user", 40)
	tracker.RecordMessage("assistant", 20)
	tracker.RecordMessage("context", 12)

	data, err := json.Marshal(tracker.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]float64{
		"user_tokens_estimate": 10, "assistant_tokens_estimate": 5,
		"context_tokens_estimate": 3, "total_tokens_estimate": 18,
	} {
		if got := fields[key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if got := fields["user_messages"]; got != float64(1) {
		t.Errorf("user_messages = %v, want 1", got)
	}
}

func TestRecordPromptWithContextSplitsTokensWithoutAddingMessage(t *testing.T) {
	tracker := NewChatAnalytics()
	tracker.RecordPromptWithContext(40, 80)
	snapshot := tracker.Snapshot()
	if snapshot.MessagesTotal != 1 || snapshot.UserMessages != 1 || snapshot.ContextMessages != 1 {
		t.Fatalf("message counts = total:%d user:%d context:%d, want one submitted message with context", snapshot.MessagesTotal, snapshot.UserMessages, snapshot.ContextMessages)
	}
	if snapshot.UserTokensEstimate == 0 || snapshot.ContextTokensEstimate == 0 || snapshot.TotalTokensEstimate != snapshot.UserTokensEstimate+snapshot.ContextTokensEstimate {
		t.Fatalf("token estimates = total:%d user:%d context:%d, want split role totals", snapshot.TotalTokensEstimate, snapshot.UserTokensEstimate, snapshot.ContextTokensEstimate)
	}
	if snapshot.DailyUserMessages[localDateKey(time.Now())] != 1 {
		t.Fatalf("daily user messages = %v, want one submitted prompt", snapshot.DailyUserMessages)
	}
}

func TestDisplayStatisticsIncludesRoleEstimatesAndModelMetrics(t *testing.T) {
	var output bytes.Buffer
	previousOutput := color.Output
	color.Output = &output
	t.Cleanup(func() { color.Output = previousOutput })

	tracker := NewChatAnalytics()
	tracker.RecordMessage("user", 40)
	tracker.RecordMessage("assistant", 20)
	tracker.RecordMessage("context", 12)
	tracker.RecordCommand("/zeta")
	tracker.RecordCommand("/alpha")
	tracker.RecordModelInteraction("model-z", 200*time.Millisecond, true, "")
	tracker.RecordModelInteraction("model-a", 100*time.Millisecond, true, "")
	tracker.RecordVQDRefresh()
	tracker.RecordHeaderRefresh()
	tracker.DisplayStatistics()

	got := output.String()
	for _, expected := range []string{
		"Estimated Tokens: ~18 total (user ~10, assistant ~5, context ~3)",
		"Daily User Activity:",
		"Model Performance:",
		"model-a: 1 requests, 100.0% success",
		"model-z: 1 requests, 100.0% success",
		"VQD refreshes: 1 | Header refreshes: 1",
	} {
		if !strings.Contains(got, expected) {
			t.Errorf("statistics output missing %q:\n%s", expected, got)
		}
	}
	if strings.Index(got, "/alpha: 1") > strings.Index(got, "/zeta: 1") {
		t.Errorf("commands are not sorted alphabetically:\n%s", got)
	}
}

func TestRecordMessageTracksUserMessagesByLocalDate(t *testing.T) {
	tracker := NewChatAnalytics()
	localDate := time.Now().In(time.Local).Format("2006-01-02")
	tracker.RecordMessage("user", 4)
	tracker.RecordMessage("assistant", 4)
	tracker.RecordMessage("user", 4)
	if got := tracker.Snapshot().DailyUserMessages[localDate]; got != 2 {
		t.Fatalf("daily user messages for %s = %d, want 2", localDate, got)
	}
}

func TestRecordMessageTracksLocalDateAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("timezone database unavailable: %v", err)
	}
	previous := time.Local
	time.Local = location
	t.Cleanup(func() { time.Local = previous })

	tracker := NewChatAnalytics()
	clock := time.Date(2026, time.March, 8, 4, 30, 0, 0, time.UTC)
	tracker.now = func() time.Time { return clock }
	tracker.RecordMessage("user", 4)
	clock = time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC)
	tracker.RecordMessage("user", 4)

	got := tracker.Snapshot().DailyUserMessages
	if got["2026-03-07"] != 1 || got["2026-03-08"] != 1 {
		t.Fatalf("daily user messages across DST = %#v, want one on Mar 7 and one on Mar 8", got)
	}
}
