package main

// TaskExecutionOrchestrator manages per-task execution during the coding
// workflow's Execution Phase (绗叚姝?. Instead of letting the LLM dump the
// entire project description into a single session, the orchestrator tracks
// which task is currently being executed and constructs focused prompts that
// include only the current task's description plus minimal context from
// confirmed requirements and design documents.

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
)

// TaskOrchestratorRegistry provides per-user orchestrator isolation.
// In maclawsrv (multi-tenant), each user gets their own orchestrator
// instance so concurrent coding workflows don't interfere. In GUI/TUI
// (single-user), there's effectively only one entry.
type TaskOrchestratorRegistry struct {
	mu              sync.RWMutex
	orchestrators   map[string]*TaskExecutionOrchestrator // userID 鈫?orchestrator
	externalChecker ExternalToolChecker                   // shared across all orchestrators
}

// NewTaskOrchestratorRegistry creates a new registry.
func NewTaskOrchestratorRegistry() *TaskOrchestratorRegistry {
	return &TaskOrchestratorRegistry{
		orchestrators: make(map[string]*TaskExecutionOrchestrator),
	}
}

// SetExternalChecker sets the external tool checker for all orchestrators.
func (r *TaskOrchestratorRegistry) SetExternalChecker(checker ExternalToolChecker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.externalChecker = checker
	// Update existing orchestrators.
	for _, o := range r.orchestrators {
		o.ExternalChecker = checker
	}
}

// Get returns the orchestrator for the given user, or nil if none exists.
func (r *TaskOrchestratorRegistry) Get(userID string) *TaskExecutionOrchestrator {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.orchestrators[userID]
}

// GetOrCreate returns the orchestrator for the given user, creating one
// if it doesn't exist yet.
func (r *TaskOrchestratorRegistry) GetOrCreate(userID string) *TaskExecutionOrchestrator {
	r.mu.Lock()
	defer r.mu.Unlock()
	if o, ok := r.orchestrators[userID]; ok {
		return o
	}
	o := NewTaskExecutionOrchestrator()
	o.ExternalChecker = r.externalChecker
	r.orchestrators[userID] = o
	return o
}

// Remove removes the orchestrator for the given user.
func (r *TaskOrchestratorRegistry) Remove(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.orchestrators, userID)
}

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
	// TaskExecModeExternal is retained only for legacy persisted state.
	// New agent task execution always resolves to direct CodingSubAgent mode.
	TaskExecModeExternal TaskExecMode = "external"
	// TaskExecModeDirect uses maclaw's internal CodingSubAgent path.
	TaskExecModeDirect TaskExecMode = "direct"
)

// TaskItem represents a single task extracted from the confirmed task list.
type TaskItem struct {
	Index              int
	DisplayNumber      int // stable one-based T number shown to users/logs
	Title              string
	Description        string
	Files              []string // expected files to create/modify
	ActualFiles        []string // files actually modified during execution (populated by SubAgent)
	ActualCreatedFiles []string // files newly created during execution (populated by SubAgent)
	AcceptanceCriteria []string // TDD test criteria
	DependsOn          []int    // indices of prerequisite tasks
	Status             TaskExecStatus
	RetryCount         int
	SessionID          string // session used for this task
	ErrorSummary       string
	ExecMode           TaskExecMode // resolved per-task at execution time
}

type TaskRunHandle struct {
	Task  *TaskItem
	RunID int
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

	// RunID increments each time Activate starts a fresh execution wave.
	RunID int
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
	o.RunID++
	o.Tasks = tasks
	for i, t := range o.Tasks {
		if t != nil {
			t.DisplayNumber = i + 1
		}
	}
	o.CurrentIndex = 0
	o.RequirementsContext = requirementsCtx
	o.DesignContext = designCtx
	o.ProjectPath = projectPath
	o.Tool = tool
	normalizeTaskDependencies(o.Tasks)

	for _, t := range o.Tasks {
		resetTaskExecutionState(t)
	}
	log.Printf("[task-orchestrator] activated with %d tasks, tool=%s, project=%s", len(tasks), tool, projectPath)
}

func resetTaskExecutionState(t *TaskItem) {
	if t == nil {
		return
	}
	t.Status = TaskExecPending
	t.RetryCount = 0
	t.SessionID = ""
	t.ErrorSummary = ""
	t.ExecMode = ""
	t.ActualFiles = nil
	t.ActualCreatedFiles = nil
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

// SnapshotTasks returns a copy of task state for read-only reporting.
func (o *TaskExecutionOrchestrator) SnapshotTasks() []*TaskItem {
	o.mu.Lock()
	defer o.mu.Unlock()
	return cloneTaskItems(o.Tasks)
}

func cloneTaskItems(tasks []*TaskItem) []*TaskItem {
	if len(tasks) == 0 {
		return nil
	}
	out := make([]*TaskItem, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, cloneTaskItem(task))
	}
	return out
}

func cloneTaskItem(task *TaskItem) *TaskItem {
	if task == nil {
		return nil
	}
	copyTask := *task
	copyTask.Files = append([]string(nil), task.Files...)
	copyTask.ActualFiles = append([]string(nil), task.ActualFiles...)
	copyTask.ActualCreatedFiles = append([]string(nil), task.ActualCreatedFiles...)
	copyTask.AcceptanceCriteria = append([]string(nil), task.AcceptanceCriteria...)
	copyTask.DependsOn = append([]int(nil), task.DependsOn...)
	return &copyTask
}

// CurrentTaskHandle returns the current task plus the active run token.
func (o *TaskExecutionOrchestrator) CurrentTaskHandle() (*TaskItem, int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.Active || o.CurrentIndex >= len(o.Tasks) {
		return nil, 0
	}
	return o.Tasks[o.CurrentIndex], o.RunID
}

