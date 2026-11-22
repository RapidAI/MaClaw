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
// - Compaction stays armed for the life of the task. A compact that does not
//   shrink the estimate is sealed until the conversation grows past that size,
//   so a long task cannot run past the window after a fixed number of passes.
// ---------------------------------------------------------------------------

const (
	// subAgentCompactionTriggerRatio: compact when tokens exceed this ratio of context window
	subAgentCompactionTriggerRatio = 0.75

	// subAgentHandoffTriggerRatio is the coding/all flag threshold.
	subAgentHandoffTriggerRatio = 0.45

	// subAgentCompactionRecencyWindow: keep last N complete assistant turns intact
	subAgentCompactionRecencyWindow = 5

	// subAgentHandoffRecencyWindow is the live tail when the handoff flag is on.
	subAgentHandoffRecencyWindow = 2

	// subAgentCompactionMarker is the flag-off summary lead. A later compact
	// replaces the previous block instead of appending another one.
	subAgentCompactionMarker = "[上下文压缩]"

	// subAgentDegeneratePromptTokens is a provider prompt size that cannot be a
	// real coding prompt after one that already filled the window. The incident
	// reported 122 tokens immediately after 257658, with no compaction between them.
	subAgentDegeneratePromptTokens = 1000

	// subAgentDegeneratePromptWindow keeps that check off tiny test windows.
	subAgentDegeneratePromptWindow = 8000

	// subAgentCompactionFallbackWindow is only an estimate trigger for a model
	// whose context length is still unknown. It is not a stall ceiling: a
	// guessed 128k window would stop a task that still has output room.
	subAgentCompactionFallbackWindow = 128000
)

