package main

import (
	"log"
	"time"
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
	if phaseID := h.getWorkflowEngine().SavePhaseOutput(msg.UserID, resp.Text); phaseID != "" {
		if cb := h.getWorkflowEngine().GetCallbacks(); cb != nil {
			_ = cb.EmitDocUpdate(msg.UserID, phaseID, resp.Text)
			log.Printf("[WorkflowEngine] post-loop doc capture: emitted doc_update for user=%s phase=%s len=%d", msg.UserID, phaseID, len(resp.Text))
		}
	}
}
