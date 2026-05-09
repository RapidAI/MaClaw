package main

// coding_subagent_orchestrator.go connects the TaskExecutionOrchestrator
// with the CodingSubAgent. When the orchestrator is active and the current
// task's execution mode is "direct" (no external coding tool), the main
// agent loop delegates the task to a SubAgent instead of trying to code
// in the polluted main context.
//
// This is the bridge between:
// - TaskExecutionOrchestrator (task scheduling, progress tracking)
// - CodingSubAgent (clean-context coding execution)
// - Main agent loop (workflow management, IM interaction)

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// SubAgentTaskRunner executes a single task from the orchestrator using
// a CodingSubAgent. It returns a structured result that the main agent
// loop can use to update the orchestrator and report progress.
type SubAgentTaskRunner struct {
	handler      *IMMessageHandler
	cfg          corelib.MaclawLLMConfig
	httpClient   *http.Client
	orchestrator *TaskExecutionOrchestrator
	loopCtx      *LoopContext // propagates cancellation from main agent loop
}

// NewSubAgentTaskRunner creates a runner bound to the orchestrator.
func NewSubAgentTaskRunner(
	handler *IMMessageHandler,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	orchestrator *TaskExecutionOrchestrator,
	loopCtx *LoopContext,
) *SubAgentTaskRunner {
	return &SubAgentTaskRunner{
		handler:      handler,
		cfg:          cfg,
		httpClient:   httpClient,
		orchestrator: orchestrator,
		loopCtx:      loopCtx,
	}
}

// runTaskWithSubAgent is overridden in tests to simulate long-running SubAgent outcomes.
var runTaskWithSubAgent = RunTaskWithSubAgent

func emitCodingSubAgentProgress(onProgress func(string), message string) {
	if onProgress == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	onProgress(message)
}

// RunCurrentTask executes the orchestrator's current task via SubAgent.
// Returns a human-readable summary for the main agent to relay to the user.
func (r *SubAgentTaskRunner) RunCurrentTask(
	onToken llm.TokenCallback,
	onProgress func(string),
) (summary string, passed bool) {
	if r == nil || r.orchestrator == nil {
		return "SubAgent runner is not attached to an active task orchestrator", false
	}

	task, runID := r.orchestrator.CurrentTaskHandle()
	if task == nil {
		return "没有待执行的任务", false
	}

	// Collect previous task outputs for context.
	prevOutputs := r.collectPreviousOutputs()
	taskTitle := compactSubAgentTaskTitle(task.Title)
	eventRunID := fmt.Sprint(runID)
	turnCtx := newCodingTurnContext(eventRunID, task, r.orchestrator.ProjectPath)
	turnProgress := turnCtx.WrapProgress(onProgress)

	log.Printf("[subagent-runner] delegating T%d to SubAgent: %s", task.Index, taskTitle)
	turnCtx.Emit(onProgress, turnCtx.TaskEvent("starting", task, taskTitle))

	// Mark task as in-progress.
	r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecInProgress, "")

	result, panicErr := r.runTaskWithRecover(task, prevOutputs, onToken, turnProgress)
	if panicErr != "" {
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("failed", task, taskTitle))
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecFailed, panicErr) {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		return fmt.Sprintf("T%d: %s - failed\nError: %s", task.Index, taskTitle, panicErr), false
	}
	if result == nil {
		errSummary := "coding SubAgent returned no result"
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("failed", task, taskTitle))
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecFailed, errSummary) {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		return fmt.Sprintf("T%d: %s - failed\nError: %s", task.Index, taskTitle, errSummary), false
	}

	// Update orchestrator based on result.
	resultSummary := compactSubAgentReportSummary(result.Summary)
	resultStatus, resultError := normalizeSubAgentResultStatus(result)
	r.orchestrator.RecordTaskActualArtifactsForRun(task, runID, result.FilesModified, result.FilesCreated)
	switch resultStatus {
	case TaskExecPassed:
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecPassed, "") {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		log.Printf("[subagent-runner] T%d passed (%d iterations, %d tool calls, %d files, %d created)", task.Index, result.Iterations, result.ToolCalls, len(result.FilesModified), len(result.FilesCreated))
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("completed", task, taskTitle))
		return fmt.Sprintf("✅ T%d: %s — 完成\n%s", task.Index, taskTitle, resultSummary), true

	case TaskExecFailed:
		retryCount, canRetry := r.orchestrator.IncrementTaskRetryForRun(task, runID)
		if retryCount == 0 && !canRetry {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		if canRetry {
			// Retry available — will be re-executed on next call.
			log.Printf("[subagent-runner] T%d failed (retry %d/%d): %s", task.Index, retryCount, r.orchestrator.MaxRetries, resultError)
			event := turnCtx.TaskEvent("retrying", task, taskTitle)
			event.Detail = fmt.Sprintf("%d/%d", retryCount, r.orchestrator.MaxRetries)
			turnCtx.Emit(onProgress, event)
			return fmt.Sprintf("⚠️ T%d: %s — 失败，将重试 (%d/%d)\n错误: %s",
				task.Index, taskTitle, retryCount, r.orchestrator.MaxRetries, resultError), false
		}
		// Max retries exhausted.
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecFailed, resultError) {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		log.Printf("[subagent-runner] T%d failed permanently: %s", task.Index, resultError)
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("failed", task, taskTitle))
		return fmt.Sprintf("❌ T%d: %s — 失败（已重试 %d 次）\n错误: %s",
			task.Index, taskTitle, r.orchestrator.MaxRetries, resultError), false

	default:
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecSkipped, resultError) {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("skipped", task, taskTitle))
		return fmt.Sprintf("⏭️ T%d: %s — 跳过\n%s", task.Index, taskTitle, resultError), false
	}
}

