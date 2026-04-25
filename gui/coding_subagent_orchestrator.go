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

// RunCurrentTask executes the orchestrator's current task via SubAgent.
// Returns a human-readable summary for the main agent to relay to the user.
func (r *SubAgentTaskRunner) RunCurrentTask(
	onToken llm.TokenCallback,
	onProgress func(string),
) (summary string, passed bool) {
	task := r.orchestrator.CurrentTask()
	if task == nil {
		return "没有待执行的任务", false
	}

	// Collect previous task outputs for context.
	prevOutputs := r.collectPreviousOutputs()

	log.Printf("[subagent-runner] delegating T%d to SubAgent: %s", task.Index, task.Title)

	// Mark task as in-progress.
	r.orchestrator.MarkCurrentStatus(TaskExecInProgress, "")

	result := RunTaskWithSubAgent(
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
	)

	// Update orchestrator based on result.
	switch result.Status {
	case TaskExecPassed:
		r.orchestrator.MarkCurrentStatus(TaskExecPassed, "")
		// Record actual files modified so subsequent tasks get accurate context.
		if len(result.FilesModified) > 0 {
			task.ActualFiles = result.FilesModified
		}
		log.Printf("[subagent-runner] T%d passed (%d iterations, %d tool calls, %d files)", task.Index, result.Iterations, result.ToolCalls, len(result.FilesModified))
		return fmt.Sprintf("✅ T%d: %s — 完成\n%s", task.Index, task.Title, result.Summary), true

	case TaskExecFailed:
		if r.orchestrator.IncrementRetry() {
			// Retry available — will be re-executed on next call.
			log.Printf("[subagent-runner] T%d failed (retry %d/%d): %s", task.Index, task.RetryCount, r.orchestrator.MaxRetries, result.Error)
			return fmt.Sprintf("⚠️ T%d: %s — 失败，将重试 (%d/%d)\n错误: %s",
				task.Index, task.Title, task.RetryCount, r.orchestrator.MaxRetries, result.Error), false
		}
		// Max retries exhausted.
		r.orchestrator.MarkCurrentStatus(TaskExecFailed, result.Error)
		log.Printf("[subagent-runner] T%d failed permanently: %s", task.Index, result.Error)
		return fmt.Sprintf("❌ T%d: %s — 失败（已重试 %d 次）\n错误: %s",
			task.Index, task.Title, r.orchestrator.MaxRetries, result.Error), false

	default:
		r.orchestrator.MarkCurrentStatus(TaskExecSkipped, result.Error)
		return fmt.Sprintf("⏭️ T%d: %s — 跳过\n%s", task.Index, task.Title, result.Error), false
	}
}

// RunAllTasks executes all tasks sequentially via SubAgent.
// Returns the final report.
func (r *SubAgentTaskRunner) RunAllTasks(
	onToken llm.TokenCallback,
	onProgress func(string),
) string {
	var reports []string

	for !r.orchestrator.AllDone() {
		// Check cancellation between tasks — don't start a new task if
		// the user already clicked cancel.
		if r.loopCtx != nil && r.loopCtx.IsCancelled() {
			reports = append(reports, "⏹️ 用户取消，剩余任务未执行")
			break
		}

		task := r.orchestrator.CurrentTask()
		if task == nil {
			break
		}

		summary, passed := r.RunCurrentTask(onToken, onProgress)
		reports = append(reports, summary)

		if passed || task.Status == TaskExecFailed || task.Status == TaskExecSkipped {
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
	for _, taskReport := range reports {
		b.WriteString(taskReport)
		b.WriteString("\n\n")
	}
	b.WriteString("---\n")
	b.WriteString(r.orchestrator.ProgressSummary())

	return b.String()
}

// collectPreviousOutputs gathers file lists from completed tasks.
// Prefers ActualFiles (what the SubAgent actually modified) over Files
// (what the task declaration expected). This ensures subsequent tasks
// see files that were created outside the original plan (e.g. CMakeLists
// modifications, new header dependencies).
func (r *SubAgentTaskRunner) collectPreviousOutputs() []string {
	var outputs []string
	for _, t := range r.orchestrator.Tasks {
		if t.Status != TaskExecPassed {
			continue
		}
		files := t.ActualFiles
		if len(files) == 0 {
			files = t.Files
		}
		for _, f := range files {
			outputs = append(outputs, fmt.Sprintf("%s (T%d: %s ✅)", f, t.Index, t.Title))
		}
	}
	return outputs
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
	mode := orchestrator.ResolveExecutionMode()
	return mode == TaskExecModeDirect
}
