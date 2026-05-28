package main

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

type agentLoopRecoverPromptResult struct {
	Conversation            []interface{}
	Tools                   []map[string]interface{}
	ToolsTokenBudget        int
	DirectModeToolsFiltered bool
	Applied                 bool
}

func (h *IMMessageHandler) applyAgentLoopRecoverPrompt(
	ctx *LoopContext,
	userID string,
	phase *agentLoopPhase,
	conversation []interface{},
	tools []map[string]interface{},
	toolsTokenBudget int,
	baseTools []map[string]interface{},
	gateConfig codingToolGateConfig,
	skipCodingGate bool,
	orchestratorActive func() bool,
) agentLoopRecoverPromptResult {
	result := agentLoopRecoverPromptResult{
		Conversation:            conversation,
		Tools:                   tools,
		ToolsTokenBudget:        toolsTokenBudget,
		DirectModeToolsFiltered: false,
	}
	if phase == nil || phase.Stage != agentStageRecover || strings.TrimSpace(phase.RecoverPrompt) == "" {
		return result
	}
	result.Applied = true

	result.Conversation = append(result.Conversation, map[string]string{
		"role":    "system",
		"content": phase.RecoverPrompt,
	})
	if phase.RecoverReason == agentRecoverSkillFailed {
		phase.ForceSkillPreference = false
		phase.SkillMode = skillPreferenceFallbackAllowed
		phase.RemoteSearchExhausted = true
		result.Tools, result.ToolsTokenBudget, result.DirectModeToolsFiltered = h.restoreToolsAfterSkillRecover(userID, baseTools, *phase, gateConfig, skipCodingGate, orchestratorActive)
	}
	recoverReason := firstNonEmptyTraceText(phase.RecoverReason.String(), "recover")
	if h.traceService != nil && ctx != nil && ctx.RunID != "" {
		h.appendTraceEvent(ctx, "loop.recover_entered", "warn", "Entered Recover stage", truncateTraceText(recoverReason, 220), "", "")
	}
	phase.RecoverPrompt = ""
	phase.RecoverReason = agentRecoverNone
	phase.Stage = agentStageConverge
	return result
}

func buildDriftRecoverPrompt(drift DriftResult) string {
	detail := strings.TrimSpace(drift.ReplanPrompt)
	if detail == "" {
		detail = "Execution drift or repetition was detected. Stop repeating the same approach, return to the original goal, and continue with a different path."
	}
	toolWarning := ""
	if drift.DriftedTool != "" {
		toolWarning = fmt.Sprintf("\nDo not call %s again after repeated failures. If no alternative exists, explain the limitation to the user.", drift.DriftedTool)
	}
	return "[Recover 阶段]\n检测到执行路径出现漂移或循环，当前进入 Recover 阶段。请先暂停重复操作，回到原始目标，基于已知结果改用不同路径继续完成任务。\n" + detail + toolWarning + "\n[/Recover 阶段]"
}

// truncateRunesForDrift truncates a string to maxRunes for use as a drift
// detector result hint. Prefers cutting at a newline boundary.
func truncateRunesForDrift(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	truncated := string(runes[:maxRunes])
	// Try to cut at last newline for readability, but keep at least half.
	if idx := strings.LastIndex(truncated, "\n"); idx > len(truncated)/2 {
		truncated = truncated[:idx]
	}
	return truncated + "…"
}

func buildDeliverableRecoverPrompt(skillName string, preferSkill bool, runID string) string {
	skillName = strings.TrimSpace(skillName)
	runID = strings.TrimSpace(runID)
	if runID != "" {
		return "[Recover]\nA deliverable was promised but not produced. " + buildSkillProgressGuidance(skillName, runID) + " If completion is still impossible, explain the failure reason and visible result.\n[/Recover]"
	}
	if preferSkill && skillName != "" {
		return fmt.Sprintf("[Recover]\nA deliverable was promised but not produced. Call manage_skill(action=\"run\", name=%q) to complete delivery, or explain the failure if no path remains.\n[/Recover]", skillName)
	}
	return "[Recover]\nA deliverable was promised but not produced. Use a real tool to complete delivery immediately, or explain the failure reason and visible result.\n[/Recover]"
}

