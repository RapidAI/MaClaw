package main

// TaskExecutionOrchestrator manages per-task execution during the coding
// workflow's Execution Phase (第六步). Instead of letting the LLM dump the
// entire project description into a single session, the orchestrator tracks
// which task is currently being executed and constructs focused prompts that
// include only the current task's description plus minimal context from
// confirmed requirements and design documents.

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

// TaskExecStatus represents the execution status of a single task.
type TaskExecStatus string

const (
	TaskExecPending    TaskExecStatus = "pending"
	TaskExecInProgress TaskExecStatus = "in_progress"
	TaskExecTesting    TaskExecStatus = "testing"
	TaskExecPassed     TaskExecStatus = "passed"
	TaskExecFailed     TaskExecStatus = "failed"
	TaskExecSkipped    TaskExecStatus = "skipped"
)

// TaskExecMode determines how a task is executed.
type TaskExecMode string

const (
	// TaskExecModeExternal delegates coding to an external tool via create_session.
	TaskExecModeExternal TaskExecMode = "external"
	// TaskExecModeDirect uses maclaw's own agent loop (bash/write_file/edit_file).
	TaskExecModeDirect TaskExecMode = "direct"
)

// TaskItem represents a single task extracted from the confirmed task list.
type TaskItem struct {
	Index              int
	Title              string
	Description        string
	Files              []string // expected files to create/modify
	AcceptanceCriteria []string // TDD test criteria
	DependsOn          []int    // indices of prerequisite tasks
	Status             TaskExecStatus
	RetryCount         int
	SessionID          string // session used for this task
	ErrorSummary       string
	ExecMode           TaskExecMode // resolved per-task at execution time
}

// ExternalToolChecker tests whether an external coding tool is available.
// Implemented by the host (GUI provides SessionPrecheck-based impl).
type ExternalToolChecker interface {
	IsExternalToolAvailable(toolName, projectPath string) bool
}

// TaskExecutionOrchestrator tracks the state of per-task execution during
// the coding workflow's Execution Phase.
type TaskExecutionOrchestrator struct {
	mu sync.Mutex

	// Active indicates the orchestrator is in control of execution.
	Active bool

	// Tasks is the ordered list of tasks to execute.
	Tasks []*TaskItem

	// CurrentIndex is the index of the task currently being executed.
	CurrentIndex int

	// RequirementsContext is a condensed summary of confirmed requirements.
	RequirementsContext string

	// DesignContext is a condensed summary of confirmed design.
	DesignContext string

	// MaxRetries is the maximum number of TDD retry attempts per task.
	MaxRetries int

	// ProjectPath for session creation.
	ProjectPath string

	// Tool name for session creation (e.g. "claude", "codex").
	Tool string

	// ExternalChecker tests external tool availability at runtime.
	// nil means always use direct mode.
	ExternalChecker ExternalToolChecker
}

// NewTaskExecutionOrchestrator creates an orchestrator with default settings.
func NewTaskExecutionOrchestrator() *TaskExecutionOrchestrator {
	return &TaskExecutionOrchestrator{
		MaxRetries: 3,
	}
}

// Activate parses the confirmed task list and enters execution mode.
// requirementsCtx and designCtx are condensed summaries (not full docs).
func (o *TaskExecutionOrchestrator) Activate(tasks []*TaskItem, requirementsCtx, designCtx, projectPath, tool string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.Active = true
	o.Tasks = tasks
	o.CurrentIndex = 0
	o.RequirementsContext = requirementsCtx
	o.DesignContext = designCtx
	o.ProjectPath = projectPath
	o.Tool = tool

	for _, t := range o.Tasks {
		t.Status = TaskExecPending
	}
	log.Printf("[task-orchestrator] activated with %d tasks, tool=%s, project=%s", len(tasks), tool, projectPath)
}

// Deactivate exits execution mode.
func (o *TaskExecutionOrchestrator) Deactivate() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.Active = false
	log.Printf("[task-orchestrator] deactivated")
}

