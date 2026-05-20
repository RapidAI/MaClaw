package memory

import (
	"strings"
	"testing"
)

func TestEntryByMemoryTraceID(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if err := store.SaveManualMemory("trace content", CategoryUserFact, nil); err != nil {
		t.Fatalf("SaveManualMemory: %v", err)
	}
	entries := store.List(CategoryUserFact, "trace content")
	if len(entries) != 1 {
		t.Fatalf("expected saved entry, got %+v", entries)
	}

	entry, err := store.EntryByMemoryTraceID("memory:" + entries[0].ID)
	if err != nil {
		t.Fatalf("EntryByMemoryTraceID: %v", err)
	}
	if entry.ID != entries[0].ID || entry.Content != "trace content" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestEntryByMemoryTraceIDRejectsInvalidTraceIDs(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	for _, traceID := range []string{"", "trace:abc", "memory:"} {
		if _, err := store.EntryByMemoryTraceID(traceID); err == nil {
			t.Fatalf("expected error for trace id %q", traceID)
		}
	}
	_, err = store.EntryByMemoryTraceID("memory:missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got %v", err)
	}
}
func TestEntryByIDForHost(t *testing.T) {
	store, err := NewStoreWithMode(t.TempDir(), StoreModeJSON)
	if err != nil {
		t.Fatalf("NewStoreWithMode: %v", err)
	}
	defer store.Stop()

	if err := store.SaveManualMemory("exact id content", CategoryUserFact, nil); err != nil {
		t.Fatalf("SaveManualMemory: %v", err)
	}
	entries := store.List(CategoryUserFact, "exact id content")
	if len(entries) != 1 {
		t.Fatalf("expected saved entry, got %+v", entries)
	}
	entry, ok := store.EntryByIDForHost(entries[0].ID)
	if !ok || entry.ID != entries[0].ID {
		t.Fatalf("EntryByIDForHost returned %+v ok=%v", entry, ok)
	}
	if _, ok := store.EntryByIDForHost("missing"); ok {
		t.Fatal("missing entry should not resolve")
	}
}
