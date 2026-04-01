package main

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/memoryshot"
)

func TestSyncChatHistoryToMemoryShotSavesCanonicalHistory(t *testing.T) {
	store, err := memoryshot.NewStore(filepath.Join(t.TempDir(), "memoryshot"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mgr := memoryshot.NewManager(store)

	app := &TUIApp{
		memShotMgr: mgr,
		chatHistory: []memoryshot.ChatMessage{
			{Role: "user", Content: "hello", Timestamp: 1},
			{Role: "assistant", Content: "world", Timestamp: 2},
		},
	}

	app.syncChatHistoryToMemoryShot(true)

	reloaded := memoryshot.NewManager(store)
	loaded, err := reloaded.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded {
		t.Fatal("expected persisted snapshot")
	}

	got := reloaded.GetChatHistory()
	if len(got) != len(app.chatHistory) {
		t.Fatalf("chat history length = %d, want %d", len(got), len(app.chatHistory))
	}
	for i := range app.chatHistory {
		if got[i] != app.chatHistory[i] {
			t.Fatalf("chat history[%d] = %+v, want %+v", i, got[i], app.chatHistory[i])
		}
	}
}

func TestShutdownPersistsChatHistory(t *testing.T) {
	store, err := memoryshot.NewStore(filepath.Join(t.TempDir(), "memoryshot"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mgr := memoryshot.NewManager(store)

	app := &TUIApp{
		memShotMgr: mgr,
		chatHistory: []memoryshot.ChatMessage{
			{Role: "user", Content: "persist on quit", Timestamp: 1},
		},
	}

	app.shutdown()

	reloaded := memoryshot.NewManager(store)
	loaded, err := reloaded.Load()
	if err != nil {
		t.Fatalf("Load after shutdown: %v", err)
	}
	if !loaded {
		t.Fatal("expected persisted snapshot after shutdown")
	}

	got := reloaded.GetChatHistory()
	if len(got) != 1 || got[0].Content != "persist on quit" {
		t.Fatalf("unexpected restored history: %+v", got)
	}
}
