package agentruntime

import (
	"fmt"
	"strings"
)

// ClientToolPromptRequest is the transport-neutral input for prompt guidance
// around tools implemented by a connected client (for example an ESP32).
// Hosts pass only names and the opaque client id; no GUI/IM state crosses the
// shared runtime boundary.
type ClientToolPromptRequest struct {
	ToolNames []string
	ClientID  string
}

// BuildClientToolPrompt returns the shared device-alarm contract. Keeping the
// wording and gating in corelib means GUI, srv and future transports cannot
// accidentally diverge on whether a client-side alarm must be dispatched.
func BuildClientToolPrompt(request ClientToolPromptRequest) string {
	if len(request.ToolNames) == 0 {
		return ""
	}
	available := make(map[string]bool, len(request.ToolNames))
	for _, raw := range request.ToolNames {
		if name := strings.TrimSpace(raw); name != "" {
			available[name] = true
		}
	}
	if !available["alarm_create"] && !available["alarm_clear"] && !available["alarm_clear_all"] && !available["alarm_list"] {
		return ""
	}

	var b strings.Builder
	b.WriteString("Device-local alarm contract (mandatory):\n")
	b.WriteString("- The alarm_* tools in this turn run on the current ESP32 device, not on the computer or MaClaw GUI.\n")
	b.WriteString("- For a spoken request to set, cancel, or list an alarm on this device, use the matching alarm_* tool. Never claim an alarm was set, cancelled, or listed unless you called that tool.\n")
	if available["alarm_create"] {
		b.WriteString("- For \"设置闹钟\" / \"几分钟后叫我\" and similar requests, call alarm_create. Resolve the spoken time to a future absolute triggerAtEpochMs (Unix milliseconds) and include a short label when useful.\n")
	}
	b.WriteString("- Do not create a host/GUI scheduled task for an ordinary device alarm. Use a host schedule only when the user explicitly asks for a computer/MaClaw task or a task that must run while this device is offline.\n")
	if clientID := strings.TrimSpace(request.ClientID); clientID != "" {
		fmt.Fprintf(&b, "- Tool calls are routed to the current device (%s); report a dispatch as a device request, and treat its later tool_result as authoritative.\n", clientID)
	}
	return strings.TrimSpace(b.String())
}
