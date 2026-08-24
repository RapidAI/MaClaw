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
		"list_providers":     true,
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

// ExtraSharedHostCapabilityNames are GUI-advertised capabilities that are
// not registered by RegisterCoreTools but must still work on a headless
// host (same outcomes, host-adapted I/O).
func ExtraSharedHostCapabilityNames() []string {
	return []string{
		"delegate_task",
		"office",
		"generate_pdf",
		"list_mcp_tools",
		"import_mcp_servers",
		"tts_render",
		"edit_lines",
	}
}
