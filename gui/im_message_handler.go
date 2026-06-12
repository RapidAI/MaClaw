package main

import (
	"log"
	"time"

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
	msgReceivedAt := time.Now()
	requestID := imRequestID(msg)
	defer func() {
		status := "success"
		if result != nil && result.Error != "" {
			status = "error"
		}
		imPerfLog("im_message_total", msgReceivedAt, requestID, msg.UserID, "status", status, "text_len", len([]rune(msg.Text)), "platform", msg.Platform)
	}()
	lifecycle := h.beginIMMessageLifecycle(msg, &result)
	defer lifecycle.Cleanup()
	trimmed := lifecycle.Trimmed

	if resp, handled := h.handleImmediateIMCommand(msg, trimmed, onProgress, onToken); handled {
		return resp
	}
	if resp, handled := h.tryImmediateCurrentTimeDirect(msg, providedLoopCtx); handled {
		return resp
	}

	// Emit immediate progress feedback before any heavy processing (preflight/entry_context).
	// This ensures the frontend shows "正在思考..." within <100ms of message receipt.
	if onProgress != nil {
		onProgress("正在思考...")
	}

	// Start a request-level heartbeat ticker that covers the ENTIRE request
	// lifecycle — including pre-loop phases (IUM LLM calls, proactive_recall,
	// system prompt build) that happen before the agent loop's own heartbeat
	// ticker starts. This ensures the frontend's activity timeout window is
	// never triggered while the backend is actively processing.
	//
	// The agent loop has its own heartbeat ticker (startAgentLoopHeartbeat)
	// which is redundant with this one but harmless — multiple resets of the
	// frontend timer are idempotent.
	stopRequestHeartbeat := startRequestLevelHeartbeat(onProgress)
	defer stopRequestHeartbeat()

	preflight := h.prepareIMMessagePreflight(&msg, &trimmed)
	if preflight.Handled {
		return preflight.Response
	}
	preflightDone := time.Since(msgReceivedAt)
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
	serializationDone := time.Since(msgReceivedAt)

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
	entryContextDone := time.Since(msgReceivedAt)
	if entryContextDone > 500*time.Millisecond {
		log.Printf("[handleIMMessage] slow pre-execution: preflight=%v serialization=%v entry_context=%v user=%s",
			preflightDone, serializationDone-preflightDone, entryContextDone-serializationDone, msg.UserID)
	}
	imPerfLog("im_pre_execution", msgReceivedAt, requestID, msg.UserID, "preflight", preflightDone, "serialization", serializationDone-preflightDone, "entry_context", entryContextDone-serializationDone)
	unfinishedSlot = entryContext.UnfinishedSlot
	freshTask = entryContext.FreshTask
	workflowAgentLoop := entryContext.WorkflowAgentLoop
	workflowDocPhase := entryContext.WorkflowDocPhase
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
		WorkflowDocPhase:          workflowDocPhase,
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
