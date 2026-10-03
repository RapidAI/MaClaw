package guiapp

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Mid-Task Compaction for CodingSubAgent
//
// Codex-inspired improvement: when a single coding task's conversation
// approaches the context window limit, perform in-place compaction —
// summarize older tool results while preserving key anchors (modified file
// paths, recent tool calls). This allows the SubAgent to handle large
// refactoring tasks (50+ file reads) without context degradation.
//
// Design principles:
// - Triggered at 75% of effective context window
// - Preserves: system prompt + last 5 tool call/result pairs + file paths
// - Middle section replaced with a static anchor summary (modified/created
//   file paths + recent command history); deliberately no LLM call — the
//   anchors carry the durable facts and the dropped middle is mostly tool
//   output whose key data is captured by them
// - Recovery prefix tells model "previous work was compacted"
// - Maximum 3 compactions per task (prevent infinite compaction loops)
// ---------------------------------------------------------------------------

const (
	// subAgentCompactionTriggerRatio: compact when tokens exceed this ratio of context window
	subAgentCompactionTriggerRatio = 0.75

	// subAgentCompactionRecencyWindow: keep last N complete assistant turns intact
	subAgentCompactionRecencyWindow = 5

	// subAgentMaxCompactionsPerTask: prevent infinite compaction loops
	subAgentMaxCompactionsPerTask = 3
)

// SubAgentCompactor handles mid-task context compaction for CodingSubAgent.
type SubAgentCompactor struct {
	contextWindow   int // effective context tokens
	compactionCount int
	filesModifiedFn func() []string
	filesCreatedFn  func() []string
	filesReadFn     func() []string
	commandsRunFn   func() []CodingSubAgentCommandResult
}

// NewSubAgentCompactor creates a compactor for a coding task.
func NewSubAgentCompactor(contextWindow int, filesModifiedFn func() []string, filesCreatedFn func() []string, filesReadFn func() []string, commandsRunFn func() []CodingSubAgentCommandResult) *SubAgentCompactor {
	return &SubAgentCompactor{
		contextWindow:   contextWindow,
		filesModifiedFn: filesModifiedFn,
		filesCreatedFn:  filesCreatedFn,
		filesReadFn:     filesReadFn,
		commandsRunFn:   commandsRunFn,
	}
}

// ShouldCompact checks if the conversation needs compaction based on token count.
func (c *SubAgentCompactor) ShouldCompact(conversation []interface{}) bool {
	if c == nil || c.compactionCount >= subAgentMaxCompactionsPerTask {
		return false
	}
	if c.contextWindow <= 0 {
		return false
	}

	tokenEstimate := estimateConversationTokensForSubAgent(conversation)
	threshold := int(float64(c.contextWindow) * subAgentCompactionTriggerRatio)
	return tokenEstimate > threshold
}

