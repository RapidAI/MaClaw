package moa

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ShouldActivateAuto implements K13:
//
//	AllowAuto && (tier==c3 || task==TaskReasoning) && task ∉ {fast,intent,summary}
//
// Pure function — no I/O. Used for automatic MoA on hard turns when user
// enabled allow_auto in settings (still requires EffectiveEnabled).
func ShouldActivateAuto(allowAuto bool, task llm.TaskType, costTier string) bool {
	if !allowAuto {
		return false
	}
	t := llm.TaskType(strings.ToLower(strings.TrimSpace(string(task))))
	switch t {
	case llm.TaskFast, llm.TaskIntent, llm.TaskSummary:
		return false
	}
	tier := strings.ToLower(strings.TrimSpace(costTier))
	if tier == "c3" {
		return true
	}
	if t == llm.TaskReasoning {
		return true
	}
	return false
}
