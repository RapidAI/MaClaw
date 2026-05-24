package main

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// SessionResult holds the outcome of a single session within an orchestrated
// queued execution.
type SessionResult struct {
	SessionID string                    `json:"session_id"`
	Tool      string                    `json:"tool"`
	Status    orchestratorSessionStatus `json:"status"`
	Output    string                    `json:"output"`
	Error     string                    `json:"error,omitempty"`
}

// TaskRequest describes a single unit of work to be queued by the Orchestrator.
type TaskRequest struct {
	Tool        string `json:"tool"`
	Description string `json:"description"`
	ProjectPath string `json:"project_path"`
}

// OrchestratorTask tracks the lifecycle of a queued execution batch.
type OrchestratorTask struct {
	ID        string
	Sessions  []string // session IDs created for this task
	Status    orchestratorTaskStatus
	Results   map[string]string
	CreatedAt time.Time
}

// OrchestratorResult is the aggregated outcome returned by ExecuteParallel.
type OrchestratorResult struct {
	TaskID  string
	Results map[string]SessionResult
	Summary string
}

// Orchestrator coordinates queued execution of multiple programming-tool
// sessions, tracks their status, and aggregates results.
type Orchestrator struct {
	app          *App
	manager      *RemoteSessionManager
	sharedCtx    *SharedContextStore
	toolSelector *ToolSelector
	mu           sync.RWMutex
	activeTasks  map[string]*OrchestratorTask
	executeTask  func(TaskRequest) SessionResult
}

// NewOrchestrator creates an Orchestrator wired to the given application
// components.
func NewOrchestrator(app *App, manager *RemoteSessionManager, sharedCtx *SharedContextStore, toolSelector *ToolSelector) *Orchestrator {
	return &Orchestrator{
		app:          app,
		manager:      manager,
		sharedCtx:    sharedCtx,
		toolSelector: toolSelector,
		activeTasks:  make(map[string]*OrchestratorTask),
	}
}

// maxQueuedSessions is the upper bound on sessions per ExecuteParallel call.
// Tasks are dispatched in bounded batches controlled by SubAgentConcurrency.
const maxQueuedSessions = 5

// ExecuteParallel queues up to 5 sessions, runs them with configured bounded
// concurrency, and returns an aggregated result.
//
// If a session fails the remaining queued sessions continue, except when the
// failure is a rate limit; rate limits stop the queue to avoid request bursts.
// The final status is "partial_failure" when at least one session failed,
// "completed" otherwise.
func (o *Orchestrator) ExecuteParallel(tasks []TaskRequest) (*OrchestratorResult, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks provided")
	}
	if len(tasks) > maxQueuedSessions {
		return nil, fmt.Errorf("too many tasks: %d exceeds maximum of %d queued sessions", len(tasks), maxQueuedSessions)
	}

	// Generate a unique task ID based on the current timestamp.
	taskID := fmt.Sprintf("orch_%d", time.Now().UnixNano())

	// Create and register the orchestrator task.
	orchTask := &OrchestratorTask{
		ID:        taskID,
		Sessions:  make([]string, 0, len(tasks)),
		Status:    orchestratorTaskStatusRunning,
		Results:   make(map[string]string),
		CreatedAt: time.Now(),
	}

	o.mu.Lock()
	if o.activeTasks == nil {
		o.activeTasks = make(map[string]*OrchestratorTask)
	}
	o.activeTasks[taskID] = orchTask
	o.mu.Unlock()

	results := make(map[string]SessionResult, len(tasks))
	concurrency := o.configuredSubAgentConcurrency()

	for start := 0; start < len(tasks); start += concurrency {
		end := start + concurrency
		if end > len(tasks) {
			end = len(tasks)
		}

		var wg sync.WaitGroup
		var resultsMu sync.Mutex
		for i := start; i < end; i++ {
			i, task := i, tasks[i]
			wg.Add(1)
			go func() {
				defer wg.Done()
				sr := o.runTask(task)
				resultsMu.Lock()
				results[fmt.Sprintf("task_%d", i)] = sr
				resultsMu.Unlock()
			}()
		}
		wg.Wait()

		rateLimitIndex := -1
		for i := start; i < end; i++ {
			sr := results[fmt.Sprintf("task_%d", i)]
			if sr.Status.IsFailed() && isSubAgentRateLimitError(sr.Error) {
				rateLimitIndex = i
				break
			}
		}
		if rateLimitIndex >= 0 {
			markQueuedTasksSkippedAfterRateLimit(results, tasks, end)
			break
		}
	}

	// Collect session IDs and determine overall status.
	hasFailure := false
	for _, sr := range results {
		if sr.SessionID != "" {
			o.mu.Lock()
			orchTask.Sessions = append(orchTask.Sessions, sr.SessionID)
			o.mu.Unlock()
		}
		if sr.Status.IsFailed() {
			hasFailure = true
		}
	}

	if hasFailure {
		o.mu.Lock()
		orchTask.Status = orchestratorTaskStatusPartialFailure
		o.mu.Unlock()
	} else {
		o.mu.Lock()
		orchTask.Status = orchestratorTaskStatusCompleted
		o.mu.Unlock()
	}

	// Remove from active tasks now that execution is done.
	o.mu.Lock()
	delete(o.activeTasks, taskID)
	o.mu.Unlock()

	summary := buildOrchestratorSummary(results)

	return &OrchestratorResult{
		TaskID:  taskID,
		Results: results,
		Summary: summary,
	}, nil
}

