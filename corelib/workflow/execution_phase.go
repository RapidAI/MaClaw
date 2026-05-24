package workflow

// IsExecutionOrchestratorPhase reports whether a phase represents the workflow
// execution boundary where planning is complete and full implementation tools
// should be available.
func IsExecutionOrchestratorPhase(phase PhaseTemplate) bool {
	return phase.ToolPolicy == ToolFilterFull && !phase.NeedsConfirm && !phase.DisableOrchestrator
}
