package guiapp

// compress_context tool: allows the LLM to actively compress its working
// context at key checkpoints during long-running tasks.
//
// Inspired by GenericAgent's update_working_checkpoint tool. Instead of
// relying solely on passive trimHistory/trimConversation to manage context
// size, this tool lets the LLM proactively replace detailed tool call
// history with a concise summary when it determines that intermediate
// details are no longer needed for future decisions.
//
// Design: compression replaces both the persisted `history` and the live
// LLM-facing `conversation` at the same boundary. Before any entries are
// dropped, their exact JSON is persisted as an encrypted read_tool_result
// handle; failure to persist leaves both views untouched. Keeping the two
// projections in sync prevents the compacted durable history from being
// undone by a later save of the uncompacted live request.

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
	if compactionSummaryUnusable(summary, "") {
		return "摘要为空、过长或包含工具调用标记。请提供不超过 5000 字符的纯文本工作状态摘要。"
	}

	preserveLastN := 4
	if v, ok := args["preserve_last"].(float64); ok && v > 0 {
		preserveLastN = int(v)
		if preserveLastN > 20 {
			preserveLastN = 20
		}
	}

	userID := h.consumeRuntimePolicyOwnerIDFromToolArgsOrCurrent(args)
	if strings.TrimSpace(userID) == "" {
		return "无法压缩上下文：当前运行上下文缺少 owner。"
	}
	h.pendingContextCompression.Store(userID, &contextCompressionRequest{
		Summary:       summary,
		PreserveLastN: preserveLastN,
		Timestamp:     time.Now(),
	})

	return "上下文压缩已排队。下一轮迭代开始时，详细历史将替换为你提供的摘要；原文会保留为加密的 read_tool_result 句柄，可用 id 和 query 检索。"
}

// contextCompressionRequest holds a pending compression request.
type contextCompressionRequest struct {
	Summary       string
	PreserveLastN int
	Timestamp     time.Time
}

// applyHistoryCompression returns the history-only plan for an active
// compression request. The lifecycle caller applies the same summary and
// recovery handle to the live conversation so the two views commit together.
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
	compressed, _, _ := planHistoryCompression(history, req)
	return compressed
}

func planHistoryCompression(history []agent.ConversationEntry, req *contextCompressionRequest) (compressed, dropped []agent.ConversationEntry, summaryIndex int) {
	summaryIndex = -1
	if req == nil || len(history) < 6 {
		return history, nil, summaryIndex
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
		return history, nil, summaryIndex
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
		return history, nil, summaryIndex
	}

	removedCount := tailStart - headEnd
	summaryMsg := fmt.Sprintf(
		"[上下文压缩] Agent 主动压缩的工作状态摘要（替代了 %d 条历史记录）：\n\n%s",
		removedCount, req.Summary,
	)

	dropped = append(dropped, history[headEnd:tailStart]...)
	compressed = append(compressed, history[:headEnd]...)
	summaryIndex = len(compressed)
	compressed = append(compressed, agent.ConversationEntry{
		Role:    "system",
		Content: summaryMsg,
	})
	compressed = append(compressed, history[tailStart:]...)

	log.Printf("[compress_context] history: removed %d entries, preserved head=%d tail=%d (group-aligned), new_len=%d",
		removedCount, headEnd, len(history)-tailStart, len(compressed))

	return compressed, dropped, summaryIndex
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