func (o *TaskExecutionOrchestrator) ReadyTaskHandles(limit int) []TaskRunHandle {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.Active || limit <= 0 {
		return nil
	}
	handles := make([]TaskRunHandle, 0, limit)
	for _, task := range o.Tasks {
		if task == nil || !taskRunnableLocked(task) || !o.taskDependenciesPassedLocked(task) {
			continue
		}
		handles = append(handles, TaskRunHandle{Task: task, RunID: o.RunID})
		if len(handles) >= limit {
			break
		}
	}
	return handles
}

func (o *TaskExecutionOrchestrator) HasBlockedRunnableTasks() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.Active {
		return false
	}
	for _, task := range o.Tasks {
		if task != nil && taskRunnableLocked(task) && !o.taskDependenciesPassedLocked(task) {
			return true
		}
	}
	return false
}

func (o *TaskExecutionOrchestrator) MarkTasksBlockedByDependencies() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.Active {
		return 0
	}
	blocked := 0
	for _, task := range o.Tasks {
		if task == nil || !taskRunnableLocked(task) {
			continue
		}
		if reason := o.taskDependencyBlockReasonLocked(task); reason != "" {
			applyTaskStatus(task, TaskExecSkipped, reason)
			blocked++
		}
	}
	return blocked
}

func (o *TaskExecutionOrchestrator) MarkDependencyDeadlockTasks() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.Active {
		return 0
	}
	for _, task := range o.Tasks {
		if task != nil && taskRunnableLocked(task) && o.taskDependenciesPassedLocked(task) {
			return 0
		}
	}
	blocked := 0
	for _, task := range o.Tasks {
		if task == nil || !taskRunnableLocked(task) || len(task.DependsOn) == 0 {
			continue
		}
		applyTaskStatus(task, TaskExecSkipped, "blocked by dependency deadlock")
		blocked++
	}
	return blocked
}

func taskRunnableLocked(task *TaskItem) bool {
	switch task.Status {
	case TaskExecPending, TaskExecInProgress, TaskExecTesting:
		return true
	default:
		return false
	}
}

func (o *TaskExecutionOrchestrator) taskDependenciesPassedLocked(task *TaskItem) bool {
	for _, depIdx := range task.DependsOn {
		if depIdx < 0 || depIdx >= len(o.Tasks) || o.Tasks[depIdx] == nil || o.Tasks[depIdx].Status != TaskExecPassed {
			return false
		}
	}
	return true
}

func (o *TaskExecutionOrchestrator) taskDependencyBlockReasonLocked(task *TaskItem) string {
	for _, depIdx := range task.DependsOn {
		if depIdx < 0 || depIdx >= len(o.Tasks) || o.Tasks[depIdx] == nil {
			return fmt.Sprintf("blocked by invalid dependency index %d", depIdx)
		}
		dep := o.Tasks[depIdx]
		switch dep.Status {
		case TaskExecPassed:
			continue
		case TaskExecFailed, TaskExecSkipped:
			return fmt.Sprintf("blocked because dependency T%d is %s", taskDisplayNumber(dep), dep.Status)
		}
	}
	return ""
}

func (o *TaskExecutionOrchestrator) validTaskRunLocked(task *TaskItem, runID int) bool {
	return o.Active && runID == o.RunID && o.taskIndexLocked(task) >= 0
}

func (o *TaskExecutionOrchestrator) validTaskLocked(task *TaskItem) bool {
	return o.Active && o.taskIndexLocked(task) >= 0
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

func (o *TaskExecutionOrchestrator) currentTaskLocked() *TaskItem {
	if !o.Active || o.CurrentIndex < 0 || o.CurrentIndex >= len(o.Tasks) {
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

func (o *TaskExecutionOrchestrator) taskIndexLocked(target *TaskItem) int {
	if target == nil {
		return -1
	}
	for i, task := range o.Tasks {
		if task == target {
			return i
		}
	}
	return -1
}

func applyTaskStatus(task *TaskItem, status TaskExecStatus, errSummary string) {
	if task == nil {
		return
	}
	task.Status = status
	switch {
	case errSummary != "":
		task.ErrorSummary = compactSubAgentErrorSummary(errSummary)
	case status == TaskExecPassed || status == TaskExecInProgress || status == TaskExecTesting:
		task.ErrorSummary = ""
	}
}

func updateTaskActualArtifacts(task *TaskItem, filesModified, filesCreated []string) {
	if task == nil {
		return
	}
	if len(filesModified) > 0 {
		task.ActualFiles = limitSubAgentStringSlice(uniqueSortedSubAgentStrings(filesModified), codingSubAgentResultFilesMax)
	}
	if len(filesCreated) > 0 {
		task.ActualCreatedFiles = limitSubAgentStringSlice(uniqueSortedSubAgentStrings(filesCreated), codingSubAgentResultFilesMax)
	}
}

// RecordTaskActualArtifactsForRun records files for a task if the run token is still current.
func (o *TaskExecutionOrchestrator) RecordTaskActualArtifactsForRun(task *TaskItem, runID int, filesModified, filesCreated []string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return false
	}
	updateTaskActualArtifacts(task, filesModified, filesCreated)
	return true
}

// RecordTaskActualArtifacts records files touched by a specific task pointer.
func (o *TaskExecutionOrchestrator) RecordTaskActualArtifacts(task *TaskItem, filesModified, filesCreated []string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskLocked(task) {
		return false
	}
	updateTaskActualArtifacts(task, filesModified, filesCreated)
	return true
}

// RecordCurrentActualArtifacts records files touched by the current task.
func (o *TaskExecutionOrchestrator) RecordCurrentActualArtifacts(filesModified, filesCreated []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	task := o.currentTaskLocked()
	if task == nil {
		return
	}
	updateTaskActualArtifacts(task, filesModified, filesCreated)
}

// TaskStatusForRun returns task status only when the run token is still current.
func (o *TaskExecutionOrchestrator) TaskStatusForRun(task *TaskItem, runID int) (TaskExecStatus, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return "", false
	}
	return task.Status, true
}

// TaskStatus returns the status for a specific task pointer.
func (o *TaskExecutionOrchestrator) TaskStatus(task *TaskItem) (TaskExecStatus, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskLocked(task) {
		return "", false
	}
	return task.Status, true
}

func isTerminalTaskStatus(status TaskExecStatus) bool {
	switch status {
	case TaskExecPassed, TaskExecFailed, TaskExecSkipped:
		return true
	default:
		return false
	}
}

// IsTaskTerminalForRun reports terminal status only for the current run token.
func (o *TaskExecutionOrchestrator) IsTaskTerminalForRun(task *TaskItem, runID int) bool {
	status, ok := o.TaskStatusForRun(task, runID)
	return ok && isTerminalTaskStatus(status)
}

// IsTaskTerminal reports whether a specific task has reached a terminal status.
func (o *TaskExecutionOrchestrator) IsTaskTerminal(task *TaskItem) bool {
	status, ok := o.TaskStatus(task)
	return ok && isTerminalTaskStatus(status)
}

// MarkTaskStatusForRun updates a task only when the run token is still current.
func (o *TaskExecutionOrchestrator) MarkTaskStatusForRun(task *TaskItem, runID int, status TaskExecStatus, errSummary string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return false
	}
	applyTaskStatus(task, status, errSummary)
	return true
}

