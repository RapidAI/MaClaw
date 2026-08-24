package tool

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSemanticExecutionCoordinatorPublishesSurfaceAtomically(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("read_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "publish-root", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("p", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	state, grants, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil || state.Revision == nil || len(grants) != 1 || len(state.Materializations) != 1 {
		t.Fatalf("state=%+v grants=%#v err=%v", state, grants, err)
	}
	if state.Materializations[0].State != RouteMaterializationExposed || !sameInvocationGrant(state.Materializations[0].Grant, grants[0]) {
		t.Fatalf("materialization=%+v grant=%+v", state.Materializations[0], grants[0])
	}
	var eventKind string
	if err := coordinator.db.QueryRow(`SELECT event_kind FROM semantic_surface_publish_outbox WHERE route_key=?`, routeStateKey(scope)).Scan(&eventKind); err != nil || eventKind != "surface_published" {
		t.Fatalf("publish outbox kind=%q err=%v", eventKind, err)
	}
	if _, err := issuer.Validate(grants[0], scope, plan); err != nil {
		t.Fatalf("published grant is not valid: %v", err)
	}
}

func TestSemanticExecutionCoordinatorSurfacePublishRollsBackOnGrantFailure(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("read_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "publish-fail-root", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("f", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	// Preoccupy only the terminal outbox row. PublishSurface will stage the
	// revision, grants and materialization before this unique insert fails; one
	// rollback must remove every staged authorization record.
	if _, err := coordinator.db.Exec(`INSERT INTO semantic_surface_publish_outbox(route_key, lineage_key, fencing_token, event_kind, created_at) VALUES (?, 'other-lineage', 1, 'surface_published', ?)`, routeStateKey(scope), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	_, _, err = coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err == nil {
		t.Fatal("invalid snapshot digest unexpectedly published")
	}
	var count int
	if queryErr := coordinator.db.QueryRow(`SELECT COUNT(*) FROM semantic_route_states WHERE route_key=?`, routeStateKey(scope)).Scan(&count); queryErr != nil || count != 0 {
		t.Fatalf("failed publish left route count=%d err=%v", count, queryErr)
	}
	if queryErr := coordinator.db.QueryRow(`SELECT COUNT(*) FROM semantic_route_materializations WHERE route_key=?`, routeStateKey(scope)).Scan(&count); queryErr != nil || count != 0 {
		t.Fatalf("failed publish left materializations count=%d err=%v", count, queryErr)
	}
	if queryErr := coordinator.db.QueryRow(`SELECT COUNT(*) FROM invocation_grants`).Scan(&count); queryErr != nil || count != 0 {
		t.Fatalf("failed publish left grants count=%d err=%v", count, queryErr)
	}
}

func TestSemanticExecutionCoordinatorMaterializesNextPhaseAtomically(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	first := semanticProvider("first_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	second := semanticProvider("second_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "phase-root", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{first, second}), Needs: []CapabilityNeed{
		{ID: "first", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
		{ID: "second", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
	}})
	if err != nil || len(plan.Selections) != 2 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	// Force a two-phase plan: the second selection becomes ready only after
	// the first trusted completion is projected.
	plan.Selections[1].Requires = []string{plan.Selections[0].ID}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("n", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	state, initial, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil || len(initial) != 1 {
		t.Fatalf("initial state=%+v grants=%#v err=%v", state, initial, err)
	}
	firstSelection := initial[0].SelectionID
	if _, err := coordinator.Routes.RecordSelectionCompletion(scope, plan.ID, firstSelection, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	completed, err := coordinator.Routes.CompletedSelections(scope)
	if err != nil {
		t.Fatal(err)
	}
	nextID := ""
	for _, selection := range plan.Selections {
		if selection.ID != firstSelection {
			nextID = selection.ID
		}
	}
	state, next, err := coordinator.MaterializeReadySurface(scope, issuer, time.Minute, completed, map[string]bool{nextID: true}, time.Now().UTC())
	if err != nil || len(next) != 1 || next[0].SelectionID != nextID {
		t.Fatalf("next state=%+v grants=%#v err=%v", state, next, err)
	}
	if len(state.Materializations) != 2 {
		t.Fatalf("materializations=%#v", state.Materializations)
	}
}

func TestSemanticExecutionCoordinatorDoesNotRematerializeExposedSelection(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("read_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "rematerialize-root", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("r", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	_, initial, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil || len(initial) != 1 {
		t.Fatalf("initial=%#v err=%v", initial, err)
	}
	_, grants, err := coordinator.MaterializeReadySurface(scope, issuer, time.Minute, map[string]bool{}, map[string]bool{initial[0].SelectionID: true}, time.Now().UTC())
	if err == nil || err.Error() != "semantic surface selection already materialized" || len(grants) != 0 {
		t.Fatalf("re-materialized grants=%#v err=%v", grants, err)
	}
	var count int
	if err := coordinator.db.QueryRow(`SELECT COUNT(*) FROM invocation_grants`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("re-materialization left grant count=%d err=%v", count, err)
	}
}

func TestSemanticExecutionCoordinatorProjectsContinuityFactsWithFencing(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("read_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "continuity-root", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("p", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	_, grants, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil || len(grants) != 1 {
		t.Fatalf("publish grants=%#v err=%v", grants, err)
	}
	var publishedSequence uint64
	if err := coordinator.db.QueryRow(`SELECT sequence FROM semantic_continuity_projection_outbox WHERE route_key=? AND event_kind='route_published'`, routeStateKey(scope)).Scan(&publishedSequence); err != nil {
		t.Fatal(err)
	}
	state, err := coordinator.ApplyContinuityProjection(publishedSequence, 0, "tenant", time.Now().UTC())
	if err != nil || state.Version != 1 || len(state.OpenNeeds) != 1 || len(state.CompletedEvidence) != 0 {
		t.Fatalf("published continuity=%#v err=%v", state, err)
	}
	if _, err := coordinator.ApplyContinuityProjection(publishedSequence, 999, "tenant", time.Now().UTC()); err != nil {
		t.Fatalf("projection retry was not idempotent: %v", err)
	}
	admission := SemanticExecutionAdmission{Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "continuity"}, Grant: grants[0], RequestDigest: "request:continuity", Scope: scope, Selection: plan.Selections[0], Now: time.Now().UTC()}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	if _, err := coordinator.Complete(admission, PlanExecutionSucceeded, "done", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	var completionSequence uint64
	if err := coordinator.db.QueryRow(`SELECT sequence FROM semantic_continuity_projection_outbox WHERE route_key=? AND event_kind='execution_updated'`, routeStateKey(scope)).Scan(&completionSequence); err != nil {
		t.Fatal(err)
	}
	state, err = coordinator.ApplyContinuityProjection(completionSequence, 1, "tenant", time.Now().UTC())
	if err != nil || state.Version != 2 || len(state.OpenNeeds) != 0 || len(state.CompletedEvidence) != 1 {
		t.Fatalf("completion continuity=%#v err=%v", state, err)
	}
}

func TestSemanticExecutionCoordinatorContinuityProjectionIsTenantScoped(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("read_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("t", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	publish := func(tenantID, root string) (InvocationScope, uint64) {
		t.Helper()
		plan, planErr := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: root, SessionID: "session-" + tenantID, TurnID: "turn-" + tenantID, Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
		if planErr != nil {
			t.Fatal(planErr)
		}
		scope := InvocationScope{RootTaskID: plan.RootTaskID, PlanID: plan.ID, SessionID: "session-" + tenantID, TurnID: "turn-" + tenantID, PrincipalID: "principal-" + tenantID}
		if _, _, publishErr := coordinator.PublishSurface(SurfacePublishRequest{Revision: RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, TenantID: tenantID, Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); publishErr != nil {
			t.Fatal(publishErr)
		}
		var sequence uint64
		if queryErr := coordinator.db.QueryRow(`SELECT sequence FROM semantic_continuity_projection_outbox WHERE route_key=? AND event_kind='route_published'`, routeStateKey(scope)).Scan(&sequence); queryErr != nil {
			t.Fatal(queryErr)
		}
		return scope, sequence
	}

	scopeA, sequenceA := publish("tenant-a", "root-a")
	scopeB, sequenceB := publish("tenant-b", "root-b")
	if applied, drainErr := coordinator.DrainContinuityProjections("tenant-a", 10, time.Now().UTC()); drainErr != nil || applied != 1 {
		t.Fatalf("tenant-a drain applied=%d err=%v", applied, drainErr)
	}
	stateA, err := coordinator.ContinuityState(ContinuityScope{TenantID: "tenant-a", PrincipalID: scopeA.PrincipalID, ConversationID: scopeA.SessionID, RootTaskID: scopeA.RootTaskID})
	if err != nil || len(stateA.OpenNeeds) != 1 {
		t.Fatalf("tenant-a state=%#v err=%v", stateA, err)
	}
	if _, err := coordinator.ContinuityState(ContinuityScope{TenantID: "tenant-a", PrincipalID: scopeB.PrincipalID, ConversationID: scopeB.SessionID, RootTaskID: scopeB.RootTaskID}); err != sql.ErrNoRows {
		t.Fatalf("tenant-a observed tenant-b state err=%v", err)
	}
	if _, err := coordinator.ApplyContinuityProjection(sequenceB, 0, "tenant-a", time.Now().UTC()); err == nil || err.Error() != "continuity_projection_tenant_mismatch" {
		t.Fatalf("cross-tenant apply err=%v", err)
	}
	var pending string
	if err := coordinator.db.QueryRow(`SELECT state FROM semantic_continuity_projection_outbox WHERE sequence=?`, sequenceB).Scan(&pending); err != nil || pending != "pending" {
		t.Fatalf("tenant-b event state=%q err=%v", pending, err)
	}
	if applied, drainErr := coordinator.DrainContinuityProjections("tenant-b", 10, time.Now().UTC()); drainErr != nil || applied != 1 {
		t.Fatalf("tenant-b drain applied=%d err=%v", applied, drainErr)
	}
	if _, err := coordinator.ContinuityProjectionEvent(sequenceA); err != nil {
		t.Fatalf("tenant-a event unavailable: %v", err)
	}
}

func TestSemanticExecutionCoordinatorContinuityProjectionCannotOverwriteChildRevision(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("read_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	parentPlan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "continuity-stale-root", SessionID: "session", TurnID: "parent", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	parentScope := InvocationScope{RootTaskID: parentPlan.RootTaskID, PlanID: parentPlan.ID, SessionID: "session", TurnID: "parent", PrincipalID: "principal"}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("s", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	parent, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: routeRevisionPublish(parentPlan, parentScope, nil), Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	var parentSequence uint64
	if err := coordinator.db.QueryRow(`SELECT sequence FROM semantic_continuity_projection_outbox WHERE route_key=? AND event_kind='route_published'`, routeStateKey(parentScope)).Scan(&parentSequence); err != nil {
		t.Fatal(err)
	}
	childPlan := parentPlan
	childPlan.ID, childPlan.SnapshotDigest = "plan:continuity-child", "snapshot:continuity-child"
	childScope := parentScope
	childScope.PlanID, childScope.TurnID = childPlan.ID, "child"
	if _, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: routeRevisionPublish(childPlan, childScope, parent.Revision), Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.ApplyContinuityProjection(parentSequence, 0, "tenant", time.Now().UTC()); err == nil || err.Error() != "continuity_projection_superseded" {
		t.Fatalf("stale projection err=%v", err)
	}
	var eventState string
	if err := coordinator.db.QueryRow(`SELECT state FROM semantic_continuity_projection_outbox WHERE sequence=?`, parentSequence).Scan(&eventState); err != nil || eventState != "obsolete" {
		t.Fatalf("stale projection state=%q err=%v", eventState, err)
	}
}

func TestSemanticExecutionCoordinatorPublishSurfaceProjectsCompatibleParentFacts(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("c", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}

	parentPlan, parentScope := artifactRouteTestPlan(t, "turn-parent")
	parent, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: routeRevisionPublish(parentPlan, parentScope, nil), Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	capture := "selection:capture"
	if _, err := coordinator.Routes.RecordSelectionCompletion(parentScope, parentPlan.ID, capture, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	payload, err := NewArtifactPayload(parentScope, capture, "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Routes.RecordArtifact(parentScope, parentPlan.ID, payload.Ref, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	childPlan := parentPlan
	childScope := parentScope
	childScope.TurnID = "turn-child"
	childPlan.ID = "plan:coordinator-child"
	childPlan.SnapshotDigest = "snapshot:coordinator-child"
	childScope.PlanID = childPlan.ID
	state, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: routeRevisionPublish(childPlan, childScope, parent.Revision), Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := coordinator.Routes.CompletedSelections(childScope)
	if err != nil || !completed[capture] {
		t.Fatalf("projected completed=%#v err=%v", completed, err)
	}
	refs, err := coordinator.Routes.ArtifactRefs(childScope)
	if err != nil || len(refs) != 1 || refs[0].ArtifactID != payload.Ref.ID || refs[0].SourceScope != parentScope {
		t.Fatalf("projected artifacts=%#v err=%v", refs, err)
	}
	if len(state.Completed) != 1 || len(state.Artifacts) != 1 {
		t.Fatalf("published state did not reload all projected facts=%#v", state)
	}
}

func TestSemanticExecutionCoordinatorPublishSurfaceProjectsCompatibleConfirmations(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("a", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	parentPlan, parentScope := confirmationRouteTestPlan(t, "turn-parent")
	parent, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: routeRevisionPublish(parentPlan, parentScope, nil), Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	requirement := parentPlan.Selections[0].ConfirmationID
	fact := RoutingFact{ID: "approval", Kind: "confirmation_granted", Authority: AuthorityPolicy, Attributes: map[string]string{"root_task_id": parentScope.RootTaskID, "confirmation_requirement": requirement}, ValidUntil: time.Now().UTC().Add(time.Hour)}
	if _, err := coordinator.Routes.RecordConfirmation(parentScope, parentPlan.ID, fact, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	childPlan, childScope := confirmationRouteTestPlan(t, "turn-child")
	state, _, err := coordinator.PublishSurface(SurfacePublishRequest{Revision: routeRevisionPublish(childPlan, childScope, parent.Revision), Issuer: issuer, GrantTTL: time.Minute, Now: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := coordinator.Routes.ConfirmedRequirements(childScope, time.Now().UTC())
	if err != nil || !confirmed[requirement] || len(state.Confirmations) != 1 {
		t.Fatalf("projected confirmations=%#v state=%#v err=%v", confirmed, state.Confirmations, err)
	}
}

func TestSemanticExecutionCoordinatorRejectConsumesGrantAndAdmissionIsAtomic(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("read_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("k", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	grant, selection := grants[0], plan.Selections[0]
	invalidID := HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "invalid"}
	rejected := SemanticExecutionAdmission{Identity: invalidID, Grant: grant, RequestDigest: "invalid:abc", Scope: scope, Selection: selection, Now: time.Now().UTC()}
	if _, action, err := coordinator.Reject(rejected, "[system rejected] parameter_schema_invalid", "parameter_schema_invalid"); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("reject action=%q err=%v", action, err)
	}
	if _, err := issuer.ValidateAndConsume(grant, scope, plan); err == nil || err.Error() != "invocation_grant_replayed" {
		t.Fatalf("parameter rejection did not consume grant: %v", err)
	}
	if record, action, err := coordinator.Reject(rejected, "[system rejected] parameter_schema_invalid", "parameter_schema_invalid"); err != nil || action != HostCallAcquireReplay || record.Result != "[system rejected] parameter_schema_invalid" {
		t.Fatalf("rejected host-call replay=%#v action=%q err=%v", record, action, err)
	}
	if record, err := coordinator.Executions.Execution(scope, selection.ID); err != nil || record.State != PlanExecutionFailed || record.ReasonCode != "parameter_schema_invalid" {
		t.Fatalf("rejection execution=%#v err=%v", record, err)
	}
	// A new plan scope is required after a rejected one-shot call. This also
	// avoids treating a failed selection record as an accidental retry target.
	plan, err = NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root-2", SessionID: "session", TurnID: "turn-2", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("recovery plan=%#v err=%v", plan, err)
	}
	scope = InvocationScope{RootTaskID: "root-2", PlanID: plan.ID, SessionID: "session", TurnID: "turn-2", PrincipalID: "principal"}
	selection = plan.Selections[0]
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	grants, err = issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("second grants=%#v err=%v", grants, err)
	}
	grant = grants[0]
	admission := SemanticExecutionAdmission{Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "valid"}, Grant: grant, RequestDigest: "request:canonical", Scope: scope, Selection: selection, Now: time.Now().UTC()}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	if record, err := coordinator.Executions.Execution(scope, selection.ID); err != nil || record.State != PlanExecutionRunning {
		t.Fatalf("execution=%#v err=%v", record, err)
	}
	if _, err := issuer.ValidateAndConsume(grant, scope, plan); err == nil || err.Error() != "invocation_grant_replayed" {
		t.Fatalf("grant not atomically consumed err=%v", err)
	}
	if record, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireInProgress || record.State != HostCallAdmitted {
		t.Fatalf("replay record=%#v action=%q err=%v", record, action, err)
	}
	if _, err := coordinator.Complete(admission, PlanExecutionSucceeded, "read result", "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if projected, err := coordinator.Routes.CompletedSelections(scope); err != nil || !projected[selection.ID] {
		t.Fatalf("completion was not atomically projected=%#v err=%v", projected, err)
	}
	if record, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireReplay || record.Result != "read result" {
		t.Fatalf("terminal replay=%#v action=%q err=%v", record, action, err)
	}
}

func TestSemanticExecutionCoordinatorRejectsSupersededRevisionBeforeGrantConsumption(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()

	registry := semanticRegistry(t)
	provider := semanticProvider("read_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root-stale", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: "root-stale", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("s", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	grant, selection := grants[0], plan.Selections[0]
	publishChildRevision(t, coordinator, scope, plan)

	admission := SemanticExecutionAdmission{
		Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "stale-admit"},
		Grant:    grant, RequestDigest: "request:canonical", Scope: scope, Selection: selection, Now: time.Now().UTC(),
	}
	if _, _, err := coordinator.Admit(admission); err == nil || err.Error() != "route_revision_superseded" {
		t.Fatalf("stale admission err=%v", err)
	}
	var grantState string
	if err := coordinator.db.QueryRow(`SELECT state FROM invocation_grants WHERE nonce=? AND fingerprint=?`, grant.Nonce, InvocationGrantFingerprint(grant)).Scan(&grantState); err != nil || grantState != "issued" {
		t.Fatalf("superseded admission grant state=%q err=%v", grantState, err)
	}
	if _, _, err := coordinator.Reject(admission, "[system rejected] parameter_schema_invalid", "parameter_schema_invalid"); err == nil || err.Error() != "route_revision_superseded" {
		t.Fatalf("stale rejection err=%v", err)
	}
	if err := coordinator.db.QueryRow(`SELECT state FROM invocation_grants WHERE nonce=? AND fingerprint=?`, grant.Nonce, InvocationGrantFingerprint(grant)).Scan(&grantState); err != nil || grantState != "issued" {
		t.Fatalf("superseded rejection grant state=%q err=%v", grantState, err)
	}
}

func TestSemanticExecutionCoordinatorDeliveryOutboxDoesNotReplayUnknown(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	payload, err := NewArtifactPayload(scope, "selection:producer", "document", "text/plain", "cGF5bG9hZA==", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Artifacts.Publish(payload); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("d", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	admission := SemanticExecutionAdmission{Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "delivery"}, Grant: grants[0], RequestDigest: "request:delivery", Scope: scope, Selection: plan.Selections[0], Now: time.Now().UTC()}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	record, host, err := coordinator.PrepareDeliveryAndComplete(admission, DeliveryRecord{Scope: scope, SelectionID: plan.Selections[0].ID, ArtifactID: payload.Ref.ID, ArtifactSourceScope: scope, ChannelScope: "test-channel", DestinationID: "group:one", State: DeliveryPrepared}, "prepared", "channel_delivery_prepared", time.Now().UTC())
	if err != nil || record.State != DeliveryPrepared || host.State != HostCallCompleted || host.Result != "prepared" {
		t.Fatalf("prepare+complete record=%#v host=%#v err=%v", record, host, err)
	}
	if execution, err := coordinator.Executions.Execution(scope, plan.Selections[0].ID); err != nil || execution.State != PlanExecutionAwaitingReceipt {
		t.Fatalf("atomic awaiting execution=%#v err=%v", execution, err)
	}
	if host, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireReplay || host.Result != "prepared" {
		t.Fatalf("prepared host replay=%#v action=%q err=%v", host, action, err)
	}
	claim, claimed, err := coordinator.ClaimDelivery(scope, plan.Selections[0].ID, time.Now().UTC())
	if err != nil || !claimed || claim.Delivery.State != DeliveryDispatching || claim.Payload.Ref.ID != payload.Ref.ID {
		t.Fatalf("claim=%#v claimed=%v err=%v", claim, claimed, err)
	}
	if settled, err := coordinator.SettleDelivery(scope, plan.Selections[0].ID, DeliveryUnknown, "transport-evidence", "transport_timeout", time.Now().UTC()); err != nil || settled.ReceiptDigest != "transport-evidence" {
		t.Fatalf("unknown settlement=%#v err=%v", settled, err)
	}
	if claim, claimed, err := coordinator.ClaimDelivery(scope, plan.Selections[0].ID, time.Now().UTC()); err != nil || claimed || claim.Delivery.State != DeliveryUnknown {
		t.Fatalf("unknown replay claim=%#v claimed=%v err=%v", claim, claimed, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || completed[plan.Selections[0].ID] {
		t.Fatalf("unknown delivery unlocked DAG=%#v err=%v", completed, err)
	}
}

func TestSemanticExecutionCoordinatorCompletionRejectsUnpublishedArtifact(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	provider.Produces = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root-artifact", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: "root-artifact", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("e", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	admission := SemanticExecutionAdmission{Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "artifact"}, Grant: grants[0], RequestDigest: "request:artifact", Scope: scope, Selection: plan.Selections[0], Now: time.Now().UTC()}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	forged, err := NewArtifactPayload(scope, plan.Selections[0].ID, "image", "image/png", "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLNYAAAAABJRU5ErkJggg==", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.CompleteWithArtifacts(admission, PlanExecutionSucceeded, "captured", "", []ArtifactRef{forged.Ref}, time.Now().UTC()); err == nil || err.Error() != "route_artifact_not_published" {
		t.Fatalf("unpublished artifact completion err=%v", err)
	}
	if execution, err := coordinator.Executions.Execution(scope, plan.Selections[0].ID); err != nil || execution.State != PlanExecutionRunning {
		t.Fatalf("failed commit changed execution=%#v err=%v", execution, err)
	}
}

func TestSemanticExecutionCoordinatorRequiresReceiptToAcceptDelivery(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root-receipt", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: "root-receipt", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	payload, err := NewArtifactPayload(scope, "selection:producer", "document", "text/plain", "cGF5bG9hZA==", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Artifacts.Publish(payload); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("r", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	admission := SemanticExecutionAdmission{Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "receipt"}, Grant: grants[0], RequestDigest: "request:receipt", Scope: scope, Selection: plan.Selections[0], Now: time.Now().UTC()}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	if _, _, err := coordinator.PrepareDeliveryAndComplete(admission, DeliveryRecord{Scope: scope, SelectionID: plan.Selections[0].ID, ArtifactID: payload.Ref.ID, ArtifactSourceScope: scope, ChannelScope: "channel", DestinationID: "user:one", State: DeliveryPrepared}, "prepared", "channel_delivery_prepared", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, claimed, err := coordinator.ClaimDelivery(scope, plan.Selections[0].ID, time.Now().UTC()); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if _, err := coordinator.SettleDelivery(scope, plan.Selections[0].ID, DeliveryAccepted, "", "channel_delivery_accepted", time.Now().UTC()); err == nil || err.Error() != "delivery_acceptance_receipt_required" {
		t.Fatalf("missing acceptance receipt err=%v", err)
	}
	if record, err := coordinator.SettleDelivery(scope, plan.Selections[0].ID, DeliveryAccepted, "provider-receipt-digest", "channel_delivery_accepted", time.Now().UTC()); err != nil || record.ReceiptDigest != "provider-receipt-digest" {
		t.Fatalf("accepted record=%#v err=%v", record, err)
	}
}

func TestSemanticExecutionCoordinatorAtomicallyPersistsProducedArtifact(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	provider.Produces = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	delivery := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	delivery.Consumes = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root-payload", SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider, delivery}), Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}, {ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}}})
	if err != nil || len(plan.Selections) != 2 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: "root-payload", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("f", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	admission := SemanticExecutionAdmission{Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "payload"}, Grant: grants[0], RequestDigest: "request:payload", Scope: scope, Selection: plan.Selections[0], Now: time.Now().UTC()}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	payload, err := NewArtifactPayload(scope, plan.Selections[0].ID, "image", "image/png", "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLNYAAAAABJRU5ErkJggg==", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.CompleteWithArtifactPayloads(admission, PlanExecutionSucceeded, "captured", "", []ArtifactPayload{payload}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if refs, err := coordinator.Artifacts.PublishedArtifacts(scope, plan.Selections[0].ID); err != nil || len(refs) != 1 || !sameArtifactIdentity(refs[0], payload.Ref) {
		t.Fatalf("published refs=%#v err=%v", refs, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || !completed[plan.Selections[0].ID] {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
}

func TestSemanticExecutionCoordinatorDocumentPayloadKindOnlyConsumerDoesNotCorruptRoute(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := NewCapabilityRegistry("v1")
	for _, descriptor := range []CapabilityDescriptor{
		{ID: "document.generate.file", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"pdf"}, Required: true}}, Effects: []EffectClass{EffectLocalMutation}},
		{ID: "artifact.deliver.current_channel", Version: "v1", Qualifiers: map[string]QualifierConstraint{"format": {Values: []string{"file"}, Required: true}}, Effects: []EffectClass{EffectExternalEffect}},
	} {
		if err := registry.Register(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	generate := semanticProvider("generate_adapter", "document.generate.file", map[string]string{"format": "pdf"}, EffectLocalMutation)
	generate.Produces = []ArtifactContract{{Kind: "document", MIMEType: "application/pdf", Required: true}}
	delivery := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	delivery.Consumes = []ArtifactContract{{Kind: "document", Required: true}}
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "root-pdf", SessionID: "session", TurnID: "turn",
		Snapshot: semanticSnapshot(t, registry, []ProviderSpec{generate, delivery}),
		Needs: []CapabilityNeed{
			{ID: "generate", Capability: "document.generate.file", Qualifiers: map[string]string{"format": "pdf"}, Required: true},
			{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true},
		},
	})
	if err != nil || len(plan.Selections) != 2 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: "root-pdf", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("g", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.IssueReady(plan, scope, time.Minute, nil)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	generateSel := plan.Selections[0]
	for _, selection := range plan.Selections {
		if selection.FitProof.MatchedCapability == "document.generate.file" {
			generateSel = selection
		}
	}
	grant := grants[0]
	if grant.SelectionID != generateSel.ID {
		t.Fatalf("issued grant=%#v generate=%#v", grant, generateSel)
	}
	admission := SemanticExecutionAdmission{Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "pdf"}, Grant: grant, RequestDigest: "request:pdf", Scope: scope, Selection: generateSel, Now: time.Now().UTC()}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	if _, err := coordinator.Routes.RecordMaterialization(scope, plan.ID, RouteMaterialization{FunctionName: grants[0].Token, Grant: grants[0], State: RouteMaterializationExposed}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	payload, err := NewArtifactPayload(scope, admission.Selection.ID, "document", "application/pdf", "JVBERi0xLjQK", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.CompleteWithArtifactPayloads(admission, PlanExecutionSucceeded, "generated", "", []ArtifactPayload{payload}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Routes.RetireMaterialization(scope, plan.ID, grants[0].Token, time.Now().UTC()); err != nil {
		t.Fatalf("kind-only file deliver must still accept a PDF artifact: %v", err)
	}
}

func TestSemanticExecutionCoordinatorExternalEffectReceiptSettlementIsAtomic(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	registry := semanticRegistry(t)
	provider := semanticProvider("dynamic_send", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root-effect", SessionID: "session-effect", TurnID: "turn-effect", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "send", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: "root-effect", PlanID: plan.ID, SessionID: "session-effect", TurnID: "turn-effect", PrincipalID: "principal-effect"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("x", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	selection := plan.Selections[0]
	admission := SemanticExecutionAdmission{Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: "external-effect"}, Grant: grants[0], RequestDigest: "request:external-effect", Scope: scope, Selection: selection, Now: time.Now().UTC()}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	op := SemanticExternalEffectOperation{OperationKey: "operation-effect", Scope: scope, TenantID: "tenant-effect", UserID: "user-effect", SelectionID: selection.ID, SelectionDigest: selectionPurposeDigest(selection), BindingID: selection.Provider.StableID(), RequestDigest: "canonical-request"}
	if prepared, execute, err := coordinator.PrepareExternalEffect(admission, op); err != nil || !execute || prepared.State != SemanticExternalEffectRunning {
		t.Fatalf("prepare=%#v execute=%v err=%v", prepared, execute, err)
	}
	if _, execute, err := coordinator.PrepareExternalEffect(admission, op); err != nil || execute {
		t.Fatalf("duplicate prepare execute=%v err=%v", execute, err)
	}
	if _, err := coordinator.CompleteExternalEffectDispatch(admission, op.OperationKey, SemanticExternalEffectAwaitingReceipt, "provider accepted locally", "awaiting_gateway_receipt", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if execution, err := coordinator.Executions.Execution(scope, selection.ID); err != nil || execution.State != PlanExecutionAwaitingReceipt {
		t.Fatalf("execution=%#v err=%v", execution, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || completed[selection.ID] {
		t.Fatalf("awaiting receipt completed=%#v err=%v", completed, err)
	}
	if _, err := coordinator.SettleExternalEffectReceipt(scope, selection.ID, op.SelectionDigest, op.BindingID, op.OperationKey, SemanticExternalEffectSucceeded, "", "receipt_missing", time.Now().UTC()); err == nil || err.Error() != "semantic_external_effect_receipt_required" {
		t.Fatalf("missing receipt settlement err=%v", err)
	}
	settled, err := coordinator.SettleExternalEffectReceipt(scope, selection.ID, op.SelectionDigest, op.BindingID, op.OperationKey, SemanticExternalEffectSucceeded, "receipt-digest", "gateway_accepted", time.Now().UTC())
	if err != nil || settled.State != SemanticExternalEffectSucceeded || settled.ReceiptDigest != "receipt-digest" {
		t.Fatalf("settled=%#v err=%v", settled, err)
	}
	if execution, err := coordinator.Executions.Execution(scope, selection.ID); err != nil || execution.State != PlanExecutionSucceeded {
		t.Fatalf("settled execution=%#v err=%v", execution, err)
	}
	if completed, err := coordinator.Routes.CompletedSelections(scope); err != nil || !completed[selection.ID] {
		t.Fatalf("settled completed=%#v err=%v", completed, err)
	}
	var projectionCount int
	if err := coordinator.db.QueryRow(`SELECT COUNT(*) FROM semantic_continuity_projection_outbox WHERE route_key=? AND event_kind='execution_updated'`, routeStateKey(scope)).Scan(&projectionCount); err != nil || projectionCount != 2 {
		t.Fatalf("receipt continuity projection count=%d err=%v", projectionCount, err)
	}
	if _, err := coordinator.SettleExternalEffectReceipt(scope, selection.ID, op.SelectionDigest, op.BindingID, op.OperationKey, SemanticExternalEffectSucceeded, "other-receipt", "gateway_accepted", time.Now().UTC()); err == nil || err.Error() != "semantic_external_effect_receipt_conflict" {
		t.Fatalf("conflicting receipt err=%v", err)
	}
}

// admittedCoordinatorCall drives one external-effect selection up to the point
// where the provider has run and only the terminal commit is left.
func admittedCoordinatorCall(t *testing.T, coordinator *SQLiteSemanticExecutionCoordinator, root, callID string) (InvocationScope, PlannedSelection, SemanticExecutionAdmission) {
	t.Helper()
	registry := semanticRegistry(t)
	provider := semanticProvider("dynamic_send", "artifact.deliver.current_channel", map[string]string{"format": "file"}, EffectExternalEffect)
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: root, SessionID: "session", TurnID: "turn", Snapshot: semanticSnapshot(t, registry, []ProviderSpec{provider}), Needs: []CapabilityNeed{{ID: "send", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "file"}, Required: true}}})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	scope := InvocationScope{RootTaskID: root, PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	if _, err := coordinator.Routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("k", 32)), coordinator.Grants)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	admission := SemanticExecutionAdmission{
		Identity: HostCallIdentity{Protocol: "test", ConnectionID: "connection", CallID: callID},
		Grant:    grants[0], RequestDigest: "request:canonical", Scope: scope, Selection: plan.Selections[0], Now: time.Now().UTC(),
	}
	if _, action, err := coordinator.Admit(admission); err != nil || action != HostCallAcquireAdmit {
		t.Fatalf("admit action=%q err=%v", action, err)
	}
	return scope, plan.Selections[0], admission
}

// An unknown outcome must not become a replayable completed host call.
//
// Replay reconstructs its verdict from the stored text, and an unknown result
// carries no rejection prefix, so a completed row turns "nobody ever observed
// whether this effect happened" into "it happened" the next time the same call
// ID arrives. The uncoordinated journal path already refuses this by writing
// MarkUnknown instead of Complete; the coordinated path used to write
// 'completed' for every terminal state, unknown included.
func TestSemanticExecutionCoordinatorUnknownOutcomeIsNotAReplayableCompletion(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	scope, selection, admission := admittedCoordinatorCall(t, coordinator, "root-unknown-replay", "unknown-call")
	if _, err := coordinator.Complete(admission, PlanExecutionUnknown, "[system unknown] host_channel_vanished", "host_channel_vanished", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if record, err := coordinator.Executions.Execution(scope, selection.ID); err != nil || record.State != PlanExecutionUnknown {
		t.Fatalf("execution=%#v err=%v", record, err)
	}
	record, action, err := coordinator.Admit(admission)
	if err != nil {
		t.Fatal(err)
	}
	if action == HostCallAcquireReplay {
		t.Fatalf("unknown outcome was journalled as a replayable completion: record=%#v", record)
	}
	if action != HostCallAcquireUnknown || record.State != HostCallUnknown {
		t.Fatalf("unknown acquire action=%q state=%q", action, record.State)
	}
	// The provider's own words stay on the row: losing observation is not a
	// reason to lose the only forensic evidence of what it said.
	if record.Result != "[system unknown] host_channel_vanished" {
		t.Fatalf("unknown record dropped its result: %q", record.Result)
	}
	if projected, err := coordinator.Routes.CompletedSelections(scope); err != nil || projected[selection.ID] {
		t.Fatalf("unknown outcome projected a route completion=%#v err=%v", projected, err)
	}
}

// A definite failure stays a completed host call. The same call ID must get
// the same refusal back rather than a second attempt, so narrowing the unknown
// case must not narrow this one with it.
func TestSemanticExecutionCoordinatorFailedOutcomeStaysReplayable(t *testing.T) {
	coordinator, err := NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic-execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	scope, selection, admission := admittedCoordinatorCall(t, coordinator, "root-failed-replay", "failed-call")
	if _, err := coordinator.Complete(admission, PlanExecutionFailed, "[system rejected] channel_refused", "channel_refused", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	record, action, err := coordinator.Admit(admission)
	if err != nil {
		t.Fatal(err)
	}
	if action != HostCallAcquireReplay || record.State != HostCallCompleted || record.Result != "[system rejected] channel_refused" {
		t.Fatalf("failed replay=%#v action=%q", record, action)
	}
	if projected, err := coordinator.Routes.CompletedSelections(scope); err != nil || projected[selection.ID] {
		t.Fatalf("failed outcome projected a route completion=%#v err=%v", projected, err)
	}
}
