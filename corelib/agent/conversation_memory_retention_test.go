package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectSessionTTLKeepsUnfinishedRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	old := time.Now().Add(-31 * 24 * time.Hour)
	snapshot := memorySnapshot{Sessions: map[string]persistedSession{
		"desktop-user:D:/unfinished": {
			LastAccess:     old,
			UnfinishedSlot: &UnfinishedTaskSlot{SlotID: "pending", Status: UnfinishedTaskSlotStatusInterrupted},
		},
		"desktop-user:D:/complete": {LastAccess: old},
	}}
	blob, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	memory := NewPersistentConversationMemory(path)
	defer memory.Stop()
	if slot := memory.GetUnfinishedSlot("desktop-user:D:/unfinished"); slot == nil || slot.SlotID != "pending" {
		t.Fatalf("unfinished session was expired: %#v", slot)
	}
	if entries := memory.Load("desktop-user:D:/complete"); len(entries) != 0 {
		t.Fatalf("completed stale session was not expired: %#v", entries)
	}
}