// MarkTaskStatus updates a specific task pointer if it still belongs to this orchestrator.
func (o *TaskExecutionOrchestrator) MarkTaskStatus(task *TaskItem, status TaskExecStatus, errSummary string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskLocked(task) {
		return false
	}
	applyTaskStatus(task, status, errSummary)
	return true
}

// IncrementTaskRetryForRun increments retry only when the run token is still current.
func (o *TaskExecutionOrchestrator) IncrementTaskRetryForRun(task *TaskItem, runID int) (retryCount int, allowed bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return 0, false
	}
	task.RetryCount++
	return task.RetryCount, task.RetryCount <= o.MaxRetries
}

// IncrementTaskRetry increments retry count on a specific task pointer.
func (o *TaskExecutionOrchestrator) IncrementTaskRetry(task *TaskItem) (retryCount int, allowed bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskLocked(task) {
		return 0, false
	}
	task.RetryCount++
	return task.RetryCount, task.RetryCount <= o.MaxRetries
}

// MarkCurrentStatus updates the current task's status.
func (o *TaskExecutionOrchestrator) MarkCurrentStatus(status TaskExecStatus, errSummary string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	task := o.currentTaskLocked()
	if task == nil {
		return
	}
	applyTaskStatus(task, status, errSummary)
}

// IncrementRetry increments the retry count for the current task.
// Returns true if retries are still available.
func (o *TaskExecutionOrchestrator) IncrementRetry() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	task := o.currentTaskLocked()
	if task == nil {
		return false
	}
	task.RetryCount++
	return task.RetryCount <= o.MaxRetries
}

// SetTaskSessionIDForRun records session only when the run token is still current.
func (o *TaskExecutionOrchestrator) SetTaskSessionIDForRun(task *TaskItem, runID int, sessionID string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return false
	}
	task.SessionID = sessionID
	return true
}

// SetTaskSessionID records which session is handling a specific task pointer.
func (o *TaskExecutionOrchestrator) SetTaskSessionID(task *TaskItem, sessionID string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskLocked(task) {
		return false
	}
	task.SessionID = sessionID
	return true
}

// SetCurrentSessionID records which session is handling the current task.
func (o *TaskExecutionOrchestrator) SetCurrentSessionID(sessionID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if task := o.currentTaskLocked(); task != nil {
		task.SessionID = sessionID
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

// BuildTaskPrompt constructs the focused prompt for the current task. It
// includes only the current task's description plus minimal context.
func (o *TaskExecutionOrchestrator) BuildTaskPrompt() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	task := o.currentTaskLocked()
	if task == nil {
		return ""
	}
	return o.buildTaskPromptLocked(task)
}

// BuildTaskPromptForTaskRun constructs a focused prompt only when the run token is still current.
func (o *TaskExecutionOrchestrator) BuildTaskPromptForTaskRun(task *TaskItem, runID int) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return ""
	}
	return o.buildTaskPromptLocked(task)
}

// BuildTaskPromptForTask constructs a focused prompt for a specific task pointer.
func (o *TaskExecutionOrchestrator) BuildTaskPromptForTask(task *TaskItem) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskLocked(task) {
		return ""
	}
	return o.buildTaskPromptLocked(task)
}

func (o *TaskExecutionOrchestrator) buildTaskPromptLocked(task *TaskItem) string {
	if task == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("## \u4efb\u52a1 %d/%d: %s\n\n", taskDisplayNumber(task), len(o.Tasks), compactSubAgentTaskTitle(task.Title)))
	b.WriteString(compactSubAgentTaskDescription(task.Description))
	b.WriteString("\n")

	if len(task.Files) > 0 {
		b.WriteString("\n### Files\n")
		files := uniqueSortedSubAgentStrings(task.Files)
		shown := len(files)
		if shown > codingSubAgentTaskFilesMax {
			shown = codingSubAgentTaskFilesMax
		}
		for _, f := range files[:shown] {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
		if remaining := len(files) - shown; remaining > 0 {
			b.WriteString(fmt.Sprintf("- ... %d more files omitted\n", remaining))
		}
	}

	if len(task.AcceptanceCriteria) > 0 {
		b.WriteString("\n### Acceptance Criteria\n")
		criteria := uniqueSubAgentStrings(task.AcceptanceCriteria)
		shown := len(criteria)
		if shown > codingSubAgentAcceptanceCriteriaMax {
			shown = codingSubAgentAcceptanceCriteriaMax
		}
		for i, ac := range criteria[:shown] {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, truncateRunesForSubAgent(ac, codingSubAgentPromptBulletMaxRunes)))
		}
		if remaining := len(criteria) - shown; remaining > 0 {
			b.WriteString(fmt.Sprintf("... %d more acceptance criteria omitted\n", remaining))
		}
	}

	if len(task.DependsOn) > 0 {
		b.WriteString("\n### \u524d\u7f6e\u4efb\u52a1\u4ea7\u51fa\n")
		shownDeps := 0
		for _, depIdx := range task.DependsOn {
			if shownDeps >= codingSubAgentDependencySummaryMax {
				break
			}
			if depIdx >= 0 && depIdx < len(o.Tasks) {
				dep := o.Tasks[depIdx]
				shownDeps++
				b.WriteString(fmt.Sprintf("- Task %d %q: %s\n", taskDisplayNumber(dep), compactSubAgentTaskTitle(dep.Title), dep.Status))
				if files := taskIntegrationFiles(dep); len(files) > 0 {
					b.WriteString(fmt.Sprintf("  files: %s\n", compactSubAgentFileList(files, codingSubAgentTaskFilesMax)))
				}
			}
		}
		if remaining := len(task.DependsOn) - shownDeps; remaining > 0 {
			b.WriteString(fmt.Sprintf("- ... \u8fd8\u6709 %d \u4e2a\u524d\u7f6e\u4efb\u52a1\u672a\u5c55\u5f00\n", remaining))
		}
	}

	if o.RequirementsContext != "" {
		b.WriteString("\n### Requirements Summary\n")
		b.WriteString(truncateRunes(o.RequirementsContext, 500))
		b.WriteString("\n")
	}
	if o.DesignContext != "" {
		b.WriteString("\n### Design Summary\n")
		b.WriteString(truncateRunes(o.DesignContext, 500))
		b.WriteString("\n")
	}

	return b.String()
}

