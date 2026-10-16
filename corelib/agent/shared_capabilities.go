package agent

// DesktopOnlyCapabilityNames are operator-screen or OS-desktop host actions.
// Headless hosts may advertise them as honestly unavailable; they are not a
// silent capability hole and are not required to execute without a display.
func DesktopOnlyCapabilityNames() map[string]bool {
	return map[string]bool{
		"screenshot":         true,
		"open":               true,
		"tts_local":          true,
		"record_audio":       true,
		"async_wait":         true,
		"set_nickname":       true,
		"list_sessions":      true,
		"project_manage":     true,
		"send_input":         true,
		"get_session_output": true,
		"get_session_events": true,
		"interrupt_session":  true,
		"kill_session":       true,
	}
}

// SharedCoreCapabilityNames returns platform-agnostic tool names from
// RegisterCoreTools, excluding desktop-display-only actions. Hosts must
// advertise and execute these when they can provide I/O.
func SharedCoreCapabilityNames() []string {
	r := NewCoreToolRegistry()
	RegisterCoreTools(r, CoreToolDeps{})
	desktop := DesktopOnlyCapabilityNames()
	out := make([]string, 0, len(r.Names()))
	for _, name := range r.Names() {
		if desktop[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

// ExtraSharedHostCapabilityNames is the hook for GUI-advertised capabilities
// that still cannot be compiled into RegisterCoreTools. Shared host tools now
// live in RegisterCoreTools; keep this empty unless a new extra appears.
func ExtraSharedHostCapabilityNames() []string {
	return nil
}

// HostPrivateCapabilityNames are GUI-catalog tools that must not enter
// RegisterCoreTools. Headless hosts serve the same outcomes through reviewed
// semantic providers (config.manage.self, template.manage.session, opaque
// MCP/Skill grants) rather than these IM soup names. FileRead/ripgrep/excel
// remain folded into office/search helpers and are not listed here.
func HostPrivateCapabilityNames() map[string]bool {
	return map[string]bool{
		"call_mcp_tool":      true,
		"parallel_execute":   true,
		"craft_tool":         true,
		"compress_context":   true,
		"manage_template":    true,
		"create_template":    true,
		"list_templates":     true,
		"launch_template":    true,
		"manage_config":      true,
		"set_max_iterations": true,
		"mis_data":           true,
	}
}
