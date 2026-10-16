package tool

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultInvocationGrantTTLIsTenMinutes(t *testing.T) {
	if DefaultInvocationGrantTTL != 10*time.Minute {
		t.Fatalf("DefaultInvocationGrantTTL = %s, want 10m", DefaultInvocationGrantTTL)
	}
}

func TestInvocationGrantBindsSelectionAndConsumesExactlyOnce(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}
	scope := InvocationScope{RootTaskID: "task-1", PlanID: plan.ID, SessionID: "s1", TurnID: "turn-1", PrincipalID: "u1", ToolSnapshotID: "toolsnap-original"}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(grants) != 1 || !strings.HasPrefix(grants[0].Token, "invoke_") {
		t.Fatalf("grants = %#v", grants)
	}
	selection, err := issuer.ValidateAndConsume(grants[0], scope, plan)
	if err != nil || selection.ID != plan.Selections[0].ID {
		t.Fatalf("validate selection=%+v err=%v", selection, err)
	}
	if _, err := issuer.ValidateAndConsume(grants[0], scope, plan); err == nil || err.Error() != "invocation_grant_replayed" {
		t.Fatalf("replay error = %v", err)
	}
}

// A grant issued before ToolSnapshotID was introduced has no signed snapshot
// identity.  Once the route row is canonicalized, validation may compare it
// with that concrete runtime snapshot for the same scope; two concrete
// snapshots must still fail closed.
func TestInvocationGrantLegacyScopeSnapshotCanonicalization(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "legacy-scope", TurnID: "legacy-turn", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("z", 32))
	store := NewMemoryInvocationGrantStore()
	issuer, err := NewInvocationIssuerWithStore(key, store)
	if err != nil {
		t.Fatal(err)
	}
	legacyScope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "legacy-session", TurnID: "legacy-turn", PrincipalID: "legacy-user"}
	grants, err := issuer.Issue(plan, legacyScope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("Issue grants=%#v err=%v", grants, err)
	}
	grant := grants[0]
	grant.CatalogDigest = ""
	grant.Token = invocationTokenForVersion(grant, invocationGrantPayloadLegacy)
	grant.Signature = invocationGrantSignatureForVersion(key, grant, invocationGrantPayloadLegacy)
	store.mu.Lock()
	store.records[grant.Nonce] = memoryInvocationGrantRecord{
		fingerprint: invocationGrantFingerprintForVersion(grant, invocationGrantPayloadLegacy),
		expiresAt:   grant.ExpiresAt.UTC(),
		state:       InvocationGrantConsumeAccepted,
	}
	store.mu.Unlock()
	runtimeScope := legacyScope
	runtimeScope.ToolSnapshotID = plan.SnapshotDigest
	if _, err := issuer.ValidateAndConsumeWithCanonicalScope(grant, runtimeScope, plan); err != nil {
		t.Fatalf("legacy scope should validate after route canonicalization: %v", err)
	}
	// The public strict entry point must still reject an arbitrary snapshot;
	// only the explicit canonical-scope migration path may bridge an empty
	// legacy field.
	wrongRuntimeScope := runtimeScope
	wrongRuntimeScope.ToolSnapshotID = "toolsnap-untrusted"
	if _, err := issuer.Validate(grant, wrongRuntimeScope, plan); err == nil || err.Error() != "invocation_grant_scope_mismatch" {
		t.Fatalf("strict legacy snapshot mismatch error=%v", err)
	}

	concreteScope := runtimeScope
	concreteScope.SessionID = "concrete-session"
	concreteScope.ToolSnapshotID = "toolsnap-a"
	concreteGrants, err := issuer.Issue(plan, concreteScope, time.Minute)
	if err != nil || len(concreteGrants) != 1 {
		t.Fatalf("concrete Issue grants=%#v err=%v", concreteGrants, err)
	}
	wrongSnapshot := concreteScope
	wrongSnapshot.ToolSnapshotID = "toolsnap-b"
	if _, err := issuer.Validate(concreteGrants[0], wrongSnapshot, plan); err == nil || err.Error() != "invocation_grant_scope_mismatch" {
		t.Fatalf("concrete snapshot mismatch error=%v", err)
	}
}

