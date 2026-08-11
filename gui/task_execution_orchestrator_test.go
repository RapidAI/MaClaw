package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseTaskListFromText(t *testing.T) {
	input := `1. 初始化项目结构
创建 Go module，设置目录结构

2. 实现核心数据模型
定义 User 和 Session 结构体
- user.go
- session.go

### 验收标准
- User 结构体包含 ID、Name、Email 字段
- Session 结构体包含 Token 和 ExpiresAt 字段

3. 实现 HTTP API
创建 REST API 端点
- handler.go
- router.go`

	tasks := ParseTaskListFromText(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	if tasks[0].Title != "初始化项目结构" {
		t.Errorf("task 0 title = %q, want %q", tasks[0].Title, "初始化项目结构")
	}
	if tasks[0].Index != 0 {
		t.Errorf("task 0 index = %d, want 0", tasks[0].Index)
	}

	if tasks[1].Title != "实现核心数据模型" {
		t.Errorf("task 1 title = %q, want %q", tasks[1].Title, "实现核心数据模型")
	}
	if len(tasks[1].AcceptanceCriteria) < 1 {
		t.Errorf("task 1 should have acceptance criteria, got %d", len(tasks[1].AcceptanceCriteria))
	}

	if tasks[2].Title != "实现 HTTP API" {
		t.Errorf("task 2 title = %q, want %q", tasks[2].Title, "实现 HTTP API")
	}
}

func TestParseTaskListFromTextMarkdownTHeaders(t *testing.T) {
	input := `## 贪吃蛇游戏 — 任务拆分

### T1: 初始化 CMake 构建环境和依赖配置
- **描述**: 创建项目目录结构，编写 CMakeLists.txt 和 vcpkg.json
- **涉及文件**: CMakeLists.txt, vcpkg.json

### T2：实现 Snake 类
- **描述**: 实现 Snake.h 和 Snake.cpp
- **涉及文件**: src/Snake.h, src/Snake.cpp

### T3. 实现 Game 类
- **描述**: 实现 Game.h 和 Game.cpp
- **涉及文件**: src/Game.h, src/Game.cpp`

	tasks := ParseTaskListFromText(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
	if tasks[0].Title != "初始化 CMake 构建环境和依赖配置" {
		t.Fatalf("task 0 title = %q", tasks[0].Title)
	}
	if tasks[1].Title != "实现 Snake 类" {
		t.Fatalf("task 1 title = %q", tasks[1].Title)
	}
	if tasks[2].Title != "实现 Game 类" {
		t.Fatalf("task 2 title = %q", tasks[2].Title)
	}
}

func TestOrchestratorLifecycle(t *testing.T) {
	o := NewTaskExecutionOrchestrator()

	if o.IsActive() {
		t.Fatal("should not be active before Activate()")
	}

	tasks := []*TaskItem{
		{Index: 0, Title: "Task A", Description: "Do A", AcceptanceCriteria: []string{"A passes"}},
		{Index: 1, Title: "Task B", Description: "Do B", DependsOn: []int{0}},
		{Index: 2, Title: "Task C", Description: "Do C"},
	}

	o.Activate(tasks, "req summary", "design summary", "/project", "claude")

	if !o.IsActive() {
		t.Fatal("should be active after Activate()")
	}

	// Current task should be Task A
	cur := o.CurrentTask()
	if cur == nil || cur.Title != "Task A" {
		t.Fatalf("current task should be Task A, got %v", cur)
	}

	// Mark as in progress
	o.MarkCurrentStatus(TaskExecInProgress, "")
	if cur.Status != TaskExecInProgress {
		t.Errorf("status = %s, want in_progress", cur.Status)
	}

	// Mark as passed and advance
	o.MarkCurrentStatus(TaskExecPassed, "")
	if !o.AdvanceToNext() {
		t.Fatal("should advance to Task B")
	}

	cur = o.CurrentTask()
	if cur == nil || cur.Title != "Task B" {
		t.Fatalf("current task should be Task B, got %v", cur)
	}

	// Mark B as failed and advance
	o.MarkCurrentStatus(TaskExecFailed, "test failed")
	if !o.AdvanceToNext() {
		t.Fatal("should advance to Task C")
	}

	cur = o.CurrentTask()
	if cur == nil || cur.Title != "Task C" {
		t.Fatalf("current task should be Task C, got %v", cur)
	}

	o.MarkCurrentStatus(TaskExecPassed, "")
	if o.AdvanceToNext() {
		t.Fatal("should not advance past last task")
	}

	if !o.AllDone() {
		t.Fatal("all tasks should be done")
	}

	o.Deactivate()
	if o.IsActive() {
		t.Fatal("should not be active after Deactivate()")
	}
}

func TestReadyTaskHandlesHonorsDependenciesAndLimit(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B", DependsOn: []int{0}},
		{Index: 2, Title: "Task C"},
	}, "", "", "/project", "")

	handles := o.ReadyTaskHandles(2)
	if len(handles) != 2 || handles[0].Task.Title != "Task A" || handles[1].Task.Title != "Task C" {
		t.Fatalf("ready handles should include independent pending tasks only, got %#v", handles)
	}

	o.MarkTaskStatus(handles[0].Task, TaskExecPassed, "")
	handles = o.ReadyTaskHandles(2)
	if len(handles) != 2 || handles[0].Task.Title != "Task B" || handles[1].Task.Title != "Task C" {
		t.Fatalf("dependency should become ready after prerequisite passes, got %#v", handles)
	}
}

