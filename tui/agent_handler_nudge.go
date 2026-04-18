package main

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/nudge"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// ensureNudgeTracker returns the existing NudgeTracker or creates a new one
// if it hasn't been initialized yet (defensive lazy init).
func (h *TUIAgentHandler) ensureNudgeTracker() *nudge.NudgeTracker {
	if h.nudgeTracker == nil {
		h.nudgeTracker = nudge.NewNudgeTracker()
	}
	return h.nudgeTracker
}

// isNudgeDisabledTUI checks whether the nudge system is disabled via config.
// Returns true if nudges should be suppressed.
func (h *TUIAgentHandler) isNudgeDisabledTUI() bool {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.NudgeDisabled
}

// injectNudgeMessagesTUI checks nudge conditions and appends appropriate system
// messages to the conversation. Nudges are injected AFTER the current response
// is delivered — they go into the conversation for the NEXT LLM call, not the
// current one.
//
// Parameters:
//   - conversation: the conversation history to append nudge messages to
//   - iteration: the current agent loop iteration count
//   - totalToolCallsInLoop: total number of tool calls across all iterations
//   - failedSkillName: name of a skill that failed in this loop (for workaround nudge)
//   - userText: the current user message text (for correction detection)
//
// Returns the (possibly extended) conversation.
func (h *TUIAgentHandler) injectNudgeMessagesTUI(
	conversation []interface{},
	iteration int,
	totalToolCallsInLoop int,
	failedSkillName string,
	userText string,
) []interface{} {
	if h.isNudgeDisabledTUI() {
		return conversation
	}
	tracker := h.ensureNudgeTracker()

	// 1. Complex task nudge: ≥5 tool calls in this loop.
	if totalToolCallsInLoop >= 5 {
		event := nudge.NudgeEvent{
			Type:           nudge.ComplexTask,
			ToolCallCount:  totalToolCallsInLoop,
			IterationCount: iteration,
		}
		if tracker.ShouldNudge(event) {
			msg := nudge.NudgeMessage(event)
			if msg != "" {
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": msg,
				})
				tracker.RecordNudge(event)
				log.Printf("[nudge] injected ComplexTask nudge: toolCalls=%d iteration=%d", totalToolCallsInLoop, iteration)
			}
		}
	}

	// 2. Skill failure workaround nudge: skill failed + LLM resolved via alternative tools.
	if failedSkillName != "" {
		event := nudge.NudgeEvent{
			Type:           nudge.SkillFailureWorkaround,
			SkillName:      failedSkillName,
			IterationCount: iteration,
		}
		if tracker.ShouldNudge(event) {
			msg := nudge.NudgeMessage(event)
			if msg != "" {
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": msg,
				})
				tracker.RecordNudge(event)
				log.Printf("[nudge] injected SkillFailureWorkaround nudge: skill=%q iteration=%d", failedSkillName, iteration)
			}
		}
	}

	// 3. User correction nudge: user message following a failed tool call
	//    with correction keywords.
	if containsCorrectionKeywordsTUI(userText) && hasRecentFailedToolCallTUI(conversation) {
		event := nudge.NudgeEvent{
			Type:           nudge.UserCorrection,
			IterationCount: iteration,
		}
		if tracker.ShouldNudge(event) {
			msg := nudge.NudgeMessage(event)
			if msg != "" {
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": msg,
				})
				tracker.RecordNudge(event)
				log.Printf("[nudge] injected UserCorrection nudge: iteration=%d", iteration)
			}
		}
	}

	return conversation
}

// containsCorrectionKeywordsTUI checks if the user message contains any
// correction keywords that suggest the user is correcting the LLM's approach.
// Mirrors the GUI's containsCorrectionKeywords.
func containsCorrectionKeywordsTUI(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, kw := range tuiCorrectionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// tuiCorrectionKeywords are keywords that indicate the user is correcting
// the LLM's approach. Used for user correction nudge detection.
// Mirrors the GUI's userCorrectionKeywords.
var tuiCorrectionKeywords = []string{
	// English
	"instead", "not like that", "wrong", "incorrect", "no,", "actually",
	"should be", "use this", "try this", "do it this way", "that's wrong",
	"fix", "correct",
	// Chinese
	"不对", "错了", "应该", "不是这样", "换一种", "改成", "用这个", "试试这个",
	"这样做", "纠正", "修正",
}

// hasRecentFailedToolCallTUI checks if the conversation has a failed tool call
// in the recent entries (last 6 entries). A failed tool call is identified by
// error-like content in a tool result message.
// Mirrors the GUI's hasRecentFailedToolCall.
func hasRecentFailedToolCallTUI(conversation []interface{}) bool {
	// Scan the last 6 entries for a tool result that looks like a failure.
	start := len(conversation) - 6
	if start < 0 {
		start = 0
	}
	for i := start; i < len(conversation); i++ {
		entry, ok := conversation[i].(map[string]interface{})
		if !ok {
			// Also check map[string]string entries.
			if sEntry, ok2 := conversation[i].(map[string]string); ok2 {
				if sEntry["role"] != "tool" {
					continue
				}
				lower := strings.ToLower(sEntry["content"])
				if strings.Contains(lower, "error") || strings.Contains(lower, "failed") ||
					strings.Contains(lower, "失败") || strings.Contains(lower, "错误") {
					return true
				}
			}
			continue
		}
		role, _ := entry["role"].(string)
		if role != "tool" {
			continue
		}
		content, _ := entry["content"].(string)
		lower := strings.ToLower(content)
		if strings.Contains(lower, "error") || strings.Contains(lower, "failed") ||
			strings.Contains(lower, "失败") || strings.Contains(lower, "错误") {
			return true
		}
	}
	return false
}
