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

func TestCollectPreviousOutputsCapsButPreservesRecentSummaries(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	var tasks []*TaskItem
	for i := 0; i < codingSubAgentPrevOutputsMax+3; i++ {
		tasks = append(tasks, &TaskItem{Index: i, Title: fmt.Sprintf("Task %02d", i)})
	}
	o.Activate(tasks, "", "", "/project", "")
	for i, task := range o.Tasks {
		task.Status = TaskExecPassed
		task.ActualFiles = []string{fmt.Sprintf("src/file_%02d.go", i)}
		task.ResultSummary = fmt.Sprintf("implementation %02d\n\n## 质量审计\n\nWARNING: verification note %02d", i, i)
	}

	runner := &SubAgentTaskRunner{orchestrator: o}
	outputs := runner.collectPreviousOutputs()
	joined := strings.Join(outputs, "\n")
	if len(outputs) != codingSubAgentPrevOutputsMax {
		t.Fatalf("expected %d capped outputs, got %d: %#v", codingSubAgentPrevOutputsMax, len(outputs), outputs)
	}
	if !strings.Contains(joined, "Previous passed task summary") || !strings.Contains(joined, "verification note") {
		t.Fatalf("capped previous outputs should preserve high-signal summaries, got %#v", outputs)
	}
	if strings.Contains(joined, "src/file_00.go") {
		t.Fatalf("oldest detail output should still be dropped, got %#v", outputs)
	}
}
func TestPreviousTaskFileOutputsIncludesCreatedFilesWithoutModifiedList(t *testing.T) {
	task := &TaskItem{
		Index:              1,
		Title:              "Create helper",
		ActualCreatedFiles: []string{"src/helper.go"},
	}

	outputs := previousTaskFileOutputs(task)
	if len(outputs) != 1 {
		t.Fatalf("expected created file output, got %d: %#v", len(outputs), outputs)
	}
	if !strings.Contains(outputs[0], "src/helper.go") || !strings.Contains(outputs[0], "created by T1") {
		t.Fatalf("created-only actual artifact should be visible downstream, got %#v", outputs)
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

func TestRunCurrentTaskRecordsFailedResultSummaryForRetryContext(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 1
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{
			Status:            TaskExecFailed,
			Summary:           "attempt changed parser\n\n## 质量审计\n\nFAILED: verification not run",
			Error:             "verification not run",
			QualityStatus:     codingSubAgentQualityFailed,
			QualitySummary:    "verification not run",
			QualityIssueCount: 1,
		}
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if passed || !strings.Contains(summary, "will retry") {
		t.Fatalf("expected retryable failure, passed=%v summary=%q", passed, summary)
	}
	got := orch.Tasks[0]
	if got.RetryCount != 1 || got.Status != TaskExecInProgress {
		t.Fatalf("expected failed task to be queued for retry, got %#v", got)
	}
	if !strings.Contains(got.ResultSummary, "## 质量审计") || !strings.Contains(got.ResultSummary, "verification not run") {
		t.Fatalf("failed result summary should be recorded for retry, got %q", got.ResultSummary)
	}
}

func TestCurrentTaskRetryOutputsIncludesFailedResultSummary(t *testing.T) {
	task := &TaskItem{
		Index:         2,
		Title:         "Fix parser",
		RetryCount:    1,
		ErrorSummary:  "quality audit evidence: FAILED: verification not run",
		ResultSummary: "changed parser\n\n## 质量审计\n\nFAILED: verification not run\n\n## 验证状态\n\nmissing: no fresh verification",
	}

	outputs := currentTaskRetryOutputs(task)
	joined := strings.Join(outputs, "\n")
	if !strings.Contains(joined, "Retry context for T2") || !strings.Contains(joined, "Recovery hint") {
		t.Fatalf("expected retry context and recovery hint, got %#v", outputs)
	}
	if !strings.Contains(joined, "Previous failed attempt summary for T2") || !strings.Contains(joined, "## 质量审计") || !strings.Contains(joined, "verification not run") {
		t.Fatalf("retry outputs should include failed attempt summary, got %#v", outputs)
	}
}

func TestCurrentTaskRetryOutputsUsesSummaryWhenErrorSummaryMissing(t *testing.T) {
	task := &TaskItem{
		Index:         3,
		Title:         "Fix retry context",
		RetryCount:    1,
		ResultSummary: "attempt touched retry flow\n\n## 质量审计\n\nFAILED: diff not checked",
		ActualFiles:   []string{"gui/coding_subagent_orchestrator.go"},
	}

	outputs := currentTaskRetryOutputs(task)
	joined := strings.Join(outputs, "\n")
	for _, want := range []string{
		"Retry context for T3",
		"did not provide an error summary",
		"Previous failed attempt summary for T3",
		"## 质量审计",
		"diff not checked",
		"Retry artifact from previous attempt: gui/coding_subagent_orchestrator.go",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("retry outputs missing %q: %#v", want, outputs)
		}
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
	if !strings.Contains(got.ErrorSummary, "tests failed") {
		t.Fatalf("retryable failure should preserve error summary for retry context, got %#v", got)
	}
	if strings.Join(got.ActualFiles, ",") != "src/a.go" || strings.Join(got.ActualCreatedFiles, ",") != "src/new.go" {
		t.Fatalf("partial artifacts should be retained for retry, got modified=%#v created=%#v", got.ActualFiles, got.ActualCreatedFiles)
	}
}

func TestRunCurrentTaskFailsPassedResultWhenQualityAuditFailed(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 1
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{
			Status:            TaskExecPassed,
			Summary:           "model claimed success",
			QualityStatus:     codingSubAgentQualityFailed,
			QualitySummary:    "verification not run; diff not checked",
			QualityIssueCount: 2,
		}
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if passed {
		t.Fatal("passed subagent result with failed quality audit must not pass")
	}
	if !strings.Contains(summary, "will retry") || !strings.Contains(summary, "quality audit failed") || !strings.Contains(summary, "verification not run") {
		t.Fatalf("quality audit failure should drive retry summary, got %q", summary)
	}
	got := orch.Tasks[0]
	if got.Status != TaskExecInProgress || got.RetryCount != 1 || !strings.Contains(got.ErrorSummary, "quality audit failed") {
		t.Fatalf("quality audit failure should be tracked as retryable task failure, got %#v", got)
	}
}

func TestRunCurrentTaskRecordsPassedResultSummaryForDownstreamContext(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{
			Status:            TaskExecPassed,
			Summary:           "implementation complete",
			QualityStatus:     codingSubAgentQualityWarning,
			QualitySummary:    "1 dynamic tool failed: call_mcp_tool browser/screenshot -> browser closed",
			QualityIssueCount: 1,
		}
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if !passed {
		t.Fatalf("task should pass, summary=%q", summary)
	}
	got := orch.Tasks[0]
	if got.ResultSummary == "" || !strings.Contains(got.ResultSummary, "## 质量审计") || !strings.Contains(got.ResultSummary, "WARNING") {
		t.Fatalf("passed result summary should preserve quality warning for downstream context, got %#v", got)
	}
}

func TestCollectPreviousOutputsIncludesPassedQualityWarningSummary(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B"},
	}, "", "", "/project", "")
	orch.Tasks[0].Status = TaskExecPassed
	orch.Tasks[0].ActualFiles = []string{"src/a.go"}
	orch.Tasks[0].ResultSummary = "implementation complete\n\n## 质量审计\n\nWARNING: dynamic tool failed"
	runner := &SubAgentTaskRunner{orchestrator: orch}

	outputs := runner.collectPreviousOutputs()
	joined := strings.Join(outputs, "\n")
	if !strings.Contains(joined, "src/a.go") ||
		!strings.Contains(joined, "Previous passed task summary for T1") ||
		!strings.Contains(joined, "WARNING: dynamic tool failed") {
		t.Fatalf("previous outputs should include file and quality warning summary, got %#v", outputs)
	}
}
func TestCollectPreviousOutputsPrioritizesQualitySummarySection(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	orch.Tasks[0].Status = TaskExecPassed
	orch.Tasks[0].ActualFiles = []string{"src/a.go"}
	orch.Tasks[0].ResultSummary = strings.Repeat("implementation detail sentence. ", 80) + "\n\n## 命令验证\n\n- PASS: `go test ./gui`\n\n## 质量审计\n\nWARNING: acceptance criteria verification not summarized (1 issue(s))"
	runner := &SubAgentTaskRunner{orchestrator: orch}

	outputs := runner.collectPreviousOutputs()
	joined := strings.Join(outputs, "\n")
	if !strings.Contains(joined, "Previous passed task summary for T1") || !strings.Contains(joined, "## 质量审计 WARNING") {
		t.Fatalf("previous outputs should prioritize quality section, got %#v", outputs)
	}
	if strings.Contains(joined, "implementation detail sentence") {
		t.Fatalf("previous output summary should not spend the compact context budget on long model prose, got %#v", outputs)
	}
}

