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
//
// The tail boundary is group-aligned: if the raw tailStart index would
// split a tool-call group (landing between an assistant(tool_calls) and
// its tool results), it is adjusted backward to include the full group.
// This prevents orphaned tool messages that cause DeepSeek HTTP 400:
//
//	"Messages with role 'tool' must be a response to a preceding message with 'tool_calls'"
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

	// --- Group-align tailStart ---
	// Use BuildEntryGroups (the single source of truth for group boundaries)
	// to find the group containing tailStart. If tailStart lands in the
	// middle of a group, adjust it backward to the group's start so the
	// entire group is preserved in the tail.
	//
	// Example: if tailStart lands on a tool entry whose parent
	// assistant(tool_calls) is at tailStart-1, we must include the
	// assistant too, otherwise the tool entry becomes orphaned.
	groups := agent.BuildEntryGroups(history)
	g := agent.GroupContaining(groups, tailStart)
	if g != nil && tailStart > g.Start {
		tailStart = g.Start
	}
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

	log.Printf("[compress_context] history: removed %d entries, preserved head=%d tail=%d (group-aligned), new_len=%d",
		removedCount, headEnd, len(history)-tailStart, len(compressed))

	return compressed
}

// persistLastCompressionSummary saves the most recent compress_context
// summary as a task_artifact memory entry. Called once at agent loop exit.
func persistLastCompressionSummary(store interface {
	UpsertTaskArtifact(corememory.TaskArtifactUpsertOptions) (corememory.UpsertResult, error)
	Path() string
}, userID, summary string) {
	if store == nil || strings.TrimSpace(summary) == "" {
		return
	}
	preview := memoryRefPreview(summary)
	refPath, err := writeMemoryRefFile(store.Path(), userID, "context_checkpoint", summary, time.Now())
	if err != nil {
		log.Printf("[compress_context] failed to write memory ref for user=%s: %v", userID, err)
	}
	tags := []string{"context_checkpoint", "working_state"}
	if refPath != "" {
		tags = append(tags, "source_ref")
	}
	_, err = store.UpsertTaskArtifact(corememory.TaskArtifactUpsertOptions{
		Title:            "Working state checkpoint",
		Content:          preview,
		Tags:             tags,
		IdentityTagCount: 2,
		OwnerID:          userID,
		SourceType:       "context_checkpoint_ref",
		SourceURL:        refPath,
	})
	if err != nil {
		log.Printf("[compress_context] failed to persist final summary: %v", err)
	}
}
