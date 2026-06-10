package main

import (
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func (h *IMMessageHandler) finalizeIMAgentLoopResponse(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool, clearUIAfterContextSwitch bool, confirmedResume bool) *IMAgentResponse {
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	resp.ClearUI = resp.ClearUI || clearUIAfterContextSwitch

	if confirmedResume {
		resp.ConfirmedResume = true
	}
	finalizeStartedAt := time.Now()
	resp = h.finalizeTraceResult(loopCtx, resp, firstNonEmptyTraceText(resp.Text, resp.TraceSummary), resp.Error)
	resp.FinalizeTraceNanos = time.Since(finalizeStartedAt).Nanoseconds()
	h.schedulePostLoopSideEffects(msg, loopCtx, resp, workflowAgentLoop)

	h.maybeAttachVoiceSummary(resp, msg.Platform, isVoiceInputMessage(msg))
	return resp
}

func (h *IMMessageHandler) schedulePostLoopSideEffects(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool) {
	if h == nil {
		return
	}
	respSnapshot := IMAgentResponse{}
	if resp != nil {
		respSnapshot = *resp
	}
	go func() {
		startedAt := time.Now()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[post-loop] panic user=%s panic=%v", msg.UserID, r)
			}
			log.Printf("[post-loop] done user=%s duration=%s workflow=%v", msg.UserID, time.Since(startedAt).Round(time.Millisecond), workflowAgentLoop)
		}()
		h.runEvidenceCollection(msg.UserID, msg.Text)
		h.captureWorkflowDocAfterAgentLoop(msg, loopCtx, &respSnapshot, workflowAgentLoop)
		h.recordAgentLoopTerminalExperience(loopCtx, &respSnapshot)
	}()
}

func (h *IMMessageHandler) captureWorkflowDocAfterAgentLoop(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool) {
	// V1 engine removed - post-loop doc capture is now handled by V2 workflow engine.
	return
}

func (h *IMMessageHandler) recordAgentLoopTerminalExperience(loopCtx *LoopContext, resp *IMAgentResponse) {
	if event, ok := agentLoopTerminalExperienceEvent(loopCtx, resp); ok {
		h.recordExperienceLifecycleEvent(event)
	}
}

func agentLoopTerminalExperienceEvent(loopCtx *LoopContext, resp *IMAgentResponse) (lifecycle.Event, bool) {
	ctx := experienceContextFromLoop(loopCtx)
	if ctx.TraceID == "" {
		return lifecycle.Event{}, false
	}
	state := LoopStateUnknown
	if loopCtx != nil {
		state = loopCtx.LoopState()
	}
	errorText := ""
	hardExit := false
	if resp != nil {
		errorText = resp.Error
		hardExit = resp.HardExit
	}
	event := ctx.Apply(lifecycle.Event{CreatedAt: time.Now()})
	switch {
	case errorText != "":
		event.EventType = lifecycle.EventTaskFailed
		event.Outcome = "failure"
		event.ErrorClass = "agent_loop_error"
		event.Reason = errorText
	case hardExit:
		event.EventType = lifecycle.EventTaskFailed
		event.Outcome = "hard_exit"
		event.ErrorClass = "agent_loop_hard_exit"
	case state == LoopStateFailed || state == LoopStateTimeout:
		event.EventType = lifecycle.EventTaskFailed
		event.Outcome = state.String()
		event.ErrorClass = "agent_loop_" + state.String()
	case state == LoopStateStopped || state == LoopStatePaused:
		return lifecycle.Event{}, false
	default:
		event.EventType = lifecycle.EventTaskSucceeded
		event.Outcome = "success"
		if state != LoopStateUnknown {
			event.Reason = "loop_state:" + state.String()
		}
	}
	return event, true
}

func (h *IMMessageHandler) recordWorkflowPhaseCompletedExperience(msg IMUserMessage, loopCtx *LoopContext, phaseID string) {
	ctx := experienceContextFromLoop(loopCtx)
	if ctx.TraceID == "" {
		return
	}
	if h != nil {
		ownerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
		h.workflowReviewExperienceContext.Store(ownerID, workflowReviewExperienceContext{
			EventContext: ctx,
			PhaseID:      phaseID,
			Query:        msg.Text,
		})
	}
	h.recordExperienceLifecycleEvent(ctx.Apply(lifecycle.Event{
		EventType: lifecycle.EventWorkflowPhaseCompleted,
		Outcome:   "success",
		Reason:    phaseID,
		Query:     msg.Text,
		CreatedAt: time.Now(),
	}))
}

type workflowReviewExperienceContext struct {
	lifecycle.EventContext
	PhaseID string
	Query   string
}

func (h *IMMessageHandler) recordExperienceLifecycleEvent(event lifecycle.Event) {
	if h == nil || h.app == nil {
		return
	}
	h.app.ensureExperienceLifecycleSink().RecordExperienceEvent(event)
}

func (h *IMMessageHandler) applyWorkflowAutoAdvanceResponse(userID string, advResp *workflow.WorkflowResponse, platform string) {
	return
}
