package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestProjectCodingRuntimeProjectionStoresOnlyOpaqueRuntimeTaskID(t *testing.T) {
	memory := agent.NewConversationMemory()
	h := &IMMessageHandler{memory: memory}
	h.projectCodingRuntimeProjection("user", &v2.WorkflowState{ProjectPath: "C:/repo"}, []v2.TaskRunResult{{RuntimeTaskID: "runtime-opaque", Status: v2.TaskFailed}}, "failed after a write", false)
	slot := memory.GetUnfinishedSlot("user")
	if slot == nil || slot.RuntimeTaskID != "runtime-opaque" {
		t.Fatalf("runtime task projection = %#v", slot)
	}
	if slot.Status != agent.UnfinishedTaskSlotStatusInterrupted || !slot.Source.IsInFlightRecovery() {
		t.Fatalf("unexpected recovery slot: %#v", slot)
	}
	if slot.ProjectPath != "C:/repo" || slot.Tool != "coding_runtime" {
		t.Fatalf("unexpected projection metadata: %#v", slot)
	}
}

func TestProjectCodingRuntimeProjectionDoesNothingWithoutLedgerReference(t *testing.T) {
	memory := agent.NewConversationMemory()
	h := &IMMessageHandler{memory: memory}
	h.projectCodingRuntimeProjection("user", nil, []v2.TaskRunResult{{Status: v2.TaskFailed}}, "failed", false)
	if slot := memory.GetUnfinishedSlot("user"); slot != nil {
		t.Fatalf("slot without runtime ID = %#v", slot)
	}
}

func TestProjectCodingRuntimeProjectionDoesNotReplaceOtherRecoveryCandidate(t *testing.T) {
	memory := agent.NewConversationMemory()
	memory.UpsertUnfinishedSlot("user", &agent.UnfinishedTaskSlot{SlotID: "other", RuntimeTaskID: "other-runtime", Status: agent.UnfinishedTaskSlotStatusInterrupted})
	h := &IMMessageHandler{memory: memory}
	h.projectCodingRuntimeProjection("user", nil, []v2.TaskRunResult{{RuntimeTaskID: "new-runtime", Status: v2.TaskFailed}}, "failed", false)
	slot := memory.GetUnfinishedSlot("user")
	if slot == nil || slot.RuntimeTaskID != "other-runtime" {
		t.Fatalf("late projection replaced slot: %#v", slot)
	}
}
