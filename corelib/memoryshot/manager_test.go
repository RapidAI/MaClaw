package memoryshot

import (
	"path/filepath"
	"testing"
)

func TestManagerSaveLoadChatHistoryRoundTrip(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memoryshot"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	mgr := NewManager(store)
	history := []ChatMessage{
		{Role: "user", Content: "hello", Timestamp: 1},
		{Role: "assistant", Content: "world", Timestamp: 2},
	}
	mgr.UpdateChatHistory(history)
	if err := mgr.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := NewManager(store)
	loaded, err := reloaded.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded {
		t.Fatal("expected snapshot to load")
	}

	got := reloaded.GetChatHistory()
	if len(got) != len(history) {
		t.Fatalf("chat history length = %d, want %d", len(got), len(history))
	}
	for i := range history {
		if got[i] != history[i] {
			t.Fatalf("chat history[%d] = %+v, want %+v", i, got[i], history[i])
		}
	}
}

func TestManagerClearRemovesPersistedHistory(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "memoryshot"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	mgr := NewManager(store)
	mgr.UpdateChatHistory([]ChatMessage{{Role: "user", Content: "persist me", Timestamp: 1}})
	if err := mgr.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := mgr.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	reloaded := NewManager(store)
	loaded, err := reloaded.Load()
	if err != nil {
		t.Fatalf("Load after Clear: %v", err)
	}
	if loaded {
		t.Fatal("expected no snapshot after Clear")
	}
	if got := reloaded.GetChatHistory(); len(got) != 0 {
		t.Fatalf("chat history length after Clear = %d, want 0", len(got))
	}
}
