package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"duckduckgo-chat-cli/internal/chatcontext"
	"duckduckgo-chat-cli/internal/intelligence"
	"duckduckgo-chat-cli/internal/media"
)

func TestHistoryManagerUsesConfigurableRetention(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	if manager.RetentionDays != 90 {
		t.Fatalf("default retention = %d, want 90", manager.RetentionDays)
	}
	old := &ConversationSession{ID: "old", StartTime: time.Now().Add(-60 * 24 * time.Hour), Messages: []intelligence.Message{{Role: "user", Content: "old"}}}
	recent := &ConversationSession{ID: "recent", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: "recent"}}}
	if err := manager.SaveSession(old); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveSession(recent); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRetentionDays(30); err != nil {
		t.Fatal(err)
	}
	sessions, err := manager.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "recent" {
		t.Fatalf("sessions after retention update = %+v, want only recent", sessions)
	}
}

func TestHistoryManagerRejectsInvalidRetention(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	if err := manager.SetRetentionDays(0); err == nil {
		t.Fatal("SetRetentionDays(0) succeeded")
	}
}

func TestCleanupOldSessionsReportsRemovalFailureAndKeepsArchive(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	manager.RetentionDays = 365
	session := &ConversationSession{
		ID:        "retention_blocked",
		StartTime: time.Now().Add(-2 * 24 * time.Hour),
		Messages:  []intelligence.Message{{Role: "user", Content: "keep archive"}},
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatal(err)
	}
	child := &ConversationSession{
		ID: "retention_child", StartTime: time.Now(), ParentSessionID: session.ID,
		BranchName: "follow-up", Messages: []intelligence.Message{{Role: "user", Content: "keep lineage"}},
	}
	if err := manager.SaveSession(child); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, "session_"+session.ID+".json")
	if err := os.Mkdir(legacyPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyPath, "keep"), []byte("block removal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRetentionDays(1); err == nil {
		t.Fatal("SetRetentionDays() hid a session archive removal failure")
	}
	if _, err := manager.LoadSession(session.ID); err != nil {
		t.Fatalf("session archive was lost after cleanup failure: %v", err)
	}
	loadedChild, err := manager.LoadSession(child.ID)
	if err != nil {
		t.Fatalf("child archive was lost after cleanup failure: %v", err)
	}
	if loadedChild.ParentSessionID != session.ID || loadedChild.BranchName != child.BranchName {
		t.Fatalf("child lineage after cleanup failure = (%q, %q), want (%q, %q)", loadedChild.ParentSessionID, loadedChild.BranchName, session.ID, child.BranchName)
	}
}

func TestHistoryManagerRestartAppliesLongerRetentionBeforePruning(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	if err := manager.SetRetentionDays(365); err != nil {
		t.Fatal(err)
	}
	archived := &ConversationSession{ID: "session_120_days", StartTime: time.Now().Add(-120 * 24 * time.Hour), Messages: []intelligence.Message{{Role: "user", Content: "keep me"}}}
	if err := manager.SaveSession(archived); err != nil {
		t.Fatal(err)
	}

	// NewHistoryManager must not apply its 90-day fallback before the caller
	// can restore the configured 365-day retention.
	restarted := NewHistoryManager(dir)
	if err := restarted.SetRetentionDays(365); err != nil {
		t.Fatal(err)
	}
	got, err := restarted.LoadSession(archived.ID)
	if err != nil || got.ID != archived.ID {
		t.Fatalf("120-day archive after restart = (%v, %v), want retained archive", got, err)
	}
}

func TestHistoryListingsTreatMissingStorageAsEmpty(t *testing.T) {
	manager := NewHistoryManager(filepath.Join(t.TempDir(), "not-created"))
	if summaries, err := manager.ListSessionSummaries(); err != nil || len(summaries) != 0 {
		t.Fatalf("ListSessionSummaries() = (%v, %v), want empty", summaries, err)
	}
	if sessions, err := manager.ListConversationSessions(); err != nil || len(sessions) != 0 {
		t.Fatalf("ListConversationSessions() = (%v, %v), want empty", sessions, err)
	}
}

