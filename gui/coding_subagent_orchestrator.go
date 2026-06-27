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

	prevOutputs = append(prevOutputs, currentTaskRetryOutputs(task)...)

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
	resultSummary = appendSubAgentQualityReportSummary(resultSummary, result)
	resultStatus, resultError := normalizeSubAgentResultStatus(result)
	r.orchestrator.RecordTaskResultSummaryForRun(task, runID, resultSummary)
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
		if isSubAgentNonRetryableFailure(resultError) {
			if !r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecFailed, resultError) {
				return staleSubAgentRunSummary(task, taskTitle), false
			}
			log.Printf("[subagent-runner] T%d failed permanently without retry: %s", displayIndex, resultError)
			event := turnCtx.TaskEvent("failed", task, taskTitle)
			event.Detail = "non_retryable"
			turnCtx.Emit(onProgress, event)
			return fmt.Sprintf("T%d: %s - failed permanently (non-retryable)\nError: %s",
				displayIndex, taskTitle, resultError), false
		}
		retryCount, canRetry := r.orchestrator.IncrementTaskRetryForRun(task, runID)
		if retryCount == 0 && !canRetry {
			return staleSubAgentRunSummary(task, taskTitle), false
		}
		if canRetry {
			// Retry available; will be re-executed on next call.
			log.Printf("[subagent-runner] T%d failed (retry %d/%d): %s", displayIndex, retryCount, r.orchestrator.MaxRetries, resultError)
			r.orchestrator.MarkTaskStatusForRun(task, runID, TaskExecInProgress, resultError)
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
	qualityFailure := subAgentQualityFailureError(result)
	switch result.Status {
	case TaskExecPassed:
		if qualityFailure != "" {
			return TaskExecFailed, qualityFailure
		}
		return TaskExecPassed, errSummary
	case TaskExecFailed:
		if errSummary == "" {
			errSummary = "coding SubAgent reported failure without an error summary"
		}
		return TaskExecFailed, enrichSubAgentFailureError(result, errSummary)
	case TaskExecSkipped:
		if errSummary == "" {
			errSummary = "coding SubAgent skipped the task without a reason"
		}
		return TaskExecSkipped, errSummary
	default:
		return TaskExecFailed, compactSubAgentErrorSummary(fmt.Sprintf("coding SubAgent returned unknown status %q", result.Status))
	}
}

func subAgentQualityFailureError(result *CodingSubAgentResult) string {
	if result == nil || result.QualityStatus != codingSubAgentQualityFailed {
		return ""
	}
	summary := strings.TrimSpace(result.QualitySummary)
	if summary == "" {
		summary = "coding SubAgent quality audit failed"
	} else {
		summary = "coding SubAgent quality audit failed: " + summary
	}
	return compactSubAgentErrorSummary(summary)
}

func appendSubAgentQualityReportSummary(summary string, result *CodingSubAgentResult) string {
	if result == nil || result.QualityStatus == "" || strings.TrimSpace(result.QualitySummary) == "" {
		return summary
	}
	section := subAgentQualityReportSection(result)
	summary = strings.TrimSpace(summary)
	if idx := strings.Index(summary, "## 质量审计"); idx >= 0 {
		end := len(summary)
		if next := strings.Index(summary[idx+len("## 质量审计"):], "\n\n## "); next >= 0 {
			end = idx + len("## 质量审计") + next
		}
		return strings.TrimSpace(summary[:idx] + section + summary[end:])
	}
	if summary == "" {
		return section
	}
	return summary + "\n\n" + section
}

func subAgentQualityReportSection(result *CodingSubAgentResult) string {
	label := strings.ToUpper(result.QualityStatus.String())
	qualitySummary := strings.TrimSpace(result.QualitySummary)
	if result.QualityIssueCount > 0 {
		qualitySummary = fmt.Sprintf("%s (%d issue(s))", qualitySummary, result.QualityIssueCount)
	}
	return fmt.Sprintf("## 质量审计\n\n%s: %s", label, qualitySummary)
}

