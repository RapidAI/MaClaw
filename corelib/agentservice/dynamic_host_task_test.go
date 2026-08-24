package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/task"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostTaskTracker struct {
	title       string
	description string
	id          string
	status      string
	note        string
	principal   Principal
	result      string
	err         error
}

func (f *fakeHostTaskTracker) TrackReviewedHostTask(_ context.Context, principal Principal, title, description, id, status, note string) (string, error) {
	f.principal = principal
	f.title = title
	f.description = description
	f.id = id
	f.status = status
	f.note = note
	return f.result, f.err
}

func TestReviewedHostTaskExecutesWithoutCoordinatorAndRejectsGoalMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	tracker := &fakeHostTaskTracker{result: "任务已创建: task-1 [pending] fix login"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Task: tracker})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "todo", Capability: CapabilityTaskTrack, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("task plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityTaskTrack {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host task must use the local mutation receipt, not the external coordinator, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	created := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"title":"fix login"}`)
	if !created.Succeeded || created.Result != tracker.result || created.Unknown {
		t.Fatalf("task create result=%#v", created)
	}
	if tracker.title != "fix login" || tracker.id != "" || tracker.status != "" || tracker.principal.TenantID != principal.TenantID || tracker.principal.UserID != principal.UserID {
		t.Fatalf("tracker=%#v", tracker)
	}
	listed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !listed.Succeeded || tracker.title != "" || tracker.id != "" || tracker.status != "" {
		t.Fatalf("task list result=%#v tracker=%#v", listed, tracker)
	}
	updated := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"task-1","status":"completed"}`)
	if !updated.Succeeded || tracker.id != "task-1" || tracker.status != "completed" || tracker.title != "" {
		t.Fatalf("task update result=%#v tracker=%#v", updated, tracker)
	}
	deleted := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"task-1"}`)
	if !deleted.Succeeded || tracker.id != "task-1" || tracker.status != "" || tracker.title != "" {
		t.Fatalf("task delete result=%#v tracker=%#v", deleted, tracker)
	}
	both := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"title":"fix login","id":"task-1"}`)
	if both.Succeeded {
		t.Fatalf("title and id together must fail closed, result=%#v", both)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"title":"fix login","channel":"lansenger"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("channel args must fail closed, result=%#v", rejected)
	}
	actionSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"delegate","delegate_to":"coder"}`)
	if actionSoup.Succeeded || actionSoup.Unknown {
		t.Fatalf("action soup must fail closed, result=%#v", actionSoup)
	}

	goalPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-goal", TurnID: "turn-goal", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "goal", Capability: coretool.CapabilityGoalManageLongRunning, Required: true}},
	})
	if err != nil || len(goalPlan.Selections) != 0 {
		t.Fatalf("goal.manage.long_running must not be satisfied by host task, plan=%#v err=%v", goalPlan, err)
	}
}

func TestReviewedHostTaskIsAbsentWithoutTracker(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Memory: &fakeHostMemoryManager{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "todo", Capability: CapabilityTaskTrack, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("task without tracker must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostTaskRejectsActionAndDelegateFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostTaskProvider(&fakeHostTaskTracker{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityTaskTrack || provider.AdapterName == "task" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["title"]; !ok {
		t.Fatalf("task schema missing title: %#v", props)
	}
	if _, ok := props["id"]; !ok || len(props) != 5 {
		t.Fatalf("task schema=%#v", props)
	}
	for _, key := range []string{"action", "task_id", "delegate_to", "depends_on", "channel", "destination", "group_name", "path", "query", "content"} {
		if _, ok := props[key]; ok {
			t.Fatalf("task schema leaked %s", key)
		}
	}
}

func TestReviewedHostTaskUsesTrustedPrincipalAndStore(t *testing.T) {
	store := task.NewStore()
	principal := Principal{TenantID: "tenant", UserID: "user"}
	other := Principal{TenantID: "tenant", UserID: "other"}
	cb := &coreAgentCallbacks{principal: principal, tasks: store}
	out, err := cb.TrackReviewedHostTask(context.Background(), principal, "fix login", "", "", "", "")
	if err != nil || !strings.Contains(out, "fix login") || !strings.Contains(out, "task-1") {
		t.Fatalf("create=%q err=%v", out, err)
	}
	listed, err := cb.TrackReviewedHostTask(context.Background(), principal, "", "", "", "", "")
	if err != nil || !strings.Contains(listed, "fix login") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	updated, err := cb.TrackReviewedHostTask(context.Background(), principal, "", "", "task-1", "completed", "")
	if err != nil || !strings.Contains(updated, "completed") {
		t.Fatalf("update=%q err=%v", updated, err)
	}
	if _, err := cb.TrackReviewedHostTask(context.Background(), principal, "fix login", "", "task-1", "", ""); err == nil {
		t.Fatal("title and id together must fail closed")
	}
	if _, err := cb.TrackReviewedHostTask(context.Background(), principal, "", "", "task-1", "delegated", ""); err == nil {
		t.Fatal("unknown status must fail closed")
	}
	if _, err := cb.TrackReviewedHostTask(context.Background(), other, "other todo", "", "", "", ""); err == nil {
		t.Fatal("principal mismatch must fail closed")
	}
	deleted, err := cb.TrackReviewedHostTask(context.Background(), principal, "", "", "task-1", "", "")
	if err != nil || !strings.Contains(deleted, "task-1") {
		t.Fatalf("delete=%q err=%v", deleted, err)
	}
	empty, err := cb.TrackReviewedHostTask(context.Background(), principal, "", "", "", "", "")
	if err != nil || !strings.Contains(empty, "当前没有任务") {
		t.Fatalf("empty list=%q err=%v", empty, err)
	}
	services := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if services.Task != nil {
		t.Fatal("host without a task store must not attach task.track.local")
	}
}
