package tool

import (
	"path/filepath"
	"testing"
	"time"
)

// awaitingReceiptLeaseFixture parks one external effect on a receipt that will
// never come, which is where every receipt-bound effect ends up in a
// deployment with no channel integration attached.
func awaitingReceiptLeaseFixture(t *testing.T, root string) (*SQLiteSemanticExecutionCoordinator, InvocationScope, SemanticExternalEffectOperation) {
	t.Helper()
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	scope, plan, admission := outboxAdmittedFixture(t, coordinator, root)
	selection := plan.Selections[0]
	operation := SemanticExternalEffectOperation{
		OperationKey: root + "-operation", Scope: scope, TenantID: "tenant", UserID: "user",
		SelectionID: selection.ID, SelectionDigest: selectionPurposeDigest(selection),
		BindingID: selection.Provider.StableID(), RequestDigest: "canonical-request",
	}
	if _, execute, err := coordinator.PrepareExternalEffect(admission, operation); err != nil || !execute {
		t.Fatalf("prepare execute=%v err=%v", execute, err)
	}
	if _, err := coordinator.CompleteExternalEffectDispatch(admission, operation.OperationKey, SemanticExternalEffectAwaitingReceipt, "dispatched", "awaiting_gateway_receipt", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return coordinator, scope, operation
}

// awaiting_receipt had no exit at all: the running-lease reconciler ignores
// it, a receipt only arrives if something sends one, and manual resolution
// refuses to race a live expectation. This is what ends the wait.
func TestExpiredReceiptWaitBecomesUnknownAndNothingElseDoes(t *testing.T) {
	coordinator, scope, operation := awaitingReceiptLeaseFixture(t, "root-receipt-lease")

	// The running-lease reconciler is the one that already existed. Proving it
	// leaves this operation alone is the whole reason the new one has to exist.
	if changed, err := coordinator.ReconcileStaleExternalEffects(time.Now().UTC().Add(48*time.Hour), time.Minute); err != nil || changed != 0 {
		t.Fatalf("running-lease reconcile changed=%d err=%v", changed, err)
	}
	if stored, err := coordinator.ExternalEffectOperation(operation.OperationKey); err != nil || stored.State != SemanticExternalEffectAwaitingReceipt {
		t.Fatalf("operation=%#v err=%v", stored, err)
	}

	// A wait that is still inside its lease is a wait, not a failure.
	if changed, err := coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC(), ExternalEffectReceiptLease); err != nil || changed != 0 {
		t.Fatalf("early reconcile changed=%d err=%v", changed, err)
	}
	if stored, err := coordinator.ExternalEffectOperation(operation.OperationKey); err != nil || stored.State != SemanticExternalEffectAwaitingReceipt {
		t.Fatalf("operation converged early=%#v err=%v", stored, err)
	}

	changed, err := coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC().Add(25*time.Hour), ExternalEffectReceiptLease)
	if err != nil || changed != 1 {
		t.Fatalf("expired reconcile changed=%d err=%v", changed, err)
	}
	stored, err := coordinator.ExternalEffectOperation(operation.OperationKey)
	if err != nil || stored.State != SemanticExternalEffectUnknown || stored.ReasonCode != "receipt_lease_expired" {
		t.Fatalf("converged operation=%#v err=%v", stored, err)
	}
	// The execution has to move with it, or replay would keep reporting a
	// wait that the ledger has already given up on.
	if record, err := coordinator.Executions.Execution(scope, operation.SelectionID); err != nil || record.State != PlanExecutionUnknown {
		t.Fatalf("converged execution=%#v err=%v", record, err)
	}
	// Converging is idempotent and monotone: a second pass finds nothing.
	if changed, err := coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC().Add(48*time.Hour), ExternalEffectReceiptLease); err != nil || changed != 0 {
		t.Fatalf("second reconcile changed=%d err=%v", changed, err)
	}
}

// Converging gives up the expectation, not the operation. This is the property
// that makes it safe: a receipt that shows up late still settles exactly as it
// would have before the lease expired.
func TestExpiredReceiptWaitStillAcceptsALateReceipt(t *testing.T) {
	coordinator, scope, operation := awaitingReceiptLeaseFixture(t, "root-receipt-late")

	if changed, err := coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC().Add(25*time.Hour), ExternalEffectReceiptLease); err != nil || changed != 1 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	settled, err := coordinator.SettleExternalEffectReceipt(scope, operation.SelectionID, operation.SelectionDigest, operation.BindingID, operation.OperationKey,
		SemanticExternalEffectSucceeded, "receipt-digest", "gateway_accepted", time.Now().UTC().Add(26*time.Hour))
	if err != nil || settled.State != SemanticExternalEffectSucceeded {
		t.Fatalf("late receipt settled=%#v err=%v", settled, err)
	}
	if record, err := coordinator.Executions.Execution(scope, operation.SelectionID); err != nil || record.State != PlanExecutionSucceeded {
		t.Fatalf("late-settled execution=%#v err=%v", record, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || !completed[operation.SelectionID] {
		t.Fatalf("late-settled completion=%#v err=%v", completed, err)
	}
}

// A settled operation is not waiting for anything, so the lease must not touch
// it however long ago it settled.
func TestReceiptLeaseLeavesSettledOperationsAlone(t *testing.T) {
	coordinator, scope, operation := awaitingReceiptLeaseFixture(t, "root-receipt-settled")

	if _, err := coordinator.SettleExternalEffectReceipt(scope, operation.SelectionID, operation.SelectionDigest, operation.BindingID, operation.OperationKey,
		SemanticExternalEffectSucceeded, "receipt-digest", "gateway_accepted", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if changed, err := coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC().Add(72*time.Hour), ExternalEffectReceiptLease); err != nil || changed != 0 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	if stored, err := coordinator.ExternalEffectOperation(operation.OperationKey); err != nil || stored.State != SemanticExternalEffectSucceeded {
		t.Fatalf("settled operation=%#v err=%v", stored, err)
	}
}

func TestReceiptLeaseRejectsANonPositiveWindow(t *testing.T) {
	coordinator, _, _ := awaitingReceiptLeaseFixture(t, "root-receipt-window")
	if _, err := coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC(), 0); err == nil {
		t.Fatal("a zero lease was accepted")
	}
	if _, err := coordinator.ReconcileExpiredReceiptWaits(time.Now().UTC(), -time.Hour); err == nil {
		t.Fatal("a negative lease was accepted")
	}
}
