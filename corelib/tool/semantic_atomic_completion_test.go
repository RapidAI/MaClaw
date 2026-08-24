package tool

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func atomicCompletionFixture(t *testing.T) (*SQLiteSemanticExecutionCoordinator, ToolPlan, InvocationScope, InvocationGrant) {
	t.Helper()
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	plan, scope, grant := routeStateTestPlan(t)
	if _, err := coordinator.Routes.Open(scope, plan, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return coordinator, plan, scope, grant
}

func acquireRunningExecution(t *testing.T, store *SQLitePlanExecutionStore, scope InvocationScope, selectionID string) {
	t.Helper()
	if _, admitted, err := store.Acquire(PlanExecutionRecord{Scope: scope, SelectionID: selectionID, StartedAt: time.Now().UTC()}); err != nil || !admitted {
		t.Fatalf("acquire admitted=%v err=%v", admitted, err)
	}
}

// TestSucceedingASelectionCommitsItsRouteCompletionInTheSameTransaction is the
// property the split writes could not offer: after a completion fails, the two
// durable views still agree. Recording success and recording the completion it
// projects either both happen or neither does.
func TestSucceedingASelectionCommitsItsRouteCompletionInTheSameTransaction(t *testing.T) {
	coordinator, plan, scope, _ := atomicCompletionFixture(t)
	completer := newAtomicSelectionCompleter(coordinator.Executions, coordinator.Routes)
	if completer == nil {
		t.Fatal("stores sharing one database should support an atomic completion")
	}

	// A selection the published plan does not contain is the cheapest way to
	// make the route projection reject after the execution row was updated.
	acquireRunningExecution(t, coordinator.Executions, scope, "selection-absent-from-plan")
	if _, err := completer.CompleteSelection(scope, plan.ID, "selection-absent-from-plan", PlanExecutionSucceeded, "digest", "", time.Now().UTC()); err == nil {
		t.Fatal("a selection outside the published plan must not complete")
	}
	record, err := coordinator.Executions.Execution(scope, "selection-absent-from-plan")
	if err != nil {
		t.Fatal(err)
	}
	if record.State != PlanExecutionRunning {
		t.Fatalf("rejected completion left state %q, want the execution untouched", record.State)
	}

	// The same rejection through the sequential writes this replaces: the
	// execution is durably succeeded while the route never recorded it. Without
	// this half the test above would pass even if nothing were rolled back.
	acquireRunningExecution(t, coordinator.Executions, scope, "selection-written-sequentially")
	if _, err := coordinator.Executions.Complete(scope, "selection-written-sequentially", PlanExecutionSucceeded, "digest", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Routes.RecordSelectionCompletion(scope, plan.ID, "selection-written-sequentially", time.Now().UTC()); err == nil {
		t.Fatal("route projection should still reject a selection outside the plan")
	}
	split, err := coordinator.Executions.Execution(scope, "selection-written-sequentially")
	if err != nil {
		t.Fatal(err)
	}
	if split.State != PlanExecutionSucceeded {
		t.Fatalf("sequential writes left state %q; the divergence this test guards no longer reproduces", split.State)
	}
	completed, err := coordinator.Routes.CompletedSelections(scope)
	if err != nil {
		t.Fatal(err)
	}
	if completed["selection-written-sequentially"] {
		t.Fatal("route completion should not exist for the sequentially written selection")
	}
}

// TestAnAdmittedSelectionSucceedsInBothViews keeps the atomic path honest about
// the ordinary case: a real selection still lands in the execution store and in
// the route completion set.
func TestAnAdmittedSelectionSucceedsInBothViews(t *testing.T) {
	coordinator, plan, scope, _ := atomicCompletionFixture(t)
	// The grant must come from the issuer that will validate it: an issuer
	// tracks the nonces it minted, so one issued elsewhere reads as forged.
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("r", 32)))
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("issue grants=%#v err=%v", grants, err)
	}
	grant := grants[0]
	executor, err := NewPlanExecutorWithRouteState(issuer, coordinator.Executions, coordinator.Routes)
	if err != nil {
		t.Fatal(err)
	}
	if executor.atomic == nil {
		t.Fatal("an executor over coordinator stores should complete atomically")
	}
	result, selection, err := executor.Execute(grant, scope, plan, nil, func(PlannedSelection) SelectionExecutionResult {
		return SelectionExecutionResult{Result: "ok", Succeeded: true}
	})
	if err != nil || !result.Succeeded {
		t.Fatalf("execute result=%+v err=%v", result, err)
	}
	record, err := coordinator.Executions.Execution(scope, selection.ID)
	if err != nil || record.State != PlanExecutionSucceeded {
		t.Fatalf("execution record=%+v err=%v", record, err)
	}
	completed, err := coordinator.Routes.CompletedSelections(scope)
	if err != nil {
		t.Fatal(err)
	}
	if !completed[selection.ID] {
		t.Fatalf("route completion missing for %q", selection.ID)
	}
}

// TestReceiptSettlementAlsoCommitsBothViewsTogether covers the other writer of
// a terminal success: a trusted transport receipt arriving long after dispatch.
func TestReceiptSettlementAlsoCommitsBothViewsTogether(t *testing.T) {
	coordinator, plan, scope, _ := atomicCompletionFixture(t)
	completer := newAtomicSelectionCompleter(coordinator.Executions, coordinator.Routes)
	if completer == nil {
		t.Fatal("stores sharing one database should support an atomic settlement")
	}
	acquireRunningExecution(t, coordinator.Executions, scope, "selection-absent-from-plan")
	if _, err := coordinator.Executions.Complete(scope, "selection-absent-from-plan", PlanExecutionAwaitingReceipt, "digest", "awaiting", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := completer.SettleSelectionReceipt(scope, plan.ID, "selection-absent-from-plan", PlanExecutionSucceeded, "digest", "", time.Now().UTC()); err == nil {
		t.Fatal("a settlement whose route projection fails must not commit")
	}
	record, err := coordinator.Executions.Execution(scope, "selection-absent-from-plan")
	if err != nil {
		t.Fatal(err)
	}
	if record.State != PlanExecutionAwaitingReceipt {
		t.Fatalf("rejected settlement left state %q, want awaiting_receipt", record.State)
	}
}

// TestStoresThatCannotShareATransactionDoNotClaimAtomicity keeps the detection
// honest. Reporting a completer for stores with no common transaction would
// promise a guarantee the commit cannot keep.
func TestStoresThatCannotShareATransactionDoNotClaimAtomicity(t *testing.T) {
	coordinator, _, _, _ := atomicCompletionFixture(t)
	separate, err := NewSQLiteRouteStateStore(filepath.Join(t.TempDir(), "route-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer separate.Close()

	for name, completer := range map[string]atomicSelectionCompleter{
		"memory execution store":  newAtomicSelectionCompleter(NewMemoryPlanExecutionStore(), coordinator.Routes),
		"memory route store":      newAtomicSelectionCompleter(coordinator.Executions, NewMemoryRouteStateStore()),
		"separate route database": newAtomicSelectionCompleter(coordinator.Executions, separate),
	} {
		if completer != nil {
			t.Fatalf("%s cannot share a transaction, so it must not report one", name)
		}
	}
}
