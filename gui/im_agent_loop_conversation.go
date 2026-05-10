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
	StartedAt    time.Time
	Elapsed      time.Duration
}

func (h *IMMessageHandler) buildAgentLoopConversationStart(loopID, userID, userText, systemPrompt, platform string, attachments []MessageAttachment, cfg corelib.MaclawLLMConfig, history []agent.ConversationEntry, priorReplanCount int, recorder *TrajectoryRecorder, tools []map[string]interface{}) agentLoopConversationStart {
	startedAt := time.Now()
	conversation := []interface{}{
		map[string]string{"role": "system", "content": systemPrompt},
	}
	for _, entry := range history {
		conversation = append(conversation, stripHistoryAttachments(entry.ToMessage()))
	}

	userContent := buildUserContent(userText, attachments, cfg.Protocol, cfg.SupportsVision)
	conversation = append(conversation, map[string]interface{}{"role": "user", "content": userContent})
	history = append(history, agent.ConversationEntry{Role: "user", Content: userContent})

	if driftTool, ok := h.sessionDriftTool.LoadAndDelete(userID); ok {
		toolName, _ := driftTool.(string)
		driftCtx := fmt.Sprintf(
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
		recorder.StartSession(loopID, h.getMaclawLLMProviders().Current, cfg.Model, cfg.Protocol, userID, platform, tools)
		recorder.Record("system", systemPrompt, nil, "", "")
		recorder.Record("user", userContent, nil, "", "")
	}

	return agentLoopConversationStart{
		Conversation: conversation,
		History:      history,
		UserContent:  userContent,
		StartedAt:    startedAt,
		Elapsed:      time.Since(startedAt),
	}
}