func TestMarkTasksBlockedByDependenciesSkipsFailedDependents(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{
		{Index: 0, Title: "Task A"},
		{Index: 1, Title: "Task B", DependsOn: []int{0}},
		{Index: 2, Title: "Task C", DependsOn: []int{99}},
		{Index: 3, Title: "Task D"},
	}, "", "", "/project", "")
	o.MarkTaskStatus(o.Tasks[0], TaskExecFailed, "boom")

	if skipped := o.MarkTasksBlockedByDependencies(); skipped != 2 {
		t.Fatalf("skipped blocked tasks = %d, want 2", skipped)
	}
	if got := o.Tasks[1]; got.Status != TaskExecSkipped || !strings.Contains(got.ErrorSummary, "dependency T1") {
		t.Fatalf("dependent on failed task should be skipped, got %#v", got)
	}
	if got := o.Tasks[2]; got.Status != TaskExecSkipped || !strings.Contains(got.ErrorSummary, "invalid dependency") {
		t.Fatalf("dependent on invalid task should be skipped, got %#v", got)
	}
	if got := o.Tasks[3]; got.Status != TaskExecPending {
		t.Fatalf("independent task should remain pending, got %#v", got)
	}
}

func TestMarkDependencyDeadlockTasksSkipsCycles(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{
		{Index: 0, Title: "Task A", DependsOn: []int{1}},
		{Index: 1, Title: "Task B", DependsOn: []int{0}},
	}, "", "", "/project", "")

	if handles := o.ReadyTaskHandles(2); len(handles) != 0 {
		t.Fatalf("cycle should have no ready handles, got %#v", handles)
	}
	if skipped := o.MarkDependencyDeadlockTasks(); skipped != 2 {
		t.Fatalf("deadlock skipped tasks = %d, want 2", skipped)
	}
	for _, task := range o.Tasks {
		if task.Status != TaskExecSkipped || !strings.Contains(task.ErrorSummary, "dependency deadlock") {
			t.Fatalf("cyclic task should be skipped with deadlock reason, got %#v", task)
		}
	}
}

