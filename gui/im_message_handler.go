package main

import (
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
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
	bindingScope, bindingErr := prepareAssistantBindingTurn(msg)
	if bindingScope != nil {
		// Continue the entire queued turn with its validated, immutable binding
		// snapshot. Besides tool policy, the binding also contributes paths and
		// initial instructions to the system prompt, so retaining the transport
		// pointer here could make prompt behavior diverge from the cache key.
		msg.AssistantBinding = cloneAssistantBinding(&bindingScope.binding)
	}
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
	if bindingErr != "" {
		return finalizeIMEntryHostResponse(&IMAgentResponse{Text: bindingErr}, requestID, msg.UserID)
	}
	// A profile queue can be stopped while a turn is waiting to enter the IM
	// pipeline. Check before command handling, audit, memory warmup or any
	// session mutation so a canceled turn cannot produce a late reply after the
	// bot was removed or reconfigured.
	if err := contextErr(msg.CancelCtx); err != nil {
		return &IMAgentResponse{Error: err.Error()}
	}

	// Notify goal continuation engine that a user message arrived.
	// This cancels any pending continuation (user takes priority) and resets
	// the no-tool counter. Skip for goal-continuation self-messages.
	if msg.Platform != "goal-continuation" && h.app != nil && h.app.goalContinuation != nil {
		h.app.goalContinuation.OnUserMessage(msg.UserID)
	}

	// /moa one-shot: arm multi-model council and rewrite to plain user text.
	// Group turns must not mutate the per-user MoA session before normal group
	// command guarding runs. Shared parser: corelib/llm/moa.ParseSlash (supports
	// @preset Phase 2).
	if classifyImmediateIMCommand(trimmed) == imCommandMoA {
		lang := h.imCommandResponseLang(msg.Lang)
		if providedLoopCtx != nil && providedLoopCtx.LansengerGroupPermissions != nil {
			return &IMAgentResponse{Text: localizedLansengerGroupCommandRestrictedMessage(lang)}
		}
		cmd := moa.ParseSlash(trimmed)
		switch cmd.Kind {
		case moa.SlashHelp:
			return &IMAgentResponse{Text: localizedIMMoAUsageText(lang)}
		case moa.SlashUsage:
			return &IMAgentResponse{Text: localizedIMMoAAtPresetUsage(lang, cmd.Hint)}
		case moa.SlashStats:
			line := moa.FormatStatsLine()
			if line == "" {
				line = localizedIMMoAStatsEmpty(lang)
			}
			return &IMAgentResponse{Text: line}
		case moa.SlashSticky:
			// Desktop sticky is controlled via sidebar/settings; surface TUI-style hint.
			return &IMAgentResponse{Text: localizedIMMoAStickyHint(lang)}
		case moa.SlashOneShot:
			if strings.TrimSpace(cmd.Prompt) == "" {
				return &IMAgentResponse{Text: localizedIMMoAUsageText(lang)}
			}
			if errText := h.tryArmMoAOneShotPreset(msg.UserID, lang, cmd.Preset); errText != "" {
				return &IMAgentResponse{Text: errText}
			}
			trimmed = cmd.Prompt
			msg.Text = cmd.Prompt
		default:
			return &IMAgentResponse{Text: localizedIMMoAUsageText(lang)}
		}
	}

	if resp, handled := h.handleImmediateIMCommandWithLoop(msg, trimmed, providedLoopCtx, onProgress, onToken); handled {
		return resp
	}
	if resp, handled := h.tryImmediateCurrentTimeDirect(msg, providedLoopCtx); handled {
		return resp
	}
	// Listing scheduled tasks is a deterministic read of local application state.
	// Route it straight to the built-in tool so every IM channel gets the same
	// scheduler view as the desktop task-management panel, without depending on
	// an LLM selecting manage_schedule from its tool budget.
	if resp, handled := h.tryImmediateScheduleListDirect(msg, providedLoopCtx); handled {
		return resp
	}
	// An explicit task ID makes an immediate run unambiguous. It can use the
	// same direct tool path as listing, while name-only requests keep normal
	// agent planning so they cannot select the wrong task.
	if resp, handled := h.tryImmediateScheduleRunDirect(msg, providedLoopCtx); handled {
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

	// 治本: expand GUI-selected document paths into bounded native extracts so the
	// model can answer without first calling office(read_document). Caps live in
	// agent.ExpandUserSelectedFilePaths (per-file + total + file-size). Idempotent.
	// MUST run after preflight (confirmation rewrites).
	if expanded := agent.ExpandUserSelectedFilePaths(msg.Text); expanded != msg.Text {
		msg.Text = expanded
		trimmed = strings.TrimSpace(msg.Text)
	}

	// Eager embedding warmup: pre-compute the query embedding in the background
	// so that proactive recall (which runs later during system prompt construction)
	// gets a cache hit instead of paying cold-start model inference latency.
	// Without this, the embedding inference (~400ms on CPU) pushes proactive recall
	// past its 2s timeout budget, causing ALL recalled memories to be discarded.
	//
	// MUST be after prepareIMMessagePreflight: the preflight may rewrite msg.Text
	// (e.g. confirmationApprovedText replaces "确认" with the full execution plan).
	// Use CompactQueryForEmbedding so a 20k–40k document body does not dominate
	// (or timeout) the embedding query — recall keys on intent + paths.
	if h.memoryStore != nil && msg.Text != "" {
		h.memoryStore.WarmQueryEmbedding(agent.CompactQueryForEmbedding(msg.Text))
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
	clearAssistantBinding := activateAssistantBindingForTurn(msg.UserID, bindingScope)
	defer clearAssistantBinding()
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
		SkipWorkflowRouting:        preflight.SkipWorkflowRouting,
	})
	if entryContext.Handled {
		return finalizeIMEntryHostResponse(entryContext.Response, requestID, msg.UserID)
	}
	// Host-side post-recording ASR (and similar) must not hold state.mu: the
	// serialization acquire timeout is 60s and long ASR would block the session.
	if entryContext.DeferredHostResponse != nil {
		serialization.Unlock() // Once-safe; deferred Unlock() becomes a no-op
		return finalizeIMEntryHostResponse(entryContext.DeferredHostResponse(), requestID, msg.UserID)
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
	workflowPhaseID := entryContext.WorkflowPhaseID
	phasePrompt := entryContext.PhasePrompt
	skipNeedsConfirmGate := entryContext.SkipNeedsConfirmGate
	askUserContext := entryContext.AskUserContext
	pendingUserReplyContext := entryContext.PendingUserReplyContext
	capabilityGapContext := entryContext.CapabilityGapContext
	clearUIAfterContextSwitch = clearUIAfterContextSwitch || entryContext.ClearUIAfterContextSwitch
	cached, cacheKey, finishCacheFlight := h.answerCacheLookup(msg, entryContext, bindingScope)
	if cached != nil {
		return cached
	}
	if finishCacheFlight != nil {
		defer finishCacheFlight()
	}
	result = h.executePreparedIMEntry(preparedIMEntryExecutionOptions{
		Message:                   msg,
		Trimmed:                   trimmed,
		ProvidedLoopContext:       providedLoopCtx,
		HTTPClient:                httpClient,
		FreshTask:                 freshTask,
		Decision:                  decision,
		UnfinishedSlot:            unfinishedSlot,
		WorkflowAgentLoop:         workflowAgentLoop,
		WorkflowDocPhase:          workflowDocPhase,
		WorkflowPhaseID:           workflowPhaseID,
		PhasePrompt:               phasePrompt,
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
	h.storeAnswerCacheResult(msg, cacheKey, bindingScope, result)
	return result
}

// finalizeIMEntryHostResponse fills request/session routing fields for short-circuit
// host responses (post-recording keep_only / deferred transcribe).
func finalizeIMEntryHostResponse(resp *IMAgentResponse, requestID, userID string) *IMAgentResponse {
	if resp == nil {
		return nil
	}
	if strings.TrimSpace(resp.RequestID) == "" {
		resp.RequestID = requestID
	}
	if strings.TrimSpace(resp.SessionKey) == "" {
		resp.SessionKey = userID
	}
	return resp
}
