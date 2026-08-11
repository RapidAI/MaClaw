package agent

import "testing"

func TestUnfinishedTaskSlotPersistsOpaqueRuntimeTaskID(t *testing.T) {
	cm := NewConversationMemory()
	defer cm.Stop()
	cm.UpsertUnfinishedSlot("user", &UnfinishedTaskSlot{SlotID: "slot", RuntimeTaskID: "runtime-task-opaque-id"})
	slot := cm.GetUnfinishedSlot("user")
	if slot == nil || slot.RuntimeTaskID != "runtime-task-opaque-id" {
		t.Fatalf("slot=%+v", slot)
	}
}
