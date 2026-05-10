package main

import (
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// HandleIMMessage processes an IM user message and returns the Agent's response.
func (h *IMMessageHandler) HandleIMMessage(msg IMUserMessage) *IMAgentResponse {
	return h.HandleIMMessageWithProgress(msg, nil)
}

// HandleIMMessageWithProgress processes an IM message with an optional progress
// callback. When onProgress is non-nil, the agent loop sends intermediate
// status updates so the Hub can relay them and keep long-running requests alive.
func (h *IMMessageHandler) HandleIMMessageWithProgress(msg IMUserMessage, onProgress tool.ProgressCallback) *IMAgentResponse {
	return h.HandleIMMessageWithProgressAndStream(msg, onProgress, nil, nil, nil)
}

func (h *IMMessageHandler) HandleIMMessageWithExistingLoop(msg IMUserMessage, loopCtx *LoopContext, onProgress tool.ProgressCallback, onToken llm.TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback) *IMAgentResponse {
	return h.handleIMMessageWithLoop(msg, loopCtx, onProgress, onToken, onNewRound, onStreamDone)
}

func (h *IMMessageHandler) HandleIMMessageWithProgressAndStream(msg IMUserMessage, onProgress tool.ProgressCallback, onToken llm.TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback) *IMAgentResponse {
	return h.handleIMMessageWithLoop(msg, nil, onProgress, onToken, onNewRound, onStreamDone)
}

func (h *IMMessageHandler) handleIMMessageWithLoop(msg IMUserMessage, providedLoopCtx *LoopContext, onProgress tool.ProgressCallback, onToken llm.TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback) (result *IMAgentResponse) {
	lifecycle := h.beginIMMessageLifecycle(msg, &result)
	defer lifecycle.Cleanup()
	trimmed := lifecycle.Trimmed

	if resp, handled := h.handleImmediateIMCommand(msg, trimmed, onProgress, onToken); handled {
		return resp
	}

	preflight := h.prepareIMMessagePreflight(&msg, &trimmed)
	if preflight.Handled {
		return preflight.Response
	}
	httpClient := preflight.HTTPClient
	entriesBeforeClear := preflight.EntriesBeforeClear
	unfinishedSlot := preflight.UnfinishedSlot
	decision := preflight.Decision
	freshTask := preflight.FreshTask
	confirmedResume := preflight.ConfirmedResume
	confirmedWorkflowAgentLoop := preflight.ConfirmedWorkflowAgentLoop
	clearUIAfterContextSwitch := preflight.ClearUIAfterContextSwitch

	serialization := h.enterIMMessageSerializationBoundary(msg, providedLoopCtx, entriesBeforeClear, unfinishedSlot, decision)
	if serialization.Handled {
		return serialization.Response
	}
	defer serialization.Unlock()
	entriesBeforeClear = serialization.EntriesBeforeClear
	unfinishedSlot = serialization.UnfinishedSlot
	decision = serialization.Decision

	entryContext := h.resolveIMEntryContext(imEntryContextOptions{
		Message:                    &msg,
		Trimmed:                    &trimmed,
		Decision:                   decision,
		EntriesBeforeClear:         entriesBeforeClear,
		UnfinishedSlot:             unfinishedSlot,
		FreshTask:                  freshTask,
		ConfirmedResume:            confirmedResume,
		ConfirmedWorkflowAgentLoop: confirmedWorkflowAgentLoop,
	})
	if entryContext.Handled {
		return entryContext.Response
	}
	unfinishedSlot = entryContext.UnfinishedSlot
	freshTask = entryContext.FreshTask
	workflowAgentLoop := entryContext.WorkflowAgentLoop
	skipNeedsConfirmGate := entryContext.SkipNeedsConfirmGate
	askUserContext := entryContext.AskUserContext
	pendingUserReplyContext := entryContext.PendingUserReplyContext
	capabilityGapContext := entryContext.CapabilityGapContext
	clearUIAfterContextSwitch = clearUIAfterContextSwitch || entryContext.ClearUIAfterContextSwitch

	return h.executePreparedIMEntry(preparedIMEntryExecutionOptions{
		Message:                   msg,
		Trimmed:                   trimmed,
		ProvidedLoopContext:       providedLoopCtx,
		HTTPClient:                httpClient,
		FreshTask:                 freshTask,
		Decision:                  decision,
		UnfinishedSlot:            unfinishedSlot,
		WorkflowAgentLoop:         workflowAgentLoop,
		SkipNeedsConfirmGate:      skipNeedsConfirmGate,
		AskUserContext:            askUserContext,
		PendingUserReplyContext:   pendingUserReplyContext,
		CapabilityGapContext:      capabilityGapContext,
		ClearUIAfterContextSwitch: clearUIAfterContextSwitch,
		ConfirmedResume:           confirmedResume,
		OnProgress:                onProgress,
		OnToken:                   onToken,
		OnNewRound:                onNewRound,
		OnStreamDone:              onStreamDone,
	})
}