// TestInvocationGrantLegacyPayloadsRemainConsumable exercises the payload
// layouts that were persisted before CatalogDigest and ToolSnapshotID were
// added. The grant JSON is durable, while the mutable store keeps only its
// fingerprint, so both halves must agree during a rolling upgrade.
func TestInvocationGrantLegacyPayloadsRemainConsumable(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "legacy-task", TurnID: "legacy-turn", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	key := []byte(strings.Repeat("l", 32))

	testCases := []struct {
		name        string
		version     invocationGrantPayloadVersion
		planCatalog string
		catalog     string
		snapshot    string
	}{
		{name: "catalog-and-snapshot", version: invocationGrantPayloadWithoutSnapshot, planCatalog: plan.CatalogDigest, catalog: plan.CatalogDigest},
		{name: "original", version: invocationGrantPayloadLegacy, planCatalog: plan.CatalogDigest},
		{name: "snapshot-before-catalog", version: invocationGrantPayloadWithoutCatalog, planCatalog: plan.CatalogDigest, snapshot: "toolsnap-legacy"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryInvocationGrantStore()
			issuer, err := NewInvocationIssuerWithStore(key, store)
			if err != nil {
				t.Fatal(err)
			}
			legacyPlan := plan
			legacyPlan.CatalogDigest = tc.planCatalog
			scope := InvocationScope{RootTaskID: legacyPlan.RootTaskID, PlanID: legacyPlan.ID, SessionID: "legacy-session", TurnID: legacyPlan.Selections[0].ID, PrincipalID: "legacy-user", ToolSnapshotID: tc.snapshot}
			// The planner's turn is independent of the grant scope turn; use the
			// same stable value in both places so IssueReady accepts it.
			scope.TurnID = "legacy-turn"
			grants, err := issuer.Issue(legacyPlan, scope, time.Minute)
			if err != nil || len(grants) != 1 {
				t.Fatalf("Issue grants=%#v err=%v", grants, err)
			}
			grant := grants[0]
			grant.CatalogDigest = tc.catalog
			grant.Token = invocationTokenForVersion(grant, tc.version)
			grant.Signature = invocationGrantSignatureForVersion(key, grant, tc.version)
			// Issue recorded the current fingerprint. Replace that mutable record
			// with the fingerprint emitted by the legacy binary.
			store.mu.Lock()
			store.records[grant.Nonce] = memoryInvocationGrantRecord{
				fingerprint: invocationGrantFingerprintForVersion(grant, tc.version),
				expiresAt:   grant.ExpiresAt.UTC(),
				state:       InvocationGrantConsumeAccepted,
			}
			store.mu.Unlock()
			if _, err := issuer.ValidateAndConsume(grant, scope, legacyPlan); err != nil {
				t.Fatalf("legacy ValidateAndConsume: %v", err)
			}
			if _, err := issuer.ValidateAndConsume(grant, scope, legacyPlan); err == nil || err.Error() != "invocation_grant_replayed" {
				t.Fatalf("legacy replay error=%v", err)
			}
		})
	}
}

func TestInvocationGrantLegacySignatureCannotInjectNewSnapshot(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "legacy-inject", TurnID: "legacy-turn", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The original payload did not bind either newly-added field. Keep the
	// plan/catalog empty so the only expected rejection is signature mismatch
	// after a non-empty snapshot is injected into the decoded grant.
	plan.CatalogDigest = ""
	key := []byte(strings.Repeat("m", 32))
	store := NewMemoryInvocationGrantStore()
	issuer, err := NewInvocationIssuerWithStore(key, store)
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "legacy-turn", PrincipalID: "principal"}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("Issue grants=%#v err=%v", grants, err)
	}
	grant := grants[0]
	grant.Token = invocationTokenForVersion(grant, invocationGrantPayloadLegacy)
	grant.Signature = invocationGrantSignatureForVersion(key, grant, invocationGrantPayloadLegacy)
	grant.Scope.ToolSnapshotID = "forged-snapshot"
	if _, err := issuer.Validate(grant, grant.Scope, plan); err == nil || err.Error() != "invocation_grant_invalid" {
		t.Fatalf("injected snapshot validation error=%v", err)
	}
}

func TestSQLiteInvocationGrantStoreConsumesLegacyFingerprintAfterRestart(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "legacy-sqlite", TurnID: "legacy-turn", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "legacy-grants.db")
	key := []byte(strings.Repeat("q", 32))
	store, err := NewSQLiteInvocationGrantStore(path)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore(key, store)
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "legacy-turn", PrincipalID: "principal"}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("Issue grants=%#v err=%v", grants, err)
	}
	grant := grants[0]
	grant.CatalogDigest = ""
	grant.Token = invocationTokenForVersion(grant, invocationGrantPayloadLegacy)
	grant.Signature = invocationGrantSignatureForVersion(key, grant, invocationGrantPayloadLegacy)
	legacyFingerprint := invocationGrantFingerprintForVersion(grant, invocationGrantPayloadLegacy)
	if _, err := store.db.Exec(`UPDATE invocation_grants SET fingerprint=? WHERE nonce=?`, legacyFingerprint, grant.Nonce); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteInvocationGrantStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted, err := NewInvocationIssuerWithStore(key, reopened)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.ValidateAndConsume(grant, scope, plan); err != nil {
		t.Fatalf("legacy consume after restart: %v", err)
	}
	var state string
	if err := reopened.db.QueryRow(`SELECT state FROM invocation_grants WHERE nonce=?`, grant.Nonce).Scan(&state); err != nil || state != "consumed" {
		t.Fatalf("legacy state=%q err=%v", state, err)
	}
}

