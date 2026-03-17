package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// SwarmOrchestrator is the core scheduler for the swarm system. It manages
// SwarmRun lifecycle, coordinates sub-components, and drives the state machine.
type SwarmOrchestrator struct {
	app          *App
	manager      *RemoteSessionManager
	sharedCtx    *SharedContextStore
	worktreeMgr  *WorktreeManager
	conflictDet  *ConflictDetector
	taskSplitter *TaskSplitter
	mergeCtrl    *MergeController
	feedbackLoop *FeedbackLoop
	reporter     *SwarmReporter
	notifier     SwarmNotifier

	mu         sync.RWMutex
	activeRun  *SwarmRun
	runHistory []*SwarmRun
	maxRounds  int // default 5
	maxAgents  int // default 5
}

// NewSwarmOrchestrator creates a SwarmOrchestrator with all dependencies.
func NewSwarmOrchestrator(
	app *App,
	manager *RemoteSessionManager,
	sharedCtx *SharedContextStore,
	scanner *ProjectScanner,
	notifier SwarmNotifier,
	llmCfg MaclawLLMConfig,
) *SwarmOrchestrator {
	wm := NewWorktreeManager()
	return &SwarmOrchestrator{
		app:          app,
		manager:      manager,
		sharedCtx:    sharedCtx,
		worktreeMgr:  wm,
		conflictDet:  NewConflictDetector(scanner),
		taskSplitter: NewTaskSplitter(llmCfg),
		mergeCtrl:    NewMergeController(wm),
		feedbackLoop: NewFeedbackLoop(llmCfg, 5),
		reporter:     NewSwarmReporter(),
		notifier:     notifier,
		maxRounds:    5,
		maxAgents:    5,
	}
}

// StartSwarmRun creates and starts a new swarm run. Returns an error if
// there is already an active run or if preconditions are not met.
func (o *SwarmOrchestrator) StartSwarmRun(req SwarmRunRequest) (*SwarmRun, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Single run limit
	if o.activeRun != nil && o.activeRun.Status == SwarmStatusRunning {
		return nil, fmt.Errorf("a swarm run is already active: %s", o.activeRun.ID)
	}

	// Validate request
	if req.ProjectPath == "" {
		return nil, fmt.Errorf("project path is required")
	}
	if req.Mode == SwarmModeGreenfield && req.Requirements == "" {
		return nil, fmt.Errorf("requirements are required for greenfield mode")
	}
	if req.Mode == SwarmModeMaintenance && req.TaskInput == nil {
		return nil, fmt.Errorf("task input is required for maintenance mode")
	}

	maxAgents := req.MaxAgents
	if maxAgents <= 0 {
		maxAgents = o.maxAgents
	}
	if maxAgents < 1 {
		maxAgents = 1
	}
	if maxAgents > 10 {
		maxAgents = 10
	}

	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = o.maxRounds
	}

	run := &SwarmRun{
		ID:           NewSwarmRunID(),
		Mode:         req.Mode,
		Status:       SwarmStatusPending,
		ProjectPath:  req.ProjectPath,
		TechStack:    req.TechStack,
		Tool:         req.Tool,
		MaxRounds:    maxRounds,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		userInputCh:  make(chan string, 1),
	}

	o.activeRun = run
	o.runHistory = append(o.runHistory, run)

	// Start the pipeline in a goroutine
	go o.runPipeline(run, req, maxAgents)

	return run, nil
}

// PauseSwarmRun pauses the active run.
func (o *SwarmOrchestrator) PauseSwarmRun(runID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.activeRun == nil || o.activeRun.ID != runID {
		return fmt.Errorf("run %s not found or not active", runID)
	}
	if o.activeRun.Status != SwarmStatusRunning {
		return fmt.Errorf("run %s is not running (status: %s)", runID, o.activeRun.Status)
	}

	o.activeRun.Status = SwarmStatusPaused
	o.activeRun.UpdatedAt = time.Now()
	return nil
}

// ResumeSwarmRun resumes a paused run.
func (o *SwarmOrchestrator) ResumeSwarmRun(runID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.activeRun == nil || o.activeRun.ID != runID {
		return fmt.Errorf("run %s not found or not active", runID)
	}
	if o.activeRun.Status != SwarmStatusPaused {
		return fmt.Errorf("run %s is not paused (status: %s)", runID, o.activeRun.Status)
	}

	o.activeRun.Status = SwarmStatusRunning
	o.activeRun.UpdatedAt = time.Now()
	return nil
}

