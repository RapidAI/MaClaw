package tool

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const semanticArtifactTestPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScL0yAAAAABJRU5ErkJggg=="

func semanticArtifactTestScope() InvocationScope {
	return InvocationScope{RootTaskID: "root", PlanID: "plan", SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
}

func TestArtifactStorePersistsScopedArtifactAndPreparedDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifacts.db")
	store, err := NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	scope := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Publish(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	access, err := store.IssueProjectedAccessGrant(ref, scope, "selection:deliver", ArtifactContract{Kind: "image", MIMEType: "image/png", Required: true}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := store.ConsumeAccessGrant(access, ArtifactContract{Kind: "image", MIMEType: "image/png", Required: true})
	if err != nil || recovered.Ref != ref || recovered.Base64 != semanticArtifactTestPNG {
		t.Fatalf("recovered artifact=%#v err=%v", recovered, err)
	}
	// A host retry can rebuild the payload at a later timestamp. The immutable
	// artifact identity is content + producer + scope, not callback wall time.
	retry, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if retriedRef, err := store.Publish(retry); err != nil || retriedRef.ID != ref.ID || retriedRef.CreatedAt != ref.CreatedAt {
		t.Fatalf("idempotent artifact publish ref=%#v err=%v", retriedRef, err)
	}
	record, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver", ArtifactID: ref.ID, ChannelScope: "lansenger", DestinationID: "user:user", State: DeliveryPrepared})
	if err != nil || record.State != DeliveryPrepared || record.ArtifactID != ref.ID {
		t.Fatalf("delivery record=%#v err=%v", record, err)
	}
	if strings.Contains(string(record.State), "sent") {
		t.Fatalf("prepared delivery was promoted without a channel receipt: %#v", record)
	}
	if duplicate, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver", ArtifactID: ref.ID, ChannelScope: "lansenger", DestinationID: "user:user", State: DeliveryPrepared}); err != nil || duplicate != record {
		t.Fatalf("idempotent delivery preparation duplicate=%#v err=%v", duplicate, err)
	}
}

func TestArtifactStoreRejectsCrossScopeAndConflictingDelivery(t *testing.T) {
	store := NewMemoryArtifactStore()
	scope := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Publish(payload)
	if err != nil {
		t.Fatal(err)
	}
	other := scope
	other.PrincipalID = "other"
	if _, err := store.IssueProjectedAccessGrant(ref, other, "selection:deliver", ArtifactContract{Kind: "image", MIMEType: "image/png", Required: true}, time.Minute); err == nil || err.Error() != "artifact_projection_invalid" {
		t.Fatalf("cross-principal artifact projection err=%v", err)
	}
	if _, err := store.PrepareDelivery(DeliveryRecord{Scope: other, SelectionID: "selection:deliver", ArtifactID: ref.ID, ChannelScope: "lansenger", DestinationID: "user:user", State: DeliveryPrepared}); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("cross-principal delivery preparation err=%v", err)
	}
	if _, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver", ArtifactID: ref.ID, ChannelScope: "lansenger", DestinationID: "user:user", State: DeliveryPrepared}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver", ArtifactID: ref.ID, ChannelScope: "other", DestinationID: "user:user", State: DeliveryPrepared}); err == nil || err.Error() != "delivery_conflict" {
		t.Fatalf("conflicting prepared delivery err=%v", err)
	}
}

func TestArtifactStoreRecordsOnlyGatewayObservedDeliveryOutcomes(t *testing.T) {
	store := NewMemoryArtifactStore()
	scope := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Publish(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver", ArtifactID: ref.ID, ChannelScope: "lansenger", DestinationID: "user:user", State: DeliveryPrepared}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordDeliveryOutcome(scope, "selection:deliver", DeliveryPrepared); err == nil || err.Error() != "delivery_outcome_invalid" {
		t.Fatalf("prepared is not an observed outcome: %v", err)
	}
	if _, dispatch, err := store.ClaimDeliveryDispatch(scope, "selection:deliver", time.Now().UTC()); err != nil || !dispatch {
		t.Fatalf("claim dispatch=%t err=%v", dispatch, err)
	}
	accepted, err := store.RecordDeliveryOutcome(scope, "selection:deliver", DeliveryAccepted)
	if err != nil || accepted.State != DeliveryAccepted {
		t.Fatalf("accepted delivery=%#v err=%v", accepted, err)
	}
	if replay, err := store.RecordDeliveryOutcome(scope, "selection:deliver", DeliveryAccepted); err != nil || replay != accepted {
		t.Fatalf("accepted retry=%#v err=%v", replay, err)
	}
	if _, err := store.RecordDeliveryOutcome(scope, "selection:deliver", DeliveryUnknown); err == nil || err.Error() != "delivery_outcome_conflict" {
		t.Fatalf("terminal outcome must not be overwritten: %v", err)
	}
}