func compactSubAgentQualityEvidence(result *CodingSubAgentResult) string {
	if result == nil || result.QualityStatus == "" {
		return ""
	}
	summary := strings.TrimSpace(result.QualitySummary)
	if summary == "" {
		return ""
	}
	label := strings.ToUpper(result.QualityStatus.String())
	if result.QualityIssueCount > 0 {
		return compactSubAgentErrorSummary(fmt.Sprintf("%s: %s (%d issue(s))", label, summary, result.QualityIssueCount))
	}
	return compactSubAgentErrorSummary(fmt.Sprintf("%s: %s", label, summary))
}
func enrichSubAgentFailureError(result *CodingSubAgentResult, errSummary string) string {
	errSummary = strings.TrimSpace(errSummary)
	if result == nil {
		return errSummary
	}
	lower := strings.ToLower(errSummary)
	var evidence []string
	if failed := unresolvedFailedSubAgentCommands(result.CommandsRun); len(failed) > 0 && !strings.Contains(lower, "failed command evidence") {
		evidence = append(evidence, "failed command evidence: "+compactFailedVerificationCommandResults(failed))
	}
	if failed := unresolvedFailedSubAgentDynamicTools(result.DynamicToolsRun); len(failed) > 0 && !strings.Contains(lower, "dynamic tool evidence") {
		evidence = append(evidence, "dynamic tool evidence: "+compactFailedSubAgentDynamicToolResults(failed))
	}
	if quality := compactSubAgentQualityEvidence(result); quality != "" && !strings.Contains(lower, "quality audit evidence") {
		evidence = append(evidence, "quality audit evidence: "+quality)
	}
	if len(evidence) == 0 {
		return errSummary
	}
	if errSummary != "" {
		errSummary += "; "
	}
	errSummary += strings.Join(evidence, "; ")
	return compactSubAgentErrorSummary(errSummary)
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
	if isSubAgentRateLimitError(lower) ||
		strings.Contains(lower, "http 500") ||
		strings.Contains(lower, "http 502") ||
		strings.Contains(lower, "http 503") ||
		strings.Contains(lower, "http 504") ||
		strings.Contains(lower, "500 internal server error") ||
		strings.Contains(lower, "502 bad gateway") ||
		strings.Contains(lower, "503 service unavailable") ||
		strings.Contains(lower, "504 gateway timeout") {
		return true
	}
	if !providerContext {
		return false
	}
	return strings.Contains(lower, "http 408") ||
		strings.Contains(lower, "gateway timeout") ||
		strings.Contains(lower, "upstream timeout") ||
		strings.Contains(lower, "upstream request timeout") ||
		strings.Contains(lower, "server overloaded") ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "unexpected eof") ||
		strings.Contains(lower, "tls handshake timeout") ||
		strings.Contains(lower, "temporarily unavailable")
}

func isSubAgentNonRetryableFailure(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if !strings.Contains(lower, "git diff unavailable") {
		return false
	}
	return strings.Contains(lower, "not a git repository") ||
		strings.Contains(lower, "is not a directory") ||
		strings.Contains(lower, "cannot inspect")
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

	var summaries []string
	var details []string
	for _, output := range outputs {
		if isSubAgentPreviousSummaryOutput(output) {
			summaries = append(summaries, output)
		} else {
			details = append(details, output)
		}
	}
	keep := map[string]bool{}
	for _, output := range recentSubAgentOutputItems(summaries, maxItems) {
		keep[output] = true
	}
	remaining := maxItems - len(keep)
	if remaining > 0 {
		for _, output := range recentSubAgentOutputItems(details, remaining) {
			keep[output] = true
		}
	}
	selected := make([]string, 0, maxItems)
	for _, output := range outputs {
		if keep[output] {
			selected = append(selected, output)
		}
	}
	return selected
}