func TestBuildTaskPrompt(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	tasks := []*TaskItem{
		{
			Index:              0,
			Title:              "初始化项目",
			Description:        "创建 Go module 和目录结构",
			Files:              []string{"go.mod", "main.go"},
			AcceptanceCriteria: []string{"go build 成功", "main.go 包含 main 函数"},
		},
		{
			Index:       1,
			Title:       "实现 API",
			Description: "创建 HTTP handler",
			DependsOn:   []int{0},
		},
	}

	o.Activate(tasks, "需求摘要", "设计摘要", "/project", "claude")

	prompt := o.BuildTaskPrompt()
	if !strings.Contains(prompt, "任务 1/2") {
		t.Error("prompt should contain task number")
	}
	if !strings.Contains(prompt, "初始化项目") {
		t.Error("prompt should contain task title")
	}
	if !strings.Contains(prompt, "go.mod") {
		t.Error("prompt should contain expected files")
	}
	if !strings.Contains(prompt, "go build 成功") {
		t.Error("prompt should contain acceptance criteria")
	}
	if !strings.Contains(prompt, "需求摘要") {
		t.Error("prompt should contain requirements context")
	}
	if !strings.Contains(prompt, "设计摘要") {
		t.Error("prompt should contain design context")
	}

	// Advance to task 2 and check dependency context
	o.MarkCurrentStatus(TaskExecPassed, "")
	o.AdvanceToNext()

	prompt2 := o.BuildTaskPrompt()
	if !strings.Contains(prompt2, "任务 2/2") {
		t.Error("prompt should contain task 2 number")
	}
	if !strings.Contains(prompt2, "前置任务产出") {
		t.Error("prompt should contain dependency section")
	}
	if !strings.Contains(prompt2, "初始化项目") {
		t.Error("prompt should reference dependency task title")
	}
}

func TestBuildSystemInjection(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	if inj := o.BuildSystemInjection(); inj != "" {
		t.Errorf("inactive orchestrator should return empty injection, got %q", inj)
	}

	tasks := []*TaskItem{
		{Index: 0, Title: "Task A", Description: "Do A", Status: TaskExecPending},
		{Index: 1, Title: "Task B", Description: "Do B", Status: TaskExecPending},
	}
	o.Activate(tasks, "", "", "/p", "claude")
	o.ExternalChecker = &fakeExternalChecker{available: true}
	o.ResolveExecutionMode()

	inj := o.BuildSystemInjection()
	if !strings.Contains(inj, "1/2") {
		t.Error("injection should contain current task number")
	}
	if !strings.Contains(inj, "CodingSubAgent") {
		t.Error("injection for pending task should mention CodingSubAgent")
	}
	if strings.Contains(inj, "create_session") {
		t.Error("injection should not mention create_session")
	}
	if !strings.Contains(inj, "remaining") {
		t.Error("injection should contain progress summary")
	}
}

func TestRetryMechanism(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.MaxRetries = 2
	tasks := []*TaskItem{
		{Index: 0, Title: "Task A", Description: "Do A"},
	}
	o.Activate(tasks, "", "", "/p", "claude")

	// First retry
	if !o.IncrementRetry() {
		t.Fatal("first retry should be allowed")
	}
	// Second retry
	if !o.IncrementRetry() {
		t.Fatal("second retry should be allowed")
	}
	// Third retry — exceeds max
	if o.IncrementRetry() {
		t.Fatal("third retry should NOT be allowed (max=2)")
	}
}

func TestFinalReport(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	tasks := []*TaskItem{
		{Index: 0, Title: "Task A", Status: TaskExecPassed},
		{Index: 1, Title: "Task B", Status: TaskExecFailed, ErrorSummary: "test timeout"},
		{Index: 2, Title: "Task C", Status: TaskExecPassed},
	}
	o.Tasks = tasks

	report := o.FinalReport()
	if !strings.Contains(report, "成功: 2") {
		t.Error("report should show 2 passed")
	}
	if !strings.Contains(report, "失败: 1") {
		t.Error("report should show 1 failed")
	}
	if !strings.Contains(report, "test timeout") {
		t.Error("report should contain error summary")
	}
}

