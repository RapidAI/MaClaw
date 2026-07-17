package main

import (
	"path/filepath"
	"strings"
	"testing"

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
		"group_id": "g1",
		"group_name": "研发",
		"fail_on_error": true,
	})
	if err != nil || d == nil {
		t.Fatalf("%v %#v", err, d)
	}
	if !d.FailOnError || d.Targets[0].GroupID != "g1" {
		t.Fatalf("%#v", d)
	}
}