func TestHistoryManagerRejectsUnsafeSessionIDs(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	for _, id := range []string{"../outside", "..\\outside", "", ".", "session/../../outside"} {
		if _, err := manager.LoadSession(id); err == nil {
			t.Errorf("LoadSession(%q) succeeded, want invalid ID error", id)
		}
		if err := manager.SaveSession(&ConversationSession{ID: id}); err == nil {
			t.Errorf("SaveSession(%q) succeeded, want invalid ID error", id)
		}
		if err := manager.UpdateSessionMetadata(id, SessionMetadata{Title: "unsafe"}); err == nil {
			t.Errorf("UpdateSessionMetadata(%q) succeeded, want invalid ID error", id)
		}
		if err := manager.DeleteSession(id); err == nil {
			t.Errorf("DeleteSession(%q) succeeded, want invalid ID error", id)
		}
	}
}

func TestSessionMetadataRoundTripAndListSummary(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	started := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	session := &ConversationSession{
		ID: "session_metadata", StartTime: started, Model: "gpt-5.6-luna",
		Messages:  []intelligence.Message{{Role: "user", Content: "Explain the release notes"}, {Role: "assistant", Content: "Done"}},
		Analytics: SessionAnalytics{APICallsCount: 3, ErrorCount: 1, OptimizationsUsed: 2},
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	savedStart, savedEnd, savedAnalytics := session.StartTime, session.EndTime, session.Analytics
	metadata := SessionMetadata{Title: "Release notes", Tags: []string{"docs", "release"}, Favorite: true}
	if err := manager.UpdateSessionMetadata(session.ID, metadata); err != nil {
		t.Fatalf("UpdateSessionMetadata() error = %v", err)
	}
	loaded, err := manager.LoadSession(session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.Title != metadata.Title || !reflect.DeepEqual(loaded.Tags, metadata.Tags) || !loaded.Favorite {
		t.Fatalf("loaded metadata = title:%q tags:%v favorite:%t", loaded.Title, loaded.Tags, loaded.Favorite)
	}
	if !loaded.StartTime.Equal(savedStart) || !loaded.EndTime.Equal(savedEnd) || !reflect.DeepEqual(loaded.Analytics, savedAnalytics) {
		t.Fatalf("metadata edit changed conversation facts: start=%s end=%s analytics=%+v; want start=%s end=%s analytics=%+v", loaded.StartTime, loaded.EndTime, loaded.Analytics, savedStart, savedEnd, savedAnalytics)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[0].Content != "Explain the release notes" {
		t.Fatalf("metadata edit changed transcript: %+v", loaded.Messages)
	}
	summaries, err := manager.ListSessionSummaries()
	if err != nil {
		t.Fatalf("ListSessionSummaries() error = %v", err)
	}
	if len(summaries) != 1 || summaries[0].Title != metadata.Title || !reflect.DeepEqual(summaries[0].Tags, metadata.Tags) || !summaries[0].Favorite {
		t.Fatalf("session summary metadata = %+v, want %+v", summaries, metadata)
	}
}

func TestLegacySessionTitleGetsGeneratedAndTagsDefaultEmptyAtReadTime(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	legacyData := []byte(`{"id":"legacy_session","start_time":"2024-10-13T00:00:00Z","model":"gpt","messages":[{"role":"user","content":"  Welcome\n back to DuckChat  "}],"analytics":{"message_count":1}}`)
	path := filepath.Join(dir, "session_legacy_session.json")
	if err := os.WriteFile(path, legacyData, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manager.LoadSession("legacy_session")
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.Title != "Welcome back to DuckChat" {
		t.Fatalf("generated title = %q, want normalized first user message", loaded.Title)
	}
	if loaded.Tags == nil || len(loaded.Tags) != 0 {
		t.Fatalf("legacy tags = %#v, want an empty slice", loaded.Tags)
	}
	summaries, err := manager.ListSessionSummaries()
	if err != nil || len(summaries) != 1 || summaries[0].Title != loaded.Title {
		t.Fatalf("ListSessionSummaries() = (%+v, %v), want generated title", summaries, err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), `"title"`) || strings.Contains(string(stored), `"tags"`) {
		t.Fatalf("legacy archive was rewritten during a read: %s", stored)
	}
}

func TestSessionMetadataNormalizesControlsAndDuplicateTags(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	if err := manager.SaveSession(&ConversationSession{ID: "session_normalize", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: "Fallback"}}}); err != nil {
		t.Fatal(err)
	}
	metadata := SessionMetadata{Title: "  Release\n  notes\x00 ", Tags: []string{" Go ", "go", "docs", " DOCS ", ""}}
	if err := manager.UpdateSessionMetadata("session_normalize", metadata); err != nil {
		t.Fatalf("UpdateSessionMetadata() error = %v", err)
	}
	loaded, err := manager.LoadSession("session_normalize")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != "Release notes" || !reflect.DeepEqual(loaded.Tags, []string{"Go", "docs"}) {
		t.Fatalf("normalized metadata = title:%q tags:%v", loaded.Title, loaded.Tags)
	}
}

func TestSessionMetadataRejectsValuesOutsideLimits(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	if err := manager.SaveSession(&ConversationSession{ID: "session_limits", StartTime: time.Now()}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		metadata SessionMetadata
	}{
		{name: "title length", metadata: SessionMetadata{Title: strings.Repeat("界", 101)}},
		{name: "tag length", metadata: SessionMetadata{Tags: []string{strings.Repeat("界", 25)}}},
		{name: "tag count", metadata: SessionMetadata{Tags: []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := manager.UpdateSessionMetadata("session_limits", tt.metadata); err == nil {
				t.Fatal("UpdateSessionMetadata() succeeded for metadata outside limits")
			}
		})
	}
}

func TestDeleteSessionRemovesCompressedAndUncompressedArchives(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	session := &ConversationSession{ID: "session_delete", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: "delete me"}}}
	if err := manager.SaveSession(session); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, "session_session_delete.json")
	if err := os.WriteFile(legacyPath, []byte(`{"id":"session_delete"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	compressedPath := filepath.Join(dir, "session_session_delete.json.gz")
	if err := manager.DeleteSession(session.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	for _, path := range []string{compressedPath, legacyPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("archive %s still exists, stat error = %v", path, err)
		}
	}
}

func TestDeleteSessionRemovesUncompressedLegacyArchive(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	path := filepath.Join(dir, "session_legacy_delete.json")
	if err := os.WriteFile(path, []byte(`{"id":"legacy_delete"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteSession("legacy_delete"); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("legacy archive still exists, stat error = %v", err)
	}
}

func TestDeleteSessionDetachesDirectBranches(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	parent := &ConversationSession{ID: "session_parent", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: "parent"}}}
	child := &ConversationSession{ID: "session_child", StartTime: time.Now(), ParentSessionID: parent.ID, BranchName: "experiment", Messages: []intelligence.Message{{Role: "user", Content: "child"}}}
	secondChild := &ConversationSession{ID: "session_child_two", StartTime: time.Now(), ParentSessionID: parent.ID, BranchName: "follow-up", Messages: []intelligence.Message{{Role: "user", Content: "second child"}}}
	for _, session := range []*ConversationSession{parent, child, secondChild} {
		if err := manager.SaveSession(session); err != nil {
			t.Fatalf("SaveSession(%s): %v", session.ID, err)
		}
	}

	if err := manager.DeleteSession(parent.ID); err != nil {
		t.Fatalf("DeleteSession(%s): %v", parent.ID, err)
	}
	if _, err := manager.LoadSession(parent.ID); err == nil {
		t.Fatal("deleted parent session remained loadable")
	}
	for _, test := range []struct{ id, content string }{{child.ID, "child"}, {secondChild.ID, "second child"}} {
		loadedChild, err := manager.LoadSession(test.id)
		if err != nil {
			t.Fatalf("LoadSession(%s) after deleting parent: %v", test.id, err)
		}
		if loadedChild.ParentSessionID != "" || loadedChild.BranchName != "" {
			t.Errorf("detached child %s lineage = parent:%q branch:%q, want no stale link", test.id, loadedChild.ParentSessionID, loadedChild.BranchName)
		}
		if len(loadedChild.Messages) != 1 || loadedChild.Messages[0].Content != test.content {
			t.Errorf("detaching child %s changed its transcript: %+v", test.id, loadedChild.Messages)
		}
	}
}

