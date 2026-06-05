package main

import (
	"log"
	"net/http"
	"time"

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
	execStart := time.Now()
	requestID := imRequestID(msg)

	if resp, handled := h.handleBackgroundIMRoute(msg, opts.ProvidedLoopContext, opts.HTTPClient, opts.OnProgress); handled {
		return resp
	}
	if resp, handled := h.handleExecutionConfirmationGate(opts.FreshTask, msg, opts.Trimmed, opts.HTTPClient); handled {
		return resp
	}
	if resp, handled := h.maybeReturnUnfinishedSlotHint(msg, opts.Trimmed, opts.FreshTask, opts.Decision, opts.UnfinishedSlot); handled {
		return resp
	}
	gatesDone := time.Since(execStart)

	historyStart := time.Now()
	history := h.memory.Load(msg.UserID)
	historyElapsed := time.Since(historyStart)

	loopCtxStart := time.Now()
	loopCtx := h.prepareIMLoopContext(
		opts.ProvidedLoopContext,
		msg,
		opts.HTTPClient,
		opts.SkipNeedsConfirmGate,
		opts.AskUserContext != "" || opts.PendingUserReplyContext != "",
	)
	loopCtx.WorkflowAgentLoop = opts.WorkflowAgentLoop
	loopCtxElapsed := time.Since(loopCtxStart)

	promptStart := time.Now()
	systemPrompt := h.buildIMEntrySystemPrompt(msg, history, loopCtx, opts.WorkflowAgentLoop, opts.AskUserContext, opts.PendingUserReplyContext, opts.CapabilityGapContext)
	promptElapsed := time.Since(promptStart)

	if resp, updatedHistory, handled := h.routeSubAgentExecution(msg, opts.HTTPClient, loopCtx, history, opts.OnProgress, opts.OnToken); handled {
		return resp
	} else {
		history = updatedHistory
	}

	totalPreLoop := time.Since(execStart)
	if totalPreLoop > 500*time.Millisecond {
		log.Printf("[executePreparedIMEntry] slow pre-loop: gates=%v history_load=%v system_prompt=%v loop_ctx=%v total=%v user=%s",
			gatesDone, historyElapsed, promptElapsed, loopCtxElapsed, totalPreLoop, msg.UserID)
	}
	imPerfLog("im_pre_loop", execStart, requestID, msg.UserID, "gates", gatesDone, "history_load", historyElapsed, "loop_ctx", loopCtxElapsed, "system_prompt", promptElapsed, "history_len", len(history), "prompt_len", len(systemPrompt))

	agentLoopUserText := h.agentLoopUserTextForWorkflow(msg, opts.WorkflowAgentLoop)
	resp := h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, agentLoopUserText, msg.Attachments, opts.OnProgress, opts.OnToken, opts.OnNewRound, opts.OnStreamDone, msg.MinIterations, msg.Platform)
	return h.finalizeIMAgentLoopResponse(msg, loopCtx, resp, opts.WorkflowAgentLoop, opts.ClearUIAfterContextSwitch, opts.ConfirmedResume)
}
