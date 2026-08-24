package tool

import (
	"strings"
	"testing"
)

// The compile-time anchor catches an ordinal without a row. It cannot catch an
// exported constant without an ordinal, which is the easier mistake to make:
// the const block reads like the whole enumeration. This test is that seam.
func TestEveryEffectStateConstantIsAccountedFor(t *testing.T) {
	declared := map[SemanticExternalEffectState]bool{}
	for _, facts := range semanticExternalEffectStateTable {
		if facts.State == "" {
			t.Fatal("a table row has no state; the ordinals and the table have drifted apart")
		}
		if declared[facts.State] {
			t.Fatalf("state %q has two rows, so two ordinals disagree about it", facts.State)
		}
		declared[facts.State] = true
	}
	for _, state := range []SemanticExternalEffectState{
		SemanticExternalEffectRunning,
		SemanticExternalEffectAwaitingReceipt,
		SemanticExternalEffectSucceeded,
		SemanticExternalEffectFailed,
		SemanticExternalEffectUnknown,
	} {
		if !declared[state] {
			t.Fatalf("state %q exists as a constant but no code declares who is responsible for leaving it", state)
		}
	}
}

// awaiting_receipt's original defect was being a state with no way out. Any
// non-terminal state needs either a sweeper or a human exit, or it becomes the
// same trap under a different name.
func TestNoStateIsADeadEnd(t *testing.T) {
	for _, facts := range semanticExternalEffectStateTable {
		if facts.Terminal {
			continue
		}
		if facts.ExpiresOnOperationLease || facts.ExpiresOnReceiptLease || facts.ManuallyResolvable {
			continue
		}
		t.Fatalf("state %q is not terminal and nothing can leave it: no lease sweeps it and no person may rule on it", facts.State)
	}
}

// A terminal state has an established outcome. Sweeping it would overwrite a
// verdict with a shrug.
func TestTerminalStatesAreNotSwept(t *testing.T) {
	for _, facts := range semanticExternalEffectStateTable {
		if !facts.Terminal {
			continue
		}
		if facts.ExpiresOnOperationLease || facts.ExpiresOnReceiptLease {
			t.Fatalf("terminal state %q is swept by a lease", facts.State)
		}
		if facts.ManuallyResolvable {
			t.Fatalf("terminal state %q admits a manual verdict over an established outcome", facts.State)
		}
	}
}

// The schema and the reconcilers must be reading the same declaration. Before
// the table they were four independent string literals.
func TestTheSchemaCheckCoversExactlyTheDeclaredStates(t *testing.T) {
	check := semanticExternalEffectStateCheckConstraint()
	for _, facts := range semanticExternalEffectStateTable {
		if !strings.Contains(check, "'"+string(facts.State)+"'") {
			t.Fatalf("schema CHECK %q rejects declared state %q", check, facts.State)
		}
	}
	if got, want := strings.Count(check, "'"), 2*len(semanticExternalEffectStateTable); got != want {
		t.Fatalf("schema CHECK %q names %d values, want %d", check, got/2, want/2)
	}
}

// Each lease must sweep exactly the states that declare it, so that adding a
// state to the table is enough to bring the sweepers along.
func TestLeaseFiltersFollowTheTable(t *testing.T) {
	receipt := semanticReceiptLeaseSweptStatesSQL()
	operation := semanticOperationLeaseSweptStatesSQL()
	for _, facts := range semanticExternalEffectStateTable {
		quoted := "'" + string(facts.State) + "'"
		if strings.Contains(receipt, quoted) != facts.ExpiresOnReceiptLease {
			t.Fatalf("receipt-lease filter %q disagrees with the table about %q", receipt, facts.State)
		}
		if strings.Contains(operation, quoted) != facts.ExpiresOnOperationLease {
			t.Fatalf("operation-lease filter %q disagrees with the table about %q", operation, facts.State)
		}
	}
	// The two sweepers must not both claim a state, or one converges a row the
	// other is still counting.
	if strings.TrimSpace(receipt) == strings.TrimSpace(operation) {
		t.Fatalf("both leases sweep the same states (%q)", receipt)
	}
}