func (o *Orchestrator) configuredSubAgentConcurrency() int {
	if o == nil || o.app == nil {
		return 1
	}
	return o.app.GetSubAgentConcurrency()
}

func (o *Orchestrator) runTask(task TaskRequest) SessionResult {
	if o != nil && o.executeTask != nil {
		return o.executeTask(task)
	}
	return o.executeOneTask(task)
}

func markQueuedTasksSkippedAfterRateLimit(results map[string]SessionResult, tasks []TaskRequest, startIndex int) {
	for i := startIndex; i < len(tasks); i++ {
		results[fmt.Sprintf("task_%d", i)] = SessionResult{
			Tool:   tasks[i].Tool,
			Status: orchestratorSessionStatusFailed,
			Error:  "skipped: prior queued task hit an LLM rate limit",
		}
	}
}

// executeOneTask creates a remote session for a single TaskRequest, sends the
// task description as input, and returns a SessionResult.
func (o *Orchestrator) executeOneTask(tr TaskRequest) SessionResult {
	sr := SessionResult{
		Tool: tr.Tool,
	}
	toolName := normalizeRemoteToolName(tr.Tool)
	if !remoteToolSupported(toolName) {
		sr.Status = orchestratorSessionStatusFailed
		sr.Error = queuedExecuteUnsupportedToolError(toolName)
		if o.sharedCtx != nil {
			o.sharedCtx.Put(ContextEntry{
				Key:       "session_create_failed",
				Value:     fmt.Sprintf("tool=%s project=%s error=%s", toolName, tr.ProjectPath, sr.Error),
				SessionID: "",
				CreatedAt: time.Now(),
			})
		}
		return sr
	}

	view, err := o.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool:         toolName,
		ProjectPath:  tr.ProjectPath,
		LaunchSource: RemoteLaunchSourceAI,
	})
	if err != nil {
		sr.Status = orchestratorSessionStatusFailed
		sr.Error = err.Error()
		// Record failure in shared context so other sessions can see it.
		if o.sharedCtx != nil {
			o.sharedCtx.Put(ContextEntry{
				Key:       "session_create_failed",
				Value:     fmt.Sprintf("tool=%s project=%s error=%s", tr.Tool, tr.ProjectPath, err.Error()),
				SessionID: "",
				CreatedAt: time.Now(),
			})
		}
		return sr
	}

	sr.SessionID = view.ID

	// Write session-started event to shared context (requirement 13.2).
	if o.sharedCtx != nil {
		o.sharedCtx.Put(ContextEntry{
			Key:       "session_started",
			Value:     fmt.Sprintf("tool=%s project=%s", tr.Tool, tr.ProjectPath),
			SessionID: view.ID,
			CreatedAt: time.Now(),
		})
	}

	// Build input: prepend relevant shared context (requirement 13.3).
	input := o.buildInputWithContext(view.ID, tr.Description)

	// Send the task description as the first input to the session.
	if err := o.manager.WriteInput(view.ID, input); err != nil {
		sr.Status = orchestratorSessionStatusFailed
		sr.Error = fmt.Sprintf("failed to send input: %v", err)
		// Record send-failure in shared context.
		if o.sharedCtx != nil {
			o.sharedCtx.Put(ContextEntry{
				Key:       "task_result",
				Value:     fmt.Sprintf("tool=%s status=failed error=%s", tr.Tool, err.Error()),
				SessionID: view.ID,
				CreatedAt: time.Now(),
			})
		}
		return sr
	}

	sr.Status = orchestratorSessionStatusSuccess
	sr.Output = fmt.Sprintf("session %s started for tool %s", view.ID, tr.Tool)

	// Record successful task dispatch in shared context.
	if o.sharedCtx != nil {
		o.sharedCtx.Put(ContextEntry{
			Key:       "task_result",
			Value:     fmt.Sprintf("tool=%s status=success session=%s", tr.Tool, view.ID),
			SessionID: view.ID,
			CreatedAt: time.Now(),
		})
	}

	return sr
}

