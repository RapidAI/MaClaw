package agentruntime

import (
	"errors"
	"strings"
)

// HostSurfaceToolNames are tools that stay on the model surface even when the
// current host cannot execute them. Callers must return CapabilityUnavailable
// instead of dropping the tool or reporting unknown tool.
func HostSurfaceToolNames() []string {
	return []string{"screenshot", "open"}
}

// IsHostSurfaceTool reports whether name is a host-capability tool that must
// remain visible across GUI and headless catalogs.
func IsHostSurfaceTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "screenshot", "open":
		return true
	default:
		return false
	}
}

// HostToolUnavailableCapability maps a host-surface tool to the capability
// that must be present to execute it.
func HostToolUnavailableCapability(name string) string {
	switch strings.TrimSpace(name) {
	case "screenshot":
		return "desktop_capture"
	case "open":
		return "document_launcher"
	default:
		return ""
	}
}

// CapabilityUnavailableResult is the stable tool-result text for a known tool
// that the current host cannot execute.
func CapabilityUnavailableResult(capability string) string {
	return CapabilityUnavailableError{Capability: strings.TrimSpace(capability)}.Error()
}

// IsCapabilityUnavailable reports whether err (or a tool result string) is the
// stable unavailable outcome rather than an unknown-tool failure.
func IsCapabilityUnavailable(err error, result string) bool {
	if err != nil {
		var unavailable CapabilityUnavailableError
		if errors.As(err, &unavailable) {
			return true
		}
		if strings.Contains(err.Error(), CapabilityUnavailableCode) {
			return true
		}
	}
	return strings.Contains(strings.TrimSpace(result), CapabilityUnavailableCode)
}
