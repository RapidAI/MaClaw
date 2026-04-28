package main

// compress_context tool: allows the LLM to actively compress its working
// context at key checkpoints during long-running tasks.
//
// Inspired by GenericAgent's update_working_checkpoint tool. Instead of
// relying solely on passive trimHistory/trimConversation to manage context
// size, this tool lets the LLM proactively replace detailed tool call
// history with a concise summary when it determines that intermediate
// details are no longer needed for future decisions.
//
// Design: compression operates on `history` (the single source of truth
// for persistence). `conversation` (the LLM-facing array) is NOT directly
// modified — it will be naturally trimmed by `trimConversation` on the
// next iteration based on the reduced token budget. This avoids the
// dual-array desync problem: `conversation` contains injected system
// messages (GoalAnchor, ProgressTracker, recover prompts) that `history`
// doesn't have, so index-based compression on both arrays would produce
// inconsistent boundaries.

import (
	"fmt"
	"log"
	"strings"
	"time"

	agent "github.com/RapidAI/CodeClaw/corelib/agent"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
)

// toolCompressContext handles the compress_context tool call.
func (h *IMMessageHandler) toolCompressContext(args map[string]interface{}) string {
	summary, _ := args["summary"].(string)
	if strings.TrimSpace(summary) == "" {
		return "缺少 summary 参数。请提供当前工作状态的摘要。"
	}

	preserveLastN := 4
	if v, ok := args["preserve_last"].(float64); ok && v > 0 {
		preserveLastN = int(v)
		if preserveLastN > 20 {
			preserveLastN = 20
		}
	}

	userID := h.lastUserID
	h.pendingContextCompression.Store(userID, &contextCompressionRequest{
		Summary:       summary,
		PreserveLastN: preserveLastN,
		Timestamp:     time.Now(),
	})

	return "✅ 上下文压缩已排队。下一轮迭代开始时，之前的详细历史将被替换为你提供的摘要。"
}

// contextCompressionRequest holds a pending compression request.
type contextCompressionRequest struct {
	Summary       string
	PreserveLastN int
	Timestamp     time.Time
}

// applyHistoryCompression replaces intermediate entries in the history
// array with a compact summary. This is the ONLY compression function —
// conversation is NOT directly compressed. trimConversation handles
// conversation naturally on the next iteration.
//
// Preserves:
// - The first user message (the original task)
// - The last N entries (recent context including the compress_context call)
// - Replaces everything in between with a summary entry.
func applyHistoryCompression(history []agent.ConversationEntry, req *contextCompressionRequest) []agent.ConversationEntry {
	if req == nil || len(history) < 6 {
		return history
	}

	preserveTail := req.PreserveLastN
	if preserveTail < 2 {
		preserveTail = 2
	}

	// Find the first user entry.
	firstUserIdx := -1
	for i, e := range history {
		if e.Role == "user" {
			firstUserIdx = i
			break
		}
	}
	if firstUserIdx < 0 {
		firstUserIdx = 0
	}

	headEnd := firstUserIdx + 1
	tailStart := len(history) - preserveTail
	if tailStart <= headEnd {
		return history
	}

	removedCount := tailStart - headEnd
	summaryMsg := fmt.Sprintf(
		"[上下文压缩] Agent 主动压缩的工作状态摘要（替代了 %d 条历史记录）：\n\n%s",
		removedCount, req.Summary,
	)

	var compressed []agent.ConversationEntry
	compressed = append(compressed, history[:headEnd]...)
	compressed = append(compressed, agent.ConversationEntry{
		Role:    "system",
		Content: summaryMsg,
	})
	compressed = append(compressed, history[tailStart:]...)

	log.Printf("[compress_context] history: removed %d entries, preserved head=%d tail=%d, new_len=%d",
		removedCount, headEnd, preserveTail, len(compressed))

	return compressed
}

// persistLastCompressionSummary saves the most recent compress_context
// summary as a task_artifact memory entry. Called once at agent loop exit.
func persistLastCompressionSummary(store interface{ Save(corememory.Entry) error }, summary string) {
	if store == nil || strings.TrimSpace(summary) == "" {
		return
	}
	entry := corememory.Entry{
		Content:  summary,
		Category: corememory.CategoryTaskArtifact,
		Tags:     []string{"context_checkpoint", "working_state"},
	}
	if err := store.Save(entry); err != nil {
		log.Printf("[compress_context] failed to persist final summary: %v", err)
	}
}