func normalizeSubAgentResultStatus(result *CodingSubAgentResult) (TaskExecStatus, string) {
	if result == nil {
		return TaskExecFailed, "coding SubAgent returned no result"
	}
	errSummary := compactSubAgentErrorSummary(result.Error)
	switch result.Status {
	case TaskExecPassed:
		return TaskExecPassed, errSummary
	case TaskExecFailed:
		if errSummary == "" {
			errSummary = "coding SubAgent reported failure without an error summary"
		}
		return TaskExecFailed, errSummary
	case TaskExecSkipped:
		if errSummary == "" {
			errSummary = "coding SubAgent skipped the task without a reason"
		}
		return TaskExecSkipped, errSummary
	default:
		return TaskExecFailed, compactSubAgentErrorSummary(fmt.Sprintf("coding SubAgent returned unknown status %q", result.Status))
	}
}

func (r *SubAgentTaskRunner) runTaskWithRecover(
	task *TaskItem,
	prevOutputs []string,
	onToken llm.TokenCallback,
	onProgress func(string),
) (result *CodingSubAgentResult, panicErr string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr = compactSubAgentErrorSummary(fmt.Sprintf("coding SubAgent panicked: %v", recovered))
			result = nil
		}
	}()
	return runTaskWithSubAgent(
		r.handler,
		r.cfg,
		r.httpClient,
		task,
		r.orchestrator.ProjectPath,
		r.orchestrator.RequirementsContext,
		r.orchestrator.DesignContext,
		prevOutputs,
		r.loopCtx,
		onToken,
		onProgress,
	), ""
}

func staleSubAgentRunSummary(task *TaskItem, taskTitle string) string {
	index := -1
	if task != nil {
		index = task.Index
	}
	return fmt.Sprintf("⏭️ T%d: %s — 结果已忽略：任务执行轮次已过期", index, taskTitle)
}

// RunAllTasks executes all tasks sequentially via SubAgent.
// Returns the final report.
func (r *SubAgentTaskRunner) RunAllTasks(
	onToken llm.TokenCallback,
	onProgress func(string),
) string {
	if r == nil || r.orchestrator == nil {
		return "SubAgent runner is not attached to an active task orchestrator"
	}

	var reports []string
	attempts := 0
	maxAttempts := r.maxRunAllTaskAttempts()

	for !r.orchestrator.AllDone() {
		attempts++
		if attempts > maxAttempts {
			reports = append(reports, fmt.Sprintf("SubAgent execution stopped after %d attempts without completing all tasks", maxAttempts))
			break
		}

		// Check cancellation between tasks — don't start a new task if
		// the user already clicked cancel.
		if r.loopCtx != nil && r.loopCtx.IsCancelled() {
			reports = append(reports, "⏹️ 用户取消，剩余任务未执行")
			break
		}

		task, runID := r.orchestrator.CurrentTaskHandle()
		if task == nil {
			break
		}

		summary, passed := r.RunCurrentTask(onToken, onProgress)
		reports = append(reports, summary)

		if passed || r.orchestrator.IsTaskTerminalForRun(task, runID) {
			// Move to next task (AdvanceToNext handles the index).
			if !r.orchestrator.AdvanceToNext() {
				break // no more tasks
			}
		}
		// If not passed and retry available, RunCurrentTask will be called
		// again for the same task on the next iteration.
	}

	// Generate final report.
	var b strings.Builder
	b.WriteString("## 编码执行报告\n\n")
	appendSubAgentRunReports(&b, reports)
	b.WriteString("---\n")
	b.WriteString(r.orchestrator.ProgressSummary())
	b.WriteString("\n\n---\n")
	appendSubAgentExecutionStats(&b, r.orchestrator.SnapshotTasks())

	return b.String()
}

func (r *SubAgentTaskRunner) maxRunAllTaskAttempts() int {
	if r == nil || r.orchestrator == nil {
		return 1
	}
	taskCount := r.orchestrator.TaskCount()
	if taskCount < 1 {
		taskCount = 1
	}
	retries := r.orchestrator.MaxRetries
	if retries < 0 {
		retries = 0
	}
	return taskCount*(retries+2) + 5
}

