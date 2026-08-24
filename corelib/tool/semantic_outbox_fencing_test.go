package tool

import (
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// outboxAdmittedFixture builds a published delivery-capable plan and one
// admitted host call, returning before any outbox record is prepared.
func outboxAdmittedFixture(t *testing.T, coordinator *SQLiteSemanticExecutionCoordinator, rootTaskID string) (InvocationScope, ToolPlan, SemanticExecutionAdmission) {
	t.Helper()
	registry := semanticRegistry(t)
	provider := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: rootTaskID, SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: rootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("o", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	admission := SemanticExecutionAdmission{Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "outbox"}, Grant: grants[0], RequestDigest: "request:outbox", Scope: scope, Selection: plan.Selections[0], Now: time.Now().UTC()}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	return scope, plan, admission
}

// outboxDeliveryFixture extends outboxAdmittedFixture with one published
// payload and one prepared delivery outbox record.
func outboxDeliveryFixture(t *testing.T, coordinator *SQLiteSemanticExecutionCoordinator, rootTaskID string) (InvocationScope, ToolPlan, SemanticExecutionAdmission, ArtifactPayload) {
	t.Helper()
	scope, plan, admission := outboxAdmittedFixture(t, coordinator, rootTaskID)
	payload, err := NewArtifactPayload(scope, "selection:producer", "document", "text/plain", "cGF5bG9hZA==", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Artifacts.Publish(payload); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coordinator.PrepareDeliveryAndComplete(admission, DeliveryRecord{Scope: scope, SelectionID: plan.Selections[0].ID, ArtifactID: payload.Ref.ID, ArtifactSourceScope: scope, ChannelScope: "test-channel", DestinationID: "group:one", State: DeliveryPrepared}, "prepared", "channel_delivery_prepared", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return scope, plan, admission, payload
}

// publishChildRevision advances the root/session/principal lineage one
// revision, superseding the fixture plan.
func publishChildRevision(t *testing.T, coordinator *SQLiteSemanticExecutionCoordinator, scope InvocationScope, plan ToolPlan) RouteState {
	t.Helper()
	childPlan := revisedRouteStatePlan(t, plan, scope.RootTaskID, "turn-child")
	childScope := scope
	childScope.PlanID, childScope.TurnID = childPlan.ID, "turn-child"
	parent, err := coordinator.Routes.CurrentRevision(scope)
	if err != nil {
		t.Fatal(err)
	}
	child, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: childScope, Plan: childPlan, ExpectedParent: &parent, SnapshotDigest: childPlan.SnapshotDigest}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return child
}

func TestOutboxClaimIsCompareAndSetAndStaleConvergesUnknown(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	scope, plan, _, _ := outboxDeliveryFixture(t, coordinator, "root-cas")
	selectionID := plan.Selections[0].ID
	// Out-of-order settle before any claim is a conflict, never a dispatch.
	if _, err := coordinator.SettleDelivery(scope, selectionID, DeliveryAccepted, "receipt", "accepted", time.Now().UTC()); err == nil || err.Error() != "delivery_outcome_conflict" {
		t.Fatalf("out-of-order settle err=%v", err)
	}
	claim, claimed, err := coordinator.ClaimDeliveryWithHolder(scope, selectionID, "gateway-1", time.Now().UTC())
	if err != nil || !claimed || claim.Delivery.State != DeliveryDispatching || claim.FencingToken == 0 || claim.HolderID != "gateway-1" {
		t.Fatalf("claim=%#v claimed=%v err=%v", claim, claimed, err)
	}
	// A duplicate claim never dispatches twice.
	duplicate, claimed, err := coordinator.ClaimDelivery(scope, selectionID, time.Now().UTC())
	if err != nil || claimed || duplicate.Delivery.State != DeliveryDispatching {
		t.Fatalf("duplicate claim=%#v claimed=%v err=%v", duplicate, claimed, err)
	}
	// A stale dispatch lease converges to unknown and can never be reclaimed.
	changed, err := coordinator.Artifacts.ReconcileStaleDeliveryDispatches(time.Now().UTC().Add(10*time.Minute), 5*time.Minute)
	if err != nil || changed != 1 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	reclaim, claimed, err := coordinator.ClaimDelivery(scope, selectionID, time.Now().UTC())
	if err != nil || claimed || reclaim.Delivery.State != DeliveryUnknown {
		t.Fatalf("reclaim=%#v claimed=%v err=%v", reclaim, claimed, err)
	}
	if record, err := coordinator.SettleDelivery(scope, selectionID, DeliveryUnknown, "", "reconciled", time.Now().UTC()); err != nil || record.State != DeliveryUnknown {
		t.Fatalf("idempotent unknown settle record=%#v err=%v", record, err)
	}
	// Unknown is terminal for the claim, which is the fact that matters: the
	// dispatch can never be reclaimed and sent a second time, as asserted
	// above. It is not terminal for the outcome. Settling records what a
	// channel observed and sends nothing, so a receipt that arrives after the
	// lease lapsed still has to land -- refusing it discarded real answers and
	// left the selection with no way to finish.
	if record, err := coordinator.SettleDelivery(scope, selectionID, DeliveryAccepted, "receipt", "accepted", time.Now().UTC()); err != nil || record.State != DeliveryAccepted {
		t.Fatalf("late receipt after unknown record=%#v err=%v", record, err)
	}
	// Recording that outcome must not have reopened the dispatch.
	if reclaim, claimed, err := coordinator.ClaimDelivery(scope, selectionID, time.Now().UTC()); err != nil || claimed || reclaim.Delivery.State != DeliveryAccepted {
		t.Fatalf("reclaim after late settle=%#v claimed=%v err=%v", reclaim, claimed, err)
	}
}

func TestRouteRevisionFencingTokenIsMonotonic(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	scope, plan, _, _ := outboxDeliveryFixture(t, coordinator, "root-fencing-monotonic")
	parent, err := coordinator.Routes.CurrentRevision(scope)
	if err != nil {
		t.Fatal(err)
	}
	parentState, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, ExpectedParent: nil, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if parentState.FencingToken == 0 {
		t.Fatalf("publish allocated no fencing token: %#v", parentState)
	}
	child := publishChildRevision(t, coordinator, scope, plan)
	if child.FencingToken <= parentState.FencingToken {
		t.Fatalf("fencing token not monotonic: parent=%d child=%d", parentState.FencingToken, child.FencingToken)
	}
	if child.ParentRevision == nil || !sameRouteRevisionRef(*child.ParentRevision, parent) {
		t.Fatalf("child parent=%#v want %#v", child.ParentRevision, parent)
	}
	// The in-memory store mirrors the monotonic contract.
	memory := NewMemoryRouteStateStore()
	first, err := memory.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC())
	if err != nil || first.FencingToken == 0 {
		t.Fatalf("memory publish state=%+v err=%v", first, err)
	}
}

func TestDeliveryClaimFencedOffAfterNewRevision(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	scope, plan, _, _ := outboxDeliveryFixture(t, coordinator, "root-fencing-claim")
	publishChildRevision(t, coordinator, scope, plan)
	// The prepared intent belongs to a superseded revision: the claim must
	// converge it to unknown and never dispatch.
	claim, claimed, err := coordinator.ClaimDelivery(scope, plan.Selections[0].ID, time.Now().UTC())
	if err != nil || claimed || claim.Delivery.State != DeliveryUnknown {
		t.Fatalf("fenced claim=%#v claimed=%v err=%v", claim, claimed, err)
	}
}

func TestDeliverySettleFencedOffAfterNewRevision(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	scope, plan, _, _ := outboxDeliveryFixture(t, coordinator, "root-fencing-settle")
	selectionID := plan.Selections[0].ID
	claim, claimed, err := coordinator.ClaimDelivery(scope, selectionID, time.Now().UTC())
	if err != nil || !claimed || claim.FencingToken == 0 {
		t.Fatalf("claim=%#v claimed=%v err=%v", claim, claimed, err)
	}
	publishChildRevision(t, coordinator, scope, plan)
	// The claim was won under the old revision: every outcome is fenced off.
	if _, err := coordinator.SettleDelivery(scope, selectionID, DeliveryAccepted, "receipt", "accepted", time.Now().UTC()); err == nil || err.Error() != "delivery_fencing_stale" {
		t.Fatalf("stale accepted settle err=%v", err)
	}
	if _, err := coordinator.SettleDelivery(scope, selectionID, DeliveryUnknown, "", "timeout", time.Now().UTC()); err == nil || err.Error() != "delivery_fencing_stale" {
		t.Fatalf("stale unknown settle err=%v", err)
	}
	record, err := coordinator.Artifacts.Delivery(scope, selectionID)
	if err != nil || record.State != DeliveryDispatching {
		t.Fatalf("fenced record=%#v err=%v", record, err)
	}
	// Only lease reconciliation converges the stale claim to unknown.
	changed, err := coordinator.Artifacts.ReconcileStaleDeliveryDispatches(time.Now().UTC().Add(10*time.Minute), 5*time.Minute)
	if err != nil || changed != 1 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	if record, err := coordinator.Artifacts.Delivery(scope, selectionID); err != nil || record.State != DeliveryUnknown {
		t.Fatalf("reconciled record=%#v err=%v", record, err)
	}
}

func TestExternalEffectFencedOffAfterNewRevision(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	scope, plan, admission := outboxAdmittedFixture(t, coordinator, "root-fencing-effect")
	selection := plan.Selections[0]
	operation := SemanticExternalEffectOperation{OperationKey: "operation-fenced", Scope: scope, TenantID: "tenant", UserID: "user", SelectionID: selection.ID, SelectionDigest: selectionPurposeDigest(selection), BindingID: selection.Provider.StableID(), RequestDigest: "canonical-request"}
	prepared, execute, err := coordinator.PrepareExternalEffect(admission, operation)
	if err != nil || !execute || prepared.FencingToken == 0 {
		t.Fatalf("prepare=%#v execute=%v err=%v", prepared, execute, err)
	}
	if _, err := coordinator.CompleteExternalEffectDispatch(admission, operation.OperationKey, SemanticExternalEffectAwaitingReceipt, "dispatched", "awaiting_gateway_receipt", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	publishChildRevision(t, coordinator, scope, plan)
	// A receipt for the superseded revision is fenced off instead of being
	// settled; reconcile/manual resolution owns the stale operation.
	if _, err := coordinator.SettleExternalEffectReceipt(scope, selection.ID, operation.SelectionDigest, operation.BindingID, operation.OperationKey, SemanticExternalEffectSucceeded, "receipt-digest", "gateway_accepted", time.Now().UTC()); err == nil || err.Error() != "semantic_external_effect_fencing_stale" {
		t.Fatalf("stale receipt settlement err=%v", err)
	}
	stored, err := coordinator.ExternalEffectOperation(operation.OperationKey)
	if err != nil || stored.State != SemanticExternalEffectAwaitingReceipt {
		t.Fatalf("fenced operation=%#v err=%v", stored, err)
	}
}

func TestOutboxCrashRecoveryAfterPrepare(t *testing.T) {
	path := filepath.Join(t.TempDir(), "semantic-execution.db")
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(path)
	if err != nil {
		t.Fatal(err)
	}
	scope, plan, _, payload := outboxDeliveryFixture(t, coordinator, "root-crash")
	selectionID := plan.Selections[0].ID
	// Simulate process loss right after prepare: rebuild every store from the
	// same database file.
	if err := coordinator.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteSemanticExecutionCoordinator(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	execution, err := reopened.Executions.Execution(scope, selectionID)
	if err != nil || execution.State != PlanExecutionAwaitingReceipt {
		t.Fatalf("recovered execution=%#v err=%v", execution, err)
	}
	claim, claimed, err := reopened.ClaimDelivery(scope, selectionID, time.Now().UTC())
	if err != nil || !claimed || claim.Payload.Ref.ID != payload.Ref.ID {
		t.Fatalf("recovered claim=%#v claimed=%v err=%v", claim, claimed, err)
	}
	if _, claimed, err := reopened.ClaimDelivery(scope, selectionID, time.Now().UTC()); err != nil || claimed {
		t.Fatalf("recovered duplicate claim claimed=%v err=%v", claimed, err)
	}
	settled, err := reopened.SettleDelivery(scope, selectionID, DeliveryAccepted, "receipt-after-restart", "channel_delivery_accepted", time.Now().UTC())
	if err != nil || settled.State != DeliveryAccepted || settled.ReceiptDigest != "receipt-after-restart" {
		t.Fatalf("recovered settle=%#v err=%v", settled, err)
	}
	if execution, err := reopened.Executions.Execution(scope, selectionID); err != nil || execution.State != PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", execution, err)
	}
}

func TestArtifactStoreQuotaIsFailClosed(t *testing.T) {
	scope := semanticArtifactTestScope()
	store, err := NewSQLiteArtifactStore(filepath.Join(t.TempDir(), "artifacts.db"), WithArtifactQuotaBytes(9))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	first, err := NewArtifactPayload(scope, "selection:one", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("12345")), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(first); err != nil {
		t.Fatal(err)
	}
	// Idempotent republish of the same bytes does not consume quota.
	if _, err := store.Publish(first); err != nil {
		t.Fatalf("idempotent republish err=%v", err)
	}
	second, err := NewArtifactPayload(scope, "selection:one", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("67890")), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(second); err == nil || err.Error() != "artifact_quota_exceeded" {
		t.Fatalf("over-quota publish err=%v", err)
	}
	// A different principal has its own budget.
	otherScope := scope
	otherScope.PrincipalID = "other-principal"
	third, err := NewArtifactPayload(otherScope, "selection:one", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("abc")), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(third); err != nil {
		t.Fatalf("other principal publish err=%v", err)
	}
}

func TestArtifactStoreSweepsExpiredPayloads(t *testing.T) {
	scope := semanticArtifactTestScope()
	now := time.Now().UTC()
	store, err := NewSQLiteArtifactStore(filepath.Join(t.TempDir(), "artifacts.db"), WithArtifactRetention(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	expired, err := NewArtifactPayload(scope, "selection:one", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("old")), now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(expired); err != nil {
		t.Fatal(err)
	}
	fresh, err := NewArtifactPayload(scope, "selection:one", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("new")), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(fresh); err != nil {
		t.Fatal(err)
	}
	inFlight, err := NewArtifactPayload(scope, "selection:two", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("in-flight")), now.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(inFlight); err != nil {
		t.Fatal(err)
	}
	// An old payload referenced by a prepared delivery is retained until the
	// outbox intent settles.
	if _, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver", ArtifactID: inFlight.Ref.ID, ArtifactSourceScope: scope, ChannelScope: "channel", DestinationID: "group:one", State: DeliveryPrepared}); err != nil {
		t.Fatal(err)
	}
	swept, err := store.SweepExpiredArtifacts(now)
	if err != nil || swept != 1 {
		t.Fatalf("swept=%d err=%v", swept, err)
	}
	if _, err := store.byID(scope, expired.Ref.ID); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("expired artifact err=%v", err)
	}
	if _, err := store.byID(scope, fresh.Ref.ID); err != nil {
		t.Fatalf("fresh artifact swept: %v", err)
	}
	if _, err := store.byID(scope, inFlight.Ref.ID); err != nil {
		t.Fatalf("in-flight artifact swept: %v", err)
	}
	// Sweeping requires an explicit retention configuration.
	unconfigured, err := NewSQLiteArtifactStore(filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer unconfigured.Close()
	if _, err := unconfigured.SweepExpiredArtifacts(now); err == nil || err.Error() != "artifact_retention_disabled" {
		t.Fatalf("unconfigured sweep err=%v", err)
	}
}

func TestArtifactStoreEncryptsPayloadsAndReadsLegacyPlaintext(t *testing.T) {
	scope := semanticArtifactTestScope()
	key := []byte(strings.Repeat("k", 32))
	dir := t.TempDir()
	encryptedPath := filepath.Join(dir, "encrypted.db")
	store, err := NewSQLiteArtifactStore(encryptedPath, WithArtifactEncryptionKey(key))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := NewArtifactPayload(scope, "selection:one", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("secret-bytes")), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(payload); err != nil {
		t.Fatal(err)
	}
	// The stored form is ciphertext with a format prefix, never plaintext.
	var stored string
	if err := store.db.QueryRow(`SELECT payload_base64 FROM semantic_artifacts WHERE artifact_key=?`, artifactStoreKey(scope, payload.Ref.ID)).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stored, artifactEncryptionPrefix) || strings.Contains(stored, payload.Base64) {
		t.Fatalf("stored payload not encrypted: %q", stored)
	}
	// Roundtrip returns the exact plaintext and the plaintext-bound digest.
	roundtrip, err := store.byID(scope, payload.Ref.ID)
	if err != nil || roundtrip.Base64 != payload.Base64 || roundtrip.Ref.IntegrityDigest != payload.Ref.IntegrityDigest {
		t.Fatalf("roundtrip=%#v err=%v", roundtrip, err)
	}
	if refs, err := store.PublishedArtifacts(scope, "selection:one"); err != nil || len(refs) != 1 || refs[0].IntegrityDigest != payload.Ref.IntegrityDigest {
		t.Fatalf("published refs=%#v err=%v", refs, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopening with the same key decrypts; without a key it fails closed.
	reopened, err := NewSQLiteArtifactStore(encryptedPath, WithArtifactEncryptionKey(key))
	if err != nil {
		t.Fatal(err)
	}
	if roundtrip, err := reopened.byID(scope, payload.Ref.ID); err != nil || roundtrip.Base64 != payload.Base64 {
		t.Fatalf("reopened roundtrip=%#v err=%v", roundtrip, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	noKey, err := NewSQLiteArtifactStore(encryptedPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := noKey.byID(scope, payload.Ref.ID); err == nil || err.Error() != "artifact_encryption_key_required" {
		t.Fatalf("no-key read err=%v", err)
	}
	if err := noKey.Close(); err != nil {
		t.Fatal(err)
	}
	// A database written before encryption remains readable once a key is
	// configured: plaintext rows pass through unchanged.
	legacyPath := filepath.Join(dir, "legacy.db")
	legacy, err := NewSQLiteArtifactStore(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	legacyPayload, err := NewArtifactPayload(scope, "selection:one", "document", "text/plain", base64.StdEncoding.EncodeToString([]byte("plain")), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Publish(legacyPayload); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := NewSQLiteArtifactStore(legacyPath, WithArtifactEncryptionKey(key))
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	if roundtrip, err := upgraded.byID(scope, legacyPayload.Ref.ID); err != nil || roundtrip.Base64 != legacyPayload.Base64 {
		t.Fatalf("legacy plaintext roundtrip=%#v err=%v", roundtrip, err)
	}
	// An invalid host key fails construction instead of storing plaintext.
	if _, err := NewSQLiteArtifactStore(filepath.Join(dir, "bad.db"), WithArtifactEncryptionKey([]byte("short"))); err == nil || err.Error() != "artifact_encryption_key_invalid" {
		t.Fatalf("invalid key err=%v", err)
	}
}

func TestParameterAuthorizationClosesTargetArtifactReferences(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string"},
			"items": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
		"additionalProperties": false,
	}
	authorization, err := NewParameterAuthorizationWithConstraints(schema, []string{"file:///docs/allowed"}, []string{"artifact:allowed"})
	if err != nil {
		t.Fatal(err)
	}
	// Authorized references pass, including nested inside arrays.
	if _, err := CanonicalizeAuthorizedInvocationArguments(`{"query":"artifact:allowed","items":["file:///docs/allowed"]}`, schema, authorization); err != nil {
		t.Fatalf("authorized references err=%v", err)
	}
	// Free text is never classified as a reference.
	if _, err := CanonicalizeAuthorizedInvocationArguments(`{"query":"Beijing weather"}`, schema, authorization); err != nil {
		t.Fatalf("free text err=%v", err)
	}
	for _, test := range []struct {
		name string
		json string
		code string
	}{
		{"unauthorized artifact", `{"query":"artifact:forged"}`, "parameter_artifact_not_authorized"},
		{"case variant artifact", `{"query":"ARTIFACT:allowed"}`, "parameter_artifact_not_authorized"},
		{"unauthorized target", `{"query":"file:///etc/passwd"}`, "parameter_target_not_authorized"},
		{"nested unauthorized target", `{"items":["location:secret-store"]}`, "parameter_target_not_authorized"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CanonicalizeAuthorizedInvocationArguments(test.json, schema, authorization); err == nil || err.Error() != test.code {
				t.Fatalf("CanonicalizeAuthorizedInvocationArguments(%s) err=%v, want %s", test.json, err, test.code)
			}
		})
	}
	// A legacy authorization declares no references, so any reference fails
	// closed while plain values keep working.
	legacy, err := NewParameterAuthorization(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalizeAuthorizedInvocationArguments(`{"query":"plain text"}`, schema, legacy); err != nil {
		t.Fatalf("legacy plain value err=%v", err)
	}
	if _, err := CanonicalizeAuthorizedInvocationArguments(`{"query":"artifact:allowed"}`, schema, legacy); err == nil || err.Error() != "parameter_artifact_not_authorized" {
		t.Fatalf("legacy artifact reference err=%v", err)
	}
	// Constraint drift changes authorization identity even when the schema
	// digest matches.
	other, err := NewParameterAuthorizationWithConstraints(schema, nil, []string{"artifact:other"})
	if err != nil {
		t.Fatal(err)
	}
	if parameterAuthorizationsEqual(authorization, other) {
		t.Fatal("constraint drift was treated as the same authorization")
	}
}