func TestInvocationGrantRejectsScopeAndBindingDrift(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	issuer, _ := NewInvocationIssuer([]byte(strings.Repeat("k", 32)))
	scope := InvocationScope{RootTaskID: "task-1", PlanID: plan.ID, SessionID: "s1", TurnID: "turn-1", PrincipalID: "u1", ToolSnapshotID: "toolsnap-original"}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	wrongScope := scope
	wrongScope.SessionID = "s2"
	if _, err := issuer.ValidateAndConsume(grants[0], wrongScope, plan); err == nil || err.Error() != "invocation_grant_scope_mismatch" {
		t.Fatalf("wrong scope error = %v", err)
	}
	wrongSnapshot := scope
	wrongSnapshot.ToolSnapshotID = "toolsnap-other"
	if _, err := issuer.ValidateAndConsume(grants[0], wrongSnapshot, plan); err == nil || err.Error() != "invocation_grant_scope_mismatch" {
		t.Fatalf("wrong tool snapshot error = %v", err)
	}
	tampered := grants[0]
	tampered.AdapterName = "different_adapter"
	if _, err := issuer.ValidateAndConsume(tampered, scope, plan); err == nil || err.Error() != "invocation_grant_invalid" {
		t.Fatalf("tamper error = %v", err)
	}
}

func TestInvocationGrantRejectsParameterAuthorizationDrift(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("p", 32)))
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: "task-1", PlanID: plan.ID, SessionID: "s1", TurnID: "turn-1", PrincipalID: "u1"}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("issue grants=%#v err=%v", grants, err)
	}
	driftedPlan := plan
	driftedPlan.Selections = append([]PlannedSelection(nil), plan.Selections...)
	driftedPlan.Selections[0].ParameterAuthorization.AllowedFields = []string{"forged"}
	if _, err := issuer.ValidateAndConsume(grants[0], scope, driftedPlan); err == nil || err.Error() != "invocation_grant_binding_mismatch" {
		t.Fatalf("parameter authorization drift error=%v", err)
	}
}

func TestInvocationIssuerDoesNotAuthorizeBlockedPlanNodes(t *testing.T) {
	registry := semanticRegistry(t)
	capture := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	capture.Produces = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	delivery := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	delivery.Consumes = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{capture, delivery})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot,
		Needs: []CapabilityNeed{
			{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true},
		},
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	issuer, _ := NewInvocationIssuer([]byte(strings.Repeat("z", 32)))
	scope := InvocationScope{RootTaskID: "task-1", PlanID: plan.ID, SessionID: "s1", TurnID: "turn-1", PrincipalID: "u1"}
	initial, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(initial) != 1 || initial[0].AdapterName != "capture_adapter" {
		t.Fatalf("initial grants=%#v err=%v", initial, err)
	}
	completed := map[string]bool{initial[0].SelectionID: true}
	later, err := issuer.IssueReady(plan, scope, time.Minute, completed)
	if err != nil || len(later) != 1 {
		t.Fatalf("later grants=%#v err=%v", later, err)
	}
	var deliveryGrant InvocationGrant
	for _, grant := range later {
		if grant.AdapterName == "delivery_adapter" {
			deliveryGrant = grant
		}
	}
	if deliveryGrant.Token == "" {
		t.Fatal("delivery grant was not materialized after dependency completion")
	}
	if _, err := issuer.ValidateAndConsume(deliveryGrant, scope, plan); err == nil || err.Error() != "selection_not_ready" {
		t.Fatalf("delivery without trusted completion error=%v", err)
	}
	if _, err := issuer.ValidateAndConsume(deliveryGrant, scope, plan, completed); err != nil {
		t.Fatalf("delivery with trusted completion: %v", err)
	}
}