func appendSubAgentExecutionStats(b *strings.Builder, tasks []*TaskItem) {
	passed, failed, skipped := 0, 0, 0
	var modified []string
	var created []string
	var plannedOnly []string
	for _, task := range tasks {
		if task == nil {
			continue
		}
		switch task.Status {
		case TaskExecPassed:
			passed++
		case TaskExecFailed:
			failed++
		case TaskExecSkipped:
			skipped++
		}
		if len(task.ActualFiles) > 0 {
			modified = append(modified, task.ActualFiles...)
		} else if task.Status == TaskExecPassed {
			plannedOnly = append(plannedOnly, task.Files...)
		}
		created = append(created, task.ActualCreatedFiles...)
	}
	modified = uniqueSortedSubAgentStrings(modified)
	created = uniqueSortedSubAgentStrings(created)
	plannedOnly = uniqueSortedSubAgentStrings(plannedOnly)

	b.WriteString("## 执行统计\n\n")
	b.WriteString(fmt.Sprintf("- 任务结果: %d passed / %d failed / %d skipped\n", passed, failed, skipped))
	b.WriteString(fmt.Sprintf("- 实际修改文件: %d\n", len(modified)))
	if len(modified) > 0 {
		b.WriteString(fmt.Sprintf("  %s\n", compactSubAgentFileList(modified, codingSubAgentFileChangeSummaryMax)))
	}
	b.WriteString(fmt.Sprintf("- 新建文件: %d\n", len(created)))
	if len(created) > 0 {
		b.WriteString(fmt.Sprintf("  %s\n", compactSubAgentFileList(created, codingSubAgentFileChangeSummaryMax)))
	}
	if len(plannedOnly) > 0 {
		b.WriteString(fmt.Sprintf("- 仅有计划产物、未追踪实际修改: %d\n", len(plannedOnly)))
	}
}

func appendSubAgentRunReports(b *strings.Builder, reports []string) {
	shown := len(reports)
	if shown > codingSubAgentRunReportMaxItems {
		shown = codingSubAgentRunReportMaxItems
	}
	for _, taskReport := range reports[:shown] {
		b.WriteString(compactSubAgentRunReport(taskReport))
		b.WriteString("\n\n")
	}
	if remaining := len(reports) - shown; remaining > 0 {
		b.WriteString(fmt.Sprintf("... 还有 %d 条任务报告未展开\n\n", remaining))
	}
}

// collectPreviousOutputs gathers file lists from completed tasks.
// Prefers ActualFiles (what the SubAgent actually modified) over Files
// (what the task declaration expected). Created files are labeled so
// subsequent tasks can rely on newly introduced modules/tests explicitly.
func (r *SubAgentTaskRunner) collectPreviousOutputs() []string {
	if r == nil || r.orchestrator == nil {
		return nil
	}
	var outputs []string
	for _, t := range r.orchestrator.SnapshotTasks() {
		if t == nil || t.Status != TaskExecPassed {
			continue
		}
		outputs = append(outputs, previousTaskFileOutputs(t)...)
	}
	return recentSubAgentOutputs(outputs, codingSubAgentPrevOutputsMax)
}

func recentSubAgentOutputs(outputs []string, maxItems int) []string {
	outputs = uniqueSubAgentStrings(outputs)
	if maxItems <= 0 || len(outputs) <= maxItems {
		return outputs
	}
	return outputs[len(outputs)-maxItems:]
}

func previousTaskFileOutputs(t *TaskItem) []string {
	if t == nil {
		return nil
	}
	files := uniqueSortedSubAgentStrings(t.ActualFiles)
	source := "modified"
	if len(files) == 0 {
		files = uniqueSortedSubAgentStrings(t.Files)
		source = "planned"
	}
	created := make(map[string]bool, len(t.ActualCreatedFiles))
	for _, f := range uniqueSortedSubAgentStrings(t.ActualCreatedFiles) {
		created[f] = true
	}
	outputs := make([]string, 0, len(files))
	title := compactSubAgentTaskTitle(t.Title)
	for _, f := range files {
		kind := source
		if created[f] {
			kind = "created"
		}
		outputs = append(outputs, fmt.Sprintf("%s (%s by T%d: %s ✅)", compactSubAgentPathText(f), kind, t.Index, title))
	}
	return outputs
}

func compactSubAgentTaskTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "未命名任务"
	}
	return truncateRunesForSubAgent(title, codingSubAgentTaskTitleMaxRunes)
}

func compactSubAgentReportSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return ""
	}
	return truncateRunesForSubAgent(summary, codingSubAgentReportSummaryMaxRunes)
}

func compactSubAgentRunReport(report string) string {
	report = strings.TrimSpace(report)
	if report == "" {
		return ""
	}
	return truncateRunesForSubAgent(report, codingSubAgentRunReportMaxRunes)
}

// ShouldUseSubAgent determines whether the current task should be delegated
// to a SubAgent (clean context) or executed in the main agent loop.
//
// Decision criteria:
// - Orchestrator must be active
// - Current task's execution mode must be "direct" (not external tool)
// - External tool mode still uses create_session (existing path)
func ShouldUseSubAgent(orchestrator *TaskExecutionOrchestrator) bool {
	if orchestrator == nil || !orchestrator.IsActive() {
		return false
	}
	task, runID := orchestrator.CurrentTaskHandle()
	mode, ok := orchestrator.ResolveExecutionModeForTaskRun(task, runID)
	return ok && mode == TaskExecModeDirect
}
