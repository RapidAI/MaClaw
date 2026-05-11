package main

import "github.com/RapidAI/CodeClaw/corelib/workflow"

type frontendWorkflowPhase struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Index           int    `json:"index"`
	ExpectsDocument bool   `json:"expects_document"`
}

type frontendWorkflowState struct {
	*workflow.WorkflowState
	Phases []frontendWorkflowPhase `json:"phases,omitempty"`
}

func canonicalWorkflowPhaseID(phaseID string) string {
	if canonical := normalizeWorkflowPhaseID(phaseID); canonical != "" {
		return canonical
	}
	return phaseID
}

func normalizeWorkflowStateForFrontend(state *workflow.WorkflowState) *frontendWorkflowState {
	return normalizeWorkflowStateForFrontendWithRegistry(state, nil)
}

func normalizeWorkflowStateForFrontendWithRegistry(state *workflow.WorkflowState, registry *workflow.WorkflowRegistry) *frontendWorkflowState {
	if state == nil {
		return nil
	}
	cp := *state
	cp.CurrentPhase = canonicalWorkflowPhaseID(cp.CurrentPhase)
	cp.PendingReviewPhaseID = canonicalWorkflowPhaseID(cp.PendingReviewPhaseID)
	cp.PhaseOutputs = normalizeWorkflowPhaseOutputs(state.PhaseOutputs)
	cp.GateResults = normalizeWorkflowGateResults(state.GateResults)
	return &frontendWorkflowState{
		WorkflowState: &cp,
		Phases:        normalizeWorkflowPhasesForFrontend(state.Type, registry),
	}
}

func normalizeWorkflowPhasesForFrontend(workflowType workflow.WorkflowType, registry *workflow.WorkflowRegistry) []frontendWorkflowPhase {
	if registry == nil {
		return nil
	}
	tmpl := registry.Match(workflowType)
	if tmpl == nil || len(tmpl.Phases) == 0 {
		return nil
	}
	phases := make([]frontendWorkflowPhase, 0, len(tmpl.Phases))
	seen := make(map[string]bool, len(tmpl.Phases))
	for _, phase := range tmpl.Phases {
		id := canonicalWorkflowPhaseID(phase.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		phases = append(phases, frontendWorkflowPhase{
			ID:              id,
			Name:            phase.Name,
			Index:           len(phases),
			ExpectsDocument: phase.ToolPolicy != workflow.ToolFilterFull && phase.ToolPolicy != workflow.ToolFilterOpsControlled,
		})
	}
	return phases
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