// Compact performs in-place compaction of the conversation.
// Preserves: system message + recent window + file/command anchors.
// Middle section is replaced with a static summary.
func (c *SubAgentCompactor) Compact(conversation []interface{}) []interface{} {
	if c == nil || len(conversation) < 4 {
		return conversation // too short to compact
	}
	c.compactionCount++
	log.Printf("[coding-subagent-compaction] triggering compaction #%d (conversation_len=%d)", c.compactionCount, len(conversation))

	// Structure: [system, user(task), ...middle..., recent_window]
	// Keep: system (idx 0) + user task (idx 1) + recent window
	systemMsg := conversation[0]
	taskMsg := conversation[1]

	// Calculate recent window by counting assistant turns from the end.
	// Each "turn" is: assistant message + N tool result messages.
	// We want to keep the last subAgentCompactionRecencyWindow complete turns,
	// then shrink by token budget and split a turn that still does not fit.
	recentStart := findSubAgentRecencyWindowStart(conversation, subAgentCompactionRecencyWindow)
	if recentStart < 2 {
		recentStart = 2
	}
	keepTokens := c.contextWindow * 3 / 10
	if keepTokens < 1000 {
		keepTokens = 1000
	}
	conversation, recentStart = fitSubAgentRecentWindow(conversation, recentStart, keepTokens)
	if recentStart <= 2 {
		return conversation // nothing to compact
	}
	recentWindow := conversation[recentStart:]

	// Assemble compacted conversation.
	// Merge the summary into the task message (user role) to avoid message
	// alternation issues. The original task text + compaction summary together
	// form the "what you need to know" context for subsequent turns.
	// This guarantees: system → user(task+summary) → assistant → tool → ...
	// which is valid for all LLM APIs (OpenAI, Anthropic, DeepSeek).
	taskContent := ""
	if tm, ok := taskMsg.(map[string]interface{}); ok {
		taskContent, _ = tm["content"].(string)
	}
	middleSection := conversation[2:recentStart]
	summary := c.buildCompactionSummary(middleSection, taskContent)
	mergedTask := map[string]interface{}{
		"role":    "user",
		"content": taskContent + "\n\n---\n\n" + summary,
	}

	compacted := make([]interface{}, 0, 2+len(recentWindow))
	compacted = append(compacted, systemMsg)
	compacted = append(compacted, mergedTask)
	compacted = append(compacted, recentWindow...)

	log.Printf("[coding-subagent-compaction] compacted: %d -> %d entries (middle=%d summarized)",
		len(conversation), len(compacted), len(middleSection))

	return compacted
}

