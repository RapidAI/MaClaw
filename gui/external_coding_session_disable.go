package main

import (
	"strings"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

var disabledExternalCodingSessionTools = coretool.DisabledExternalCodingSessionTools()

func isDisabledExternalCodingSessionTool(name string) bool {
	return coretool.IsDisabledExternalCodingSessionTool(name)
}

func disabledExternalCodingSessionToolText(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "create_session":
		return "[system rejected] create_session is disabled. Coding tasks must run through the internal CodingSubAgent, not external coding sessions."
	case "send_and_observe":
		return "[system rejected] send_and_observe is disabled. Coding tasks must run through the internal CodingSubAgent, not external coding sessions."
	case "control_session":
		return "[system rejected] control_session is disabled. External coding sessions cannot be controlled by the agent."
	default:
		return "[system rejected] external coding-session tools are disabled. Coding tasks must run through the internal CodingSubAgent."
	}
}

func filterDisabledExternalCodingSessionToolDefs(tools []map[string]interface{}) []map[string]interface{} {
	return coretool.FilterDisabledExternalCodingSessionToolDefs(tools)
}
