package agentservice

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type fakeHostScheduleAdministrator struct {
	args      reviewedHostScheduleArgs
	principal Principal
	result    string
	err       error
}

func (f *fakeHostScheduleAdministrator) AdministerReviewedHostSchedule(_ context.Context, principal Principal, args reviewedHostScheduleArgs) (string, error) {
	f.principal = principal
	f.args = args
	return f.result, f.err
}

func TestReviewedHostScheduleExecutesWithoutCoordinatorAndRejectsDispatchMapping(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	admin := &fakeHostScheduleAdministrator{result: "定时任务已创建: standup"}
	observed := dynamicCatalogLifecycleForKind("mcp", IncompleteDynamicCatalogLifecycle(coretool.CatalogCoverageReasonNotReady))
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, observed, reviewedHostOwnedServices{Schedule: admin})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "schedule", Capability: CapabilityScheduleAdminister, Required: true}},
	})
	if err != nil || len(plan.Selections) != 1 || len(plan.Unmet) != 0 {
		t.Fatalf("schedule plan=%#v err=%v", plan, err)
	}
	if plan.Selections[0].Provider.Kind != reviewedHostProviderKind || plan.Selections[0].FitProof.MatchedCapability != CapabilityScheduleAdminister {
		t.Fatalf("selection=%#v", plan.Selections[0])
	}
	if !dynamicHostLocalMutationSelection(plan.Selections[0]) || dynamicSelectionRequiresReceipt(plan.Selections[0]) {
		t.Fatalf("host schedule must use the local mutation receipt, not the external coordinator, selection=%#v", plan.Selections[0])
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	created := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"name":"standup","task_action":"remind standup","hour":9}`)
	if !created.Succeeded || created.Result != admin.result || created.Unknown {
		t.Fatalf("schedule create result=%#v", created)
	}
	if admin.args.Name != "standup" || admin.args.TaskAction != "remind standup" || !admin.args.HasHour || admin.args.Hour != 9 || admin.principal.UserID != principal.UserID {
		t.Fatalf("admin=%#v", admin)
	}
	listed := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{}`)
	if !listed.Succeeded || admin.args.Name != "" || admin.args.HasHour {
		t.Fatalf("schedule list result=%#v admin=%#v", listed, admin)
	}
	deleted := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"task-1"}`)
	if !deleted.Succeeded || admin.args.ID != "task-1" || admin.args.hasUpdateExtras() {
		t.Fatalf("schedule delete result=%#v admin=%#v", deleted, admin)
	}
	paused := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"id":"task-1","status":"paused"}`)
	if !paused.Succeeded || admin.args.Status != "paused" {
		t.Fatalf("schedule pause result=%#v admin=%#v", paused, admin)
	}
	nameOnly := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"name":"standup"}`)
	if nameOnly.Succeeded {
		t.Fatalf("name without task_action and hour must fail closed, result=%#v", nameOnly)
	}
	rejected := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"name":"standup","task_action":"remind standup","hour":9,"group_name":"ops"}`)
	if rejected.Succeeded || rejected.Unknown {
		t.Fatalf("group_name must fail closed, result=%#v", rejected)
	}
	actionSoup := catalog.ExecuteSelection(context.Background(), principal, nil, nil, plan.Selections[0], `{"action":"create"}`)
	if actionSoup.Succeeded || actionSoup.Unknown {
		t.Fatalf("action soup must fail closed, result=%#v", actionSoup)
	}

	dispatchPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-dispatch", TurnID: "turn-dispatch", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "dispatch", Capability: coretool.CapabilityScheduleDispatchChannel, Required: true}},
	})
	if err != nil || len(dispatchPlan.Selections) != 0 {
		t.Fatalf("schedule.dispatch.channel must not be satisfied by host administer, plan=%#v err=%v", dispatchPlan, err)
	}
	taskPlan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task-todo", TurnID: "turn-todo", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "todo", Capability: CapabilityTaskTrack, Required: true}},
	})
	if err != nil || len(taskPlan.Selections) != 0 {
		t.Fatalf("task.track.local must not be satisfied by host schedule, plan=%#v err=%v", taskPlan, err)
	}
}

