package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestNormalizeWorkflowStateForFrontendCanonicalizesAllPhaseFields(t *testing.T) {
	state := &workflow.WorkflowState{
		CurrentPhase:         "tech_design",
		PendingReviewPhaseID: "task_breakdown",
		PhaseOutputs: map[string]string{
			"requirements":   "requirements doc",
			"tech_design":    "design doc",
			"task_breakdown": "tasks doc",
		},
		GateResults: map[string]*workflow.QualityGateResult{
			"tech_design": {
				PhaseID: "tech_design",
				Passed:  true,
			},
		},
	}

	got := normalizeWorkflowStateForFrontend(state)

	if got.CurrentPhase != "design" {
		t.Fatalf("CurrentPhase = %q, want design", got.CurrentPhase)
	}
	if got.PendingReviewPhaseID != "tasks" {
		t.Fatalf("PendingReviewPhaseID = %q, want tasks", got.PendingReviewPhaseID)
	}
	if got.PhaseOutputs["design"] != "design doc" {
		t.Fatalf("design output missing after normalization: %#v", got.PhaseOutputs)
	}
	if got.PhaseOutputs["tasks"] != "tasks doc" {
		t.Fatalf("tasks output missing after normalization: %#v", got.PhaseOutputs)
	}
	if _, ok := got.PhaseOutputs["tech_design"]; ok {
		t.Fatalf("raw tech_design key leaked to frontend: %#v", got.PhaseOutputs)
	}
	if got.GateResults["design"] == nil || got.GateResults["design"].PhaseID != "design" {
		t.Fatalf("design gate result not canonicalized: %#v", got.GateResults["design"])
	}
}

func TestNormalizeWorkflowStateForFrontendIncludesCanonicalTemplatePhases(t *testing.T) {
	registry := workflow.NewWorkflowRegistry()
	state := &workflow.WorkflowState{
		Type:         workflow.WorkflowCoding,
		CurrentPhase: "tech_design",
	}

	got := normalizeWorkflowStateForFrontendWithRegistry(state, registry)

	if len(got.Phases) != 5 {
		t.Fatalf("len(Phases) = %d, want 5: %#v", len(got.Phases), got.Phases)
	}
	if got.Phases[1].ID != "design" {
		t.Fatalf("second phase ID = %q, want design: %#v", got.Phases[1].ID, got.Phases)
	}
	if got.Phases[2].ID != "tasks" {
		t.Fatalf("third phase ID = %q, want tasks: %#v", got.Phases[2].ID, got.Phases)
	}
	if got.Phases[1].Name == "" || got.Phases[2].Name == "" {
		t.Fatalf("phase names should be populated: %#v", got.Phases)
	}
	if !got.Phases[0].ExpectsDocument || !got.Phases[1].ExpectsDocument || !got.Phases[2].ExpectsDocument {
		t.Fatalf("planning phases should expect preview documents: %#v", got.Phases)
	}
	if got.Phases[3].ID != "implementation" || got.Phases[3].ExpectsDocument {
		t.Fatalf("implementation should be marked as a non-document phase: %#v", got.Phases[3])
	}
	if !got.Phases[4].ExpectsDocument {
		t.Fatalf("review should expect a preview document: %#v", got.Phases[4])
	}
}

func TestNormalizeWorkflowStateForFrontendDoesNotMutateEngineState(t *testing.T) {
	state := &workflow.WorkflowState{
		CurrentPhase: "tech_design",
		PhaseOutputs: map[string]string{
			"tech_design": "design doc",
		},
		GateResults: map[string]*workflow.QualityGateResult{
			"tech_design": {PhaseID: "tech_design"},
		},
	}

	got := normalizeWorkflowStateForFrontend(state)
	got.PhaseOutputs["design"] = "changed"
	got.GateResults["design"].PhaseID = "changed"

	if state.CurrentPhase != "tech_design" {
		t.Fatalf("original CurrentPhase mutated: %q", state.CurrentPhase)
	}
	if state.PhaseOutputs["tech_design"] != "design doc" {
		t.Fatalf("original PhaseOutputs mutated: %#v", state.PhaseOutputs)
	}
	if state.GateResults["tech_design"].PhaseID != "tech_design" {
		t.Fatalf("original GateResults mutated: %#v", state.GateResults["tech_design"])
	}
}
