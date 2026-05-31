package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestShouldUseSubAgent_NilOrchestrator(t *testing.T) {
	if ShouldUseSubAgent(nil) {
		t.Error("nil orchestrator should return false")
	}
}

func TestShouldUseSubAgent_InactiveOrchestrator(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	if ShouldUseSubAgent(o) {
		t.Error("inactive orchestrator should return false")
	}
}

func TestShouldUseSubAgent_ActiveDirectMode(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	// ExternalChecker is nil → always resolves to direct mode.
	o.Activate([]*TaskItem{
		{Index: 1, Title: "test task", Status: TaskExecPending},
	}, "req", "design", "/project", "")

	if !ShouldUseSubAgent(o) {
		t.Error("active orchestrator with direct mode should return true")
	}
}

func TestShouldUseSubAgent_ExternalCheckerStillUsesSubAgent(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = &mockExternalChecker{available: true}
	o.Activate([]*TaskItem{
		{Index: 1, Title: "test task", Status: TaskExecPending},
	}, "req", "design", "/project", "claude")

	if !ShouldUseSubAgent(o) {
		t.Error("external checker availability should still route to SubAgent")
	}
}

func TestShouldUseSubAgent_UsesNextReadyTaskMode(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{
		{Index: 0, Title: "blocked external task", Status: TaskExecPending, DependsOn: []int{1}},
		{Index: 1, Title: "ready direct task", Status: TaskExecPending},
	}, "req", "design", "/project", "claude")
	o.Tasks[0].ExecMode = TaskExecModeExternal
	o.Tasks[1].ExecMode = TaskExecModeDirect

	if !ShouldUseSubAgent(o) {
		t.Error("ready direct task should route to SubAgent even when current task is blocked external")
	}
}

func TestShouldUseSubAgent_RoutesReadyLegacyExternalTask(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{
		{Index: 0, Title: "blocked direct task", Status: TaskExecPending, DependsOn: []int{1}},
		{Index: 1, Title: "ready external task", Status: TaskExecPending},
	}, "req", "design", "/project", "claude")
	o.Tasks[0].ExecMode = TaskExecModeDirect
	o.Tasks[1].ExecMode = TaskExecModeExternal

	if !ShouldUseSubAgent(o) {
		t.Error("legacy external task should route to SubAgent")
	}
}

func TestShouldUseSubAgent_RoutesDependencyDeadlockForCleanup(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{
		{Index: 0, Title: "Task A", DependsOn: []int{1}},
		{Index: 1, Title: "Task B", DependsOn: []int{0}},
	}, "req", "design", "/project", "")

	if !ShouldUseSubAgent(o) {
		t.Error("dependency deadlock should route to SubAgent runner so it can mark tasks skipped")
	}
}

type mockExternalChecker struct {
	available bool
}

func (m *mockExternalChecker) IsExternalToolAvailable(toolName, projectPath string) bool {
	return m.available
}

func TestCollectPreviousOutputs(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{
		{Index: 1, Title: "Player", Files: []string{"src/player.h", "src/player.cpp"}},
		{Index: 2, Title: "Level", Files: []string{"src/level.h"}},
		{Index: 3, Title: "Game", Files: []string{"src/game.h"}},
	}, "", "", "/project", "")

	// Set statuses after activation (Activate resets all to Pending).
	o.Tasks[0].Status = TaskExecPassed
	o.Tasks[1].Status = TaskExecPassed

	// Task 0 has ActualFiles (SubAgent tracked real modifications).
	o.Tasks[0].ActualFiles = []string{"src/player.h", "src/player.cpp", "CMakeLists.txt"}
	o.Tasks[0].ActualCreatedFiles = []string{"src/player.h"}

	runner := &SubAgentTaskRunner{orchestrator: o}
	outputs := runner.collectPreviousOutputs()

	// Task 0: 3 actual files (prefers ActualFiles over Files).
	// Task 1: 1 declared file (no ActualFiles, falls back to Files).
	// Task 2: pending, not included.
	if len(outputs) != 4 {
		t.Fatalf("expected 4 outputs (3 actual + 1 declared), got %d: %v", len(outputs), outputs)
	}
	// CMakeLists.txt should be present (from ActualFiles, not in original Files).
	found := false
	for _, out := range outputs {
		if strings.Contains(out, "CMakeLists.txt") {
			found = true
		}
	}
	if !found {
		t.Error("ActualFiles should include CMakeLists.txt")
	}
	foundCreated := false
	for _, out := range outputs {
		if strings.Contains(out, "src/player.h") && strings.Contains(out, "created by T1") {
			foundCreated = true
		}
	}
	if !foundCreated {
		t.Fatalf("created files should be labeled in previous outputs: %v", outputs)
	}
}

