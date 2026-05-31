package main

import "github.com/RapidAI/CodeClaw/corelib/agent"

type imEntryContextOptions struct {
	Message                    *IMUserMessage
	Trimmed                    *string
	Decision                   explicitTaskSlotDecision
	EntriesBeforeClear         []agent.ConversationEntry
	UnfinishedSlot             *agent.UnfinishedTaskSlot
	FreshTask                  bool
	ConfirmedResume            bool
	ConfirmedWorkflowAgentLoop bool
}

type imEntryContextResult struct {
	EntriesBeforeClear        []agent.ConversationEntry
	UnfinishedSlot            *agent.UnfinishedTaskSlot
	FreshTask                 bool
	WorkflowAgentLoop         bool
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
	if opts.Message == nil || opts.Trimmed == nil {
		return result
	}
	msg := opts.Message
	trimmed := *opts.Trimmed

	if slotFreshTask, resp, handled := h.applyExplicitTaskSlotAction(msg, opts.Trimmed, opts.Decision, &result.EntriesBeforeClear, &result.UnfinishedSlot); handled {
		result.Handled = true
		result.Response = resp
		return result
	} else if slotFreshTask {
		result.FreshTask = true
	}

	workflowReviewPending := h.workflowReviewPending(msg.UserID, msg.IsBackground)
	if !workflowReviewPending {
		result.PendingUserReplyContext, result.HasPendingUserReply = h.bindPendingUserReplyAnswer(*msg, trimmed, &result.EntriesBeforeClear, &result.UnfinishedSlot)
	} else {
		h.pendingUserReply.Delete(msg.UserID)
		h.pendingAskUser.Delete(msg.UserID)
	}

	workflowRoute := h.routeWorkflowIMMessage(*msg, trimmed, opts.ConfirmedWorkflowAgentLoop, result.HasPendingUserReply)
	if workflowRoute.Response != nil {
		result.Handled = true
		result.Response = workflowRoute.Response
		return result
	}
	result.WorkflowAgentLoop = workflowRoute.WorkflowAgentLoop
	result.SkipNeedsConfirmGate = workflowRoute.SkipNeedsConfirmGate

	result.AskUserContext, result.HasPendingAskUser = h.consumePendingAskUserAnswer(msg.UserID, trimmed, result.EntriesBeforeClear)
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
	result.CapabilityGapContext = h.consumePendingCapabilityGapContext(msg.UserID)
	return result
}