// BuildTDDPrompt constructs the prompt to run TDD tests for the current task.
func (o *TaskExecutionOrchestrator) BuildTDDPrompt() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	task := o.currentTaskLocked()
	if task == nil {
		return ""
	}
	return o.buildTDDPromptLocked(task)
}

// BuildTDDPromptForTaskRun constructs a TDD prompt only when the run token is still current.
func (o *TaskExecutionOrchestrator) BuildTDDPromptForTaskRun(task *TaskItem, runID int) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return ""
	}
	return o.buildTDDPromptLocked(task)
}

func (o *TaskExecutionOrchestrator) buildTDDPromptLocked(task *TaskItem) string {
	if task == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Task %d %q is implemented.\n\n", taskDisplayNumber(task), compactSubAgentTaskTitle(task.Title)))
	b.WriteString("Run the acceptance checks below to verify the implementation:\n\n")

	if len(task.AcceptanceCriteria) > 0 {
		criteria := uniqueSubAgentStrings(task.AcceptanceCriteria)
		shown := len(criteria)
		if shown > codingSubAgentAcceptanceCriteriaMax {
			shown = codingSubAgentAcceptanceCriteriaMax
		}
		for i, ac := range criteria[:shown] {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, truncateRunesForSubAgent(ac, codingSubAgentPromptBulletMaxRunes)))
		}
		if remaining := len(criteria) - shown; remaining > 0 {
			b.WriteString(fmt.Sprintf("... %d more acceptance criteria omitted\n", remaining))
		}
	} else {
		b.WriteString("Run the project test suite and ensure existing behavior is preserved.\n")
	}

	appendTaskActualArtifactPrompt(&b, task)
	b.WriteString("\nIf tests fail, fix the code and rerun the relevant checks.")
	return b.String()
}

// BuildFixPrompt constructs the prompt to fix a failed test.
func (o *TaskExecutionOrchestrator) BuildFixPrompt(testOutput string) string {
	o.mu.Lock()
	defer o.mu.Unlock()

	task := o.currentTaskLocked()
	if task == nil {
		return ""
	}
	return o.buildFixPromptLocked(task, testOutput)
}

// BuildFixPromptForTaskRun constructs a fix prompt only when the run token is still current.
func (o *TaskExecutionOrchestrator) BuildFixPromptForTaskRun(task *TaskItem, runID int, testOutput string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return ""
	}
	return o.buildFixPromptLocked(task, testOutput)
}

func (o *TaskExecutionOrchestrator) buildFixPromptLocked(task *TaskItem, testOutput string) string {
	if task == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Task %d %q failed tests (retry %d/%d).\n\n",
		taskDisplayNumber(task), compactSubAgentTaskTitle(task.Title), task.RetryCount, o.MaxRetries))
	b.WriteString("Test output:\n")
	b.WriteString(truncateRunes(testOutput, 1000))
	appendTaskActualArtifactPrompt(&b, task)
	b.WriteString("\n\nAnalyze the failure, fix the code, and rerun the relevant checks.")
	return b.String()
}

func appendTaskActualArtifactPrompt(b *strings.Builder, task *TaskItem) {
	if b == nil || task == nil {
		return
	}
	modified := uniqueSortedSubAgentStrings(task.ActualFiles)
	created := uniqueSortedSubAgentStrings(task.ActualCreatedFiles)
	if len(modified) == 0 && len(created) == 0 {
		return
	}
	b.WriteString("\n### Actual Artifacts From This Task\n")
	if len(created) > 0 {
		b.WriteString(fmt.Sprintf("Created: %s\n", compactSubAgentFileList(created, codingSubAgentTaskFilesMax)))
	}
	if len(modified) > 0 {
		b.WriteString(fmt.Sprintf("Modified: %s\n", compactSubAgentFileList(modified, codingSubAgentTaskFilesMax)))
	}
}