func TestCollectPreviousOutputsCapsToRecentItems(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	var tasks []*TaskItem
	for i := 0; i < codingSubAgentPrevOutputsMax+3; i++ {
		tasks = append(tasks, &TaskItem{Index: i + 1, Title: fmt.Sprintf("Task %02d", i+1), Files: []string{fmt.Sprintf("src/file_%02d.go", i)}})
	}
	o.Activate(tasks, "", "", "/project", "")
	for _, task := range o.Tasks {
		task.Status = TaskExecPassed
	}

	runner := &SubAgentTaskRunner{orchestrator: o}
	outputs := runner.collectPreviousOutputs()
	if len(outputs) != codingSubAgentPrevOutputsMax {
		t.Fatalf("expected %d recent outputs, got %d: %v", codingSubAgentPrevOutputsMax, len(outputs), outputs)
	}
	if strings.Contains(strings.Join(outputs, "\n"), "src/file_00.go") {
		t.Fatalf("expected oldest output to be dropped, got %v", outputs)
	}
	if !strings.Contains(outputs[len(outputs)-1], fmt.Sprintf("src/file_%02d.go", codingSubAgentPrevOutputsMax+2)) {
		t.Fatalf("expected newest output to be retained, got %v", outputs)
	}
}

func TestPreviousTaskFileOutputsFallbackToPlannedFiles(t *testing.T) {
	task := &TaskItem{
		Index: 2,
		Title: "Level",
		Files: []string{"src/level.h"},
	}

	outputs := previousTaskFileOutputs(task)
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d: %v", len(outputs), outputs)
	}
	if !strings.Contains(outputs[0], "src/level.h") || !strings.Contains(outputs[0], "planned by T2") {
		t.Fatalf("expected planned fallback label, got %q", outputs[0])
	}
}

func TestPreviousTaskFileOutputsDedupesAndSorts(t *testing.T) {
	task := &TaskItem{
		Index:              3,
		Title:              "Inventory",
		ActualFiles:        []string{" src/z.go ", "src/a.go", "src/z.go", "", "src/new.go"},
		ActualCreatedFiles: []string{"src/new.go", "src/new.go", " "},
	}

	outputs := previousTaskFileOutputs(task)
	if len(outputs) != 3 {
		t.Fatalf("expected 3 unique outputs, got %d: %v", len(outputs), outputs)
	}
	if !strings.Contains(outputs[0], "src/a.go") || !strings.Contains(outputs[1], "src/new.go") || !strings.Contains(outputs[2], "src/z.go") {
		t.Fatalf("expected sorted outputs, got %v", outputs)
	}
	if !strings.Contains(outputs[1], "created by T3") {
		t.Fatalf("expected created file label, got %q", outputs[1])
	}
	if strings.Count(strings.Join(outputs, "\n"), "src/z.go") != 1 {
		t.Fatalf("expected duplicate file to be listed once, got %v", outputs)
	}
}

func TestPreviousTaskFileOutputsCompactsLongEntries(t *testing.T) {
	task := &TaskItem{
		Index:       4,
		Title:       "Long task " + strings.Repeat("title ", 80),
		ActualFiles: []string{strings.Repeat("very-long-folder/", 30) + "file.go"},
	}

	outputs := previousTaskFileOutputs(task)
	if len(outputs) != 1 {
		t.Fatalf("expected 1 output, got %d: %v", len(outputs), outputs)
	}
	if !strings.Contains(outputs[0], "截断") {
		t.Fatalf("expected long previous output to be compacted, got %q", outputs[0])
	}
	if strings.Contains(outputs[0], strings.Repeat("very-long-folder/", 20)) {
		t.Fatalf("expected long path to be compacted, got %q", outputs[0])
	}
}

