package tool

import (
	"log"
	"time"
)

// checkFingerprint compares the current fingerprint for the given tool against
// the stored LastFingerprint. If the fingerprint has changed and LastFingerprint
// was non-empty (not a first-ever recording), it generates an InvalidationEvent
// with Reason="fingerprint_change" and applies it.
//
// This method MUST be called while t.mu is held (write lock).
// It wraps provider iteration in recover() to handle panicking providers gracefully.
//
// PERF NOTE: FingerprintProviders may call LoadConfig() which acquires configMu.
// This means RecordExperience (which holds t.mu) can block on configMu contention.
// This is acceptable because configMu hold times are short (<50ms) in practice.
// If this becomes a bottleneck, consider caching fingerprint results with TTL.
func (t *UsageTracker) checkFingerprint(toolName string) {
	if len(t.FingerprintProviders) == 0 {
		return
	}

	// Get the current fingerprint from the first provider that returns non-empty.
	currentFP := t.computeCurrentFingerprint(toolName)
	if currentFP == "" {
		// No provider returned a fingerprint — skip comparison.
		return
	}

	// Get stored fingerprint for this tool.
	state := t.invalidations[toolName]
	storedFP := state.LastFingerprint

	if storedFP == "" {
		// First-ever recording: store fingerprint, no event.
		state.LastFingerprint = currentFP
		t.invalidations[toolName] = state
		return
	}

	if storedFP != currentFP {
		// Fingerprint changed: generate InvalidationEvent before recording.
		event := InvalidationEvent{
			ToolName:  toolName,
			Timestamp: time.Now(),
			Reason:    "fingerprint_change",
		}
		// Apply the invalidation directly: update state with the new event.
		state.LastInvalidation = &event
		state.LastFingerprint = currentFP
		t.invalidations[toolName] = state
		log.Printf("[usage-tracker] fingerprint_change detected for tool=%s", toolName)
		return
	}

	// Fingerprint matches — update LastFingerprint (no-op since same value, but
	// ensures state is consistent after successful comparison).
	state.LastFingerprint = currentFP
	t.invalidations[toolName] = state
}

// computeCurrentFingerprint iterates over FingerprintProviders and returns the
// first non-empty fingerprint for the given tool. Provider iteration is wrapped
// in recover() to handle panicking providers gracefully.
func (t *UsageTracker) computeCurrentFingerprint(toolName string) string {
	for _, provider := range t.FingerprintProviders {
		fp := t.safeComputeFingerprint(provider, toolName)
		if fp != "" {
			return fp
		}
	}
	return ""
}

// safeComputeFingerprint calls provider.ComputeFingerprint with panic recovery.
// Returns "" if the provider panics.
func (t *UsageTracker) safeComputeFingerprint(provider FingerprintProvider, toolName string) (result string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[usage-tracker] fingerprint provider panicked for tool=%s: %v", toolName, r)
			result = ""
		}
	}()
	return provider.ComputeFingerprint(toolName)
}
