package tool

// ToolsetGroups maps toolset names to their constituent tool names.
// Used by skill conditional activation to evaluate requires_toolsets and
// fallback_for_toolsets conditions.
var ToolsetGroups = map[string][]string{
	"coding": {
		"list_sessions",
		"get_session_output", "send_input", "interrupt_session",
		"kill_session", "get_session_events",
	},
	"browser": allBrowserToolNames,
	"ssh":     {"ssh"},
}

// ExpandToolset returns the list of tool names for a given toolset name.
// Returns nil if the toolset is not defined.
func ExpandToolset(toolset string) []string {
	return ToolsetGroups[toolset]
}

// IsToolsetAvailable checks if all tools in the named toolset are present
// in the availableTools set.
func IsToolsetAvailable(toolset string, availableTools map[string]bool) bool {
	tools := ToolsetGroups[toolset]
	if tools == nil {
		return false
	}
	for _, t := range tools {
		if !availableTools[t] {
			return false
		}
	}
	return true
}
