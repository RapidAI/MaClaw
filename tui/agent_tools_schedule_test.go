package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func newTUIScheduleTestApp(t *testing.T) *TUIApp {
	t.Helper()
	mgr, err := scheduler.NewManager(filepath.Join(t.TempDir(), "scheduled_tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &TUIApp{scheduledTaskManager: mgr}
}

func TestTUICreateScheduledTaskRejectsDeliveryBinding(t *testing.T) {
	app := newTUIScheduleTestApp(t)
	got := app.toolCreateScheduledTask(map[string]interface{}{
		"name": "daily", "task_action": "remind", "hour": float64(9),
		"delivery": map[string]interface{}{"enabled": true, "channel": "lansenger"},
	})
	if !strings.Contains(got, "不能绑定渠道投递") {
		t.Fatalf("create with delivery=%q", got)
	}
	got = app.toolCreateScheduledTask(map[string]interface{}{
		"name": "daily", "task_action": "remind", "hour": float64(9), "group_name": "ops",
	})
	if !strings.Contains(got, "不能绑定渠道投递") {
		t.Fatalf("create with group_name=%q", got)
	}
	if n := len(app.scheduledTaskManager.List()); n != 0 {
		t.Fatalf("rejected create must not persist tasks, got %d", n)
	}
}

func TestTUICreateScheduledTaskDoesNotWriteDelivery(t *testing.T) {
	app := newTUIScheduleTestApp(t)
	got := app.toolCreateScheduledTask(map[string]interface{}{
		"name": "daily", "task_action": "remind", "hour": float64(9),
	})
	if !strings.Contains(got, "定时任务已创建") {
		t.Fatalf("create=%q", got)
	}
	tasks := app.scheduledTaskManager.List()
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	if tasks[0].Delivery != nil {
		t.Fatalf("create must not write Delivery: %#v", tasks[0].Delivery)
	}
}

func TestTUIUpdateScheduledTaskRejectsDeliveryBinding(t *testing.T) {
	app := newTUIScheduleTestApp(t)
	id, err := app.scheduledTaskManager.Add(scheduler.ScheduledTask{
		Name:   "legacy",
		Action: "remind",
		Hour:   8,
		Delivery: &scheduler.TaskDelivery{
			Enabled: true,
			Channel: scheduler.DeliveryChannelLansenger,
			Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindGroup, GroupID: "g1"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := app.toolUpdateScheduledTask(map[string]interface{}{
		"id": id, "name": "renamed",
		"delivery": map[string]interface{}{"enabled": true, "channel": "lansenger", "targets": []interface{}{
			map[string]interface{}{"kind": "group", "group_id": "g2"},
		}},
	})
	if !strings.Contains(got, "不能绑定渠道投递") {
		t.Fatalf("update with delivery=%q", got)
	}
	got = app.toolUpdateScheduledTask(map[string]interface{}{
		"id": id, "group_id": "g2",
	})
	if !strings.Contains(got, "不能绑定渠道投递") {
		t.Fatalf("update with group_id=%q", got)
	}
	task := app.scheduledTaskManager.Get(id)
	if task == nil || task.Name != "legacy" {
		t.Fatalf("rejected update must leave name, got %#v", task)
	}
	if task.Delivery == nil || len(task.Delivery.Targets) != 1 || task.Delivery.Targets[0].GroupID != "g1" {
		t.Fatalf("legacy Delivery must stay, got %#v", task.Delivery)
	}
}

func TestTUIUpdateScheduledTaskPreservesLegacyDelivery(t *testing.T) {
	app := newTUIScheduleTestApp(t)
	id, err := app.scheduledTaskManager.Add(scheduler.ScheduledTask{
		Name:   "legacy",
		Action: "remind",
		Hour:   8,
		Delivery: &scheduler.TaskDelivery{
			Enabled: true,
			Channel: scheduler.DeliveryChannelLansenger,
			Targets: []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindGroup, GroupID: "g1"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := app.toolUpdateScheduledTask(map[string]interface{}{
		"id": id, "name": "renamed", "hour": float64(10),
	})
	if !strings.Contains(got, "定时任务已更新") {
		t.Fatalf("update=%q", got)
	}
	task := app.scheduledTaskManager.Get(id)
	if task == nil || task.Name != "renamed" || task.Hour != 10 {
		t.Fatalf("schedule fields not updated: %#v", task)
	}
	if task.Delivery == nil || len(task.Delivery.Targets) != 1 || task.Delivery.Targets[0].GroupID != "g1" {
		t.Fatalf("legacy Delivery must stay, got %#v", task.Delivery)
	}
	listed := app.toolListScheduledTasks()
	if !strings.Contains(listed, "推送:") {
		t.Fatalf("list should still show legacy delivery, got %q", listed)
	}
}

func TestTUIManageScheduleHandlerRejectsDeliveryOnCreate(t *testing.T) {
	app := newTUIScheduleTestApp(t)
	got := newManageScheduleHandler(app)(map[string]interface{}{
		"action": "create", "name": "daily", "task_action": "remind", "hour": float64(9),
		"channel": "lansenger", "group_id": "g1",
	})
	if !strings.Contains(got, "不能绑定渠道投递") {
		t.Fatalf("handler create=%q", got)
	}
}
