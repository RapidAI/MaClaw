package main

import (
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
