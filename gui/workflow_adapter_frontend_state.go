package main

import workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"

// frontendWorkflowState is the workflow state shape emitted to the frontend on
// the workflow:phase_update event. It embeds the canonicalized engine state and
// attaches the dashboard-ready phase metadata derived from the backend template.
//
// Phases carries workflow.PhaseMeta (the single serialized shape produced by the
// one workflow.PhaseMetadata deriver). It is omitempty so that, when the registry
// is unavailable, the field is absent and the dashboard degrades to its fallback
// maps.
type frontendWorkflowState struct {
	*workflow.EngineState
	Phases []workflow.PhaseMeta `json:"phases,omitempty"`
}

func canonicalWorkflowPhaseID(phaseID string) string {
	if canonical := normalizeWorkflowPhaseID(phaseID); canonical != "" {
		return canonical
	}
	return phaseID
}

func normalizeWorkflowStateForFrontend(state *workflow.EngineState) *frontendWorkflowState {
	return normalizeWorkflowStateForFrontendWithRegistry(state, nil)
}

func normalizeWorkflowStateForFrontendWithRegistry(state *workflow.EngineState, registry *workflow.WorkflowRegistry) *frontendWorkflowState {
	if state == nil {
		return nil
	}
	cp := *state
	cp.CurrentPhase = canonicalWorkflowPhaseID(cp.CurrentPhase)
	cp.PendingReviewPhaseID = canonicalWorkflowPhaseID(cp.PendingReviewPhaseID)
	cp.PhaseOutputs = normalizeWorkflowPhaseOutputs(state.PhaseOutputs)
	cp.GateResults = normalizeWorkflowGateResults(state.GateResults)

	// Derive dashboard phase metadata through the single workflow.PhaseMetadata
	// deriver. A nil registry leaves Phases nil so the omitempty field is dropped
	// and the dashboard falls back to its hardcoded maps (degraded mode).
	var phases []workflow.PhaseMeta
	if registry != nil {
		phases = workflow.PhaseMetadata(registry.Match(state.Type))
	}
	return &frontendWorkflowState{
		EngineState: &cp,
		Phases:      phases,
	}
}

func normalizeWorkflowPhaseOutputs(outputs map[string]string) map[string]string {
	if outputs == nil {
		return nil
	}
	normalized := make(map[string]string, len(outputs))
	for phaseID, content := range outputs {
		canonical := canonicalWorkflowPhaseID(phaseID)
		if _, exists := normalized[canonical]; exists && phaseID != canonical {
			continue
		}
		normalized[canonical] = content
	}
	return normalized
}

func normalizeWorkflowGateResults(results map[string]*workflow.QualityGateResult) map[string]*workflow.QualityGateResult {
	if results == nil {
		return nil
	}
	normalized := make(map[string]*workflow.QualityGateResult, len(results))
	for phaseID, result := range results {
		canonical := canonicalWorkflowPhaseID(phaseID)
		if _, exists := normalized[canonical]; exists && phaseID != canonical {
			continue
		}
		if result == nil {
			normalized[canonical] = nil
			continue
		}
		cp := *result
		cp.PhaseID = canonicalWorkflowPhaseID(cp.PhaseID)
		normalized[canonical] = &cp
	}
	return normalized
}