// buildEmptyResultRecoverPromptWithTasks builds the empty-response Recover
// prompt. When pendingTaskHint is non-empty it is appended to guide the LLM
// toward checking active background tasks instead of stalling.
func buildEmptyResultRecoverPromptWithTasks(pendingTaskHint string) string {
	base := "[Recover]\nThe previous round returned no visible result. Either provide a visible result or clearly explain the failure reason and current status. If more execution is needed, call a real tool immediately."
	if pendingTaskHint != "" {
		base += "\n" + pendingTaskHint
	}
	base += "\n[/Recover]"
	return base
}

func buildPendingBackgroundTaskRecoverPrompt(pendingTaskHint string) string {
	base := "[Recover]\nA background task that was started during this loop is still active. Do not finalize with a wait/promise-only message. Call the appropriate status tool now and report concrete progress. For SSH tasks that should finish soon, use ssh(action=\"wait_task\", task_id=..., timeout=60)."
	if strings.TrimSpace(pendingTaskHint) != "" {
		base += "\n" + pendingTaskHint
	}
	base += "\n[/Recover]"
	return base
}

// pruneStaleNoToolTurns removes the most recent consecutive no-tool-call
// assistant messages and any system messages injected between them (recover
// prompts, no-tool nudges) from the conversation. This prevents the positive
// feedback loop where stale turns inflate context, push useful history out
// of the token window, and cause the LLM to produce even more stale turns.
//
// The function scans backwards from the end of the conversation, removing
// assistant messages that have no tool_calls and system messages that look
// like recover/nudge injections. It stops at the first message that is:
// - a user message
// - an assistant message with tool_calls
// - a tool result message
// - the system prompt (index 0)
//
// This ensures the LLM sees: original context + user request + one fresh
// recover prompt, instead of: original context + N failed attempts + N
// recover prompts.
func pruneStaleNoToolTurns(conversation []interface{}) []interface{} {
	if len(conversation) <= 2 {
		return conversation
	}

	// Scan backwards to find the cut point.
	cutFrom := len(conversation)
loop:
	for i := len(conversation) - 1; i > 0; i-- {
		role := msgRole(conversation[i])
		switch role {
		case "assistant":
			if msgHasToolCalls(conversation[i]) {
				break loop // productive turn 鈥?stop
			}
			cutFrom = i // stale turn 鈥?mark for removal
		case "system":
			if isRecoverOrNudgeSystemMessage(msgContent(conversation[i])) {
				cutFrom = i // recover/nudge injection 鈥?mark for removal
			} else {
				break loop // non-recover system message 鈥?stop
			}
		default:
			break loop // user, tool, or other 鈥?stop
		}
	}

	if cutFrom >= len(conversation) {
		return conversation // nothing to prune
	}

	pruned := len(conversation) - cutFrom
	if pruned > 0 {
		log.Printf("[prune-stale-turns] removed %d stale no-tool-call messages from conversation (len %d 鈫?%d)",
			pruned, len(conversation), cutFrom)
	}
	return conversation[:cutFrom]
}

// msgContent extracts the "content" string from a conversation message
// regardless of whether it's map[string]string or map[string]interface{}.
func msgContent(m interface{}) string {
	switch v := m.(type) {
	case map[string]interface{}:
		s, _ := v["content"].(string)
		return s
	case map[string]string:
		return v["content"]
	}
	return ""
}

// isRecoverOrNudgeSystemMessage checks if a system message content looks like
// a recover prompt or no-tool nudge injection. These are the messages that
// accumulate during no-tool-stall loops and should be pruned.
func isRecoverOrNudgeSystemMessage(content string) bool {
	if content == "" {
		return false
	}
	recoverMarkers := []string{
		"[Recover", "[/Recover",
	}
	for _, marker := range recoverMarkers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	noToolMarkers := []string{
		"[执行要求]", "[/执行要求]",
		"[鎵ц瑕佹眰]", "[/鎵ц瑕佹眰]",
		"[閹笛嗩攽鐟曠焦鐪癩", "[/閹笛嗩攽鐟曠焦鐪癩",
	}
	for _, marker := range noToolMarkers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	// Goal anchor and progress tracker injections from previous iterations
	// are not pruned; they carry useful context.
	return false
}

