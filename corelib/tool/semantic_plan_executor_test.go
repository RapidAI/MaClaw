package tool

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanExecutorPersistsDAGCompletionAndNeverReplays(t *testing.T) {
	registry := semanticRegistry(t)
	capture := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	capture.Produces = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	deliver := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	deliver.Consumes = []ArtifactContract{{Kind: "image", MIMEType: "image/png", Required: true}}
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{capture, deliver})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "task", TurnID: "turn", Snapshot: snapshot, Needs: []CapabilityNeed{
		{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true},
		{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true},
	}})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	grantStore, err := NewSQLiteInvocationGrantStore(filepath.Join(t.TempDir(), "grants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer grantStore.Close()
	execStore, err := NewSQLitePlanExecutionStore(filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer execStore.Close()
	issuer, err := NewInvocationIssuerWithStore([]byte(strings.Repeat("p", 32)), grantStore)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewPlanExecutor(issuer, execStore)
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: "task", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "user"}
	initial, err := issuer.IssueReady(plan, scope, time.Minute, nil)
	if err != nil || len(initial) != 1 {
		t.Fatalf("initial grants=%#v err=%v", initial, err)
	}
	calls := 0
	if result, selected, err := executor.Execute(initial[0], scope, plan, nil, func(selection PlannedSelection) SelectionExecutionResult {
		calls++
		return SelectionExecutionResult{Result: "captured", Succeeded: true}
	}); err != nil || selected.AdapterName != "capture_adapter" || !result.Succeeded {
		t.Fatalf("capture result=%#v selection=%#v err=%v", result, selected, err)
	}
	if calls != 1 {
		t.Fatalf("capture calls=%d", calls)
	}
	completed, err := execStore.Succeeded(scope)
	if err != nil || !completed["selection:capture"] {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	later, err := issuer.IssueReady(plan, scope, time.Minute, completed)
	if err != nil || len(later) != 1 || later[0].AdapterName != "delivery_adapter" {
		t.Fatalf("delivery grants=%#v err=%v", later, err)
	}
	if _, _, err := executor.Execute(initial[0], scope, plan, nil, func(PlannedSelection) SelectionExecutionResult {
		calls++
		return SelectionExecutionResult{Succeeded: true}
	}); err == nil || err.Error() != "invocation_grant_replayed" {
		t.Fatalf("capture replay err=%v", err)
	}
	if calls != 1 {
		t.Fatalf("capture was replayed calls=%d", calls)
	}
	if result, selected, err := executor.Execute(later[0], scope, plan, nil, func(selection PlannedSelection) SelectionExecutionResult {
		calls++
		return SelectionExecutionResult{Result: "delivered", Succeeded: true}
	}); err != nil || selected.AdapterName != "delivery_adapter" || !result.Succeeded {
		t.Fatalf("delivery result=%#v selection=%#v err=%v", result, selected, err)
	}
}

func TestSQLitePlanExecutionStoreReconcilesStaleRunningAsUnknown(t *testing.T) {
	store, err := NewSQLitePlanExecutionStore(filepath.Join(t.TempDir(), "execution.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := InvocationScope{RootTaskID: "task", PlanID: "plan", SessionID: "session", TurnID: "turn", PrincipalID: "user"}
	started := time.Now().UTC().Add(-2 * time.Hour)
	if _, acquired, err := store.Acquire(PlanExecutionRecord{Scope: scope, SelectionID: "selection:x", StartedAt: started}); err != nil || !acquired {
		t.Fatalf("Acquire acquired=%v err=%v", acquired, err)
	}
	changed, err := store.ReconcileStaleRunning(time.Now().UTC(), time.Hour)
	if err != nil || changed != 1 {
		t.Fatalf("Reconcile changed=%d err=%v", changed, err)
	}
	if record, acquired, err := store.Acquire(PlanExecutionRecord{Scope: scope, SelectionID: "selection:x"}); err != nil || acquired || record.State != PlanExecutionUnknown {
		t.Fatalf("unknown record=%#v acquired=%v err=%v", record, acquired, err)
	}
}

func TestPlanExecutorRecordsUnknownProviderEffectWithoutReplay(t *testing.T) {
	registry := semanticRegistry(t)
	provider := semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{provider})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "task", TurnID: "turn", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("p", 32)))
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: "task", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "user"}
	grants, err := issuer.Issue(plan, scope, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	executor, err := NewPlanExecutor(issuer, NewMemoryPlanExecutionStore())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, _, err := executor.Execute(grants[0], scope, plan, nil, func(PlannedSelection) SelectionExecutionResult {
		calls++
		return SelectionExecutionResult{Unknown: true, ReasonCode: "provider_transport_unknown"}
	})
	if err != nil || result.Succeeded || !result.Unknown || result.ReasonCode != "provider_transport_unknown" {
		t.Fatalf("unknown result=%#v err=%v", result, err)
	}
	completed, err := executor.Completed(scope)
	if err != nil || completed[plan.Selections[0].ID] {
		t.Fatalf("unknown execution became a completed dependency: completed=%#v err=%v", completed, err)
	}
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestPlanExecutorRecordsPreparedExternalEffectAsAwaitingReceipt(t *testing.T) {
	registry := semanticRegistry(t)
	provider := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{provider})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}},
		Facts: []RoutingFact{{ID: "artifact", Kind: "artifact_available", Authority: AuthorityRuntime, Attributes: map[string]string{"artifact_id": "artifact:one", "kind": "image", "mime_type": "image/png"}}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("p", 32)))
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: "task", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "user"}
	grants, err := issuer.IssueReady(plan, scope, time.Minute, nil)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	executor, err := NewPlanExecutor(issuer, NewMemoryPlanExecutionStore())
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := executor.Execute(grants[0], scope, plan, nil, func(PlannedSelection) SelectionExecutionResult {
		return SelectionExecutionResult{Result: "prepared", AwaitingReceipt: true}
	})
	if err != nil || result.Succeeded || !result.AwaitingReceipt || result.ReasonCode != "selection_awaiting_receipt" {
		t.Fatalf("awaiting result=%#v err=%v", result, err)
	}
	completed, err := executor.Completed(scope)
	if err != nil || completed[plan.Selections[0].ID] {
		t.Fatalf("prepared external effect became completed=%#v err=%v", completed, err)
	}
	if record, err := executor.Execution(scope, plan.Selections[0].ID); err != nil || record.State != PlanExecutionAwaitingReceipt {
		t.Fatalf("prepared external effect state=%#v err=%v", record, err)
	}
	if _, _, err := executor.Execute(grants[0], scope, plan, nil, func(PlannedSelection) SelectionExecutionResult { return SelectionExecutionResult{Succeeded: true} }); err == nil || err.Error() != "invocation_grant_replayed" {
		t.Fatalf("prepared effect replay err=%v", err)
	}
}

