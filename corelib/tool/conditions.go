package tool

// ToolConditions holds the four tool availability condition fields from a skill.
// This struct avoids importing the corelib package (which would create an import cycle).
type ToolConditions struct {
	RequiresTools       []string
	FallbackForTools    []string
	RequiresToolsets    []string
	FallbackForToolsets []string
}

// EvaluateToolConditions checks whether a skill's tool availability conditions
// are satisfied given the current set of available tools.
//
// The function implements AND logic across all four condition fields:
//   - requires_tools: every listed tool must be available
//   - fallback_for_tools: every listed tool must be unavailable
//   - requires_toolsets: every toolset must be fully available (all constituent tools present)
//   - fallback_for_toolsets: every toolset must be unavailable (at least one constituent tool missing)
//
// Skills with no conditions (all four slices empty) return true for backward compatibility.
func EvaluateToolConditions(cond ToolConditions, availableTools map[string]bool) bool {
	// No conditions → backward compatible, always active.
	if len(cond.RequiresTools) == 0 &&
		len(cond.FallbackForTools) == 0 &&
		len(cond.RequiresToolsets) == 0 &&
		len(cond.FallbackForToolsets) == 0 {
		return true
	}

	// Check requires_tools: all must be available.
	for _, t := range cond.RequiresTools {
		if !availableTools[t] {
			return false
		}
	}

	// Check fallback_for_tools: all must be unavailable.
	for _, t := range cond.FallbackForTools {
		if availableTools[t] {
			return false
		}
	}

	// Check requires_toolsets: expand each toolset, all constituent tools must be available.
	for _, ts := range cond.RequiresToolsets {
		tools := ExpandToolset(ts)
		if tools == nil {
			// Unknown toolset is treated as unavailable.
			return false
		}
		for _, t := range tools {
			if !availableTools[t] {
				return false
			}
		}
	}

	// Check fallback_for_toolsets: expand each toolset, all constituent tools must be unavailable
	// (i.e., at least one tool in the toolset is missing for the fallback to activate).
	for _, ts := range cond.FallbackForToolsets {
		tools := ExpandToolset(ts)
		if tools == nil {
			// Unknown toolset → treat as unavailable, so fallback condition is satisfied.
			continue
		}
		// The toolset must NOT be fully available. If all tools are present, the
		// primary toolset is available and this fallback skill should not activate.
		allPresent := true
		for _, t := range tools {
			if !availableTools[t] {
				allPresent = false
				break
			}
		}
		if allPresent {
			return false
		}
	}

	return true
}

// EvaluateToolConditionsForSkill is a convenience wrapper that accepts the four
// condition slices directly from an NLSkillEntry, avoiding the need to construct
// a ToolConditions struct at the call site.
func EvaluateToolConditionsForSkill(
	requiresTools, fallbackForTools, requiresToolsets, fallbackForToolsets []string,
	availableTools map[string]bool,
) bool {
	return EvaluateToolConditions(ToolConditions{
		RequiresTools:       requiresTools,
		FallbackForTools:    fallbackForTools,
		RequiresToolsets:    requiresToolsets,
		FallbackForToolsets: fallbackForToolsets,
	}, availableTools)
}
