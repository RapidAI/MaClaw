package tool

import (
	"sort"
	"testing"
)

func repeatSelections(ids ...string) []PlannedSelection {
	out := make([]PlannedSelection, 0, len(ids))
	for _, id := range ids {
		out = append(out, PlannedSelection{ID: id})
	}
	return out
}

func repeatSelected(t *testing.T, got map[string]bool) []string {
	t.Helper()
	ids := make([]string, 0, len(got))
	for id := range got {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Both hosts adopt this closure, so the load-bearing property is that a need
// without a budget resolves exactly as it did before repeats existed: grant
// every ready selection that has not been granted, and nothing else.
func TestNextRepeatSelectionsLeavesSingleInvocationFamiliesUnchanged(t *testing.T) {
	ready := repeatSelections("selection:need:a", "selection:need:b", "selection:need:c")

	first := NextRepeatSelections(RepeatExposure{Ready: ready})
	if got := repeatSelected(t, first); len(got) != 3 {
		t.Fatalf("fresh plan granted %v, want all three", got)
	}

	// One granted and still live, one granted and spent, one untouched.
	partial := NextRepeatSelections(RepeatExposure{
		Ready:     ready,
		Granted:   map[string]bool{"selection:need:a": true, "selection:need:b": true},
		Live:      map[string]bool{"selection:need:a": true},
		Completed: map[string]bool{"selection:need:b": true},
	})
	if got := repeatSelected(t, partial); len(got) != 1 || got[0] != "selection:need:c" {
		t.Fatalf("granted %v, want only the untouched selection", got)
	}
}

func TestNextRepeatSelectionsExposesOneSiblingAtATime(t *testing.T) {
	base := "selection:need:fs.read.local:abcdef123456"
	ready := repeatSelections(base, RepeatSiblingNeedID(base, 1), RepeatSiblingNeedID(base, 2))

	first := NextRepeatSelections(RepeatExposure{Ready: ready})
	if got := repeatSelected(t, first); len(got) != 1 || got[0] != base {
		t.Fatalf("fresh family granted %v, want only the first sibling", got)
	}

	// While the first grant is live the family must not hand out a second.
	held := NextRepeatSelections(RepeatExposure{
		Ready:   ready,
		Granted: map[string]bool{base: true},
		Live:    map[string]bool{base: true},
	})
	if got := repeatSelected(t, held); len(got) != 0 {
		t.Fatalf("family granted %v while a sibling was still live", got)
	}

	// Once spent, the next sibling — and only the next — becomes available.
	advanced := NextRepeatSelections(RepeatExposure{
		Ready:     ready,
		Granted:   map[string]bool{base: true},
		Completed: map[string]bool{base: true},
	})
	if got := repeatSelected(t, advanced); len(got) != 1 || got[0] != RepeatSiblingNeedID(base, 1) {
		t.Fatalf("granted %v, want exactly the second sibling", got)
	}
}

func TestNextRepeatSelectionsStopsWhenTheBudgetIsSpent(t *testing.T) {
	base := "selection:need:fs.read.local:abcdef123456"
	second, third := RepeatSiblingNeedID(base, 1), RepeatSiblingNeedID(base, 2)
	spent := map[string]bool{base: true, second: true, third: true}

	exhausted := NextRepeatSelections(RepeatExposure{
		Ready:     repeatSelections(base, second, third),
		Granted:   spent,
		Completed: spent,
	})
	if got := repeatSelected(t, exhausted); len(got) != 0 {
		t.Fatalf("exhausted family granted %v, want nothing", got)
	}
}

// Spending budget on a new call and retrying an effect that may already have
// happened are different acts. A failed attempt is settled and the family
// moves on; an unresolved one holds the whole family.
func TestNextRepeatSelectionsHoldsAFamilyWithAnUnresolvedSibling(t *testing.T) {
	base := "selection:need:fs.write.local:abcdef123456"
	second := RepeatSiblingNeedID(base, 1)
	ready := repeatSelections(base, second)

	held := NextRepeatSelections(RepeatExposure{
		Ready:     ready,
		Granted:   map[string]bool{base: true},
		Unsettled: func(selectionID string) bool { return selectionID == base },
	})
	if got := repeatSelected(t, held); len(got) != 0 {
		t.Fatalf("family granted %v past an unresolved sibling", got)
	}

	settled := NextRepeatSelections(RepeatExposure{
		Ready:     ready,
		Granted:   map[string]bool{base: true},
		Unsettled: func(string) bool { return false },
	})
	if got := repeatSelected(t, settled); len(got) != 1 || got[0] != second {
		t.Fatalf("granted %v, want the family to move past a settled failure", got)
	}
}

// One family stalling must not silence the others, or a single unresolved
// write would take the whole turn's surface down with it.
func TestNextRepeatSelectionsIsolatesFamiliesFromEachOther(t *testing.T) {
	stalled := "selection:need:fs.write.local:aaaaaaaaaaaa"
	healthy := "selection:need:fs.read.local:bbbbbbbbbbbb"

	got := repeatSelected(t, NextRepeatSelections(RepeatExposure{
		Ready:     repeatSelections(stalled, RepeatSiblingNeedID(stalled, 1), healthy),
		Granted:   map[string]bool{stalled: true},
		Unsettled: func(selectionID string) bool { return selectionID == stalled },
	}))
	if len(got) != 1 || got[0] != healthy {
		t.Fatalf("granted %v, want only the unaffected family", got)
	}
}