// BuildIntegrationPrompt constructs the prompt for the integration phase
// that runs after all individual tasks are complete. It instructs the coding
// tool to wire all modules together, fix cross-module issues, and ensure
// the project compiles and runs as a whole.
func (o *TaskExecutionOrchestrator) BuildIntegrationPrompt() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	var b strings.Builder
	b.WriteString("## \u96c6\u6210\u8054\u8c03\n\n")
	b.WriteString("All subtasks have completed. Integrate the outputs into a buildable, runnable whole.\n\n")

	b.WriteString("### Completed Subtasks And Outputs\n")
	var failedNames []string
	for _, t := range o.Tasks {
		icon := "\u2713"
		if t.Status == TaskExecFailed {
			icon = "\u274c"
			failedNames = append(failedNames, fmt.Sprintf("Task %d %q", taskDisplayNumber(t), compactSubAgentTaskTitle(t.Title)))
		} else if t.Status == TaskExecSkipped {
			icon = "SKIPPED"
		}
		b.WriteString(fmt.Sprintf("%s task %d: %s\n", icon, taskDisplayNumber(t), compactSubAgentTaskTitle(t.Title)))
		if files := taskIntegrationFiles(t); len(files) > 0 {
			b.WriteString(fmt.Sprintf("   files: %s\n", compactSubAgentFileList(files, codingSubAgentTaskFilesMax)))
		}
	}

	if len(failedNames) > 0 {
		b.WriteString("\n### Failed Tasks\n")
		shownFailed := len(failedNames)
		if shownFailed > codingSubAgentTaskListSummaryMax {
			shownFailed = codingSubAgentTaskListSummaryMax
		}
		for _, name := range failedNames[:shownFailed] {
			b.WriteString(fmt.Sprintf("- %s\n", name))
		}
		b.WriteString("Check these modules during integration: \u4e0d\u5b8c\u6574 outputs may need repair.\n")
	}

	b.WriteString("\n### Integration Requirements\n")
	b.WriteString("1. Check imports/dependencies between modules.\n")
	b.WriteString("2. Ensure entry points initialize and reference all required modules.\n")
	b.WriteString("3. Verify interface compatibility across module boundaries.\n")
	b.WriteString("4. Fill missing glue code such as routing, dependency injection, and config loading.\n")
	b.WriteString("5. Run build/compile checks and fix all \u7f16\u8bd1 errors.\n")
	b.WriteString("6. Run relevant tests or smoke checks.\n")

	if o.RequirementsContext != "" {
		b.WriteString("\n### Requirements Context Summary\n")
		b.WriteString(truncateRunes(o.RequirementsContext, 800))
		b.WriteString("\n")
	}

	if o.DesignContext != "" {
		b.WriteString("\n### Design Context Summary\n")
		b.WriteString(truncateRunes(o.DesignContext, 800))
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
	task := o.currentTaskLocked()
	if task == nil {
		return TaskExecModeDirect
	}
	return o.resolveExecutionModeForTaskLocked(task)
}

// ResolveExecutionModeForTaskRun determines and caches a task's execution mode
// only when the task still belongs to the active run.
func (o *TaskExecutionOrchestrator) ResolveExecutionModeForTaskRun(task *TaskItem, runID int) (TaskExecMode, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return TaskExecModeDirect, false
	}
	return o.resolveExecutionModeForTaskLocked(task), true
}

func (o *TaskExecutionOrchestrator) resolveExecutionModeForTaskLocked(task *TaskItem) TaskExecMode {
	if task == nil {
		return TaskExecModeDirect
	}
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
	return TaskExecModeDirect
}

// DegradeCurrentToDirectMode switches the current task from external to
// direct mode. Called when an external tool fails with a non-code error
// (rate limit, connection failure, tool crash). Returns true if degraded.
func (o *TaskExecutionOrchestrator) DegradeCurrentToDirectMode() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	task := o.currentTaskLocked()
	if task == nil {
		return false
	}
	return o.degradeTaskToDirectModeLocked(task)
}

// DegradeTaskToDirectModeForRun switches a task to direct mode only when the
// task still belongs to the active run.
func (o *TaskExecutionOrchestrator) DegradeTaskToDirectModeForRun(task *TaskItem, runID int) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return false
	}
	return o.degradeTaskToDirectModeLocked(task)
}

func (o *TaskExecutionOrchestrator) degradeTaskToDirectModeLocked(task *TaskItem) bool {
	if task == nil || task.ExecMode != TaskExecModeExternal {
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
	task := o.currentTaskLocked()
	if task == nil {
		return TaskExecModeDirect
	}
	return currentTaskExecModeLocked(task)
}

// TaskExecutionModeForRun returns a task's resolved mode only when the run
// token is still current.
func (o *TaskExecutionOrchestrator) TaskExecutionModeForRun(task *TaskItem, runID int) (TaskExecMode, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return TaskExecModeDirect, false
	}
	return currentTaskExecModeLocked(task), true
}

func currentTaskExecModeLocked(task *TaskItem) TaskExecMode {
	if task == nil || task.ExecMode == "" {
		return TaskExecModeDirect
	}
	return task.ExecMode
}

// BuildSystemInjection returns a system message to inject into the conversation
// at the start of each iteration, reminding the LLM which task to focus on.
func (o *TaskExecutionOrchestrator) BuildSystemInjection() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	task := o.currentTaskLocked()
	if task == nil {
		return ""
	}
	return o.buildSystemInjectionLocked(task)
}

// BuildSystemInjectionForTaskRun builds task guidance only when the task still
// belongs to the active run. It prevents stale execution loops from injecting
// guidance for whichever task happens to be current now.
func (o *TaskExecutionOrchestrator) BuildSystemInjectionForTaskRun(task *TaskItem, runID int) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.validTaskRunLocked(task, runID) {
		return ""
	}
	return o.buildSystemInjectionLocked(task)
}

func (o *TaskExecutionOrchestrator) buildSystemInjectionLocked(task *TaskItem) string {
	if task == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(o.buildTaskContextLocked(task))
	b.WriteString(o.buildExecutionGuideLocked(task))
	return b.String()
}

