package botmgmt

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestActiveDesktopRunsListsRunningAndQueued(t *testing.T) {
	settings := &memSettings{}
	raw, err := json.Marshal(record{
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}, {ID: "bot_bob", Name: "x", InstanceID: "inst_bob", OwnerUserID: "bob"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	started := time.Date(2026, 10, 22, 7, 0, 0, 0, time.UTC)
	svc.desktopAdmits = map[string]*desktopAdmitState{
		desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_a"): {task: "打开百度", status: "running", userID: "alice", botID: "bot_alice", runID: "run_a", started: started, activity: "正在打开网页"},
		desktopAdmitKey("tenant-a", "alice", "bot_alice", ""):      {task: "下一件", userID: "alice", botID: "bot_alice", started: started.Add(time.Minute), posting: 1},
		desktopAdmitKey("tenant-a", "alice", "bot_alice", "run_d"): {task: "done", status: "succeeded", done: true, userID: "alice", botID: "bot_alice", runID: "run_d"},
	}
	items, err := svc.ActiveDesktopRuns(context.Background(), "tenant-a", "alice", "bot_alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].RunID != "run_a" || items[0].Status != "running" || items[0].Step != "正在打开网页" || items[0].StartedAt == nil || !items[0].StartedAt.Equal(started) {
		t.Fatalf("running row = %#v", items[0])
	}
	if items[1].RunID != "" || items[1].Status != "queued" || items[1].Task != "下一件" {
		t.Fatalf("queued row = %#v", items[1])
	}
	if _, err := svc.ActiveDesktopRuns(context.Background(), "tenant-a", "alice", "bot_bob"); err == nil {
		t.Fatal("another owner's bot must not be listed")
	}
}
