package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// projectCodingRuntimeProjection keeps presentation state intentionally weaker
// than the execution ledger. It writes only opaque runtime task references and
// a bounded outcome summary; recovery must go back through codingruntime.
func (h *IMMessageHandler) projectCodingRuntimeProjection(userID string, state *v2.WorkflowState, results []v2.TaskRunResult, report string, cancelled bool) {
	if h == nil || h.memory == nil || strings.TrimSpace(userID) == "" {
		return
	}
	runtimeTaskID := latestCodingRuntimeTaskID(results)
	if runtimeTaskID == "" {
		return
	}
	status, source := codingRuntimeProjectionSlotStatus(results, cancelled)
	// A completed execution has already been projected into Workflow output and
	// needs no unfinished-session marker. Reserving ConversationMemory for
	// actual recovery candidates prevents this projection from replacing an
	// unrelated active chat task.
	if status == agent.UnfinishedTaskSlotStatusCompleted {
		return
	}
	slot := h.memory.GetUnfinishedSlot(userID)
	if slot != nil && slot.RuntimeTaskID != "" && slot.RuntimeTaskID != runtimeTaskID {
		// ConversationMemory has one unfinished-task slot per user. Do not let a
		// late workflow callback overwrite a different recovery candidate.
		return
	}
	if slot == nil {
		slot = &agent.UnfinishedTaskSlot{
			SlotID:      "coding-runtime:" + runtimeTaskID,
			UserID:      userID,
			ProjectPath: codingRuntimeProjectionProjectPath(state),
			Tool:        "coding_runtime",
		}
	}
	slot.RuntimeTaskID = runtimeTaskID
	slot.Status = status
	slot.Source = source
	slot.Summary = codingRuntimeProjectionSummary(report, cancelled)
	slot.UpdatedAt = time.Now().UTC()
	if slot.CreatedAt.IsZero() {
		slot.CreatedAt = slot.UpdatedAt
	}
	h.memory.UpsertUnfinishedSlot(userID, slot)
}

func latestCodingRuntimeTaskID(results []v2.TaskRunResult) string {
	for i := len(results) - 1; i >= 0; i-- {
		if id := strings.TrimSpace(results[i].RuntimeTaskID); id != "" {
			return id
		}
	}
	return ""
}

func codingRuntimeProjectionSlotStatus(results []v2.TaskRunResult, cancelled bool) (agent.UnfinishedTaskSlotStatus, agent.UnfinishedTaskSlotSource) {
	if cancelled {
		return agent.UnfinishedTaskSlotStatusInterrupted, agent.UnfinishedTaskSlotSourceInFlightRecovery
	}
	for _, result := range results {
		if result.Status == v2.TaskFailed || result.Status == v2.TaskSkipped {
			return agent.UnfinishedTaskSlotStatusInterrupted, agent.UnfinishedTaskSlotSourceInFlightRecovery
		}
	}
	return agent.UnfinishedTaskSlotStatusCompleted, agent.UnfinishedTaskSlotSourceSessionExit
}

func codingRuntimeProjectionProjectPath(state *v2.WorkflowState) string {
	if state == nil {
		return ""
	}
	return strings.TrimSpace(state.ProjectPath)
}

func codingRuntimeProjectionSummary(report string, cancelled bool) string {
	prefix := "coding runtime task projection"
	if cancelled {
		prefix = "coding runtime interrupted; read-only recovery probe required"
	}
	report = strings.TrimSpace(report)
	if report == "" {
		return prefix
	}
	const maxRunes = 360
	runes := []rune(report)
	if len(runes) > maxRunes {
		report = string(runes[:maxRunes]) + "…"
	}
	return fmt.Sprintf("%s: %s", prefix, report)
}