// buildTaskContextLocked writes pure task context: current task, task list,
// progress stats. No tool-specific instructions. Caller holds mu.
func (o *TaskExecutionOrchestrator) buildTaskContextLocked(task *TaskItem) string {
	if task == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("[task-orchestrator] executing task %d/%d (\u4efb\u52a1 %d/%d): %q\n", task.Index+1, len(o.Tasks), task.Index+1, len(o.Tasks), compactSubAgentTaskTitle(task.Title)))

	b.WriteString("\nTask list:\n")
	shownTasks := len(o.Tasks)
	if shownTasks > codingSubAgentTaskListSummaryMax {
		shownTasks = codingSubAgentTaskListSummaryMax
	}
	for _, t := range o.Tasks[:shownTasks] {
		title := compactSubAgentTaskTitle(t.Title)
		switch t.Status {
		case TaskExecPassed:
			b.WriteString(fmt.Sprintf("T%d: %s [passed]\n", t.Index+1, title))
		case TaskExecFailed:
			b.WriteString(fmt.Sprintf("T%d: %s [failed]", t.Index+1, title))
			if t.ErrorSummary != "" {
				b.WriteString(fmt.Sprintf(" - %s", compactSubAgentErrorSummary(t.ErrorSummary)))
			}
			b.WriteString("\n")
		case TaskExecSkipped:
			b.WriteString(fmt.Sprintf("T%d: %s [skipped]\n", t.Index+1, title))
		case TaskExecInProgress, TaskExecTesting:
			b.WriteString(fmt.Sprintf("T%d: %s [active]\n", t.Index+1, title))
		default:
			b.WriteString(fmt.Sprintf("T%d: %s\n", t.Index+1, title))
		}
	}
	if remaining := len(o.Tasks) - shownTasks; remaining > 0 {
		b.WriteString(fmt.Sprintf("... \u8fd8\u6709 %d \u4e2a\u4efb\u52a1\u672a\u5c55\u5f00\n", remaining))
	}

	passed, failed, remaining := o.countStatusLocked()
	b.WriteString(fmt.Sprintf("\nProgress: %d passed | %d failed | %d remaining\n", passed, failed, remaining))
	b.WriteString("\nWhen reporting progress to the user, use one line per task: T1: description [passed|active|pending].\n")
	return b.String()
}

// buildExecutionGuideLocked writes tool-specific execution instructions
// based on the current task's execution mode. Caller holds mu.
func (o *TaskExecutionOrchestrator) buildExecutionGuideLocked(task *TaskItem) string {
	if task == nil {
		return ""
	}
	mode := task.ExecMode
	if mode == "" {
		mode = TaskExecModeDirect
	}

	var b strings.Builder
	if mode == TaskExecModeDirect {
		switch task.Status {
		case TaskExecPending:
			b.WriteString("\nExecution mode: internal CodingSubAgent.\n")
			b.WriteString("Delegate this task to CodingSubAgent; do not create external coding sessions.\n")
		case TaskExecInProgress:
			b.WriteString("Continue the current task through CodingSubAgent.\n")
		case TaskExecTesting:
			b.WriteString("Use CodingSubAgent to run acceptance checks and fix failures before marking the task done.\n")
		}
	} else {
		switch task.Status {
		case TaskExecPending:
			b.WriteString("Legacy external execution mode is disabled. Use CodingSubAgent for this task.\n")
		case TaskExecInProgress:
			b.WriteString("Legacy external execution mode is disabled. Continue through CodingSubAgent.\n")
		case TaskExecTesting:
			b.WriteString("Legacy external execution mode is disabled. Use CodingSubAgent for verification.\n")
		}
	}
	return b.String()
}

// ProgressSummary returns a user-facing progress message.
func (o *TaskExecutionOrchestrator) ProgressSummary() string {
	o.mu.Lock()
	defer o.mu.Unlock()

	var b strings.Builder
	shownTasks := len(o.Tasks)
	if shownTasks > codingSubAgentTaskListSummaryMax {
		shownTasks = codingSubAgentTaskListSummaryMax
	}
	for _, t := range o.Tasks[:shownTasks] {
		b.WriteString(fmt.Sprintf("T%d: %s", t.Index+1, compactSubAgentTaskTitle(t.Title)))
		switch t.Status {
		case TaskExecPassed:
			b.WriteString(" \u2713")
		case TaskExecFailed:
			b.WriteString(" \u274c")
			if t.ErrorSummary != "" {
				b.WriteString(fmt.Sprintf(" - %s", compactSubAgentErrorSummary(t.ErrorSummary)))
			}
		case TaskExecSkipped:
			b.WriteString(" [skipped]")
		case TaskExecInProgress, TaskExecTesting:
			b.WriteString(" \u27f3")
		}
		b.WriteString("\n")
	}
	if remaining := len(o.Tasks) - shownTasks; remaining > 0 {
		b.WriteString(fmt.Sprintf("... \u8fd8\u6709 %d \u4e2a\u4efb\u52a1\u672a\u5c55\u5f00\n", remaining))
	}

	passed, failed, remaining := o.countStatusLocked()
	skipped := len(o.Tasks) - passed - failed - remaining
	b.WriteString(fmt.Sprintf("\nTotal: %d/%d passed", passed, len(o.Tasks)))
	if failed > 0 {
		b.WriteString(fmt.Sprintf(", %d failed", failed))
	}
	if skipped > 0 {
		b.WriteString(fmt.Sprintf(", %d skipped", skipped))
	}
	if remaining > 0 {
		b.WriteString(fmt.Sprintf(", %d remaining", remaining))
	}
	return b.String()
}

// HasPassedTasks returns true if at least one task has passed.
// Used to determine whether the integration phase should run.
func (o *TaskExecutionOrchestrator) HasPassedTasks() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, t := range o.Tasks {
		if t.Status == TaskExecPassed {
			return true
		}
	}
	return false
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
			failedTasks = append(failedTasks, fmt.Sprintf("- Task %d %q: %s", t.Index+1, compactSubAgentTaskTitle(t.Title), compactSubAgentErrorSummary(t.ErrorSummary)))
		case TaskExecSkipped:
			skipped++
		}
	}

	var b strings.Builder
	b.WriteString("## Coding Task Execution Report\n\n")
	b.WriteString(fmt.Sprintf("- Total tasks: %d\n", len(o.Tasks)))
	b.WriteString(fmt.Sprintf("- \u6210\u529f: %d\n", passed))
	b.WriteString(fmt.Sprintf("- \u5931\u8d25: %d\n", failed))
	if skipped > 0 {
		b.WriteString(fmt.Sprintf("- Skipped: %d\n", skipped))
	}
	appendFinalReportArtifactSummary(&b, o.Tasks)

	if len(failedTasks) > 0 {
		b.WriteString("\n### Failed Tasks\n")
		shownFailed := len(failedTasks)
		if shownFailed > codingSubAgentTaskListSummaryMax {
			shownFailed = codingSubAgentTaskListSummaryMax
		}
		for _, ft := range failedTasks[:shownFailed] {
			b.WriteString(ft + "\n")
		}
		b.WriteString("\nRecommendation: retry failed tasks individually or inspect the error summary and repair manually.\n")
	}

	return b.String()
}