func TestProgressSummary(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	tasks := []*TaskItem{
		{Index: 0, Title: "Task A", Status: TaskExecPassed},
		{Index: 1, Title: "Task B", Status: TaskExecInProgress},
		{Index: 2, Title: "Task C", Status: TaskExecPending},
	}
	o.Tasks = tasks

	summary := o.ProgressSummary()
	if !strings.Contains(summary, "[OK]") {
		t.Error("summary should contain passed marker [OK]")
	}
	if !strings.Contains(summary, "[..]") {
		t.Error("summary should contain in-progress marker [..]")
	}
	if !strings.Contains(summary, "T1: Task A [OK]") {
		t.Error("summary should contain 'T1: Task A [OK]'")
	}
	if !strings.Contains(summary, "T2: Task B [..]") {
		t.Error("summary should contain 'T2: Task B [..]'")
	}
	// Pending tasks have no suffix marker
	if !strings.Contains(summary, "T3: Task C\n") {
		t.Error("summary should contain 'T3: Task C' on its own line without marker")
	}
}

func TestProgressSummaryShowsWaitingChildAsPendingHandoff(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Tasks = []*TaskItem{{Index: 0, Title: "Inspect", Status: TaskExecWaitingChild}}
	if summary := o.ProgressSummary(); !strings.Contains(summary, "[waiting child results]") || !strings.Contains(summary, "1 remaining") {
		t.Fatalf("waiting child must remain visible as an unfinished handoff: %q", summary)
	}
	if context := o.buildTaskContextLocked(o.Tasks[0]); !strings.Contains(context, "[waiting child results]") {
		t.Fatalf("task context must not hide waiting child status: %q", context)
	}
}

func TestDependencyDeadlockDoesNotDiscardTasksBehindRuntimeHandoff(t *testing.T) {
	for _, parentStatus := range []TaskExecStatus{TaskExecWaitingApproval, TaskExecWaitingChild, TaskExecInterrupted} {
		t.Run(string(parentStatus), func(t *testing.T) {
			o := NewTaskExecutionOrchestrator()
			o.Active = true
			o.Tasks = []*TaskItem{
				{Index: 0, Title: "parent", Status: parentStatus},
				{Index: 1, Title: "dependent", Status: TaskExecPending, DependsOn: []int{0}},
			}
			if skipped := o.MarkDependencyDeadlockTasks(); skipped != 0 {
				t.Fatalf("skipped=%d; downstream task must remain pending during %s", skipped, parentStatus)
			}
			if got := o.Tasks[1].Status; got != TaskExecPending {
				t.Fatalf("dependent status=%s, want pending", got)
			}
		})
	}
}

func TestIsTaskHeader_FalsePositives(t *testing.T) {
	// "任务完成后..." should NOT be a task header (no digit after 任务)
	if isTaskHeader("任务完成后需要运行测试") {
		t.Error("'任务完成后...' should not be a task header")
	}
	// "任务 1: xxx" SHOULD be a task header
	if !isTaskHeader("任务 1: 初始化项目") {
		t.Error("'任务 1: xxx' should be a task header")
	}
	// "Task 2: xxx" SHOULD be a task header
	if !isTaskHeader("Task 2: Implement API") {
		t.Error("'Task 2: xxx' should be a task header")
	}
	// "任务描述" should NOT be a task header
	if isTaskHeader("任务描述很长") {
		t.Error("'任务描述...' should not be a task header")
	}
}

func TestExtractTaskTitle_MultiByte(t *testing.T) {
	// Chinese full-width colon
	title := extractTaskTitle("任务 1：初始化项目结构")
	if title != "初始化项目结构" {
		t.Errorf("got %q, want %q", title, "初始化项目结构")
	}

	// Chinese enumeration comma
	title2 := extractTaskTitle("1、创建数据模型")
	if title2 != "创建数据模型" {
		t.Errorf("got %q, want %q", title2, "创建数据模型")
	}

	// ASCII dot
	title3 := extractTaskTitle("3. Implement HTTP handler")
	if title3 != "Implement HTTP handler" {
		t.Errorf("got %q, want %q", title3, "Implement HTTP handler")
	}
}

