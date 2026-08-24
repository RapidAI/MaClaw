package tool

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func routeStateTestPlan(t *testing.T) (ToolPlan, InvocationScope, InvocationGrant) {
	t.Helper()
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "task", TurnID: "turn", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	scope := InvocationScope{RootTaskID: "task", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "principal"}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("r", 32)))
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("issue grants=%#v err=%v", grants, err)
	}
	return plan, scope, grants[0]
}

func exerciseRouteStateStore(t *testing.T, store RouteStateStore) {
	t.Helper()
	plan, scope, grant := routeStateTestPlan(t)
	now := time.Date(2026, 8, 15, 1, 2, 3, 0, time.UTC)
	state, err := store.Open(scope, plan, now)
	if err != nil || state.PlanDigest == "" || state.Plan.ID != plan.ID {
		t.Fatalf("open state=%+v err=%v", state, err)
	}
	materialization := RouteMaterialization{FunctionName: grant.Token, Grant: grant, State: RouteMaterializationExposed}
	state, err = store.RecordMaterialization(scope, plan.ID, materialization, now.Add(time.Second))
	if err != nil || len(state.Materializations) != 1 || state.Materializations[0].State != RouteMaterializationExposed {
		t.Fatalf("record state=%+v err=%v", state, err)
	}
	if _, err := store.RecordMaterialization(scope, plan.ID, materialization, now.Add(2*time.Second)); err != nil {
		t.Fatalf("idempotent record: %v", err)
	}
	state, err = store.RetireMaterialization(scope, plan.ID, grant.Token, now.Add(3*time.Second))
	if err != nil || state.Materializations[0].State != RouteMaterializationRetired {
		t.Fatalf("retire state=%+v err=%v", state, err)
	}
	if _, err := store.RetireMaterialization(scope, plan.ID, grant.Token, now.Add(4*time.Second)); err != nil {
		t.Fatalf("idempotent retire: %v", err)
	}
	changed := plan
	changed.Selections[0].AdapterName = "other_adapter"
	if _, err := store.Open(scope, changed, now); err == nil || err.Error() != "route_state_conflict" {
		t.Fatalf("changed immutable plan err=%v", err)
	}
}

func TestMemoryRouteStateStorePersistsImmutableMaterializations(t *testing.T) {
	exerciseRouteStateStore(t, NewMemoryRouteStateStore())
}

func TestSQLiteRouteStateStoreRecoversImmutableMaterializations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "route-state.db")
	store, err := NewSQLiteRouteStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	exerciseRouteStateStore(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	plan, scope, _ := routeStateTestPlan(t)
	restarted, err := NewSQLiteRouteStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	state, err := restarted.Open(scope, plan, time.Now().UTC())
	if err != nil || len(state.Materializations) != 1 || state.Materializations[0].State != RouteMaterializationRetired {
		t.Fatalf("restart state=%+v err=%v", state, err)
	}
}

