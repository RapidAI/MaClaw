package memory

import (
	"strings"
	"testing"
	"time"
)

func TestManualMemoryFacadesForHost(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if err := store.SaveManualMemoryForHost("manual host memory", CategoryUserFact, []string{"host"}); err != nil {
		t.Fatalf("SaveManualMemoryForHost: %v", err)
	}
	entries := store.ListEntriesForHost(CategoryUserFact, "manual host")
	if len(entries) != 1 {
		t.Fatalf("ListEntriesForHost after save = %+v", entries)
	}
	entry := entries[0]
	if err := store.UpdateManualMemoryForHost(entry.ID, "manual host memory updated", CategoryInstruction, []string{"host", "updated"}); err != nil {
		t.Fatalf("UpdateManualMemoryForHost: %v", err)
	}
	updated, ok := store.EntryByIDForHost(entry.ID)
	if !ok || updated.Category != CategoryInstruction || updated.Content != "manual host memory updated" {
		t.Fatalf("EntryByIDForHost after update = %+v, ok=%v", updated, ok)
	}
	if err := store.PinEntryForHost(entry.ID); err != nil {
		t.Fatalf("PinEntryForHost: %v", err)
	}
	pinned, _ := store.EntryByIDForHost(entry.ID)
	if !pinned.Pinned {
		t.Fatalf("PinEntryForHost did not pin entry: %+v", pinned)
	}
	if err := store.UnpinEntryForHost(entry.ID); err != nil {
		t.Fatalf("UnpinEntryForHost: %v", err)
	}
	unpinned, _ := store.EntryByIDForHost(entry.ID)
	if unpinned.Pinned {
		t.Fatalf("UnpinEntryForHost did not unpin entry: %+v", unpinned)
	}

	if nilStore := (*Store)(nil); nilStore.SaveManualMemoryForHost("", "", nil) != nil {
		t.Fatalf("nil SaveManualMemoryForHost should be nil error")
	}
}

func TestManualRewriteClearsExpiry(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()
	expired := time.Now().Add(-time.Hour)
	if err := store.Save(Entry{Content: "old fact", Category: CategoryUserFact, InvalidAt: &expired, Stale: true, Embedding: []float32{1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	entries := store.List("", "")
	if len(entries) != 1 {
		t.Fatalf("saved entries = %+v", entries)
	}
	id := entries[0].ID
	if err := store.UpdateManualMemory(id, "old fact", CategoryUserFact, nil); err != nil {
		t.Fatal(err)
	}
	kept, ok := store.EntryByIDForHost(id)
	if !ok || kept.InvalidAt == nil || len(kept.Embedding) == 0 {
		t.Fatalf("unchanged rewrite dropped expiry or embedding: %+v", kept)
	}
	if err := store.UpdateManualMemory(id, "new fact", CategoryUserFact, nil); err != nil {
		t.Fatal(err)
	}
	rewritten, ok := store.EntryByIDForHost(id)
	if !ok || rewritten.InvalidAt != nil || rewritten.Stale || len(rewritten.Embedding) != 0 {
		t.Fatalf("rewrite kept expiry, stale, or the old vector: %+v", rewritten)
	}
	if summary := store.UserFactSummary(200); !strings.Contains(summary, "new fact") {
		t.Fatalf("summary = %q", summary)
	}
}
