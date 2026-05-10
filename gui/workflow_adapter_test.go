package main

import (
	"os"
	"path/filepath"
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

func TestNormalizeWorkflowStateForFrontendIncludesOpsMaintenancePhases(t *testing.T) {
	registry := workflow.NewWorkflowRegistry()
	state := &workflow.WorkflowState{
		Type:         workflow.WorkflowOpsMaintenance,
		CurrentPhase: "risk_policy",
	}

	got := normalizeWorkflowStateForFrontendWithRegistry(state, registry)

	wantIDs := []string{"ops_intake", "readonly_collection", "artifact_plan", "risk_policy", "controlled_execution"}
	if len(got.Phases) != len(wantIDs) {
		t.Fatalf("len(Phases) = %d, want %d: %#v", len(got.Phases), len(wantIDs), got.Phases)
	}
	for i, wantID := range wantIDs {
		if got.Phases[i].ID != wantID {
			t.Fatalf("phase %d ID = %q, want %q: %#v", i, got.Phases[i].ID, wantID, got.Phases)
		}
		wantDocument := i < len(wantIDs)-1
		if got.Phases[i].ExpectsDocument != wantDocument {
			t.Fatalf("phase %s ExpectsDocument = %v, want %v", wantID, got.Phases[i].ExpectsDocument, wantDocument)
		}
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

func TestEmitDocUpdateUsesExplicitPhaseIDWithoutContentInference(t *testing.T) {
	dir := t.TempDir()
	adapter := NewGUIWorkflowAdapter(&App{}, nil)
	adapter.SetWorkingDir("u1", dir)

	requirementsContent := "# Technical Design\n\nThis title must not move the document."
	if err := adapter.EmitDocUpdate("u1", "requirements", requirementsContent); err != nil {
		t.Fatalf("EmitDocUpdate requirements failed: %v", err)
	}
	designContent := "# Tasks\n\nThis title must not move the document either."
	if err := adapter.EmitDocUpdate("u1", "design", designContent); err != nil {
		t.Fatalf("EmitDocUpdate design failed: %v", err)
	}

	workflowDir := filepath.Join(dir, ".maclaw", "workflow")
	requirementsPath := filepath.Join(workflowDir, phaseFileName["requirements"])
	designPath := filepath.Join(workflowDir, phaseFileName["design"])
	tasksPath := filepath.Join(workflowDir, phaseFileName["tasks"])

	gotRequirements, err := os.ReadFile(requirementsPath)
	if err != nil {
		t.Fatalf("requirements doc was not persisted at explicit phase path: %v", err)
	}
	if string(gotRequirements) != requirementsContent {
		t.Fatalf("requirements doc content = %q, want %q", string(gotRequirements), requirementsContent)
	}
	gotDesign, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatalf("design doc was not persisted at explicit phase path: %v", err)
	}
	if string(gotDesign) != designContent {
		t.Fatalf("design doc content = %q, want %q", string(gotDesign), designContent)
	}
	if _, err := os.Stat(tasksPath); !os.IsNotExist(err) {
		t.Fatalf("tasks doc should not be created from content heading, stat err=%v", err)
	}
}
