package ha

import (
	"encoding/json"
	"strings"
)

// LLMProviderMonitorLeaseKey is the system-setting key holding the LLM
// provider monitor's single-runner lease. The lease is owned by the monitor
// loop in httpapi, but the constant lives here: the HA apply path fences
// stale replicated writes to this key, and ha cannot import httpapi.
const LLMProviderMonitorLeaseKey = "llm_provider_monitor_lease"

// llmProviderMonitorLeaseFence reports whether an incoming replicated write to
// the monitor lease should be skipped: a lagging op must not demote a fresher
// local lease. System-setting replication can lag the lease TTL by minutes
// under default sync settings, so without this fence a standby would see a
// healthy holder's lease as expired. Malformed payloads do not fence — they
// are applied as usual so a corrupted lease never blocks recovery.
func llmProviderMonitorLeaseFence(localRaw, incomingRaw string) bool {
	var local, incoming struct {
		Until int64 `json:"until"`
	}
	if err := json.Unmarshal([]byte(incomingRaw), &incoming); err != nil {
		return false
	}
	if strings.TrimSpace(localRaw) == "" {
		return false
	}
	if err := json.Unmarshal([]byte(localRaw), &local); err != nil {
		return false
	}
	return local.Until > incoming.Until
}