func TestArtifactStoreClaimsOneTrustedDispatchAndDoesNotReplayUnknown(t *testing.T) {
	store := NewMemoryArtifactStore()
	scope := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Publish(payload)
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver", ArtifactID: ref.ID, ChannelScope: "channel", DestinationID: "group:one", State: DeliveryPrepared})
	if err != nil || record.OperationKey == "" {
		t.Fatalf("prepared delivery=%#v err=%v", record, err)
	}
	if claim, dispatch, err := store.ClaimDeliveryDispatch(scope, "selection:deliver", time.Now().UTC()); err != nil || !dispatch || claim.State != DeliveryDispatching {
		t.Fatalf("first claim record=%#v dispatch=%t err=%v", claim, dispatch, err)
	}
	if claim, dispatch, err := store.ClaimDeliveryDispatch(scope, "selection:deliver", time.Now().UTC()); err != nil || dispatch || claim.State != DeliveryDispatching {
		t.Fatalf("second claim record=%#v dispatch=%t err=%v", claim, dispatch, err)
	}
	if _, err := store.RecordDeliveryOutcome(scope, "selection:deliver", DeliveryUnknown); err != nil {
		t.Fatal(err)
	}
	if claim, dispatch, err := store.ClaimDeliveryDispatch(scope, "selection:deliver", time.Now().UTC()); err != nil || dispatch || claim.State != DeliveryUnknown {
		t.Fatalf("unknown must not replay record=%#v dispatch=%t err=%v", claim, dispatch, err)
	}
}

func TestArtifactStoreDeliveryOperationIsDestinationScoped(t *testing.T) {
	store := NewMemoryArtifactStore()
	scope := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Publish(payload)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver-one", ArtifactID: ref.ID, ChannelScope: "channel", DestinationID: "user:one", State: DeliveryPrepared})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver-two", ArtifactID: ref.ID, ChannelScope: "channel", DestinationID: "user:two", State: DeliveryPrepared})
	if err != nil || first.OperationKey == second.OperationKey {
		t.Fatalf("destination must scope operation first=%#v second=%#v err=%v", first, second, err)
	}
}

