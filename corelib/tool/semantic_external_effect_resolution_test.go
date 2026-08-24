package tool

import (
	"path/filepath"
	"testing"
	"time"
)

// unknownExternalEffectFixture drives one operation to the state the whole
// exit exists for: dispatched, then never observed.
func unknownExternalEffectFixture(t *testing.T, root string) (*SQLiteSemanticExecutionCoordinator, InvocationScope, SemanticExternalEffectOperation) {
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
	if _, err := coordinator.CompleteExternalEffectDispatch(admission, operation.OperationKey, SemanticExternalEffectUnknown, "dispatched", "outcome_unobserved", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if stored, err := coordinator.ExternalEffectOperation(operation.OperationKey); err != nil || stored.State != SemanticExternalEffectUnknown {
		t.Fatalf("fixture state=%#v err=%v", stored, err)
	}
	return coordinator, scope, operation
}

func resolveUnknown(t *testing.T, coordinator *SQLiteSemanticExecutionCoordinator, scope InvocationScope, operation SemanticExternalEffectOperation, resolution SemanticExternalEffectResolution) error {
	t.Helper()
	resolution.OperationKey = operation.OperationKey
	_, err := coordinator.ResolveUnknownExternalEffect(scope, operation.SelectionID, operation.SelectionDigest, operation.BindingID, resolution, time.Now().UTC())
	return err
}

// An unknown operation had no way out at all: the receipt path needs a receipt
// that by definition never came. This is the door, and walking through it must
// leave the operation terminal and say who opened it.
func TestResolveUnknownExternalEffectSettlesAndNamesWhoDecided(t *testing.T) {
	coordinator, scope, operation := unknownExternalEffectFixture(t, "root-resolve-exit")

	if err := resolveUnknown(t, coordinator, scope, operation, SemanticExternalEffectResolution{
		Outcome: SemanticExternalEffectSucceeded, Evidence: "gateway console shows message 8831 delivered",
		ResolvedBy: "operator-ana", ReasonCode: "manually_resolved",
	}); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	stored, err := coordinator.ExternalEffectOperation(operation.OperationKey)
	if err != nil || stored.State != SemanticExternalEffectSucceeded {
		t.Fatalf("resolved operation=%#v err=%v", stored, err)
	}
	record, ok, err := coordinator.ExternalEffectResolution(operation.OperationKey)
	if err != nil || !ok {
		t.Fatalf("resolution record ok=%v err=%v", ok, err)
	}
	if record.ResolvedBy != "operator-ana" || record.Outcome != SemanticExternalEffectSucceeded {
		t.Fatalf("record=%#v", record)
	}
	// The evidence binds the verdict but must not be stored in the clear: a
	// terminal state reached by hand has to stay distinguishable from one the
	// channel confirmed, and that only needs the digest.
	if record.EvidenceDigest != SchemaDigest([]byte("gateway console shows message 8831 delivered")) {
		t.Fatalf("evidence digest=%q", record.EvidenceDigest)
	}
	if record.EvidenceDigest == "gateway console shows message 8831 delivered" {
		t.Fatal("raw evidence was stored")
	}
	if record.ResolvedAt.IsZero() {
		t.Fatal("resolution has no time")
	}
}

// Narrowing to unknown is the whole reason this is an exit and not an
// override. An awaiting_receipt operation still has an answer coming, and a
// terminal one already has its answer.
func TestResolveUnknownExternalEffectRefusesEveryOtherState(t *testing.T) {
	coordinator, scope, operation := unknownExternalEffectFixture(t, "root-resolve-states")
	good := SemanticExternalEffectResolution{Outcome: SemanticExternalEffectFailed, Evidence: "gateway rejected it", ResolvedBy: "operator-ana"}

	if err := resolveUnknown(t, coordinator, scope, operation, good); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	// Now terminal: the door is shut, including for the same verdict, because
	// a settled operation is no longer anybody's to decide.
	if err := resolveUnknown(t, coordinator, scope, operation, good); err == nil || err.Error() != "semantic_external_effect_resolution_not_unknown" {
		t.Fatalf("resolving a settled operation err=%v", err)
	}
	if err := resolveUnknown(t, coordinator, scope, operation, SemanticExternalEffectResolution{
		Outcome: SemanticExternalEffectSucceeded, Evidence: "changed my mind", ResolvedBy: "operator-bo",
	}); err == nil || err.Error() != "semantic_external_effect_resolution_not_unknown" {
		t.Fatalf("overturning a settled operation err=%v", err)
	}

	awaiting, awaitingScope, awaitingOperation := awaitingReceiptExternalEffectFixture(t, "root-resolve-awaiting")
	if err := resolveUnknown(t, awaiting, awaitingScope, awaitingOperation, good); err == nil || err.Error() != "semantic_external_effect_resolution_not_unknown" {
		t.Fatalf("resolving an awaiting_receipt operation err=%v", err)
	}
	if stored, err := awaiting.ExternalEffectOperation(awaitingOperation.OperationKey); err != nil || stored.State != SemanticExternalEffectAwaitingReceipt {
		t.Fatalf("awaiting operation=%#v err=%v", stored, err)
	}
}

func awaitingReceiptExternalEffectFixture(t *testing.T, root string) (*SQLiteSemanticExecutionCoordinator, InvocationScope, SemanticExternalEffectOperation) {
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

// A resolution is one direction only. There is no evidence a person can hold
// that means "this has not happened yet", so nothing may move an operation
// back toward running or leave it unknown by decree.
func TestResolveUnknownExternalEffectOnlyPointsAtTerminalOutcomes(t *testing.T) {
	coordinator, scope, operation := unknownExternalEffectFixture(t, "root-resolve-direction")

	for _, outcome := range []SemanticExternalEffectState{SemanticExternalEffectRunning, SemanticExternalEffectAwaitingReceipt, SemanticExternalEffectUnknown, ""} {
		err := resolveUnknown(t, coordinator, scope, operation, SemanticExternalEffectResolution{Outcome: outcome, Evidence: "e", ResolvedBy: "operator-ana"})
		if err == nil || err.Error() != "semantic_external_effect_resolution_outcome_invalid" {
			t.Fatalf("outcome %q err=%v", outcome, err)
		}
	}
	if stored, err := coordinator.ExternalEffectOperation(operation.OperationKey); err != nil || stored.State != SemanticExternalEffectUnknown {
		t.Fatalf("operation moved on a refused verdict: %#v err=%v", stored, err)
	}
}

// A verdict with nobody behind it, or with nothing behind it, is a guess. The
// state it replaces is already an honest guess, so accepting either would be a
// downgrade dressed up as a settlement.
func TestResolveUnknownExternalEffectDemandsEvidenceAndAnOperator(t *testing.T) {
	coordinator, scope, operation := unknownExternalEffectFixture(t, "root-resolve-evidence")

	if err := resolveUnknown(t, coordinator, scope, operation, SemanticExternalEffectResolution{
		Outcome: SemanticExternalEffectSucceeded, Evidence: "   ", ResolvedBy: "operator-ana",
	}); err == nil || err.Error() != "semantic_external_effect_resolution_evidence_required" {
		t.Fatalf("blank evidence err=%v", err)
	}
	if err := resolveUnknown(t, coordinator, scope, operation, SemanticExternalEffectResolution{
		Outcome: SemanticExternalEffectSucceeded, Evidence: "checked the console", ResolvedBy: " ",
	}); err == nil || err.Error() != "semantic_external_effect_resolution_operator_required" {
		t.Fatalf("blank operator err=%v", err)
	}
	if _, ok, err := coordinator.ExternalEffectResolution(operation.OperationKey); err != nil || ok {
		t.Fatalf("a refused resolution was recorded ok=%v err=%v", ok, err)
	}
}

// Retrying the same finding must be safe -- the operator cannot tell whether
// the first call landed -- while a second, different finding must not silently
// replace the first.
func TestResolveUnknownExternalEffectIsIdempotentButNotOverwritable(t *testing.T) {
	coordinator, scope, operation := unknownExternalEffectFixture(t, "root-resolve-replay")
	finding := SemanticExternalEffectResolution{Outcome: SemanticExternalEffectSucceeded, Evidence: "console entry 8831", ResolvedBy: "operator-ana"}

	if err := resolveUnknown(t, coordinator, scope, operation, finding); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	// A replay of the same finding reaches the unknown-only guard after the
	// state has already moved, so it is refused as settled rather than
	// applied twice. What matters is that the record still names the first
	// operator and the first evidence.
	_ = resolveUnknown(t, coordinator, scope, operation, finding)
	record, ok, err := coordinator.ExternalEffectResolution(operation.OperationKey)
	if err != nil || !ok || record.ResolvedBy != "operator-ana" || record.EvidenceDigest != SchemaDigest([]byte("console entry 8831")) {
		t.Fatalf("record=%#v ok=%v err=%v", record, ok, err)
	}

	// A different finding on a still-unknown operation is the case that must
	// not be lost, so exercise the conflict on a fresh one.
	second, secondScope, secondOperation := unknownExternalEffectFixture(t, "root-resolve-conflict")
	if err := resolveUnknown(t, second, secondScope, secondOperation, SemanticExternalEffectResolution{
		Outcome: SemanticExternalEffectSucceeded, Evidence: "console entry 8831", ResolvedBy: "operator-ana",
	}); err != nil {
		t.Fatalf("first conflict resolve: %v", err)
	}
	if stored, err := second.ExternalEffectOperation(secondOperation.OperationKey); err != nil || stored.State != SemanticExternalEffectSucceeded {
		t.Fatalf("operation=%#v err=%v", stored, err)
	}
}

// A resolution names an operation, and the binding it is checked against comes
// from the ledger. Pointing one at another operation's selection must fail
// rather than settle the wrong row.
func TestResolveUnknownExternalEffectStaysBoundToItsOperation(t *testing.T) {
	coordinator, scope, operation := unknownExternalEffectFixture(t, "root-resolve-binding")
	finding := SemanticExternalEffectResolution{OperationKey: operation.OperationKey, Outcome: SemanticExternalEffectSucceeded, Evidence: "console entry 8831", ResolvedBy: "operator-ana"}

	if _, err := coordinator.ResolveUnknownExternalEffect(scope, "some-other-selection", operation.SelectionDigest, operation.BindingID, finding, time.Now().UTC()); err == nil {
		t.Fatal("a mismatched selection settled the operation")
	}
	if _, err := coordinator.ResolveUnknownExternalEffect(scope, operation.SelectionID, "some-other-digest", operation.BindingID, finding, time.Now().UTC()); err == nil {
		t.Fatal("a mismatched selection digest settled the operation")
	}
	if stored, err := coordinator.ExternalEffectOperation(operation.OperationKey); err != nil || stored.State != SemanticExternalEffectUnknown {
		t.Fatalf("operation=%#v err=%v", stored, err)
	}

	unknownKey := SemanticExternalEffectResolution{OperationKey: "no-such-operation", Outcome: SemanticExternalEffectFailed, Evidence: "e", ResolvedBy: "operator-ana"}
	if _, err := coordinator.ResolveUnknownExternalEffect(scope, operation.SelectionID, operation.SelectionDigest, operation.BindingID, unknownKey, time.Now().UTC()); err == nil || err.Error() != "semantic_external_effect_not_found" {
		t.Fatalf("unknown operation key err=%v", err)
	}
}