func TestAppendSubAgentQualityReportSummaryIsIdempotent(t *testing.T) {
	result := &CodingSubAgentResult{
		QualityStatus:     codingSubAgentQualityWarning,
		QualitySummary:    "dynamic tool warning",
		QualityIssueCount: 1,
	}
	first := appendSubAgentQualityReportSummary("implementation complete", result)
	second := appendSubAgentQualityReportSummary(first, result)
	if strings.Count(second, "## 质量审计") != 1 {
		t.Fatalf("quality section should be appended once, got %q", second)
	}
	if !strings.Contains(first, "WARNING: dynamic tool warning (1 issue(s))") {
		t.Fatalf("quality section should include status and issue count, got %q", first)
	}

	stale := "implementation complete\n\n## 质量审计\n\nPASS: model claimed everything passed\n\n## 验证状态\n\nPASS: go test ./gui"
	replaced := appendSubAgentQualityReportSummary(stale, result)
	if strings.Count(replaced, "## 质量审计") != 1 ||
		strings.Contains(replaced, "model claimed everything passed") ||
		!strings.Contains(replaced, "WARNING: dynamic tool warning (1 issue(s))") ||
		!strings.Contains(replaced, "## 验证状态") {
		t.Fatalf("authoritative quality section should replace stale model-provided section and preserve following sections, got %q", replaced)
	}
}

