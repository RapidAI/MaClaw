package workflow

import (
	"sort"
	"testing"
)

// --- Task 2.2: Unit test for All() determinism ---

// TestRegistryAll_DeterministicAndComplete asserts that WorkflowRegistry.All()
// returns every registered type exactly once and in the same sorted (by Type)
// order across repeated calls. The code generator and contract tests depend on
// this byte-stable enumeration.
//
// _Requirements: 2.4_
func TestRegistryAll_DeterministicAndComplete(t *testing.T) {
	r := NewWorkflowRegistry()

	first := r.All()
	if len(first) == 0 {
		t.Fatal("All() returned no templates")
	}

	// Every registered type appears exactly once.
	seen := make(map[WorkflowType]int, len(first))
	for _, tmpl := range first {
		if tmpl == nil {
			t.Fatal("All() returned a nil template pointer")
		}
		seen[tmpl.Type]++
	}
	for wt, count := range seen {
		if count != 1 {
			t.Errorf("type %q appears %d times, want exactly 1", wt, count)
		}
	}

	// All() must agree with Match() for every enumerated type.
	for _, tmpl := range first {
		if got := r.Match(tmpl.Type); got != tmpl {
			t.Errorf("All() and Match(%q) disagree: %p vs %p", tmpl.Type, tmpl, got)
		}
	}

	// Returned order is sorted by Type.
	types := make([]string, len(first))
	for i, tmpl := range first {
		types[i] = string(tmpl.Type)
	}
	if !sort.StringsAreSorted(types) {
		t.Errorf("All() order is not sorted by Type: %v", types)
	}

	// Repeated calls return the same types in the same order (determinism).
	for iter := 0; iter < 5; iter++ {
		next := r.All()
		if len(next) != len(first) {
			t.Fatalf("All() length changed across calls: %d vs %d", len(next), len(first))
		}
		for i := range first {
			if next[i].Type != first[i].Type {
				t.Fatalf("All() order changed at index %d across calls: %q vs %q",
					i, next[i].Type, first[i].Type)
			}
		}
	}
}