func TestCountStatus_SkippedNotFailed(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Tasks = []*TaskItem{
		{Index: 0, Title: "A", Status: TaskExecPassed},
		{Index: 1, Title: "B", Status: TaskExecFailed},
		{Index: 2, Title: "C", Status: TaskExecSkipped},
		{Index: 3, Title: "D", Status: TaskExecPending},
	}
	o.mu.Lock()
	passed, failed, remaining := o.countStatusLocked()
	o.mu.Unlock()

	if passed != 1 {
		t.Errorf("passed = %d, want 1", passed)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want 1 (skipped should not count as failed)", failed)
	}
	if remaining != 1 {
		t.Errorf("remaining = %d, want 1", remaining)
	}
}

func TestParseTaskList_DescriptionLines(t *testing.T) {
	// Ensure "任务完成后" inside a description doesn't start a new task
	input := `1. 初始化项目
创建目录结构
任务完成后需要运行 go build 验证

2. 实现 API
创建 HTTP handler`

	tasks := ParseTaskListFromText(input)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if !strings.Contains(tasks[0].Description, "任务完成后") {
		t.Error("'任务完成后...' should be part of task 1 description, not a new task")
	}
}

func TestBuildIntegrationPrompt(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Tasks = []*TaskItem{
		{Index: 0, Title: "初始化项目", Status: TaskExecPassed, Files: []string{"go.mod", "main.go"}},
		{Index: 1, Title: "实现 API", Status: TaskExecPassed, Files: []string{"handler.go", "router.go"}},
		{Index: 2, Title: "实现数据库", Status: TaskExecFailed, Files: []string{"db.go"}, ErrorSummary: "连接超时"},
	}
	o.RequirementsContext = "一个简单的 REST API 服务"
	o.DesignContext = "三层架构：handler → service → repository"

	prompt := o.BuildIntegrationPrompt()

	if !strings.Contains(prompt, "集成联调") {
		t.Error("prompt should contain integration header")
	}
	if !strings.Contains(prompt, "go.mod") {
		t.Error("prompt should list output files from completed tasks")
	}
	if !strings.Contains(prompt, "handler.go") {
		t.Error("prompt should list output files from task 2")
	}
	if !strings.Contains(prompt, "import") {
		t.Error("prompt should mention import/dependency checks")
	}
	if !strings.Contains(prompt, "main") {
		t.Error("prompt should mention main entry file")
	}
	if !strings.Contains(prompt, "编译") {
		t.Error("prompt should mention compilation")
	}
	// Failed task warning
	if !strings.Contains(prompt, "不完整") {
		t.Error("prompt should warn about incomplete files from failed tasks")
	}
	if !strings.Contains(prompt, "实现数据库") {
		t.Error("prompt should name the failed task in the warning")
	}
	// Design context should be included (more important than requirements for integration)
	if !strings.Contains(prompt, "三层架构") {
		t.Error("prompt should include design context")
	}
	// Requirements context should also be included
	if !strings.Contains(prompt, "REST API") {
		t.Error("prompt should include requirements context")
	}
}

func TestBuildIntegrationPrompt_NoFailedTasks(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Tasks = []*TaskItem{
		{Index: 0, Title: "Task A", Status: TaskExecPassed, Files: []string{"a.go"}},
		{Index: 1, Title: "Task B", Status: TaskExecPassed, Files: []string{"b.go"}},
	}

	prompt := o.BuildIntegrationPrompt()

	// Should NOT contain the failed task warning
	if strings.Contains(prompt, "不完整") {
		t.Error("prompt should not warn about incomplete files when no tasks failed")
	}
}