func TestCompactSubAgentTaskTitle(t *testing.T) {
	if got := compactSubAgentTaskTitle("  "); got != "Untitled task" {
		t.Fatalf("empty title = %q", got)
	}
	if got := compactSubAgentTaskTitle("  Level loader  "); got != "Level loader" {
		t.Fatalf("expected title to be trimmed, got %q", got)
	}
	long := compactSubAgentTaskTitle("Task " + strings.Repeat("very long title ", 40))
	if !strings.Contains(long, "截断") {
		t.Fatalf("expected long title to be truncated, got %q", long)
	}
	if len([]rune(long)) > codingSubAgentTaskTitleMaxRunes+20 {
		t.Fatalf("task title too long: %d", len([]rune(long)))
	}
}

func TestCompactSubAgentReportSummary(t *testing.T) {
	if got := compactSubAgentReportSummary("  done  "); got != "done" {
		t.Fatalf("expected summary to be trimmed, got %q", got)
	}
	long := compactSubAgentReportSummary(strings.Repeat("report line\n", codingSubAgentReportSummaryMaxRunes))
	if !strings.Contains(long, "截断") {
		t.Fatalf("expected long report summary to be truncated, got %q", long)
	}
	if len([]rune(long)) > codingSubAgentReportSummaryMaxRunes+20 {
		t.Fatalf("report summary too long: %d", len([]rune(long)))
	}
}

func TestCompactSubAgentRunReport(t *testing.T) {
	if got := compactSubAgentRunReport("  task report  "); got != "task report" {
		t.Fatalf("expected report to be trimmed, got %q", got)
	}
	long := compactSubAgentRunReport(strings.Repeat("task report line\n", codingSubAgentRunReportMaxRunes))
	if !strings.Contains(long, "截断") {
		t.Fatalf("expected long run report to be truncated, got %q", long)
	}
	if len([]rune(long)) > codingSubAgentRunReportMaxRunes+20 {
		t.Fatalf("run report too long: %d", len([]rune(long)))
	}
}

func TestAppendSubAgentRunReportsCapsLongLists(t *testing.T) {
	var reports []string
	for i := 0; i < codingSubAgentRunReportMaxItems+2; i++ {
		reports = append(reports, fmt.Sprintf("T%d report", i))
	}

	var b strings.Builder
	appendSubAgentRunReports(&b, reports)
	out := b.String()
	if strings.Contains(out, fmt.Sprintf("T%d report", codingSubAgentRunReportMaxItems+1)) {
		t.Fatalf("expected reports to be capped, got %q", out)
	}
	if !strings.Contains(out, "and 2 more task reports omitted") {
		t.Fatalf("expected remaining report count, got %q", out)
	}
}

func TestAppendSubAgentExecutionStats(t *testing.T) {
	tasks := []*TaskItem{
		{Index: 0, Status: TaskExecPassed, ActualFiles: []string{"src/a.go", "src/b.go"}, ActualCreatedFiles: []string{"src/a.go"}},
		{Index: 1, Status: TaskExecFailed, ActualFiles: []string{"src/failed.go"}},
		{Index: 2, Status: TaskExecSkipped},
		{Index: 3, Status: TaskExecPassed, Files: []string{"planned.go"}},
	}

	var b strings.Builder
	appendSubAgentExecutionStats(&b, tasks)
	out := b.String()
	if !strings.Contains(out, "1 failed") || !strings.Contains(out, "1 skipped") {
		t.Fatalf("expected task status counts, got %q", out)
	}
	if !strings.Contains(out, "Modified files: 3") || !strings.Contains(out, "src/a.go") || !strings.Contains(out, "src/failed.go") {
		t.Fatalf("expected actual modified file stats, got %q", out)
	}
	if !strings.Contains(out, "Created files: 1") || !strings.Contains(out, "src/a.go") {
		t.Fatalf("expected created file stats, got %q", out)
	}
	if !strings.Contains(out, "without tracked file changes: 1") {
		t.Fatalf("expected planned-only count, got %q", out)
	}
}

