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
	FreshTask                 bool
	WorkflowAgentLoop         bool
	WorkflowDocPhase          bool
	WorkflowPhaseID           string
	SkipNeedsConfirmGate      bool
	AskUserContext            string
	PendingUserReplyContext   string
	CapabilityGapContext      string
	ClearUIAfterContextSwitch bool
	HasPendingUserReply       bool
	HasPendingAskUser         bool
	Response                  *IMAgentResponse
	Handled                   bool
}

func (h *IMMessageHandler) resolveIMEntryContext(opts imEntryContextOptions) imEntryContextResult {
	result := imEntryContextResult{
		EntriesBeforeClear: opts.EntriesBeforeClear,
		UnfinishedSlot:     opts.UnfinishedSlot,
		FreshTask:          opts.FreshTask,
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
	if !workflowReviewPending {
		result.PendingUserReplyContext, result.HasPendingUserReply = h.bindPendingUserReplyAnswer(*msg, trimmed, &result.EntriesBeforeClear, &result.UnfinishedSlot)
	} else {
		h.pendingUserReply.Delete(msg.UserID)
		h.pendingAskUser.Delete(msg.UserID)
	}
	pendingReplyElapsed = time.Since(lastPhaseAt)
	lastPhaseAt = time.Now()

	// V2 workflow engine is the sole workflow routing path.
	// routeWorkflowIMMessage is kept for compilation but never called.
	var workflowRoute workflowIMRouteResult
	v2State := h.getWorkflowV2()
	v2Disabled := h.app != nil && h.app.workflowDisabled.Load()
	log.Printf("[workflow-v2-debug] entry_context: v2=%v disabled=%v user=%s skip=%v", v2State != nil, v2Disabled, msg.UserID, opts.SkipWorkflowRouting)
	// Skip workflow routing when the user already confirmed execution at the
	// confirmation gate. The modified msg.Text now contains the execution plan /
	// enhanced instruction — running it through BM25 template matching causes
	// false-positive workflow triggers (e.g. plan text matching unrelated templates).
	if v2State != nil && !v2Disabled && !opts.SkipWorkflowRouting {
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
	result.WorkflowDocPhase = workflowRoute.WorkflowDocPhase
	result.WorkflowPhaseID = workflowRoute.WorkflowPhaseID
	result.SkipNeedsConfirmGate = workflowRoute.SkipNeedsConfirmGate
	workflowRouteElapsed = time.Since(lastPhaseAt)
	lastPhaseAt = time.Now()

	result.AskUserContext, result.HasPendingAskUser = h.consumePendingAskUserAnswer(msg.UserID, trimmed, result.EntriesBeforeClear)
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
	result.ClearUIAfterContextSwitch = taskContextClearUI
	taskContextElapsed = time.Since(lastPhaseAt)
	lastPhaseAt = time.Now()
	result.CapabilityGapContext = h.consumePendingCapabilityGapContext(msg.UserID)
	capabilityGapElapsed = time.Since(lastPhaseAt)
	return result
}