// CancelSwarmRun cancels a run, kills all active agents, cleans up worktrees,
// and generates a partial report.
func (o *SwarmOrchestrator) CancelSwarmRun(runID string) error {
	o.mu.Lock()
	run := o.activeRun
	if run == nil || run.ID != runID {
		o.mu.Unlock()
		return fmt.Errorf("run %s not found", runID)
	}

	// Snapshot active agent sessions under lock
	var activeSessionIDs []string
	for _, agent := range run.Agents {
		if agent.Status == "running" && agent.SessionID != "" {
			activeSessionIDs = append(activeSessionIDs, agent.SessionID)
		}
	}
	o.mu.Unlock()

	// Kill all active agent sessions (outside lock — Kill may block)
	for _, sid := range activeSessionIDs {
		_ = o.manager.Kill(sid)
	}

	// Cleanup worktrees
	_ = o.worktreeMgr.CleanupRun(run.ProjectPath, run.ID)

	// Restore project state
	if run.ProjectState != nil {
		_ = o.worktreeMgr.RestoreProject(run.ProjectPath, run.ProjectState)
	}

	o.mu.Lock()
	run.Status = SwarmStatusCancelled
	now := time.Now()
	run.CompletedAt = &now
	run.UpdatedAt = now
	o.activeRun = nil
	o.mu.Unlock()

	// Generate partial report
	report, _ := o.reporter.GenerateReport(run)
	if report != nil {
		_ = o.reporter.WriteReportFiles(run.ProjectPath, report)
		_ = o.notifier.NotifyRunComplete(run, report)
	}

	return nil
}

// ListSwarmRuns returns summaries of all runs (including history).
func (o *SwarmOrchestrator) ListSwarmRuns() []SwarmRunSummary {
	o.mu.RLock()
	defer o.mu.RUnlock()

	summaries := make([]SwarmRunSummary, len(o.runHistory))
	for i, run := range o.runHistory {
		summaries[i] = SwarmRunSummary{
			ID:        run.ID,
			Mode:      run.Mode,
			Status:    run.Status,
			Phase:     run.Phase,
			TaskCount: len(run.Tasks),
			Round:     run.CurrentRound,
			CreatedAt: run.CreatedAt,
		}
	}
	return summaries
}

// GetSwarmRun returns the full details of a specific run.
func (o *SwarmOrchestrator) GetSwarmRun(runID string) (*SwarmRun, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	for _, run := range o.runHistory {
		if run.ID == runID {
			return run, nil
		}
	}
	return nil, fmt.Errorf("run %s not found", runID)
}

// ProvideUserInput sends user input to a paused run waiting for confirmation.
func (o *SwarmOrchestrator) ProvideUserInput(runID, input string) error {
	o.mu.RLock()
	run := o.activeRun
	o.mu.RUnlock()

	if run == nil || run.ID != runID {
		return fmt.Errorf("run %s not found", runID)
	}

	select {
	case run.userInputCh <- input:
		return nil
	default:
		return fmt.Errorf("run %s is not waiting for input", runID)
	}
}

// addTimelineEvent appends an event to the run's timeline.
// Caller must NOT hold o.mu — this method acquires it internally.
func (o *SwarmOrchestrator) addTimelineEvent(run *SwarmRun, eventType, message string, agentID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	run.Timeline = append(run.Timeline, TimelineEvent{
		Timestamp: time.Now(),
		Type:      eventType,
		Message:   message,
		AgentID:   agentID,
		Phase:     string(run.Phase),
	})
}

// setPhase transitions the run to a new phase and notifies.
// Caller must NOT hold o.mu — this method acquires it internally.
func (o *SwarmOrchestrator) setPhase(run *SwarmRun, phase SwarmPhase) {
	o.mu.Lock()
	run.Phase = phase
	run.UpdatedAt = time.Now()
	run.Timeline = append(run.Timeline, TimelineEvent{
		Timestamp: time.Now(),
		Type:      "phase_change",
		Message:   fmt.Sprintf("Entered phase: %s", phase),
		Phase:     string(phase),
	})
	o.mu.Unlock()
	_ = o.notifier.NotifyPhaseChange(run, phase)
}

// runPipeline drives the swarm run through its phases.
func (o *SwarmOrchestrator) runPipeline(run *SwarmRun, req SwarmRunRequest, maxAgents int) {
	o.mu.Lock()
	run.Status = SwarmStatusRunning
	o.mu.Unlock()

	o.addTimelineEvent(run, "run_start", "Swarm run started", "")

	var err error
	switch run.Mode {
	case SwarmModeGreenfield:
		err = o.runGreenfield(run, req, maxAgents)
	case SwarmModeMaintenance:
		err = o.runMaintenance(run, req, maxAgents)
	default:
		err = fmt.Errorf("unknown mode: %s", run.Mode)
	}

	if err != nil {
		o.addTimelineEvent(run, "run_error", err.Error(), "")
		log.Printf("[SwarmOrchestrator] run %s failed: %v", run.ID, err)
	}

	o.mu.Lock()
	if err != nil {
		run.Status = SwarmStatusFailed
	} else {
		run.Status = SwarmStatusCompleted
	}
	now := time.Now()
	run.CompletedAt = &now
	run.UpdatedAt = now
	o.activeRun = nil
	o.mu.Unlock()

	// Generate final report
	report, _ := o.reporter.GenerateReport(run)
	if report != nil {
		_ = o.reporter.WriteReportFiles(run.ProjectPath, report)
	}
	_ = o.notifier.NotifyRunComplete(run, report)
	o.addTimelineEvent(run, "run_complete", fmt.Sprintf("Run completed: %s", run.Status), "")
}

// ValidateMaxAgents clamps the value to [1, 10].
func ValidateMaxAgents(n int) int {
	if n < 1 {
		return 1
	}
	if n > 10 {
		return 10
	}
	return n
}