func appendFinalReportArtifactSummary(b *strings.Builder, tasks []*TaskItem) {
	if b == nil {
		return
	}
	var modified []string
	var created []string
	for _, task := range tasks {
		if task == nil {
			continue
		}
		modified = append(modified, task.ActualFiles...)
		created = append(created, task.ActualCreatedFiles...)
	}
	modified = uniqueSortedSubAgentStrings(modified)
	created = uniqueSortedSubAgentStrings(created)
	if len(modified) == 0 && len(created) == 0 {
		return
	}
	b.WriteString("\n### \u5b9e\u9645\u4ea7\u7269\n")
	b.WriteString(fmt.Sprintf("- \u5b9e\u9645\u4fee\u6539\u6587\u4ef6: %d", len(modified)))
	if len(modified) > 0 {
		b.WriteString(fmt.Sprintf(" (%s)", compactSubAgentFileList(modified, codingSubAgentFileChangeSummaryMax)))
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("- \u65b0\u5efa\u6587\u4ef6: %d", len(created)))
	if len(created) > 0 {
		b.WriteString(fmt.Sprintf(" (%s)", compactSubAgentFileList(created, codingSubAgentFileChangeSummaryMax)))
	}
	b.WriteString("\n")
}

func taskIntegrationFiles(t *TaskItem) []string {
	if t == nil {
		return nil
	}
	files := uniqueSubAgentStrings(append([]string{}, t.ActualFiles...))
	files = append(files, t.ActualCreatedFiles...)
	files = uniqueSortedSubAgentStrings(files)
	if len(files) > 0 {
		return files
	}
	return uniqueSortedSubAgentStrings(t.Files)
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

		if isTaskHeader(trimmed) {
			if current != nil {
				tasks = append(tasks, current)
			}
			current = &TaskItem{Index: len(tasks), DisplayNumber: len(tasks) + 1, Title: extractTaskTitle(trimmed), Status: TaskExecPending}
			inCriteria = false
			continue
		}
		if current == nil {
			continue
		}

		lowerTrimmed := strings.ToLower(trimmed)
		if strings.Contains(lowerTrimmed, "acceptance") || strings.Contains(lowerTrimmed, "criteria") || strings.Contains(lowerTrimmed, "test") || strings.Contains(trimmed, "\u9a8c\u6536") || strings.Contains(trimmed, "\u6d4b\u8bd5") {
			if strings.HasPrefix(trimmed, "#") || strings.HasSuffix(trimmed, ":") {
				inCriteria = true
				continue
			}
		}

		if isTaskFileListItem(trimmed, inCriteria, lowerTrimmed) {
			file := strings.TrimSpace(strings.TrimLeft(trimmed, "-* "))
			current.Files = append(current.Files, file)
			continue
		}

		if inCriteria {
			criterion := strings.TrimLeft(trimmed, "-*0123456789.) ")
			if criterion != "" {
				current.AcceptanceCriteria = append(current.AcceptanceCriteria, criterion)
			}
			continue
		}
		if deps, ok := parseTaskDependencyLine(trimmed); ok {
			current.DependsOn = mergeTaskDependencyIndexes(current.DependsOn, deps)
			continue
		}
		if current.Description != "" {
			current.Description += "\n"
		}
		current.Description += trimmed
	}

	if current != nil {
		tasks = append(tasks, current)
	}
	normalizeExplicitTaskDependencyIndexes(tasks)
	normalizeTaskDependencies(tasks)
	return tasks
}

func parseTaskDependencyLine(line string) ([]int, bool) {
	trimmed := strings.TrimSpace(strings.TrimLeft(line, "-* "))
	lower := strings.ToLower(trimmed)
	markers := []string{"depends on", "dependency", "dependencies", "depends", "依赖", "前置任务", "前置"}
	markerPos := -1
	for _, marker := range markers {
		if idx := strings.Index(lower, marker); idx >= 0 && (markerPos < 0 || idx < markerPos) {
			markerPos = idx
		}
	}
	if markerPos < 0 {
		return nil, false
	}
	segment := trimmed[markerPos:]
	if idx := strings.IndexAny(segment, ":：-"); idx >= 0 {
		segment = segment[idx+1:]
	}
	if deps, ok := extractTTaskDependencyIndexes(segment); ok {
		return deps, true
	}
	deps := extractTaskDependencyIndexes(segment)
	return deps, true
}

func extractTTaskDependencyIndexes(text string) ([]int, bool) {
	lower := strings.ToLower(text)
	var deps []int
	found := false
	for i := 0; i < len(lower); i++ {
		if lower[i] != 't' || i+1 >= len(lower) || lower[i+1] < '0' || lower[i+1] > '9' {
			continue
		}
		j := i + 1
		label := 0
		for j < len(lower) && lower[j] >= '0' && lower[j] <= '9' {
			label = label*10 + int(lower[j]-'0')
			j++
		}
		found = true
		// Store the raw label value. The caller chain applies
		// normalizeExplicitTaskDependencyIndexes afterward to convert
		// 1-based labels (T1=first task) to 0-based indices when all
		// deps look 1-based. If any dep==0 is present, labels are
		// treated as already 0-based and left as-is.
		deps = append(deps, label)
		i = j - 1
	}
	return deps, found
}

func extractTaskDependencyIndexes(text string) []int {
	var deps []int
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= '0' && r <= '9')
	})
	for _, field := range fields {
		if field == "" {
			continue
		}
		idx := 0
		for _, r := range field {
			idx = idx*10 + int(r-'0')
		}
		deps = append(deps, idx)
	}
	return deps
}

