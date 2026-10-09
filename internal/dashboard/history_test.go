package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
)

func TestHistoryStoreAppendsAndUpsertsBySessionStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	store := NewHistoryStore(path, 90)
	started := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	first := analytics.Snapshot{SessionStartTime: started, MessagesTotal: 2}
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	first.MessagesTotal = 5
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	second := analytics.Snapshot{SessionStartTime: started.Add(time.Hour), MessagesTotal: 7}
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].MessagesTotal != 5 || !got[0].SessionStartTime.Equal(started) {
		t.Fatalf("history = %+v, want stable session upsert and both entries", got)
	}
}

func TestHistoryStorePrunesByConfigurableRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	store := NewHistoryStore(path, 3)
	now := time.Now().UTC()
	for _, age := range []time.Duration{2 * 24 * time.Hour, 4 * 24 * time.Hour} {
		if err := store.Save(analytics.Snapshot{SessionStartTime: now.Add(-age), MessagesTotal: int(age.Hours())}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].SessionStartTime.Before(now.Add(-3*24*time.Hour)) {
		t.Fatalf("retained history = %+v, want only recent snapshot", got)
	}
	if err := store.SetRetentionDays(1); err != nil {
		t.Fatal(err)
	}
	got, err = store.List()
	if err != nil || len(got) != 0 {
		t.Fatalf("retention update left entries: %+v, err=%v", got, err)
	}
}

func TestHistoryStoreIgnoresCorruptEntriesAndWritesPrivateAtomicFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	started := time.Now().UTC().Truncate(time.Second)
	valid, err := json.Marshal(analytics.Snapshot{SessionStartTime: started, MessagesTotal: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(append([]byte("{corrupt}\n"), valid...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewHistoryStore(path, 90)
	got, err := store.List()
	if err != nil || len(got) != 1 || got[0].MessagesTotal != 3 {
		t.Fatalf("List() = %+v, %v; want valid entry despite corrupt line", got, err)
	}
	if err := store.Save(analytics.Snapshot{SessionStartTime: started.Add(time.Minute), MessagesTotal: 4}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("history permissions = %o, want 600", info.Mode().Perm())
	}
	files, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name() != filepath.Base(path) {
		t.Fatalf("temporary file was not cleaned after atomic replacement: %v", files)
	}
}

func TestHistoryStoreDefaultsRetentionToNinetyDays(t *testing.T) {
	store := NewHistoryStore(filepath.Join(t.TempDir(), "history.jsonl"), 0)
	old := time.Now().Add(-91 * 24 * time.Hour)
	if err := store.Save(analytics.Snapshot{SessionStartTime: old}); err != nil {
		t.Fatal(err)
	}
	if got, err := store.List(); err != nil || len(got) != 0 {
		t.Fatalf("default retention kept old record: %+v, err=%v", got, err)
	}
}