func TestDeleteSessionKeepsParentWhenChildDetachmentFails(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	parent := &ConversationSession{ID: "session_parent", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: "parent"}}}
	child := &ConversationSession{ID: "session_child", StartTime: time.Now(), ParentSessionID: parent.ID, BranchName: "experiment", Messages: []intelligence.Message{{Role: "user", Content: "child"}}}
	for _, session := range []*ConversationSession{parent, child} {
		if err := manager.SaveSession(session); err != nil {
			t.Fatalf("SaveSession(%s): %v", session.ID, err)
		}
	}
	manager.CompressionLevel = 99 // Force child archival to fail before parent removal.

	if err := manager.DeleteSession(parent.ID); err == nil {
		t.Fatal("DeleteSession() succeeded although its child could not be detached")
	}
	if _, err := manager.LoadSession(parent.ID); err != nil {
		t.Fatalf("parent was removed after child-detachment failure: %v", err)
	}
	loadedChild, err := manager.LoadSession(child.ID)
	if err != nil {
		t.Fatalf("LoadSession(child) after failed deletion: %v", err)
	}
	if loadedChild.ParentSessionID != parent.ID || loadedChild.BranchName != child.BranchName {
		t.Fatalf("failed detach partially changed child lineage: parent:%q branch:%q", loadedChild.ParentSessionID, loadedChild.BranchName)
	}
}