func TestCurrentTaskAPIsIgnoreInactiveOrchestrator(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/p", "claude")
	task := o.Tasks[0]
	task.ExecMode = TaskExecModeExternal
	o.Deactivate()

	o.RecordCurrentActualArtifacts([]string{"inactive.go"}, []string{"created.go"})
	o.MarkCurrentStatus(TaskExecFailed, "inactive")
	if o.IncrementRetry() {
		t.Fatal("inactive current retry should be rejected")
	}
	o.SetCurrentSessionID("inactive-session")
	if prompt := o.BuildTaskPrompt(); prompt != "" {
		t.Fatalf("inactive current task prompt should be empty, got %q", prompt)
	}
	if prompt := o.BuildTDDPrompt(); prompt != "" {
		t.Fatalf("inactive TDD prompt should be empty, got %q", prompt)
	}
	if prompt := o.BuildFixPrompt("failed"); prompt != "" {
		t.Fatalf("inactive fix prompt should be empty, got %q", prompt)
	}
	if o.DegradeCurrentToDirectMode() {
		t.Fatal("inactive current degradation should be rejected")
	}
	o.ResolveExecutionMode()

	if task.Status != TaskExecPending || task.RetryCount != 0 || task.SessionID != "" || len(task.ActualFiles) != 0 || len(task.ActualCreatedFiles) != 0 || task.ExecMode != TaskExecModeExternal {
		t.Fatalf("inactive current API mutated task: %#v", task)
	}
}

func TestTargetTaskAPIsIgnoreInactiveOrchestrator(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/p", "")
	task := o.Tasks[0]
	o.Deactivate()

	if o.RecordTaskActualArtifacts(task, []string{"inactive.go"}, []string{"created.go"}) {
		t.Fatal("inactive target artifact update should be rejected")
	}
	if status, ok := o.TaskStatus(task); ok || status != "" {
		t.Fatalf("inactive target status read should be rejected, got %q ok=%v", status, ok)
	}
	if o.IsTaskTerminal(task) {
		t.Fatal("inactive target terminal read should be false")
	}
	if o.MarkTaskStatus(task, TaskExecFailed, "inactive") {
		t.Fatal("inactive target status update should be rejected")
	}
	if retryCount, allowed := o.IncrementTaskRetry(task); retryCount != 0 || allowed {
		t.Fatalf("inactive target retry should be rejected, got count=%d allowed=%v", retryCount, allowed)
	}
	if o.SetTaskSessionID(task, "inactive-session") {
		t.Fatal("inactive target session update should be rejected")
	}
	if prompt := o.BuildTaskPromptForTask(task); prompt != "" {
		t.Fatalf("inactive target prompt should be empty, got %q", prompt)
	}
	if task.Status != TaskExecPending || task.RetryCount != 0 || task.SessionID != "" || len(task.ActualFiles) != 0 || len(task.ActualCreatedFiles) != 0 {
		t.Fatalf("inactive target API mutated task: %#v", task)
	}
}

func TestBuildSystemInjectionForTaskRunRejectsStaleRun(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	task := &TaskItem{Index: 0, Title: "Task A", Status: TaskExecPending}
	o.Activate([]*TaskItem{task}, "", "", "/p", "")
	_, oldRunID := o.CurrentTaskHandle()
	o.Activate([]*TaskItem{{Index: 0, Title: "Task B", Status: TaskExecPending}}, "", "", "/p", "")

	if inj := o.BuildSystemInjectionForTaskRun(task, oldRunID); inj != "" {
		t.Fatalf("stale run should not build injection, got %q", inj)
	}
}

func TestBuildSystemInjectionForTaskRunUsesTargetTask(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{
		{Index: 0, Title: "Task A", Status: TaskExecPending, ExecMode: TaskExecModeDirect},
		{Index: 1, Title: "Task B", Status: TaskExecPending, ExecMode: TaskExecModeExternal},
	}, "", "", "/p", "claude")
	taskA, runID := o.CurrentTaskHandle()
	o.CurrentIndex = 1

	inj := o.BuildSystemInjectionForTaskRun(taskA, runID)
	if !strings.Contains(inj, "Task A") || strings.Contains(inj, "executing task 2/2") {
		t.Fatalf("target injection mismatch, got %q", inj)
	}
	if !strings.Contains(inj, "CodingSubAgent") || strings.Contains(inj, "create_session") {
		t.Fatalf("target injection should use target task mode, got %q", inj)
	}
}

