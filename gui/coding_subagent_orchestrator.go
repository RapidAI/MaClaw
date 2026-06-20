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
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// SubAgentTaskRunner executes a single task from the orchestrator using
// a CodingSubAgent. It returns a structured result that the main agent
// loop can use to update the orchestrator and report progress.
type SubAgentTaskRunner struct {
	handler       *IMMessageHandler
	cfg           corelib.MaclawLLMConfig
	httpClient    *http.Client
	orchestrator  *TaskExecutionOrchestrator
	loopCtx       *LoopContext // propagates cancellation from main agent loop
	codeSessionID string
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
		handler:       handler,
		cfg:           cfg,
		httpClient:    httpClient,
		orchestrator:  orchestrator,
		loopCtx:       loopCtx,
		codeSessionID: "subagent-workflow",
	}
}

type subAgentTaskFunc func(
	handler *IMMessageHandler,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	task *TaskItem,
	projectPath, reqCtx, designCtx string,
	prevOutputs []string,
	loopCtx *LoopContext,
	onToken func(string),
	onProgress func(string),
) *CodingSubAgentResult

// runTaskWithSubAgent is overridden in tests to simulate long-running SubAgent outcomes.
// Nil means use RunTaskWithSubAgent; this avoids a package initialization cycle
// through the full GUI call graph.
var runTaskWithSubAgent subAgentTaskFunc

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
		return "No runnable task", false
	}
	return r.runTaskHandle(task, runID, r.collectPreviousOutputs(), onToken, onProgress)
}

func (r *SubAgentTaskRunner) runTaskHandle(task *TaskItem, runID int, prevOutputs []string, onToken llm.TokenCallback, onProgress func(string)) (summary string, passed bool) {
	if r == nil || r.orchestrator == nil || task == nil {
		return "SubAgent runner is not attached to an active task orchestrator", false
	}
	taskTitle := compactSubAgentTaskTitle(task.Title)
	displayIndex := taskDisplayNumber(task)
	eventRunID := fmt.Sprint(runID)
	turnCtx := newCodingTurnContext(eventRunID, task, r.orchestrator.ProjectPath)
	turnProgress := turnCtx.WrapProgress(onProgress)

	log.Printf("[subagent-runner] delegating T%d to SubAgent: %s", displayIndex, taskTitle)
	turnCtx.Emit(onProgress, turnCtx.TaskEvent("starting", task, taskTitle))

	// Mark task as in-progress.
	r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecInProgress, "")

	result, panicErr := r.runTaskWithRecover(task, prevOutputs, onToken, turnProgress)
	if panicErr != "" {
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("failed", task, taskTitle))
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecFailed, panicErr) {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		return fmt.Sprintf("T%d: %s - failed\nError: %s", displayIndex, taskTitle, panicErr), false
	}
	if result == nil {
		errSummary := "coding SubAgent returned no result"
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("failed", task, taskTitle))
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecFailed, errSummary) {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		return fmt.Sprintf("T%d: %s - failed\nError: %s", displayIndex, taskTitle, errSummary), false
	}

	// Update orchestrator based on result.
	resultSummary := compactSubAgentReportSummary(result.Summary)
	resultStatus, resultError := normalizeSubAgentResultStatus(result)
	artifactsRecorded := r.orchestrator.RecordTaskActualArtifactsForRun(task, runID, result.FilesModified, result.FilesCreated)
	if artifactsRecorded && r.handler != nil {
		emitCodingSubAgentCodeFileEvents(r.handler.app, r.activeCodeSessionID(), r.orchestrator.ProjectPath, result.FilesModified, result.FilesCreated)
	}
	switch resultStatus {
	case TaskExecPassed:
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecPassed, "") {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		log.Printf("[subagent-runner] T%d passed (%d iterations, %d tool calls, %d files, %d created)", displayIndex, result.Iterations, result.ToolCalls, len(result.FilesModified), len(result.FilesCreated))
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("completed", task, taskTitle))

		// Trigger experience extraction (best-effort, async)
		wasRetry := task.RetryCount > 0
		r.extractAndSaveExperience(task, result, wasRetry)

		return fmt.Sprintf("T%d: %s - completed\n%s", displayIndex, taskTitle, resultSummary), true

	case TaskExecFailed:
		if isSubAgentTransientProviderError(resultError) {
			log.Printf("[subagent-runner] T%d paused by transient provider error: %s", displayIndex, resultError)
			event := turnCtx.TaskEvent("failed", task, taskTitle)
			event.Detail = "provider_transient"
			turnCtx.Emit(onProgress, event)
			return fmt.Sprintf("T%d: %s - paused by transient provider error\nError: %s",
				displayIndex, taskTitle, resultError), false
		}
		retryCount, canRetry := r.orchestrator.IncrementTaskRetryForRun(task, runID)
		if retryCount == 0 && !canRetry {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		if canRetry {
			// Retry available; will be re-executed on next call.
			log.Printf("[subagent-runner] T%d failed (retry %d/%d): %s", displayIndex, retryCount, r.orchestrator.MaxRetries, resultError)
			event := turnCtx.TaskEvent("retrying", task, taskTitle)
			event.Detail = fmt.Sprintf("%d/%d", retryCount, r.orchestrator.MaxRetries)
			turnCtx.Emit(onProgress, event)
			return fmt.Sprintf("T%d: %s - failed, will retry (%d/%d)\nError: %s",
				displayIndex, taskTitle, retryCount, r.orchestrator.MaxRetries, resultError), false
		}
		// Max retries exhausted.
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecFailed, resultError) {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		log.Printf("[subagent-runner] T%d failed permanently: %s", displayIndex, resultError)
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("failed", task, taskTitle))
		return fmt.Sprintf("T%d: %s - failed permanently after %d retries\nError: %s",
			displayIndex, taskTitle, r.orchestrator.MaxRetries, resultError), false

	default:
		if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecSkipped, resultError) {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		turnCtx.Emit(onProgress, turnCtx.TaskEvent("skipped", task, taskTitle))
		return fmt.Sprintf("T%d: %s - skipped\n%s", displayIndex, taskTitle, resultError), false
	}
}

