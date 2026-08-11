package main

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestSoftenExploreOnlyPlanDeps(t *testing.T) {
	tasks := finalizeCodingWorkbenchTasks([]*v2.TaskItem{
		{Title: "explore auth", Description: "定位登录入口"},
		{Title: "explore billing", Description: "探查计费模块"},
		{Title: "implement JWT", Description: "实现签发"},
	}, "build auth and billing")
	tasks = softenExploreOnlyPlanDeps(tasks)
	if len(tasks) != 3 {
		t.Fatalf("len=%d", len(tasks))
	}
	// T2 explore should no longer depend solely on T1 explore.
	if len(tasks[1].DependsOn) != 0 {
		t.Fatalf("explore T2 should have empty deps after soften, got %v", tasks[1].DependsOn)
	}
	// Implement still depends on previous.
	if len(tasks[2].DependsOn) == 0 {
		t.Fatal("implement step should keep a dependency")
	}
}

func TestTaskRunnerParallelWave(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32
	sub := func(ctx context.Context, task *v2.TaskItem, config v2.TaskRunnerConfig, onToken func(string), onProgress func(string)) *v2.TaskRunResult {
		n := atomic.AddInt32(&concurrent, 1)
		for {
			cur := atomic.LoadInt32(&maxConcurrent)
			if n <= cur || atomic.CompareAndSwapInt32(&maxConcurrent, cur, n) {
				break
			}
		}
		time.Sleep(40 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
		return &v2.TaskRunResult{TaskIndex: task.Index, Title: task.Title, Status: v2.TaskPassed, Summary: "ok"}
	}
	// Two independent tasks (no deps) + one dependent.
	tasks := []*v2.TaskItem{
		{Index: 1, Title: "A"},
		{Index: 2, Title: "B"},
		{Index: 3, Title: "C", DependsOn: []int{1, 2}},
	}
	runner := v2.NewTaskRunner(v2.TaskRunnerConfig{ProjectPath: t.TempDir(), MaxRetries: 0, MaxParallel: 2}, sub)
	results := runner.RunAll(context.Background(), tasks, nil, nil)
	if len(results) != 3 {
		t.Fatalf("results=%d", len(results))
	}
	for _, r := range results {
		if r.Status != v2.TaskPassed {
			t.Fatalf("result=%+v", r)
		}
	}
	if atomic.LoadInt32(&maxConcurrent) < 2 {
		t.Fatalf("expected parallel wave concurrency>=2, got %d", maxConcurrent)
	}
}

func TestCodingWorkbenchParallelWriterAdmission(t *testing.T) {
	project := t.TempDir()
	base := func(filesA, filesB []string) []*v2.TaskItem {
		return []*v2.TaskItem{
			{Index: 1, Title: "implement auth", Description: "write auth", Files: filesA},
			{Index: 2, Title: "implement billing", Description: "write billing", Files: filesB},
		}
	}
	if !codingWorkbenchCanRunParallelWriterWave(codingRequestImplementation, true, codingWorktreeModeAuto, project, base([]string{"internal/auth/service.go"}, []string{"internal/billing/service.go"})) {
		t.Fatal("disjoint declared writer wave should be admitted")
	}
	for _, wave := range [][]*v2.TaskItem{
		base(nil, []string{"internal/billing/service.go"}),
		base([]string{"internal/auth/"}, []string{"internal/auth/service.go"}),
		base([]string{"../escape.go"}, []string{"internal/billing/service.go"}),
		base([]string{"internal/auth/*.go"}, []string{"internal/billing/service.go"}),
	} {
		if codingWorkbenchCanRunParallelWriterWave(codingRequestImplementation, true, codingWorktreeModeAuto, project, wave) {
			t.Fatalf("unsafe writer wave admitted: %+v", wave)
		}
	}
	if codingWorkbenchCanRunParallelWriterWave(codingRequestImplementation, true, codingWorktreeModeOff, project, base([]string{"a.go"}, []string{"b.go"})) {
		t.Fatal("worktree-off writer wave must remain serial")
	}
}

func TestParseRemoteVerifyExitCode(t *testing.T) {
	if parseRemoteVerifyExitCode("ok\n__MACLAW_VERIFY_EXIT:0__\n") != 0 {
		t.Fatal("want 0")
	}
	if parseRemoteVerifyExitCode("fail\n__MACLAW_VERIFY_EXIT:1__") != 1 {
		t.Fatal("want 1")
	}
}

func TestCodingLoopUsageFields(t *testing.T) {
	in, out, cost := codingLoopUsageFields(agent.TurnUsage{InputTokens: 10, OutputTokens: 5})
	if in != 10 || out != 5 {
		t.Fatalf("in=%d out=%d", in, out)
	}
	if cost <= 0 {
		t.Fatalf("expected est cost > 0, got %v", cost)
	}
}

func TestAccumulateStickyCodingUsage(t *testing.T) {
	h := &IMMessageHandler{}
	userID := "desktop-user:cost-test"
	h.accumulateStickyCodingUsage(userID, 100, 20, 0.01)
	h.accumulateStickyCodingUsage(userID, 50, 10, 0.005)
	mem := h.getStickyCodingWorkbenchMemory(userID)
	if mem.SessionInputTokens != 150 || mem.SessionOutputTokens != 30 {
		t.Fatalf("tokens in=%d out=%d", mem.SessionInputTokens, mem.SessionOutputTokens)
	}
	if mem.LastTurnInputTokens != 50 {
		t.Fatalf("last turn=%d", mem.LastTurnInputTokens)
	}
	line := formatCodingSessionCostLine(mem)
	if !strings.Contains(line, "in=150") {
		t.Fatalf("line=%q", line)
	}
	h.clearStickyCodingWorkbenchMemory(userID)
}

func TestIsCodingWorkbenchSlashBgCost(t *testing.T) {
	if !isCodingWorkbenchSlash("/bg test") || !isCodingWorkbenchSlash("/cost") {
		t.Fatal("bg/cost should classify")
	}
	if classifyImmediateIMCommand("/bg test") != imCommandCodingWorkbench {
		t.Fatal("classify bg")
	}
}