func TestUpdateTaskActualArtifactsRecordsFailedOutputs(t *testing.T) {
	task := &TaskItem{Index: 1, Title: "Failed partial"}
	updateTaskActualArtifacts(task, []string{"src/z.go", "src/a.go", "src/z.go", " "}, []string{"src/new.go", "src/new.go"})
	if len(task.ActualFiles) != 2 || task.ActualFiles[0] != "src/a.go" || task.ActualFiles[1] != "src/z.go" {
		t.Fatalf("expected sorted unique modified files from failed result, got %#v", task.ActualFiles)
	}
	if len(task.ActualCreatedFiles) != 1 || task.ActualCreatedFiles[0] != "src/new.go" {
		t.Fatalf("expected sorted unique created files from failed result, got %#v", task.ActualCreatedFiles)
	}
}

func TestRunCurrentTaskHandlesNilRunnerOrOrchestrator(t *testing.T) {
	if summary, passed := (*SubAgentTaskRunner)(nil).RunCurrentTask(nil, nil); passed || !strings.Contains(summary, "orchestrator") {
		t.Fatalf("expected nil runner to fail safely, got passed=%v summary=%q", passed, summary)
	}

	runner := &SubAgentTaskRunner{}
	if summary, passed := runner.RunCurrentTask(nil, nil); passed || !strings.Contains(summary, "orchestrator") {
		t.Fatalf("expected missing orchestrator to fail safely, got passed=%v summary=%q", passed, summary)
	}
}

func TestRunAllTasksHandlesNilRunnerOrOrchestrator(t *testing.T) {
	if summary := (*SubAgentTaskRunner)(nil).RunAllTasks(nil, nil); !strings.Contains(summary, "orchestrator") {
		t.Fatalf("expected nil runner to fail safely, got %q", summary)
	}

	runner := &SubAgentTaskRunner{}
	if summary := runner.RunAllTasks(nil, nil); !strings.Contains(summary, "orchestrator") {
		t.Fatalf("expected missing orchestrator to fail safely, got %q", summary)
	}
}

func TestRunAllTasksStopsAfterNoProgressAttempts(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 1
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}
	maxAttempts := runner.maxRunAllTaskAttempts()
	calls := 0

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "stale pass"}
	}

	report := runner.RunAllTasks(nil, nil)
	if !strings.Contains(report, "stopped after") {
		t.Fatalf("expected no-progress stop report, got %q", report)
	}
	if calls != maxAttempts {
		t.Fatalf("subagent calls = %d, want max attempts %d", calls, maxAttempts)
	}
}

func TestRunAllTasksUsesConfiguredSubAgentConcurrency(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SetSubAgentConcurrency(2); err != nil {
		t.Fatalf("SetSubAgentConcurrency: %v", err)
	}
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B"},
		{Index: 2, Title: "Task C"},
	}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{handler: &IMMessageHandler{app: app}, orchestrator: orch}

	var mu sync.Mutex
	active := 0
	maxActive := 0
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
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
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: task.Title + " done"}
	}

	report := runner.RunAllTasks(nil, nil)
	if maxActive != 2 {
		t.Fatalf("max active SubAgents = %d, want 2; report=%q", maxActive, report)
	}
	for _, task := range orch.Tasks {
		if task.Status != TaskExecPassed {
			t.Fatalf("task did not pass: %#v", task)
		}
	}
}

func TestRunCurrentTaskHandlesNilSubAgentResult(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return nil
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if passed {
		t.Fatal("nil subagent result must not pass")
	}
	if !strings.Contains(summary, "no result") {
		t.Fatalf("expected nil-result summary, got %q", summary)
	}
	if got := orch.Tasks[0]; got.Status != TaskExecFailed || !strings.Contains(got.ErrorSummary, "no result") {
		t.Fatalf("expected task to be failed with nil-result error, got %#v", got)
	}
}