// findSubAgentRecencyWindowStart finds the start index for keeping the last N
// complete assistant turns. A "turn" = one assistant message + all following
// tool result messages until the next assistant/user message.
func findSubAgentRecencyWindowStart(conversation []interface{}, turnsToKeep int) int {
	turnsFound := 0
	for i := len(conversation) - 1; i >= 2; i-- {
		msg, ok := conversation[i].(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "assistant" {
			turnsFound++
			if turnsFound >= turnsToKeep {
				return i
			}
		}
	}
	// Couldn't find enough turns — keep everything from index 2
	return 2
}

// buildCompactionSummary creates a static summary of the compacted middle section.
// This includes file modification anchors and command history.
func (c *SubAgentCompactor) buildCompactionSummary(middleSection []interface{}, taskContent string) string {
	var b strings.Builder

	b.WriteString("[上下文压缩] 之前的工具调用因 context 长度限制被压缩。以下是工作摘要。\n\n")
	b.WriteString("另一个编码执行器已经开始处理此任务。文件系统反映了已完成的工作。请基于已完成的工作继续，避免重复已做过的事情。\n\n")

	// File anchors. Read paths accumulate with any list already written
	// into the task message by an earlier compaction.
	readFiles := mergeSubAgentPaths(callStringList(c.filesReadFn), filePathsFromSection(taskContent, "已读取文件"))
	if len(readFiles) > 0 {
		b.WriteString("## 已读取文件\n")
		for _, f := range readFiles {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
		b.WriteString("\n")
	}
	if c.filesModifiedFn != nil || taskContent != "" {
		modified := mergeSubAgentPaths(callStringList(c.filesModifiedFn), filePathsFromSection(taskContent, "已修改文件"))
		if len(modified) > 0 {
			b.WriteString("## 已修改文件\n")
			for _, f := range modified {
				b.WriteString(fmt.Sprintf("- %s\n", f))
			}
			b.WriteString("\n")
		}
	}
	if c.filesCreatedFn != nil {
		created := c.filesCreatedFn()
		if len(created) > 0 {
			b.WriteString("## 已创建文件\n")
			for _, f := range created {
				b.WriteString(fmt.Sprintf("- %s\n", f))
			}
			b.WriteString("\n")
		}
	}

	// Command history summary
	if c.commandsRunFn != nil {
		commands := c.commandsRunFn()
		if len(commands) > 0 {
			b.WriteString("## 已执行命令\n")
			shown := 0
			for i := len(commands) - 1; i >= 0 && shown < 10; i-- {
				cmd := commands[i]
				status := "OK"
				if !cmd.Succeeded {
					status = "ERR"
				}
				b.WriteString(fmt.Sprintf("- [%s] %s\n", status, truncateRunesForSubAgent(cmd.Command, 100)))
				shown++
			}
			b.WriteString("\n")
		}
	}

	// Middle section statistics
	toolCallCount := 0
	for _, msg := range middleSection {
		if m, ok := msg.(map[string]interface{}); ok {
			if m["role"] == "tool" {
				toolCallCount++
			}
		}
	}
	b.WriteString(fmt.Sprintf("压缩了 %d 条对话条目（含 %d 次工具调用）。请继续当前任务。\n", len(middleSection), toolCallCount))

	return b.String()
}

// estimateConversationTokensForSubAgent estimates the token count of a conversation.
func estimateConversationTokensForSubAgent(conversation []interface{}) int {
	totalBytes := 0
	for _, msg := range conversation {
		switch m := msg.(type) {
		case map[string]interface{}:
			if content, ok := m["content"].(string); ok {
				totalBytes += len(content)
			}
			// Handle tool_calls in both typed and untyped forms.
			// In RunLoop, tool_calls are stored as []llm.ToolCall.
			// After JSON round-trip they might become []interface{}.
			switch tc := m["tool_calls"].(type) {
			case []llm.ToolCall:
				for _, call := range tc {
					totalBytes += len(call.Function.Arguments)
				}
			case []interface{}:
				for _, item := range tc {
					if call, ok := item.(map[string]interface{}); ok {
						if fn, ok := call["function"].(map[string]interface{}); ok {
							if args, ok := fn["arguments"].(string); ok {
								totalBytes += len(args)
							}
						}
					}
				}
			}
		case map[string]string:
			if content, ok := m["content"]; ok {
				totalBytes += len(content)
			}
		case agent.ConversationEntry:
			if content, ok := m.Content.(string); ok {
				totalBytes += len(content)
			}
		}
	}
	// bytes / 2.5 ≈ tokens
	return (totalBytes*10 + 24) / 25
}

// codingSubAgentHooks implements agent.LoopHooks with mid-task compaction and
// live guide-launch / supplementary injection support for pure coding loops.
type codingSubAgentHooks struct {
	agent.DefaultLoopHooks
	compactor *SubAgentCompactor
	handler   *IMMessageHandler
	userID    string
	iteration int
	loopCtx   *LoopContext
	// replanRevision belongs to the callback queried by RunLoop. The hook only
	// advances it after incorporating the revision observed before draining.
	// A steer accepted during drain must remain visible to the replacement loop.
	replanRevision *atomic.Int64
}

func (h *codingSubAgentHooks) TransformConversation(conversation []interface{}) []interface{} {
	out := conversation
	changed := false
	processedRevision := int64(0)
	if h != nil && h.loopCtx != nil {
		// Snapshot before drain, exactly like the shared agent loop. This closes
		// the race where a steer arrives while pending injections are consumed.
		processedRevision = h.loopCtx.ReplanRevision()
	}
	if h != nil && h.handler != nil && strings.TrimSpace(h.userID) != "" {
		// Prefer length growth over injected payload text: a drained-but-empty
		// guide wrapper should not force a no-op conversation rewrite.
		next, _ := h.handler.appendPendingSteerInjections(h.userID, out, h.iteration)
		if len(next) > len(out) {
			out = next
			changed = true
		}
	}
	if h != nil && h.replanRevision != nil {
		h.replanRevision.Store(processedRevision)
	}
	if h != nil {
		h.iteration++
	}
	if h != nil && h.compactor != nil && h.compactor.ShouldCompact(out) {
		if compacted := h.compactor.Compact(out); compacted != nil {
			return compacted
		}
	}
	if !changed {
		return nil
	}
	return out
}

func codingSubAgentOwnerUserID(loopCtx *LoopContext) string {
	if loopCtx == nil {
		return ""
	}
	return strings.TrimSpace(loopCtx.UserID)
}

// buildLoopHooks creates LoopHooks for the SubAgent with compaction + steer injection.
func (s *CodingSubAgent) buildLoopHooks(cb *codingSubAgentCallbacks) *codingSubAgentHooks {
	contextWindow := s.cfg.EffectiveContextTokens()
	if contextWindow <= 0 {
		contextWindow = 128000 // conservative default
	}

	compactor := NewSubAgentCompactor(
		contextWindow,
		func() []string { return cb.getFilesModified() },
		func() []string { return cb.getFilesCreated() },
		func() []string { return cb.getFilesRead() },
		func() []CodingSubAgentCommandResult { return cb.getCommandsRun() },
	)

	var handler *IMMessageHandler
	var loopCtx *LoopContext
	if s != nil {
		handler = s.handler
		loopCtx = s.loopCtx
	}
	return &codingSubAgentHooks{
		compactor: compactor,
		handler:   handler,
		userID:    codingSubAgentOwnerUserID(loopCtx),
		loopCtx:   loopCtx,
		replanRevision: func() *atomic.Int64 {
			if cb == nil {
				return nil
			}
			return &cb.llmReplanRevision
		}(),
	}
}

// buildRemoteCodingLoopHooks wires guide-launch injection for pure remote coding.
func (r *RemoteCodingSubAgent) buildRemoteCodingLoopHooks(cb *remoteCodingCallbacks) *codingSubAgentHooks {
	var handler *IMMessageHandler
	var loopCtx *LoopContext
	if r != nil {
		handler = r.handler
		loopCtx = r.loopCtx
	}
	return &codingSubAgentHooks{
		handler: handler,
		userID:  codingSubAgentOwnerUserID(loopCtx),
		loopCtx: loopCtx,
		replanRevision: func() *atomic.Int64 {
			if cb == nil {
				return nil
			}
			return &cb.llmReplanRevision
		}(),
	}
}

func callStringList(fn func() []string) []string {
	if fn == nil {
		return nil
	}
	return fn()
}

func mergeSubAgentPaths(parts ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range parts {
		for _, path := range part {
			path = strings.TrimSpace(path)
			if path == "" || seen[path] {
				continue
			}
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}

func filePathsFromSection(summary, heading string) []string {
	marker := "## " + heading
	idx := strings.Index(summary, marker)
	if idx < 0 {
		return nil
	}
	rest := summary[idx+len(marker):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	var paths []string
	for _, line := range strings.Split(rest, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			paths = append(paths, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
		}
	}
	return paths
}

// fitSubAgentRecentWindow shrinks the kept suffix to keepTokens. A single
// assistant turn that is still too large is split so its trailing tool
// results stay verbatim and the earlier tool results fall into the summary.
func fitSubAgentRecentWindow(conversation []interface{}, recentStart, keepTokens int) ([]interface{}, int) {
	if keepTokens <= 0 || recentStart >= len(conversation) {
		return conversation, recentStart
	}
	start := recentStart
	for start < len(conversation)-1 && estimateConversationTokensForSubAgent(conversation[start:]) > keepTokens {
		if subAgentMsgHasToolCalls(conversation[start]) {
			rewritten, next, ok := splitSubAgentToolGroup(conversation, start, keepTokens)
			if ok {
				return rewritten, next
			}
		}
		start++
		for start < len(conversation) && subAgentMsgRole(conversation[start]) == "tool" {
			start++
		}
	}
	return conversation, start
}

func subAgentMsgRole(msg interface{}) string {
	switch m := msg.(type) {
	case map[string]interface{}:
		role, _ := m["role"].(string)
		return role
	case map[string]string:
		return m["role"]
	default:
		return ""
	}
}

func subAgentMsgHasToolCalls(msg interface{}) bool {
	m, ok := msg.(map[string]interface{})
	if !ok || m["tool_calls"] == nil {
		return false
	}
	switch calls := m["tool_calls"].(type) {
	case []llm.ToolCall:
		return len(calls) > 0
	case []interface{}:
		return len(calls) > 0
	default:
		return true
	}
}

func splitSubAgentToolGroup(conversation []interface{}, start, keepTokens int) ([]interface{}, int, bool) {
	end := start + 1
	for end < len(conversation) && subAgentMsgRole(conversation[end]) == "tool" {
		end++
	}
	if end-start < 2 {
		return conversation, start, false
	}
	assistantTokens := estimateConversationTokensForSubAgent(conversation[start : start+1])
	tailTokens := estimateConversationTokensForSubAgent(conversation[end:])
	remaining := keepTokens - assistantTokens - tailTokens
	if remaining <= 0 {
		return conversation, start, false
	}
	firstKept := end
	used := 0
	for i := end - 1; i > start; i-- {
		tokens := estimateConversationTokensForSubAgent(conversation[i : i+1])
		if used+tokens > remaining {
			break
		}
		used += tokens
		firstKept = i
	}
	if firstKept >= end {
		last, ok := conversation[end-1].(map[string]interface{})
		if !ok {
			return conversation, start, false
		}
		content, _ := last["content"].(string)
		maxRunes := remaining * 2
		if maxRunes < 200 {
			maxRunes = 200
		}
		truncated, _ := truncateEntryContent(content, maxRunes).(string)
		if strings.TrimSpace(truncated) == "" {
			return conversation, start, false
		}
		copied := map[string]interface{}{}
		for k, v := range last {
			copied[k] = v
		}
		copied["content"] = truncated
		assistant, ok := filterSubAgentAssistantCalls(conversation[start], []interface{}{copied})
		if !ok {
			return conversation, start, false
		}
		out := make([]interface{}, 0, len(conversation))
		out = append(out, conversation[:start]...)
		out = append(out, conversation[start+1:end-1]...)
		newStart := len(out)
		out = append(out, assistant)
		out = append(out, copied)
		out = append(out, conversation[end:]...)
		return out, newStart, true
	}
	assistant, ok := filterSubAgentAssistantCalls(conversation[start], conversation[firstKept:end])
	if !ok {
		return conversation, start, false
	}
	out := make([]interface{}, 0, len(conversation)-(firstKept-start-1))
	out = append(out, conversation[:start]...)
	out = append(out, conversation[start+1:firstKept]...)
	newStart := len(out)
	out = append(out, assistant)
	out = append(out, conversation[firstKept:]...)
	return out, newStart, true
}

func filterSubAgentAssistantCalls(assistant interface{}, keptTools []interface{}) (interface{}, bool) {
	msg, ok := assistant.(map[string]interface{})
	if !ok {
		return assistant, false
	}
	keep := map[string]bool{}
	for _, toolMsg := range keptTools {
		m, ok := toolMsg.(map[string]interface{})
		if !ok {
			return assistant, false
		}
		id, _ := m["tool_call_id"].(string)
		if id == "" {
			return assistant, false
		}
		keep[id] = true
	}
	copied := map[string]interface{}{}
	for k, v := range msg {
		copied[k] = v
	}
	switch calls := msg["tool_calls"].(type) {
	case []llm.ToolCall:
		filtered := make([]llm.ToolCall, 0, len(keep))
		for _, call := range calls {
			if keep[call.ID] {
				filtered = append(filtered, call)
			}
		}
		if len(filtered) == 0 {
			return assistant, false
		}
		copied["tool_calls"] = filtered
		return copied, true
	case []interface{}:
		filtered := make([]interface{}, 0, len(keep))
		for _, item := range calls {
			tc, ok := item.(map[string]interface{})
			if !ok {
				return assistant, false
			}
			id, _ := tc["id"].(string)
			if keep[id] {
				filtered = append(filtered, item)
			}
		}
		if len(filtered) == 0 {
			return assistant, false
		}
		copied["tool_calls"] = filtered
		return copied, true
	default:
		return assistant, false
	}
}
