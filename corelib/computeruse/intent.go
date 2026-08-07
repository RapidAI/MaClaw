package computeruse

import "strings"

// ToolNames is the full Computer Use tool surface exposed to the agent.
var ToolNames = []string{
	"computer_observe",
	"computer_click",
	"computer_type",
	"computer_key",
	"computer_scroll",
	"computer_wait",
	"computer_focus",
	"computer_find",
	"computer_done",
	"computer_playbook",
}

// LegacyGUICompeteTools are older raw GUI tools that should be de-prioritized
// when Computer Use is active (prefer ref-based computer_* instead).
var LegacyGUICompeteTools = map[string]bool{
	"gui_click":      true,
	"gui_type":       true,
	"gui_screenshot": true,
}

// HasExplicitTrigger reports whether the user message explicitly invokes
// Computer Use via the @computer / "computer use" trigger syntax. This is an
// addressing override, not intent detection — semantic activation is handled
// by the unified intent classifier (corelib/intent LabelComputerUse).
func HasExplicitTrigger(userText string) bool {
	t := strings.ToLower(strings.TrimSpace(userText))
	if t == "" {
		return false
	}
	return strings.Contains(t, "@computer") || strings.Contains(t, "computer use") ||
		strings.Contains(t, "computer_use") || strings.Contains(t, "computer-use")
}

// IsComputerUseTool reports whether name is part of the CU surface.
func IsComputerUseTool(name string) bool {
	name = strings.TrimSpace(name)
	for _, n := range ToolNames {
		if n == name {
			return true
		}
	}
	return false
}
