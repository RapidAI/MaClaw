package main

import (
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type agentLoopConversationStart struct {
	Conversation []interface{}
	History      []agent.ConversationEntry
	UserContent  interface{}
	TaskAnchor   *taskIdentityAnchor
	StartedAt    time.Time
	Elapsed      time.Duration
}

func (h *IMMessageHandler) buildAgentLoopConversationStart(loopID, userID, userText, systemPrompt, platform string, attachments []MessageAttachment, cfg corelib.MaclawLLMConfig, history []agent.ConversationEntry, priorReplanCount int, recorder *TrajectoryRecorder, tools []map[string]interface{}, onProgress func(string), allowLocalAttachmentStaging bool) agentLoopConversationStart {
	startedAt := time.Now()
	// Capture identity/source before historical attachment stripping and before
	// any compaction can discard the document body.
	anchorPrompt := ""
	var anchorSnapshot *taskIdentityAnchor
	if anchor, ok := h.taskIdentityAnchorForTurn(userID, userText); ok {
		copy := anchor
		anchorSnapshot = &copy
		anchorPrompt = taskIdentityAnchorPrompt(anchor)
		if anchorPrompt != "" {
			// The first system message is preserved by every compactor. Keep the
			// anchor there rather than as a later synthetic message that a long
			// tool loop could discard.
			systemPrompt = appendTaskIdentityAnchorPrompt(systemPrompt, anchorSnapshot)
		}
	}
	conversation := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
	}
	for _, entry := range history {
		conversation = append(conversation, stripHistoryAttachments(entry.ToMessage()))
	}

	userContent := buildUserContentWithPreparedLocalAttachments(userText, attachments, cfg.Protocol, cfg.SupportsVision, h.app, onProgress, allowLocalAttachmentStaging, true, cfg.EffectiveContextTokens())
	conversation = append(conversation, map[string]interface{}{"role": "user", "content": userContent})
	history = append(history, agent.ConversationEntry{Role: "user", Content: userContent})

	var driftCtx string
	if driftTool, ok := h.sessionDriftTool.LoadAndDelete(userID); ok {
		toolName, _ := driftTool.(string)
		driftCtx = fmt.Sprintf(
			"[System notice] The previous turn stopped after repeated failures calling %s. "+
				"Do not use the same approach again. "+
				"If no alternative is available, explain the current limitation and recommendation to the user.",
			toolName,
		)
		conversation = append(conversation, map[string]string{
			"role": "system", "content": driftCtx,
		})
		log.Printf("[DriftContext] injected drift warning for user=%s tool=%s priorReplanCount=%d", userID, toolName, priorReplanCount)
	}

	if recorder != nil {
		provider := ""
		if h != nil {
			provider = h.getMaclawLLMProviders().Current
		}
		// Kind defaults to main; shared/subagent paths overwrite via SetKind / their own sessions.
		recorder.StartSessionWithMeta(loopID, provider, cfg.Model, cfg.Protocol, userID, platform, "main", "", tools)
		recorder.SetExperienceDomain(h.resolveTrajectoryExperienceDomain("main", userID))
		recorder.Record("system", systemPrompt, nil, "", "")
		// Prior multi-turn context the model will see (before current user turn).
		if prior := history[:len(history)-1]; len(prior) > 0 {
			recorder.RecordHistory(prior)
		}
		recorder.Record("user", userContent, nil, "", "")
		if driftCtx != "" {
			recorder.Record("system", driftCtx, nil, "", "")
		}
	}

	return agentLoopConversationStart{
		Conversation: conversation,
		History:      history,
		UserContent:  userContent,
		TaskAnchor:   anchorSnapshot,
		StartedAt:    startedAt,
		Elapsed:      time.Since(startedAt),
	}
}
