package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func applyImplicitInFlightRecoveryDecision(msg IMUserMessage, trimmed string, slot *agent.UnfinishedTaskSlot, decision explicitTaskSlotDecision) explicitTaskSlotDecision {
	if slot == nil || !isInFlightRecoverySlot(slot) || msg.IsBackground {
		return decision
	}
	if decision.ResumeSlotID != "" || decision.StartNewTask || decision.DismissSlotID != "" || isSlotActionCommand(trimmed) {
		return decision
	}
	if strings.TrimSpace(trimmed) == "" {
		return decision
	}
	if shouldResumeIncompleteTask(trimmed) {
		decision.ResumeSlotID = slot.SlotID
		return decision
	}
	decision.StartNewTask = true
	decision.DismissSlotID = slot.SlotID
	return decision
}

func shouldRecoverInFlightMarker(msg IMUserMessage, unfinishedSlot *agent.UnfinishedTaskSlot, currentLoopCtx *LoopContext) bool {
	return unfinishedSlot == nil && !msg.IsBackground && (currentLoopCtx == nil || currentLoopCtx.IsCancelled())
}

func isInFlightRecoverySlot(slot *agent.UnfinishedTaskSlot) bool {
	if slot == nil {
		return false
	}
	return slot.Source.IsInFlightRecovery()
}

func (h *IMMessageHandler) clearInFlightTaskMarker(userID string) {
	if h == nil || h.memory == nil {
		return
	}
	h.memory.ClearInFlightTask(userID)
	_ = h.memory.FlushNow()
}
