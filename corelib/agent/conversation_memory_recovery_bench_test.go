package agent

import (
	"fmt"
	"testing"
)

func BenchmarkConversationMemoryRecoverySlots(b *testing.B) {
	cm := NewConversationMemory()
	defer cm.Stop()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 200; j++ {
			cm.UpsertUnfinishedSlot("bench-user", &UnfinishedTaskSlot{
				SlotID:   fmt.Sprintf("slot-%d", j),
				Status:   UnfinishedTaskSlotStatusInterrupted,
				LastTask: fmt.Sprintf("task %d", j),
			})
		}
		for j := 0; j < 200; j++ {
			cm.DismissUnfinishedSlot("bench-user", cm.GetUnfinishedSlot("bench-user").SlotID)
		}
	}
}

func BenchmarkConversationMemoryRecoverySlotReload(b *testing.B) {
	path := b.TempDir() + "/conversation.json"
	seed := NewPersistentConversationMemory(path)
	for j := 0; j < 200; j++ {
		seed.UpsertUnfinishedSlot("bench-user", &UnfinishedTaskSlot{
			SlotID:   fmt.Sprintf("slot-%d", j),
			Status:   UnfinishedTaskSlotStatusInterrupted,
			LastTask: fmt.Sprintf("task %d", j),
		})
	}
	if err := seed.FlushNow(); err != nil {
		b.Fatal(err)
	}
	seed.Stop()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cm := NewPersistentConversationMemory(path)
		if len(cm.UnfinishedSlots()) != 200 {
			b.Fatalf("unexpected recovery slots: %d", len(cm.UnfinishedSlots()))
		}
		cm.Stop()
	}
}