func (r *SubAgentTaskRunner) activeCodeSessionID() string {
	if r != nil && strings.TrimSpace(r.codeSessionID) != "" {
		return strings.TrimSpace(r.codeSessionID)
	}
	return "subagent-workflow"
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
	runner := runTaskWithSubAgent
	if runner == nil {
		runner = RunTaskWithSubAgent
	}
	return runner(
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
		index = taskDisplayNumber(task)
	}
	return fmt.Sprintf("T%d: %s - result ignored: task run expired/stale", index, taskTitle)
}

// RunAllTasks executes runnable tasks via SubAgent with bounded concurrency.
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
	configuredConc := r.configuredConcurrency()
	concurrency := configuredConc
	log.Printf("[subagent-runner] RunAllTasks starting: configured_concurrency=%d tasks=%d max_attempts=%d",
		configuredConc, r.orchestrator.TaskCount(), maxAttempts)

	for !r.orchestrator.AllDone() {
		attempts++
		if attempts > maxAttempts {
			reports = append(reports, fmt.Sprintf("SubAgent execution stopped after %d attempts without completing all tasks", maxAttempts))
			break
		}

		// Check cancellation between tasks; don't start a new task if
		// the user already clicked cancel.
		if r.loopCtx != nil && r.loopCtx.IsCancelled() {
			reports = append(reports, "User cancelled; remaining tasks were not executed")
			break
		}

		if skipped := r.orchestrator.MarkTasksBlockedByDependencies(); skipped > 0 {
			reports = append(reports, fmt.Sprintf("SubAgent skipped %d task(s) blocked by failed dependencies", skipped))
			if r.orchestrator.AllDone() {
				break
			}
		}

		if concurrency <= 1 {
			handles := r.orchestrator.ReadyTaskHandles(1)
			if len(handles) == 0 {
				if skipped := r.orchestrator.MarkDependencyDeadlockTasks(); skipped > 0 {
					reports = append(reports, fmt.Sprintf("SubAgent skipped %d task(s) blocked by dependency deadlock", skipped))
					continue
				}
				reports = append(reports, "SubAgent execution stopped: no runnable tasks are ready")
				break
			}

			handle := handles[0]
			summary, _ := r.runTaskHandle(handle.Task, handle.RunID, r.collectPreviousOutputs(), onToken, onProgress)
			reports = append(reports, summary)

			if isSubAgentTransientProviderError(summary) {
				reports = append(reports, "LLM provider is temporarily unavailable; SubAgent execution paused to avoid retry storms. Retry after the provider recovers.")
				break
			}
			// If concurrency was reduced from a higher configured value
			// (due to a prior batch failure) and this sequential task
			// passed, restore configured concurrency for the next iteration.
			if configuredConc > 1 && !batchHasFailedTask(handles) {
				concurrency = configuredConc
				log.Printf("[subagent-runner] sequential task succeeded, restoring concurrency=%d", concurrency)
			}
			continue
		}

		handles := r.orchestrator.ReadyTaskHandles(concurrency)
		if len(handles) == 0 {
			if skipped := r.orchestrator.MarkDependencyDeadlockTasks(); skipped > 0 {
				reports = append(reports, fmt.Sprintf("SubAgent skipped %d task(s) blocked by dependency deadlock", skipped))
				continue
			}
			reports = append(reports, "SubAgent execution stopped: no runnable tasks are ready")
			break
		}
		if len(handles) > 1 {
			taskNames := make([]string, len(handles))
			for i, h := range handles {
				taskNames[i] = fmt.Sprintf("T%d", taskDisplayNumber(h.Task))
			}
			log.Printf("[subagent-runner] launching parallel batch: concurrency=%d tasks=[%s]", len(handles), strings.Join(taskNames, ","))
			if onProgress != nil {
				onProgress(fmt.Sprintf("🚀 并行执行 %d 个任务: %s", len(handles), strings.Join(taskNames, ", ")))
			}
		}
		prevOutputs := r.collectPreviousOutputs()
		batchReports := make([]string, len(handles))
		var wg sync.WaitGroup
		safeOnProgress := serializedProgressCallback(onProgress)
		safeOnToken := serializedTokenCallback(onToken)
		for i, handle := range handles {
			i, handle := i, handle
			wg.Add(1)
			go func() {
				defer wg.Done()
				summary, _ := r.runTaskHandle(handle.Task, handle.RunID, prevOutputs, safeOnToken, safeOnProgress)
				batchReports[i] = summary
			}()
		}
		wg.Wait()
		reports = append(reports, batchReports...)
		if containsSubAgentTransientProviderReport(batchReports) {
			reports = append(reports, "LLM provider is temporarily unavailable; SubAgent execution paused to avoid retry storms. Retry after the provider recovers.")
			break
		}
		// Error fallback: if any task in this batch permanently failed,
		// reduce concurrency to 1 for the NEXT batch only. If the next
		// batch succeeds without permanent failures, restore configured
		// concurrency. This avoids cascading failures while allowing
		// recovery after transient issues.
		if batchHasFailedTask(handles) {
			concurrency = 1
			log.Printf("[subagent-runner] batch had failed task(s), reducing next batch to sequential (concurrency=1)")
			if onProgress != nil {
				onProgress("⚠️ 检测到任务失败，下一批次将顺序执行")
			}
		} else if concurrency < configuredConc {
			// Previous batch had a failure that reduced concurrency;
			// this batch succeeded — restore configured concurrency.
			concurrency = configuredConc
			log.Printf("[subagent-runner] batch succeeded, restoring concurrency=%d", concurrency)
		}
	}

	// Generate final report.
	var b strings.Builder
	b.WriteString("## Coding Execution Report\n\n")
	appendSubAgentRunReports(&b, reports)
	b.WriteString("---\n")
	b.WriteString(r.orchestrator.ProgressSummary())
	b.WriteString("\n\n---\n")
	appendSubAgentExecutionStats(&b, r.orchestrator.SnapshotTasks())

	return b.String()
}