func TestRunCurrentTaskIncludesQualityWarningInPassedSummary(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		return &CodingSubAgentResult{
			Status:            TaskExecPassed,
			Summary:           "implementation complete",
			QualityStatus:     codingSubAgentQualityWarning,
			QualitySummary:    "1 dynamic tool failed: call_mcp_tool browser/screenshot -> browser closed",
			QualityIssueCount: 1,
		}
	}

	summary, passed := runner.RunCurrentTask(nil, nil)
	if !passed {
		t.Fatalf("quality warning should not fail an otherwise passed task, summary=%q", summary)
	}
	if !strings.Contains(summary, "implementation complete") || !strings.Contains(summary, "## 质量审计") || !strings.Contains(summary, "WARNING") || !strings.Contains(summary, "1 issue") {
		t.Fatalf("passed summary should include quality warning audit, got %q", summary)
	}
	if got := orch.Tasks[0]; got.Status != TaskExecPassed || got.ErrorSummary != "" {
		t.Fatalf("quality warning should keep task passed without error summary, got %#v", got)
	}
}
func TestRunCurrentTaskDoesNotRetryNonRetryableGitDiffFailure(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 3
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	calls := 0
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		return &CodingSubAgentResult{Status: TaskExecFailed, Error: "git diff unavailable: D:\\work is not a git repository"}
	}

	var progress []string
	summary, passed := runner.RunCurrentTask(nil, func(text string) {
		progress = append(progress, text)
	})
	if passed {
		t.Fatal("non-retryable failure must not pass")
	}
	if calls != 1 {
		t.Fatalf("non-retryable failure should call subagent once, got %d", calls)
	}
	if strings.Contains(summary, "will retry") || !strings.Contains(summary, "non-retryable") {
		t.Fatalf("expected non-retryable failure summary without retry, got %q", summary)
	}
	got := orch.Tasks[0]
	if got.Status != TaskExecFailed || got.RetryCount != 0 || !strings.Contains(got.ErrorSummary, "not a git repository") {
		t.Fatalf("non-retryable failure should fail without incrementing retry count, got %#v", got)
	}
	joined := strings.Join(progress, "\n")
	if !strings.Contains(joined, `"phase":"failed"`) || !strings.Contains(joined, `"detail":"non_retryable"`) {
		t.Fatalf("expected non-retryable failed progress event, got %#v", progress)
	}
}

