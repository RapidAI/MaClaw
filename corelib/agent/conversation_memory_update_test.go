package agent

import "testing"

func TestUpdateEntriesPatchesStoredHistory(t *testing.T) {
	cm := NewConversationMemory()
	t.Cleanup(cm.Stop)
	userID := "u-update"
	cm.Save(userID, []ConversationEntry{
		{Role: "user", Content: "make a pdf"},
		{Role: "assistant", Content: "done"},
	})

	changed := cm.UpdateEntries(userID, func(entries []ConversationEntry) ([]ConversationEntry, bool) {
		if len(entries) != 2 {
			t.Fatalf("update saw %d entries, want 2", len(entries))
		}
		entries[1].LocalFilePaths = []string{`C:\out\report.pdf`}
		return entries, true
	})
	if !changed {
		t.Fatal("UpdateEntries reported no change")
	}

	got := cm.Load(userID)
	if len(got) != 2 || len(got[1].LocalFilePaths) != 1 {
		t.Fatalf("patched entries = %+v", got)
	}
}

func TestUpdateEntriesUnchangedResultSavesNothing(t *testing.T) {
	cm := NewConversationMemory()
	t.Cleanup(cm.Stop)
	userID := "u-update-noop"
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "hi"}})

	if cm.UpdateEntries(userID, func(entries []ConversationEntry) ([]ConversationEntry, bool) {
		return entries, false
	}) {
		t.Fatal("unchanged update must not report a change")
	}
	got := cm.Load(userID)
	if len(got) != 1 || got[0].Content != "hi" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestUpdateEntriesMissingSessionFollowsSaveSemantics(t *testing.T) {
	cm := NewConversationMemory()
	t.Cleanup(cm.Stop)
	// A mutator that returns changed entries creates the session, matching
	// Save: UpdateEntries is a generic read-modify-write, not a patch-only API.
	changed := cm.UpdateEntries("u-missing", func(entries []ConversationEntry) ([]ConversationEntry, bool) {
		if len(entries) != 0 {
			t.Fatalf("update saw %d entries on a missing session", len(entries))
		}
		return []ConversationEntry{{Role: "assistant", Content: "injected"}}, true
	})
	if !changed {
		t.Fatal("UpdateEntries reported no change")
	}
	if got := cm.Load("u-missing"); len(got) != 1 || got[0].Content != "injected" {
		t.Fatalf("entries = %+v", got)
	}
}