func TestRouteStateStoreRejectsConflictingFunctionMaterialization(t *testing.T) {
	store := NewMemoryRouteStateStore()
	plan, scope, grant := routeStateTestPlan(t)
	if _, err := store.Open(scope, plan, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordMaterialization(scope, plan.ID, RouteMaterialization{FunctionName: grant.Token, Grant: grant, State: RouteMaterializationExposed}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	forged := grant
	forged.Nonce = "other"
	if _, err := store.RecordMaterialization(scope, plan.ID, RouteMaterialization{FunctionName: grant.Token, Grant: forged, State: RouteMaterializationExposed}, time.Now().UTC()); err == nil || err.Error() != "route_state_materialization_conflict" {
		t.Fatalf("conflicting materialization err=%v", err)
	}
}

func TestRouteStateStoreRejectsGrantBoundToDifferentSelection(t *testing.T) {
	store := NewMemoryRouteStateStore()
	plan, scope, grant := routeStateTestPlan(t)
	if _, err := store.Open(scope, plan, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	forged := grant
	forged.ProviderBinding = "builtin:other:other:schema"
	if _, err := store.RecordMaterialization(scope, plan.ID, RouteMaterialization{FunctionName: forged.Token, Grant: forged, State: RouteMaterializationExposed}, time.Now().UTC()); err == nil || err.Error() != "route_state_grant_binding_mismatch" {
		t.Fatalf("wrong binding err=%v", err)
	}
}

func revisedRouteStatePlan(t *testing.T, plan ToolPlan, rootTaskID, turnID string) ToolPlan {
	t.Helper()
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	revised, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: rootTaskID, TurnID: turnID, Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if revised.ID == plan.ID {
		t.Fatal("revision needs a distinct immutable plan id")
	}
	return revised
}

func routeRevisionPublish(plan ToolPlan, scope InvocationScope, parent *RouteRevisionRef) RouteRevisionPublishRequest {
	return RouteRevisionPublishRequest{Scope: scope, Plan: plan, ExpectedParent: parent, SnapshotDigest: plan.SnapshotDigest}
}

func exerciseRouteRevisionStore(t *testing.T, store RouteStateStore) {
	t.Helper()
	parentPlan, parentScope, parentGrant := routeStateTestPlan(t)
	now := time.Date(2026, 8, 15, 2, 3, 4, 0, time.UTC)
	parent, err := store.PublishRevision(routeRevisionPublish(parentPlan, parentScope, nil), now)
	if err != nil || parent.Revision == nil || parent.Revision.Revision != 1 {
		t.Fatalf("publish parent state=%+v err=%v", parent, err)
	}
	if _, err := store.RecordMaterialization(parentScope, parentPlan.ID, RouteMaterialization{FunctionName: parentGrant.Token, Grant: parentGrant, State: RouteMaterializationExposed}, now); err != nil {
		t.Fatal(err)
	}
	childPlan := revisedRouteStatePlan(t, parentPlan, parentScope.RootTaskID, "turn-child")
	childScope := parentScope
	childScope.PlanID, childScope.TurnID = childPlan.ID, "turn-child"
	child, err := store.PublishRevision(routeRevisionPublish(childPlan, childScope, parent.Revision), now.Add(time.Second))
	if err != nil || child.Revision == nil || child.Revision.Revision != 2 || child.ParentRevision == nil || !sameRouteRevisionRef(*child.ParentRevision, *parent.Revision) {
		t.Fatalf("publish child state=%+v err=%v", child, err)
	}
	if err := store.IsCurrent(parentScope); err == nil || err.Error() != "route_revision_superseded" {
		t.Fatalf("parent current err=%v", err)
	}
	if err := store.IsCurrent(childScope); err != nil {
		t.Fatalf("child current err=%v", err)
	}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("x", 32)))
	if err != nil {
		t.Fatal(err)
	}
	oldGrants, err := issuer.Issue(parentPlan, parentScope, time.Minute)
	if err != nil || len(oldGrants) != 1 {
		t.Fatalf("issue old grant grants=%+v err=%v", oldGrants, err)
	}
	executor, err := NewPlanExecutorWithRouteState(issuer, NewMemoryPlanExecutionStore(), store)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := executor.Execute(oldGrants[0], parentScope, parentPlan, nil, func(PlannedSelection) SelectionExecutionResult {
		return SelectionExecutionResult{Succeeded: true}
	}); err == nil || err.Error() != "route_revision_superseded" {
		t.Fatalf("old revision execution err=%v", err)
	}
	retired, err := store.Open(parentScope, parentPlan, now)
	if err != nil || len(retired.Materializations) != 1 || retired.Materializations[0].State != RouteMaterializationRetired {
		t.Fatalf("parent retirement state=%+v err=%v", retired, err)
	}
	if _, err := store.PublishRevision(routeRevisionPublish(childPlan, childScope, parent.Revision), now.Add(2*time.Second)); err != nil {
		t.Fatalf("idempotent child publication: %v", err)
	}
	stale := *parent.Revision
	stale.Revision++
	if _, err := store.PublishRevision(routeRevisionPublish(childPlan, childScope, &stale), now.Add(2*time.Second)); err == nil {
		t.Fatalf("child publication with stale parent err=%v", err)
	}
	if _, err := store.PublishRevision(RouteRevisionPublishRequest{Scope: InvocationScope{RootTaskID: parentScope.RootTaskID, PlanID: "plan:stale", SessionID: parentScope.SessionID, TurnID: "turn-stale", PrincipalID: parentScope.PrincipalID}, Plan: ToolPlan{RootTaskID: parentScope.RootTaskID, ID: "plan:stale"}, ExpectedParent: parent.Revision, SnapshotDigest: "snapshot:stale"}, now); err == nil || err.Error() != "route_revision_conflict" {
		t.Fatalf("stale parent publication err=%v", err)
	}
}

func TestMemoryRouteStateStorePublishesRevisionAndRetiresParent(t *testing.T) {
	exerciseRouteRevisionStore(t, NewMemoryRouteStateStore())
}

func TestSQLiteRouteStateStorePublishesRevisionAndRecoversLineage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "route-state.db")
	store, err := NewSQLiteRouteStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exerciseRouteRevisionStore(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	parentPlan, parentScope, _ := routeStateTestPlan(t)
	childPlan := revisedRouteStatePlan(t, parentPlan, parentScope.RootTaskID, "turn-child")
	childScope := parentScope
	childScope.PlanID, childScope.TurnID = childPlan.ID, "turn-child"
	restarted, err := NewSQLiteRouteStateStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	state, err := restarted.Open(childScope, childPlan, time.Now().UTC())
	if err != nil || state.Revision == nil || state.Revision.Revision != 2 || state.ParentRevision == nil {
		t.Fatalf("recover revision state=%+v err=%v", state, err)
	}
	if err := restarted.IsCurrent(parentScope); err == nil || err.Error() != "route_revision_superseded" {
		t.Fatalf("recovered parent current err=%v", err)
	}
}

