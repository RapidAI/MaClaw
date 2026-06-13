// Package main provides lightweight V2→V1 type mapping helpers for GUI code
// that still consumes V1 workflow.ToolFilterPolicy and workflow.WorkflowState types.
// These are standalone functions (not methods on a facade struct).
package main

import (
	"github.com/RapidAI/CodeClaw/corelib/workflow"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// mapV2ToolPolicyToV1 converts V2 ToolPolicy to V1 ToolFilterPolicy.
func mapV2ToolPolicyToV1(policy v2.ToolPolicy) workflow.ToolFilterPolicy {
	switch policy {
	case v2.ToolPolicyDocOnly:
		return workflow.ToolFilterDocOnly
	case v2.ToolPolicyFull:
		return workflow.ToolFilterFull
	default:
		return workflow.ToolFilterNone
	}
}

// mapV2StateToV1 converts a V2 WorkflowState to the V1 WorkflowState type
// used by GUI code (e.g. handlePostStartWorkflow).
func mapV2StateToV1(s *v2.WorkflowState) *workflow.WorkflowState {
	if s == nil {
		return nil
	}

	// Map V2 status to V1
	var status workflow.WorkflowStatus
	switch s.Status {
	case v2.StatusActive:
		status = workflow.WorkflowActive
	case v2.StatusCompleted:
		status = workflow.WorkflowCompleted
	case v2.StatusCancelled:
		status = workflow.WorkflowCancelled
	default:
		status = workflow.WorkflowActive
	}

	// Map current phase ID
	currentPhaseID := ""
	if s.CurrentPhase >= 0 && s.CurrentPhase < len(s.Phases) {
		currentPhaseID = s.Phases[s.CurrentPhase].ID
	}

	// Build phase outputs from completed phases
	phaseOutputs := make(map[string]string)
	for _, p := range s.Phases {
		if p.Output != "" {
			phaseOutputs[p.ID] = p.Output
		}
	}

	return &workflow.WorkflowState{
		ID:           s.ID,
		UserID:       s.UserID,
		Type:         workflow.WorkflowType(s.Type),
		CurrentPhase: currentPhaseID,
		PhaseIndex:   s.CurrentPhase,
		PhaseOutputs: phaseOutputs,
		Status:       status,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		ProjectPath:  s.ProjectPath,
		Intent: workflow.StructuredIntent{
			Category: workflow.WorkflowType(s.Type),
			Summary:  s.Summary,
		},
	}
}
