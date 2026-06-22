package tool

import (
	"log"
	"time"
)

// ApplyInvalidation persists the event, updates state, and triggers save.
// Safe for concurrent use. The most recent invalidation replaces the old one
// (no compounding across multiple events).
func (t *UsageTracker) ApplyInvalidation(event InvalidationEvent) {
	t.mu.Lock()
	if t.invalidations == nil {
		t.invalidations = make(map[string]ToolInvalidationState)
	}
	state := t.invalidations[event.ToolName]
	state.LastInvalidation = &event
	t.invalidations[event.ToolName] = state

	snapshot := make([]UsageRecord, len(t.records))
	copy(snapshot, t.records)
	invalidations := copyInvalidations(t.invalidations)
	t.mu.Unlock()

	log.Printf("[usage-tracker] ApplyInvalidation: tool=%s reason=%s", event.ToolName, event.Reason)
	_ = t.saveSnapshot(snapshot, invalidations)
}

// InvalidateOutcomes immediately generates an InvalidationEvent with nil ScopeTokens
// (global invalidation for that tool) and applies it through the standard pipeline.
// Safe for concurrent use from multiple goroutines.
//
// When called for a tool with no existing outcome records, the invalidation timestamp
// is still persisted so that future records created before the next fingerprint check
// are properly handled.
func (t *UsageTracker) InvalidateOutcomes(toolName string, reason string) {
	event := InvalidationEvent{
		ToolName:    toolName,
		Timestamp:   time.Now(),
		Reason:      reason,
		ScopeTokens: nil, // global invalidation
	}
	t.ApplyInvalidation(event)
}
