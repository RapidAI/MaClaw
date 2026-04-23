package agent

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestCoreToolNames_AllRegistered verifies that every tool declared in
// CoreToolNames (the router's "always include" set) is either:
//   - registered by RegisterCoreTools, or
//   - a GUI-only tool that TUI intentionally does not support.
//
// This test is the mechanism-level guard against the "tool declared in
// CoreToolNames but missing from CoreToolRegistry" class of bugs.
// When a new tool is added to CoreToolNames, this test forces the developer
// to either register it in RegisterCoreTools or add it to the exclusion list.
func TestCoreToolNames_AllRegistered(t *testing.T) {
	reg := NewCoreToolRegistry()
	RegisterCoreTools(reg, CoreToolDeps{})

	// GUI-only tools that require desktop infrastructure (Wails, browser
	// engine, remote session manager, etc.) and are intentionally not
	// available in TUI. Each entry must have a comment explaining why.
	guiOnly := map[string]bool{
		"list_sessions":    true, // requires RemoteSessionManager (Wails)
		"create_session":   true, // requires RemoteSessionManager (Wails)
		"send_and_observe": true, // requires RemoteSessionManager (Wails)
		"get_session_output": true, // requires RemoteSessionManager (Wails)
		"get_session_events": true, // requires RemoteSessionManager (Wails)
		"control_session":  true, // requires RemoteSessionManager (Wails)
		"call_mcp_tool":    true, // requires MCPRegistry (Wails)
		"set_nickname":     true, // requires GUI user model
		"discover_tool":    true, // requires GUI ToolRegistry + deferred tools
	}

	missing := reg.MissingTools(tool.CoreToolNames)
	for _, name := range missing {
		if guiOnly[name] {
			continue
		}
		t.Errorf("CoreToolNames declares %q but RegisterCoreTools does not register it.\n"+
			"Fix: either register it in RegisterCoreTools (with ExtraHandlers if host-specific),\n"+
			"or add it to guiOnly in this test with a comment explaining why.", name)
	}
}
