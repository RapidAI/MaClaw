package main

import (
	"path/filepath"
	"testing"
)

func TestPersistentConversationMemorySaveLoadRoundTrip(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := newPersistentConversationMemory(storePath)
	defer cm.stop()

	history := []conversationEntry{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world", ReasoningContent: "thinking"},
	}
	cm.save("desktop-user", history)
	cm.stop()

	reloaded := newPersistentConversationMemory(storePath)
	defer reloaded.stop()
	got := reloaded.load("desktop-user")
	if len(got) != len(history) {
		t.Fatalf("history length = %d, want %d", len(got), len(history))
	}
	for i := range history {
		if got[i].Role != history[i].Role {
			t.Fatalf("history[%d].Role = %q, want %q", i, got[i].Role, history[i].Role)
		}
		if got[i].ReasoningContent != history[i].ReasoningContent {
			t.Fatalf("history[%d].ReasoningContent = %q, want %q", i, got[i].ReasoningContent, history[i].ReasoningContent)
		}
		if got[i].Content != history[i].Content {
			t.Fatalf("history[%d].Content = %#v, want %#v", i, got[i].Content, history[i].Content)
		}
	}
}

func TestPersistentConversationMemoryClearRemovesPersistedSession(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := newPersistentConversationMemory(storePath)
	defer cm.stop()

	cm.save("desktop-user", []conversationEntry{{Role: "user", Content: "persist me"}})
	cm.clear("desktop-user")
	cm.stop()

	reloaded := newPersistentConversationMemory(storePath)
	defer reloaded.stop()
	if got := reloaded.load("desktop-user"); len(got) != 0 {
		t.Fatalf("history length after clear = %d, want 0", len(got))
	}
}
