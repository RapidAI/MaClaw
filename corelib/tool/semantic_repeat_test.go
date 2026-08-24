package tool

import (
	"sort"
	"testing"
)

// The first sibling must keep the identity a non-repeating need always had.
// If it shifted, every already-published plan, durable execution key, and
// stored grant built from that identity would stop matching after an upgrade.
func TestRepeatSiblingZeroKeepsTheHistoricalIdentity(t *testing.T) {
	base := "need:fs.read.local:abc123def456"
	if got := RepeatSiblingNeedID(base, 0); got != base {
		t.Fatalf("first sibling id = %q, want the unchanged base %q", got, base)
	}
	if got := RepeatSiblingNeedID(base, -3); got != base {
		t.Fatalf("negative index id = %q, want the unchanged base %q", got, base)
	}
}

// Siblings are exposed in plan order, so their identities must sort in
// invocation order rather than lexically stumbling at ten.
func TestRepeatSiblingsSortInInvocationOrder(t *testing.T) {
	base := "need:fs.read.local:abc123def456"
	ids := make([]string, 0, 12)
	for index := 0; index < 12; index++ {
		ids = append(ids, RepeatSiblingNeedID(base, index))
	}
	ordered := append([]string(nil), ids...)
	sort.Strings(ordered)
	for i := range ids {
		if ids[i] != ordered[i] {
			t.Fatalf("sibling order breaks at %d: generated=%v sorted=%v", i, ids, ordered)
		}
	}
}

func TestRepeatFamilyCollapsesSiblingsAndLeavesOthersAlone(t *testing.T) {
	base := "need:fs.read.local:abc123def456"
	for index := 0; index < 12; index++ {
		if got := RepeatFamilyID(RepeatSiblingNeedID(base, index)); got != base {
			t.Fatalf("sibling %d family = %q, want %q", index, got, base)
		}
	}
	// A qualifier, capability, or adapter is free to contain the separator.
	// Splitting on it blindly would merge two unrelated selections into one
	// family and silently hide the second from the model.
	for _, id := range []string{
		"need:information.lookup:c0ffee#tag",
		"need:shell.execute.local:abc#1",
		"need:fs.read.local:abc#123",
		"#02",
		"need:fs.read.local:abc#00",
	} {
		if got := RepeatFamilyID(id); got != id {
			t.Fatalf("non-sibling id %q collapsed to %q", id, got)
		}
	}
}

func TestRepeatSiblingBudgetTreatsSilenceAsSingleInvocation(t *testing.T) {
	for _, declared := range []int{-5, 0, 1} {
		if got := RepeatSiblingBudget(declared); got != 1 {
			t.Fatalf("budget(%d) = %d, want 1", declared, got)
		}
	}
	if got := RepeatSiblingBudget(12); got != 12 {
		t.Fatalf("budget(12) = %d, want 12", got)
	}
	// The budget becomes real plan nodes, so an unbounded rule must be capped
	// rather than trusted.
	if got := RepeatSiblingBudget(5000); got != RepeatSiblingBudgetLimit {
		t.Fatalf("budget(5000) = %d, want the %d cap", got, RepeatSiblingBudgetLimit)
	}
}