// IsActive returns whether the orchestrator is currently managing execution.
func (o *TaskExecutionOrchestrator) IsActive() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.Active
}

// CurrentTask returns the task currently being executed, or nil if done.
func (o *TaskExecutionOrchestrator) CurrentTask() *TaskItem {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.Active || o.CurrentIndex >= len(o.Tasks) {
		return nil
	}
	return o.Tasks[o.CurrentIndex]
}

// AdvanceToNext moves to the next pending task. Returns false if all done.
func (o *TaskExecutionOrchestrator) AdvanceToNext() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.CurrentIndex++
	for o.CurrentIndex < len(o.Tasks) {
		if o.Tasks[o.CurrentIndex].Status == TaskExecPending {
			return true
		}
		o.CurrentIndex++
	}
	return false
}

// MarkCurrentStatus updates the current task's status.
func (o *TaskExecutionOrchestrator) MarkCurrentStatus(status TaskExecStatus, errSummary string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.CurrentIndex < len(o.Tasks) {
		o.Tasks[o.CurrentIndex].Status = status
		if errSummary != "" {
			o.Tasks[o.CurrentIndex].ErrorSummary = errSummary
		}
	}
}

// IncrementRetry increments the retry count for the current task.
// Returns true if retries are still available.
func (o *TaskExecutionOrchestrator) IncrementRetry() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.CurrentIndex >= len(o.Tasks) {
		return false
	}
	o.Tasks[o.CurrentIndex].RetryCount++
	return o.Tasks[o.CurrentIndex].RetryCount <= o.MaxRetries
}

// SetCurrentSessionID records which session is handling the current task.
func (o *TaskExecutionOrchestrator) SetCurrentSessionID(sessionID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.CurrentIndex < len(o.Tasks) {
		o.Tasks[o.CurrentIndex].SessionID = sessionID
	}
}

// AllDone returns true if all tasks have been executed (passed, failed, or skipped).
func (o *TaskExecutionOrchestrator) AllDone() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, t := range o.Tasks {
		if t.Status == TaskExecPending || t.Status == TaskExecInProgress || t.Status == TaskExecTesting {
			return false
		}
	}
	return true
}

// TaskCount returns the total number of tasks. Thread-safe.
func (o *TaskExecutionOrchestrator) TaskCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.Tasks)
}

// BuildTaskPrompt constructs the focused prompt for the current task that
// should be sent to the coding session via send_and_observe. It includes
// only the current task's description plus minimal context.
func (o *TaskExecutionOrchestrator) BuildTaskPrompt() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.CurrentIndex >= len(o.Tasks) {
		return ""
	}
	task := o.Tasks[o.CurrentIndex]

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## 任务 %d/%d: %s\n\n", task.Index+1, len(o.Tasks), task.Title))
	b.WriteString(task.Description)
	b.WriteString("\n")

	if len(task.Files) > 0 {
		b.WriteString("\n### 涉及文件\n")
		for _, f := range task.Files {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
	}

	if len(task.AcceptanceCriteria) > 0 {
		b.WriteString("\n### 验收标准（完成后必须通过这些测试）\n")
		for i, ac := range task.AcceptanceCriteria {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, ac))
		}
	}

	// Add dependency context: list what previous tasks produced
	if len(task.DependsOn) > 0 {
		b.WriteString("\n### 前置任务产出\n")
		for _, depIdx := range task.DependsOn {
			if depIdx >= 0 && depIdx < len(o.Tasks) {
				dep := o.Tasks[depIdx]
				b.WriteString(fmt.Sprintf("- 任务 %d「%s」: %s\n", dep.Index+1, dep.Title, dep.Status))
				if len(dep.Files) > 0 {
					b.WriteString(fmt.Sprintf("  产出文件: %s\n", strings.Join(dep.Files, ", ")))
				}
			}
		}
	}

	// Append condensed requirements/design context (not the full docs)
	if o.RequirementsContext != "" {
		b.WriteString("\n### 需求上下文（摘要）\n")
		b.WriteString(truncateRunes(o.RequirementsContext, 500))
		b.WriteString("\n")
	}
	if o.DesignContext != "" {
		b.WriteString("\n### 设计上下文（摘要）\n")
		b.WriteString(truncateRunes(o.DesignContext, 500))
		b.WriteString("\n")
	}

	return b.String()
}