func TestToolPlanConfirmationRequiresTrustedScopedFact(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task-1", Snapshot: snapshot,
		Needs:       []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}},
		Constraints: []RoutingConstraint{{ID: "confirm", Capability: "artifact.deliver.current_channel", Effect: "require_confirmation", Authority: AuthorityPolicy}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	selection := plan.Selections[0]
	if !selection.RequiresConfirm || selection.ConfirmationID != ConfirmationRequirementID("deliver") {
		t.Fatalf("selection=%#v", selection)
	}
	if ready := plan.ReadySelections(nil); len(ready) != 0 {
		t.Fatalf("unconfirmed selection ready=%#v", ready)
	}
	for _, facts := range [][]RoutingFact{
		{{Kind: "confirmation_granted", Authority: AuthorityUser, Attributes: map[string]string{"root_task_id": "task-1", "confirmation_requirement": selection.ConfirmationID}}},
		{{Kind: "confirmation_granted", Authority: AuthorityRuntime, Attributes: map[string]string{"root_task_id": "other-task", "confirmation_requirement": selection.ConfirmationID}}},
		{{Kind: "confirmation_granted", Authority: AuthorityRuntime, Attributes: map[string]string{"root_task_id": "task-1", "confirmation_requirement": "confirmation:other"}}},
	} {
		if got := plan.TrustedSatisfiedDependencies(facts, time.Now().UTC()); len(got) != 0 {
			t.Fatalf("untrusted/mismatched confirmation satisfied=%#v", got)
		}
	}
	facts := []RoutingFact{{Kind: "confirmation_granted", Authority: AuthorityRuntime, Attributes: map[string]string{"root_task_id": "task-1", "confirmation_requirement": selection.ConfirmationID}}}
	satisfied := plan.TrustedSatisfiedDependencies(facts, time.Now().UTC())
	if !satisfied[selection.ConfirmationID] || len(plan.ReadySelections(satisfied)) != 1 {
		t.Fatalf("trusted confirmation did not unlock selection: %#v", satisfied)
	}
}

func TestSQLiteInvocationGrantStorePersistsOneTimeConsumption(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	dbPath := filepath.Join(t.TempDir(), "invocation-grants.db")
	store, err := NewSQLiteInvocationGrantStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteInvocationGrantStore: %v", err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("d", 32)), store)
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}
	scope := InvocationScope{RootTaskID: "task-1", PlanID: plan.ID, SessionID: "session", TurnID: "turn-1", PrincipalID: "principal"}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("Issue grants=%#v err=%v", grants, err)
	}
	grant := grants[0]
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	reopened, err := NewSQLiteInvocationGrantStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	issuer, err = NewInvocationIssuerWithStore([]byte(strings.Repeat("d", 32)), reopened)
	if err != nil {
		t.Fatalf("reopen issuer: %v", err)
	}
	if _, err := issuer.ValidateAndConsume(grant, scope, plan); err != nil {
		t.Fatalf("consume after restart: %v", err)
	}
	if _, err := issuer.ValidateAndConsume(grant, scope, plan); err == nil || err.Error() != "invocation_grant_replayed" {
		t.Fatalf("replay after restart=%v", err)
	}
}

func TestSQLiteInvocationGrantStoreRejectsConcurrentConsumeAndRevocation(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "task-1", TurnID: "turn-1", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	store, err := NewSQLiteInvocationGrantStore(filepath.Join(t.TempDir(), "invocation-grants.db"))
	if err != nil {
		t.Fatalf("NewSQLiteInvocationGrantStore: %v", err)
	}
	defer store.Close()
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("c", 32)), store)
	if err != nil {
		t.Fatalf("issuer: %v", err)
	}
	scope := InvocationScope{RootTaskID: "task-1", PlanID: plan.ID, SessionID: "session", TurnID: "turn-1", PrincipalID: "principal"}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("Issue grants=%#v err=%v", grants, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := issuer.ValidateAndConsume(grants[0], scope, plan)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	successes, replays := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if err.Error() == "invocation_grant_replayed" {
			replays++
		} else {
			t.Fatalf("concurrent consume error=%v", err)
		}
	}
	if successes != 1 || replays != 1 {
		t.Fatalf("concurrent outcomes successes=%d replays=%d", successes, replays)
	}
	grants, err = issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("second Issue grants=%#v err=%v", grants, err)
	}
	if err := issuer.RevokeWithError(grants[0]); err != nil {
		t.Fatalf("RevokeWithError: %v", err)
	}
	if _, err := issuer.ValidateAndConsume(grants[0], scope, plan); err == nil || err.Error() != "invocation_grant_revoked" {
		t.Fatalf("revoked grant execution=%v", err)
	}
}
