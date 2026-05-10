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
