package main

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type preparedIMEntryExecutionOptions struct {
	Message                   IMUserMessage
	Trimmed                   string
	ProvidedLoopContext       *LoopContext
	HTTPClient                *http.Client
	FreshTask                 bool
	Decision                  explicitTaskSlotDecision
	UnfinishedSlot            *agent.UnfinishedTaskSlot
	WorkflowAgentLoop         bool
	SkipNeedsConfirmGate      bool
	AskUserContext            string
	PendingUserReplyContext   string
	CapabilityGapContext      string
	ClearUIAfterContextSwitch bool
	ConfirmedResume           bool
	OnProgress                tool.ProgressCallback
	OnToken                   llm.TokenCallback
	OnNewRound                NewRoundCallback
	OnStreamDone              StreamDoneCallback
}

func (h *IMMessageHandler) executePreparedIMEntry(opts preparedIMEntryExecutionOptions) *IMAgentResponse {
	msg := opts.Message
	if resp, handled := h.handleBackgroundIMRoute(msg, opts.ProvidedLoopContext, opts.HTTPClient, opts.OnProgress); handled {
		return resp
	}
	if resp, handled := h.handleExecutionConfirmationGate(opts.FreshTask, msg, opts.Trimmed, opts.HTTPClient); handled {
		return resp
	}
	if resp, handled := h.maybeReturnUnfinishedSlotHint(msg, opts.Trimmed, opts.FreshTask, opts.Decision, opts.UnfinishedSlot); handled {
		return resp
	}

	history := h.memory.Load(msg.UserID)
	systemPrompt := h.buildIMEntrySystemPrompt(msg, history, opts.WorkflowAgentLoop, opts.AskUserContext, opts.PendingUserReplyContext, opts.CapabilityGapContext)
	loopCtx := h.prepareIMLoopContext(
		opts.ProvidedLoopContext,
		msg,
		opts.HTTPClient,
		opts.SkipNeedsConfirmGate,
		opts.AskUserContext != "" || opts.PendingUserReplyContext != "",
	)

	if resp, updatedHistory, handled := h.routeSubAgentExecution(msg, opts.HTTPClient, loopCtx, history, opts.OnProgress, opts.OnToken); handled {
		return resp
	} else {
		history = updatedHistory
	}
	agentLoopUserText := h.agentLoopUserTextForWorkflow(msg, opts.WorkflowAgentLoop)
	resp := h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, agentLoopUserText, msg.Attachments, opts.OnProgress, opts.OnToken, opts.OnNewRound, opts.OnStreamDone, msg.MinIterations, msg.Platform)
	return h.finalizeIMAgentLoopResponse(msg, loopCtx, resp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
}
