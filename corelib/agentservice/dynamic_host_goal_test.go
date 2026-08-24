package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/goal"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostGoalManager struct {
	objective string
	status    string
	note      string
	principal Principal
	result    string
	err       error
}

func (f *fakeHostGoalManager) ManageReviewedHostGoal(_ context.Context, principal Principal, objective, status, note string) (string, error) {
	f.principal = principal
	f.objective = objective
	f.status = status
	f.note = note
	return f.result, f.err
}

func TestReviewedHostGoalExecutesWithoutCoordinatorAndRejectsTaskMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	manager := &fakeHostGoalManager{result: "目标已创建: keep docs current"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Goal: manager})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "goal", Capability: CapabilityGoalManage, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("goal plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityGoalManage {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host goal must use the local mutation receipt, not the external coordinator, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	created := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"objective":"keep docs current"}`)
	if !created.Succeeded || created.Result != manager.result || created.Unknown {
		t.Fatalf("goal create result=%#v", created)
	}
	if manager.objective != "keep docs current" || manager.status != "" || manager.principal.TenantID != principal.TenantID || manager.principal.UserID != principal.UserID {
		t.Fatalf("manager=%#v", manager)
	}
	got := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !got.Succeeded || manager.objective != "" || manager.status != "" {
		t.Fatalf("goal get result=%#v manager=%#v", got, manager)
	}
	completed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"status":"completed","note":"done"}`)
	if !completed.Succeeded || manager.status != "completed" || manager.note != "done" || manager.objective != "" {
		t.Fatalf("goal complete result=%#v manager=%#v", completed, manager)
	}
	both := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"objective":"keep docs current","status":"completed"}`)
	if both.Succeeded {
		t.Fatalf("objective and status together must fail closed, result=%#v", both)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"objective":"keep docs current","token_budget":100}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("token_budget must fail closed, result=%#v", rejected)
	}
	actionSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"pause"}`)
	if actionSoup.Succeeded || actionSoup.Unknown {
		t.Fatalf("action soup must fail closed, result=%#v", actionSoup)
	}

	taskPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-todo", TurnID: "turn-todo", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "todo", Capability: CapabilityTaskTrack, Required: true}},
	})
	if err != nil || len(taskPlan.Selections) != 0 {
		t.Fatalf("task.track.local must not be satisfied by host goal, plan=%#v err=%v", taskPlan, err)
	}
}

func TestReviewedHostGoalIsAbsentWithoutManager(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Task: &fakeHostTaskTracker{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "goal", Capability: CapabilityGoalManage, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("goal without manager must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostGoalRejectsBudgetAndActionFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostGoalProvider(&fakeHostGoalManager{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityGoalManage || provider.AdapterName == "goal" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["objective"]; !ok {
		t.Fatalf("goal schema missing objective: %#v", props)
	}
	if _, ok := props["status"]; !ok || len(props) != 3 {
		t.Fatalf("goal schema=%#v", props)
	}
	for _, key := range []string{"action", "token_budget", "max_turns", "acceptance_criteria", "project_path", "pause", "resume", "channel", "destination", "id", "goal_id"} {
		if _, ok := props[key]; ok {
			t.Fatalf("goal schema leaked %s", key)
		}
	}
}

func TestReviewedHostGoalUsesTrustedPrincipalAndStore(t *testing.T) {
	store := goal.NewStore("")
	principal := Principal{TenantID: "tenant", UserID: "user"}
	other := Principal{TenantID: "tenant", UserID: "other"}
	cb := &coreAgentCallbacks{principal: principal, goals: store}
	out, err := cb.ManageReviewedHostGoal(context.Background(), principal, "keep this documentation up to date", "", "")
	if err != nil || !strings.Contains(out, "keep this documentation up to date") {
		t.Fatalf("create=%q err=%v", out, err)
	}
	if _, err := cb.ManageReviewedHostGoal(context.Background(), principal, "another goal", "", ""); err == nil {
		t.Fatal("second active goal must fail closed")
	}
	got, err := cb.ManageReviewedHostGoal(context.Background(), principal, "", "", "")
	if err != nil || !strings.Contains(got, "keep this documentation up to date") {
		t.Fatalf("get=%q err=%v", got, err)
	}
	if _, err := cb.ManageReviewedHostGoal(context.Background(), principal, "", "paused", ""); err == nil {
		t.Fatal("pause status must fail closed")
	}
	if _, err := cb.ManageReviewedHostGoal(context.Background(), other, "other goal", "", ""); err == nil {
		t.Fatal("principal mismatch must fail closed")
	}
	otherCB := &coreAgentCallbacks{principal: other, goals: store}
	hidden, err := otherCB.ManageReviewedHostGoal(context.Background(), other, "", "", "")
	if err != nil || strings.Contains(hidden, "keep this documentation") {
		t.Fatalf("other principal must not see foreign goal: %q err=%v", hidden, err)
	}
	updated, err := cb.ManageReviewedHostGoal(context.Background(), principal, "", "completed", "shipped")
	if err != nil || !strings.Contains(updated, "complete") {
		t.Fatalf("complete=%q err=%v", updated, err)
	}
	if _, err := cb.ManageReviewedHostGoal(context.Background(), principal, "", "failed", ""); err == nil {
		t.Fatal("terminal goal must not accept another status change")
	}
	services := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if services.Goal != nil {
		t.Fatal("host without a goal store must not attach goal.manage.longrunning")
	}
}
