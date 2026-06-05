package tool

import "strings"

var disabledExternalCodingSessionTools = map[string]bool{
	"create_session":   true,
	"send_and_observe": true,
	"control_session":  true,
}

// IsDisabledExternalCodingSessionTool reports whether name is a legacy
// external coding-session tool that must never be exposed to agents.
func IsDisabledExternalCodingSessionTool(name string) bool {
	return disabledExternalCodingSessionTools[strings.ToLower(strings.TrimSpace(name))]
}

// DisabledExternalCodingSessionTools returns a copy of the disabled tool set.
func DisabledExternalCodingSessionTools() map[string]bool {
	out := make(map[string]bool, len(disabledExternalCodingSessionTools))
	for name, disabled := range disabledExternalCodingSessionTools {
		out[name] = disabled
	}
	return out
}

// FilterDisabledExternalCodingSessionToolDefs removes disabled external
// coding-session tools from OpenAI-style tool definitions.
func FilterDisabledExternalCodingSessionToolDefs(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		if !IsDisabledExternalCodingSessionTool(ExtractToolName(t)) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
