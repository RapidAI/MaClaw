package workflow

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Unit Tests for HasPhaseOutput — Property 4 from design
//
// HasPhaseOutput(userID) returns true iff the user's active workflow has
// a non-empty output stored for the current phase.
//
// **Validates: Requirements 2.5, 1.5**
// ---------------------------------------------------------------------------

// TestHasPhaseOutput_NoActiveWorkflow verifies that HasPhaseOutput returns
// false when the user has no active workflow.
func TestHasPhaseOutput_NoActiveWorkflow(t *testing.T) {
	engine, _ := newTestEngine()

	if engine.HasPhaseOutput("nonexistent-user") {
		t.Error("HasPhaseOutput should return false when no active workflow exists")
	}
}

// TestHasPhaseOutput_ActiveWorkflow_NoOutputForCurrentPhase verifies that
// HasPhaseOutput returns false when the user has an active workflow but
// no output has been stored for the current phase.
func TestHasPhaseOutput_ActiveWorkflow_NoOutputForCurrentPhase(t *testing.T) {
	engine, _ := newTestEngine()

	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	_, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// No output stored yet — should return false
	if engine.HasPhaseOutput("u1") {
		t.Error("HasPhaseOutput should return false when no output exists for current phase")
	}
}

// TestHasPhaseOutput_ActiveWorkflow_EmptyStringOutput verifies that
// HasPhaseOutput returns false when the current phase has an empty string
// output (key exists but value is "").
func TestHasPhaseOutput_ActiveWorkflow_EmptyStringOutput(t *testing.T) {
	engine, _ := newTestEngine()

	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	_, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// Inject empty string output for current phase
	engine.mu.Lock()
	ws := engine.workflows["u1"]
	ws.PhaseOutputs[ws.CurrentPhase] = ""
	engine.mu.Unlock()

	if engine.HasPhaseOutput("u1") {
		t.Error("HasPhaseOutput should return false when output is empty string")
	}
}

// TestHasPhaseOutput_ActiveWorkflow_NonEmptyOutput verifies that
// HasPhaseOutput returns true when the current phase has a non-empty output.
func TestHasPhaseOutput_ActiveWorkflow_NonEmptyOutput(t *testing.T) {
	engine, _ := newTestEngine()

	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	_, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// Inject non-empty output for current phase
	engine.mu.Lock()
	ws := engine.workflows["u1"]
	ws.PhaseOutputs[ws.CurrentPhase] = "# Requirements Document\n\nThis is the full document."
	engine.mu.Unlock()

	if !engine.HasPhaseOutput("u1") {
		t.Error("HasPhaseOutput should return true when output is non-empty for current phase")
	}
}

// TestHasPhaseOutput_ActiveWorkflow_OutputForDifferentPhase verifies that
// HasPhaseOutput returns false when output exists for a different phase
// (not the current one).
func TestHasPhaseOutput_ActiveWorkflow_OutputForDifferentPhase(t *testing.T) {
	engine, _ := newTestEngine()

	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	_, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// Inject output for a different phase (not the current one)
	engine.mu.Lock()
	ws := engine.workflows["u1"]
	currentPhase := ws.CurrentPhase
	ws.PhaseOutputs["some_other_phase"] = "output for a different phase"
	engine.mu.Unlock()

	if engine.HasPhaseOutput("u1") {
		t.Errorf("HasPhaseOutput should return false when output exists for different phase "+
			"(current=%s, output stored for 'some_other_phase')", currentPhase)
	}
}

// TestHasPhaseOutput_CancelledWorkflow verifies that HasPhaseOutput returns
// false when the workflow has been cancelled (not active).
func TestHasPhaseOutput_CancelledWorkflow(t *testing.T) {
	engine, _ := newTestEngine()

	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	_, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// Store output then cancel
	engine.mu.Lock()
	ws := engine.workflows["u1"]
	ws.PhaseOutputs[ws.CurrentPhase] = "some output"
	engine.mu.Unlock()

	err = engine.CancelWorkflow("u1")
	if err != nil {
		t.Fatalf("CancelWorkflow failed: %v", err)
	}

	if engine.HasPhaseOutput("u1") {
		t.Error("HasPhaseOutput should return false when workflow is cancelled")
	}
}
