package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestPersistentConversationMemoryRapidSavePersistsLatestState(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := newPersistentConversationMemory(storePath)
	defer cm.stop()

	for i := 0; i < 5; i++ {
		cm.save("desktop-user", []conversationEntry{{Role: "user", Content: fmt.Sprintf("msg-%d", i)}})
	}
	cm.stop()

	reloaded := newPersistentConversationMemory(storePath)
	defer reloaded.stop()
	got := reloaded.load("desktop-user")
	if len(got) != 1 {
		t.Fatalf("history length = %d, want 1", len(got))
	}
	if got[0].Content != "msg-4" {
		t.Fatalf("final content = %#v, want %#v", got[0].Content, "msg-4")
	}
}

func TestPersistentConversationMemoryImmediateStopFlushesLatestState(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := newPersistentConversationMemory(storePath)
	defer cm.stop()

	history := []conversationEntry{{Role: "user", Content: "flush me"}}
	cm.save("desktop-user", history)
	cm.stop()

	reloaded := newPersistentConversationMemory(storePath)
	defer reloaded.stop()
	got := reloaded.load("desktop-user")
	if len(got) != 1 || got[0].Content != "flush me" {
		t.Fatalf("unexpected reloaded history: %+v", got)
	}
}

func TestPersistentConversationMemoryImmediateClearThenStopPersistsDeletion(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := newPersistentConversationMemory(storePath)
	defer cm.stop()

	cm.save("desktop-user", []conversationEntry{{Role: "user", Content: "delete me"}})
	cm.clear("desktop-user")
	cm.stop()

	reloaded := newPersistentConversationMemory(storePath)
	defer reloaded.stop()
	if got := reloaded.load("desktop-user"); len(got) != 0 {
		t.Fatalf("history length after immediate clear+stop = %d, want 0", len(got))
	}
}

func TestPersistentConversationMemoryPersistsUnfinishedSlotAndBinding(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := newPersistentConversationMemory(storePath)
	defer cm.stop()

	cm.save("desktop-user", []conversationEntry{{Role: "user", Content: "hello"}})
	cm.upsertUnfinishedSlot("desktop-user", &unfinishedTaskSlot{
		SlotID:       "slot-1",
		UserID:       "desktop-user",
		ProjectPath:  "/project",
		Status:       "pending_resume",
		Summary:      "unfinished summary",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		ResumePrompt: "resume this task",
	})
	if !cm.bindUnfinishedSlot("desktop-user", "slot-1") {
		t.Fatal("expected bindUnfinishedSlot to succeed")
	}
	cm.stop()

	reloaded := newPersistentConversationMemory(storePath)
	defer reloaded.stop()
	slot := reloaded.getUnfinishedSlot("desktop-user")
	if slot == nil {
		t.Fatal("expected unfinished slot after reload")
	}
	if slot.SlotID != "slot-1" {
		t.Fatalf("slot.SlotID = %q, want slot-1", slot.SlotID)
	}
	active := reloaded.activeUnfinishedSlot("desktop-user")
	if active == nil || active.SlotID != "slot-1" {
		t.Fatalf("active slot = %#v, want slot-1", active)
	}
}

func TestPersistentConversationMemoryClearConversationButKeepSlot(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := newPersistentConversationMemory(storePath)
	defer cm.stop()

	cm.save("desktop-user", []conversationEntry{{Role: "user", Content: "old history"}})
	cm.upsertUnfinishedSlot("desktop-user", &unfinishedTaskSlot{
		SlotID:    "slot-2",
		UserID:    "desktop-user",
		Status:    "pending_resume",
		Summary:   "keep me",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	cm.bindUnfinishedSlot("desktop-user", "slot-2")
	cm.clearConversationButKeepSlot("desktop-user")

	if got := cm.load("desktop-user"); len(got) != 0 {
		t.Fatalf("history length = %d, want 0", len(got))
	}
	slot := cm.getUnfinishedSlot("desktop-user")
	if slot == nil || slot.SlotID != "slot-2" {
		t.Fatalf("slot = %#v, want slot-2", slot)
	}
	if active := cm.activeUnfinishedSlot("desktop-user"); active != nil {
		t.Fatalf("active slot should be cleared after clearConversationButKeepSlot, got %#v", active)
	}
}

func TestPersistentConversationMemoryClearConversationAndDismissSlot(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	cm := newPersistentConversationMemory(storePath)
	defer cm.stop()

	cm.save("desktop-user", []conversationEntry{{Role: "user", Content: "old history"}})
	cm.upsertUnfinishedSlot("desktop-user", &unfinishedTaskSlot{
		SlotID:    "slot-3",
		UserID:    "desktop-user",
		Status:    "pending_resume",
		Summary:   "dismiss me",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	cm.bindUnfinishedSlot("desktop-user", "slot-3")
	cm.clearConversationAndDismissSlot("desktop-user")

	if got := cm.load("desktop-user"); len(got) != 0 {
		t.Fatalf("history length = %d, want 0", len(got))
	}
	if slot := cm.getUnfinishedSlot("desktop-user"); slot != nil {
		t.Fatalf("slot = %#v, want nil", slot)
	}
	if active := cm.activeUnfinishedSlot("desktop-user"); active != nil {
		t.Fatalf("active slot = %#v, want nil", active)
	}
}

func TestPersistentConversationMemoryReloadDropsLegacySessionFields(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "conversation.json")
	legacyJSON := `{"sessions":{"desktop-user":{"entries":[],"last_access":"2026-01-01T00:00:00Z","unfinished_slot":{"slot_id":"slot-legacy","user_id":"desktop-user","project_path":"/project","session_id":"sess-legacy","tool":"claude","status":"pending_resume","summary":"legacy summary","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}}}}`
	if err := os.WriteFile(storePath, []byte(legacyJSON), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cm := newPersistentConversationMemory(storePath)
	slot := cm.getUnfinishedSlot("desktop-user")
	if slot == nil {
		t.Fatal("expected unfinished slot from legacy payload")
	}
	if slot.SlotID != "slot-legacy" {
		t.Fatalf("slot.SlotID = %q, want slot-legacy", slot.SlotID)
	}
	cm.stop()

	reloadedBytes, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(reloadedBytes), "session_id") {
		t.Fatalf("persisted payload still contains legacy session_id: %s", string(reloadedBytes))
	}
	if strings.Contains(string(reloadedBytes), "\"tool\"") {
		t.Fatalf("persisted payload still contains legacy tool field: %s", string(reloadedBytes))
	}
}
