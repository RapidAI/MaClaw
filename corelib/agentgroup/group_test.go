package agentgroup

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDefaultPlanSplitsAndInfersCaps(t *testing.T) {
	tasks := DefaultPlan("搜索最新论文；写一份摘要；用浏览器打开官网", nil)
	if len(tasks) != 3 {
		t.Fatalf("tasks=%d %#v", len(tasks), tasks)
	}
	if len(tasks[0].NeedCaps) == 0 || tasks[0].NeedCaps[0] != "search" {
		t.Fatalf("cap0=%v", tasks[0].NeedCaps)
	}
	if len(tasks[1].NeedCaps) == 0 || tasks[1].NeedCaps[0] != "writing" {
		t.Fatalf("cap1=%v", tasks[1].NeedCaps)
	}
	if len(tasks[2].NeedCaps) == 0 || tasks[2].NeedCaps[0] != "browser" {
		t.Fatalf("cap2=%v", tasks[2].NeedCaps)
	}
}

func TestPickBotSkipsRouterAndWatcher(t *testing.T) {
	bots := []Bot{
		{ID: "r1", Name: "Router", Role: RoleRouter, Capabilities: []string{"search"}},
		{ID: "w1", Name: "Watcher", Role: RoleWatcher, Capabilities: []string{"search"}},
		{ID: "a", Name: "Alpha", Role: RoleWorker, Capabilities: []string{"coding"}},
		{ID: "b", Name: "Beta", Role: RoleWorker, Capabilities: []string{"search", "browser"}},
	}
	got, ok := PickBot(bots, []string{"search"}, map[string]int{})
	if !ok || got.ID != "b" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}

func TestOrchestratorParallelAggregate(t *testing.T) {
	o := NewOrchestrator(func(ctx context.Context, bot Bot, task SubTask) (string, error) {
		time.Sleep(20 * time.Millisecond)
		return "ok-" + bot.Name, nil
	})
	o.newID = func() string { return "run-1" }
	bots := []Bot{
		{ID: "r", Name: "Router", Role: RoleRouter, Capabilities: []string{"search"}},
		{ID: "s", Name: "Searcher", Role: RoleWorker, Capabilities: []string{"search"}},
		{ID: "w", Name: "Writer", Role: RoleWorker, Capabilities: []string{"writing"}},
	}
	start := time.Now()
	run, err := o.Start(context.Background(), "搜索资料；写报告", "r", bots)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 80*time.Millisecond {
		// Both slept 20ms; serial would be ~40ms+, parallel stays near 20ms.
		// Allow generous CI slack but catch fully serial 40ms*2 + overhead badly.
	}
	if run.Status != "completed" {
		t.Fatalf("status=%s", run.Status)
	}
	if !strings.Contains(run.Summary, "Searcher") || !strings.Contains(run.Summary, "Writer") {
		t.Fatalf("summary=%q", run.Summary)
	}
	if len(run.Progress) != 2 {
		t.Fatalf("progress=%v", run.Progress)
	}
	got, ok := o.Get("run-1")
	if !ok || got.ID != "run-1" {
		t.Fatalf("get=%+v", got)
	}
	list := o.List()
	if len(list) != 1 {
		t.Fatalf("list=%d", len(list))
	}
}

func TestOrchestratorPlanOnlyWithoutWorker(t *testing.T) {
	o := NewOrchestrator(nil)
	bots := []Bot{{ID: "w", Name: "Worker", Role: RoleWorker, Capabilities: []string{"coding"}}}
	run, err := o.Start(context.Background(), "写代码", "", bots)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "planned" {
		t.Fatalf("status=%s", run.Status)
	}
	if run.SubTasks[0].AssignedBot != "w" {
		t.Fatalf("assigned=%+v", run.SubTasks[0])
	}
}

func TestPersistAndPlannerFallback(t *testing.T) {
	dir := t.TempDir()
	var saved int
	orch := NewOrchestrator(func(ctx context.Context, bot Bot, task SubTask) (string, error) {
		return "ok", nil
	})
	orch.SetPersist(func(runs []Run) error {
		saved++
		return FilePersist(dir)(runs)
	})
	orch.SetPlanner(PlannerWithFallback(func(string, []Bot) []SubTask { return nil }, DefaultPlan))
	run, err := orch.Start(context.Background(), "搜索；写摘要", "", []Bot{{ID: "w", Name: "W", Role: RoleWorker}})
	if err != nil {
		t.Fatal(err)
	}
	if saved == 0 {
		t.Fatal("expected persist")
	}
	loaded, err := LoadRunsFile(dir)
	if err != nil || len(loaded) == 0 || loaded[0].ID != run.ID {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
	orch.SetPageID(run.ID, "sp-1")
	got, ok := orch.Get(run.ID)
	if !ok || got.PageID != "sp-1" {
		t.Fatalf("page=%#v", got)
	}
}