func TestRunCurrentTaskRetryContextIncludesPartialArtifacts(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 2
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	calls := 0
	var retryPrevOutputs []string
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		if calls == 1 {
			return &CodingSubAgentResult{
				Status:        TaskExecFailed,
				Error:         "tests failed",
				FilesModified: []string{"src/a.go"},
				FilesCreated:  []string{"src/new.go"},
			}
		}
		retryPrevOutputs = append([]string(nil), prevOutputs...)
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "fixed after retry"}
	}

	if summary, passed := runner.RunCurrentTask(nil, nil); passed || !strings.Contains(summary, "will retry") {
		t.Fatalf("first run should be retryable failure, passed=%v summary=%q", passed, summary)
	}
	if summary, passed := runner.RunCurrentTask(nil, nil); !passed || !strings.Contains(summary, "fixed after retry") {
		t.Fatalf("second run should pass, passed=%v summary=%q", passed, summary)
	}
	joined := strings.Join(retryPrevOutputs, "\n")
	if !strings.Contains(joined, "Retry artifact from previous attempt") ||
		!strings.Contains(joined, "src/a.go") ||
		!strings.Contains(joined, "modified by T1") ||
		!strings.Contains(joined, "src/new.go") ||
		!strings.Contains(joined, "created by T1") {
		t.Fatalf("retry prevOutputs missing partial artifact context: %#v", retryPrevOutputs)
	}
}
func TestEnrichSubAgentFailureErrorAddsQualityEvidenceOnce(t *testing.T) {
	result := &CodingSubAgentResult{
		Status:            TaskExecFailed,
		Error:             "quality gate failed",
		QualityStatus:     codingSubAgentQualityFailed,
		QualitySummary:    "verification not run; diff not checked",
		QualityIssueCount: 2,
	}

	enriched := enrichSubAgentFailureError(result, result.Error)
	if !strings.Contains(enriched, "quality audit evidence") ||
		!strings.Contains(enriched, "FAILED: verification not run; diff not checked") ||
		!strings.Contains(enriched, "2 issue") {
		t.Fatalf("quality evidence missing from enriched error: %q", enriched)
	}

	again := enrichSubAgentFailureError(result, enriched)
	if strings.Count(again, "quality audit evidence") != 1 {
		t.Fatalf("quality evidence should not be duplicated, got %q", again)
	}
}
func TestRunCurrentTaskRetryContextIncludesFailedToolEvidence(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 2
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	calls := 0
	var retryPrevOutputs []string
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		if calls == 1 {
			return &CodingSubAgentResult{
				Status: TaskExecFailed,
				Error:  "quality gate failed",
				CommandsRun: []CodingSubAgentCommandResult{
					{Command: "go test ./gui", Succeeded: false, Summary: "compile failed"},
				},
				DynamicToolsRun: []CodingSubAgentDynamicToolResult{
					{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "MCP call failed: browser closed"},
				},
				QualityStatus:     codingSubAgentQualityFailed,
				QualitySummary:    "verification failed; diff not checked",
				QualityIssueCount: 2,
			}
		}
		retryPrevOutputs = append([]string(nil), prevOutputs...)
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "fixed after retry"}
	}

	if summary, passed := runner.RunCurrentTask(nil, nil); passed || !strings.Contains(summary, "will retry") {
		t.Fatalf("first run should be retryable failure, passed=%v summary=%q", passed, summary)
	}
	if summary, passed := runner.RunCurrentTask(nil, nil); !passed || !strings.Contains(summary, "fixed after retry") {
		t.Fatalf("second run should pass, passed=%v summary=%q", passed, summary)
	}
	joined := strings.Join(retryPrevOutputs, "\n")
	if !strings.Contains(joined, "failed command evidence") ||
		!strings.Contains(joined, "go test ./gui") ||
		!strings.Contains(joined, "compile failed") ||
		!strings.Contains(joined, "dynamic tool evidence") ||
		!strings.Contains(joined, "call_mcp_tool browser/screenshot") ||
		!strings.Contains(joined, "MCP call failed") ||
		!strings.Contains(joined, "quality audit evidence") ||
		!strings.Contains(joined, "verification failed; diff not checked") ||
		!strings.Contains(joined, "2 issue") {
		t.Fatalf("retry prevOutputs missing failed tool evidence: %#v", retryPrevOutputs)
	}
}