// pendingBackgroundTaskHint checks all runtime task managers for running
// tasks that were submitted after loopStart and returns a hint string for
// the Recover prompt. The loopStart filter prevents stale tasks from a
// previous conversation from misleading the current loop.
// Returns "" if no relevant tasks are active.
//
// Delegates to collectRuntimeStatus() (single enumeration point) +
// pendingBackgroundTaskHintFromStatus() (formatting). The extra session/
// main-agent data collected by collectRuntimeStatus() is unused here but
// the cost is negligible (List() calls on empty or small slices).
func (h *IMMessageHandler) pendingBackgroundTaskHint(loopStart time.Time) string {
	return pendingBackgroundTaskHintFromStatus(h.collectRuntimeStatus(), loopStart)
}

func (h *IMMessageHandler) pendingBackgroundTaskBoundaryKey(ctx *LoopContext) string {
	if h == nil || ctx == nil {
		return ""
	}
	return pendingBackgroundTaskKeyFromStatus(h.collectRuntimeStatus(), ctx.StartedAt)
}

// cancelledExitResponse saves accumulated history and returns a clean
// cancellation message. This is the single exit point for all cancellation
// paths inside runAgentLoop, structurally enforcing the invariant that the
// loop always saves (never clears) history.
func (h *IMMessageHandler) cancelledExitResponse(userID string, history []agent.ConversationEntry, userText string) *IMAgentResponse {
	// Cancel can interrupt the tool execution loop after the assistant
	// message (with tool_calls) was recorded but before all tool results
	// were added. This leaves a broken pair in history that would cause
	// HTTP 400 on strict APIs (DeepSeek). Fix at the point of creation:
	// find the last assistant(tool_calls), strip its ToolCalls, and remove
	// any partial tool results that follow it.
	history = stripTrailingBrokenToolGroup(history)
	h.saveConversationHistoryTimed(userID, history, nil)
	cancelMsg := "⏹️ Task cancelled."
	if taskPreview := truncateRunes(userText, 30); taskPreview != "" {
		cancelMsg = fmt.Sprintf("⏹️ Task cancelled: %s", taskPreview)
	}
	return &IMAgentResponse{Text: cancelMsg}
}

// llmErrorExitResponse saves accumulated history and returns an LLM error
// message. This is the single exit point for all LLM error paths inside
// runAgentLoop, structurally enforcing the invariant that the loop always
// saves (never clears) history. Mirrors cancelledExitResponse for cancel
// paths 鈥?see #54 and #55 for the design rationale.
//
// The error message includes a task context hint extracted from the last
// user message in history, so the user knows what was being worked on and
// that they can resume by sending another message.
func (h *IMMessageHandler) llmErrorExitResponse(userID string, history []agent.ConversationEntry, errorMsg string) *IMAgentResponse {
	history = stripTrailingBrokenToolGroup(history)
	h.saveConversationHistoryTimed(userID, history, nil)

	// Extract last user message as task context hint.
	taskHint := ""
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			if s, ok := history[i].Content.(string); ok && strings.TrimSpace(s) != "" {
				taskHint = truncateRunes(strings.TrimSpace(s), 80)
				break
			}
		}
	}
	if taskHint != "" {
		errorMsg += fmt.Sprintf("\n\nPrevious task: %s\nSend another message to continue.", taskHint)
	}

	return &IMAgentResponse{Error: errorMsg}
}

