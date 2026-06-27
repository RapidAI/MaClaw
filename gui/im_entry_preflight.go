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
	SkipWorkflowRouting        bool // User already made a decision at the confirmation gate; do not re-route through workflow-v2.
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

	// NOTE: In-flight marker recovery is intentionally NOT done here.
	// It is performed AFTER acquiring state.mu in enterIMMessageSerializationBoundary.
	// Reason: shouldRecoverInFlightMarker reads loopCtx (under stateMu) to check
	// if a loop is active, but the in-flight marker lifecycle is protected by
	// state.mu (the serialization lock). Without state.mu held, there is a TOCTOU
	// race: loopCtx may already be nil (cleanup func ran) while the in-flight
	// marker hasn't been cleared yet (Cleanup defer hasn't executed or FlushNow
	// is blocked). Moving recovery to post-lock ensures ConsumeInFlightTask only
	// runs after the owning loop's Cleanup() has completed.

	result.Decision = resolveExplicitTaskSlotDecision(*msg, result.UnfinishedSlot)
	if pendingResult := h.handlePendingExecutionConfirmation(msg, trimmed); pendingResult.Handled {
		result.Handled = true
		result.Response = pendingResult.Response
		return result
	} else if pendingResult.ConfirmedResume {
		result.ConfirmedResume = true
		result.SkipWorkflowRouting = true
		result.ConfirmedWorkflowAgentLoop = pendingResult.WorkflowAgentLoop
	} else if pendingResult.SkipWorkflowOnce || pendingResult.ReprocessAsFreshTask {
		// User cancelled or revised a workflow confirmation — the modified msg.Text
		// should not be re-routed through workflow matching (it would re-trigger the
		// same workflow confirmation panel in an infinite loop).
		result.SkipWorkflowRouting = true
	}

	if h.app != nil && h.getSessionStarter() == nil {
		h.ensureInteractionInfra()
	}
	if resp, handled, setsFreshTask := h.handleRecoverableSessionDecision(&result.Decision, msg.Lang); handled {
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