func mergeTaskDependencyIndexes(existing, incoming []int) []int {
	seen := make(map[int]bool, len(existing)+len(incoming))
	merged := make([]int, 0, len(existing)+len(incoming))
	for _, idx := range existing {
		if idx >= 0 && !seen[idx] {
			seen[idx] = true
			merged = append(merged, idx)
		}
	}
	for _, idx := range incoming {
		if idx >= 0 && !seen[idx] {
			seen[idx] = true
			merged = append(merged, idx)
		}
	}
	return merged
}

func taskDisplayNumber(task *TaskItem) int {
	if task == nil {
		return 0
	}
	if task.DisplayNumber > 0 {
		return task.DisplayNumber
	}
	if task.Index <= 0 {
		return 1
	}
	return task.Index
}

func normalizeExplicitTaskDependencyIndexes(tasks []*TaskItem) {
	if !taskDependenciesLookOneBased(tasks) {
		return
	}
	for _, task := range tasks {
		if task == nil {
			continue
		}
		for i, dep := range task.DependsOn {
			if dep > 0 {
				task.DependsOn[i] = dep - 1
			}
		}
	}
}

func taskDependenciesLookOneBased(tasks []*TaskItem) bool {
	if len(tasks) == 0 {
		return false
	}
	found := false
	for _, task := range tasks {
		if task == nil {
			continue
		}
		for _, dep := range task.DependsOn {
			found = true
			if dep == 0 || dep > len(tasks) {
				return false
			}
		}
	}
	return found
}

func normalizeTaskDependencies(tasks []*TaskItem) {
	if len(tasks) < 2 {
		return
	}
	bootstrap := looksLikeBootstrapTask(tasks[0])
	for i, task := range tasks[1:] {
		if task == nil {
			continue
		}
		if len(task.DependsOn) > 0 {
			continue
		}
		idx := i + 1
		var deps []int
		if bootstrap {
			deps = append(deps, 0)
		}
		if looksLikeIntegrationOrVerificationTask(task) {
			for dep := 0; dep < idx; dep++ {
				deps = append(deps, dep)
			}
		}
		if len(deps) > 0 {
			task.DependsOn = mergeTaskDependencyIndexes(task.DependsOn, deps)
		}
	}
}

func looksLikeBootstrapTask(task *TaskItem) bool {
	if task == nil {
		return false
	}
	text := strings.ToLower(task.Title + "\n" + task.Description + "\n" + strings.Join(task.Files, "\n"))
	bootstrapSignals := []string{"cmake", "project", "scaffold", "bootstrap", "setup", "directory", "目录", "项目", "构建", "创建"}
	for _, signal := range bootstrapSignals {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func looksLikeIntegrationOrVerificationTask(task *TaskItem) bool {
	if task == nil {
		return false
	}
	text := strings.ToLower(task.Title + "\n" + task.Description)
	signals := []string{"integration", "integrate", "main", "compile", "build", "test", "debug", "verify", "验收", "测试", "编译", "调试", "整合", "集成", "主循环"}
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			return true
		}
	}
	return false
}

func isTaskFileListItem(trimmed string, inCriteria bool, lowerTrimmed string) bool {
	if inCriteria || !(strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*")) {
		return false
	}
	item := strings.TrimSpace(strings.TrimLeft(trimmed, "-* "))
	if item == "" || strings.ContainsAny(item, " 	") {
		return false
	}
	if strings.Contains(lowerTrimmed, "file") || strings.Contains(trimmed, "\u6587\u4ef6") {
		return strings.Contains(item, ".")
	}
	return looksLikeTaskFilePath(item)
}

func looksLikeTaskFilePath(item string) bool {
	if item == "" || strings.HasPrefix(item, "http://") || strings.HasPrefix(item, "https://") {
		return false
	}
	if strings.ContainsAny(item, "<>|?*") {
		return false
	}
	base := filepath.Base(filepath.ToSlash(item))
	if base == "." || base == "/" || base == "" {
		return false
	}
	return strings.Contains(base, ".")
}

// isTaskHeader checks if a line looks like a numbered task header.
func isTaskHeader(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	line = strings.TrimSpace(strings.TrimLeft(line, "#"))
	if line == "" {
		return false
	}
	if hasNumericTaskPrefix(line) {
		return true
	}
	lower := strings.ToLower(line)
	if hasAlphaNumericTaskPrefix(lower, 't') {
		return true
	}
	for _, prefix := range []string{"\u4efb\u52a1", "task"} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := strings.TrimSpace(line[len(prefix):])
		return len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9'
	}
	return false
}

func hasAlphaNumericTaskPrefix(line string, prefix rune) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	r, width := utf8DecodeRuneInString(line)
	if r != prefix {
		return false
	}
	rest := strings.TrimSpace(line[width:])
	return hasNumericTaskPrefix(rest)
}

func hasNumericTaskPrefix(line string) bool {
	sawDigit := false
	for _, r := range line {
		if r >= '0' && r <= '9' {
			sawDigit = true
			continue
		}
		if !sawDigit {
			return false
		}
		return isTaskHeaderDelimiter(r)
	}
	return false
}

func isTaskHeaderDelimiter(r rune) bool {
	switch r {
	case '.', ')', ':', '\uff1a', '\u3001', '\uff0e':
		return true
	default:
		return false
	}
}

// extractTaskTitle extracts the title from a task header line.
func extractTaskTitle(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimSpace(strings.TrimLeft(line, "#"))
	if len(line) > 1 {
		lower := strings.ToLower(line)
		if hasAlphaNumericTaskPrefix(lower, 't') {
			_, width := utf8DecodeRuneInString(line)
			line = strings.TrimSpace(line[width:])
		}
	}
	for i, r := range line {
		if isTaskHeaderDelimiter(r) {
			rest := strings.TrimSpace(line[i+utf8RuneWidth(r):])
			if rest != "" {
				return rest
			}
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
