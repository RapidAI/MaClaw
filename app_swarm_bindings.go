package main

import (
	"fmt"
	"sync"
)

var errSwarmNotInit = fmt.Errorf("swarm orchestrator not initialised")

var swarmInitOnce sync.Once

// ---------------------------------------------------------------------------
// Wails frontend bindings for SwarmOrchestrator
// ---------------------------------------------------------------------------

// StartSwarmRun starts a new swarm run (exposed to frontend).
func (a *App) StartSwarmRun(req SwarmRunRequest) (*SwarmRun, error) {
	a.ensureSwarmOrchestrator()
	return a.swarmOrchestrator.StartSwarmRun(req)
}

// PauseSwarmRun pauses the active swarm run.
func (a *App) PauseSwarmRun(runID string) error {
	if a.swarmOrchestrator == nil {
		return errSwarmNotInit
	}
	return a.swarmOrchestrator.PauseSwarmRun(runID)
}

// ResumeSwarmRun resumes a paused swarm run.
func (a *App) ResumeSwarmRun(runID string) error {
	if a.swarmOrchestrator == nil {
		return errSwarmNotInit
	}
	return a.swarmOrchestrator.ResumeSwarmRun(runID)
}

// CancelSwarmRun cancels a swarm run.
func (a *App) CancelSwarmRun(runID string) error {
	if a.swarmOrchestrator == nil {
		return errSwarmNotInit
	}
	return a.swarmOrchestrator.CancelSwarmRun(runID)
}

// ListSwarmRuns returns summaries of all swarm runs.
func (a *App) ListSwarmRuns() []SwarmRunSummary {
	if a.swarmOrchestrator == nil {
		return nil
	}
	return a.swarmOrchestrator.ListSwarmRuns()
}

// GetSwarmRun returns details of a specific swarm run.
func (a *App) GetSwarmRun(runID string) (*SwarmRun, error) {
	if a.swarmOrchestrator == nil {
		return nil, errSwarmNotInit
	}
	return a.swarmOrchestrator.GetSwarmRun(runID)
}

// ProvideSwarmUserInput sends user input to a waiting swarm run.
func (a *App) ProvideSwarmUserInput(runID, input string) error {
	if a.swarmOrchestrator == nil {
		return errSwarmNotInit
	}
	return a.swarmOrchestrator.ProvideUserInput(runID, input)
}

// ensureSwarmOrchestrator lazily initialises the SwarmOrchestrator (thread-safe).
func (a *App) ensureSwarmOrchestrator() {
	swarmInitOnce.Do(func() {
		a.ensureRemoteInfra()
		llmCfg := a.GetMaclawLLMConfig()
		notifier := NewDefaultSwarmNotifier(a)
		a.swarmOrchestrator = NewSwarmOrchestrator(
			a,
			a.remoteSessions,
			a.sharedContext,
			a.projectScanner,
			notifier,
			llmCfg,
		)
	})
}