// BuildTDDPrompt constructs the prompt to run TDD tests for the current task.
func (o *TaskExecutionOrchestrator) BuildTDDPrompt() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.CurrentIndex >= len(o.Tasks) {
		return ""
	}
	task := o.Tasks[o.CurrentIndex]

	var b strings.Builder
	b.WriteString(fmt.Sprintf("任务 %d「%s」的代码已完成。\n\n", task.Index+1, task.Title))
	b.WriteString("请运行以下验收测试来验证实现是否正确：\n\n")

	if len(task.AcceptanceCriteria) > 0 {
		for i, ac := range task.AcceptanceCriteria {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, ac))
		}
	} else {
		b.WriteString("运行项目的测试套件，确保新增代码不破坏现有功能。\n")
	}

	b.WriteString("\n如果测试失败，请修复代码并重新运行测试。")
	return b.String()
}

// BuildFixPrompt constructs the prompt to fix a failed test.
func (o *TaskExecutionOrchestrator) BuildFixPrompt(testOutput string) string {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.CurrentIndex >= len(o.Tasks) {
		return ""
	}
	task := o.Tasks[o.CurrentIndex]

	var b strings.Builder
	b.WriteString(fmt.Sprintf("任务 %d「%s」的测试失败（第 %d/%d 次重试）。\n\n",
		task.Index+1, task.Title, task.RetryCount, o.MaxRetries))
	b.WriteString("测试输出：\n")
	b.WriteString(truncateRunes(testOutput, 1000))
	b.WriteString("\n\n请分析失败原因，修复代码，然后重新运行测试。")
	return b.String()
}

// BuildIntegrationPrompt constructs the prompt for the integration phase
// that runs after all individual tasks are complete. It instructs the coding
// tool to wire all modules together, fix cross-module issues, and ensure
// the project compiles and runs as a whole.
func (o *TaskExecutionOrchestrator) BuildIntegrationPrompt() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	var b strings.Builder
	b.WriteString("## 集成联调阶段\n\n")
	b.WriteString("所有子任务已完成，现在需要将各模块集成为一个可运行的整体。\n\n")

	// List all completed tasks and their output files
	b.WriteString("### 已完成的子任务及产出文件\n")
	var failedNames []string
	for _, t := range o.Tasks {
		icon := "✅"
		if t.Status == TaskExecFailed {
			icon = "❌"
			failedNames = append(failedNames, fmt.Sprintf("任务 %d「%s」", t.Index+1, t.Title))
		} else if t.Status == TaskExecSkipped {
			icon = "⏭️"
		}
		b.WriteString(fmt.Sprintf("%s 任务 %d: %s\n", icon, t.Index+1, t.Title))
		if len(t.Files) > 0 {
			b.WriteString(fmt.Sprintf("   文件: %s\n", strings.Join(t.Files, ", ")))
		}
	}

	// Warn about failed tasks whose files may be incomplete
	if len(failedNames) > 0 {
		b.WriteString("\n⚠️ 以下任务未通过测试，其产出文件可能不完整或有错误：\n")
		for _, name := range failedNames {
			b.WriteString(fmt.Sprintf("- %s\n", name))
		}
		b.WriteString("集成时请检查这些模块，必要时补全或修复。\n")
	}

	b.WriteString("\n### 集成要求\n")
	b.WriteString("请按以下顺序完成集成：\n")
	b.WriteString("1. 检查所有模块间的 import/依赖关系，补全缺失的引用\n")
	b.WriteString("2. 确保 main 入口文件正确引用并初始化所有模块\n")
	b.WriteString("3. 检查模块间的接口是否匹配（函数签名、数据类型、参数顺序）\n")
	b.WriteString("4. 补全任何缺失的胶水代码（路由注册、依赖注入、配置加载等）\n")
	b.WriteString("5. 运行编译/构建命令，修复所有编译错误\n")
	b.WriteString("6. 运行项目，确保基本功能可用\n")

	// Design context is more important than requirements for integration —
	// it contains architecture, module boundaries, and interface definitions.
	if o.DesignContext != "" {
		b.WriteString("\n### 设计上下文（摘要）\n")
		b.WriteString(truncateRunes(o.DesignContext, 500))
		b.WriteString("\n")
	}
	if o.RequirementsContext != "" {
		b.WriteString("\n### 需求上下文（摘要）\n")
		b.WriteString(truncateRunes(o.RequirementsContext, 300))
		b.WriteString("\n")
	}

	return b.String()
}