func TestPlanExecutorSettlesAwaitingReceiptAndProjectsAcceptedCompletion(t *testing.T) {
	registry := semanticRegistry(t)
	provider := semanticProvider("delivery_adapter", "artifact.deliver.current_channel", map[string]string{"format": "image"}, EffectExternalEffect)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{provider})
	plan, err := NewToolPlanner(registry).Plan(RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []CapabilityNeed{{ID: "deliver", Capability: "artifact.deliver.current_channel", Qualifiers: map[string]string{"format": "image"}, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 {
		t.Fatalf("plan=%#v err=%v", plan, err)
	}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("p", 32)))
	if err != nil {
		t.Fatal(err)
	}
	scope := InvocationScope{RootTaskID: "task", PlanID: plan.ID, SessionID: "session", TurnID: "turn", PrincipalID: "user"}
	routes := NewMemoryRouteStateStore()
	if _, err := routes.PublishRevision(RouteRevisionPublishRequest{Scope: scope, Plan: plan, SnapshotDigest: plan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	executor, err := NewPlanExecutorWithRouteState(issuer, NewMemoryPlanExecutionStore(), routes)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.IssueReady(plan, scope, time.Minute, nil)
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants=%#v err=%v", grants, err)
	}
	if _, _, err := executor.Execute(grants[0], scope, plan, nil, func(PlannedSelection) SelectionExecutionResult {
		return SelectionExecutionResult{Result: "prepared", AwaitingReceipt: true}
	}); err != nil {
		t.Fatal(err)
	}
	if record, err := executor.SettleAwaitingReceipt(scope, plan.Selections[0].ID, PlanExecutionSucceeded, "receipt-digest", "channel_delivery_accepted"); err != nil || record.State != PlanExecutionSucceeded {
		t.Fatalf("accepted settlement record=%#v err=%v", record, err)
	}
	if completed, err := executor.Completed(scope); err != nil || !completed[plan.Selections[0].ID] {
		t.Fatalf("accepted settlement completion=%#v err=%v", completed, err)
	}
	if projected, err := routes.CompletedSelections(scope); err != nil || !projected[plan.Selections[0].ID] {
		t.Fatalf("accepted settlement route projection=%#v err=%v", projected, err)
	}
	if _, err := executor.SettleAwaitingReceipt(scope, plan.Selections[0].ID, PlanExecutionFailed, "other", "channel_delivery_failed"); err == nil || !strings.Contains(err.Error(), "settlement conflict") {
		t.Fatalf("conflicting settlement err=%v", err)
	}
}

func TestPlanExecutorProjectsSucceededDependencyToCompatibleRevision(t *testing.T) {
	registry := semanticRegistry(t)
	snapshot := semanticSnapshot(t, registry, []ProviderSpec{
		semanticProvider("capture_adapter", "visual.capture.desktop", map[string]string{"display": "primary"}, EffectReadOnly),
	})
	parentPlan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn-parent", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	parentScope := InvocationScope{RootTaskID: "root", PlanID: parentPlan.ID, SessionID: "session", TurnID: "turn-parent", PrincipalID: "principal"}
	routes := NewMemoryRouteStateStore()
	parent, err := routes.PublishRevision(RouteRevisionPublishRequest{Scope: parentScope, Plan: parentPlan, SnapshotDigest: parentPlan.SnapshotDigest}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewInvocationIssuer([]byte(strings.Repeat("p", 32)))
	if err != nil {
		t.Fatal(err)
	}
	grants, err := issuer.Issue(parentPlan, parentScope, time.Minute)
	if err != nil || len(grants) != 1 {
		t.Fatalf("issue grants=%#v err=%v", grants, err)
	}
	executor, err := NewPlanExecutorWithRouteState(issuer, NewMemoryPlanExecutionStore(), routes)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := executor.Execute(grants[0], parentScope, parentPlan, nil, func(PlannedSelection) SelectionExecutionResult { return SelectionExecutionResult{Succeeded: true} }); err != nil {
		t.Fatal(err)
	}
	childPlan, err := NewToolPlanner(registry).Plan(RouteRequest{RootTaskID: "root", SessionID: "session", TurnID: "turn-child", Snapshot: snapshot, Needs: []CapabilityNeed{{ID: "capture", Capability: "visual.capture.desktop", Qualifiers: map[string]string{"display": "primary"}, Required: true}}})
	if err != nil {
		t.Fatal(err)
	}
	childScope := parentScope
	childScope.PlanID, childScope.TurnID = childPlan.ID, "turn-child"
	if _, err := routes.PublishRevision(RouteRevisionPublishRequest{Scope: childScope, Plan: childPlan, ExpectedParent: parent.Revision, SnapshotDigest: childPlan.SnapshotDigest}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	projected, err := routes.CompletedSelections(childScope)
	if err != nil || !projected[parentPlan.Selections[0].ID] {
		t.Fatalf("projected completion=%#v err=%v", projected, err)
	}
}