func TestRouteRevisionCompareAndPublishAllowsOneConcurrentChild(t *testing.T) {
	store := NewMemoryRouteStateStore()
	parentPlan, parentScope, _ := routeStateTestPlan(t)
	parent, err := store.PublishRevision(routeRevisionPublish(parentPlan, parentScope, nil), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	candidates := make([]struct {
		plan  ToolPlan
		scope InvocationScope
	}, 2)
	for index := range candidates {
		candidatePlan := revisedRouteStatePlan(t, parentPlan, parentScope.RootTaskID, "turn-child-"+string(rune('a'+index)))
		candidateScope := parentScope
		candidateScope.PlanID, candidateScope.TurnID = candidatePlan.ID, "turn-child-"+string(rune('a'+index))
		candidates[index] = struct {
			plan  ToolPlan
			scope InvocationScope
		}{plan: candidatePlan, scope: candidateScope}
	}
	var successes int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			candidate := candidates[index]
			if _, err := store.PublishRevision(routeRevisionPublish(candidate.plan, candidate.scope, parent.Revision), time.Now().UTC()); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}(index)
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("concurrent publications succeeded=%d, want 1", successes)
	}
}

func TestRouteRevisionSupersedesLegacyStateInSameAuthorityScope(t *testing.T) {
	store := NewMemoryRouteStateStore()
	legacyPlan, legacyScope, _ := routeStateTestPlan(t)
	if _, err := store.Open(legacyScope, legacyPlan, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	publishedPlan := revisedRouteStatePlan(t, legacyPlan, legacyScope.RootTaskID, "turn-published")
	publishedScope := legacyScope
	publishedScope.PlanID, publishedScope.TurnID = publishedPlan.ID, "turn-published"
	if _, err := store.PublishRevision(routeRevisionPublish(publishedPlan, publishedScope, nil), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.IsCurrent(legacyScope); err == nil || err.Error() != "route_revision_superseded" {
		t.Fatalf("legacy state after publication err=%v", err)
	}
}

func TestRouteRevisionRejectsMismatchedPlannerSnapshotDigest(t *testing.T) {
	store := NewMemoryRouteStateStore()
	plan, scope, _ := routeStateTestPlan(t)
	if _, err := store.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: "other-snapshot"}, time.Now().UTC()); err == nil || err.Error() != "route_snapshot_digest_mismatch" {
		t.Fatalf("mismatched snapshot digest err=%v", err)
	}
}

