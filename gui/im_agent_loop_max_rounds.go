package main

import (
	"fmt"
	"log"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type agentLoopMaxRoundsExitOptions struct {
	UserID                 string
	UserText               string
	History                []agent.ConversationEntry
	Phase                  agentLoopPhase
	FinalIteration         int
	TotalToolCallsInLoop   int
	EffectiveMax           int
	ConfigMax              int
	LoopMaxOverride        int
	ChatFinalizeGrace      int
	ConversationStartedAt  time.Time
	AttachLLMTelemetry     func(*IMAgentResponse)
	AttachVisibleArtifacts func(*IMAgentResponse)
}

func (h *IMMessageHandler) maxRoundsAgentLoopExit(ctx *LoopContext, opts agentLoopMaxRoundsExitOptions) *IMAgentResponse {
	log.Printf("[AgentLoop] MAX ROUNDS EXHAUSTED loop=%s iteration=%d effectiveMax=%d configMax=%d loopOverride=%d grace=%d kind=%d user=%q task=%q elapsed=%s",
		ctx.ID, opts.FinalIteration, opts.EffectiveMax, opts.ConfigMax, opts.LoopMaxOverride, opts.ChatFinalizeGrace, ctx.Kind, opts.UserID, truncateRunes(opts.UserText, 80), time.Since(opts.ConversationStartedAt))
	h.createMaxRoundsUnfinishedSlot(opts.UserID, opts.History)

	resp := &IMAgentResponse{Text: "(Maximum reasoning rounds reached. Send another message to continue the task.)"}
	opts.AttachLLMTelemetry(resp)
	opts.AttachVisibleArtifacts(resp)
	history := h.injectNudgeMessages(opts.History, opts.FinalIteration, opts.TotalToolCallsInLoop, opts.Phase, opts.UserText)
	h.saveConversationHistoryTimed(opts.UserID, history, resp)
	return resp
}

func (h *IMMessageHandler) createMaxRoundsUnfinishedSlot(userID string, history []agent.ConversationEntry) {
	originalTask := extractOriginalUserTask(history)
	if originalTask == "" {
		return
	}
	slotID := fmt.Sprintf("maxround-%d", time.Now().UnixMilli())
	h.memory.UpsertUnfinishedSlot(userID, &agent.UnfinishedTaskSlot{
		SlotID:       slotID,
		UserID:       userID,
		ProjectPath:  h.effectiveWorkingDirForUser(userID),
		Status:       agent.UnfinishedTaskSlotStatusMaxRoundsReached,
		LastTask:     originalTask,
		Summary:      extractProgressSummary(history),
		ResumePrompt: "The user sent 'continue' to resume this task. Continue from completed work in the conversation history and avoid repeating completed steps.\n",
		Source:       agent.UnfinishedTaskSlotSourceMaxRounds,
	})
	log.Printf("[MaxRounds] created unfinished slot %s for user %s, task=%q", slotID, userID, truncateRunes(originalTask, 80))
}