func TestReviewedHostScheduleIsAbsentWithoutManager(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatal(err)
	}
	catalog, lifecycle, err := prepareReviewedDynamicSemanticCatalog(registry, nil, nil, DynamicCatalogLifecycle{}, reviewedHostOwnedServices{Template: &fakeHostTemplateManager{}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := coretool.NewToolCatalog(registry).PublishWithCoverage(catalog.Providers, lifecycle.Coverage, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := coretool.NewToolPlanner(registry).Plan(coretool.RouteRequest{
		RootTaskID: "task", TurnID: "turn", Snapshot: snapshot,
		Needs: []coretool.CapabilityNeed{{ID: "schedule", Capability: CapabilityScheduleAdminister, Required: true}},
	})
	if err != nil || len(plan.Selections) != 0 {
		t.Fatalf("schedule without manager must stay unmet, plan=%#v err=%v", plan, err)
	}
}

func TestProjectReviewedHostScheduleRejectsDeliveryAndActionFields(t *testing.T) {
	provider, definition, _, err := ProjectReviewedHostScheduleProvider(&fakeHostScheduleAdministrator{})
	if err != nil {
		t.Fatal(err)
	}
	if provider.Provides[0].Capability != CapabilityScheduleAdminister || provider.AdapterName == "manage_schedule" || provider.AdapterName == "schedule_administer" {
		t.Fatalf("provider=%#v", provider)
	}
	fn, _ := definition["function"].(map[string]interface{})
	params, _ := fn["parameters"].(map[string]interface{})
	props, _ := params["properties"].(map[string]interface{})
	if _, ok := props["name"]; !ok || len(props) != 11 {
		t.Fatalf("schedule schema=%#v", props)
	}
	for _, key := range []string{"action", "channel", "destination", "group_name", "group_id", "user_id", "delivery", "list_targets", "path", "run", "fire"} {
		if _, ok := props[key]; ok {
			t.Fatalf("schedule schema leaked %s", key)
		}
	}
}

func TestReviewedHostScheduleUsesTrustedPrincipalAndStore(t *testing.T) {
	store, err := scheduler.NewManager(filepath.Join(t.TempDir(), "schedules.json"))
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "user"}
	other := Principal{TenantID: "tenant", UserID: "other"}
	cb := &coreAgentCallbacks{principal: principal, schedules: store}
	out, err := cb.AdministerReviewedHostSchedule(context.Background(), principal, reviewedHostScheduleArgs{
		Name: "standup", TaskAction: "remind standup", Hour: 9, HasHour: true,
	})
	if err != nil || !strings.Contains(out, "standup") {
		t.Fatalf("create=%q err=%v", out, err)
	}
	if created := store.Get(store.List()[0].ID); created == nil || created.Delivery != nil {
		t.Fatal("managed create must not write Delivery")
	}
	listed, err := cb.AdministerReviewedHostSchedule(context.Background(), principal, reviewedHostScheduleArgs{})
	if err != nil || !strings.Contains(listed, "standup") {
		t.Fatalf("list=%q err=%v", listed, err)
	}
	id := store.List()[0].ID
	if _, err := cb.AdministerReviewedHostSchedule(context.Background(), principal, reviewedHostScheduleArgs{Name: "standup"}); err == nil {
		t.Fatal("name without task_action and hour must fail closed")
	}
	if _, err := cb.AdministerReviewedHostSchedule(context.Background(), other, reviewedHostScheduleArgs{
		Name: "other", TaskAction: "other", Hour: 8, HasHour: true,
	}); err == nil {
		t.Fatal("principal mismatch must fail closed")
	}
	paused, err := cb.AdministerReviewedHostSchedule(context.Background(), principal, reviewedHostScheduleArgs{ID: id, Status: "paused"})
	if err != nil || !strings.Contains(paused, "paused") {
		t.Fatalf("pause=%q err=%v", paused, err)
	}
	if _, err := cb.AdministerReviewedHostSchedule(context.Background(), principal, reviewedHostScheduleArgs{ID: id}); err != nil {
		t.Fatalf("delete=%v", err)
	}
	if len(store.List()) != 0 {
		t.Fatal("delete must remove the local schedule record")
	}
	services := (&coreAgentCallbacks{principal: principal}).reviewedHostOwnedServices()
	if services.Schedule != nil {
		t.Fatal("host without a schedule store must not attach schedule.administer.local")
	}
}

func TestReviewedHostScheduleIsolatesSameUserInstances(t *testing.T) {
	store, err := scheduler.NewManager(filepath.Join(t.TempDir(), "schedules.json"))
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{TenantID: "tenant", UserID: "alice"}
	first := &coreAgentCallbacks{principal: principal, schedules: store, instance: Instance{ID: "bot-1"}}
	second := &coreAgentCallbacks{principal: principal, schedules: store, instance: Instance{ID: "bot-2"}}
	created, err := first.AdministerReviewedHostSchedule(context.Background(), principal, reviewedHostScheduleArgs{
		Name: "standup", TaskAction: "remind standup", Hour: 9, HasHour: true,
	})
	if err != nil || !strings.Contains(created, "standup") {
		t.Fatalf("create=%q err=%v", created, err)
	}
	tasks := store.List()
	if len(tasks) != 1 || tasks[0].InstanceID != "bot-1" || tasks[0].OwnerUserID != "alice" {
		t.Fatalf("stored task = %#v", tasks)
	}
	listed, err := second.AdministerReviewedHostSchedule(context.Background(), principal, reviewedHostScheduleArgs{})
	if err != nil || strings.Contains(listed, "standup") {
		t.Fatalf("other instance list=%q err=%v", listed, err)
	}
	if _, err := second.AdministerReviewedHostSchedule(context.Background(), principal, reviewedHostScheduleArgs{ID: tasks[0].ID}); err == nil {
		t.Fatal("other instance deleted the task")
	}
	if len(store.List()) != 1 {
		t.Fatal("cross-instance delete removed the task")
	}
	ownerList, err := first.AdministerReviewedHostSchedule(context.Background(), principal, reviewedHostScheduleArgs{})
	if err != nil || !strings.Contains(ownerList, "standup") {
		t.Fatalf("owner list=%q err=%v", ownerList, err)
	}
}

func TestScheduleManagerForDataDirRequiresPersistPathAndDoesNotStart(t *testing.T) {
	e := &CoreAgentExecutor{}
	if e.scheduleManagerForDataDir("") != nil {
		t.Fatal("empty dataDir must not attach schedule.administer.local")
	}
	dir := t.TempDir()
	mgr := e.scheduleManagerForDataDir(dir)
	if mgr == nil {
		t.Fatal("non-empty dataDir must open a schedule store")
	}
	id, err := mgr.Add(scheduler.ScheduledTask{Name: "n", Action: "a", Hour: 1})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if got := mgr.Get(id); got == nil || got.RunCount != 0 {
		t.Fatalf("administer store must not fire tasks, got=%#v", got)
	}
}

// TestReviewedHostScheduleStatusVocabulary pins the alias shape at this host:
// the schema advertises only canonical names, while the runtime also accepts
// their aliases and the empty-is-unset reading. Both come from the shared
// contract, so the host cannot drift from the GUI surface serving the same
// capability.
func TestReviewedHostScheduleStatusVocabulary(t *testing.T) {
	schema := reviewedHostScheduleInvocationSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schedule schema has no properties: %#v", schema)
	}
	status, ok := properties["status"].(map[string]interface{})
	if !ok {
		t.Fatalf("schedule schema has no status property: %#v", properties)
	}
	enum, ok := status["enum"].([]string)
	if !ok || len(enum) == 0 {
		t.Fatalf("status must publish a non-empty enum, got %#v", status["enum"])
	}
	for _, name := range enum {
		canonical, ok := reviewedHostScheduleStatus(name)
		if !ok || canonical != name {
			t.Fatalf("schema advertises status %q, but the runtime maps it to (%q, %v)", name, canonical, ok)
		}
	}
	for sent, want := range map[string]string{"pause": "paused", "resume": "active"} {
		canonical, ok := reviewedHostScheduleStatus(sent)
		if !ok || canonical != want {
			t.Fatalf("alias %q maps to (%q, %v), want %q", sent, canonical, ok, want)
		}
	}
	if _, ok := reviewedHostScheduleStatus(""); !ok {
		t.Fatal("empty status must stay accepted-as-unset")
	}
	for _, invented := range []string{"disabled", "enabled", "off"} {
		_, err := administerScheduleStatusProbe(t, invented)
		if err == nil || !strings.Contains(err.Error(), "host_schedule_status_rejected") {
			t.Fatalf("status %q = %v, want host_schedule_status_rejected", invented, err)
		}
		// The refusal must name the canonical vocabulary so the first failure
		// can redirect the turn instead of ending it.
		for _, valid := range coretool.ScheduleStatusOptions() {
			if !strings.Contains(err.Error(), valid) {
				t.Fatalf("refusal for %q = %v does not mention %q", invented, err, valid)
			}
		}
	}
}

// administerScheduleStatusProbe drives one invented status through the real
// administer path — an update-shaped record (an id plus a status) passes the
// presence, name, and range gates and arrives at the status check.
func administerScheduleStatusProbe(t *testing.T, status string) (string, error) {
	t.Helper()
	store, err := scheduler.NewManager(filepath.Join(t.TempDir(), "schedules.json"))
	if err != nil {
		return "", err
	}
	cb := &coreAgentCallbacks{principal: Principal{TenantID: "tenant", UserID: "user"}, schedules: store}
	return cb.AdministerReviewedHostSchedule(context.Background(), cb.principal, reviewedHostScheduleArgs{ID: "task-1", Status: status})
}
