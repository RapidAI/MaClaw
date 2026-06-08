package workflow

// IsExecutionOrchestratorPhase reports whether a phase represents the workflow
// execution boundary where planning is complete and full implementation tools
// should be available.
func IsExecutionOrchestratorPhase(phase PhaseTemplate) bool {
	return DerivePhaseContract(nil, phase).ActivatesOrchestrator
}

// IsTemplatePhaseExecutionOrchestrator reports whether a phase is the
// template-declared project execution boundary. Unlike the legacy
// IsExecutionOrchestratorPhase helper, this uses workflow type and phase ID so
// artifact-generation phases with ToolFilterFull do not look like coding
// implementation phases.
func IsTemplatePhaseExecutionOrchestrator(tmpl *WorkflowTemplate, phase PhaseTemplate) bool {
	return DerivePhaseContract(tmpl, phase).ActivatesOrchestrator
}
