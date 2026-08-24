package tool

import (
	"path/filepath"
	"testing"
	"time"
)

// Channel deliveries park their plan execution in awaiting_receipt too, and
// they are settled by a different path with its own lease. The external-effect
// receipt lease must therefore converge executions one operation at a time
// rather than sweeping every awaiting_receipt row it finds.
//
// A blanket sweep passes every other test in this package, which is exactly
// why this one exists: the damage only shows up when the delivery is later
// settled and finds its execution already moved out from under it.
func TestReceiptLeaseDoesNotStealAChannelDeliverysExecution(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	scope, plan, _, _ := outboxDeliveryFixture(t, coordinator, "root-lease-delivery")
	selectionID := plan.Selections[0].ID
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != PlanExecutionAwaitingReceipt {
		t.Fatalf("prepared delivery execution=%#v err=%v", record, err)
	}
	if _, claimed, err := coordinator.ClaimDelivery(scope, selectionID, time.Now().UTC()); err != nil || !claimed {
		t.Fatalf("claim claimed=%v err=%v", claimed, err)
	}

	// Long enough that a blanket sweep would certainly pick the row up. There
	// is no external effect operation here at all, so a correct lease has
	// nothing to do.
	later := time.Now().UTC().Add(72 * time.Hour)
	changed, err := coordinator.ReconcileExpiredReceiptWaits(later, ExternalEffectReceiptLease)
	if err != nil || changed != 0 {
		t.Fatalf("lease changed=%d err=%v", changed, err)
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != PlanExecutionAwaitingReceipt {
		t.Fatalf("the lease moved a delivery's execution=%#v err=%v", record, err)
	}

	// The real proof: the delivery's own settlement still lands. If the lease
	// had converged this execution, the accepted receipt below would be
	// rejected with selection_execution_not_awaiting_receipt and a delivery
	// that genuinely succeeded would be unsettleable.
	settled, err := coordinator.SettleDelivery(scope, selectionID, DeliveryAccepted, "channel-receipt", "accepted", later)
	if err != nil || settled.State != DeliveryAccepted {
		t.Fatalf("delivery settlement=%#v err=%v", settled, err)
	}
	if record, err := coordinator.Executions.Execution(scope, selectionID); err != nil || record.State != PlanExecutionSucceeded {
		t.Fatalf("settled delivery execution=%#v err=%v", record, err)
	}
}
