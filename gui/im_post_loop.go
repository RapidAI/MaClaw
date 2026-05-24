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

	h.runEvidenceCollection(msg.UserID, msg.Text)
	h.captureWorkflowDocAfterAgentLoop(msg, loopCtx, resp, workflowAgentLoop)

	if confirmedResume {
		resp.ConfirmedResume = true
	}
	finalizeStartedAt := time.Now()
	resp = h.finalizeTraceResult(loopCtx, resp, firstNonEmptyTraceText(resp.Text, resp.TraceSummary), resp.Error)
	resp.FinalizeTraceNanos = time.Since(finalizeStartedAt).Nanoseconds()
	h.recordAgentLoopTerminalExperience(loopCtx, resp)

	h.maybeAttachVoiceSummary(resp, msg.Platform, isVoiceInputMessage(msg))
	return resp
}

func (h *IMMessageHandler) captureWorkflowDocAfterAgentLoop(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool) {
	if !workflowAgentLoop || h.getWorkflowEngine() == nil || msg.IsBackground || resp == nil || resp.HardExit || len(resp.Text) <= 50 {
		return
	}
	if h.app != nil && h.app.workflowArtifactSaver != nil {
		h.app.workflowArtifactSaver.SetCurrentUserID(msg.UserID)
	}
	if phaseID, advResp, err := h.getWorkflowEngine().SavePhaseOutputAndMaybeAdvance(msg.UserID, resp.Text); err != nil {
		log.Printf("[WorkflowEngine] post-loop doc capture failed: user=%s err=%v", msg.UserID, err)
	} else if phaseID != "" {
		h.recordWorkflowPhaseCompletedExperience(msg, loopCtx, phaseID)
		if cb := h.getWorkflowEngine().GetCallbacks(); cb != nil {
			_ = cb.EmitDocUpdate(msg.UserID, phaseID, resp.Text)
			log.Printf("[WorkflowEngine] post-loop doc capture: emitted doc_update for user=%s phase=%s len=%d", msg.UserID, phaseID, len(resp.Text))
		}
		h.applyWorkflowAutoAdvanceResponse(msg.UserID, advResp, msg.Platform)
	}
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
		h.workflowReviewExperienceContext.Store(msg.UserID, workflowReviewExperienceContext{
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
	if advResp == nil {
		return
	}
	engine := h.getWorkflowEngine()
	if engine == nil {
		return
	}
	if advResp.ShowForm && advResp.FormSchema != nil {
		formResp := h.workflowFormResponse(engine, userID, platform, advResp)
		if formResp != nil && formResp.Text != "" {
			if cb := engine.GetCallbacks(); cb != nil {
				_ = cb.SendTextToUser(userID, formResp.Text)
			}
		}
		return
	}
	if advResp.Text != "" {
		if cb := engine.GetCallbacks(); cb != nil {
			_ = cb.SendTextToUser(userID, advResp.Text)
		}
	}
	if advResp.Complete {
		if cb := engine.GetCallbacks(); cb != nil {
			if adapter, ok := cb.(*GUIWorkflowAdapter); ok {
				adapter.ResetSuggestMaximize(userID)
			}
		}
		return
	}
	if advResp.RunAgentLoop && advResp.PhasePrompt != "" {
		h.stashedPhasePrompt.Store(userID, advResp.PhasePrompt)
		h.workflowAgentLoopMarker.Store(userID, true)
	}
}
