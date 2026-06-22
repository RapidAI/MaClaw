package tool

import (
	"math"
	"time"
)

// decayMultiplier computes the effective weight multiplier for a record that
// predates the most recent applicable invalidation.
//
// Formula: effective_weight = max(0.1, 1.0 - 0.9 × min(hours_since_invalidation / 48.0, 1.0))
//
// At t=0:   multiplier = 1.0 (grace period start)
// At t=24h: multiplier ≈ 0.55
// At t=48h: multiplier = 0.1 (minimum, held constant thereafter)
//
// Only the most recent invalidation event is used (no compounding).
// Records that postdate the invalidation are unaffected (returns 1.0).
// Scoped invalidations (ScopeTokens non-nil) only apply when the Jaccard
// similarity between the RECORD's query tokens and the event's ScopeTokens
// meets the threshold (≥ 0.3). This implements Requirement 2.2.
//
// Parameters:
//   - recordTimestamp: the record's timestamp
//   - toolName: the tool being evaluated
//   - recordTokens: the record's query tokens (used for scope matching against invalidation ScopeTokens)
func (t *UsageTracker) decayMultiplier(recordTimestamp time.Time, toolName string, recordTokens []string) float64 {
	state, ok := t.invalidations[toolName]
	if !ok || state.LastInvalidation == nil {
		return 1.0
	}
	inv := state.LastInvalidation

	// Only decay records that predate the invalidation.
	if !recordTimestamp.Before(inv.Timestamp) {
		return 1.0
	}

	// Scope check: if invalidation has ScopeTokens, only apply to records whose
	// query tokens have sufficient overlap with the scope (Jaccard >= 0.3).
	// Per Requirement 2.2: "apply decay ONLY to records where
	// Jaccard(record.QueryTokens, event.ScopeTokens) >= 0.3"
	if inv.ScopeTokens != nil && len(recordTokens) > 0 {
		sim := jaccardSliceVsSlice(inv.ScopeTokens, recordTokens)
		if sim < 0.3 {
			return 1.0 // record's context doesn't match invalidation scope, no decay
		}
	}

	hoursSinceInvalidation := time.Since(inv.Timestamp).Hours()
	if hoursSinceInvalidation < 0 {
		hoursSinceInvalidation = 0
	}
	ratio := math.Min(hoursSinceInvalidation/48.0, 1.0)
	return math.Max(0.1, 1.0-0.9*ratio)
}

// jaccardSliceVsSlice computes Jaccard similarity between two string slices
// without allocating maps. Uses O(n*m) comparison which is faster than map
// allocation for the small token slices typical in this system (≤5 tokens each).
func jaccardSliceVsSlice(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var intersection int
	for _, tokA := range a {
		for _, tokB := range b {
			if tokA == tokB {
				intersection++
				break
			}
		}
	}
	if intersection == 0 {
		return 0
	}
	// union = |A| + |B| - |intersection| (assumes no duplicates within each slice)
	union := len(a) + len(b) - intersection
	return float64(intersection) / float64(union)
}
