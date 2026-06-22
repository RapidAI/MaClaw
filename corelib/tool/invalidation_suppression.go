package tool

import (
	"sort"
	"strings"
	"time"
)

// consecutiveFailureKey builds the context key for tracking consecutive failures.
// It takes the first 3 query tokens, sorts them lexicographically, and joins with "|".
func consecutiveFailureKey(queryTokens []string) string {
	tokens := make([]string, 0, 3)
	for _, t := range queryTokens {
		tokens = append(tokens, t)
		if len(tokens) >= 3 {
			break
		}
	}
	sort.Strings(tokens)
	return strings.Join(tokens, "|")
}

// recordConsecutiveFailure increments the failure counter for the given tool and context key.
// When the counter reaches 3, suppression is activated.
// This method acquires t.mu for thread safety.
func (t *UsageTracker) recordConsecutiveFailure(toolName, contextKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.recordConsecutiveFailureLocked(toolName, contextKey)
}

// recordConsecutiveFailureLocked is the lock-free implementation.
// Caller must hold t.mu.Lock().
func (t *UsageTracker) recordConsecutiveFailureLocked(toolName, contextKey string) {
	if t.invalidations == nil {
		t.invalidations = make(map[string]ToolInvalidationState)
	}

	state := t.invalidations[toolName]

	// Find or create the suppression entry for this context key.
	idx := -1
	for i := range state.Suppressions {
		if state.Suppressions[i].ContextKey == contextKey {
			idx = i
			break
		}
	}

	if idx == -1 {
		// Cap suppressions slice to prevent unbounded growth.
		// When at capacity, evict the first inactive entry (oldest by position).
		// This bounds cleanupStaleSuppressionsLocked to O(maxSuppressionsPerTool * M).
		const maxSuppressionsPerTool = 10
		if len(state.Suppressions) >= maxSuppressionsPerTool {
			evicted := false
			for i := range state.Suppressions {
				if !state.Suppressions[i].Active {
					state.Suppressions = append(state.Suppressions[:i], state.Suppressions[i+1:]...)
					evicted = true
					break
				}
			}
			// If all entries are active, evict the first one (oldest).
			if !evicted {
				state.Suppressions = state.Suppressions[1:]
			}
		}
		// Create new entry.
		state.Suppressions = append(state.Suppressions, SuppressionEntry{
			ContextKey:   contextKey,
			FailureCount: 1,
			Active:       false,
		})
		idx = len(state.Suppressions) - 1
	} else {
		state.Suppressions[idx].FailureCount++
	}

	// Activate suppression at threshold 3.
	if state.Suppressions[idx].FailureCount >= 3 {
		state.Suppressions[idx].Active = true
	}

	t.invalidations[toolName] = state
}

// resetConsecutiveFailure resets all consecutive failure counters for the given tool
// on any success, regardless of context key.
// This method acquires t.mu for thread safety.
func (t *UsageTracker) resetConsecutiveFailure(toolName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.resetConsecutiveFailureLocked(toolName)
}

// resetConsecutiveFailureLocked is the lock-free implementation.
// Caller must hold t.mu.Lock().
func (t *UsageTracker) resetConsecutiveFailureLocked(toolName string) {
	state, ok := t.invalidations[toolName]
	if !ok {
		return
	}

	changed := false
	for i := range state.Suppressions {
		if state.Suppressions[i].FailureCount != 0 || state.Suppressions[i].Active {
			state.Suppressions[i].FailureCount = 0
			state.Suppressions[i].Active = false
			changed = true
		}
	}

	if changed {
		t.invalidations[toolName] = state
	}
}

// consecutiveFailureKeyFromSet builds the context key from a querySet map.
// It takes the first 3 keys (sorted lexicographically), joins with "|".
func consecutiveFailureKeyFromSet(querySet map[string]bool) string {
	if len(querySet) == 0 {
		return ""
	}
	keys := make([]string, 0, len(querySet))
	for k := range querySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 3 {
		keys = keys[:3]
	}
	return strings.Join(keys, "|")
}

// isSuppressed checks if the given tool and context key is under active
// consecutive failure suppression.
// Caller must hold t.mu.RLock() or t.mu.Lock().
func (t *UsageTracker) isSuppressed(toolName, contextKey string) bool {
	state, ok := t.invalidations[toolName]
	if !ok {
		return false
	}

	for i := range state.Suppressions {
		if state.Suppressions[i].ContextKey == contextKey {
			return state.Suppressions[i].Active
		}
	}
	return false
}

// shouldAutoLiftSuppression checks whether a suppression should be auto-lifted
// because all failure records for the tool+context have aged out of the 7-day window.
// Returns true if suppression should be lifted.
// Caller must hold t.mu.RLock() or t.mu.Lock().
func (t *UsageTracker) shouldAutoLiftSuppression(toolName, contextKey string) bool {
	cutoff := time.Now().AddDate(0, 0, -7)
	toolName = normalizeUsageToolName(toolName)

	for _, r := range t.records {
		if r.ToolName != toolName {
			continue
		}
		if r.Timestamp.Before(cutoff) {
			continue
		}
		// Check if it's a failure record (retry or abandon).
		if r.FollowUp != "retry" && r.FollowUp != "abandon" {
			continue
		}
		// Check context match: derive context key from record's query tokens.
		recordContextKey := consecutiveFailureKey(r.QueryTokens)
		if recordContextKey == contextKey {
			// There's still at least one failure record within the 7-day window
			// for this context — suppression should remain active.
			return false
		}
	}
	// No failure records found within the window for this context — auto-lift.
	return true
}

// isSuppressedWithAutoLift checks suppression status and performs auto-lift
// when all failure records have aged out of the 7-day window.
// Since this is called under RLock, it cannot modify state directly.
// It returns (isSuppressed, shouldLift). The caller can schedule deferred cleanup.
// Caller must hold t.mu.RLock() or t.mu.Lock().
func (t *UsageTracker) isSuppressedWithAutoLift(toolName, contextKey string) bool {
	if !t.isSuppressed(toolName, contextKey) {
		return false
	}
	// Check if suppression should be auto-lifted.
	if t.shouldAutoLiftSuppression(toolName, contextKey) {
		// All failure records have aged out. The suppression is stale.
		// We cannot modify state under RLock, but we can report it as
		// not suppressed (the suppression is semantically expired).
		return false
	}
	return true
}

// cleanupStaleSuppressionsLocked removes suppression entries that are Active=true
// but whose failure records have all aged out of the 7-day window.
// This prevents unbounded growth of the suppressions slice on disk.
// Caller must hold t.mu.Lock().
func (t *UsageTracker) cleanupStaleSuppressionsLocked(toolName string) {
	state, ok := t.invalidations[toolName]
	if !ok || len(state.Suppressions) == 0 {
		return
	}

	changed := false
	alive := state.Suppressions[:0] // reuse backing array
	for _, s := range state.Suppressions {
		if s.Active && t.shouldAutoLiftSuppression(toolName, s.ContextKey) {
			// Stale suppression — drop it.
			changed = true
			continue
		}
		// Keep non-active entries (counter tracking) and still-valid active entries.
		alive = append(alive, s)
	}

	if changed {
		state.Suppressions = alive
		t.invalidations[toolName] = state
	}
}
