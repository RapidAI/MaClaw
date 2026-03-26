package swarm

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// SwarmOrchestrator is the core scheduler for the swarm system. It manages
// SwarmRun lifecycle, coordinates sub-components, and drives the state machine.
type SwarmOrchestrator struct {
	sessionMgr   SwarmSessionManager
	appCtx       SwarmAppContext
	llmCaller    SwarmLLMCaller
	worktreeMgr  *WorktreeManager
	conflictDet  *ConflictDetector
	taskSplitter *TaskSplitter
	mergeCtrl    *MergeController
	feedbackLoop *FeedbackLoop
	reporter     *SwarmReporter
	notifier     Notifier
	taskVerifier *TaskVerifier
	docGenerator *SwarmDocGenerator
	toolSelector *tool.Selector

	mu              sync.RWMutex
	activeRun       *SwarmRun
	runHistory      []*SwarmRun
	cachedInstalled []string
	maxRounds       int
	maxAgents       int
}

// OrchestratorOption configures a SwarmOrchestrator via functional options.
type OrchestratorOption func(*SwarmOrchestrator)

// WithAppContext sets the application context for tool discovery.
func WithAppContext(ctx SwarmAppContext) OrchestratorOption {
	return func(o *SwarmOrchestrator) { o.appCtx = ctx }
}

// WithLLMCaller sets the LLM caller for AI-powered sub-components.
func WithLLMCaller(caller SwarmLLMCaller) OrchestratorOption {
	return func(o *SwarmOrchestrator) { o.llmCaller = caller }
}

// WithMaxRounds sets the maximum feedback loop rounds.
func WithMaxRounds(n int) OrchestratorOption {
	return func(o *SwarmOrchestrator) {
		if n > 0 {
			o.maxRounds = n
		}
	}
}

// WithMaxAgents sets the maximum concurrent developer agents.
func WithMaxAgents(n int) OrchestratorOption {
	return func(o *SwarmOrchestrator) {
		if n > 0 {
			o.maxAgents = ValidateMaxAgents(n)
		}
	}
}