// ResolveExecutionMode determines the execution mode for the current task.
// Called at runtime (not at Activate time) so it reflects the current state
// of external tool availability. The result is cached on the TaskItem.
func (o *TaskExecutionOrchestrator) ResolveExecutionMode() TaskExecMode {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.CurrentIndex >= len(o.Tasks) {
		return TaskExecModeDirect
	}
	task := o.Tasks[o.CurrentIndex]
	// Return cached mode if already resolved. Re-resolution only happens
	// when AdvanceToNext moves to a new task (ExecMode is "" on fresh tasks).
	if task.ExecMode != "" {
		return task.ExecMode
	}
	mode := o.resolveModeLocked()
	task.ExecMode = mode
	return mode
}

// resolveModeLocked determines the mode without locking (caller holds mu).
func (o *TaskExecutionOrchestrator) resolveModeLocked() TaskExecMode {
	if o.Tool == "" {
		return TaskExecModeDirect
	}
	if o.ExternalChecker == nil {
		return TaskExecModeDirect
	}
	if o.ExternalChecker.IsExternalToolAvailable(o.Tool, o.ProjectPath) {
		return TaskExecModeExternal
	}
	return TaskExecModeDirect
}

// DegradeCurrentToDirectMode switches the current task from external to
// direct mode. Called when an external tool fails with a non-code error
// (rate limit, connection failure, tool crash). Returns true if degraded.
func (o *TaskExecutionOrchestrator) DegradeCurrentToDirectMode() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.CurrentIndex >= len(o.Tasks) {
		return false
	}
	task := o.Tasks[o.CurrentIndex]
	if task.ExecMode != TaskExecModeExternal {
		return false
	}
	task.ExecMode = TaskExecModeDirect
	log.Printf("[task-orchestrator] degraded task %d to direct mode", task.Index+1)
	return true
}

// CurrentExecutionMode returns the resolved mode for the current task.
func (o *TaskExecutionOrchestrator) CurrentExecutionMode() TaskExecMode {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.CurrentIndex >= len(o.Tasks) {
		return TaskExecModeDirect
	}
	if o.Tasks[o.CurrentIndex].ExecMode == "" {
		return TaskExecModeDirect
	}
	return o.Tasks[o.CurrentIndex].ExecMode
}

// BuildSystemInjection returns a system message to inject into the conversation
// at the start of each iteration, reminding the LLM which task to focus on.
func (o *TaskExecutionOrchestrator) BuildSystemInjection() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.Active || o.CurrentIndex >= len(o.Tasks) {
		return ""
	}

	var b strings.Builder
	b.WriteString(o.buildTaskContextLocked())
	b.WriteString(o.buildExecutionGuideLocked())
	return b.String()
}

