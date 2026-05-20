package memory

import (
	"strings"
	"testing"
	"time"
)

func TestLatestSessionCheckpointForHostReturnsNewestAndTouches(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if _, err := store.UpsertSessionCheckpoint(SessionCheckpointUpsertOptions{
		Title:            "old checkpoint",
		Content:          "checkpoint old",
		Tags:             []string{"session_checkpoint", "D:/repo", "codex", "session-old", "user-1"},
		IdentityTagCount: 4,
		OwnerID:          "user-1",
	}); err != nil {
		t.Fatalf("old UpsertSessionCheckpoint: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	if _, err := store.UpsertSessionCheckpoint(SessionCheckpointUpsertOptions{
		Title:            "new checkpoint",
		Content:          "checkpoint new",
		Tags:             []string{"session_checkpoint", "D:/repo", "codex", "session-new", "user-1"},
		IdentityTagCount: 4,
		OwnerID:          "user-1",
	}); err != nil {
		t.Fatalf("new UpsertSessionCheckpoint: %v", err)
	}

	entries := store.List(CategorySessionCheckpoint, "checkpoint old")
	if len(entries) != 1 {
		t.Fatalf("expected old checkpoint, got %+v", entries)
	}
	oldAccess := entries[0].AccessCount

	got := store.LatestSessionCheckpointForHost("D:/repo")
	if !strings.Contains(got, "checkpoint new") {
		t.Fatalf("expected newest checkpoint, got %q", got)
	}
	newEntries := store.List(CategorySessionCheckpoint, "checkpoint new")
	if len(newEntries) != 1 || newEntries[0].AccessCount == 0 {
		t.Fatalf("expected selected checkpoint to be touched: %+v", newEntries)
	}
	oldEntries := store.List(CategorySessionCheckpoint, "checkpoint old")
	if len(oldEntries) != 1 || oldEntries[0].AccessCount != oldAccess {
		t.Fatalf("old checkpoint should not be touched: oldAccess=%d entries=%+v", oldAccess, oldEntries)
	}
}

func TestLatestSessionCheckpointForHostEmpty(t *testing.T) {
	var nilStore *Store
	if got := nilStore.LatestSessionCheckpointForHost("D:/repo"); got != "" {
		t.Fatalf("nil store returned %q", got)
	}
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()
	if got := store.LatestSessionCheckpointForHost(""); got != "" {
		t.Fatalf("empty project returned %q", got)
	}
	if got := store.LatestSessionCheckpointForHost("missing"); got != "" {
		t.Fatalf("missing project returned %q", got)
	}
}