func serializedProgressCallback(onProgress func(string)) func(string) {
	if onProgress == nil {
		return nil
	}
	var mu sync.Mutex
	return func(text string) {
		mu.Lock()
		defer mu.Unlock()
		onProgress(text)
	}
}

func serializedTokenCallback(onToken llm.TokenCallback) llm.TokenCallback {
	if onToken == nil {
		return nil
	}
	var mu sync.Mutex
	return func(text string) {
		mu.Lock()
		defer mu.Unlock()
		onToken(text)
	}
}

func (r *SubAgentTaskRunner) configuredConcurrency() int {
	if r == nil || r.handler == nil || r.handler.app == nil {
		return 1
	}
	return r.handler.app.GetSubAgentConcurrency()
}

func containsSubAgentTransientProviderReport(reports []string) bool {
	for _, report := range reports {
		if isSubAgentTransientProviderError(report) {
			return true
		}
	}
	return false
}

// batchHasFailedTask checks whether any task in the concurrent batch ended
// with a terminal failure (failed or skipped). Retryable failures (status
// still in-progress/testing) do NOT trigger fallback — the task will be
// retried in the next loop iteration.
func batchHasFailedTask(handles []TaskRunHandle) bool {
	for _, h := range handles {
		if h.Task == nil {
			continue
		}
		switch h.Task.Status {
		case TaskExecFailed, TaskExecSkipped:
			return true
		}
	}
	return false
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

func isSubAgentRateLimitError(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "http 429") ||
		strings.Contains(lower, "llm_endpoint_user_rate_limited") ||
		strings.Contains(lower, "user request rate exceeded") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "rate_limited") ||
		strings.Contains(lower, "too many requests")
}

