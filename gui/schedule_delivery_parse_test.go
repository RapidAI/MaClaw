package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestParseScheduleDeliveryArgsShorthand(t *testing.T) {
	d, err := parseScheduleDeliveryArgs(map[string]interface{}{
		"group_name": "产品讨论群",
		"channel":    "lansenger",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || !d.Enabled || d.Targets[0].GroupName != "产品讨论群" {
		t.Fatalf("%#v", d)
	}
	if d.Targets[0].Kind != scheduler.DeliveryKindGroup {
		t.Fatalf("kind=%q", d.Targets[0].Kind)
	}
}

func TestParseScheduleDeliveryArgsUser(t *testing.T) {
	d, err := parseScheduleDeliveryArgs(map[string]interface{}{
		"user_id": "staff-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || d.Targets[0].UserID != "staff-1" || d.Targets[0].Kind != scheduler.DeliveryKindUser {
		t.Fatalf("%#v", d)
	}
}

func TestParseScheduleDeliveryArgsNullClears(t *testing.T) {
	d, err := parseScheduleDeliveryArgs(map[string]interface{}{"delivery": nil})
	if err != nil || d != nil {
		t.Fatalf("got %#v err=%v", d, err)
	}
}

func TestScheduleArgsTouchDelivery(t *testing.T) {
	if !scheduleArgsTouchDelivery(map[string]interface{}{"group_id": "g1"}) {
		t.Fatal("group_id should touch delivery")
	}
	if scheduleArgsTouchDelivery(map[string]interface{}{"hour": 9.0}) {
		t.Fatal("hour alone should not touch delivery")
	}
}

func TestNormalizeManageScheduleListTargetsAliases(t *testing.T) {
	for _, a := range []string{"list_targets", "list_groups", "list_im_targets", "list_delivery_targets"} {
		if got := normalizeManageScheduleAction(a); got != manageScheduleActionListTargets {
			t.Fatalf("%s -> %q", a, got)
		}
	}
}

func TestManageScheduleDoesNotClobberActionWithTaskAction(t *testing.T) {
	// Regression: previously any task_action overwrote action, so create failed.
	args := map[string]interface{}{
		"action":      "create",
		"task_action": "搜索今日新闻",
		"name":        "早报",
	}
	actionText := stringVal(args, "action")
	if ta := stringVal(args, "task_action"); ta != "" {
		if normalized := normalizeManageScheduleAction(ta); normalized != manageScheduleActionUnknown {
			args["task_action"] = actionText
			actionText = ta
			args["action"] = ta
		}
	}
	if normalizeManageScheduleAction(actionText) != manageScheduleActionCreate {
		t.Fatalf("CRUD action lost: actionText=%q args=%v", actionText, args)
	}
	if stringVal(args, "task_action") != "搜索今日新闻" {
		t.Fatalf("task_action clobbered: %v", args)
	}
}

func TestUpdateMapsTaskActionAndDropsCRUDVerb(t *testing.T) {
	// Simulate args after manage_schedule dispatch for update.
	args := map[string]interface{}{
		"action":      "update",
		"task_action": "新的执行内容",
		"id":          "x",
	}
	if ta := stringVal(args, "task_action"); ta != "" {
		args["action"] = ta
	} else if normalizeManageScheduleAction(stringVal(args, "action")) != manageScheduleActionUnknown {
		delete(args, "action")
	}
	if stringVal(args, "action") != "新的执行内容" {
		t.Fatalf("action=%q", stringVal(args, "action"))
	}

	args2 := map[string]interface{}{"action": "update", "id": "x"}
	if ta := stringVal(args2, "task_action"); ta != "" {
		args2["action"] = ta
	} else if normalizeManageScheduleAction(stringVal(args2, "action")) != manageScheduleActionUnknown {
		delete(args2, "action")
	}
	if _, ok := args2["action"]; ok {
		t.Fatalf("CRUD verb should be stripped: %v", args2)
	}
}
