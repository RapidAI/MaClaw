package agent

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestRecoverySlotsStressRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation.json")
	const count = 500
	memory := NewPersistentConversationMemory(path)
	for i := 0; i < count; i++ {
		memory.UpsertUnfinishedSlot("stress-user", &UnfinishedTaskSlot{
			SlotID:   fmt.Sprintf("stress-slot-%03d", i),
			Status:   UnfinishedTaskSlotStatusInterrupted,
			LastTask: fmt.Sprintf("stress task %03d", i),
		})
	}
	if err := memory.FlushNow(); err != nil {
		t.Fatal(err)
	}
	memory.Stop()

	reloaded := NewPersistentConversationMemory(path)
	defer reloaded.Stop()
	if got := len(reloaded.UnfinishedSlots()); got != count {
		t.Fatalf("reloaded slots = %d, want %d", got, count)
	}
	for i := count - 1; i >= 0; i-- {
		current := reloaded.GetUnfinishedSlot("stress-user")
		if current == nil || current.SlotID != fmt.Sprintf("stress-slot-%03d", i) {
			t.Fatalf("slot %d = %#v", i, current)
		}
		reloaded.DismissUnfinishedSlot("stress-user", current.SlotID)
	}
	if slot := reloaded.GetUnfinishedSlot("stress-user"); slot != nil {
		t.Fatalf("stress slots were not exhausted: %#v", slot)
	}
}
