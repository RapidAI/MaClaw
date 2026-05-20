package memory

import "testing"

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