func recentSubAgentOutputItems(outputs []string, maxItems int) []string {
	if maxItems <= 0 || len(outputs) == 0 {
		return nil
	}
	if len(outputs) <= maxItems {
		return outputs
	}
	return outputs[len(outputs)-maxItems:]
}

func isSubAgentPreviousSummaryOutput(output string) bool {
	output = strings.TrimSpace(output)
	return strings.HasPrefix(output, "Previous passed task summary") || strings.HasPrefix(output, "Previous failed attempt summary")
}

func previousTaskFileOutputs(t *TaskItem) []string {
	if t == nil {
		return nil
	}
	actualFiles := append([]string{}, t.ActualFiles...)
	actualFiles = append(actualFiles, t.ActualCreatedFiles...)
	files := uniqueSortedSubAgentStrings(actualFiles)
	source := "modified"
	if len(files) == 0 {
		files = uniqueSortedSubAgentStrings(t.Files)
		source = "planned"
	}
	created := make(map[string]bool, len(t.ActualCreatedFiles))
	for _, f := range uniqueSortedSubAgentStrings(t.ActualCreatedFiles) {
		created[f] = true
	}
	outputs := make([]string, 0, len(files)+1)
	title := compactSubAgentTaskTitle(t.Title)
	for _, f := range files {
		kind := source
		if created[f] {
			kind = "created"
		}
		outputs = append(outputs, fmt.Sprintf("%s (%s by T%d: %s passed)", compactSubAgentPathText(f), kind, taskDisplayNumber(t), title))
	}
	if summary := compactSubAgentPreviousResultSummary(t.ResultSummary); summary != "" {
		outputs = append(outputs, fmt.Sprintf("Previous passed task summary for T%d (%s): %s", taskDisplayNumber(t), title, summary))
	}
	return outputs
}

func currentTaskRetryOutputs(t *TaskItem) []string {
	if t == nil || t.RetryCount <= 0 {
		return nil
	}
	title := compactSubAgentTaskTitle(t.Title)
	errSummary := compactSubAgentErrorSummary(t.ErrorSummary)
	if errSummary == "" {
		errSummary = "previous attempt did not provide an error summary"
	}
	message := fmt.Sprintf("Retry context for T%d (%s): previous attempt failed with: %s. Do not repeat the same failed approach; inspect the previous summary/artifacts, adjust the fix, and run verification again.",
		taskDisplayNumber(t), title, errSummary)
	if hint := subAgentRetryRecoveryHint(errSummary); hint != "" {
		message += " Recovery hint: " + hint
	}
	outputs := []string{message}
	if summary := compactSubAgentPreviousResultSummary(t.ResultSummary); summary != "" {
		outputs = append(outputs, fmt.Sprintf("Previous failed attempt summary for T%d (%s): %s", taskDisplayNumber(t), title, summary))
	}
	outputs = append(outputs, retryTaskArtifactOutputs(t)...)
	return outputs
}

func retryTaskArtifactOutputs(t *TaskItem) []string {
	if t == nil {
		return nil
	}
	actualFiles := append([]string{}, t.ActualFiles...)
	actualFiles = append(actualFiles, t.ActualCreatedFiles...)
	files := uniqueSortedSubAgentStrings(actualFiles)
	if len(files) == 0 {
		return nil
	}
	created := make(map[string]bool, len(t.ActualCreatedFiles))
	for _, f := range uniqueSortedSubAgentStrings(t.ActualCreatedFiles) {
		created[f] = true
	}
	shown := len(files)
	if shown > codingSubAgentResultFilesMax {
		shown = codingSubAgentResultFilesMax
	}
	title := compactSubAgentTaskTitle(t.Title)
	outputs := make([]string, 0, shown+1)
	for _, f := range files[:shown] {
		kind := "modified"
		if created[f] {
			kind = "created"
		}
		outputs = append(outputs, fmt.Sprintf("Retry artifact from previous attempt: %s (%s by T%d: %s failed)", compactSubAgentPathText(f), kind, taskDisplayNumber(t), title))
	}
	if remaining := len(files) - shown; remaining > 0 {
		outputs = append(outputs, fmt.Sprintf("Retry artifact from previous attempt: ... %d more touched file(s) omitted", remaining))
	}
	return outputs
}