func TestRunCurrentTaskEmitsCodingAgentProgress(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 1, Title: "Implement parser"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		emitCodingAgentEvent(onProgress, CodingAgentEvent{
			Version: 1,
			Agent:   "coding",
			Event:   "tool_started",
			Phase:   "running",
			Detail:  "read_file",
		})
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "done"}
	}

	var progress []string
	summary, passed := runner.RunCurrentTask(nil, func(text string) {
		progress = append(progress, text)
	})
	if !passed {
		t.Fatalf("expected task to pass, summary=%q", summary)
	}
	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, "Coding Agent Event:") ||
		!strings.Contains(joined, `"phase":"starting"`) ||
		!strings.Contains(joined, `"phase":"completed"`) ||
		!strings.Contains(joined, `"event":"tool_started"`) ||
		!strings.Contains(joined, `"task_id":"T1"`) ||
		!strings.Contains(joined, `"run_id":"`) ||
		!strings.Contains(joined, `"turn_id":"coding-turn-`) ||
		!strings.Contains(joined, `"title":"Implement parser"`) {
		t.Fatalf("expected coding agent progress markers, got %#v", progress)
	}
}

func TestEmitCodingSubAgentProgressFiltersEmptyMessages(t *testing.T) {
	var progress []string
	emitCodingSubAgentProgress(func(text string) {
		progress = append(progress, text)
	}, "  Coding Agent: running T2 - Write tests  ")
	emitCodingSubAgentProgress(func(text string) {
		progress = append(progress, text)
	}, "   ")
	emitCodingSubAgentProgress(nil, "Coding Agent: completed T2 - Write tests")

	if strings.Join(progress, "\n") != "Coding Agent: running T2 - Write tests" {
		t.Fatalf("expected only trimmed non-empty progress, got %#v", progress)
	}
}

func TestRunCurrentTaskNormalizesEmptyFailedError(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 0
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecFailed}
	}

	var progress []string
	summary, passed := runner.RunCurrentTask(nil, func(text string) {
		progress = append(progress, text)
	})
	if passed {
		t.Fatal("failed result must not pass")
	}
	if !strings.Contains(summary, "without an error summary") {
		t.Fatalf("expected fallback error summary, got %q", summary)
	}
	if joined := strings.Join(progress, "\n"); !strings.Contains(joined, `"phase":"failed"`) || !strings.Contains(joined, `"task_id":"T0"`) {
		t.Fatalf("expected failed coding agent progress, got %#v", progress)
	}
	if got := orch.Tasks[0]; got.Status != TaskExecFailed || !strings.Contains(got.ErrorSummary, "without an error summary") {
		t.Fatalf("expected fallback error on task, got %#v", got)
	}
}

func TestRunCurrentTaskTreatsUnknownSubAgentStatusAsFailed(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 0
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: ""}
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if passed {
		t.Fatal("unknown status must not pass")
	}
	if !strings.Contains(summary, "unknown status") {
		t.Fatalf("expected unknown-status summary, got %q", summary)
	}
	if got := orch.Tasks[0]; got.Status != TaskExecFailed || !strings.Contains(got.ErrorSummary, "unknown status") {
		t.Fatalf("expected task to fail on unknown status, got %#v", got)
	}
}

func TestRunCurrentTaskRetainsPartialArtifactsDuringRetry(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 2
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{
			Status:        TaskExecFailed,
			Error:         "tests failed",
			FilesModified: []string{"src/a.go"},
			FilesCreated:  []string{"src/new.go"},
		}
	}

	var progress []string
	summary, passed := runner.RunCurrentTask(nil, func(text string) {
		progress = append(progress, text)
	})
	if passed {
		t.Fatal("failed retry result must not pass")
	}
	if !strings.Contains(summary, "tests failed") {
		t.Fatalf("expected failure summary, got %q", summary)
	}
	if joined := strings.Join(progress, "\n"); !strings.Contains(joined, `"phase":"retrying"`) || !strings.Contains(joined, `"detail":"1/2"`) {
		t.Fatalf("expected retrying coding agent progress, got %#v", progress)
	}
	got := orch.Tasks[0]
	if got.Status != TaskExecInProgress || got.RetryCount != 1 {
		t.Fatalf("retryable failure should remain in progress with retry count 1, got %#v", got)
	}
	if strings.Join(got.ActualFiles, ",") != "src/a.go" || strings.Join(got.ActualCreatedFiles, ",") != "src/new.go" {
		t.Fatalf("partial artifacts should be retained for retry, got modified=%#v created=%#v", got.ActualFiles, got.ActualCreatedFiles)
	}
}

