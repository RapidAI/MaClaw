// Package nudge provides a post-use skill nudge system that suggests creating
// or improving skills after complex tasks, failed skill executions, or user
// corrections. The nudge system injects low-priority system messages to
// encourage organic growth of the skill library.
package nudge

import (
	"fmt"
	"sync"
	"time"
)

// NudgeEvent represents a trigger event for the nudge system.
type NudgeEvent struct {
	// Type identifies the kind of nudge event.
	// Valid values: "complex_task", "skill_failure_workaround", "user_correction".
	Type string

	// SkillName is the name of the skill involved (used for skill_failure_workaround events).
	SkillName string

	// ErrorClass is the unified error classification from corelib/skill.ClassifyStepError
	// (used for skill_failure_workaround events to provide actionable repair hints).
	ErrorClass string

	// SelfRepairAttempted indicates whether the self-repair system already
	// attempted to fix this skill. When true, the nudge message changes from
	// "consider patching" to "self-repair was attempted but failed".
	SelfRepairAttempted bool

	// ToolCallCount is the number of tool calls in the agent loop (used for complex_task events).
	ToolCallCount int

	// IterationCount is the current agent loop iteration count.
	IterationCount int
}

// Nudge event type constants.
const (
	ComplexTask             = "complex_task"
	SkillFailureWorkaround  = "skill_failure_workaround"
	UserCorrection          = "user_correction"
)

// cooldownDuration is the per-session cooldown between nudge injections.
const cooldownDuration = 10 * time.Minute

// complexTaskThreshold is the minimum number of tool calls to trigger a complex task nudge.
const complexTaskThreshold = 5

// iterationThreshold is the minimum iteration count before nudges are allowed.
const iterationThreshold = 3

// NudgeTracker manages nudge state for a single session, including cooldown
// timing, deduplication, and iteration thresholds.
type NudgeTracker struct {
	mu            sync.Mutex
	lastNudgeTime time.Time
	dedupSet      map[string]bool
}

// NewNudgeTracker creates a new NudgeTracker for a session.
func NewNudgeTracker() *NudgeTracker {
	return &NudgeTracker{
		dedupSet: make(map[string]bool),
	}
}

// dedupKey returns a unique key for deduplication based on the event.
func dedupKey(event NudgeEvent) string {
	switch event.Type {
	case ComplexTask:
		return "complex_task"
	case SkillFailureWorkaround:
		return fmt.Sprintf("skill_failure_workaround:%s", event.SkillName)
	case UserCorrection:
		return "user_correction"
	default:
		return event.Type
	}
}

// ShouldNudge checks whether a nudge should be injected for the given event.
// It returns false if:
//   - the iteration count is below the threshold (3),
//   - the session cooldown has not elapsed (10 minutes),
//   - the same trigger event has already fired in this session (dedup).
//
// For ComplexTask events, it also checks that ToolCallCount >= 5.
func (t *NudgeTracker) ShouldNudge(event NudgeEvent) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Suppress nudges below iteration threshold.
	if event.IterationCount < iterationThreshold {
		return false
	}

	// For complex task events, require at least 5 tool calls.
	if event.Type == ComplexTask && event.ToolCallCount < complexTaskThreshold {
		return false
	}

	// Check session cooldown.
	if !t.lastNudgeTime.IsZero() && time.Since(t.lastNudgeTime) < cooldownDuration {
		return false
	}

	// Check dedup set.
	key := dedupKey(event)
	if t.dedupSet[key] {
		return false
	}

	return true
}

// RecordNudge records that a nudge was injected for the given event.
// It updates the cooldown timer and adds the event to the dedup set.
func (t *NudgeTracker) RecordNudge(event NudgeEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.lastNudgeTime = time.Now()
	key := dedupKey(event)
	t.dedupSet[key] = true
}

// NudgeMessage returns the appropriate system message text for the given event type.
// Messages are in Chinese (matching the target user base) and include actionable
// information when available (error class, self-repair status).
func NudgeMessage(event NudgeEvent) string {
	switch event.Type {
	case ComplexTask:
		return "刚才的任务比较复杂（多步工具调用）。可以考虑将这个方法保存为 Skill，下次遇到类似任务时直接复用。"
	case SkillFailureWorkaround:
		if event.SelfRepairAttempted {
			return fmt.Sprintf(
				"Skill「%s」执行失败，系统已尝试自动修复但未成功。可以用 manage_skill(action=patch, skill_name=\"%s\") 手动修补。",
				event.SkillName, event.SkillName,
			)
		}
		if event.ErrorClass != "" {
			return fmt.Sprintf(
				"Skill「%s」执行失败（错误类型: %s）。可以用 manage_skill(action=patch, skill_name=\"%s\") 修补它。",
				event.SkillName, event.ErrorClass, event.SkillName,
			)
		}
		return fmt.Sprintf(
			"Skill「%s」未能覆盖这个场景。可以用 manage_skill(action=patch, skill_name=\"%s\") 修补它。",
			event.SkillName, event.SkillName,
		)
	case UserCorrection:
		return "用户纠正了你的方法。可以考虑将正确的方法保存为记忆条目或 Skill，避免下次重复犯错。"
	default:
		return ""
	}
}

// IsDisabled checks whether the nudge system is disabled via configuration.
// It looks for a "nudge_disabled" key in the config map. If the key is present
// and truthy (bool true, or string "true"/"1"), nudges are disabled.
func IsDisabled(config map[string]interface{}) bool {
	if config == nil {
		return false
	}
	val, ok := config["nudge_disabled"]
	if !ok {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}