func subAgentRetryRecoveryHint(errSummary string) string {
	normalized := strings.ToLower(strings.TrimSpace(errSummary))
	if normalized == "" {
		return ""
	}
	if strings.Contains(normalized, "quality audit evidence") {
		if hint := subAgentQualityAuditRecoveryHint(normalized); hint != "" {
			return hint
		}
	}
	switch {
	case strings.Contains(normalized, "example valid arguments:"):
		return "Use the Example valid arguments snippet from the previous tool error as the JSON shape for the next call. Preserve the same tool intent, fill real task paths/content/query values, and do not retry malformed or empty arguments."
	case strings.Contains(normalized, "truncated tool") ||
		strings.Contains(normalized, "truncated arguments") ||
		strings.Contains(normalized, "tool call truncated") ||
		strings.Contains(normalized, "incomplete tool") ||
		strings.Contains(normalized, "incomplete json") ||
		strings.Contains(normalized, "unexpected end of json") ||
		strings.Contains(normalized, "unterminated string"):
		return "The previous tool call arguments were truncated or incomplete. Retry with valid complete JSON, reduce large content/query payloads, and split big write_file/edit payloads into smaller focused calls instead of resending one oversized tool call."
	case strings.Contains(normalized, "连续返回空响应") ||
		strings.Contains(normalized, "empty response") ||
		strings.Contains(normalized, "hard exit"):
		return "The previous attempt ended with empty model responses. Start the retry with a concise plan, then immediately make a concrete tool call for exploration or verification instead of replying with another empty/no-tool turn."
	case strings.Contains(normalized, "quality audit evidence") &&
		(strings.Contains(normalized, "verification not run") || strings.Contains(normalized, "no verification") || strings.Contains(normalized, "diff not checked") || strings.Contains(normalized, "git diff")):
		return "Resolve the quality audit evidence in order: inspect/explore before editing when required, run a focused verification command after the final edit, then run git_diff as the final self-check."
	case strings.Contains(normalized, "no exploration before existing-file edits") ||
		strings.Contains(normalized, "no exploration before editing existing files") ||
		strings.Contains(normalized, "first edit") ||
		strings.Contains(normalized, "首次修改前"):
		return "Before editing existing files, use codegraph explore/codegraph node first when .codegraph/ exists; otherwise use Glob/ripgrep/read_file to inspect the relevant code. Then make the minimal edit and continue with verification."
	case strings.Contains(normalized, "before the final edit") ||
		strings.Contains(normalized, "rerun test/build/lint/typecheck after editing"):
		return "Run verification after the final edit, not before it. Re-run the focused test/build/lint/typecheck command after making changes and use that fresh result."
	case strings.Contains(normalized, "stale diff") ||
		strings.Contains(normalized, "final diff"):
		return "Run git_diff after the last edit so the self-check reflects the final workspace state."
	case strings.Contains(normalized, "未实际执行测试或检查") ||
		strings.Contains(normalized, "no tests collected") ||
		strings.Contains(normalized, "no test files found") ||
		strings.Contains(normalized, "no tests to run") ||
		strings.Contains(normalized, "matched no packages") ||
		strings.Contains(normalized, "total tests: 0") ||
		strings.Contains(normalized, "tests run: 0") ||
		strings.Contains(normalized, "running 0 tests") ||
		strings.Contains(normalized, "0 tests found"):
		return "The previous verification command succeeded without actually running tests or checks. Inspect the test selector/path/config, rerun a command that discovers real tests, or use a focused build/lint/typecheck fallback and explain why no tests exist."
	case strings.Contains(normalized, "read_file") &&
		(strings.Contains(normalized, "before") || strings.Contains(normalized, "snapshot") || strings.Contains(normalized, "快照") || strings.Contains(normalized, "已变化") || strings.Contains(normalized, "重新调用")):
		return "Re-read the exact target file with read_file, then retry the edit using edit_file/edit_lines so the snapshot is fresh before modifying."
	case strings.Contains(normalized, "缺少 read_file") ||
		strings.Contains(normalized, "自上次 read_file") ||
		strings.Contains(normalized, "请先调用 read_file"):
		return "Call read_file on the target path again to refresh the snapshot, then make the minimal edit with edit_file/edit_lines."
	case strings.Contains(normalized, "failure-suppressing shell syntax") ||
		strings.Contains(normalized, "without || fallback") ||
		strings.Contains(normalized, "pipe filters") ||
		strings.Contains(normalized, "output redirection") ||
		strings.Contains(normalized, "extra commands after the verifier"):
		return "Re-run verification as a clean standalone command. Do not append `||` fallbacks, pipe filters, output redirection, or extra commands after the test/build/lint/typecheck command because they can hide failures."
	case strings.Contains(normalized, "shell_write") ||
		strings.Contains(normalized, "shell 直接改写文件") ||
		strings.Contains(normalized, "set-content") ||
		strings.Contains(normalized, "add-content") ||
		strings.Contains(normalized, "out-file") ||
		strings.Contains(normalized, "writefilesync") ||
		strings.Contains(normalized, "promises.rm") ||
		strings.Contains(normalized, "fs.rm") ||
		strings.Contains(normalized, ".rename(") ||
		strings.Contains(normalized, ".replace(") ||
		strings.Contains(normalized, ".touch(") ||
		strings.Contains(normalized, ".rmdir(") ||
		strings.Contains(normalized, "shutil.") ||
		strings.Contains(normalized, "redirection"):
		return "Use read_file first, then edit_file/edit_lines for existing files; use write_file only for new files. Do not use shell redirection, shell helpers, or inline Node/Python filesystem APIs to mutate files."
	case strings.Contains(normalized, "missing required argument"):
		return "Regenerate the failed tool call with every required argument populated. Do not retry with empty `{}`; include the needed path/pattern/query/command/action/tool/run_id fields for that tool."
	case strings.Contains(normalized, "invalid argument type") ||
		strings.Contains(normalized, "invalid argument value") ||
		strings.Contains(normalized, "invalid tool argument"):
		return "Regenerate the failed tool call with valid JSON scalar types and allowed values. Use strings for path/pattern/content/command fields, JSON integers for line/timeout fields, and JSON booleans for boolean fields."
	case strings.Contains(normalized, "timed out") ||
		strings.Contains(normalized, "timeout") ||
		strings.Contains(normalized, "context deadline exceeded"):
		return "The previous command timed out. Check whether the command hung or was too broad, narrow it to a focused package/test when possible, or set a reasonable timeout before rerunning verification after the final edit."
	case strings.Contains(normalized, "command not found") ||
		strings.Contains(normalized, "executable file not found") ||
		strings.Contains(normalized, "not recognized as an internal or external command") ||
		strings.Contains(normalized, "no such file or directory") ||
		strings.Contains(normalized, "cannot find the path specified"):
		return "The previous command failed because an executable, script, or path was unavailable. Verify the working directory and project toolchain first, use an available package-manager wrapper or focused fallback verification command if possible, and report an environment/tooling blocker instead of rewriting code when the failure is not caused by the patch."
	case strings.Contains(normalized, "failed command evidence") ||
		strings.Contains(normalized, "verification failed") ||
		strings.Contains(normalized, "command(s) failed") ||
		strings.Contains(normalized, "命令失败") ||
		strings.Contains(normalized, "验证命令失败"):
		return "Inspect the failing command output first, fix the reported compile/test/lint/typecheck root cause, then rerun the same focused verification command after the final edit."
	case strings.Contains(normalized, "missing required mcp argument"):
		return "Regenerate call_mcp_tool with all required MCP arguments populated inside arguments. Use the matched tool's required fields exactly, such as arguments.parent_id, and do not retry with empty arguments."
	case strings.Contains(normalized, "missing required skill argument"):
		return "Regenerate manage_skill with all required skill arguments populated inside args. Use the matched skill's required fields exactly, such as args.input, and do not retry with empty args."
	case strings.Contains(normalized, "dynamic tool failed") ||
		strings.Contains(normalized, "manage_skill") ||
		strings.Contains(normalized, "call_mcp_tool") ||
		strings.Contains(normalized, "mcp call failed") ||
		strings.Contains(normalized, "mcp tool error"):
		return "Inspect the dynamic tool failure output before retrying. Reuse only tools matched for this task, fix server/tool/name/arguments, and fall back to built-in read/search/bash verification if the host-backed tool is unavailable."
	case strings.Contains(normalized, "项目目录外") ||
		strings.Contains(normalized, "outside project") ||
		strings.Contains(normalized, "project path") ||
		strings.Contains(normalized, "working_dir"):
		return "Stay inside the assigned project path. Use project-relative paths and set bash working_dir to a directory under the project."
	case strings.Contains(normalized, "not a git repository") ||
		strings.Contains(normalized, "git diff unavailable"):
		return "The project path is not a Git repository, so repeating git_diff will not help. Verify with focused commands, summarize the modified/created file list, and explicitly report that Git diff self-check is unavailable."
	case strings.Contains(normalized, "git diff self-check") ||
		strings.Contains(normalized, "diff not checked") ||
		strings.Contains(normalized, "git diff failed"):
		return "Run git_diff after edits; if it fails, inspect the repository/working_dir state before finalizing."

	case strings.Contains(normalized, "acceptance criteria") &&
		strings.Contains(normalized, "not summarized"):
		return "Update the final task summary with an explicit acceptance-criteria verification section. For each listed criterion, state the evidence from the actual verification command or explain why it cannot be automated."
	case strings.Contains(normalized, "acceptance criteria") &&
		(strings.Contains(normalized, "each listed criterion") || strings.Contains(normalized, "listed criterion")):
		return "Update the final task summary to verify every listed acceptance criterion explicitly. Reference each item by its AC/标准 label, such as AC1/标准1 and AC2/标准2, and map each one to the verification command or explain why it cannot be automated."
	case strings.Contains(normalized, "created files without inspection") ||
		strings.Contains(normalized, "project-context evidence"):
		return "Inspect existing project context before creating new files. Use codegraph explore/codegraph node when available, or read/search adjacent modules, then keep the created file consistent with the discovered patterns."
	case strings.Contains(normalized, "no file changes and no inspection or verification evidence"):
		return "Do not retry with another empty/no-evidence answer. If the task needs code changes, inspect the relevant code and make the minimal edit; if no change is needed, gather inspection or verification evidence and summarize why the existing behavior already satisfies the task."
	case strings.Contains(normalized, "outside listed task scope") ||
		strings.Contains(normalized, "scope rationale"):
		return "Review the extra changed files against the task's listed scope. Revert unrelated edits when possible; if the extra files are required, keep them minimal and explicitly explain the scope rationale and file paths in the final summary."
	case strings.Contains(normalized, "claimed verification command not found in audit log"):
		return "Do not claim verification commands that were not actually run. Re-run the exact verification command after the final edit, or correct the final summary to list only commands present in the audit log, preserving shell wrappers and quoting when they were part of the audited command."
	case strings.Contains(normalized, "claimed verification command passed but audit log recorded failure"):
		return "Do not claim a verification command passed when the audit log recorded failure. Inspect the failed command output, fix the root cause or correct the final summary, then rerun the focused verification command."
	case strings.Contains(normalized, "fresh verification command outcome not referenced in final summary") ||
		strings.Contains(normalized, "verification command outcome not referenced in final summary"):
		return "Update the final summary to state the outcome of the fresh post-edit verification command exactly as audited, such as passed, failed, or blocked by missing tooling."
	case strings.Contains(normalized, "fresh verification command not referenced in final summary") ||
		strings.Contains(normalized, "verification command not referenced in final summary"):
		return "Update the final summary to cite the fresh post-edit verification command exactly as run in the audit log, including its pass/fail outcome. Do not substitute stale pre-edit verification or an unrun command."
	case strings.Contains(normalized, "changed files not referenced in final summary"):
		return "Update the final summary to name the actual modified and created file paths from the audit evidence, then briefly state what changed in each important file."
	case strings.Contains(normalized, "remaining risk not called out"):
		return "Update the final summary to include a remaining-risk note. State either the concrete residual risk/blocker, or say that no known remaining risk was found after the listed verification."
	case strings.Contains(normalized, "no verification") ||
		strings.Contains(normalized, "verification not run") ||
		strings.Contains(normalized, "验证命令") ||
		strings.Contains(normalized, "test/build/lint/typecheck"):
		return "Run a focused verification command that matches the change, such as test/build/lint/typecheck, and use its output to guide the next edit."
	case strings.Contains(normalized, "guardrail"):
		return "Address the blocked policy directly before retrying; choose the allowed tool path instead of repeating the blocked tool call."
	}
	return ""
}

