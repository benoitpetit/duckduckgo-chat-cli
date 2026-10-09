package dashboard

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"duckduckgo-chat-cli/internal/analytics"
	"duckduckgo-chat-cli/internal/ui"
)

const defaultHistoryRetentionDays = 90

// HistoryStore persists aggregate session snapshots independently of transcripts.
type HistoryStore struct {
	mu            sync.Mutex
	path          string
	retentionDays int
}

func NewHistoryStore(path string, retentionDays int) *HistoryStore {
	if retentionDays < 1 || retentionDays > 3650 {
		retentionDays = defaultHistoryRetentionDays
	}
	return &HistoryStore{path: path, retentionDays: retentionDays}
}

func (s *HistoryStore) Save(snapshot analytics.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if snapshot.SessionStartTime.IsZero() {
		return fmt.Errorf("snapshot has no session start time")
	}
	entries, err := s.readLocked()
	if err != nil {
		return err
	}
	updated := false
	for i := range entries {
		if entries[i].SessionStartTime.Equal(snapshot.SessionStartTime) {
			entries[i] = snapshot
			updated = true
			break
		}
	}
	if !updated {
		entries = append(entries, snapshot)
	}
	return s.writeLocked(pruneSnapshots(entries, s.retentionDays, time.Now()))
}

func (s *HistoryStore) List() ([]analytics.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	return pruneSnapshots(entries, s.retentionDays, time.Now()), nil
}

func (s *HistoryStore) SetRetentionDays(days int) error {
	if days < 1 || days > 3650 {
		return fmt.Errorf("retention days must be between 1 and 3650")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retentionDays = days
	entries, err := s.readLocked()
	if err != nil {
		return err
	}
	return s.writeLocked(pruneSnapshots(entries, days, time.Now()))
}

func (s *HistoryStore) readLocked() ([]analytics.Snapshot, error) {
	file, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return []analytics.Snapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	entries := make([]analytics.Snapshot, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		var snapshot analytics.Snapshot
		if err := json.Unmarshal(scanner.Bytes(), &snapshot); err != nil || snapshot.SessionStartTime.IsZero() {
			ui.Warningln("Ignoring corrupt dashboard history entry at line %d", line)
			continue
		}
		entries = append(entries, snapshot)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dashboard history: %w", err)
	}
	return entries, nil
}

func (s *HistoryStore) writeLocked(entries []analytics.Snapshot) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create dashboard history directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".dashboard-history-*.tmp")
	if err != nil {
		return fmt.Errorf("create dashboard history temp file: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	encoder := json.NewEncoder(tmp)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			tmp.Close()
			return fmt.Errorf("encode dashboard history: %w", err)
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync dashboard history: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close dashboard history: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace dashboard history: %w", err)
	}
	ok = true
	return nil
}

func pruneSnapshots(entries []analytics.Snapshot, retentionDays int, now time.Time) []analytics.Snapshot {
	cutoff := now.Add(-time.Duration(retentionDays) * 24 * time.Hour)
	kept := make([]analytics.Snapshot, 0, len(entries))
	for _, entry := range entries {
		if !entry.SessionStartTime.Before(cutoff) {
			kept = append(kept, entry)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].SessionStartTime.Before(kept[j].SessionStartTime) })
	return kept
}
