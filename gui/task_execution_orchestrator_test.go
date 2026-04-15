package main

import (
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

	// Not active — should return empty
	if inj := o.BuildSystemInjection(); inj != "" {
		t.Errorf("inactive orchestrator should return empty injection, got %q", inj)
	}

	tasks := []*TaskItem{
		{Index: 0, Title: "Task A", Description: "Do A", Status: TaskExecPending},
		{Index: 1, Title: "Task B", Description: "Do B", Status: TaskExecPending},
	}
	o.Activate(tasks, "", "", "/p", "claude")

	inj := o.BuildSystemInjection()
	if !strings.Contains(inj, "任务 1/2") {
		t.Error("injection should contain current task number")
	}
	if !strings.Contains(inj, "create_session") {
		t.Error("injection for pending task should mention create_session")
	}
	if !strings.Contains(inj, "⏳ 1 剩余") {
		// 1 remaining because task A is pending (counted as remaining) but
		// we're on task A, so remaining = 2 (both pending)
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
	if !strings.Contains(summary, "✓") {
		t.Error("summary should contain passed checkmark ✓")
	}
	if !strings.Contains(summary, "⟳") {
		t.Error("summary should contain in-progress icon ⟳")
	}
	if !strings.Contains(summary, "T1: Task A ✓") {
		t.Error("summary should contain 'T1: Task A ✓'")
	}
	if !strings.Contains(summary, "T2: Task B ⟳") {
		t.Error("summary should contain 'T2: Task B ⟳'")
	}
	// Pending tasks have no suffix marker
	if !strings.Contains(summary, "T3: Task C\n") {
		t.Error("summary should contain 'T3: Task C' on its own line without marker")
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
	// Failed task should show ❌
	if !strings.Contains(prompt, "❌") {
		t.Error("prompt should show failed task icon")
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