func TestRunCurrentTaskRetryContextOmitsResolvedFailedToolEvidence(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 2
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	calls := 0
	var retryPrevOutputs []string
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		if calls == 1 {
			return &CodingSubAgentResult{
				Status: TaskExecFailed,
				Error:  "quality gate failed",
				CommandsRun: []CodingSubAgentCommandResult{
					{Command: "go test ./gui", Succeeded: false, Summary: "compile failed", seq: 1},
					{Command: "go   test ./gui", Succeeded: true, Summary: "ok github.com/RapidAI/CodeClaw/gui 0.1s", seq: 2},
				},
				DynamicToolsRun: []CodingSubAgentDynamicToolResult{
					{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: false, Summary: "MCP call failed: browser closed", seq: 1},
					{Tool: "call_mcp_tool", Name: "browser/screenshot", Succeeded: true, Summary: "ok", seq: 2},
				},
				QualityStatus:     codingSubAgentQualityFailed,
				QualitySummary:    "remaining risk not called out",
				QualityIssueCount: 1,
			}
		}
		retryPrevOutputs = append([]string(nil), prevOutputs...)
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "fixed after retry"}
	}

	if summary, passed := runner.RunCurrentTask(nil, nil); passed || !strings.Contains(summary, "will retry") {
		t.Fatalf("first run should be retryable failure, passed=%v summary=%q", passed, summary)
	}
	if summary, passed := runner.RunCurrentTask(nil, nil); !passed || !strings.Contains(summary, "fixed after retry") {
		t.Fatalf("second run should pass, passed=%v summary=%q", passed, summary)
	}
	joined := strings.Join(retryPrevOutputs, "\n")
	if strings.Contains(joined, "failed command evidence") || strings.Contains(joined, "dynamic tool evidence") {
		t.Fatalf("retry prevOutputs should omit resolved failed evidence, got %#v", retryPrevOutputs)
	}
	if !strings.Contains(joined, "quality audit evidence") || !strings.Contains(joined, "remaining risk not called out") {
		t.Fatalf("retry prevOutputs should retain unresolved quality evidence, got %#v", retryPrevOutputs)
	}
}

func TestRunCurrentTaskInjectsRetryFailureContext(t *testing.T) {
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 2
	orch.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{orchestrator: orch}

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	calls := 0
	var retryPrevOutputs []string
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		calls++
		if calls == 1 {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: "go test failed: expected 200 got 500"}
		}
		retryPrevOutputs = append([]string(nil), prevOutputs...)
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "fixed after retry"}
	}

	if summary, passed := runner.RunCurrentTask(nil, nil); passed || !strings.Contains(summary, "will retry") {
		t.Fatalf("first run should be retryable failure, passed=%v summary=%q", passed, summary)
	}
	if got := orch.Tasks[0]; got.RetryCount != 1 || got.Status != TaskExecInProgress || !strings.Contains(got.ErrorSummary, "expected 200") {
		t.Fatalf("first failure should preserve retry metadata, got %#v", got)
	}

	if summary, passed := runner.RunCurrentTask(nil, nil); !passed || !strings.Contains(summary, "fixed after retry") {
		t.Fatalf("second run should pass, passed=%v summary=%q", passed, summary)
	}
	joined := strings.Join(retryPrevOutputs, "\n")
	if !strings.Contains(joined, "Retry context for T1") || !strings.Contains(joined, "expected 200") || !strings.Contains(joined, "Do not repeat") {
		t.Fatalf("retry prevOutputs missing failure context: %#v", retryPrevOutputs)
	}
}

