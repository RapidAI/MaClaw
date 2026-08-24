package tool

import (
	"path/filepath"
	"testing"
	"time"
)

func staleDeliveryFixture(t *testing.T, root string) (*SQLiteSemanticExecutionCoordinator, InvocationScope, string) {
	t.Helper()
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = coordinator.Close() })
	scope, plan, _, _ := outboxDeliveryFixture(t, coordinator, root)
	selectionID := plan.Selections[0].ID
	if _, claimed, err := coordinator.ClaimDelivery(scope, selectionID, time.Now().UTC()); err != nil || !claimed {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}
	return coordinator, scope, selectionID
}

// A dispatch whose worker died is reconciled to unknown. That much is right.
// What was wrong is everything after: the selection execution was left behind
// at awaiting_receipt with nothing able to move it, and the delivery itself
// would refuse the receipt if the channel ever produced one.
func TestStaleDeliveryReconciliationLeavesNothingStranded(t *testing.T) {
	coordinator, scope, selectionID := staleDeliveryFixture(t, "root-delivery-stranded")

	later := time.Now().UTC().Add(30 * time.Minute)
	changed, err := coordinator.ReconcileStaleDeliveryDispatches(later, DeliveryDispatchLease)
	if err != nil || changed != 1 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	if record, err := coordinator.Artifacts.Delivery(scope, selectionID); err != nil || record.State != DeliveryUnknown {
		t.Fatalf("reconciled delivery=%#v err=%v", record, err)
	}
	// The execution has to travel with the delivery. Leaving it at
	// awaiting_receipt strands it: the delivery lease has already fired, so
	// nothing will look at this row again.
	record, err := coordinator.Executions.Execution(scope, selectionID)
	if err != nil || record.State != PlanExecutionUnknown {
		t.Fatalf("stranded execution=%#v err=%v", record, err)
	}
}

// Giving up on observing a dispatch is not the same as knowing it failed, so a
// receipt that turns up afterwards must still be accepted. This mirrors the
// external-effect path, where settlement takes both awaiting_receipt and
// unknown as starting points.
func TestUnknownDeliveryStillAcceptsALateReceipt(t *testing.T) {
	coordinator, scope, selectionID := staleDeliveryFixture(t, "root-delivery-late")

	later := time.Now().UTC().Add(30 * time.Minute)
	if _, err := coordinator.ReconcileStaleDeliveryDispatches(later, DeliveryDispatchLease); err != nil {
		t.Fatal(err)
	}
	settled, err := coordinator.SettleDelivery(scope, selectionID, DeliveryAccepted, "channel-receipt", "accepted", later)
	if err != nil || settled.State != DeliveryAccepted {
		t.Fatalf("late receipt settled=%#v err=%v", settled, err)
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != PlanExecutionSucceeded {
		t.Fatalf("late-settled execution=%#v err=%v", record, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || !completed[selectionID] {
		t.Fatalf("late-settled completion=%#v err=%v", completed, err)
	}
}

// A late failure notice must land too, and must not be mistaken for success.
func TestUnknownDeliveryAcceptsALateFailure(t *testing.T) {
	coordinator, scope, selectionID := staleDeliveryFixture(t, "root-delivery-late-fail")

	later := time.Now().UTC().Add(30 * time.Minute)
	if _, err := coordinator.ReconcileStaleDeliveryDispatches(later, DeliveryDispatchLease); err != nil {
		t.Fatal(err)
	}
	settled, err := coordinator.SettleDelivery(scope, selectionID, DeliveryFailed, "", "channel_rejected", later)
	if err != nil || settled.State != DeliveryFailed {
		t.Fatalf("late failure settled=%#v err=%v", settled, err)
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != PlanExecutionFailed {
		t.Fatalf("late-failed execution=%#v err=%v", record, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || completed[selectionID] {
		t.Fatalf("a failed delivery completed the route=%#v err=%v", completed, err)
	}
}

// Two deliveries can both read state='unknown' and mean opposite things. A
// lapsed lease means nobody was watching, so a receipt is still possible. A
// fire worker's own unknown is its final answer about a channel that issues no
// receipts, so a later "acceptance" could not have come from anywhere real.
//
// The column cannot tell them apart, which is the entire reason the origin is
// recorded. This test exists to fail the day someone decides one unknown is
// enough.
func TestTheTwoDeliveryUnknownsAreNotTheSameFact(t *testing.T) {
	lapsed, lapsedScope, lapsedSelection := staleDeliveryFixture(t, "root-unknown-lapsed")
	later := time.Now().UTC().Add(30 * time.Minute)
	if _, err := lapsed.ReconcileStaleDeliveryDispatches(later, DeliveryDispatchLease); err != nil {
		t.Fatal(err)
	}

	decided, decidedScope, decidedSelection := staleDeliveryFixture(t, "root-unknown-decided")
	if record, err := decided.SettleStandaloneDelivery(decidedScope, decidedSelection, DeliveryUnknown, "", "channel_gives_no_receipt", later); err != nil || record.State != DeliveryUnknown {
		t.Fatalf("worker-settled unknown=%#v err=%v", record, err)
	}

	lapsedRecord, err := lapsed.Artifacts.Delivery(lapsedScope, lapsedSelection)
	if err != nil {
		t.Fatal(err)
	}
	decidedRecord, err := decided.Artifacts.Delivery(decidedScope, decidedSelection)
	if err != nil {
		t.Fatal(err)
	}
	if lapsedRecord.State != decidedRecord.State {
		t.Skipf("the two unknowns no longer share a state (%q vs %q); this test only has a point while they do",
			lapsedRecord.State, decidedRecord.State)
	}

	if _, err := lapsed.SettleDelivery(lapsedScope, lapsedSelection, DeliveryAccepted, "channel-receipt", "accepted", later); err != nil {
		t.Fatalf("a lapsed lease refused a real receipt: %v", err)
	}
	if _, err := decided.SettleStandaloneDelivery(decidedScope, decidedSelection, DeliveryAccepted, "invented-receipt", "accepted", later); err == nil {
		t.Fatal("a worker's final unknown was overwritten by an acceptance no channel could have produced")
	}
}

// Reconciliation stays idempotent and must not disturb a delivery that was
// already settled before the lease fired.
func TestStaleDeliveryReconciliationLeavesSettledDeliveriesAlone(t *testing.T) {
	coordinator, scope, selectionID := staleDeliveryFixture(t, "root-delivery-settled")

	if _, err := coordinator.SettleDelivery(scope, selectionID, DeliveryAccepted, "channel-receipt", "accepted", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	changed, err := coordinator.ReconcileStaleDeliveryDispatches(time.Now().UTC().Add(72*time.Hour), DeliveryDispatchLease)
	if err != nil || changed != 0 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", record, err)
	}
}