func TestRunAllTasksPausesOnRateLimitWithoutRetryStorm(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 3
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B"},
	}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}
	calls := 0

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		return &CodingSubAgentResult{Status: TaskExecFailed, Error: `LLM call failed: HTTP 429: {"code":"LLM_ENDPOINT_USER_RATE_LIMITED","message":"user request rate exceeded, please retry shortly"}`}
	}

	report := runner.RunAllTasks(nil, nil)
	if calls != 1 {
		t.Fatalf("rate limit should stop after one subagent call, got %d", calls)
	}
	if !strings.Contains(report, "paused to avoid retry storms") {
		t.Fatalf("expected paused report, got %q", report)
	}
	if got := orch.Tasks[0]; got.RetryCount != 0 || got.Status != TaskExecInProgress {
		t.Fatalf("current task should remain retryable/in progress, got %#v", got)
	}
	if got := orch.Tasks[1]; got.Status != TaskExecPending || got.RetryCount != 0 {
		t.Fatalf("next task should not start after rate limit, got %#v", got)
	}
}

func TestRunAllTasksPausesOnTransientProviderErrorWithoutRetryStorm(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 3
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B"},
	}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}
	calls := 0

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		return &CodingSubAgentResult{Status: TaskExecFailed, Error: `LLM call failed: HTTP 503: service unavailable`}
	}

	report := runner.RunAllTasks(nil, nil)
	if calls != 1 {
		t.Fatalf("transient provider error should stop after one subagent call, got %d", calls)
	}
	if !strings.Contains(report, "paused to avoid retry storms") {
		t.Fatalf("expected paused report, got %q", report)
	}
	if got := orch.Tasks[0]; got.RetryCount != 0 || got.Status != TaskExecInProgress {
		t.Fatalf("current task should remain retryable/in progress, got %#v", got)
	}
	if got := orch.Tasks[1]; got.Status != TaskExecPending || got.RetryCount != 0 {
		t.Fatalf("next task should not start after transient provider error, got %#v", got)
	}
}

func TestIsSubAgentTransientProviderErrorRequiresProviderContextForGenericTimeouts(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{name: "http 503", text: `LLM call failed: HTTP 503: service unavailable`, want: true},
		{name: "provider deadline", text: `LLM call failed: Post "https://example.test/api/anthropic/v1/messages": context deadline exceeded`, want: true},
		{name: "provider connection reset", text: `provider request failed: connection reset by peer`, want: true},
		{name: "local command deadline", text: `bash command failed: context deadline exceeded`, want: false},
		{name: "local http deadline", text: `integration test failed: Post "http://127.0.0.1:3000/health": context deadline exceeded`, want: false},
		{name: "local connection reset", text: `integration test failed: connection reset by peer`, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSubAgentTransientProviderError(tc.text); got != tc.want {
				t.Fatalf("isSubAgentTransientProviderError(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestRunAllTasksPausesFutureBatchesAfterConcurrentRateLimit(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SetSubAgentConcurrency(2); err != nil {
		t.Fatalf("SetSubAgentConcurrency: %v", err)
	}
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 3
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B"},
		{Index: 2, Title: "Task C"},
	}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{handler: &IMMessageHandler{app: app}, orchestrator: orch}

	var mu sync.Mutex
	started := map[int]bool{}
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		mu.Lock()
		started[task.Index] = true
		mu.Unlock()

		if task.Index == 0 {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: `HTTP 429: too many requests`}
		}
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: task.Title + " done"}
	}

	report := runner.RunAllTasks(nil, nil)
	if !strings.Contains(report, "paused to avoid retry storms") {
		t.Fatalf("expected paused report, got %q", report)
	}
	mu.Lock()
	defer mu.Unlock()
	if !started[0] || !started[1] {
		t.Fatalf("first batch should start tasks 0 and 1, got %#v", started)
	}
	if started[2] {
		t.Fatalf("future batch should not start after rate limit, got %#v", started)
	}
	if got := orch.Tasks[2]; got.Status != TaskExecPending || got.RetryCount != 0 {
		t.Fatalf("future task should remain pending, got %#v", got)
	}
}