func TestCurrentTaskRetryOutputsAddsRecoveryHint(t *testing.T) {
	cases := []string{
		"1 guardrail block(s): Set-Content src\\a.go x",
		"1 guardrail block(s): python -c \"from pathlib import Path; Path('src/a.go').rename('src/b.go')\"",
		"1 guardrail block(s): node -e \"const fs = require('fs'); fs.rm('src/a.go', () => {})\"",
	}
	for _, errSummary := range cases {
		task := &TaskItem{
			Index:        0,
			Title:        "Fix generated file",
			RetryCount:   1,
			ErrorSummary: errSummary,
		}

		outputs := currentTaskRetryOutputs(task)
		joined := strings.Join(outputs, "\n")
		if !strings.Contains(joined, "Recovery hint") ||
			!strings.Contains(joined, "edit_file/edit_lines") ||
			!strings.Contains(joined, "inline Node/Python filesystem APIs") {
			t.Fatalf("retry output should include shell-write recovery hint for %q, got %#v", errSummary, outputs)
		}
	}
}

func TestCurrentTaskRetryOutputsIncludesPartialArtifacts(t *testing.T) {
	task := &TaskItem{
		Index:              0,
		Title:              "Fix generated file",
		RetryCount:         1,
		ErrorSummary:       "tests failed",
		ActualFiles:        []string{"src/a.go"},
		ActualCreatedFiles: []string{"src/new.go"},
	}

	outputs := currentTaskRetryOutputs(task)
	joined := strings.Join(outputs, "\n")
	if !strings.Contains(joined, "Retry context for T1") ||
		!strings.Contains(joined, "Retry artifact from previous attempt") ||
		!strings.Contains(joined, "src/a.go") ||
		!strings.Contains(joined, "modified by T1") ||
		!strings.Contains(joined, "src/new.go") ||
		!strings.Contains(joined, "created by T1") {
		t.Fatalf("retry outputs should include partial artifacts, got %#v", outputs)
	}
}
func TestCurrentTaskRetryOutputsCapsPartialArtifacts(t *testing.T) {
	var files []string
	for i := 0; i < codingSubAgentResultFilesMax+3; i++ {
		files = append(files, fmt.Sprintf("src/file-%03d.go", i))
	}
	task := &TaskItem{
		Index:        0,
		Title:        "Many files",
		RetryCount:   1,
		ErrorSummary: "tests failed",
		ActualFiles:  files,
	}

	outputs := currentTaskRetryOutputs(task)
	joined := strings.Join(outputs, "\n")
	if got := strings.Count(joined, "Retry artifact from previous attempt:"); got != codingSubAgentResultFilesMax+1 {
		t.Fatalf("retry artifact output count = %d, want %d; outputs=%#v", got, codingSubAgentResultFilesMax+1, outputs)
	}
	if !strings.Contains(joined, "3 more touched file(s) omitted") {
		t.Fatalf("retry artifact output should report omitted count, got %#v", outputs)
	}
	if strings.Contains(joined, "src/file-082.go") {
		t.Fatalf("retry artifact output should be capped, got %#v", outputs)
	}
}
func TestSubAgentRetryRecoveryHintCoversQualityGateTiming(t *testing.T) {
	cases := []struct {
		err  string
		want string
	}{
		{err: "no exploration before existing-file edits", want: "codegraph explore"},
		{err: "请先调用 read_file(path=\"src/a.go\") 查看当前内容，再使用 edit_file 修改。", want: "refresh the snapshot"},
		{err: "文件 src/a.go 自上次 read_file 后已变化，请重新调用 read_file 获取最新内容后再修改。", want: "snapshot is fresh"},
		{err: "verification ran before the final edit (1 command); rerun test/build/lint/typecheck after editing", want: "after the final edit"},
		{err: "verification command used failure-suppressing shell syntax (1 command); rerun test/build/lint/typecheck without || fallback, pipe filters, output redirection, or extra commands after the verifier", want: "output redirection"},
		{err: "stale diff was not final diff", want: "after the last edit"},
		{err: "quality gate failed; quality audit evidence: FAILED: verification not run; diff not checked (2 issue(s))", want: "focused verification command after the final edit"},
		{err: "quality gate failed; quality audit evidence: FAILED: no exploration before existing-file edits; verification not run; diff not checked", want: "inspect/explore before editing"},
		{err: "quality gate failed; quality audit evidence: FAILED: 有 1 条验证命令未实际执行测试或检查：`pytest tests`", want: "succeeded without actually running tests"},
		{err: "quality gate failed; quality audit evidence: FAILED: npm test -> No test files found, exiting with code 0", want: "test selector/path/config"},
		{err: "quality gate failed; quality audit evidence: FAILED: acceptance criteria verification does not reference each listed criterion", want: "AC/标准 label"},
		{err: "quality gate failed; quality audit evidence: FAILED: created files without inspection or project-context evidence", want: "Inspect existing project context"},
		{err: "quality gate failed; quality audit evidence: FAILED: no file changes and no inspection or verification evidence", want: "empty/no-evidence answer"},
		{err: "quality gate failed; quality audit evidence: FAILED: changed files outside listed task scope without summary rationale: `src/router.go`", want: "scope rationale"},
		{err: "quality gate failed; quality audit evidence: FAILED: claimed verification command not found in audit log: `go test ./gui`", want: "commands present in the audit log"},
		{err: "quality gate failed; quality audit evidence: FAILED: claimed verification command passed but audit log recorded failure: `go test ./gui`", want: "audit log recorded failure"},
		{err: "quality gate failed; quality audit evidence: FAILED: changed files not referenced in final summary: `src/settings.go`", want: "actual modified and created file paths"},
		{err: "quality gate failed; quality audit evidence: FAILED: remaining risk not called out in final summary", want: "remaining-risk note"},
		{err: "git diff unavailable: D:\\work is not a git repository", want: "repeating git_diff will not help"},
		{err: `Error: tool call "read_file" is missing required argument "path". Example valid arguments: {"path":"src/main.go"}.`, want: "Example valid arguments snippet"},
		{err: "模型连续返回空响应，任务中断", want: "empty model responses"},
		{err: "hard exit after repeated empty response", want: "concrete tool call"},
		{err: `Error: tool call "read_file" is missing required argument "path".`, want: "every required argument"},
		{err: `Error: tool call "bash" has invalid argument type for "working_dir": expected string, got number.`, want: "valid JSON scalar types"},
		{err: `Error: tool call "read_file" has invalid argument value for "lines": expected integer >= 1, got 0.`, want: "allowed values"},
		{err: "failed command evidence: go test ./... -> context deadline exceeded", want: "command timed out"},
		{err: "quality gate failed; failed command evidence: go test ./gui -> compile failed", want: "failing command output"},
		{err: `Error: call_mcp_tool target "Wiki/get_page_children" is missing required MCP argument "parent_id" in arguments.`, want: "arguments.parent_id"},
		{err: `Error: manage_skill target "ui-ux-pro-max" is missing required skill argument "input" in args.`, want: "args.input"},
		{err: "1 dynamic tool failed: call_mcp_tool browser/screenshot -> MCP call failed: browser closed", want: "dynamic tool failure output"},
	}
	for _, tc := range cases {
		got := subAgentRetryRecoveryHint(tc.err)
		if !strings.Contains(got, tc.want) {
			t.Fatalf("hint for %q = %q, want containing %q", tc.err, got, tc.want)
		}
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

func TestSubAgentNonRetryableFailureClassification(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{name: "not git repo", text: "git diff unavailable: D:\\work is not a git repository", want: true},
		{name: "missing directory", text: "git diff unavailable: D:\\missing is not a directory", want: true},
		{name: "cannot inspect", text: "git diff unavailable: cannot inspect project path D:\\work", want: true},
		{name: "missing verification", text: "verification not run after final edit", want: false},
		{name: "exploration", text: "no exploration before existing-file edits", want: false},
		{name: "guardrail", text: "1 guardrail block(s): Set-Content src\\a.go", want: false},
		{name: "test failure", text: "go test failed: expected 200 got 500", want: false},
		{name: "dynamic tool", text: "1 dynamic tool failed: call_mcp_tool browser/screenshot", want: false},
		{name: "provider transient", text: "LLM call failed: HTTP 503: service unavailable", want: false},
		{name: "generic git diff", text: "git diff failed: exit status 1", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSubAgentNonRetryableFailure(tc.text); got != tc.want {
				t.Fatalf("isSubAgentNonRetryableFailure(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
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
		{name: "provider overloaded", text: `LLM provider overloaded, retry later`, want: true},
		{name: "provider gateway timeout", text: `openai upstream request timeout`, want: true},
		{name: "provider unexpected eof", text: `anthropic request failed: unexpected EOF`, want: true},
		{name: "provider http 408", text: `LLM call failed: HTTP 408 request timeout`, want: true},
		{name: "local command deadline", text: `bash command failed: context deadline exceeded`, want: false},
		{name: "local http deadline", text: `integration test failed: Post "http://127.0.0.1:3000/health": context deadline exceeded`, want: false},
		{name: "local connection reset", text: `integration test failed: connection reset by peer`, want: false},
		{name: "local http 408", text: `integration test failed: HTTP 408 request timeout`, want: false},
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

func TestRunAllTasksFallsBackToSequentialAfterTerminalFailure(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SetSubAgentConcurrency(3); err != nil {
		t.Fatalf("SetSubAgentConcurrency: %v", err)
	}
	orch := NewTaskExecutionOrchestrator()
	orch.MaxRetries = 0 // no retries — first failure is terminal
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B"},
		{Index: 2, Title: "Task C"},
		{Index: 3, Title: "Task D"},
	}, "", "", "/project", "")
	runner := &SubAgentTaskRunner{handler: &IMMessageHandler{app: app}, orchestrator: orch}

	var mu sync.Mutex
	batchActive := 0

	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		mu.Lock()
		batchActive++
		mu.Unlock()

		time.Sleep(15 * time.Millisecond)

		mu.Lock()
		batchActive--
		mu.Unlock()

		// Task B fails terminally
		if task.Title == "Task B" {
			return &CodingSubAgentResult{Status: TaskExecFailed, Error: "compile error"}
		}
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: task.Title + " done"}
	}

	var progressMsgs []string
	report := runner.RunAllTasks(nil, func(text string) {
		mu.Lock()
		progressMsgs = append(progressMsgs, text)
		mu.Unlock()
	})

	// Task B should be failed.
	if orch.Tasks[1].Status != TaskExecFailed {
		t.Fatalf("Task B should be failed, got %s", orch.Tasks[1].Status)
	}

	// After fallback, remaining tasks should execute sequentially (batch size 1).
	// The report should mention the fallback.
	if !strings.Contains(report, "Task B") {
		t.Fatalf("report should mention failed task, got: %s", report)
	}

	// Check that user-visible progress includes the fallback notice.
	joined := strings.Join(progressMsgs, "\n")
	if !strings.Contains(joined, "顺序执行") {
		t.Fatalf("expected fallback progress notification, got: %s", joined)
	}
}

func TestBatchHasFailedTaskIgnoresRetryableFailures(t *testing.T) {
	// A task that failed but will be retried has Status still in InProgress
	// (IncrementTaskRetryForRun doesn't change status). This should NOT
	// trigger the fallback.
	handles := []TaskRunHandle{
		{Task: &TaskItem{Index: 0, Status: TaskExecPassed}},
		{Task: &TaskItem{Index: 1, Status: TaskExecInProgress}}, // retryable
	}
	if batchHasFailedTask(handles) {
		t.Fatal("in-progress (retryable) task should not trigger fallback")
	}

	// Terminal failures DO trigger fallback.
	handles[1].Task.Status = TaskExecFailed
	if !batchHasFailedTask(handles) {
		t.Fatal("failed task should trigger fallback")
	}

	handles[1].Task.Status = TaskExecSkipped
	if !batchHasFailedTask(handles) {
		t.Fatal("skipped task should trigger fallback")
	}
}