// NewSwarmOrchestrator creates a SwarmOrchestrator with the given session
// manager, notifier, and optional configuration. sessionMgr and notifier are
// required; appCtx and llmCaller can be injected via options.
func NewSwarmOrchestrator(
	sessionMgr SwarmSessionManager,
	notifier Notifier,
	opts ...OrchestratorOption,
) *SwarmOrchestrator {
	o := &SwarmOrchestrator{
		sessionMgr:  sessionMgr,
		notifier:    notifier,
		worktreeMgr: NewWorktreeManager(),
		conflictDet: NewConflictDetector(),
		reporter:    NewSwarmReporter(),
		docGenerator: NewSwarmDocGenerator(),
		toolSelector: tool.NewSelector(),
		maxRounds:   5,
		maxAgents:   5,
	}

	for _, opt := range opts {
		opt(o)
	}

	// Wire up LLM-dependent sub-components.
	o.taskSplitter = NewTaskSplitter(o.llmCaller)
	o.mergeCtrl = NewMergeController(o.worktreeMgr)
	o.feedbackLoop = NewFeedbackLoop(o.llmCaller, o.maxRounds)
	o.taskVerifier = NewTaskVerifier(o.llmCaller)

	return o
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

// installedToolNames returns the names of tools that are currently installed
// and available on this machine. The result is cached for the lifetime of the
// orchestrator to avoid repeated lookups during concurrent agent scheduling.
func (o *SwarmOrchestrator) installedToolNames() []string {
	o.mu.RLock()
	if o.cachedInstalled != nil {
		result := o.cachedInstalled
		o.mu.RUnlock()
		return result
	}
	o.mu.RUnlock()

	if o.appCtx == nil {
		return nil
	}
	tools := o.appCtx.ListInstalledTools()
	var names []string
	for _, t := range tools {
		if t.CanStart {
			names = append(names, t.Name)
		}
	}

	o.mu.Lock()
	o.cachedInstalled = names
	o.mu.Unlock()
	return names
}

// selectToolForTask picks the best tool for a given sub-task. If the run
// specifies a fixed tool, that tool is always used. Otherwise the Selector
// recommends one based on the task description and installed tools.
func (o *SwarmOrchestrator) selectToolForTask(run *SwarmRun, task SubTask) (string, string) {
	if run.Tool != "" {
		return run.Tool, "用户指定工具"
	}
	if o.toolSelector == nil {
		return "claude", "默认工具"
	}
	installed := o.installedToolNames()
	desc := task.Description
	if run.TechStack != "" {
		desc += " " + run.TechStack
	}
	return o.toolSelector.Recommend(desc, installed)
}

// InferTestCommand returns a test command based on the tech stack string.
func InferTestCommand(techStack string) string {
	ts := strings.ToLower(techStack)
	switch {
	case strings.Contains(ts, "go"):
		return "go test ./..."
	case strings.Contains(ts, "rust"):
		return "cargo test"
	case strings.Contains(ts, "node") || strings.Contains(ts, "typescript") || strings.Contains(ts, "javascript"):
		return "npm test"
	case strings.Contains(ts, "python"):
		return "pytest"
	case strings.Contains(ts, "java"):
		return "mvn test"
	default:
		return "echo no test command configured"
	}
}

// ---------------------------------------------------------------------------
// Lifecycle methods
// ---------------------------------------------------------------------------

// StartSwarmRun creates and starts a new swarm run. Returns an error if
// there is already an active run or if preconditions are not met.
func (o *SwarmOrchestrator) StartSwarmRun(req SwarmRunRequest) (*SwarmRun, error) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.activeRun != nil && o.activeRun.Status == SwarmStatusRunning {
		return nil, fmt.Errorf("a swarm run is already active: %s", o.activeRun.ID)
	}

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
	maxAgents = ValidateMaxAgents(maxAgents)

	maxRounds := req.MaxRounds
	if maxRounds <= 0 {
		maxRounds = o.maxRounds
	}

	run := &SwarmRun{
		ID:          NewSwarmRunID(),
		Mode:        req.Mode,
		Status:      SwarmStatusPending,
		ProjectPath: req.ProjectPath,
		TechStack:   req.TechStack,
		Tool:        req.Tool,
		MaxRounds:   maxRounds,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		UserInputCh: make(chan string, 1),
	}

	o.activeRun = run
	o.runHistory = append(o.runHistory, run)

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

	var activeSessionIDs []string
	for _, agent := range run.Agents {
		if agent.Status == "running" && agent.SessionID != "" {
			activeSessionIDs = append(activeSessionIDs, agent.SessionID)
		}
	}
	o.mu.Unlock()

	if o.sessionMgr != nil {
		for _, sid := range activeSessionIDs {
			_ = o.sessionMgr.Kill(sid)
		}
	}

	_ = o.worktreeMgr.CleanupRun(run.ProjectPath, run.ID)

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
	case run.UserInputCh <- input:
		return nil
	default:
		return fmt.Errorf("run %s is not waiting for input", runID)
	}
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SwarmOrchestrator] runPipeline panic recovered: %v", r)
			o.mu.Lock()
			run.Status = SwarmStatusFailed
			now := time.Now()
			run.CompletedAt = &now
			run.UpdatedAt = now
			o.activeRun = nil
			o.mu.Unlock()
			o.addTimelineEvent(run, "run_panic", fmt.Sprintf("Pipeline panic: %v", r), "")
		}
	}()

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

	report, _ := o.reporter.GenerateReport(run)
	if report != nil {
		_ = o.reporter.WriteReportFiles(run.ProjectPath, report)
	}
	_ = o.notifier.NotifyRunComplete(run, report)
	o.addTimelineEvent(run, "run_complete", fmt.Sprintf("Run completed: %s", run.Status), "")
}

// SetIMDelivery sets IM delivery callbacks on the notifier, enabling Swarm
// documents and notifications to be sent through IM channels.
func (o *SwarmOrchestrator) SetIMDelivery(fileFn IMFileDeliveryFunc, textFn func(string)) {
	if dn, ok := o.notifier.(*DefaultNotifier); ok {
		dn.SetIMDelivery(fileFn, textFn)
	}
}

// ClearIMDelivery clears IM delivery callbacks.
func (o *SwarmOrchestrator) ClearIMDelivery() {
	if dn, ok := o.notifier.(*DefaultNotifier); ok {
		dn.SetIMDelivery(nil, nil)
	}
}
