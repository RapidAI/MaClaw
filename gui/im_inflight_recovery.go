package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func applyImplicitInFlightRecoveryDecision(msg IMUserMessage, trimmed string, slot *agent.UnfinishedTaskSlot, decision explicitTaskSlotDecision) explicitTaskSlotDecision {
	// Recovery is a semantic task-association decision. Do not decide it from
	// a phrase such as "continue", or decide a fresh task from the absence of
	// that phrase: either rule is vulnerable to wording changes and can bind
	// interrupted side-effect evidence to the wrong request. Explicit UI
	// commands remain authoritative; all ordinary messages reach the trusted
	// task-context classifier in applyUnifiedTaskContextDecision.
	_ = msg
	_ = trimmed
	_ = slot
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

// applyAppExitAutoResumeDecision binds an app-exit unfinished slot without
// asking. A graceful-exit snapshot (Source=app_exit) only exists because the
// app was closed mid-task; when the user opens that restored (or task-list)
// tab and sends a message, the open itself is the resume intent — showing a
// "continue previous task?" banner would be redundant, and treating the
// message as a new task would wipe the saved context the user expects to
// continue from. Explicit slot actions and background turns are untouched; a
// genuinely new topic is still handled downstream by the unified task-context
// classifier.
func applyAppExitAutoResumeDecision(msg IMUserMessage, trimmed string, slot *agent.UnfinishedTaskSlot, decision explicitTaskSlotDecision) explicitTaskSlotDecision {
	if slot == nil || !slot.Source.IsAppExit() || !unfinishedSlotNeedsDecision(slot) || msg.IsBackground {
		return decision
	}
	if decision.ResumeSlotID != "" || decision.StartNewTask || decision.DismissSlotID != "" || isSlotActionCommand(trimmed) {
		return decision
	}
	if strings.TrimSpace(trimmed) == "" {
		return decision
	}
	decision.ResumeSlotID = slot.SlotID
	return decision
}

func (h *IMMessageHandler) clearInFlightTaskMarker(userID string) {
	if h == nil || h.memory == nil {
		return
	}
	h.memory.ClearInFlightTask(userID)
	_ = h.memory.FlushNow()
}
