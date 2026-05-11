package main

import (
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/nudge"
)

// ensureNudgeTracker returns the existing NudgeTracker or creates a new one
// if it hasn't been initialized yet (defensive lazy init).
func (h *IMMessageHandler) ensureNudgeTracker() *nudge.NudgeTracker {
	if h.nudgeTracker == nil {
		h.nudgeTracker = nudge.NewNudgeTracker()
	}
	return h.nudgeTracker
}

// isNudgeDisabled checks whether the nudge system is disabled via config.
// Returns true if nudges should be suppressed.
func (h *IMMessageHandler) isNudgeDisabled() bool {
	if h.app == nil {
		return false
	}
	cfg, err := h.loadConfig()
	if err != nil {
		return false
	}
	return cfg.NudgeDisabled
}

// userCorrectionKeywords are keywords that indicate the user is correcting
// the LLM's approach. Used for user correction nudge detection.
var userCorrectionKeywords = []string{
	// English
	"instead", "not like that", "wrong", "incorrect", "no,", "actually",
	"should be", "use this", "try this", "do it this way", "that's wrong",
	"fix", "correct",
	// Chinese
	"涓嶅", "閿欎簡", "搴旇", "涓嶆槸杩欐牱", "鏀规垚", "璇曡瘯杩欎釜",
	"绾犳", "淇",
}

// containsCorrectionKeywords checks if the user message contains any
// correction keywords that suggest the user is correcting the LLM's approach.
func containsCorrectionKeywords(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}

	for _, kw := range userCorrectionKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// hasRecentFailedToolCall checks if the conversation history has a failed
// tool call in the recent entries (last 6 entries). Failure is determined
// from structured tool outcome metadata, not by inspecting result text.
func hasRecentFailedToolCall(history []agent.ConversationEntry) bool {
	start := len(history) - 6
	if start < 0 {
		start = 0
	}
	for i := start; i < len(history); i++ {
		entry := history[i]
		if entry.Role != "tool" {
			continue
		}
		if normalizeToolOutcome(entry.ToolOutcome) == toolOutcomeFailed {
			return true
		}
	}
	return false
}

// wasSkillRecentlyRepaired checks if a skill was auto-repaired within the
// last 5 minutes by examining its persisted RepairHistory. This uses
// persisted data (not the one-shot ConsumeRepairNotifications) to avoid
// competing with appendSkillRepairNotifications for the same data.
func (h *IMMessageHandler) wasSkillRecentlyRepaired(skillName string) bool {
	if h.getSkillExecutor() == nil {
		return false
	}
	for _, s := range h.getSkillExecutor().loadSkills() {
		if s.Name == skillName && s.RepairAttemptCount > 0 && s.LastRepairAt != "" {
			if t, err := time.Parse(time.RFC3339, s.LastRepairAt); err == nil {
				return time.Since(t) < 5*time.Minute
			}
		}
	}
	return false
}

// injectNudgeMessages checks nudge conditions and appends appropriate system
// messages to the conversation history. Nudges are injected AFTER the current
// response is delivered 鈥?they go into the conversation history for the NEXT
// LLM call, not the current one.
//
// Parameters:
//   - history: the conversation history to append nudge messages to
//   - iteration: the current agent loop iteration count
//   - totalToolCallsInLoop: total number of tool calls across all iterations
//   - phase: the current agent loop phase (for workaround detection)
//   - userText: the current user message text (for correction detection)
//
// Returns the (possibly extended) history.
func (h *IMMessageHandler) injectNudgeMessages(
	history []agent.ConversationEntry,
	iteration int,
	totalToolCallsInLoop int,
	phase agentLoopPhase,
	userText string,
) []agent.ConversationEntry {
	if h.isNudgeDisabled() {
		return history
	}
	tracker := h.ensureNudgeTracker()

	// 1. Complex task nudge: 鈮? tool calls in this loop.
	if totalToolCallsInLoop >= 5 {
		event := nudge.NudgeEvent{
			Type:           nudge.ComplexTask,
			ToolCallCount:  totalToolCallsInLoop,
			IterationCount: iteration,
		}
		if tracker.ShouldNudge(event) {
			msg := nudge.NudgeMessage(event)
			if msg != "" {
				history = append(history, agent.ConversationEntry{
					Role:    "system",
					Content: msg,
				})
				tracker.RecordNudge(event)
				log.Printf("[nudge] injected ComplexTask nudge: toolCalls=%d iteration=%d", totalToolCallsInLoop, iteration)
			}
		}
	}

	// 2. Skill failure workaround nudge: skill failed + LLM resolved via alternative tools.
	//    Coordinates with self-repair: checks the skill's persisted repair history
	//    to determine if self-repair was recently attempted (within 5 minutes).
	//    This avoids competing with appendSkillRepairNotifications for the
	//    one-shot ConsumeRepairNotifications data.
	if phase.FailedSkillName != "" {
		selfRepairAttempted := h.wasSkillRecentlyRepaired(phase.FailedSkillName)
		event := nudge.NudgeEvent{
			Type:                nudge.SkillFailureWorkaround,
			SkillName:           phase.FailedSkillName,
			SelfRepairAttempted: selfRepairAttempted,
			IterationCount:      iteration,
		}
		if tracker.ShouldNudge(event) {
			msg := nudge.NudgeMessage(event)
			if msg != "" {
				history = append(history, agent.ConversationEntry{
					Role:    "system",
					Content: msg,
				})
				tracker.RecordNudge(event)
				log.Printf("[nudge] injected SkillFailureWorkaround nudge: skill=%q selfRepairAttempted=%v iteration=%d",
					phase.FailedSkillName, selfRepairAttempted, iteration)
			}
		}
	}

	// 3. User correction nudge: user message following a failed tool call
	//    with correction keywords.
	if containsCorrectionKeywords(userText) && hasRecentFailedToolCall(history) {
		event := nudge.NudgeEvent{
			Type:           nudge.UserCorrection,
			IterationCount: iteration,
		}
		if tracker.ShouldNudge(event) {
			msg := nudge.NudgeMessage(event)
			if msg != "" {
				history = append(history, agent.ConversationEntry{
					Role:    "system",
					Content: msg,
				})
				tracker.RecordNudge(event)
				log.Printf("[nudge] injected UserCorrection nudge: iteration=%d", iteration)
			}
		}
	}

	return history
}