// buildTaskContextLocked writes pure task context: current task, task list,
// progress stats. No tool-specific instructions. Caller holds mu.
func (o *TaskExecutionOrchestrator) buildTaskContextLocked() string {
	task := o.Tasks[o.CurrentIndex]

	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔧 [任务执行调度器] 当前正在执行任务 %d/%d: 「%s」\n", task.Index+1, len(o.Tasks), task.Title))

	// Full task list with status
	b.WriteString("\n📋 任务清单：\n")
	for _, t := range o.Tasks {
		switch t.Status {
		case TaskExecPassed:
			b.WriteString(fmt.Sprintf("T%d: %s ✓\n", t.Index+1, t.Title))
		case TaskExecFailed:
			b.WriteString(fmt.Sprintf("T%d: %s ✗", t.Index+1, t.Title))
			if t.ErrorSummary != "" {
				b.WriteString(fmt.Sprintf(" — %s", t.ErrorSummary))
			}
			b.WriteString("\n")
		case TaskExecSkipped:
			b.WriteString(fmt.Sprintf("T%d: %s (跳过)\n", t.Index+1, t.Title))
		case TaskExecInProgress, TaskExecTesting:
			b.WriteString(fmt.Sprintf("T%d: %s ⟳\n", t.Index+1, t.Title))
		default:
			b.WriteString(fmt.Sprintf("T%d: %s\n", t.Index+1, t.Title))
		}
	}

	passed, failed, remaining := o.countStatusLocked()
	b.WriteString(fmt.Sprintf("\n📊 进度：✅ %d 通过 | ❌ %d 失败 | ⏳ %d 剩余\n", passed, failed, remaining))

	b.WriteString("\n📝 向用户汇报进度时，请严格按以下格式输出（每个任务独占一行）：\n")
	b.WriteString("T1: 任务描述 ✓\n")
	b.WriteString("T2: 任务描述 ⟳\n")
	b.WriteString("T3: 任务描述\n")
	b.WriteString("已完成的任务后面加 ✓，正在执行的加 ⟳，未开始的不加标记。不要把多个任务写在同一行。\n")

	return b.String()
}

// buildExecutionGuideLocked writes tool-specific execution instructions
// based on the current task's execution mode. Caller holds mu.
func (o *TaskExecutionOrchestrator) buildExecutionGuideLocked() string {
	task := o.Tasks[o.CurrentIndex]
	mode := task.ExecMode
	if mode == "" {
		mode = TaskExecModeDirect // default if not yet resolved
	}

	var b strings.Builder

	if mode == TaskExecModeDirect {
		// Direct coding mode: maclaw's own agent loop writes code.
		switch task.Status {
		case TaskExecPending:
			b.WriteString("\n📌 执行模式：直接编码\n")
			b.WriteString("操作步骤：\n")
			b.WriteString("1. 先用 read_file 理解涉及文件的现有代码结构\n")
			b.WriteString("2. 用 write_file 创建新文件，用 edit_file 修改现有文件\n")
			b.WriteString("3. 每个文件修改后用 bash 编译/lint 检查\n")
			b.WriteString("4. 全部完成后用 bash 运行验收标准中的测试\n")
			b.WriteString("⚠️ 优先用 edit_file 做增量修改，避免 write_file 全文覆盖已有文件。\n")
			b.WriteString("⚠️ 单次 write_file 内容不超过 200 行，超过时分多次写入。\n")
		case TaskExecInProgress:
			b.WriteString("📌 继续用 write_file/edit_file 完成当前任务的编码。\n")
		case TaskExecTesting:
			b.WriteString("📌 用 bash 运行验收测试，验证任务完成质量。\n")
		}
	} else {
		// External tool mode: delegate to create_session → send_and_observe.
		switch task.Status {
		case TaskExecPending:
			b.WriteString("📌 操作：调用 create_session 创建编程会话，然后用 send_and_observe 发送本任务的编程指令。\n")
			b.WriteString("⚠️ 只发送当前任务的描述，不要发送整个项目的需求。系统会自动构造包含上下文的任务 prompt。\n")
		case TaskExecInProgress:
			b.WriteString("📌 操作：用 get_session_output 检查编程工具的进度。\n")
		case TaskExecTesting:
			b.WriteString("📌 操作：用 send_and_observe 发送 TDD 测试指令，验证任务完成质量。\n")
		}
	}

	return b.String()
}