func TestTDDAndFixPromptsLabelActualArtifacts(t *testing.T) {
	o := NewTaskExecutionOrchestrator()
	o.Activate([]*TaskItem{{Index: 0, Title: "Task A"}}, "", "", "/project", "")
	o.Tasks[0].ActualFiles = []string{"src/a.go"}
	o.Tasks[0].ActualCreatedFiles = []string{"src/new.go"}

	tdd := o.BuildTDDPrompt()
	if !strings.Contains(tdd, "Actual Artifacts From This Task") || !strings.Contains(tdd, "Created: src/new.go") || !strings.Contains(tdd, "Modified: src/a.go") {
		t.Fatalf("TDD prompt should label actual artifacts, got %q", tdd)
	}
	fix := o.BuildFixPrompt("test failed")
	if !strings.Contains(fix, "Actual Artifacts From This Task") || !strings.Contains(fix, "Created: src/new.go") || !strings.Contains(fix, "Modified: src/a.go") {
		t.Fatalf("fix prompt should label actual artifacts, got %q", fix)
	}
}

func TestParseTaskListExtractsBareFileBullets(t *testing.T) {
	input := "1. Implement model\nDefine files below.\n- user.go\n- internal/session_store.go\n- README.md\n\n### acceptance\n- go test ./..."
	tasks := ParseTaskListFromText(input)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %#v", tasks)
	}
	want := []string{"user.go", "internal/session_store.go", "README.md"}
	if strings.Join(tasks[0].Files, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected files: got %#v want %#v", tasks[0].Files, want)
	}
	if len(tasks[0].AcceptanceCriteria) != 1 || tasks[0].AcceptanceCriteria[0] != "go test ./..." {
		t.Fatalf("acceptance criteria should not be classified as files: %#v", tasks[0].AcceptanceCriteria)
	}
}

func TestParseTaskListDoesNotTreatURLsOrSentencesAsFiles(t *testing.T) {
	input := "1. Document behavior\n- https://example.com/spec.md\n- this is not a file.go sentence\n- pkg/server.go"
	tasks := ParseTaskListFromText(input)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %#v", tasks)
	}
	if len(tasks[0].Files) != 1 || tasks[0].Files[0] != "pkg/server.go" {
		t.Fatalf("expected only real file path, got %#v", tasks[0].Files)
	}
}

func TestParseTaskListUnicodeHeadersAndCriteria(t *testing.T) {
	input := "1\u3001\u521d\u59cb\u5316\n\u63cf\u8ff0\n\n\u4efb\u52a1 2\uff1a\u5b9e\u73b0 API\n### \u9a8c\u6536\u6807\u51c6\n- go test ./...\n\nTask 3: Wire UI\n### acceptance\n- npm test"
	tasks := ParseTaskListFromText(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %#v", tasks)
	}
	if tasks[0].Title != "\u521d\u59cb\u5316" || tasks[1].Title != "\u5b9e\u73b0 API" || tasks[2].Title != "Wire UI" {
		t.Fatalf("unexpected titles: %#v", []string{tasks[0].Title, tasks[1].Title, tasks[2].Title})
	}
	if len(tasks[1].AcceptanceCriteria) != 1 || tasks[1].AcceptanceCriteria[0] != "go test ./..." {
		t.Fatalf("expected unicode acceptance criteria, got %#v", tasks[1].AcceptanceCriteria)
	}
	if len(tasks[2].AcceptanceCriteria) != 1 || tasks[2].AcceptanceCriteria[0] != "npm test" {
		t.Fatalf("expected english acceptance criteria, got %#v", tasks[2].AcceptanceCriteria)
	}
}

func TestParseTaskListExtractsExplicitDependencies(t *testing.T) {
	input := "1. Scaffold project\nCreate build files.\n\n2. Implement game\nDepends on: 0\n\n3. Verify build\n- dependency: 0, 1"
	tasks := ParseTaskListFromText(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %#v", tasks)
	}
	if got := strings.Join(intSliceStrings(tasks[1].DependsOn), ","); got != "0" {
		t.Fatalf("task 1 deps = %q, want 0", got)
	}
	if got := strings.Join(intSliceStrings(tasks[2].DependsOn), ","); got != "0,1" {
		t.Fatalf("task 2 deps = %q, want 0,1", got)
	}
}

