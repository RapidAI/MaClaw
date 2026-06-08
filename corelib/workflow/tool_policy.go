package workflow

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// IsToolAllowedByPolicy returns whether a tool may be exposed or executed for
// a workflow tool policy. Restrictive policies are deny-by-default.
func IsToolAllowedByPolicy(policy ToolFilterPolicy, name string) bool {
	name = strings.TrimSpace(name)
	switch policy {
	case ToolFilterDocOnly:
		return DocOnlyAllowedTools[name]
	case ToolFilterPlanning:
		return PlanningAllowedTools[name]
	case ToolFilterOpsControlled:
		return OpsControlledAllowedTools[name]
	default:
		return true
	}
}

// FilterToolDefinitions applies a workflow policy to LLM-facing tool
// definitions. It returns an empty slice when no definitions are allowed; that
// is intentional because a restrictive workflow policy is an execution boundary,
// not a routing hint.
func FilterToolDefinitions(policy ToolFilterPolicy, tools []map[string]interface{}) []map[string]interface{} {
	if policy == ToolFilterNone || policy == ToolFilterFull || len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if IsToolAllowedByPolicy(policy, tooldef.Name(def)) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

// RequiredToolNamesForPolicy returns ordered tools that are not optional for a
// workflow phase. Routing may still select additional allowed tools, but these
// names must remain advertised whenever available because workflow prompts and
// execution policy depend on them.
func RequiredToolNamesForPolicy(policy ToolFilterPolicy) []string {
	var names []string
	switch policy {
	case ToolFilterFull:
		names = []string{"bash", "read_file", "list_directory", "write_file", "edit_file"}
	case ToolFilterDocOnly:
		names = []string{"read_file", "list_directory", "send_file"}
	case ToolFilterPlanning:
		names = []string{"bash", "read_file", "list_directory", "send_file"}
	case ToolFilterOpsControlled:
		names = []string{"bash", "ssh", "read_file", "list_directory"}
	default:
		return nil
	}
	return append([]string(nil), names...)
}
