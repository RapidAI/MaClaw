package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestSrvManageScheduleCreateListDelete(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scheduler.NewManager(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := newSrvManageScheduleHandler(nil, mgr)

	// list empty
	if out := h(map[string]interface{}{"action": "list"}); !strings.Contains(out, "没有") {
		t.Fatalf("list empty: %s", out)
	}

	// create with user self delivery (no resolve needed)
	out := h(map[string]interface{}{
		"action":      "create",
		"name":        "daily-news",
		"task_action": "search news",
		"hour":        9,
		"minute":      0,
		"channel":     "telegram",
		"user_id":     "self",
	})
	if !strings.Contains(out, "已创建") {
		t.Fatalf("create: %s", out)
	}
	if !strings.Contains(out, "telegram") && !strings.Contains(out, "推送") {
		// SummarizeDelivery should mention channel
		t.Logf("create out (ok if summary empty for self): %s", out)
	}

	list := h(map[string]interface{}{"action": "list"})
	if !strings.Contains(list, "daily-news") {
		t.Fatalf("list: %s", list)
	}

	tasks := mgr.List()
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	if tasks[0].Delivery == nil || !tasks[0].Delivery.Active() {
		t.Fatalf("delivery not set: %#v", tasks[0].Delivery)
	}
	if tasks[0].Delivery.Channel != scheduler.DeliveryChannelTelegram {
		t.Fatalf("channel=%q", tasks[0].Delivery.Channel)
	}

	del := h(map[string]interface{}{"action": "delete", "id": tasks[0].ID})
	if !strings.Contains(del, "已删除") {
		t.Fatalf("delete: %s", del)
	}
	if n := len(mgr.List()); n != 0 {
		t.Fatalf("after delete count=%d", n)
	}
}

func TestSrvManageScheduleUpdateParsesStringHour(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scheduler.NewManager(filepath.Join(dir, "update-hour.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := newSrvManageScheduleHandler(nil, mgr)
	created := h(map[string]interface{}{
		"action": "create", "name": "clock", "task_action": "ping", "hour": 7, "minute": 0,
	})
	if !strings.Contains(created, "已创建") {
		t.Fatalf("create: %s", created)
	}
	id := mgr.List()[0].ID
	updated := h(map[string]interface{}{"action": "update", "id": id, "hour": "11", "interval_minutes": "20"})
	if !strings.Contains(updated, "已更新") {
		t.Fatalf("update: %s", updated)
	}
	got := mgr.Get(id)
	if got == nil || got.Hour != 11 || got.IntervalMinutes != 20 {
		t.Fatalf("string update = %#v", got)
	}
}

func TestSrvManageScheduleCreateParsesStringNumbers(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scheduler.NewManager(filepath.Join(dir, "string-hour.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := newSrvManageScheduleHandler(nil, mgr)
	out := h(map[string]interface{}{
		"action":           "create",
		"name":             "string-args",
		"task_action":      "ping",
		"hour":             "8",
		"minute":           "15",
		"interval_minutes": "45",
		"fail_on_error":    "true",
		"user_id":          "self",
		"channel":          "telegram",
	})
	if !strings.Contains(out, "已创建") {
		t.Fatalf("string hour/interval create: %s", out)
	}
	tasks := mgr.List()
	if len(tasks) != 1 || tasks[0].Hour != 8 || tasks[0].Minute != 15 || tasks[0].IntervalMinutes != 45 {
		t.Fatalf("parsed clock/interval = %#v", tasks)
	}
	if tasks[0].Delivery == nil || !tasks[0].Delivery.FailOnError {
		t.Fatalf("fail_on_error string not applied: %#v", tasks[0].Delivery)
	}
}

func TestSrvManageScheduleCreateIntervalWithoutHour(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scheduler.NewManager(filepath.Join(dir, "interval.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := newSrvManageScheduleHandler(nil, mgr)
	out := h(map[string]interface{}{
		"action":           "create",
		"name":             "every-30",
		"task_action":      "poll inbox",
		"interval_minutes": 30,
	})
	if !strings.Contains(out, "已创建") {
		t.Fatalf("interval create without hour: %s", out)
	}
	tasks := mgr.List()
	if len(tasks) != 1 || tasks[0].IntervalMinutes != 30 {
		t.Fatalf("interval task = %#v", tasks)
	}
}

func TestSameUserInstancesKeepIndependentScheduledTasks(t *testing.T) {
	dir := t.TempDir()
	mgr, err := scheduler.NewManager(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	alice := srvScheduleCaller{InstanceID: "bot-1", TenantID: "tenant", UserID: "alice"}
	other := srvScheduleCaller{InstanceID: "bot-2", TenantID: "tenant", UserID: "alice"}
	first := newSrvManageScheduleHandlerForCaller(nil, mgr, alice)
	second := newSrvManageScheduleHandlerForCaller(nil, mgr, other)

	created := first(map[string]interface{}{
		"action": "create", "name": "standup", "task_action": "remind standup", "hour": 9,
		"instance_id": "bot-2", "owner_user_id": "mallory",
	})
	if !strings.Contains(created, "已创建") {
		t.Fatalf("create: %s", created)
	}
	tasks := mgr.List()
	if len(tasks) != 1 || tasks[0].InstanceID != "bot-1" || tasks[0].OwnerUserID != "alice" {
		t.Fatalf("task must stay on the calling instance: %#v", tasks)
	}
	if list := second(map[string]interface{}{"action": "list"}); strings.Contains(list, "standup") {
		t.Fatalf("other instance listed the task: %s", list)
	}
	if list := first(map[string]interface{}{"action": "list"}); !strings.Contains(list, "standup") {
		t.Fatalf("owner instance list: %s", list)
	}
	if updated := second(map[string]interface{}{"action": "update", "id": tasks[0].ID, "hour": 10}); !strings.Contains(updated, "not found") {
		t.Fatalf("other instance update: %s", updated)
	}
	if deleted := second(map[string]interface{}{"action": "delete", "name": "standup"}); !strings.Contains(deleted, "not found") {
		t.Fatalf("other instance delete: %s", deleted)
	}
	if got := mgr.Get(tasks[0].ID); got == nil || got.Hour != 9 {
		t.Fatalf("task changed by the other instance: %#v", got)
	}
	if deleted := first(map[string]interface{}{"action": "delete", "id": tasks[0].ID}); !strings.Contains(deleted, "已删除") {
		t.Fatalf("owner delete: %s", deleted)
	}
}

func TestScheduledTaskRunsOnItsBoundInstance(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "01234567890123456789012345678901",
	}, agentservice.NewMemoryStore(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Alice"})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	first, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Bot 1", AllowInvalidConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateInstance(context.Background(), principal, agentservice.CreateInstanceInput{Name: "Bot 2", AllowInvalidConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	gotPrincipal, instanceID, err := srvScheduledTaskRunTarget(context.Background(), svc, nil, agentservice.Principal{TenantID: "system", UserID: "scheduler"}, &scheduler.ScheduledTask{
		InstanceID: first.ID, OwnerTenantID: principal.TenantID, OwnerUserID: principal.UserID, Name: "standup",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if instanceID != first.ID || gotPrincipal.UserID != principal.UserID {
		t.Fatalf("target instance=%s user=%s, want %s %s", instanceID, gotPrincipal.UserID, first.ID, principal.UserID)
	}
	_, otherID, err := srvScheduledTaskRunTarget(context.Background(), svc, nil, agentservice.Principal{}, &scheduler.ScheduledTask{
		InstanceID: second.ID, OwnerTenantID: principal.TenantID, OwnerUserID: principal.UserID,
	}, nil, nil)
	if err != nil || otherID != second.ID || otherID == instanceID {
		t.Fatalf("second target=%s err=%v", otherID, err)
	}
	if _, _, err := srvScheduledTaskRunTarget(context.Background(), svc, nil, agentservice.Principal{}, &scheduler.ScheduledTask{
		InstanceID: "missing", OwnerTenantID: principal.TenantID, OwnerUserID: principal.UserID,
	}, nil, nil); err == nil {
		t.Fatal("missing instance must not fall back to a shared scheduler instance")
	}
}

func TestNormalizeSrvScheduleAction(t *testing.T) {
	if normalizeSrvScheduleAction("list_groups") != "list_targets" {
		t.Fatal("alias")
	}
	if normalizeSrvScheduleAction("CREATE") != "create" {
		t.Fatal("case")
	}
}

func TestParseSrvScheduleDeliveryShorthand(t *testing.T) {
	d, err := parseSrvScheduleDelivery(map[string]interface{}{
		"group_id":      "g1",
		"group_name":    "研发",
		"fail_on_error": true,
	})
	if err != nil || d == nil {
		t.Fatalf("%v %#v", err, d)
	}
	if !d.FailOnError || d.Targets[0].GroupID != "g1" {
		t.Fatalf("%#v", d)
	}
}
