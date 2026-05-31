package main

import (
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type imMessagePreflightResult struct {
	HTTPClient                 *http.Client
	EntriesBeforeClear         []agent.ConversationEntry
	UnfinishedSlot             *agent.UnfinishedTaskSlot
	Decision                   explicitTaskSlotDecision
	FreshTask                  bool
	ConfirmedResume            bool
	ConfirmedWorkflowAgentLoop bool
	ClearUIAfterContextSwitch  bool
	Response                   *IMAgentResponse
	Handled                    bool
}

func (h *IMMessageHandler) prepareIMMessagePreflight(msg *IMUserMessage, trimmed *string) imMessagePreflightResult {
	result := imMessagePreflightResult{HTTPClient: h.client}
	if msg != nil && msg.IsBackground {
		result.HTTPClient = h.taskClient
	}
	if msg == nil || trimmed == nil {
		return result
	}
	if h.confirmationStore != nil {
		h.confirmationStore.clearExpired(time.Now())
	}

	result.EntriesBeforeClear = sanitizeVEGroupExecutorHistory(msg.UserID, h.memory.Load(msg.UserID))
	result.UnfinishedSlot = h.memory.GetUnfinishedSlot(msg.UserID)

	h.extractSessionStartMemoryAsync(msg.UserID, result.EntriesBeforeClear)

	if result.UnfinishedSlot == nil && !msg.IsBackground {
		if slot := h.recoverInterruptedTaskSlot(msg.UserID, result.EntriesBeforeClear); slot != nil {
			result.UnfinishedSlot = slot
		}
	}

	result.Decision = resolveExplicitTaskSlotDecision(*msg, result.UnfinishedSlot)
	if pendingResult := h.handlePendingExecutionConfirmation(msg, trimmed); pendingResult.Handled {
		result.Handled = true
		result.Response = pendingResult.Response
		return result
	} else if pendingResult.ConfirmedResume {
		result.ConfirmedResume = true
		result.ConfirmedWorkflowAgentLoop = pendingResult.WorkflowAgentLoop
	}

	if h.app != nil && h.getSessionStarter() == nil {
		h.ensureInteractionInfra()
	}
	if resp, handled, setsFreshTask := h.handleRecoverableSessionDecision(&result.Decision); handled {
		if setsFreshTask {
			result.FreshTask = true
		}
		result.Handled = true
		result.Response = resp
		return result
	}

	if !h.isMaclawLLMConfigured() {
		result.Handled = true
		result.Response = &IMAgentResponse{
			Error: "MaClaw LLM is not configured, so the request cannot be processed. Configure LLM in the MaClaw client settings.",
		}
	}
	return result
}