func TestSQLiteArtifactStoreReconcilesStaleDispatchToUnknown(t *testing.T) {
	store, err := NewSQLiteArtifactStore(filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Publish(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PrepareDelivery(DeliveryRecord{Scope: scope, SelectionID: "selection:deliver", ArtifactID: ref.ID, ChannelScope: "channel", DestinationID: "user:one", State: DeliveryPrepared}); err != nil {
		t.Fatal(err)
	}
	claimedAt := time.Now().UTC().Add(-2 * DeliveryDispatchLease)
	if _, dispatch, err := store.ClaimDeliveryDispatch(scope, "selection:deliver", claimedAt); err != nil || !dispatch {
		t.Fatalf("claim dispatch=%t err=%v", dispatch, err)
	}
	if changed, err := store.ReconcileStaleDeliveryDispatches(time.Now().UTC(), DeliveryDispatchLease); err != nil || changed != 1 {
		t.Fatalf("reconcile changed=%d err=%v", changed, err)
	}
	record, err := store.Delivery(scope, "selection:deliver")
	if err != nil || record.State != DeliveryUnknown {
		t.Fatalf("reconciled record=%#v err=%v", record, err)
	}
	if _, dispatch, err := store.ClaimDeliveryDispatch(scope, "selection:deliver", time.Now().UTC()); err != nil || dispatch {
		t.Fatalf("unknown replay dispatch=%t err=%v", dispatch, err)
	}
}

func TestArtifactAccessGrantIsOneTimeScopedAndContractBound(t *testing.T) {
	store := NewMemoryArtifactStore()
	scope := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(payload); err != nil {
		t.Fatal(err)
	}
	contract := ArtifactContract{Kind: "image", MIMEType: "image/png", Required: true}
	grant, err := store.IssueProjectedAccessGrant(payload.Ref, scope, "selection:deliver", contract, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeAccessGrant(grant, ArtifactContract{Kind: "image", MIMEType: "image/jpeg", Required: true}); err == nil || err.Error() != "artifact_access_contract_mismatch" {
		t.Fatalf("different contract consumed grant: %v", err)
	}
	consumed, err := store.ConsumeAccessGrant(grant, contract)
	if err != nil || consumed.Ref.ID != payload.Ref.ID {
		t.Fatalf("grant consumption=%#v err=%v", consumed, err)
	}
	if _, err := store.ConsumeAccessGrant(grant, contract); err == nil || err.Error() != "artifact_access_grant_replayed" {
		t.Fatalf("replayed grant err=%v", err)
	}
	other := scope
	other.PrincipalID = "other"
	forged := grant
	forged.Scope = other
	if _, err := store.ConsumeAccessGrant(forged, contract); !errors.Is(err, ErrArtifactAccessNotFound) {
		t.Fatalf("cross-scope forged grant err=%v", err)
	}
}

func exerciseExactArtifactReferenceAccess(t *testing.T, store ArtifactStore) {
	t.Helper()
	scope := semanticArtifactTestScope()
	first, err := NewArtifactPayload(scope, "selection:capture-a", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewArtifactPayload(scope, "selection:capture-b", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	firstRef, err := store.Publish(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(second); err != nil {
		t.Fatal(err)
	}
	contract := ArtifactContract{Kind: "image", MIMEType: "image/png", Required: true}
	grant, err := store.IssueProjectedAccessGrant(firstRef, scope, "selection:deliver", contract, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := store.ConsumeAccessGrant(grant, contract)
	if err != nil || consumed.Ref.ID != firstRef.ID || consumed.Ref.ProducerSelection != "selection:capture-a" {
		t.Fatalf("exact artifact consumption=%#v err=%v", consumed, err)
	}
}

func TestMemoryArtifactStoreConsumesOnlyExactReferencedArtifact(t *testing.T) {
	exerciseExactArtifactReferenceAccess(t, NewMemoryArtifactStore())
}

func TestSQLiteArtifactStoreConsumesOnlyExactReferencedArtifact(t *testing.T) {
	store, err := NewSQLiteArtifactStore(filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exerciseExactArtifactReferenceAccess(t, store)
}

func TestSQLiteArtifactAccessGrantSurvivesRestartAndRejectsReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifacts.db")
	store, err := NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	scope := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(payload); err != nil {
		t.Fatal(err)
	}
	contract := ArtifactContract{Kind: "image", MIMEType: "image/png", Required: true}
	grant, err := store.IssueProjectedAccessGrant(payload.Ref, scope, "selection:deliver", contract, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if consumed, err := store.ConsumeAccessGrant(grant, contract); err != nil || consumed.Ref.ID != payload.Ref.ID {
		t.Fatalf("restart grant consumption=%#v err=%v", consumed, err)
	}
	if _, err := store.ConsumeAccessGrant(grant, contract); err == nil || err.Error() != "artifact_access_grant_replayed" {
		t.Fatalf("restart replay err=%v", err)
	}
}

func exerciseProjectedArtifactAccess(t *testing.T, store ArtifactStore) {
	t.Helper()
	parent := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(parent, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Publish(payload)
	if err != nil {
		t.Fatal(err)
	}
	child := parent
	child.PlanID, child.TurnID = "plan:child", "turn:child"
	contract := ArtifactContract{Kind: "image", MIMEType: "image/png", Required: true}
	grant, err := store.IssueProjectedAccessGrant(ref, child, "selection:deliver", contract, time.Minute)
	if err != nil || grant.Scope != child || grant.SourceScope != parent {
		t.Fatalf("projected grant=%#v err=%v", grant, err)
	}
	if _, err := store.ConsumeAccessGrant(grant, contract); err != nil {
		t.Fatalf("consume projected grant: %v", err)
	}
	if _, err := store.ConsumeAccessGrant(grant, contract); err == nil || err.Error() != "artifact_access_grant_replayed" {
		t.Fatalf("replayed projected grant err=%v", err)
	}
	wrongPrincipal := child
	wrongPrincipal.PrincipalID = "other"
	if _, err := store.IssueProjectedAccessGrant(ref, wrongPrincipal, "selection:deliver", contract, time.Minute); err == nil || err.Error() != "artifact_projection_invalid" {
		t.Fatalf("cross-principal projected grant err=%v", err)
	}
	if _, err := store.IssueProjectedAccessGrant(ref, child, "selection:deliver", ArtifactContract{Kind: "image", MIMEType: "image/jpeg", Required: true}, time.Minute); err == nil || err.Error() != "artifact_projection_invalid" {
		t.Fatalf("mismatched projected contract err=%v", err)
	}
	forged := ref
	forged.IntegrityDigest = "forged"
	if _, err := store.IssueProjectedAccessGrant(forged, child, "selection:deliver", contract, time.Minute); !errors.Is(err, ErrArtifactNotFound) {
		t.Fatalf("forged source ref err=%v", err)
	}
}

func TestMemoryArtifactStoreProjectsOnlyBoundArtifactAccess(t *testing.T) {
	exerciseProjectedArtifactAccess(t, NewMemoryArtifactStore())
}

func TestSQLiteArtifactStoreProjectsOnlyBoundArtifactAccessAndRecovers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifacts.db")
	store, err := NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	parent := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(parent, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.Publish(payload)
	if err != nil {
		t.Fatal(err)
	}
	child := parent
	child.PlanID, child.TurnID = "plan:child", "turn:child"
	contract := ArtifactContract{Kind: "image", MIMEType: "image/png", Required: true}
	grant, err := store.IssueProjectedAccessGrant(ref, child, "selection:deliver", contract, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if recovered, err := store.ConsumeAccessGrant(grant, contract); err != nil || recovered.Ref != ref {
		t.Fatalf("recovered projected payload=%#v err=%v", recovered, err)
	}
	if _, err := store.ConsumeAccessGrant(grant, contract); err == nil || err.Error() != "artifact_access_grant_replayed" {
		t.Fatalf("replayed restarted projected grant err=%v", err)
	}
}

func TestSQLiteArtifactStoreMigratesPreparedDeliverySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifacts.db")
	store, err := NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DROP TABLE semantic_delivery_preparations`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TABLE semantic_delivery_preparations (
		delivery_key TEXT PRIMARY KEY, root_task_id TEXT NOT NULL, plan_id TEXT NOT NULL,
		session_id TEXT NOT NULL, turn_id TEXT NOT NULL, principal_id TEXT NOT NULL,
		selection_id TEXT NOT NULL, artifact_id TEXT NOT NULL, channel_scope TEXT NOT NULL,
		state TEXT NOT NULL CHECK(state IN ('prepared')), created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var definition string
	if err := store.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='semantic_delivery_preparations'`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(definition), "'accepted'") || !strings.Contains(strings.ToLower(definition), "'failed'") || !strings.Contains(strings.ToLower(definition), "'unknown'") {
		t.Fatalf("delivery table was not migrated: %s", definition)
	}
}

func TestArtifactStorePublishedArtifactsAreProducerScoped(t *testing.T) {
	store := NewMemoryArtifactStore()
	scope := semanticArtifactTestScope()
	for index, producer := range []string{"selection:capture", "selection:other"} {
		content := semanticArtifactTestPNG
		if index == 1 {
			content = "b3RoZXI="
		}
		payload, err := NewArtifactPayload(scope, producer, "image", "image/png", content, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Publish(payload); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := store.PublishedArtifacts(scope, "selection:capture")
	if err != nil || len(refs) != 1 || refs[0].ProducerSelection != "selection:capture" {
		t.Fatalf("producer-scoped refs=%#v err=%v", refs, err)
	}
	otherScope := scope
	otherScope.TurnID = "other-turn"
	refs, err = store.PublishedArtifacts(otherScope, "selection:capture")
	if err != nil || len(refs) != 0 {
		t.Fatalf("cross-scope refs=%#v err=%v", refs, err)
	}
}

func TestArtifactRefIdentityBindsScopeAndProducerProvenance(t *testing.T) {
	scope := semanticArtifactTestScope()
	first, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	sameProducer, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if first.Ref.ID != sameProducer.Ref.ID {
		t.Fatalf("same artifact provenance changed ID: %q vs %q", first.Ref.ID, sameProducer.Ref.ID)
	}
	otherProducer, err := NewArtifactPayload(scope, "selection:other", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	otherScope := scope
	otherScope.TurnID = "other-turn"
	otherTurn, err := NewArtifactPayload(otherScope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if first.Ref.IntegrityDigest != otherProducer.Ref.IntegrityDigest || first.Ref.IntegrityDigest != otherTurn.Ref.IntegrityDigest {
		t.Fatal("test requires identical content digest")
	}
	if first.Ref.ID == otherProducer.Ref.ID || first.Ref.ID == otherTurn.Ref.ID {
		t.Fatalf("content-only artifact ID leaked across provenance: capture=%q other=%q turn=%q", first.Ref.ID, otherProducer.Ref.ID, otherTurn.Ref.ID)
	}
	store := NewMemoryArtifactStore()
	for _, payload := range []ArtifactPayload{first, otherProducer} {
		if _, err := store.Publish(payload); err != nil {
			t.Fatalf("publish distinct provenance artifact: %v", err)
		}
	}
	refs, err := store.PublishedArtifacts(scope, "selection:capture")
	if err != nil || len(refs) != 1 || refs[0].ID != first.Ref.ID {
		t.Fatalf("capture refs=%#v err=%v", refs, err)
	}
	refs, err = store.PublishedArtifacts(scope, "selection:other")
	if err != nil || len(refs) != 1 || refs[0].ID != otherProducer.Ref.ID {
		t.Fatalf("other refs=%#v err=%v", refs, err)
	}
}

func TestArtifactPayloadAcceptsLegacyContentOnlyIDForRecovery(t *testing.T) {
	scope := semanticArtifactTestScope()
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	payload.Ref.ID = legacyArtifactRefID(payload.Ref.IntegrityDigest)
	store := NewMemoryArtifactStore()
	if _, err := store.Publish(payload); err != nil {
		t.Fatalf("legacy artifact ID rejected: %v", err)
	}
}

func TestArtifactStorePersistsDisplayNameOutsideIdentity(t *testing.T) {
	scope := semanticArtifactTestScope()
	newNamedPayload := func() ArtifactPayload {
		payload, err := NewArtifactPayload(scope, "selection:generate", "document", "application/pdf", semanticArtifactTestPNG, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		payload.Ref.Name = "南京天气报告.pdf"
		return payload
	}

	path := filepath.Join(t.TempDir(), "artifacts.db")
	store, err := NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(newNamedPayload()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewSQLiteArtifactStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	refs, err := store.PublishedArtifacts(scope, "selection:generate")
	if err != nil || len(refs) != 1 || refs[0].Name != "南京天气报告.pdf" {
		t.Fatalf("sqlite name round trip refs=%#v err=%v", refs, err)
	}
	// Name is display-only: a republish differing only in Name matches the
	// stored identity and keeps the originally recorded display name.
	renamed := newNamedPayload()
	renamed.Ref.Name = "别的名字.pdf"
	republished, err := store.Publish(renamed)
	if err != nil || republished.Name != "南京天气报告.pdf" {
		t.Fatalf("name must stay outside artifact identity: %#v err=%v", republished, err)
	}

	memory := NewMemoryArtifactStore()
	if _, err := memory.Publish(newNamedPayload()); err != nil {
		t.Fatal(err)
	}
	refs, err = memory.PublishedArtifacts(scope, "selection:generate")
	if err != nil || len(refs) != 1 || refs[0].Name != "南京天气报告.pdf" {
		t.Fatalf("memory name round trip refs=%#v err=%v", refs, err)
	}
	if republished, err := memory.Publish(renamed); err != nil || republished.Name != "南京天气报告.pdf" {
		t.Fatalf("memory store must also keep name outside identity: %#v err=%v", republished, err)
	}
}
