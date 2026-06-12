package main

// ---------------------------------------------------------------------------
// Direct-mode tool blocklist: used by SubAgent orchestration to prevent the
// main loop from receiving coding tools while implementation is owned by the
// internal CodingSubAgent.
// ---------------------------------------------------------------------------

// directModeMainLoopBlocklist lists tools that the main loop must not receive
// while implementation is owned by the internal CodingSubAgent.
var directModeMainLoopBlocklist = map[string]bool{
	"bash":               true,
	"write_file":         true,
	"edit_file":          true,
	"edit_lines":         true,
	"craft_tool":         true,
	"parallel_execute":   true,
	"create_session":     true,
	"send_and_observe":   true,
	"control_session":    true,
	"get_session_output": true,
	"get_session_events": true,
	"interrupt_session":  true,
	"kill_session":       true,
	"list_sessions":      true,
	"send_input":         true,
}

func init() {
	for name := range disabledExternalCodingSessionTools {
		directModeMainLoopBlocklist[name] = true
	}
}

// isDirectModeBlockedTool returns true if the main loop must not receive this
// tool while direct mode delegates code changes to CodingSubAgent.
func isDirectModeBlockedTool(name string) bool {
	return directModeMainLoopBlocklist[name]
}