func TestParseTaskListTDependencyLabelsAreOneBased(t *testing.T) {
	input := "### T1: Scaffold project\nCreate build files.\n\n### T2: Implement game\nDepends on: T1\n\n### T3: Verify build\n- dependency: T1, T2"
	tasks := ParseTaskListFromText(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %#v", tasks)
	}
	if got := strings.Join(intSliceStrings(tasks[1].DependsOn), ","); got != "0" {
		t.Fatalf("task 1 deps = %q, want internal index 0", got)
	}
	if got := strings.Join(intSliceStrings(tasks[2].DependsOn), ","); got != "0,1" {
		t.Fatalf("task 2 deps = %q, want internal indexes 0,1", got)
	}
}

func TestParseTaskListNormalizesOneBasedDependencyLabels(t *testing.T) {
	input := "1. Scaffold project\nCreate build files.\n\n2. Implement game\nDepends on: 1\n\n3. Verify build\n- dependency: 1, 2"
	tasks := ParseTaskListFromText(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %#v", tasks)
	}
	if got := strings.Join(intSliceStrings(tasks[1].DependsOn), ","); got != "0" {
		t.Fatalf("task 1 deps = %q, want internal index 0", got)
	}
	if got := strings.Join(intSliceStrings(tasks[2].DependsOn), ","); got != "0,1" {
		t.Fatalf("task 2 deps = %q, want internal indexes 0,1", got)
	}
}

func TestParseTaskListAddsImplicitBootstrapDependency(t *testing.T) {
	input := "1. Create CMake project scaffold\nCreate CMakeLists.txt and src directory.\n\n2. Implement Snake\nAdd snake movement.\n\n3. Implement Food\nAdd food spawning."
	tasks := ParseTaskListFromText(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %#v", tasks)
	}
	if got := strings.Join(intSliceStrings(tasks[1].DependsOn), ","); got != "0" {
		t.Fatalf("task 1 deps = %q, want 0", got)
	}
	if got := strings.Join(intSliceStrings(tasks[2].DependsOn), ","); got != "0" {
		t.Fatalf("task 2 deps = %q, want 0", got)
	}
}

func TestParseTaskListAddsImplicitIntegrationDependencies(t *testing.T) {
	input := "1. Implement model\n\n2. Implement renderer\n\n3. Build and test integration\nCompile and verify all parts."
	tasks := ParseTaskListFromText(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %#v", tasks)
	}
	if got := strings.Join(intSliceStrings(tasks[2].DependsOn), ","); got != "0,1" {
		t.Fatalf("integration deps = %q, want 0,1", got)
	}
}

func TestParseTaskListPreservesExplicitDepsAndAddsImplicitForUnspecifiedTasks(t *testing.T) {
	input := "1. Create CMake project scaffold\nCreate CMakeLists.txt.\n\n2. Implement Snake\nDepends on: T0\n\n3. Build and test integration\nCompile and verify all parts."
	tasks := ParseTaskListFromText(input)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %#v", tasks)
	}
	if got := strings.Join(intSliceStrings(tasks[1].DependsOn), ","); got != "0" {
		t.Fatalf("explicit deps should be preserved, got %q", got)
	}
	if got := strings.Join(intSliceStrings(tasks[2].DependsOn), ","); got != "0,1" {
		t.Fatalf("implicit integration deps = %q, want 0,1", got)
	}
}

func intSliceStrings(values []int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, fmt.Sprintf("%d", value))
	}
	return out
}

func TestIsTaskHeaderUnicodePunctuation(t *testing.T) {
	cases := []string{"1\u3001\u521d\u59cb\u5316", "2\uff0e\u5b9e\u73b0", "\u4efb\u52a1 3\uff1a\u96c6\u6210", "Task 4: Verify"}
	for _, tc := range cases {
		if !isTaskHeader(tc) {
			t.Fatalf("expected %q to be a task header", tc)
		}
	}
	if isTaskHeader("\u4efb\u52a1\u5b8c\u6210\u540e\u8fd0\u884c\u6d4b\u8bd5") {
		t.Fatal("plain sentence should not be a task header")
	}
}
