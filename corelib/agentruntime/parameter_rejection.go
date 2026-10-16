package agentruntime

import "strings"

// ParameterRejectionGuidance keeps model-facing schema rejection guidance in
// the shared Runtime contract. Detailed validator diagnostics are preserved;
// only the bare generic rejection receives deterministic recovery advice.
func ParameterRejectionGuidance(result string) string {
	trimmed := strings.TrimSpace(result)
	if !strings.HasPrefix(trimmed, "[system rejected] parameter_") {
		return result
	}
	const guidance = " The call was refused before execution, so the tool remains available: do not retry the same arguments, call it again with arguments that match its rendered parameter schema exactly (some channel adapters take no arguments: {}), and do not ask the user to re-authorize tools."
	if trimmed == "[system rejected] parameter_schema_invalid" {
		return trimmed + "." + guidance
	}
	return trimmed + guidance
}