func TestRunAllTasksPausesOnRateLimitWhenRetriesDisabled(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 0
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}
	calls := 0

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		return &CodingSubAgentResult{Status: TaskExecFailed, Error: `HTTP 429: too many requests`}
	}

	report := runner.RunAllTasks(nil, nil)
	if calls != 1 {
		t.Fatalf("rate limit should stop after one subagent call, got %d", calls)
	}
	if !strings.Contains(report, "paused to avoid retry storms") {
		t.Fatalf("expected paused report, got %q", report)
	}
	if got := orch.Tasks[0]; got.RetryCount != 0 || got.Status != TaskExecInProgress {
		t.Fatalf("current task should stay resumable after rate limit, got %#v", got)
	}
}

func TestRunAllTasksSkipsTasksBlockedByFailedDependencies(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SetSubAgentConcurrency(2); err != nil {
		t.Fatalf("SetSubAgentConcurrency: %v", err)
	}
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 0
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B", DependsOn: []int{0}},
	}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{handler: &IMMessageHandler{app: app}, orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecFailed, Error: `compile failed`}
	}

	report := runner.RunAllTasks(nil, nil)
	if !strings.Contains(report, "blocked by failed dependencies") {
		t.Fatalf("expected blocked dependency report, got %q", report)
	}
	if got := orch.Tasks[0]; got.Status != TaskExecFailed {
		t.Fatalf("dependency task should fail, got %#v", got)
	}
	if got := orch.Tasks[1]; got.Status != TaskExecSkipped || !strings.Contains(got.ErrorSummary, "dependency T1") {
		t.Fatalf("dependent task should be skipped, got %#v", got)
	}
}

func TestRunAllTasksSequentialSkipsTasksBlockedByFailedDependencies(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 0
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B", DependsOn: []int{0}},
	}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}
	calls := 0

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		return &CodingSubAgentResult{Status: TaskExecFailed, Error: `compile failed`}
	}

	report := runner.RunAllTasks(nil, nil)
	if calls != 1 {
		t.Fatalf("dependent task should not run after failed dependency, calls=%d", calls)
	}
	if !strings.Contains(report, "blocked by failed dependencies") {
		t.Fatalf("expected blocked dependency report, got %q", report)
	}
	if got := orch.Tasks[1]; got.Status != TaskExecSkipped || !strings.Contains(got.ErrorSummary, "dependency T1") {
		t.Fatalf("dependent task should be skipped, got %#v", got)
	}
}

func TestRunAllTasksSequentialWaitsForDependenciesOutOfOrder(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A", DependsOn: []int{1}},
		{Index: 1, Title: "Task B"},
	}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}
	var order []int

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		order = append(order, task.Index)
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: task.Title + " done"}
	}

	report := runner.RunAllTasks(nil, nil)
	if strings.Contains(report, "no runnable tasks") {
		t.Fatalf("dependency-ordered tasks should complete, got %q", report)
	}
	if got := fmt.Sprint(order); got != "[1 0]" {
		t.Fatalf("execution order = %s, want [1 0]", got)
	}
	for _, task := range orch.Tasks {
		if task.Status != TaskExecPassed {
			t.Fatalf("task should pass after dependency ordering, got %#v", task)
		}
	}
}

func TestRunAllTasksSkipsDependencyCycles(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A", DependsOn: []int{1}},
		{Index: 1, Title: "Task B", DependsOn: []int{0}},
	}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}
	calls := 0

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: task.Title + " done"}
	}

	report := runner.RunAllTasks(nil, nil)
	if calls != 0 {
		t.Fatalf("cyclic dependency tasks should not run, calls=%d", calls)
	}
	if !strings.Contains(report, "dependency deadlock") {
		t.Fatalf("expected dependency deadlock report, got %q", report)
	}
	for _, task := range orch.Tasks {
		if task.Status != TaskExecSkipped || !strings.Contains(task.ErrorSummary, "dependency deadlock") {
			t.Fatalf("cyclic task should be skipped, got %#v", task)
		}
	}
}

