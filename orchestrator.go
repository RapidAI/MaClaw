package main

import (
	"fmt"
	"sync"
	"time"
)

// SessionResult holds the outcome of a single session within an orchestrated
// parallel execution.
type SessionResult struct {
	SessionID string `json:"session_id"`
	Tool      string `json:"tool"`
	Status    string `json:"status"` // "success" or "failed"
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
}

// TaskRequest describes a single unit of work to be executed in parallel by
// the Orchestrator.
type TaskRequest struct {
	Tool        string `json:"tool"`
	Description string `json:"description"`
	ProjectPath string `json:"project_path"`
}

// OrchestratorTask tracks the lifecycle of a parallel execution batch.
type OrchestratorTask struct {
	ID        string
	Sessions  []string // session IDs created for this task
	Status    string   // "running", "completed", "partial_failure"
	Results   map[string]string
	CreatedAt time.Time
}

// OrchestratorResult is the aggregated outcome returned by ExecuteParallel.
type OrchestratorResult struct {
	TaskID  string
	Results map[string]SessionResult
	Summary string
}

// Orchestrator coordinates parallel execution of multiple programming-tool
// sessions, tracks their status, and aggregates results.
type Orchestrator struct {
	app          *App
	manager      *RemoteSessionManager
	sharedCtx    *SharedContextStore
	toolSelector *ToolSelector
	mu           sync.RWMutex
	activeTasks  map[string]*OrchestratorTask
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

// maxParallelSessions is the upper bound on concurrent sessions per
// ExecuteParallel call (requirement 12.1).
const maxParallelSessions = 5

// ExecuteParallel launches up to 5 sessions in parallel — one per TaskRequest
// — waits for all of them to finish, and returns an aggregated result.
//
// If any individual session fails the others continue; the final status is
// "partial_failure" when at least one session failed, "completed" otherwise.
func (o *Orchestrator) ExecuteParallel(tasks []TaskRequest) (*OrchestratorResult, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks provided")
	}
	if len(tasks) > maxParallelSessions {
		return nil, fmt.Errorf("too many tasks: %d exceeds maximum of %d parallel sessions", len(tasks), maxParallelSessions)
	}

	// Generate a unique task ID based on the current timestamp.
	taskID := fmt.Sprintf("orch_%d", time.Now().UnixNano())

	// Create and register the orchestrator task.
	orchTask := &OrchestratorTask{
		ID:        taskID,
		Sessions:  make([]string, 0, len(tasks)),
		Status:    "running",
		Results:   make(map[string]string),
		CreatedAt: time.Now(),
	}

	o.mu.Lock()
	o.activeTasks[taskID] = orchTask
	o.mu.Unlock()

	// results is written to by goroutines; protected by resultsMu.
	results := make(map[string]SessionResult, len(tasks))
	var resultsMu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(len(tasks))

	for i, task := range tasks {
		go func(idx int, tr TaskRequest) {
			defer wg.Done()

			sr := o.executeOneTask(tr)

			resultsMu.Lock()
			key := fmt.Sprintf("task_%d", idx)
			results[key] = sr
			resultsMu.Unlock()
		}(i, task)
	}

	wg.Wait()

	// Collect session IDs and determine overall status.
	hasFailure := false
	for _, sr := range results {
		if sr.SessionID != "" {
			o.mu.Lock()
			orchTask.Sessions = append(orchTask.Sessions, sr.SessionID)
			o.mu.Unlock()
		}
		if sr.Status == "failed" {
			hasFailure = true
		}
	}

	if hasFailure {
		orchTask.Status = "partial_failure"
	} else {
		orchTask.Status = "completed"
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

// executeOneTask creates a remote session for a single TaskRequest, sends the
// task description as input, and returns a SessionResult.
func (o *Orchestrator) executeOneTask(tr TaskRequest) SessionResult {
	sr := SessionResult{
		Tool: tr.Tool,
	}

	view, err := o.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool:         tr.Tool,
		ProjectPath:  tr.ProjectPath,
		LaunchSource: RemoteLaunchSourceAI,
	})
	if err != nil {
		sr.Status = "failed"
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
		sr.Status = "failed"
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

	sr.Status = "success"
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

// buildOrchestratorSummary produces a human-readable summary of the parallel
// execution results.
func buildOrchestratorSummary(results map[string]SessionResult) string {
	total := len(results)
	succeeded := 0
	failed := 0
	for _, sr := range results {
		switch sr.Status {
		case "success":
			succeeded++
		case "failed":
			failed++
		}
	}

	if failed == 0 {
		return fmt.Sprintf("all %d tasks completed successfully", total)
	}
	return fmt.Sprintf("%d/%d tasks completed, %d failed", succeeded, total, failed)
}

// GetTask returns the OrchestratorTask for the given ID, if it exists.
func (o *Orchestrator) GetTask(taskID string) (*OrchestratorTask, bool) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	t, ok := o.activeTasks[taskID]
	return t, ok
}
