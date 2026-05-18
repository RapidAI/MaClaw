package main

import (
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func (h *IMMessageHandler) finalizeIMAgentLoopResponse(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse, workflowAgentLoop bool, clearUIAfterContextSwitch bool, confirmedResume bool) *IMAgentResponse {
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	resp.ClearUI = resp.ClearUI || clearUIAfterContextSwitch

	h.runEvidenceCollection(msg.UserID, msg.Text)
	h.captureWorkflowDocAfterAgentLoop(msg, resp, workflowAgentLoop)

	if confirmedResume {
		resp.ConfirmedResume = true
	}
	finalizeStartedAt := time.Now()
	resp = h.finalizeTraceResult(loopCtx, resp, firstNonEmptyTraceText(resp.Text, resp.TraceSummary), resp.Error)
	resp.FinalizeTraceNanos = time.Since(finalizeStartedAt).Nanoseconds()

	h.maybeAttachVoiceSummary(resp, msg.Platform, isVoiceInputMessage(msg))
	return resp
}

func (h *IMMessageHandler) captureWorkflowDocAfterAgentLoop(msg IMUserMessage, resp *IMAgentResponse, workflowAgentLoop bool) {
	if !workflowAgentLoop || h.getWorkflowEngine() == nil || msg.IsBackground || resp == nil || resp.HardExit || len(resp.Text) <= 50 {
		return
	}
	if h.app != nil && h.app.workflowArtifactSaver != nil {
		h.app.workflowArtifactSaver.SetCurrentUserID(msg.UserID)
	}
	if phaseID, advResp, err := h.getWorkflowEngine().SavePhaseOutputAndMaybeAdvance(msg.UserID, resp.Text); err != nil {
		log.Printf("[WorkflowEngine] post-loop doc capture failed: user=%s err=%v", msg.UserID, err)
	} else if phaseID != "" {
		if cb := h.getWorkflowEngine().GetCallbacks(); cb != nil {
			_ = cb.EmitDocUpdate(msg.UserID, phaseID, resp.Text)
			log.Printf("[WorkflowEngine] post-loop doc capture: emitted doc_update for user=%s phase=%s len=%d", msg.UserID, phaseID, len(resp.Text))
		}
		h.applyWorkflowAutoAdvanceResponse(msg.UserID, advResp, msg.Platform)
	}
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
