package tool

import "time"

// InvalidationEvent represents a condition change that should decay stale outcome records.
type InvalidationEvent struct {
	ToolName    string    `json:"tool_name"`
	Timestamp   time.Time `json:"timestamp"`
	Reason      string    `json:"reason"`
	ScopeTokens []string  `json:"scope_tokens,omitempty"` // nil = global invalidation for this tool
}

// ToolInvalidationState holds the invalidation metadata for a single tool.
type ToolInvalidationState struct {
	LastInvalidation *InvalidationEvent `json:"last_invalidation,omitempty"`
	LastFingerprint  string             `json:"last_fingerprint,omitempty"`
	Suppressions     []SuppressionEntry `json:"suppressions,omitempty"`
}

// SuppressionEntry tracks consecutive failure suppression for a (tool, context) pair.
type SuppressionEntry struct {
	ContextKey   string `json:"context_key"`
	FailureCount int    `json:"failure_count"`
	Active       bool   `json:"active"`
}

// UsageData is the top-level persistence structure, replacing the flat []UsageRecord array.
// On load, if the file parses as []UsageRecord (flat array), it is migrated to
// UsageData{Records: parsed}. If it parses as UsageData, it is used directly.
type UsageData struct {
	Records       []UsageRecord                    `json:"records"`
	Invalidations map[string]ToolInvalidationState `json:"invalidations,omitempty"` // keyed by tool name
}

// copyInvalidations returns a deep copy of the invalidations map that is safe
// to pass to saveSnapshot outside the lock. Both the map and all slice/pointer
// fields within values are copied to prevent data races with concurrent writers.
func copyInvalidations(src map[string]ToolInvalidationState) map[string]ToolInvalidationState {
	if src == nil {
		return nil
	}
	dst := make(map[string]ToolInvalidationState, len(src))
	for k, v := range src {
		cp := ToolInvalidationState{
			LastFingerprint: v.LastFingerprint,
		}
		if v.LastInvalidation != nil {
			eventCopy := *v.LastInvalidation
			if len(v.LastInvalidation.ScopeTokens) > 0 {
				eventCopy.ScopeTokens = make([]string, len(v.LastInvalidation.ScopeTokens))
				copy(eventCopy.ScopeTokens, v.LastInvalidation.ScopeTokens)
			}
			cp.LastInvalidation = &eventCopy
		}
		if len(v.Suppressions) > 0 {
			cp.Suppressions = make([]SuppressionEntry, len(v.Suppressions))
			copy(cp.Suppressions, v.Suppressions)
		}
		dst[k] = cp
	}
	return dst
}
