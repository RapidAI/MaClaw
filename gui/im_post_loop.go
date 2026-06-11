package main

import (
	"log"
	"strings"
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

	// V2 workflow doc phase: capture output SYNCHRONOUSLY before returning to the caller.
	// If captured asynchronously in the goroutine, the next user message ("确认") may
	// arrive before the goroutine runs, causing the state machine to advance without
	// the phase output being recorded — SubAgent then sees empty tasks output.
	if workflowAgentLoop && h.isWorkflowV2Active(msg.UserID) {
		h.captureWorkflowDocAfterAgentLoop(msg, loopCtx, &respSnapshot, workflowAgentLoop)
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
		// V2 capture already done synchronously above; skip in goroutine to avoid double-recording.
		if !(workflowAgentLoop && h.isWorkflowV2Active(msg.UserID)) {
			h.captureWorkflowDocAfterAgentLoop(msg, loopCtx, &respSnapshot, workflowAgentLoop)
		}
		h.recordAgentLoopTerminalExperience(loopCtx, &respSnapshot)
	}()
}

func (h *IMMessageHandler) captureWorkflowDocAfterAgentLoop(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool) {
	if !workflowAgentLoop || resp == nil || resp.HardExit {
		return
	}
	if h.isWorkflowV2Active(msg.UserID) && h.getWorkflowV2() != nil {
		// Skip if the phase output was already recorded (e.g. by SubAgent execution path)
		wf := h.getWorkflowV2()
		if state := wf.machine.GetActive(msg.UserID); state != nil {
			if p := state.ActivePhase(); p != nil && p.Output != "" {
				log.Printf("[workflow-v2] post-loop doc capture skipped: phase already has output (len=%d)", len([]rune(p.Output)))
				return
			}
		}
		// Prefer the accumulated WorkflowDocBuffer (captures all iterations' text)
		// over resp.Text (which only contains the last iteration's finalized text).
		var docText string
		source := "resp.Text"
		if loopCtx != nil && loopCtx.WorkflowDocBuffer.Len() > 0 {
			if t := strings.TrimSpace(loopCtx.WorkflowDocBuffer.String()); t != "" {
				docText = t
				source = "buffer"
			}
		}
		if docText == "" {
			docText = strings.TrimSpace(resp.Text)
		}
		if docText == "" && resp.Error != "" {
			docText = "⚠️ 阶段执行出错: " + resp.Error
			source = "error"
		}
		if docText != "" {
			h.recordWorkflowV2Output(msg.UserID, docText)
			log.Printf("[workflow-v2] post-loop doc capture: user=%s len=%d source=%s", msg.UserID, len([]rune(docText)), source)
		}
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