func isSubAgentTransientProviderError(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	providerContext := strings.Contains(lower, "llm") ||
		strings.Contains(lower, "provider") ||
		strings.Contains(lower, "api/") ||
		strings.Contains(lower, "chat/completions") ||
		strings.Contains(lower, "anthropic") ||
		strings.Contains(lower, "/v1/messages") ||
		strings.Contains(lower, "openai") ||
		strings.Contains(lower, "bigmodel")
	return isSubAgentRateLimitError(lower) ||
		strings.Contains(lower, "http 500") ||
		strings.Contains(lower, "http 502") ||
		strings.Contains(lower, "http 503") ||
		strings.Contains(lower, "http 504") ||
		strings.Contains(lower, "500 internal server error") ||
		strings.Contains(lower, "503 service unavailable") ||
		(providerContext && strings.Contains(lower, "context deadline exceeded")) ||
		(providerContext && strings.Contains(lower, "connection reset")) ||
		(providerContext && strings.Contains(lower, "temporarily unavailable"))
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

	b.WriteString("## Execution Stats\n\n")
	b.WriteString(fmt.Sprintf("- Task results: %d passed / %d failed / %d skipped\n", passed, failed, skipped))
	b.WriteString(fmt.Sprintf("- Modified files: %d\n", len(modified)))
	if len(modified) > 0 {
		b.WriteString(fmt.Sprintf("  %s\n", compactSubAgentFileList(modified, codingSubAgentFileChangeSummaryMax)))
	}
	b.WriteString(fmt.Sprintf("- Created files: %d\n", len(created)))
	if len(created) > 0 {
		b.WriteString(fmt.Sprintf("  %s\n", compactSubAgentFileList(created, codingSubAgentFileChangeSummaryMax)))
	}
	if len(plannedOnly) > 0 {
		b.WriteString(fmt.Sprintf("- Planned artifacts without tracked file changes: %d\n", len(plannedOnly)))
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
		b.WriteString(fmt.Sprintf("... and %d more task reports omitted\n\n", remaining))
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
		outputs = append(outputs, fmt.Sprintf("%s (%s by T%d: %s passed)", compactSubAgentPathText(f), kind, taskDisplayNumber(t), title))
	}
	return outputs
}

func compactSubAgentTaskTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "Untitled task"
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
// - Next ready task's execution mode must be "direct"
// - Coding tasks use CodingSubAgent; external coding sessions are disabled
// - Dependency deadlocks still route here so RunAllTasks can mark them skipped
func ShouldUseSubAgent(orchestrator *TaskExecutionOrchestrator) bool {
	if orchestrator == nil || !orchestrator.IsActive() {
		return false
	}
	handles := orchestrator.ReadyTaskHandles(1)
	if len(handles) == 0 {
		return orchestrator.HasBlockedRunnableTasks()
	}
	mode, ok := orchestrator.ResolveExecutionModeForTaskRun(handles[0].Task, handles[0].RunID)
	return ok && (mode == TaskExecModeDirect || mode == TaskExecModeExternal)
}