func exerciseRouteRevisionProjectsCompletedSelection(t *testing.T, store RouteStateStore) {
	t.Helper()
	parentPlan, parentScope, _ := routeStateTestPlan(t)
	parent, err := store.PublishRevision(routeRevisionPublish(parentPlan, parentScope, nil), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	selectionID := parentPlan.Selections[0].ID
	if _, err := store.RecordSelectionCompletion(parentScope, parentPlan.ID, selectionID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	childPlan := revisedRouteStatePlan(t, parentPlan, parentScope.RootTaskID, "turn-child")
	childScope := parentScope
	childScope.PlanID, childScope.TurnID = childPlan.ID, "turn-child"
	if _, err := store.PublishRevision(routeRevisionPublish(childPlan, childScope, parent.Revision), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	projected, err := store.CompletedSelections(childScope)
	if err != nil || !projected[selectionID] {
		t.Fatalf("projected completions=%#v err=%v", projected, err)
	}
	changed := childPlan
	changed.Selections[0].Effects = []EffectClass{EffectExternalEffect}
	changed.SnapshotDigest = ""
	changedScope := childScope
	changedScope.PlanID, changedScope.TurnID = "plan:changed-purpose", "turn-changed"
	changed.ID = changedScope.PlanID
	changedState, err := store.PublishRevision(RouteRevisionPublishRequest{Scope: changedScope, Plan: changed, ExpectedParent: mustCurrentRevision(t, store, childScope), SnapshotDigest: "snapshot:changed-purpose"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if changedState.Revision == nil {
		t.Fatal("changed revision was not published")
	}
	projected, err = store.CompletedSelections(changedScope)
	if err != nil || len(projected) != 0 {
		t.Fatalf("changed-purpose completion leaked: %#v err=%v", projected, err)
	}
}

func mustCurrentRevision(t *testing.T, store RouteStateStore, scope InvocationScope) *RouteRevisionRef {
	t.Helper()
	ref, err := store.CurrentRevision(scope)
	if err != nil {
		t.Fatal(err)
	}
	return &ref
}

func TestMemoryRouteRevisionProjectsCompletedSelection(t *testing.T) {
	exerciseRouteRevisionProjectsCompletedSelection(t, NewMemoryRouteStateStore())
}

func TestSQLiteRouteRevisionProjectsCompletedSelection(t *testing.T) {
	store, err := NewSQLiteRouteStateStore(filepath.Join(t.TempDir(), "route-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exerciseRouteRevisionProjectsCompletedSelection(t, store)
}

func confirmationRouteTestPlan(t *testing.T, turnID string) (ToolPlan, InvocationScope) {
	t.Helper()
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect),
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "root", SessionID: "session", TurnID: turnID, Snapshot: snapshot,
		Needs:       []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}},
		Constraints: []RoutingConstraint{{ID: "confirm", Capability: "artifact.deliver.current_channel", Effect: "require_confirmation", Authority: AuthorityPolicy}},
	})
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: turnID, PrincipalID: "principal"}
	return plan, scope
}

func exerciseRouteRevisionProjectsConfirmation(t *testing.T, store RouteStateStore) {
	t.Helper()
	parentPlan, parentScope := confirmationRouteTestPlan(t, "turn-parent")
	parent, err := store.PublishRevision(RouteRevisionPublishRequest{Scope: parentScope, Plan: parentPlan, SnapshotDigest: parentPlan.SnapshotDigest}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	requirement := parentPlan.Selections[0].ConfirmationID
	fact := RoutingFact{ID: "approval", Kind: "confirmation_granted", Authority: AuthorityPolicy, Attributes: map[string]string{"root_task_id": "root", "confirmation_requirement": requirement}, ValidUntil: time.Now().UTC().Add(time.Hour)}
	if _, err := store.RecordConfirmation(parentScope, parentPlan.ID, fact, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	childPlan, childScope := confirmationRouteTestPlan(t, "turn-child")
	if _, err := store.PublishRevision(RouteRevisionPublishRequest{Scope: childScope, Plan: childPlan, ExpectedParent: parent.Revision, SnapshotDigest: childPlan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	confirmed, err := store.ConfirmedRequirements(childScope, time.Now().UTC())
	if err != nil || !confirmed[requirement] {
		t.Fatalf("projected confirmation=%#v err=%v", confirmed, err)
	}
	if _, err := store.RecordConfirmation(childScope, childPlan.ID, RoutingFact{Kind: "confirmation_granted", Authority: AuthorityUser, Attributes: map[string]string{"root_task_id": "root", "confirmation_requirement": requirement}}, time.Now().UTC()); err == nil || err.Error() != "route_confirmation_authority_invalid" {
		t.Fatalf("untrusted confirmation err=%v", err)
	}
	changed := childPlan
	changed.Selections[0].Effects = []EffectClass{EffectReadOnly}
	changed.SnapshotDigest = ""
	changed.ID = "plan:changed-confirmation-purpose"
	changedScope := childScope
	changedScope.PlanID, changedScope.TurnID = changed.ID, "turn-changed"
	if _, err := store.PublishRevision(RouteRevisionPublishRequest{Scope: changedScope, Plan: changed, ExpectedParent: mustCurrentRevision(t, store, childScope), SnapshotDigest: "snapshot:changed-confirmation-purpose"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	confirmed, err = store.ConfirmedRequirements(changedScope, time.Now().UTC())
	if err != nil || len(confirmed) != 0 {
		t.Fatalf("changed confirmation leaked=%#v err=%v", confirmed, err)
	}
}

func TestMemoryRouteRevisionProjectsConfirmation(t *testing.T) {
	exerciseRouteRevisionProjectsConfirmation(t, NewMemoryRouteStateStore())
}

func TestSQLiteRouteRevisionProjectsConfirmation(t *testing.T) {
	store, err := NewSQLiteRouteStateStore(filepath.Join(t.TempDir(), "route-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exerciseRouteRevisionProjectsConfirmation(t, store)
}

func artifactRouteTestPlan(t *testing.T, turnID string) (ToolPlan, InvocationScope) {
	t.Helper()
	registry := semanticRegistry(t)
	capture := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	capture.Binding = ProviderBinding{Kind: "builtin", ProviderID: "capture", ImplementationID: "capture-v1", SchemaDigest: SchemaDigest([]byte("capture"))}
	capture.Produces = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	delivery := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	delivery.Binding = ProviderBinding{Kind: "channel", ProviderID: "channel", ImplementationID: "delivery-v1", SchemaDigest: SchemaDigest([]byte("delivery"))}
	delivery.Consumes = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		capture,
		delivery,
	})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: turnID, Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}, {ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	return plan, InvocationScope{RootTaskID: "root", PlanID: plan.ID, SessionID: "session", TurnID: turnID, PrincipalID: "principal"}
}

func exerciseRouteRevisionProjectsArtifactRef(t *testing.T, store RouteStateStore) {
	t.Helper()
	parentPlan, parentScope := artifactRouteTestPlan(t, "turn-parent")
	parent, err := store.PublishRevision(routeRevisionPublish(parentPlan, parentScope, nil), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	capture := "selection:capture"
	if _, err := store.RecordSelectionCompletion(parentScope, parentPlan.ID, capture, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	payload, err := NewArtifactPayload(parentScope, capture, "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordArtifact(parentScope, parentPlan.ID, payload.Ref, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	childPlan, childScope := artifactRouteTestPlan(t, "turn-child")
	if _, err := store.PublishRevision(routeRevisionPublish(childPlan, childScope, parent.Revision), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	refs, err := store.ArtifactRefs(childScope)
	if err != nil || len(refs) != 1 || refs[0].SourceScope != parentScope || refs[0].ArtifactID != payload.Ref.ID {
		t.Fatalf("projected refs=%#v err=%v", refs, err)
	}
	changed := childPlan
	changed.ID, changed.SnapshotDigest = "plan:changed-artifact-contract", ""
	for i := range changed.Selections {
		if changed.Selections[i].ID == "selection:deliver" {
			changed.Selections[i].Consumes = []ArtifactContract{{Kind: "image", MIMEType: "image/jpeg", Required: true}}
			changed.Selections[i].ArtifactDependencies = []ArtifactDependency{{ProducerSelection: "selection:capture", Contract: ArtifactContract{Kind: "image", MIMEType: "image/jpeg", Required: true}}}
		}
		if changed.Selections[i].ID == "selection:capture" {
			changed.Selections[i].Produces = []ArtifactContract{{Kind: "image", MIMEType: "image/jpeg", Required: true}}
		}
	}
	changedScope := childScope
	changedScope.PlanID, changedScope.TurnID = changed.ID, "turn-changed"
	if _, err := store.PublishRevision(RouteRevisionPublishRequest{Scope: changedScope, Plan: changed, ExpectedParent: mustCurrentRevision(t, store, childScope), SnapshotDigest: "snapshot:changed-artifact-contract"}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	refs, err = store.ArtifactRefs(changedScope)
	if err != nil || len(refs) != 0 {
		t.Fatalf("changed artifact contract leaked refs=%#v err=%v", refs, err)
	}
}

func TestMemoryRouteRevisionProjectsArtifactRef(t *testing.T) {
	exerciseRouteRevisionProjectsArtifactRef(t, NewMemoryRouteStateStore())
}

func TestSQLiteRouteRevisionProjectsArtifactRef(t *testing.T) {
	store, err := NewSQLiteRouteStateStore(filepath.Join(t.TempDir(), "route-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exerciseRouteRevisionProjectsArtifactRef(t, store)
}

func exerciseRouteRevisionDoesNotProjectArtifactToDifferentProducer(t *testing.T, store RouteStateStore) {
	t.Helper()
	parentPlan, parentScope := artifactRouteTestPlan(t, "turn-parent")
	parent, err := store.PublishRevision(routeRevisionPublish(parentPlan, parentScope, nil), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	capture := "selection:capture"
	if _, err := store.RecordSelectionCompletion(parentScope, parentPlan.ID, capture, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	payload, err := NewArtifactPayload(parentScope, capture, "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordArtifact(parentScope, parentPlan.ID, payload.Ref, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	childPlan, childScope := artifactRouteTestPlan(t, "turn-child")
	var alternate PlannedSelection
	for _, selection := range childPlan.Selections {
		if selection.ID == "selection:capture" {
			alternate = clonePlannedSelection(selection)
			alternate.ID, alternate.NeedID, alternate.AdapterName = "selection:other-capture", "other-capture", "other_capture_adapter"
			break
		}
	}
	if alternate.ID == "" {
		t.Fatal("missing capture selection")
	}
	childPlan.Selections = append(childPlan.Selections, alternate)
	for i := range childPlan.Selections {
		if childPlan.Selections[i].ID != "selection:deliver" {
			continue
		}
		childPlan.Selections[i].ArtifactDependencies = []ArtifactDependency{{ProducerSelection: "selection:other-capture", Contract: ArtifactContract{Kind: "image", MIMEType: "image/png", Required: true}}}
		childPlan.Selections[i].Requires = []string{"selection:other-capture"}
	}
	if _, err := store.PublishRevision(routeRevisionPublish(childPlan, childScope, parent.Revision), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	refs, err := store.ArtifactRefs(childScope)
	if err != nil || len(refs) != 0 {
		t.Fatalf("artifact leaked across a different producer edge refs=%#v err=%v", refs, err)
	}
}

func TestMemoryRouteRevisionDoesNotProjectArtifactToDifferentProducer(t *testing.T) {
	exerciseRouteRevisionDoesNotProjectArtifactToDifferentProducer(t, NewMemoryRouteStateStore())
}

func TestSQLiteRouteRevisionDoesNotProjectArtifactToDifferentProducer(t *testing.T) {
	store, err := NewSQLiteRouteStateStore(filepath.Join(t.TempDir(), "route-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	exerciseRouteRevisionDoesNotProjectArtifactToDifferentProducer(t, store)
}

func TestRouteStateRejectsRequiredArtifactConsumerWithoutExactDependency(t *testing.T) {
	store := NewMemoryRouteStateStore()
	plan, scope := artifactRouteTestPlan(t, "turn-invalid-artifact-edge")
	for i := range plan.Selections {
		if plan.Selections[i].ID == "selection:deliver" {
			plan.Selections[i].ArtifactDependencies = nil
		}
	}
	if _, err := store.PublishRevision(routeRevisionPublish(plan, scope, nil), time.Now().UTC()); err == nil || err.Error() != "artifact_dependency_invalid" {
		t.Fatalf("missing exact artifact edge err=%v", err)
	}
}

func exerciseRouteArtifactPublicationReconciliation(t *testing.T, routes RouteStateStore, artifacts ArtifactStore) {
	t.Helper()
	plan, scope := artifactRouteTestPlan(t, "turn-parent")
	if _, err := routes.PublishRevision(routeRevisionPublish(plan, scope, nil), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// Simulate process loss after payload Publish and durable selection success,
	// but before RouteState metadata registration.
	capture := "selection:capture"
	if _, err := routes.RecordSelectionCompletion(scope, plan.ID, capture, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	payload, err := NewArtifactPayload(scope, capture, "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifacts.Publish(payload); err != nil {
		t.Fatal(err)
	}
	state, err := routes.ReconcileCurrentArtifacts(scope, artifacts, time.Now().UTC())
	if err != nil || len(state.Artifacts) != 1 || state.Artifacts[0].ArtifactID != payload.Ref.ID {
		t.Fatalf("reconciled state=%#v err=%v", state, err)
	}
	if state, err = routes.ReconcileCurrentArtifacts(scope, artifacts, time.Now().UTC()); err != nil || len(state.Artifacts) != 1 {
		t.Fatalf("idempotent reconciliation state=%#v err=%v", state, err)
	}
	// A published artifact from a selection that never durably completed must
	// never be promoted by recovery.
	other, err := NewArtifactPayload(scope, "selection:deliver", "image", "image/png", "b3RoZXI=", time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifacts.Publish(other); err != nil {
		t.Fatal(err)
	}
	state, err = routes.ReconcileCurrentArtifacts(scope, artifacts, time.Now().UTC())
	if err != nil || len(state.Artifacts) != 1 {
		t.Fatalf("uncompleted producer leaked state=%#v err=%v", state, err)
	}
}

func TestMemoryRouteArtifactPublicationReconciliation(t *testing.T) {
	exerciseRouteArtifactPublicationReconciliation(t, NewMemoryRouteStateStore(), NewMemoryArtifactStore())
}

func TestSQLiteRouteArtifactPublicationReconciliationSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	routes, err := NewSQLiteRouteStateStore(filepath.Join(dir, "route-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := NewSQLiteArtifactStore(filepath.Join(dir, "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	plan, scope := artifactRouteTestPlan(t, "turn-parent")
	if _, err := routes.PublishRevision(routeRevisionPublish(plan, scope, nil), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := routes.RecordSelectionCompletion(scope, plan.ID, "selection:capture", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	payload, err := NewArtifactPayload(scope, "selection:capture", "image", "image/png", semanticArtifactTestPNG, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := artifacts.Publish(payload); err != nil {
		t.Fatal(err)
	}
	if err := routes.Close(); err != nil {
		t.Fatal(err)
	}
	if err := artifacts.Close(); err != nil {
		t.Fatal(err)
	}
	routes, err = NewSQLiteRouteStateStore(filepath.Join(dir, "route-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer routes.Close()
	artifacts, err = NewSQLiteArtifactStore(filepath.Join(dir, "artifacts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer artifacts.Close()
	state, err := routes.ReconcileCurrentArtifacts(scope, artifacts, time.Now().UTC())
	if err != nil || len(state.Artifacts) != 1 || state.Artifacts[0].ArtifactID != payload.Ref.ID {
		t.Fatalf("recovered reconciliation state=%#v err=%v", state, err)
	}
}

func TestRouteArtifactHasCurrentConsumerAcceptsKindOnlyFileDeliver(t *testing.T) {
	plan := ToolPlan{Selections: []PlannedSelection{{
		ID: "selection:deliver",
		ArtifactDependencies: []ArtifactDependency{{
			ProducerSelection: "selection:generate",
			Contract:          ArtifactContract{Kind: "document", Required: true},
		}},
	}}}
	ref := RouteArtifactRef{
		ArtifactID: "pdf-1", Kind: "document", MIMEType: "application/pdf",
		IntegrityDigest: "digest", ProducerSelection: "selection:generate",
		ProducerPurposeDigest: "purpose", CreatedAt: time.Now().UTC(),
		SourceScope: InvocationScope{RootTaskID: "root", PlanID: "plan", SessionID: "session", TurnID: "turn", PrincipalID: "principal"},
	}
	if !routeArtifactHasCurrentConsumer(ref, plan) {
		t.Fatal("kind-only document consumer must accept application/pdf")
	}
	ref.Kind = "image"
	if routeArtifactHasCurrentConsumer(ref, plan) {
		t.Fatal("document consumer must not accept an image artifact")
	}
}