func TestCleanupOldSessionsDetachesBranchesBeforeRemoval(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	if err := manager.SetRetentionDays(365); err != nil {
		t.Fatalf("SetRetentionDays(365): %v", err)
	}
	parent := &ConversationSession{ID: "session_expired_parent", StartTime: time.Now().Add(-120 * 24 * time.Hour), Messages: []intelligence.Message{{Role: "user", Content: "old parent"}}}
	child := &ConversationSession{ID: "session_recent_child", StartTime: time.Now(), ParentSessionID: parent.ID, BranchName: "current work", Messages: []intelligence.Message{{Role: "user", Content: "recent child"}}}
	for _, session := range []*ConversationSession{parent, child} {
		if err := manager.SaveSession(session); err != nil {
			t.Fatalf("SaveSession(%s): %v", session.ID, err)
		}
	}

	if err := manager.SetRetentionDays(30); err != nil {
		t.Fatalf("SetRetentionDays(30): %v", err)
	}
	if _, err := manager.LoadSession(parent.ID); err == nil {
		t.Fatal("expired parent session remained loadable")
	}
	loadedChild, err := manager.LoadSession(child.ID)
	if err != nil {
		t.Fatalf("LoadSession(%s) after retention cleanup: %v", child.ID, err)
	}
	if loadedChild.ParentSessionID != "" || loadedChild.BranchName != "" {
		t.Fatalf("retained child lineage = parent:%q branch:%q, want no stale link", loadedChild.ParentSessionID, loadedChild.BranchName)
	}
}

func TestDeleteNestedBranchKeepsGrandchildLoadable(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	root := &ConversationSession{ID: "session_root", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: "root"}}}
	branch := &ConversationSession{ID: "session_branch", StartTime: time.Now(), ParentSessionID: root.ID, BranchName: "branch", Messages: []intelligence.Message{{Role: "user", Content: "branch"}}}
	grandchild := &ConversationSession{ID: "session_grandchild", StartTime: time.Now(), ParentSessionID: branch.ID, BranchName: "nested", Messages: []intelligence.Message{{Role: "user", Content: "grandchild"}}}
	for _, session := range []*ConversationSession{root, branch, grandchild} {
		if err := manager.SaveSession(session); err != nil {
			t.Fatalf("SaveSession(%s): %v", session.ID, err)
		}
	}

	if err := manager.DeleteSession(branch.ID); err != nil {
		t.Fatalf("DeleteSession(%s): %v", branch.ID, err)
	}
	loadedRoot, err := manager.LoadSession(root.ID)
	if err != nil || loadedRoot.ID != root.ID {
		t.Fatalf("LoadSession(%s) = (%+v, %v), want surviving root", root.ID, loadedRoot, err)
	}
	loadedGrandchild, err := manager.LoadSession(grandchild.ID)
	if err != nil {
		t.Fatalf("LoadSession(%s) after deleting its parent: %v", grandchild.ID, err)
	}
	if loadedGrandchild.ParentSessionID != "" || loadedGrandchild.BranchName != "" || len(loadedGrandchild.Messages) != 1 || loadedGrandchild.Messages[0].Content != "grandchild" {
		t.Fatalf("grandchild after parent deletion = %+v, want loadable standalone session", loadedGrandchild)
	}
}

