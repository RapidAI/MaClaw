package agent

import (
	"testing"
	"time"
)

// forceSessionLastAccess backdates a session's lastAccess so EvictExpired
// treats it as stale.
func forceSessionLastAccess(cm *ConversationMemory, userID string, at time.Time) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	if s := sh.sessions[userID]; s != nil {
		s.lastAccess = at
	}
	sh.mu.Unlock()
}

func TestEvictExpiredRemovesStaleIMSession(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "weixin:user-1"
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "hello"}})
	forceSessionLastAccess(cm, userID, time.Now().Add(-MemoryTTL-time.Minute))

	cm.EvictExpired()

	if entries := cm.Load(userID); len(entries) != 0 {
		t.Fatalf("expected stale IM session to be evicted, got %d entries", len(entries))
	}
}

func TestEvictExpiredKeepsDesktopSessions(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	users := []string{
		"desktop-user",
		"desktop-user:D:/work/project",
		"desktop-user:expert:code-reviewer",
	}
	for _, userID := range users {
		cm.Save(userID, []ConversationEntry{{Role: "user", Content: "task for " + userID}})
		forceSessionLastAccess(cm, userID, time.Now().Add(-26*time.Hour))
	}

	cm.EvictExpired()

	for _, userID := range users {
		if entries := cm.Load(userID); len(entries) != 1 {
			t.Fatalf("desktop session %q was evicted (entries=%d); desktop sessions must survive restarts", userID, len(entries))
		}
	}
}

func TestEvictExpiredKeepsSessionWithUnfinishedSlot(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "weixin:user-with-slot"
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "long task"}})
	cm.UpsertUnfinishedSlot(userID, &UnfinishedTaskSlot{
		SlotID:  "slot-1",
		UserID:  userID,
		Status:  UnfinishedTaskSlotStatusInterrupted,
		LastTask: "long task",
	})
	forceSessionLastAccess(cm, userID, time.Now().Add(-MemoryTTL-time.Minute))

	cm.EvictExpired()

	if slot := cm.GetUnfinishedSlot(userID); slot == nil {
		t.Fatal("session with pending unfinished slot was evicted; resume entry point lost")
	}
	if entries := cm.Load(userID); len(entries) != 1 {
		t.Fatalf("expected slotted session entries to survive, got %d", len(entries))
	}
}

func TestEvictExpiredKeepsFreshSession(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()

	userID := "weixin:fresh"
	cm.Save(userID, []ConversationEntry{{Role: "user", Content: "hi"}})

	cm.EvictExpired()

	if entries := cm.Load(userID); len(entries) != 1 {
		t.Fatalf("fresh session was evicted, got %d entries", len(entries))
	}
}
