package main

import (
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
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
	ownerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
	if workflowAgentLoop && h.isWorkflowV2Active(ownerID) {
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
		if !(workflowAgentLoop && h.isWorkflowV2Active(ownerID)) {
			h.captureWorkflowDocAfterAgentLoop(msg, loopCtx, &respSnapshot, workflowAgentLoop)
		}
		h.recordAgentLoopTerminalExperience(loopCtx, &respSnapshot)
	}()
}

func (h *IMMessageHandler) captureWorkflowDocAfterAgentLoop(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool) {
	if !workflowAgentLoop || resp == nil || resp.HardExit {
		return
	}
	ownerID := h.workflowPolicyOwnerID(msg.UserID, loopCtx)
	if h.isWorkflowV2Active(ownerID) && h.getWorkflowV2() != nil {
		// Skip if the phase output was already recorded (e.g. by SubAgent execution path)
		wf := h.getWorkflowV2()
		completedPhaseID := ""
		if state := wf.machine.GetActive(ownerID); state != nil {
			if p := state.ActivePhase(); p != nil && p.Output != "" {
				log.Printf("[workflow-v2] post-loop doc capture skipped: phase already has output (len=%d)", len([]rune(p.Output)))
				return
			} else if p != nil {
				completedPhaseID = p.ID
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
			h.recordWorkflowV2Output(ownerID, docText)
			if completedPhaseID != "" {
				h.recordWorkflowPhaseCompletedExperience(msg, loopCtx, completedPhaseID)
			}
			log.Printf("[workflow-v2] post-loop doc capture: user=%s len=%d source=%s", ownerID, len([]rune(docText)), source)
			// After recording, check if the next phase is ExecModeAutoFromPrev
			// and auto-complete it (same logic as SubAgent path).
			if wf := h.getWorkflowV2(); wf != nil {
				if updatedState := wf.machine.GetActive(ownerID); updatedState != nil {
					if nextPhase := updatedState.ActivePhase(); nextPhase != nil && nextPhase.ExecMode == v2.ExecModeAutoFromPrev {
						log.Printf("[workflow-v2] post-loop auto-completing phase=%s (ExecMode=auto_from_prev)", nextPhase.ID)
						wf.machine.RecordOutput(ownerID, docText)
					}
				}
			}
		}
		return
	}
	if h.app == nil || h.app.workflowEngine == nil || !h.app.workflowEngine.HasActiveWorkflow(ownerID) {
		return
	}
	docText := ""
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
		docText = "Workflow phase failed: " + resp.Error
		source = "error"
	}
	if docText == "" {
		return
	}
	phaseID, advResp, err := h.app.workflowEngine.SavePhaseOutputAndMaybeAdvance(ownerID, docText)
	if err != nil {
		log.Printf("[workflow] post-loop doc capture failed: user=%s err=%v", ownerID, err)
		return
	}
	if phaseID != "" {
		h.recordWorkflowPhaseCompletedExperience(msg, loopCtx, phaseID)
	}
	if advResp != nil && advResp.PhasePrompt != "" {
		h.stashedPhasePrompt.Store(ownerID, advResp.PhasePrompt)
		h.workflowAgentLoopMarker.Store(ownerID, true)
	}
	log.Printf("[workflow] post-loop doc capture: user=%s phase=%s len=%d source=%s", ownerID, phaseID, len([]rune(docText)), source)
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
