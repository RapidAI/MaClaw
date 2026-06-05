package main

import (
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func (h *IMMessageHandler) persistCompressionSummaryOnExit(userID string, summary *string) {
	if h == nil || h.memoryStore == nil || summary == nil || *summary == "" {
		return
	}
	memoryStore := h.memoryStore
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

	history = applyHistoryCompression(history, ccReq)
	if lastSummary != nil {
		*lastSummary = ccReq.Summary
	}
	log.Printf("[compress_context] applied history compression for user=%s summary_len=%d", userID, len(ccReq.Summary))
	return history
}
