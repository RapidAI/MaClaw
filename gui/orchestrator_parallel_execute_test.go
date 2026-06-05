package main

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOrchestratorRejectsExternalQueuedCodingSessions(t *testing.T) {
	o := &Orchestrator{}

	got := o.executeOneTask(TaskRequest{
		Tool:        "codex",
		Description: "fix code",
		ProjectPath: t.TempDir(),
	})

	if !got.Status.IsFailed() {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if !strings.Contains(got.Error, "external queued coding sessions are disabled") || !strings.Contains(got.Error, "CodingSubAgent") {
		t.Fatalf("unexpected error: %q", got.Error)
	}
}

func TestQueuedExecuteUnsupportedToolErrorForUnknownTool(t *testing.T) {
	got := queuedExecuteUnsupportedToolError("not-a-real-tool")
	if strings.Contains(got, "local built-in tool") {
		t.Fatalf("unknown tool should not be reported as local built-in: %q", got)
	}
	if !strings.Contains(got, "not a remote coding tool") {
		t.Fatalf("unexpected error: %q", got)
	}
}

func TestMarkQueuedTasksSkippedAfterRateLimit(t *testing.T) {
	tasks := []TaskRequest{
		{Tool: "codex"},
		{Tool: "claude"},
		{Tool: "opencode"},
	}
	results := map[string]SessionResult{
		"task_0": {Tool: "codex", Status: orchestratorSessionStatusFailed, Error: "HTTP 429: rate limit"},
	}

	markQueuedTasksSkippedAfterRateLimit(results, tasks, 1)

	if len(results) != len(tasks) {
		t.Fatalf("results len = %d, want %d", len(results), len(tasks))
	}
	for _, key := range []string{"task_1", "task_2"} {
		got := results[key]
		if !got.Status.IsFailed() || !strings.Contains(got.Error, "skipped") || !strings.Contains(got.Error, "rate limit") {
			t.Fatalf("%s not marked as skipped after rate limit: %#v", key, got)
		}
	}
}

func TestBuildOrchestratorSummaryCountsRateLimitSkipsSeparately(t *testing.T) {
	results := map[string]SessionResult{
		"task_0": {Tool: "codex", Status: orchestratorSessionStatusFailed, Error: "HTTP 429: rate limit"},
		"task_1": {Tool: "claude", Status: orchestratorSessionStatusFailed, Error: "skipped: prior queued task hit an LLM rate limit"},
		"task_2": {Tool: "opencode", Status: orchestratorSessionStatusFailed, Error: "skipped: prior queued task hit an LLM rate limit"},
	}

	got := buildOrchestratorSummary(results)
	if !strings.Contains(got, "1 failed") || !strings.Contains(got, "2 skipped") {
		t.Fatalf("summary should separate failed and skipped tasks, got %q", got)
	}
}

func TestFormatQueuedSessionResultLineLabelsSkippedReason(t *testing.T) {
	got := formatQueuedSessionResultLine("task_1", SessionResult{
		Tool:   "claude",
		Status: orchestratorSessionStatusFailed,
		Error:  "skipped: prior queued task hit an LLM rate limit",
	})

	if !strings.Contains(got, "status=skipped") || !strings.Contains(got, "reason=prior queued task hit an LLM rate limit") {
		t.Fatalf("skipped result should show skipped status and reason, got %q", got)
	}
	if strings.Contains(got, "status=failed") || strings.Contains(got, " error=") || strings.Contains(got, "reason=skipped:") {
		t.Fatalf("skipped result should not look like a failed error, got %q", got)
	}
}

func TestFormatQueuedSessionResultLineKeepsFailuresAsErrors(t *testing.T) {
	got := formatQueuedSessionResultLine("task_0", SessionResult{
		Tool:   "codex",
		Status: orchestratorSessionStatusFailed,
		Error:  "HTTP 500",
	})

	if !strings.Contains(got, "status=failed") || !strings.Contains(got, "error=HTTP 500") {
		t.Fatalf("failed result should keep failed status and error label, got %q", got)
	}
	if strings.Contains(got, "status=skipped") || strings.Contains(got, " reason=") {
		t.Fatalf("failed result should not look skipped, got %q", got)
	}
}

func TestExecuteParallelUsesConfiguredSubAgentConcurrency(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SetSubAgentConcurrency(2); err != nil {
		t.Fatalf("SetSubAgentConcurrency: %v", err)
	}

	var mu sync.Mutex
	active := 0
	maxActive := 0
	o := &Orchestrator{
		app:         app,
		activeTasks: make(map[string]*OrchestratorTask),
		executeTask: func(task TaskRequest) SessionResult {
			mu.Lock()
			active++
			if active > maxActive {
				maxActive = active
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			active--
			mu.Unlock()
			return SessionResult{Tool: task.Tool, Status: orchestratorSessionStatusSuccess}
		},
	}

	_, err := o.ExecuteParallel([]TaskRequest{{Tool: "codex"}, {Tool: "codex"}, {Tool: "codex"}, {Tool: "codex"}})
	if err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}
	if maxActive != 2 {
		t.Fatalf("max active tasks = %d, want 2", maxActive)
	}
}

func TestExecuteParallelInitializesActiveTaskMap(t *testing.T) {
	o := &Orchestrator{
		executeTask: func(task TaskRequest) SessionResult {
			return SessionResult{Tool: task.Tool, Status: orchestratorSessionStatusSuccess}
		},
	}

	result, err := o.ExecuteParallel([]TaskRequest{{Tool: "codex"}})
	if err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}
	if result == nil || result.TaskID == "" {
		t.Fatalf("missing result/task id: %#v", result)
	}
	if o.activeTasks == nil {
		t.Fatal("activeTasks should be initialized")
	}
}
