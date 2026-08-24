package main

import (
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type imEntryContextOptions struct {
	Message                    *IMUserMessage
	Trimmed                    *string
	Decision                   explicitTaskSlotDecision
	EntriesBeforeClear         []agent.ConversationEntry
	UnfinishedSlot             *agent.UnfinishedTaskSlot
	FreshTask                  bool
	ConfirmedResume            bool
	ConfirmedWorkflowAgentLoop bool
	SkipWorkflowRouting        bool
}

type imEntryContextResult struct {
	EntriesBeforeClear        []agent.ConversationEntry
	UnfinishedSlot            *agent.UnfinishedTaskSlot
	Decision                  explicitTaskSlotDecision
	FreshTask                 bool
	ConfirmedResume           bool
	SkipWorkflowRouting       bool
	WorkflowActive            bool
	WorkflowChoicePending     bool
	TemplateSubAgentPending   bool
	WorkflowAgentLoop         bool
	WorkflowReviewPending     bool
	WorkflowDocPhase          bool
	WorkflowPhaseID           string
	PhasePrompt               string // Carried synchronously from runWorkflowV2Phase; avoids sync.Map race
	SkipNeedsConfirmGate      bool
	AskUserContext            string
	PendingUserReplyContext   string
	CapabilityGapContext      string
	ClearUIAfterContextSwitch bool
	HasPendingUserReply       bool
	HasPendingAskUser         bool
	ResumeWorkingState        *agent.WorkingState
	Response                  *IMAgentResponse
	Handled                   bool
	// DeferredHostResponse runs AFTER the per-session IM serialization lock is
	// released (e.g. host-side ASR). Unlock uses sync.Once so early Unlock + defer is safe.
	DeferredHostResponse func() *IMAgentResponse
}

func (h *IMMessageHandler) resolveIMEntryContext(opts imEntryContextOptions) imEntryContextResult {
	result := imEntryContextResult{
		EntriesBeforeClear:  opts.EntriesBeforeClear,
		UnfinishedSlot:      opts.UnfinishedSlot,
		Decision:            opts.Decision,
		FreshTask:           opts.FreshTask,
		ConfirmedResume:     opts.ConfirmedResume,
		SkipWorkflowRouting: opts.SkipWorkflowRouting,
	}
	startedAt := time.Now()
	lastPhaseAt := startedAt
	var slotActionElapsed time.Duration
	var pendingReplyElapsed time.Duration
	var workflowRouteElapsed time.Duration
	var askUserElapsed time.Duration
	var taskContextElapsed time.Duration
	var capabilityGapElapsed time.Duration
	defer func() {
		total := time.Since(startedAt)
		if total > 500*time.Millisecond {
			userID := ""
			if opts.Message != nil {
				userID = opts.Message.UserID
			}
			log.Printf("[im-entry-context] slow: total=%s slot_action=%s pending_reply=%s workflow_route=%s ask_user=%s task_context=%s capability_gap=%s user=%s handled=%v",
				total.Truncate(time.Millisecond),
				slotActionElapsed.Truncate(time.Millisecond),
				pendingReplyElapsed.Truncate(time.Millisecond),
				workflowRouteElapsed.Truncate(time.Millisecond),
				askUserElapsed.Truncate(time.Millisecond),
				taskContextElapsed.Truncate(time.Millisecond),
				capabilityGapElapsed.Truncate(time.Millisecond),
				userID,
				result.Handled,
			)
		}
	}()
	if opts.Message == nil || opts.Trimmed == nil {
		return result
	}
	msg := opts.Message
	trimmed := *opts.Trimmed

	// App-exit slots (written at graceful shutdown for in-flight sessions) bind
	// automatically: reopening the restored/historical tab is the resume intent.
	opts.Decision = applyAppExitAutoResumeDecision(*msg, trimmed, result.UnfinishedSlot, opts.Decision)
	// Crash/lease-expiry recovery is deliberately opt-in: only an explicit
	// continuation message binds the recovered context. Any other substantive
	// message starts a new task and dismisses the stale recovery slot.
	//
	// This must run here, after the serialization boundary has reloaded (and,
	// if needed, materialized) the slot, but before slot actions mutate memory.
	opts.Decision = applyImplicitInFlightRecoveryDecision(*msg, trimmed, result.UnfinishedSlot, opts.Decision)
	result.Decision = opts.Decision
	if slotFreshTask, resp, handled := h.applyExplicitTaskSlotAction(msg, opts.Trimmed, opts.Decision, &result.EntriesBeforeClear, &result.UnfinishedSlot); handled {
		slotActionElapsed = time.Since(lastPhaseAt)
		result.Handled = true
		result.Response = resp
		return result
	} else if slotFreshTask {
		result.FreshTask = true
	}
	slotActionElapsed = time.Since(lastPhaseAt)
	lastPhaseAt = time.Now()

	workflowReviewPending := h.workflowReviewPending(msg.UserID, msg.IsBackground)
	result.WorkflowReviewPending = workflowReviewPending
	if !workflowReviewPending {
		result.PendingUserReplyContext, result.HasPendingUserReply = h.bindPendingUserReplyAnswer(*msg, trimmed, &result.EntriesBeforeClear, &result.UnfinishedSlot)
	} else {
		h.pendingUserReply.Delete(msg.UserID)
		h.pendingAskUser.Delete(msg.UserID)
		h.pendingRecordAudio.Delete(msg.UserID)
		h.clearPendingPostRecording(msg.UserID)
	}
	pendingReplyElapsed = time.Since(lastPhaseAt)
	lastPhaseAt = time.Now()

	// V2 workflow engine is the sole workflow routing path.
	// routeWorkflowIMMessage is kept for compilation but never called.
	var workflowRoute workflowIMRouteResult
	v2State := h.getWorkflowV2()
	log.Printf("[workflow-v2-debug] entry_context: v2=%v user=%s skip=%v", v2State != nil, msg.UserID, opts.SkipWorkflowRouting)
	// Skip workflow routing when the user already confirmed execution at the
	// confirmation gate. The modified msg.Text now contains the execution plan /
	// enhanced instruction — running it through BM25 template matching causes
	// false-positive workflow triggers (e.g. plan text matching unrelated templates).
	// Explicit workflow interactions and active sessions must always be processed.
	// Ordinary messages never auto-start a workflow.
	h.ensurePureCodingArmedForIncomingMessage(msg.UserID)
	isExplicitWorkflowCommand := strings.HasPrefix(trimmed, workflowChoiceCommandPrefix)
	hasActiveWorkflow := h.hasWorkflowRoutingContinuation(msg.UserID)
	hasPendingTemplateExecution := h.hasPendingTemplateSubAgentExecution(msg.UserID)
	_, hasPendingWorkflowChoice := h.pendingWorkflowChoice.Load(msg.UserID)
	result.WorkflowActive = hasActiveWorkflow
	result.WorkflowChoicePending = hasPendingWorkflowChoice
	// A pure-coding template can be armed before the workflow router sets its
	// WorkflowAgentLoop marker. It is still a stateful continuation and must
	// never replay an independent-answer cache.
	result.TemplateSubAgentPending = hasPendingTemplateExecution
	shouldRouteWorkflow := isExplicitWorkflowCommand || hasActiveWorkflow || hasPendingTemplateExecution
	if v2State != nil && shouldRouteWorkflow && !opts.SkipWorkflowRouting {
		workflowRoute = h.routeWithWorkflowV2(*msg, trimmed)
	}
	// Legacy fallback removed — StateMachine is the only workflow engine.
	if workflowRoute.Response != nil {
		workflowRouteElapsed = time.Since(lastPhaseAt)
		result.Handled = true
		result.Response = workflowRoute.Response
		return result
	}
	// ReplayText: user chose "skip workflow" — replace the button command text
	// with the original task text so the agent loop processes the actual task.
	if workflowRoute.ReplayText != "" {
		msg.Text = workflowRoute.ReplayText
		trimmed = strings.TrimSpace(workflowRoute.ReplayText)
		*opts.Trimmed = trimmed
	}
	result.WorkflowAgentLoop = workflowRoute.WorkflowAgentLoop
	// When workflow routing was skipped (user already confirmed at the gate)
	// but the confirmation handler set the workflow agent loop marker,
	// propagate it so the agent loop runs in workflow mode.
	if !result.WorkflowAgentLoop && opts.ConfirmedWorkflowAgentLoop {
		result.WorkflowAgentLoop = true
	}
	result.WorkflowDocPhase = workflowRoute.WorkflowDocPhase
	result.WorkflowPhaseID = workflowRoute.WorkflowPhaseID
	result.PhasePrompt = workflowRoute.PhasePrompt
	result.SkipNeedsConfirmGate = workflowRoute.SkipNeedsConfirmGate

	// Goal continuation messages bypass all confirm gates — the goal itself
	// is the user's confirmed intent. No need for per-turn confirmation.
	if msg.Platform == "goal-continuation" {
		result.SkipNeedsConfirmGate = true
	}

	workflowRouteElapsed = time.Since(lastPhaseAt)
	lastPhaseAt = time.Now()

	var resumeWS *agent.WorkingState
	result.AskUserContext, resumeWS, result.HasPendingAskUser = h.consumePendingAskUserAnswer(msg.UserID, trimmed, result.EntriesBeforeClear)
	result.ResumeWorkingState = resumeWS

	// Engine-injected post-recording choice (minutes / transcribe / keep_only).
	// Must run before record completion handling so button clicks resolve first.
	var postWS *agent.WorkingState
	if raw, ok := h.pendingPostRecording.Load(msg.UserID); ok {
		if pending, fresh := pendingPostRecordingForCurrentHistory(raw, result.EntriesBeforeClear); fresh && pending != nil {
			postWS = agent.CloneWorkingState(pending.WorkingState)
		}
	}
	if postCtx, hostResp, deferredHost, hasPost := h.consumePendingPostRecordingChoice(msg.UserID, trimmed, result.EntriesBeforeClear); hasPost {
		if deferredHost != nil {
			// Long host work (ASR) must run after the session lock is released.
			result.DeferredHostResponse = deferredHost
			return result
		}
		if hostResp != nil {
			// Fast host path (e.g. keep_only file delivery).
			result.Handled = true
			result.Response = hostResp
			return result
		}
		if result.AskUserContext != "" {
			result.AskUserContext = result.AskUserContext + "\n\n" + postCtx
		} else {
			result.AskUserContext = postCtx
		}
		result.HasPendingAskUser = true
		if postWS != nil {
			if !h.hasActivePendingPostRecording(msg.UserID, result.EntriesBeforeClear) {
				agent.AdvanceWorkingStateAfterUserReply(postWS)
			}
			result.ResumeWorkingState = postWS
		}
	}

	// Capture title/purpose/workspace before consumePendingRecordAudioAnswer clears state.
	recTitle, recPurpose := "", ""
	var openRecWS *agent.WorkingState
	if raw, ok := h.pendingRecordAudio.Load(msg.UserID); ok {
		if pending, fresh := pendingRecordAudioForCurrentHistory(raw, result.EntriesBeforeClear); fresh && pending != nil {
			recTitle, recPurpose = pending.Title, pending.Purpose
			openRecWS = agent.CloneWorkingState(pending.WorkingState)
		}
	}
	if recordCtx, recWS, hasRecord := h.consumePendingRecordAudioAnswer(msg.UserID, trimmed, result.EntriesBeforeClear); hasRecord {
		// Option B: successful save with path → inject choice GUI, skip LLM for this step.
		if isSuccessfulRecordingForChoice(trimmed) {
			lang := h.imCommandResponseLang(msg.Lang)
			if resp := h.offerPostRecordingChoice(msg.UserID, recTitle, recPurpose, trimmed, lang, result.EntriesBeforeClear, recWS); resp != nil {
				result.Handled = true
				result.Response = resp
				return result
			}
		}
		if result.AskUserContext != "" {
			result.AskUserContext = result.AskUserContext + "\n\n" + recordCtx
		} else {
			result.AskUserContext = recordCtx
		}
		result.HasPendingAskUser = true
		if recWS != nil {
			agent.AdvanceWorkingStateAfterUserReply(recWS)
			result.ResumeWorkingState = recWS
		}
	} else if h.hasActivePendingRecordAudio(msg.UserID, result.EntriesBeforeClear) {
		// Recording UI still open: force TaskContinue so casual chat cannot
		// archive/clear the session and wipe pendingRecordAudio.
		result.HasPendingAskUser = true
		soft := "[Context hint] An interactive recording session is still open (desktop mic UI; input is locked on the client). Do not call record_audio again. The user has not finished recording until a structured [Recording completed] report arrives. Answer their current message briefly if needed without ending the recording session."
		if result.AskUserContext != "" {
			result.AskUserContext = result.AskUserContext + "\n\n" + soft
		} else {
			result.AskUserContext = soft
		}
		if openRecWS != nil {
			result.ResumeWorkingState = openRecWS
		}
	}
	askUserElapsed = time.Since(lastPhaseAt)
	lastPhaseAt = time.Now()
	var taskContextClearUI bool
	result.AskUserContext, result.FreshTask, taskContextClearUI = h.applyUnifiedTaskContextDecision(
		*msg,
		trimmed,
		opts.Decision,
		result.EntriesBeforeClear,
		&result.UnfinishedSlot,
		result.AskUserContext,
		opts.ConfirmedResume || result.WorkflowAgentLoop,
		result.FreshTask,
		result.HasPendingAskUser || result.HasPendingUserReply,
	)
	// Defensive: a preset FreshTask / slot switch must not win over an open mic session
	// or a pending post-recording choice. clearPerUserSessionState would wipe pending state.
	if h.hasActivePendingRecordAudio(msg.UserID, result.EntriesBeforeClear) || h.hasActivePendingPostRecording(msg.UserID, result.EntriesBeforeClear) {
		if result.FreshTask || taskContextClearUI {
			log.Printf("[record-audio] suppressing FreshTask/clearUI while recording/post-choice active user=%s fresh=%v clearUI=%v",
				msg.UserID, result.FreshTask, taskContextClearUI)
		}
		result.FreshTask = false
		taskContextClearUI = false
		result.HasPendingAskUser = true
	}
	if result.FreshTask && !result.HasPendingAskUser {
		result.ResumeWorkingState = nil
	}
	result.ClearUIAfterContextSwitch = taskContextClearUI
	taskContextElapsed = time.Since(lastPhaseAt)
	lastPhaseAt = time.Now()
	result.CapabilityGapContext = h.consumePendingCapabilityGapContext(msg.UserID)
	capabilityGapElapsed = time.Since(lastPhaseAt)
	return result
}

func (h *IMMessageHandler) hasWorkflowRoutingContinuation(userID string) bool {
	userID = strings.TrimSpace(userID)
	if h == nil || userID == "" {
		return false
	}
	if v2State := h.getWorkflowV2(); v2State != nil && v2State.machine != nil && v2State.machine.GetActive(userID) != nil {
		return true
	}
	if h.app != nil && h.app.workflowEngine != nil && h.app.workflowEngine.HasActiveWorkflow(userID) {
		return true
	}
	return false
}
