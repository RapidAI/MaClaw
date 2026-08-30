package main

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestDeleteProjectSessionsRemovesIndexedAndOrphanedSessions(t *testing.T) {
	persist := NewProjectTabSessionPersistForBaseDir(t.TempDir())
	projectPath := filepath.Join(t.TempDir(), "deleted-task")
	otherPath := filepath.Join(t.TempDir(), "other-task")
	if err := persist.SaveIndex(&TabIndex{Tabs: []TabIndexEntry{
		{ID: "deleted-indexed", Type: "project", ProjectPath: projectPath, LastActiveAt: time.Now().Unix()},
		{ID: "keep", Type: "project", ProjectPath: otherPath, LastActiveAt: time.Now().Unix()},
	}}); err != nil {
		t.Fatal(err)
	}
	for _, session := range []*TabSessionData{
		{TabID: "deleted-indexed", ProjectPath: projectPath},
		{TabID: "deleted-orphan", ProjectPath: projectPath},
		{TabID: "keep", ProjectPath: otherPath},
	} {
		if err := persist.SaveSession(session); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := persist.DeleteProjectSessions(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Fatalf("removed sessions = %d, want 2", removed)
	}
	if index, err := persist.LoadIndex(); err != nil {
		t.Fatal(err)
	} else if len(index.Tabs) != 1 || index.Tabs[0].ID != "keep" {
		t.Fatalf("index after deletion = %+v", index.Tabs)
	}
	for _, id := range []string{"deleted-indexed", "deleted-orphan"} {
		if session, err := persist.LoadSession(id); err != nil {
			t.Fatal(err)
		} else if session != nil {
			t.Fatalf("deleted session %q remains: %+v", id, session)
		}
	}
	if session, err := persist.LoadSession("keep"); err != nil || session == nil {
		t.Fatalf("unrelated session was deleted: session=%+v err=%v", session, err)
	}
}

func TestLoadLatestSessionForProjectUsesNewestMatchingSessionWithoutIndex(t *testing.T) {
	persist := NewProjectTabSessionPersistForBaseDir(t.TempDir())
	projectPath := filepath.Join(t.TempDir(), "history-task")
	otherPath := filepath.Join(t.TempDir(), "other-task")
	olderAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	newerAt := time.Now().UTC().Format(time.RFC3339)
	for _, session := range []*TabSessionData{
		{TabID: "older", ProjectPath: projectPath, LastActiveAt: olderAt, Conversation: []interface{}{map[string]interface{}{"role": "user", "content": "older history"}}},
		{TabID: "other", ProjectPath: otherPath, LastActiveAt: newerAt, Conversation: []interface{}{map[string]interface{}{"role": "user", "content": "other task"}}},
		{TabID: "newer", ProjectPath: projectPath, LastActiveAt: newerAt, Conversation: []interface{}{map[string]interface{}{"role": "user", "content": "newest history"}}},
	} {
		if err := persist.SaveSession(session); err != nil {
			t.Fatal(err)
		}
	}

	// No index is written: this reproduces an interrupted index update or a
	// closed tab reopened from the task history list.
	latest, err := persist.LoadLatestSessionForProject(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.TabID != "newer" {
		t.Fatalf("latest session = %+v, want newer project session", latest)
	}
}

func TestLoadLatestSessionForProjectPrefersSavedTimestampOverNewerLegacyFile(t *testing.T) {
	persist := NewProjectTabSessionPersistForBaseDir(t.TempDir())
	projectPath := filepath.Join(t.TempDir(), "history-task")
	olderAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	newerAt := time.Now().UTC().Format(time.RFC3339)
	if err := persist.SaveSession(&TabSessionData{TabID: "current", ProjectPath: projectPath, LastActiveAt: newerAt}); err != nil {
		t.Fatal(err)
	}
	if err := persist.SaveSession(&TabSessionData{TabID: "legacy", ProjectPath: projectPath, LastActiveAt: olderAt}); err != nil {
		t.Fatal(err)
	}

	// A legacy session without LastActiveAt falls back to its file timestamp,
	// which is naturally newer because it was written last. It must not replace
	// a session with an authoritative saved activity timestamp.
	legacy := &TabSessionData{TabID: "legacy", ProjectPath: projectPath}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath, err := persist.sessionFilePath("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(legacyPath, data); err != nil {
		t.Fatal(err)
	}

	latest, err := persist.LoadLatestSessionForProject(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.TabID != "current" {
		t.Fatalf("latest session = %+v, want current timestamped session", latest)
	}
}

func TestClearSessionConversationPreservesTabMetadata(t *testing.T) {
	persist := NewProjectTabSessionPersistForBaseDir(t.TempDir())
	projectPath := filepath.Join(t.TempDir(), "history-task")
	if err := persist.SaveSession(&TabSessionData{
		TabID:       "clear-one",
		ProjectPath: projectPath,
		WorkingDir:  filepath.Join(projectPath, "workspace"),
		InputText:   "draft",
		Conversation: []interface{}{
			map[string]interface{}{"role": "user", "content": "clear me"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := persist.ClearSessionConversation("clear-one"); err != nil {
		t.Fatal(err)
	}
	session, err := persist.LoadSession("clear-one")
	if err != nil {
		t.Fatal(err)
	}
	if session == nil || len(session.Conversation) != 0 || session.ProjectPath != projectPath || session.WorkingDir != filepath.Join(projectPath, "workspace") || session.InputText != "draft" {
		t.Fatalf("session after clear = %+v, want metadata with empty conversation", session)
	}
}
