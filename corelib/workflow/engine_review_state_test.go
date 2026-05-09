package workflow

import "testing"

const reviewStateValidContent = "# Phase Output\n\n- Functional item A\n- Functional item B\n- Functional item C\n\nThis document is long enough to pass the minimum quality gate and enter the workflow review state when the phase requires confirmation."

func saveReviewOutputForCurrentPhase(t *testing.T, engine *WorkflowEngine, userID string) {
	t.Helper()
	if phaseID := engine.SavePhaseOutput(userID, reviewStateValidContent); phaseID == "" {
		t.Fatalf("SavePhaseOutput failed for user %s", userID)
	}
}

func TestEngine_SavePhaseOutputEntersReviewState(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u1", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build a desktop snake game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	phaseID := engine.SavePhaseOutput("u1", reviewStateValidContent)
	if phaseID != PhaseCodingRequirements {
		t.Fatalf("SavePhaseOutput phase=%q, want %q", phaseID, PhaseCodingRequirements)
	}
	if !engine.IsAwaitingReview("u1") {
		t.Fatal("expected workflow to wait for user review after saving NeedsConfirm output")
	}
}

func TestEngine_AdvancePhaseClearsReviewState(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u1", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build a desktop snake game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	if phaseID := engine.SavePhaseOutput("u1", reviewStateValidContent); phaseID == "" {
		t.Fatal("SavePhaseOutput failed")
	}
	if !engine.IsAwaitingReview("u1") {
		t.Fatal("expected review state before advancing")
	}
	if _, err := engine.AdvancePhase("u1"); err != nil {
		t.Fatalf("AdvancePhase failed: %v", err)
	}
	if engine.IsAwaitingReview("u1") {
		t.Fatal("review state should be cleared after explicit phase advance")
	}
}

func TestEngine_SavePhaseOutputDoesNotEnterReviewForNonConfirmPhase(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u1", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build a desktop snake game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	engine.mu.Lock()
	ws := engine.workflows["u1"]
	ws.CurrentPhase = PhaseCodingImplementation
	ws.PhaseIndex = 3
	engine.mu.Unlock()

	phaseID := engine.SavePhaseOutput("u1", reviewStateValidContent)
	if phaseID != PhaseCodingImplementation {
		t.Fatalf("SavePhaseOutput phase=%q, want %q", phaseID, PhaseCodingImplementation)
	}
	if engine.IsAwaitingReview("u1") {
		t.Fatal("non-NeedsConfirm phase must not enter review state")
	}
}

func TestEngine_ReviewStateAppliesToEveryNeedsConfirmTemplatePhase(t *testing.T) {
	engine, _ := newTestEngine()
	registry := engine.GetRegistry()

	registry.mu.RLock()
	templates := make([]*WorkflowTemplate, 0, len(registry.templates))
	for _, tmpl := range registry.templates {
		templates = append(templates, tmpl)
	}
	registry.mu.RUnlock()

	for _, tmpl := range templates {
		if len(tmpl.Phases) == 0 || !tmpl.Phases[0].NeedsConfirm {
			continue
		}
		userID := "review_" + string(tmpl.Type)
		_, err := engine.StartWorkflow(userID, StructuredIntent{
			Category: tmpl.Type,
			Summary:  "test workflow review barrier",
		})
		if err != nil {
			t.Fatalf("%s: StartWorkflow failed: %v", tmpl.Type, err)
		}

		if phaseID := engine.SavePhaseOutput(userID, reviewStateValidContent); phaseID != tmpl.Phases[0].ID {
			t.Fatalf("%s: saved phase=%q, want %q", tmpl.Type, phaseID, tmpl.Phases[0].ID)
		}
		if !engine.IsAwaitingReview(userID) {
			t.Fatalf("%s: expected first NeedsConfirm phase to enter review state", tmpl.Type)
		}
	}
}

func TestEngine_ApplyReviewIntentConfirmAdvances(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u1", StructuredIntent{Category: WorkflowCoding, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	saveReviewOutputForCurrentPhase(t, engine, "u1")

	resp, err := engine.ApplyReviewIntent("u1", ReviewIntentConfirm, "")
	if err != nil {
		t.Fatalf("ApplyReviewIntent confirm failed: %v", err)
	}
	if !resp.Advance {
		t.Fatal("classified confirm should advance")
	}
	if engine.IsAwaitingReview("u1") {
		t.Fatal("classified confirm should clear review state")
	}
}

func TestEngine_ApplyReviewIntentSupplementRegeneratesCurrentPhase(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u1", StructuredIntent{Category: WorkflowCoding, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	saveReviewOutputForCurrentPhase(t, engine, "u1")

	resp, err := engine.ApplyReviewIntent("u1", ReviewIntentSupplement, "add login")
	if err != nil {
		t.Fatalf("ApplyReviewIntent supplement failed: %v", err)
	}
	if !resp.RunAgentLoop {
		t.Fatal("supplement should regenerate the current phase")
	}
	if resp.Advance {
		t.Fatal("supplement must not advance the workflow")
	}
	if !engine.IsAwaitingReview("u1") {
		t.Fatal("supplement keeps the review state until regenerated output is confirmed")
	}
}

func TestEngine_ApplyReviewIntentOtherKeepsBarrier(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u1", StructuredIntent{Category: WorkflowCoding, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	saveReviewOutputForCurrentPhase(t, engine, "u1")

	resp, err := engine.ApplyReviewIntent("u1", ReviewIntentOther, "weather")
	if err != nil {
		t.Fatalf("ApplyReviewIntent other failed: %v", err)
	}
	if resp.RunAgentLoop || resp.Advance {
		t.Fatal("other intent should not run tools or advance while review barrier is active")
	}
	if !engine.IsAwaitingReview("u1") {
		t.Fatal("other intent should keep review barrier active")
	}
}

func TestEngine_ApplyReviewIntentCancelStopsWorkflow(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u1", StructuredIntent{Category: WorkflowCoding, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	saveReviewOutputForCurrentPhase(t, engine, "u1")

	resp, err := engine.ApplyReviewIntent("u1", ReviewIntentCancel, "")
	if err != nil {
		t.Fatalf("ApplyReviewIntent cancel failed: %v", err)
	}
	if !resp.Complete {
		t.Fatal("cancel should end the active workflow")
	}
	if engine.HasActiveWorkflow("u1") {
		t.Fatal("cancel should remove active workflow")
	}
}