// ProgressSummary returns a user-facing progress message.
func (o *TaskExecutionOrchestrator) ProgressSummary() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	var b strings.Builder
	for _, t := range o.Tasks {
		b.WriteString(fmt.Sprintf("T%d: %s", t.Index+1, t.Title))
		switch t.Status {
		case TaskExecPassed:
			b.WriteString(" ✓")
		case TaskExecFailed:
			b.WriteString(" ✗")
			if t.ErrorSummary != "" {
				b.WriteString(fmt.Sprintf(" — %s", t.ErrorSummary))
			}
		case TaskExecSkipped:
			b.WriteString(" (跳过)")
		case TaskExecInProgress, TaskExecTesting:
			b.WriteString(" ⟳")
		}
		b.WriteString("\n")
	}

	passed, failed, _ := o.countStatusLocked()
	b.WriteString(fmt.Sprintf("\n总计: %d/%d 通过", passed, len(o.Tasks)))
	if failed > 0 {
		b.WriteString(fmt.Sprintf(", %d 失败", failed))
	}
	return b.String()
}

// FinalReport generates the verification report after all tasks are done.
func (o *TaskExecutionOrchestrator) FinalReport() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	passed, failed, skipped := 0, 0, 0
	var failedTasks []string
	for _, t := range o.Tasks {
		switch t.Status {
		case TaskExecPassed:
			passed++
		case TaskExecFailed:
			failed++
			failedTasks = append(failedTasks, fmt.Sprintf("- 任务 %d「%s」: %s", t.Index+1, t.Title, t.ErrorSummary))
		case TaskExecSkipped:
			skipped++
		}
	}

	var b strings.Builder
	b.WriteString("## 编码任务执行报告\n\n")
	b.WriteString(fmt.Sprintf("- 总任务数: %d\n", len(o.Tasks)))
	b.WriteString(fmt.Sprintf("- 成功: %d ✅\n", passed))
	b.WriteString(fmt.Sprintf("- 失败: %d ❌\n", failed))
	if skipped > 0 {
		b.WriteString(fmt.Sprintf("- 跳过: %d ⏭️\n", skipped))
	}

	if len(failedTasks) > 0 {
		b.WriteString("\n### 失败任务详情\n")
		for _, ft := range failedTasks {
			b.WriteString(ft + "\n")
		}
		b.WriteString("\n建议：可以针对失败的任务单独重试，或检查错误信息后手动修复。\n")
	}

	return b.String()
}

// countStatusLocked counts task statuses. Must be called with mu held.
func (o *TaskExecutionOrchestrator) countStatusLocked() (passed, failed, remaining int) {
	for _, t := range o.Tasks {
		switch t.Status {
		case TaskExecPassed:
			passed++
		case TaskExecFailed:
			failed++
		case TaskExecSkipped:
			// Skipped tasks don't count as failed or remaining.
		default:
			remaining++
		}
	}
	return
}