// SubAgentCompactor handles mid-task context compaction for CodingSubAgent.
type SubAgentCompactor struct {
	// contextWindow is the effective window when windowFn is nil (tests).
	// Production reads the live model window through windowFn, because the
	// loop can route to a different context length after the hooks are built.
	contextWindow int
	// windowFn returns the context window of the model the loop is about to
	// call. A positive value is authoritative. Zero means unknown: the byte
	// estimate may still compact against the fallback, but a prompt size must
	// not stall the task against a guessed ceiling.
	windowFn        func() int
	compactionCount int
	// sealedEstimate is the token estimate after a compact that did not shrink.
	// ShouldCompact stays quiet until the conversation grows past it, unless
	// the provider prompt already fills the known window.
	sealedEstimate int
	// lastPromptTokens is the provider-reported prompt size of the latest
	// request. The byte estimate is not the only occupancy signal.
	lastPromptTokens int
	// forceCompact is set when the latest accepted prompt already fills the
	// known window, so the next transform compacts even if a no-shrink seal
	// is holding. A later prompt under that window clears it.
	forceCompact bool
	// stalled means the prompt fills the window and the last compact did not
	// shrink it. The loop stops instead of sending that prompt again.
	stalled         bool
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
// The provider's last prompt size counts as occupancy, so a prompt the byte
// estimate under-counted still compacts. A no-shrink seal holds until the
// conversation grows past the sealed estimate, but a prompt that already fills
// the known window bypasses it. If that pass still does not shrink, the
// compactor stalls and the loop stops.
func (c *SubAgentCompactor) ShouldCompact(conversation []interface{}) bool {
	if c == nil || c.stalled {
		return false
	}
	if agent.HandoffMemoCoding() && agent.ConversationHasPendingAsk(conversation) {
		return false
	}
	window := c.triggerWindow()
	if window <= 0 {
		return false
	}

	est := estimateConversationTokensForSubAgent(conversation)
	if c.sealedEstimate > 0 && est <= c.sealedEstimate && !c.promptPressured() {
		return false
	}
	threshold := c.triggerThreshold()
	_, known := c.contextLimit()
	overPrompt := known && c.lastPromptTokens > threshold
	return c.forceCompact || overPrompt || est > threshold
}

// contextLimit is the effective window of the model about to be called.
// known is false when that length was never configured. Callers must not
// treat the fallback trigger as a full window.
func (c *SubAgentCompactor) contextLimit() (window int, known bool) {
	if c == nil {
		return 0, false
	}
	if c.windowFn != nil {
		if n := c.windowFn(); n > 0 {
			return n, true
		}
		return 0, false
	}
	if c.contextWindow > 0 {
		return c.contextWindow, true
	}
	return 0, false
}

// triggerWindow is the denominator for the byte-estimate trigger. An unknown
// live window still compacts against the fallback so a long task is not left
// unbounded, but promptPressured stays false in that case.
func (c *SubAgentCompactor) triggerWindow() int {
	if window, known := c.contextLimit(); known {
		return window
	}
	if c != nil && c.windowFn != nil {
		return subAgentCompactionFallbackWindow
	}
	return 0
}

// promptPressured reports that the latest provider prompt fills the known
// window, so a no-shrink seal must not skip compaction.
func (c *SubAgentCompactor) promptPressured() bool {
	window, known := c.contextLimit()
	return c != nil && known && (c.forceCompact || c.lastPromptTokens >= window)
}

func (c *SubAgentCompactor) triggerThreshold() int {
	window := c.triggerWindow()
	if window <= 0 {
		return 0
	}
	ratio := subAgentCompactionTriggerRatio
	if agent.HandoffMemoCoding() {
		ratio = subAgentHandoffTriggerRatio
	}
	return int(float64(window) * ratio)
}

// notePromptTokens records a provider prompt size. A tiny report after a
// prompt that already filled the window is not a successful shrink: the
// over-window size stays, and the next transform still compacts. A later
// accepted prompt under the window clears that force, so a reference fan-out
// cannot stick a full-window flag onto the aggregator's smaller prompt. The
// bool reports whether the meter should treat n as the live fill.
func (c *SubAgentCompactor) notePromptTokens(n int) bool {
	if c == nil || n <= 0 {
		return false
	}
	window, known := c.contextLimit()
	if known && window >= subAgentDegeneratePromptWindow &&
		c.lastPromptTokens >= window &&
		n < subAgentDegeneratePromptTokens {
		c.forceCompact = true
		return false
	}
	c.lastPromptTokens = n
	c.forceCompact = known && n >= window
	return true
}

// promptFillsWindow reports that this prompt leaves no output room inside the
// known effective window, or that n is the degenerate tiny report that follows
// one. An unknown window does not fill: the fallback ceiling is not the model.
func (c *SubAgentCompactor) promptFillsWindow(promptTokens int) bool {
	window, known := c.contextLimit()
	if !known || promptTokens <= 0 {
		return false
	}
	if promptTokens >= window {
		return true
	}
	return window >= subAgentDegeneratePromptWindow &&
		c.lastPromptTokens >= window &&
		promptTokens < subAgentDegeneratePromptTokens
}

// absorbOverflow arms one compaction before the next send. It returns false
// when a previous pass already stalled, so the loop stops instead of appending
// a continuation onto a full prompt.
func (c *SubAgentCompactor) absorbOverflow(promptTokens int) bool {
	if c == nil || !c.promptFillsWindow(promptTokens) || c.stalled {
		return false
	}
	window, _ := c.contextLimit()
	if promptTokens >= window {
		c.lastPromptTokens = promptTokens
	}
	c.forceCompact = true
	return true
}

// codingPromptFill is the context-window meter value. A pathological tiny
// prompt after one that already filled the window does not replace it.
func codingPromptFill(prev, next, window int) int {
	if next <= 0 {
		return prev
	}
	if window >= subAgentDegeneratePromptWindow && prev >= window && next < subAgentDegeneratePromptTokens {
		return prev
	}
	return next
}

// Compact performs in-place compaction of the conversation.
// Preserves: system message + recent window + file/command anchors.
// Middle section is replaced with a static summary.
func (c *SubAgentCompactor) Compact(conversation []interface{}) []interface{} {
	if c == nil || len(conversation) < 4 {
		if c != nil {
			return c.finishCompact(conversation, conversation, 0)
		}
		return conversation // too short to compact
	}
	if agent.HandoffMemoCoding() && agent.ConversationHasPendingAsk(conversation) {
		return conversation
	}

	// Structure: [system, user(task), ...middle..., recent_window]
	// Keep: system (idx 0) + user task (idx 1) + recent window
	systemMsg := conversation[0]
	taskMsg := conversation[1]

	// Calculate recent window by counting assistant turns from the end.
	// Each "turn" is: assistant message + N tool result messages.
	// Flag off keeps the last 5 turns, then shrinks to 30% of the window.
	// Flag coding/all keeps the last 2, then shrinks to 15% (floor 1000).
	turns := subAgentCompactionRecencyWindow
	window := c.triggerWindow()
	keepTokens := window * 3 / 10
	if agent.HandoffMemoCoding() {
		turns = subAgentHandoffRecencyWindow
		keepTokens = window * 15 / 100
	}
	recentStart := findSubAgentRecencyWindowStart(conversation, turns)
	if recentStart < 2 {
		recentStart = 2
	}
	if keepTokens < 1000 {
		keepTokens = 1000
	}
	// Fit may already have dropped or truncated tool results. The shrink
	// decision has to see that original, not the fitted slice compared with
	// itself: a full window would otherwise stall and throw the shorter
	// prompt away.
	beforeFit := conversation
	conversation, recentStart = fitSubAgentRecentWindow(conversation, recentStart, keepTokens)
	if recentStart <= 2 {
		return c.finishCompact(beforeFit, conversation, 0)
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
	var mergedContent string
	if agent.HandoffMemoCoding() {
		memo := c.memoFromAnchors(taskContent, middleSection)
		if !memo.Usable() {
			agent.NoteHandoffKeptExtraGroup()
			moved := subAgentKeepOneExtraGroup(conversation, recentStart)
			if moved < recentStart {
				recentStart = moved
				recentWindow = conversation[recentStart:]
				if recentStart <= 2 {
					log.Printf("[coding-subagent-compaction] memo %s kept_extra", memo.LengthNote())
					return c.finishCompact(beforeFit, conversation, 0)
				}
				middleSection = conversation[2:recentStart]
				memo = c.memoFromAnchors(taskContent, middleSection)
			}
		}
		if memo.Usable() {
			agent.NoteHandoffFallback()
			mergedContent = agent.ReplaceHandoffBlock(taskContent, memo.Render())
			log.Printf("[coding-subagent-compaction] memo %s", memo.LengthNote())
		} else {
			agent.NoteHandoffFallback()
			mergedContent = taskContent
			log.Printf("[coding-subagent-compaction] memo %s fallback", memo.LengthNote())
		}
	} else {
		summary := c.buildCompactionSummary(middleSection, taskContent)
		mergedContent = replaceSubAgentCompactionBlock(taskContent, summary)
	}
	mergedTask := map[string]interface{}{
		"role":    "user",
		"content": mergedContent,
	}

	compacted := make([]interface{}, 0, 2+len(recentWindow))
	compacted = append(compacted, systemMsg)
	compacted = append(compacted, mergedTask)
	compacted = append(compacted, recentWindow...)
	return c.finishCompact(beforeFit, compacted, len(middleSection))
}

// finishCompact applies a shorter rewrite, and seals a rewrite that did not
// shrink so the next turn does not compact the same bytes again. An
// already-full provider prompt that still does not shrink stalls the loop.
func (c *SubAgentCompactor) finishCompact(before, after []interface{}, middle int) []interface{} {
	if c == nil {
		return after
	}
	if after == nil {
		after = before
	}
	beforeTokens := estimateConversationTokensForSubAgent(before)
	afterTokens := estimateConversationTokensForSubAgent(after)
	if afterTokens >= beforeTokens {
		seal := afterTokens
		if seal <= 0 {
			seal = 1
		}
		c.sealedEstimate = seal
		// Read pressure before clearing force. A full prompt stalls; an
		// estimate-only no-shrink only seals, so the task can still send.
		pressured := c.promptPressured()
		c.forceCompact = false
		if pressured {
			c.stalled = true
		}
		window, _ := c.contextLimit()
		log.Printf("[coding-subagent-compaction] sealed: %d -> %d tokens (window=%d prompt=%d)",
			beforeTokens, afterTokens, window, c.lastPromptTokens)
		return after
	}
	c.sealedEstimate = 0
	c.stalled = false
	c.forceCompact = false
	if afterTokens <= c.triggerThreshold() {
		c.lastPromptTokens = 0
	}
	c.compactionCount++
	log.Printf("[coding-subagent-compaction] triggering compaction #%d (conversation_len=%d)", c.compactionCount, len(before))
	log.Printf("[coding-subagent-compaction] compacted: %d -> %d entries (middle=%d summarized)",
		len(before), len(after), middle)
	return after
}

// replaceSubAgentCompactionBlock keeps a single flag-off summary. Appending a
// new block on every compact grows the task message once compaction runs for
// the whole task.
func replaceSubAgentCompactionBlock(taskContent, summary string) string {
	const sep = "\n\n---\n\n"
	marker := sep + subAgentCompactionMarker
	if idx := strings.Index(taskContent, marker); idx >= 0 {
		taskContent = taskContent[:idx]
	} else if idx := strings.Index(taskContent, subAgentCompactionMarker); idx >= 0 {
		taskContent = strings.TrimRight(taskContent[:idx], "\n- ")
	}
	taskContent = strings.TrimRight(taskContent, "\n")
	if strings.TrimSpace(taskContent) == "" {
		return summary
	}
	return taskContent + sep + summary
}

func (c *SubAgentCompactor) memoFromAnchors(taskContent string, middle []interface{}) agent.HandoffMemo {
	prior, ok := agent.ParseHandoffMemo(taskContent)
	if !ok {
		if progress := agent.RecoveryHandoffProgress(taskContent); progress != "" {
			prior.Progress = progress
		}
	}
	var users []string
	if text := strings.TrimSpace(agent.HandoffOutside(taskContent)); text != "" {
		users = append(users, text)
	}
	for _, msg := range middle {
		if subAgentMsgRole(msg) != "user" {
			continue
		}
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := m["content"].(string)
		if strings.TrimSpace(content) != "" {
			users = append(users, content)
		}
	}
	var commands []agent.HandoffCommand
	if c != nil && c.commandsRunFn != nil {
		for _, cmd := range c.commandsRunFn() {
			commands = append(commands, agent.HandoffCommand{
				Tool:      "bash",
				Path:      cmd.WorkingDir,
				Command:   cmd.Command,
				Succeeded: cmd.Succeeded,
				Outcome:   cmd.Summary,
			})
		}
	}
	memo := agent.FillHandoffMemo(agent.HandoffFill{
		UserTexts: users,
		Read:      callStringList(c.filesReadFn),
		Modified:  callStringList(c.filesModifiedFn),
		Created:   callStringList(c.filesCreatedFn),
		Commands:  commands,
		Prior:     prior,
	})
	agent.PreservePriorProgress(&memo, prior)
	return memo
}

func (c *SubAgentCompactor) collapsePerRound(conversation []interface{}) []interface{} {
	if c == nil || !agent.HandoffMemoPerRound() || len(conversation) < 4 {
		return nil
	}
	if codingPerRoundBlocked(conversation) {
		return conversation
	}
	hasTool := false
	for _, msg := range conversation[2:] {
		if subAgentMsgRole(msg) == "tool" {
			hasTool = true
			break
		}
	}
	if !hasTool {
		return nil
	}
	taskContent := ""
	if task, ok := conversation[1].(map[string]interface{}); ok {
		taskContent, _ = task["content"].(string)
	}
	memo := c.memoFromAnchors(taskContent, conversation[2:])
	if !memo.Usable() {
		agent.NoteHandoffKeptExtraGroup()
		return conversation
	}
	agent.NoteHandoffFallback()
	log.Printf("[coding-subagent-compaction] per-round memo %s", memo.LengthNote())
	return []interface{}{
		conversation[0],
		map[string]interface{}{
			"role":    "user",
			"content": agent.ReplaceHandoffBlock(taskContent, memo.Render()),
		},
	}
}

func codingPerRoundBlocked(conversation []interface{}) bool {
	if agent.ConversationHasPendingAsk(conversation) {
		return true
	}
	for _, msg := range conversation {
		if subAgentMsgRole(msg) != "user" {
			continue
		}
		m, ok := msg.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := m["content"].(string)
		if strings.HasPrefix(strings.TrimSpace(content), agent.SessionRoundPrefix) {
			return true
		}
	}
	return false
}

func subAgentKeepOneExtraGroup(conversation []interface{}, recentStart int) int {
	for i := recentStart - 1; i >= 2; i-- {
		if subAgentMsgRole(conversation[i]) == "assistant" {
			return i
		}
	}
	return recentStart
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

	b.WriteString(subAgentCompactionMarker + " 之前的工具调用因 context 长度限制被压缩。以下是工作摘要。\n\n")
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

// OnLLMUsage records the provider prompt size for the next compaction decision
// and for the context-window meter. The session total stays a sum; this path
// only publishes the latest prompt.
func (c *codingSubAgentCallbacks) OnLLMUsage(model string, inputTokens, outputTokens int) {
	if c == nil || c.compactor == nil {
		return
	}
	if !c.compactor.notePromptTokens(inputTokens) {
		return
	}
	if c.subagent == nil || c.subagent.handler == nil {
		return
	}
	c.subagent.handler.noteCodingContextFill(codingSubAgentOwnerUserID(c.subagent.loopCtx), inputTokens, outputTokens)
}

// PromptFillsContextWindow reports that promptTokens leaves no output room
// inside this attempt's effective window.
func (c *codingSubAgentCallbacks) PromptFillsContextWindow(promptTokens int) bool {
	if c == nil || c.compactor == nil {
		return false
	}
	return c.compactor.promptFillsWindow(promptTokens)
}

// AbsorbContextOverflow arms compaction before the next send.
func (c *codingSubAgentCallbacks) AbsorbContextOverflow(promptTokens int) bool {
	if c == nil || c.compactor == nil {
		return false
	}
	return c.compactor.absorbOverflow(promptTokens)
}

// ContextCompactStalled reports that compaction cannot shrink a full window.
func (c *codingSubAgentCallbacks) ContextCompactStalled() bool {
	return c != nil && c.compactor != nil && c.compactor.stalled
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
		next, _, _ := h.handler.appendPendingSteerInjections(h.userID, out, h.iteration)
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
	if h != nil && h.compactor != nil && agent.HandoffMemoPerRound() {
		if collapsed := h.compactor.collapsePerRound(out); collapsed != nil {
			// A per-round memo used to return before the occupancy compact.
			// A blocked or non-shrinking collapse then hid a prompt that
			// already filled the window, and the loop sent it again.
			if !h.compactor.promptPressured() {
				return collapsed
			}
			if estimateConversationTokensForSubAgent(collapsed) < estimateConversationTokensForSubAgent(out) {
				return h.compactor.finishCompact(out, collapsed, len(out)-len(collapsed))
			}
			out = collapsed
			changed = true
		}
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
	compactor := NewSubAgentCompactor(
		0,
		func() []string { return cb.getFilesModified() },
		func() []string { return cb.getFilesCreated() },
		func() []string { return cb.getFilesRead() },
		func() []CodingSubAgentCommandResult { return cb.getCommandsRun() },
	)
	// RouteTurn can replace cfg after these hooks are built. Read the window
	// at compaction time so the stall ceiling is the model that just ran.
	if s != nil {
		compactor.windowFn = func() int {
			return s.cfg.EffectiveContextTokens()
		}
	}
	if cb != nil {
		cb.compactor = compactor
	}

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
