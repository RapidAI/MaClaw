package memory

import (
	"math"
	"time"
)

// Forgetting curve constants.
const (
	// decayLambda controls the forgetting rate. With λ=0.003 the half-life
	// is ~231 hours ≈ 9.6 days. After 14 days without access a memory with
	// initial strength 1.0 decays to ~0.36.
	decayLambda = 0.003

	// dormantThreshold is the strength below which an entry becomes dormant.
	dormantThreshold = 0.1

	// recallStrengthBoost is added to Strength each time an entry is recalled.
	recallStrengthBoost = 1.0
)

const (
	// NoteHalfLife is the desktop-bot note curve. It is not decayLambda.
	// Profile and log do not use it. A note that is not written or searched
	// for NoteForgetAfter is deleted from the hot copy and notes.md.
	NoteHalfLife    = 6 * time.Hour
	NoteForgetAfter = 24 * time.Hour
)

// NoteStrength is exp(-ln2 * hours / 6). At one half-life it is 0.5.
// It does not read decayLambda.
func NoteStrength(lastTouch, now time.Time) float64 {
	if lastTouch.IsZero() || !now.After(lastTouch) {
		return 1
	}
	hours := now.Sub(lastTouch).Hours()
	return math.Exp(-math.Ln2 * hours / NoteHalfLife.Hours())
}

// NoteForgotten reports whether a note has gone 24 hours without a write or
// a search hit. Profile and log are never forgotten by this rule.
func NoteForgotten(lastTouch, now time.Time) bool {
	if lastTouch.IsZero() {
		return false
	}
	return !now.Before(lastTouch.Add(NoteForgetAfter))
}

// decayStrength computes the current strength of an entry using the
// Ebbinghaus forgetting curve: S(t) = S₀ × exp(-λ × hours).
func decayStrength(e Entry, now time.Time) float64 {
	if e.Strength <= 0 {
		return 0
	}
	hours := now.Sub(e.UpdatedAt).Hours()
	if hours < 0 {
		hours = 0
	}
	return e.Strength * math.Exp(-decayLambda*hours)
}

// isDormant returns true if the entry's current strength is below the
// dormant threshold.
func isDormant(e Entry, now time.Time) bool {
	return decayStrength(e, now) < dormantThreshold
}

// boostStrength increases an entry's strength after a recall hit.
// It also resets UpdatedAt so the decay restarts from the new peak.
func boostStrength(e *Entry, now time.Time) {
	current := decayStrength(*e, now)
	e.Strength = current + recallStrengthBoost
	e.UpdatedAt = now
}

// batchDecayAndMark iterates all entries, computes current strength, and
// marks entries as dormant when they fall below the threshold. Returns the
// number of newly dormant entries.
//
// NOTE: We only persist the decayed strength for entries that become dormant.
// Active entries keep their peak Strength so the exponential decay formula
// remains correct on subsequent runs (decay is always computed from the
// peak value at UpdatedAt, not from a previously decayed snapshot).
func batchDecayAndMark(entries []Entry, now time.Time) int {
	count := 0
	for i := range entries {
		if entries[i].Category.IsProtected() || IsDurableTaskManagementEntry(&entries[i]) {
			continue
		}
		if entries[i].Status == StatusSuperseded {
			continue
		}
		cur := decayStrength(entries[i], now)
		if cur < dormantThreshold && entries[i].Status != StatusDormant {
			entries[i].Status = StatusDormant
			entries[i].Strength = cur // snapshot only for dormant entries
			count++
		}
	}
	return count
}

func dormantDecayUpdates(entries []Entry, now time.Time) []Entry {
	updates := make([]Entry, 0)
	for i := range entries {
		if entries[i].Category.IsProtected() || IsDurableTaskManagementEntry(&entries[i]) || entries[i].Status == StatusSuperseded {
			continue
		}
		cur := decayStrength(entries[i], now)
		if cur < dormantThreshold && entries[i].Status != StatusDormant {
			updated := entries[i]
			updated.Status = StatusDormant
			updated.Strength = cur
			updates = append(updates, updated)
		}
	}
	return updates
}