// ParseTaskListFromText extracts tasks from a markdown task list document.
// Expected format: numbered list with task descriptions.
func ParseTaskListFromText(text string) []*TaskItem {
	lines := strings.Split(text, "\n")
	var tasks []*TaskItem
	var current *TaskItem
	inCriteria := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Detect numbered task headers: "1. xxx", "1) xxx", "任务 1: xxx"
		if isTaskHeader(trimmed) {
			if current != nil {
				tasks = append(tasks, current)
			}
			title := extractTaskTitle(trimmed)
			current = &TaskItem{
				Index:  len(tasks),
				Title:  title,
				Status: TaskExecPending,
			}
			inCriteria = false
			continue
		}

		if current == nil {
			continue
		}

		// Detect acceptance criteria section
		lowerTrimmed := strings.ToLower(trimmed)
		if strings.Contains(lowerTrimmed, "验收") || strings.Contains(lowerTrimmed, "测试") ||
			strings.Contains(lowerTrimmed, "acceptance") || strings.Contains(lowerTrimmed, "test") {
			if strings.HasPrefix(trimmed, "#") || strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "：") {
				inCriteria = true
				continue
			}
		}

		// Detect file references
		if strings.Contains(lowerTrimmed, "文件") || strings.Contains(lowerTrimmed, "file") {
			if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*") {
				file := strings.TrimLeft(trimmed, "-* ")
				if strings.Contains(file, ".") && !strings.Contains(file, " ") {
					current.Files = append(current.Files, file)
					continue
				}
			}
		}

		if inCriteria {
			criterion := strings.TrimLeft(trimmed, "-*0123456789.) ")
			if criterion != "" {
				current.AcceptanceCriteria = append(current.AcceptanceCriteria, criterion)
			}
		} else {
			// Append to description
			if current.Description != "" {
				current.Description += "\n"
			}
			current.Description += trimmed
		}
	}

	if current != nil {
		tasks = append(tasks, current)
	}

	return tasks
}

// isTaskHeader checks if a line looks like a numbered task header.
func isTaskHeader(line string) bool {
	// "1. xxx", "1) xxx", "1、xxx"
	if len(line) >= 2 && line[0] >= '0' && line[0] <= '9' {
		for i := 1; i < len(line) && i < 4; i++ {
			if line[i] == '.' || line[i] == ')' {
				return true
			}
			if line[i] >= '0' && line[i] <= '9' {
				continue
			}
			break
		}
		// Check for Chinese period "、"
		if strings.Contains(line[:min(6, len(line))], "、") {
			return true
		}
	}
	// "任务 1:", "任务1:", "Task 1:", "Task1:"
	// Must have a digit after the prefix to avoid matching "任务完成后..." in descriptions.
	lower := strings.ToLower(line)
	for _, prefix := range []string{"任务", "task"} {
		if strings.HasPrefix(lower, prefix) {
			rest := strings.TrimSpace(line[len(prefix):])
			if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
				return true
			}
		}
	}
	return false
}

// extractTaskTitle extracts the title from a task header line.
func extractTaskTitle(line string) string {
	// Remove leading number and punctuation
	for i, r := range line {
		if r == '.' || r == ')' || r == ':' || r == '：' {
			rest := strings.TrimSpace(line[i+utf8RuneWidth(r):])
			if rest != "" {
				return rest
			}
		}
		// Handle "、"
		if r == '、' {
			rest := strings.TrimSpace(line[i+utf8RuneWidth(r):])
			if rest != "" {
				return rest
			}
		}
	}
	// Remove "任务 N:" or "Task N:" prefix
	lower := strings.ToLower(line)
	if strings.HasPrefix(lower, "任务") || strings.HasPrefix(lower, "task") {
		idx := strings.IndexAny(line, ":：")
		if idx >= 0 {
			_, size := utf8DecodeRuneInString(line[idx:])
			return strings.TrimSpace(line[idx+size:])
		}
	}
	return line
}

// utf8RuneWidth returns the byte width of a rune in UTF-8 encoding.
func utf8RuneWidth(r rune) int {
	if r < 0x80 {
		return 1
	}
	if r < 0x800 {
		return 2
	}
	if r < 0x10000 {
		return 3
	}
	return 4
}

// utf8DecodeRuneInString decodes the first rune in s and returns it with its byte width.
func utf8DecodeRuneInString(s string) (rune, int) {
	for i, r := range s {
		_ = i
		return r, utf8RuneWidth(r)
	}
	return 0, 0
}
