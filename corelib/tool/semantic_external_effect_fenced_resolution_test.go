package tool

import (
	"strings"
	"testing"
	"time"
)

// supersededUnknownFixture leaves one unknown operation behind a route
// revision that has since been replaced.
func supersededUnknownFixture(t *testing.T, root string) (*SQLiteSemanticExecutionCoordinator, InvocationScope, SemanticExternalEffectOperation) {
	t.Helper()
	coordinator, scope, operation := unknownExternalEffectFixture(t, root)
	plan, err := coordinator.Routes.PublishedPlan(scope)
	if err != nil {
		t.Fatal(err)
	}
	publishChildRevision(t, coordinator, scope, plan)
	return coordinator, scope, operation
}

// An operation can end unknown and then have the plan behind it replaced. The
// fencing token then refuses the settlement, which is right for a receipt and
// wrong for a person: it left the operator's verdict recorded in the
// resolutions table and the operation itself unknown forever, with every retry
// failing the same way.
//
// The side effect really happened. Declining to write down what it was
// protected nothing, because the scope carries the superseded plan id, so
// every row this touches belongs to the old revision.
func TestASupersededRouteNoLongerTrapsAnUnknownOperation(t *testing.T) {
	coordinator, scope, operation := supersededUnknownFixture(t, "root-fenced-resolve")

	if err := resolveUnknown(t, coordinator, scope, operation, SemanticExternalEffectResolution{
		Outcome: SemanticExternalEffectFailed, Evidence: "channel console shows the send was rejected",
		ResolvedBy: "operator-ana", ReasonCode: "manually_resolved",
	}); err != nil {
		t.Fatalf("resolve over a superseded route: %v", err)
	}
	stored, err := coordinator.ExternalEffectOperation(operation.OperationKey)
	if err != nil || stored.State != SemanticExternalEffectFailed {
		t.Fatalf("resolved operation=%#v err=%v", stored, err)
	}
	if record, ok, err := coordinator.ExternalEffectResolution(operation.OperationKey); err != nil || !ok || record.ResolvedBy != "operator-ana" {
		t.Fatalf("resolution record=%#v ok=%v err=%v", record, ok, err)
	}
}

// The exception is for a person's verdict only. A receipt claiming to settle a
// superseded claim is exactly what the fencing token exists to stop, and
// widening the exit must not have widened that.
func TestASupersededRouteStillRefusesAReceipt(t *testing.T) {
	coordinator, scope, operation := supersededUnknownFixture(t, "root-fenced-receipt")

	_, err := coordinator.SettleExternalEffectReceipt(scope, operation.SelectionID, operation.SelectionDigest,
		operation.BindingID, operation.OperationKey, SemanticExternalEffectSucceeded, "channel-receipt", "accepted", time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "fencing_stale") {
		t.Fatalf("a receipt settled a superseded claim, err=%v", err)
	}
	if stored, err := coordinator.ExternalEffectOperation(operation.OperationKey); err != nil || stored.State != SemanticExternalEffectUnknown {
		t.Fatalf("operation after refused receipt=%#v err=%v", stored, err)
	}
}

// Resolving over a superseded route records the outcome and stops there. The
// selection belonged to a plan that no longer exists, so completing it would
// be asserting progress on a route nobody is running.
func TestResolvingASupersededRouteCompletesNothing(t *testing.T) {
	coordinator, scope, operation := supersededUnknownFixture(t, "root-fenced-projection")

	if err := resolveUnknown(t, coordinator, scope, operation, SemanticExternalEffectResolution{
		Outcome: SemanticExternalEffectSucceeded, Evidence: "gateway console shows message 8831 delivered",
		ResolvedBy: "operator-ana", ReasonCode: "manually_resolved",
	}); err != nil {
		t.Fatalf("resolve over a superseded route: %v", err)
	}
	if stored, err := coordinator.ExternalEffectOperation(operation.OperationKey); err != nil || stored.State != SemanticExternalEffectSucceeded {
		t.Fatalf("resolved operation=%#v err=%v", stored, err)
	}
	completed, err := coordinator.Routes.CompletedSelections(scope)
	if err != nil {
		t.Fatal(err)
	}
	if completed[operation.SelectionID] {
		t.Fatal("a superseded route was told one of its selections had completed")
	}
}

// A current route is untouched by any of this: resolving still completes the
// selection exactly as it did before.
func TestResolvingACurrentRouteStillCompletesTheSelection(t *testing.T) {
	coordinator, scope, operation := unknownExternalEffectFixture(t, "root-current-projection")

	if err := resolveUnknown(t, coordinator, scope, operation, SemanticExternalEffectResolution{
		Outcome: SemanticExternalEffectSucceeded, Evidence: "gateway console shows message 8831 delivered",
		ResolvedBy: "operator-ana", ReasonCode: "manually_resolved",
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	completed, err := coordinator.Routes.CompletedSelections(scope)
	if err != nil || !completed[operation.SelectionID] {
		t.Fatalf("current route completion=%#v err=%v", completed, err)
	}
}
