package tool

import (
	"fmt"
	"strings"
)

// The closed set of external-effect states, and what each one obliges.
//
// The states were previously spelled out in four independent places: the const
// block, the table's CHECK constraint, the running-lease reconciler, and the
// receipt-lease reconciler — each as its own hardcoded string. Nothing tied
// them together, so adding a sixth state was a change the compiler had no
// opinion about. The reconcilers would keep passing their tests by silently
// ignoring rows in the new state, which is the failure mode that produced
// awaiting_receipt's original dead end: a state nothing was responsible for
// leaving.
//
// What the compiler enforces here is the count. Adding an ordinal without
// giving it a row fails the build (see the anchor below), and a row cannot be
// written without answering which reconciler owns the state. What the compiler
// still cannot enforce is that a new exported constant gets an ordinal at all;
// TestEveryEffectStateConstantIsAccountedFor covers that seam.

type semanticExternalEffectStateOrdinal int

const (
	effectStateOrdinalRunning semanticExternalEffectStateOrdinal = iota
	effectStateOrdinalAwaitingReceipt
	effectStateOrdinalSucceeded
	effectStateOrdinalFailed
	effectStateOrdinalUnknown

	semanticExternalEffectStateCount
)

// semanticExternalEffectStateFacts is what every state must declare. The
// fields are obligations, not descriptions: each one names a piece of code that
// has to handle the state or deliberately decline to.
type semanticExternalEffectStateFacts struct {
	State SemanticExternalEffectState
	// Terminal states are never swept. An operation in one has an established
	// outcome, whether or not anyone liked it.
	Terminal bool
	// ExpiresOnOperationLease is swept by ReconcileExpiredOperations: the
	// process that was performing the effect is gone.
	ExpiresOnOperationLease bool
	// ExpiresOnReceiptLease is swept by ReconcileExpiredReceiptWaits: the
	// effect was dispatched and the confirmation never came.
	ExpiresOnReceiptLease bool
	// ManuallyResolvable admits an out-of-band human verdict. A state with a
	// live expectation must not, or the verdict races a receipt in flight.
	ManuallyResolvable bool
}

var semanticExternalEffectStateTable = [...]semanticExternalEffectStateFacts{
	effectStateOrdinalRunning: {
		State: SemanticExternalEffectRunning, ExpiresOnOperationLease: true,
	},
	effectStateOrdinalAwaitingReceipt: {
		State: SemanticExternalEffectAwaitingReceipt, ExpiresOnReceiptLease: true,
	},
	effectStateOrdinalSucceeded: {State: SemanticExternalEffectSucceeded, Terminal: true},
	effectStateOrdinalFailed:    {State: SemanticExternalEffectFailed, Terminal: true},
	// unknown is the one non-terminal state no sweeper owns, because it is
	// where the sweepers put things. It is the only state a person may rule on.
	effectStateOrdinalUnknown: {
		State: SemanticExternalEffectUnknown, ManuallyResolvable: true,
	},
}

// Compile-time exhaustiveness anchor. If an ordinal is added above without a
// row in the table, this index goes negative and the build fails.
var _ = [1]struct{}{}[len(semanticExternalEffectStateTable)-int(semanticExternalEffectStateCount)]

// semanticExternalEffectStatesWhere lists the states satisfying a predicate, in
// declaration order so generated SQL is stable across builds.
func semanticExternalEffectStatesWhere(match func(semanticExternalEffectStateFacts) bool) []SemanticExternalEffectState {
	states := make([]SemanticExternalEffectState, 0, len(semanticExternalEffectStateTable))
	for _, facts := range semanticExternalEffectStateTable {
		if match(facts) {
			states = append(states, facts.State)
		}
	}
	return states
}

// semanticExternalEffectStateSQLList renders states as a SQL IN-list literal.
// The values are compile-time constants from the table above, never caller
// input, so quoting them here cannot become an injection path.
func semanticExternalEffectStateSQLList(states []SemanticExternalEffectState) string {
	quoted := make([]string, 0, len(states))
	for _, state := range states {
		quoted = append(quoted, "'"+string(state)+"'")
	}
	return strings.Join(quoted, ",")
}

// semanticExternalEffectStateCheckConstraint is the table CHECK, derived from
// the same declaration the reconcilers read. A state the code knows about and
// the schema rejects is a write that fails at runtime for no reason a reader
// could have predicted.
func semanticExternalEffectStateCheckConstraint() string {
	return fmt.Sprintf("CHECK(state IN (%s))", semanticExternalEffectStateSQLList(
		semanticExternalEffectStatesWhere(func(semanticExternalEffectStateFacts) bool { return true }),
	))
}

// semanticReceiptLeaseSweptStatesSQL is the state filter for
// ReconcileExpiredReceiptWaits.
func semanticReceiptLeaseSweptStatesSQL() string {
	return semanticExternalEffectStateSQLList(semanticExternalEffectStatesWhere(
		func(facts semanticExternalEffectStateFacts) bool { return facts.ExpiresOnReceiptLease },
	))
}

// semanticOperationLeaseSweptStatesSQL is the state filter for
// ReconcileExpiredOperations.
func semanticOperationLeaseSweptStatesSQL() string {
	return semanticExternalEffectStateSQLList(semanticExternalEffectStatesWhere(
		func(facts semanticExternalEffectStateFacts) bool { return facts.ExpiresOnOperationLease },
	))
}
