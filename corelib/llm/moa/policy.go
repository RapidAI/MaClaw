package moa

import "github.com/RapidAI/CodeClaw/corelib"

// FanOutDecision is the result of ShouldFanOut.
type FanOutDecision struct {
	Allow  bool
	Reason string
}

// ShouldFanOut applies K11 defaults: fanout_max_iterations (default 1) and
// only_before_first_tool (default true).
func ShouldFanOut(cfg corelib.MoAConfig, preset corelib.MoAPresetConfig, iteration, fanoutsRan int, toolsSeen bool) FanOutDecision {
	if !preset.Enabled {
		return FanOutDecision{Allow: false, Reason: "preset_disabled"}
	}
	max := cfg.EffectiveFanoutMaxIterations()
	if preset.FanoutMaxIterations > 0 {
		max = preset.FanoutMaxIterations
	}
	if fanoutsRan >= max {
		return FanOutDecision{Allow: false, Reason: "fanout_budget"}
	}
	onlyBefore := cfg.EffectiveOnlyBeforeFirstTool()
	if preset.OnlyBeforeFirstTool != nil {
		onlyBefore = *preset.OnlyBeforeFirstTool
	}
	if onlyBefore && toolsSeen {
		return FanOutDecision{Allow: false, Reason: "only_before_first_tool"}
	}
	// iteration is informational; budget is on fanoutsRan.
	_ = iteration
	return FanOutDecision{Allow: true, Reason: "ok"}
}

// ConversationHasToolResults reports whether durable-style messages include tool results.
func ConversationHasToolResults(messages []interface{}) bool {
	for _, m := range messages {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		if role == "tool" {
			return true
		}
		// OpenAI tool_calls on assistant
		if role == "assistant" {
			if _, has := mm["tool_calls"]; has {
				return true
			}
		}
	}
	return false
}