func subAgentQualityAuditRecoveryHint(normalized string) string {
	var hints []string
	add := func(hint string) {
		if strings.TrimSpace(hint) == "" {
			return
		}
		for _, existing := range hints {
			if existing == hint {
				return
			}
		}
		hints = append(hints, hint)
	}
	if strings.Contains(normalized, "no exploration before existing-file edits") ||
		strings.Contains(normalized, "no exploration before editing existing files") ||
		strings.Contains(normalized, "first edit") ||
		strings.Contains(normalized, "首次修改前") {
		add("inspect/explore before editing existing files; use codegraph explore/codegraph node when .codegraph/ exists, then keep the edit minimal.")
	}
	if strings.Contains(normalized, "created files without inspection") ||
		strings.Contains(normalized, "project-context evidence") {
		add("Inspect existing project context before creating new files, then keep new files consistent with discovered patterns.")
	}
	if strings.Contains(normalized, "no file changes and no inspection or verification evidence") {
		add("Do not retry with another empty/no-evidence answer; inspect or verify the relevant behavior, then either make the minimal edit or explain why no change is needed.")
	}
	if strings.Contains(normalized, "outside listed task scope") ||
		strings.Contains(normalized, "scope rationale") {
		add("Review changed files outside the listed task scope; revert unrelated edits or explicitly explain the scope rationale and paths in the final summary.")
	}
	if strings.Contains(normalized, "acceptance criteria") && strings.Contains(normalized, "not summarized") {
		add("Update the final task summary with an explicit acceptance-criteria verification section mapping each criterion to actual verification evidence or a non-automated rationale.")
	} else if strings.Contains(normalized, "acceptance criteria") &&
		(strings.Contains(normalized, "each listed criterion") || strings.Contains(normalized, "listed criterion")) {
		add("Update the final task summary to verify every listed acceptance criterion explicitly, using AC/标准 labels and mapping each item to verification evidence.")
	}
	if strings.Contains(normalized, "fresh verification command not referenced in final summary") ||
		strings.Contains(normalized, "verification command not referenced in final summary") {
		add("Update the final summary to cite the fresh post-edit verification command exactly as run in the audit log, including its outcome.")
	}
	if strings.Contains(normalized, "claimed verification command not found in audit log") {
		add("Do not claim verification commands that were not actually run; rerun the exact command after the final edit or list only commands present in the audit log, preserving shell wrappers and quoting when they were part of the audited command.")
	}
	if strings.Contains(normalized, "claimed verification command passed but audit log recorded failure") {
		add("Do not claim a verification command passed when the audit log recorded failure; fix the root cause or correct the final summary, then rerun verification.")
	}
	if strings.Contains(normalized, "fresh verification command outcome not referenced in final summary") ||
		strings.Contains(normalized, "verification command outcome not referenced in final summary") {
		add("Update the final summary to state the outcome of the fresh post-edit verification command exactly as audited, such as passed, failed, or blocked by missing tooling.")
	}
	if strings.Contains(normalized, "changed files not referenced in final summary") {
		add("Update the final summary to name the actual modified and created file paths and state what changed in each important file.")
	}
	if strings.Contains(normalized, "remaining risk not called out") {
		add("Update the final summary with a remaining-risk note, either naming concrete residual risk or stating no known remaining risk after verification.")
	}
	if strings.Contains(normalized, "未实际执行测试或检查") ||
		strings.Contains(normalized, "no tests collected") ||
		strings.Contains(normalized, "no test files found") ||
		strings.Contains(normalized, "no tests to run") ||
		strings.Contains(normalized, "matched no packages") ||
		strings.Contains(normalized, "total tests: 0") ||
		strings.Contains(normalized, "tests run: 0") ||
		strings.Contains(normalized, "running 0 tests") ||
		strings.Contains(normalized, "0 tests found") {
		add("The previous verification command succeeded without actually running tests or checks; inspect the test selector/path/config and rerun a command that discovers real tests or use a focused build/lint/typecheck fallback.")
	}
	if strings.Contains(normalized, "failure-suppressing shell syntax") ||
		strings.Contains(normalized, "without || fallback") ||
		strings.Contains(normalized, "pipe filters") ||
		strings.Contains(normalized, "output redirection") ||
		strings.Contains(normalized, "extra commands after the verifier") {
		add("Re-run verification as a clean standalone command without `||` fallbacks, pipe filters, output redirection, or extra commands after the verifier.")
	}
	if strings.Contains(normalized, "timed out") ||
		strings.Contains(normalized, "timeout") ||
		strings.Contains(normalized, "context deadline exceeded") {
		add("The previous command timed out; narrow broad commands when possible or set a reasonable timeout before rerunning verification after the final edit.")
	}
	if strings.Contains(normalized, "failed command evidence") ||
		strings.Contains(normalized, "verification failed") ||
		strings.Contains(normalized, "command(s) failed") ||
		strings.Contains(normalized, "命令失败") ||
		strings.Contains(normalized, "验证命令失败") {
		add("Inspect the failing command output first, fix the reported compile/test/lint/typecheck root cause, then rerun the same focused verification command after the final edit.")
	}
	if strings.Contains(normalized, "verification not run") ||
		strings.Contains(normalized, "no verification") ||
		strings.Contains(normalized, "验证命令") ||
		strings.Contains(normalized, "test/build/lint/typecheck") {
		add("Run a focused verification command after the final edit that matches the change, such as test/build/lint/typecheck.")
	}
	if strings.Contains(normalized, "diff not checked") ||
		strings.Contains(normalized, "git diff") {
		add("Run git_diff after edits as the final self-check.")
	}
	return strings.Join(hints, " ")
}

func compactSubAgentPreviousResultSummary(summary string) string {
	summary = compactSubAgentReportSummary(summary)
	if summary == "" {
		return ""
	}
	for _, header := range []string{"## 质量审计", "## 验证状态", "## Diff 自检", "## 探索状态"} {
		if section := compactSubAgentReportSection(summary, header); section != "" {
			return section
		}
	}
	return strings.Join(strings.Fields(summary), " ")
}

func compactSubAgentReportSection(summary, header string) string {
	summary = strings.TrimSpace(summary)
	header = strings.TrimSpace(header)
	if summary == "" || header == "" {
		return ""
	}
	idx := strings.Index(summary, header)
	if idx < 0 {
		return ""
	}
	section := summary[idx:]
	if next := strings.Index(section[len(header):], "\n\n## "); next >= 0 {
		section = section[:len(header)+next]
	}
	return strings.Join(strings.Fields(section), " ")
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
