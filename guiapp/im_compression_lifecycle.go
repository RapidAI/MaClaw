package guiapp

import (
	"encoding/json"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func (h *IMMessageHandler) persistCompressionSummaryOnExit(userID string, summary *string) {
	if h == nil || h.currentMemoryStore() == nil || summary == nil || *summary == "" || isIsolatedAssistantSessionUserID(userID) {
		return
	}
	memoryStore := h.currentMemoryStore()
	app := h.app
	summaryText := *summary
	go func() {
		startedAt := time.Now()
		persistLastCompressionSummary(memoryStore, userID, summaryText)
		if app != nil {
			app.triggerMemoryPipelineSoon(45 * time.Second)
		}
		log.Printf("[post-conversation] compression summary persisted user=%s duration=%s", userID, time.Since(startedAt).Round(time.Millisecond))
	}()
}

func (h *IMMessageHandler) applyPendingContextCompression(userID string, history []agent.ConversationEntry, lastSummary *string) []agent.ConversationEntry {
	if h == nil {
		return history
	}
	req, ok := h.pendingContextCompression.LoadAndDelete(userID)
	if !ok {
		return history
	}
	ccReq, ok := req.(*contextCompressionRequest)
	if !ok {
		return history
	}

	compressed, dropped, summaryIndex := planHistoryCompression(history, ccReq)
	if summaryIndex < 0 || len(dropped) == 0 {
		return history
	}
	raw, err := json.Marshal(dropped)
	if err != nil {
		log.Printf("[compress_context] keep original history: marshal dropped entries failed: %v", err)
		return history
	}
	projection, err := toolresult.Project(toolresult.ProjectOptions{
		ToolName:   "context_checkpoint",
		SessionKey: h.workflowPolicyOwnerID(userID, nil),
		Content:    string(raw),
		Preview:    compressed[summaryIndex].Content.(string),
		Limit:      4096,
		ForceSpill: true,
	})
	if err != nil || projection.Handle == nil || !projection.Spilled {
		log.Printf("[compress_context] keep original history: persist dropped entries failed: %v", err)
		return history
	}
	compressed[summaryIndex].Content = projection.Preview
	if lastSummary != nil {
		*lastSummary = ccReq.Summary
	}
	log.Printf("[compress_context] applied lossless history compression for user=%s summary_len=%d handle=%s", userID, len(ccReq.Summary), projection.Handle.ID)
	return compressed
}