func queuedExecuteUnsupportedToolError(toolName string) string {
	const remoteTools = "claude, codex, opencode, gemini, cursor, codebuddy, iflow, or kilo"
	switch classifyAgentToolKind(toolName) {
	case agentToolKindBash, agentToolKindReadFile, agentToolKindWriteFile, agentToolKindEditFile, agentToolKindListDirectory,
		agentToolKindCraftTool, agentToolKindGeneratePDF, agentToolKindOffice:
		return fmt.Sprintf("tool %q is a local built-in tool and cannot be used with queued remote sessions; call it directly, or use a remote coding tool such as %s", toolName, remoteTools)
	default:
		return fmt.Sprintf("tool %q is not a remote coding tool for queued execution; use %s", toolName, remoteTools)
	}
}

// buildInputWithContext prepends relevant shared context entries to the task
// description so the session is aware of what other sessions have done.
func (o *Orchestrator) buildInputWithContext(sessionID, description string) string {
	if o.sharedCtx == nil {
		return description
	}

	entries := o.sharedCtx.GetForSession(sessionID)
	if len(entries) == 0 {
		return description
	}

	var ctx string
	for _, e := range entries {
		ctx += fmt.Sprintf("[%s] %s\n", e.Key, e.Value)
	}

	return fmt.Sprintf("[Shared Context]\n%s\n%s", ctx, description)
}

// buildOrchestratorSummary produces a human-readable summary of the queued
// execution results.
func buildOrchestratorSummary(results map[string]SessionResult) string {
	total := len(results)
	succeeded := 0
	failed := 0
	skipped := 0
	for _, sr := range results {
		if isQueuedSessionSkipped(sr) {
			skipped++
			continue
		}
		switch sr.Status.Normalized() {
		case orchestratorSessionStatusSuccess:
			succeeded++
		case orchestratorSessionStatusFailed:
			failed++
		}
	}

	if failed == 0 {
		if skipped > 0 {
			return fmt.Sprintf("%d/%d tasks completed, %d skipped", succeeded, total, skipped)
		}
		return fmt.Sprintf("all %d tasks completed successfully", total)
	}
	if skipped > 0 {
		return fmt.Sprintf("%d/%d tasks completed, %d failed, %d skipped", succeeded, total, failed, skipped)
	}
	return fmt.Sprintf("%d/%d tasks completed, %d failed", succeeded, total, failed)
}

func isQueuedSessionSkipped(result SessionResult) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(result.Error)), "skipped:")
}

// GetTask returns the OrchestratorTask for the given ID, if it exists.
func (o *Orchestrator) GetTask(taskID string) (*OrchestratorTask, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	t, ok := o.activeTasks[taskID]
	return t, ok
}