func TestLegacySessionLoadsWithZeroLineageAndLocalMetadata(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	legacyData := []byte(`{"id":"legacy_lineage","start_time":"2024-10-13T00:00:00Z","model":"gpt","messages":[{"role":"user","content":"legacy prompt"}],"analytics":{}}`)
	path := filepath.Join(dir, "session_legacy_lineage.json")
	if err := os.WriteFile(path, legacyData, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manager.LoadSession("legacy_lineage")
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if loaded.ParentSessionID != "" || loaded.BranchName != "" || loaded.Favorite || loaded.Compacted || loaded.Notes != nil || len(loaded.CoordinateReports) != 0 || len(loaded.PendingSources) != 0 || len(loaded.PendingImages) != 0 {
		t.Fatalf("legacy session acquired metadata that was absent from its archive: %+v", loaded)
	}
}

func TestCoordinateReportRoundTripKeepsStageOrder(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	session := &ConversationSession{
		ID: "session_coordinate_report", StartTime: time.Now(),
		Messages: []intelligence.Message{{Role: "user", Content: "Question"}, {Role: "assistant", Content: "Final answer"}},
		CoordinateReports: []CoordinateReport{{
			ID: "coordinate_123456789", Strategy: "independent", Question: "Question",
			StartedAt: time.Now().Add(-time.Minute), FinishedAt: time.Now(), Complete: true, FinalAnswer: "Final answer",
			Stages: []CoordinateReportStage{
				{ID: "agent-1", Label: "Agent 1", Model: "model-a", Role: "Analyst", Status: "completed", Attempt: 1, Prompt: "first prompt", Response: "first output"},
				{ID: "agent-2", Label: "Agent 2", Model: "model-b", Role: "Verifier", Status: "completed", Attempt: 1, Prompt: "second prompt", Response: "second output"},
			},
		}, {
			ID: "coordinate_223456789", Strategy: "debate", Question: "Question",
			StartedAt: time.Now().Add(-time.Minute), FinishedAt: time.Now(), Partial: true, FinalAnswer: "Partial answer",
			Stages: []CoordinateReportStage{{ID: "agent-3", Label: "Synthesis", Status: "failed", Error: "temporary failure"}},
		}},
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	loaded, err := manager.LoadSession(session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(loaded.CoordinateReports) != 2 || loaded.CoordinateReports[0].ID != "coordinate_123456789" || !loaded.CoordinateReports[0].Complete || !loaded.CoordinateReports[1].Partial {
		t.Fatalf("loaded coordinate report = %+v", loaded.CoordinateReports)
	}
	stages := loaded.CoordinateReports[0].Stages
	if len(stages) != 2 || stages[0].ID != "agent-1" || stages[1].ID != "agent-2" || stages[0].Response != "first output" {
		t.Fatalf("coordinate stage order/content = %+v", stages)
	}
	entries, err := os.ReadDir(manager.StorageDir)
	if err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".json.gz") {
		t.Fatalf("report should live inside the compressed session archive; entries=%v err=%v", entries, err)
	}
}

func TestLegacySessionLoadsWithoutCoordinateReports(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	path := filepath.Join(dir, "session_legacy_coordinate.json")
	if err := os.WriteFile(path, []byte(`{"id":"legacy_coordinate","start_time":"2026-01-01T00:00:00Z","model":"model","messages":[],"analytics":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := manager.LoadSession("legacy_coordinate")
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(loaded.CoordinateReports) != 0 {
		t.Fatalf("legacy coordinate reports = %+v, want empty", loaded.CoordinateReports)
	}
}

func TestSaveSessionValidatesCoordinateReportIDsAndTextLimits(t *testing.T) {
	tests := []struct {
		name   string
		report CoordinateReport
	}{
		{
			name:   "path traversal id",
			report: CoordinateReport{ID: "../outside", Strategy: "independent", Question: "Q", Complete: true, Stages: []CoordinateReportStage{{ID: "agent-1", Status: "completed"}}},
		},
		{
			name:   "path traversal stage id",
			report: CoordinateReport{ID: "coordinate_9", Strategy: "independent", Question: "Q", Complete: true, Stages: []CoordinateReportStage{{ID: "../outside", Status: "completed"}}},
		},
		{
			name:   "oversized prompt",
			report: CoordinateReport{ID: "coordinate_1", Strategy: "independent", Question: "Q", Complete: true, Stages: []CoordinateReportStage{{ID: "agent-1", Status: "completed", Prompt: strings.Repeat("x", CoordinateReportTextLimit+1)}}},
		},
		{
			name:   "oversized response",
			report: CoordinateReport{ID: "coordinate_2", Strategy: "independent", Question: "Q", Complete: true, Stages: []CoordinateReportStage{{ID: "agent-1", Status: "completed", Response: strings.Repeat("x", CoordinateReportTextLimit+1)}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager := NewHistoryManager(t.TempDir())
			session := &ConversationSession{ID: "session_invalid_report", StartTime: time.Now(), CoordinateReports: []CoordinateReport{test.report}}
			if err := manager.SaveSession(session); err == nil {
				t.Fatal("SaveSession() succeeded for an invalid coordinate report")
			}
		})
	}
}

func TestDeleteSessionRemovesCoordinateReports(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	session := &ConversationSession{
		ID: "session_delete_report", StartTime: time.Now(), Messages: []intelligence.Message{{Role: "user", Content: "question"}},
		CoordinateReports: []CoordinateReport{{ID: "coordinate_2", Strategy: "independent", Question: "question", Complete: true, FinalAnswer: "done", Stages: []CoordinateReportStage{{ID: "synthesis", Status: "completed"}}}},
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteSession(session.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if _, err := manager.LoadSession(session.ID); err == nil {
		t.Fatal("deleted conversation and its inline coordinate report remained loadable")
	}
}

func TestRetentionRemovesConversationWithCoordinateReports(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	session := &ConversationSession{
		ID: "session_expired_report", StartTime: time.Now().Add(-60 * 24 * time.Hour),
		Messages:          []intelligence.Message{{Role: "user", Content: "question"}},
		CoordinateReports: []CoordinateReport{{ID: "coordinate_3", Strategy: "independent", Question: "question", Complete: true, FinalAnswer: "done", Stages: []CoordinateReportStage{{ID: "synthesis", Status: "completed"}}}},
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatal(err)
	}
	if err := manager.SetRetentionDays(30); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.LoadSession(session.ID); err == nil {
		t.Fatal("expired conversation and its inline coordinate report remained loadable")
	}
}

func TestDeleteSessionReportsPartialRemovalFailure(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	session := &ConversationSession{ID: "session_partial_delete", StartTime: time.Now()}
	if err := manager.SaveSession(session); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, "session_session_partial_delete.json")
	if err := os.Mkdir(legacyPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyPath, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteSession(session.ID); err == nil {
		t.Fatal("DeleteSession() claimed success while one archive path remained")
	}
	if _, err := os.Stat(filepath.Join(dir, "session_session_partial_delete.json.gz")); !os.IsNotExist(err) {
		t.Errorf("compressed archive was not removed despite partial failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacyPath, "keep")); err != nil {
		t.Errorf("failed legacy removal unexpectedly altered its contents: %v", err)
	}
}

func TestListSessionSummariesAreNewestFirstWithPreviewOnly(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	older := &ConversationSession{ID: "session_older", StartTime: time.Now().Add(-time.Hour), Model: "model-old", Messages: []intelligence.Message{{Role: "user", Content: "older conversation"}, {Role: "assistant", Content: "answer"}}}
	newer := &ConversationSession{ID: "session_newer", StartTime: time.Now(), Model: "model-new", Messages: []intelligence.Message{{Role: "user", Content: "newer conversation"}, {Role: "assistant", Content: "secret transcript"}}}
	if err := manager.SaveSession(older); err != nil {
		t.Fatal(err)
	}
	if err := manager.SaveSession(newer); err != nil {
		t.Fatal(err)
	}
	summaries, err := manager.ListSessionSummaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].ID != newer.ID || summaries[1].ID != older.ID {
		t.Fatalf("summary ordering = %+v", summaries)
	}
	if summaries[0].FirstMessage != "newer conversation" || summaries[0].ResumeCommand != "/load session_newer" {
		t.Fatalf("summary preview/resume command = %+v", summaries[0])
	}
	encoded, err := json.Marshal(summaries)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret transcript") {
		t.Fatalf("summary output contains transcript content: %s", encoded)
	}
}

func TestSessionRoundTripPreservesImageAttachments(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	image := media.ImageAttachment{Name: "logo.png", MIMEType: "image/png", Data: []byte{1, 2, 3, 4}}
	session := &ConversationSession{
		ID: "session_with_image", StartTime: time.Now(), Model: "gpt-5.6-luna",
		Messages: []intelligence.Message{{Role: "user", Content: "Describe this", Images: []media.ImageAttachment{image}}},
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}

	loaded, err := manager.LoadSession(session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(loaded.Messages) != 1 || len(loaded.Messages[0].Images) != 1 {
		t.Fatalf("loaded messages = %+v, want one image-bearing message", loaded.Messages)
	}
	got := loaded.Messages[0].Images[0]
	if got.Name != image.Name || got.MIMEType != image.MIMEType || string(got.Data) != string(image.Data) {
		t.Fatalf("loaded attachment = %+v, want original metadata and bytes", got)
	}
}

func TestSaveSessionReplacesLegacyUncompressedArchive(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	legacy := ConversationSession{
		ID: "session_duplicate", StartTime: time.Now().Add(-time.Hour),
		Messages: []intelligence.Message{{Role: "user", Content: "old"}},
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(dir, "session_session_duplicate.json")
	if err := os.WriteFile(legacyPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	updated := &ConversationSession{
		ID: "session_duplicate", StartTime: time.Now(),
		Messages: []intelligence.Message{{Role: "user", Content: "new"}},
	}
	if err := manager.SaveSession(updated); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy archive still exists, stat error = %v", err)
	}
	sessions, err := manager.ListSessions()
	if err != nil || len(sessions) != 1 {
		t.Fatalf("ListSessions() = %d sessions, error = %v; want one", len(sessions), err)
	}
	loaded, err := manager.LoadSession(updated.ID)
	if err != nil || loaded.Messages[0].Content != "new" {
		t.Fatalf("LoadSession() = (%+v, %v), want updated session", loaded, err)
	}
}

func TestSessionRoundTripPreservesSourceReferences(t *testing.T) {
	manager := NewHistoryManager(t.TempDir())
	source := chatcontext.SourceRef{
		ID: "source-1", Kind: chatcontext.SourceKindURL, Name: "Release notes",
		URI: "https://example.test/releases", SizeBytes: 72, State: chatcontext.SourceStateActive,
	}
	session := &ConversationSession{
		ID: "session_with_sources", StartTime: time.Now(), Model: "gpt-5.6-luna",
		Messages: []intelligence.Message{{Role: "user", Content: "Summarize", Sources: []chatcontext.SourceRef{source}}},
	}
	if err := manager.SaveSession(session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	loaded, err := manager.LoadSession(session.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(loaded.Messages) != 1 || !reflect.DeepEqual(loaded.Messages[0].Sources, []chatcontext.SourceRef{source}) {
		t.Fatalf("loaded sources = %+v, want %+v", loaded.Messages, source)
	}
}

func TestLegacySessionLoadsWithoutInventedSources(t *testing.T) {
	dir := t.TempDir()
	manager := NewHistoryManager(dir)
	legacy := ConversationSession{
		ID: "legacy_without_sources", StartTime: time.Now(), Model: "gpt-5.6-luna",
		Messages: []intelligence.Message{{Role: "user", Content: "Older archive"}},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session_"+legacy.ID+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := manager.LoadSession(legacy.ID)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(loaded.Messages) != 1 || len(loaded.Messages[0].Sources) != 0 {
		t.Fatalf("legacy message sources = %+v, want no invented references", loaded.Messages)
	}
}