// stripTrailingBrokenToolGroup checks if the history ends with an incomplete
// tool_calls group (assistant with tool_calls + fewer tool results than
// tool_call IDs). If so, strips ToolCalls from the assistant and removes
// the partial tool results. This is only needed for cancel/error exit paths
// where the agent loop was interrupted mid-execution.
func stripTrailingBrokenToolGroup(history []agent.ConversationEntry) []agent.ConversationEntry {
	if len(history) == 0 {
		return history
	}
	// Find the last assistant(tool_calls) by scanning backwards.
	assistantIdx := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" && history[i].ToolCalls != nil {
			assistantIdx = i
			break
		}
		// Stop scanning if we hit a user message 鈥?the broken group must
		// be at the tail of the conversation.
		if history[i].Role == "user" {
			break
		}
	}
	if assistantIdx < 0 {
		return history
	}
	// Check if all tool results are present after this assistant.
	// Count tool entries immediately following the assistant.
	toolCount := 0
	for j := assistantIdx + 1; j < len(history); j++ {
		if history[j].Role != "tool" {
			break
		}
		toolCount++
	}
	// Count expected tool_call IDs.
	expectedCount := 0
	if data, err := json.Marshal(history[assistantIdx].ToolCalls); err == nil {
		var arr []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(data, &arr) == nil {
			expectedCount = len(arr)
		}
	}
	if expectedCount > 0 && toolCount >= expectedCount {
		return history // group is complete, nothing to fix
	}
	// Incomplete group 鈥?strip ToolCalls and remove partial tool results.
	// Copy the entry before mutating to avoid corrupting the caller's slice.
	patched := history[assistantIdx]
	patched.ToolCalls = nil
	history[assistantIdx] = patched
	// Remove tool entries after the assistant.
	cutEnd := assistantIdx + 1
	for cutEnd < len(history) && history[cutEnd].Role == "tool" {
		cutEnd++
	}
	if cutEnd > assistantIdx+1 {
		history = append(history[:assistantIdx+1], history[cutEnd:]...)
	}
	return history
}

// findLastAssistantContent scans conversation history backwards and returns
// the content of the last non-empty assistant message. This is used as a
// fallback when the loop must hard-exit due to consecutive empty responses.
func findLastAssistantContent(history []agent.ConversationEntry) string {
	for i := len(history) - 1; i >= 0; i-- {
		entry := history[i]
		if entry.Role == "assistant" {
			var content string
			switch v := entry.Content.(type) {
			case string:
				content = v
			default:
				continue
			}
			content = strings.TrimSpace(content)
			if content != "" && len([]rune(content)) > 10 {
				return content
			}
		}
	}
	return ""
}

// incrementCompactionCount increments and returns the compaction count for
// the given user. Safe for concurrent use (sync.Map), though in practice
// saveConversationHistoryTimed is serialized per user by chatLoopMu.
func (h *IMMessageHandler) incrementCompactionCount(userID string) int {
	val, _ := h.compactionCount.LoadOrStore(userID, 0)
	newCount := val.(int) + 1
	h.compactionCount.Store(userID, newCount)
	return newCount
}

// resetCompactionTokenCalibration signals that the token calibration data
// from the previous LLM call is stale after compaction. The actual reset
// happens in the agent loop via lastLLMInputTokens 鈥?this is a no-op
// placeholder that documents the intent. The agent loop's local variable
// lastLLMInputTokens is naturally reset when the loop re-enters after
// saveConversationHistoryTimed returns.
func (h *IMMessageHandler) resetCompactionTokenCalibration(_ string) {
	// The calibration state (lastLLMInputTokens, lastLLMOutputTokens) is
	// local to runAgentLoop. After compaction, the next loop iteration will
	// use the stale values for one calibration cycle, then self-correct.
	// This is acceptable because the calibration ratio check (>1.15) has
	// enough margin to absorb one stale cycle.
	//
	// A more aggressive approach would be to store the calibration state
	// in a sync.Map and reset it here, but the current design keeps the
	// calibration state loop-local for simplicity.
}

func buildTrialFailureRecoverPrompt(observation string, repeatedFailures []string) string {
	var b strings.Builder
	b.WriteString("[Recover]\nThe previous real tool attempt failed. Adjust the plan based on the failure and do not repeat the same failed attempt.")
	if obs := strings.TrimSpace(observation); obs != "" {
		b.WriteString("\nFailure observation: ")
		b.WriteString(obs)
	}
	if len(repeatedFailures) > 0 {
		items := append([]string(nil), repeatedFailures...)
		sort.Strings(items)
		b.WriteString("\nAvoid repeating: ")
		b.WriteString(strings.Join(items, ", "))
	}
	b.WriteString("\nNext step: use a different path or corrected parameters. If completion is still impossible, explain the blocker and current state.\n[/Recover]")
	return b.String()
}

func buildRemoteSkillSearchPrompt() string {
	return "[Execution requirement]\nThis task should prefer a reusable Skill, but no suitable local Skill was found. Search/install a reusable Skill first. Only switch to craft_tool or bash after the remote Skill path is unavailable.\n[/Execution requirement]"
}