func TestRunCurrentTaskEmitsSkippedCodingAgentProgress(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{Status: TaskExecSkipped, Error: "blocked by dependency"}
	}

	var progress []string
	summary, passed := runner.RunCurrentTask(nil, func(text string) {
		progress = append(progress, text)
	})
	if passed {
		t.Fatal("skipped result must not pass")
	}
	if !strings.Contains(summary, "blocked by dependency") {
		t.Fatalf("expected skipped summary, got %q", summary)
	}
	if joined := strings.Join(progress, "\n"); !strings.Contains(joined, `"phase":"skipped"`) || !strings.Contains(joined, `"task_id":"T0"`) {
		t.Fatalf("expected skipped coding agent progress, got %#v", progress)
	}
	if got := orch.Tasks[0]; got.Status != TaskExecSkipped || !strings.Contains(got.ErrorSummary, "blocked by dependency") {
		t.Fatalf("expected task to be skipped with reason, got %#v", got)
	}
}

func TestRunCurrentTaskRecoversSubAgentPanic(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		panic("boom")
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if passed {
		t.Fatal("panic result must not pass")
	}
	if !strings.Contains(summary, "panicked") || !strings.Contains(summary, "boom") {
		t.Fatalf("expected panic summary, got %q", summary)
	}
	if got := orch.Tasks[0]; got.Status != TaskExecFailed || !strings.Contains(got.ErrorSummary, "panicked") {
		t.Fatalf("expected task to be failed with panic error, got %#v", got)
	}
}

func TestRunCurrentTaskIgnoresStaleSubAgentPanic(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		orch.Activate([]*TaskItem{{Index: 0, Title: "New Task"}}, "", "", "/project", "")
		panic("old run")
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if passed {
		t.Fatal("stale panic must not pass")
	}
	if !strings.Contains(summary, "过期") && !strings.Contains(summary, "杩囨湡") && !strings.Contains(summary, "expired") {
		t.Fatalf("expected stale result summary, got %q", summary)
	}
	if got := orch.Tasks[0]; got.Title != "New Task" || got.Status != TaskExecPending || got.ErrorSummary != "" {
		t.Fatalf("stale panic mutated new task: %#v", got)
	}
}

func TestRunCurrentTaskIgnoresStaleNilSubAgentResult(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		orch.Activate([]*TaskItem{{Index: 0, Title: "New Task"}}, "", "", "/project", "")
		return nil
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if passed {
		t.Fatal("stale nil result must not pass")
	}
	if !strings.Contains(summary, "过期") && !strings.Contains(summary, "杩囨湡") && !strings.Contains(summary, "expired") {
		t.Fatalf("expected stale result summary, got %q", summary)
	}
	if got := orch.Tasks[0]; got.Title != "New Task" || got.Status != TaskExecPending || got.ErrorSummary != "" {
		t.Fatalf("stale nil result mutated new task: %#v", got)
	}
}

func TestRunCurrentTaskIgnoresStalePassedResult(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	originalRun := orch.RunID
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		orch.Activate([]*TaskItem{{Index: 0, Title: "New Task"}}, "", "", "/project", "")
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "stale success", FilesModified: []string{"stale.go"}}
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if passed {
		t.Fatal("stale passed result must not be reported as passed")
	}
	if !strings.Contains(summary, "过期") && !strings.Contains(summary, "expired") {
		t.Fatalf("expected stale result summary, got %q", summary)
	}
	if orch.RunID == originalRun {
		t.Fatal("test setup failed: run id did not advance")
	}
	if got := orch.Tasks[0]; got.Title != "New Task" || got.Status != TaskExecPending || len(got.ActualFiles) != 0 {
		t.Fatalf("stale result mutated new task: %#v", got)
	}
}

func TestStaleSubAgentRunSummaryHandlesNilTask(t *testing.T) {
	summary := staleSubAgentRunSummary(nil, "Lost")
	if !strings.Contains(summary, "T-1") || !strings.Contains(summary, "Lost") {
		t.Fatalf("unexpected stale summary: %q", summary)
	}
}
