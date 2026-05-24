package workflow

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const reviewStateValidContent = "# Phase Output\n\n- Functional item A\n- Functional item B\n- Functional item C\n\nThis document is long enough to pass the minimum quality gate and enter the workflow review state when the phase requires confirmation."

func saveReviewOutputForCurrentPhase(t *testing.T, engine *WorkflowEngine, userID string) {
	t.Helper()
	makeCurrentPhaseExecutable(t, engine, userID)
	if phaseID, err := engine.SavePhaseOutput(userID, reviewStateValidContent); err != nil || phaseID == "" {
		t.Fatalf("SavePhaseOutput failed for user %s: phase=%q err=%v", userID, phaseID, err)
	}
}

func makeCurrentPhaseExecutable(t *testing.T, engine *WorkflowEngine, userID string) {
	t.Helper()
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil {
		return
	}
	tmpl := engine.GetRegistry().Match(ws.Type)
	if tmpl == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
		return
	}
	if ws.IsWaitingForInput(tmpl) {
		if _, err := engine.SubmitInputPayload(userID, &WorkflowInputPayload{Text: "test source material", ReceivedAt: time.Now()}); err != nil {
			t.Fatalf("SubmitInputPayload failed for user %s: %v", userID, err)
		}
		ws = engine.GetActiveWorkflow(userID)
		if ws == nil || ws.PhaseIndex < 0 || ws.PhaseIndex >= len(tmpl.Phases) {
			return
		}
	}
	if tmpl.Phases[ws.PhaseIndex].InputSchema != nil && !ws.phaseFormGateSatisfied() {
		if err := engine.SkipPhaseForm(userID); err != nil {
			t.Fatalf("SkipPhaseForm failed for user %s: %v", userID, err)
		}
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

	makeCurrentPhaseExecutable(t, engine, "u1")
	phaseID, err := engine.SavePhaseOutput("u1", reviewStateValidContent)
	if err != nil {
		t.Fatalf("SavePhaseOutput failed: %v", err)
	}
	if phaseID != PhaseCodingRequirements {
		t.Fatalf("SavePhaseOutput phase=%q, want %q", phaseID, PhaseCodingRequirements)
	}
	if !engine.IsAwaitingReview("u1") {
		t.Fatal("expected workflow to wait for user review after saving NeedsConfirm output")
	}
}

func TestEngine_AdvancePhaseRejectsReviewBypass(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u1", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build a desktop snake game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	makeCurrentPhaseExecutable(t, engine, "u1")
	if phaseID, err := engine.SavePhaseOutput("u1", reviewStateValidContent); err != nil || phaseID == "" {
		t.Fatalf("SavePhaseOutput failed: phase=%q err=%v", phaseID, err)
	}
	if !engine.IsAwaitingReview("u1") {
		t.Fatal("expected review state before advancing")
	}
	if _, err := engine.AdvancePhase("u1"); err == nil {
		t.Fatal("AdvancePhase must not bypass a pending review gate")
	}
	if !engine.IsAwaitingReview("u1") {
		t.Fatal("review state should remain until ApplyReviewIntent confirms it")
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

	makeCurrentPhaseExecutable(t, engine, "u1")
	phaseID, err := engine.SavePhaseOutput("u1", reviewStateValidContent)
	if err != nil {
		t.Fatalf("SavePhaseOutput failed: %v", err)
	}
	if phaseID != PhaseCodingImplementation {
		t.Fatalf("SavePhaseOutput phase=%q, want %q", phaseID, PhaseCodingImplementation)
	}
	if engine.IsAwaitingReview("u1") {
		t.Fatal("non-NeedsConfirm phase must not enter review state")
	}
}

func TestEngine_SavePhaseOutputAndMaybeAdvanceAdvancesNonConfirmPhase(t *testing.T) {
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

	phaseID, resp, err := engine.SavePhaseOutputAndMaybeAdvance("u1", reviewStateValidContent)
	if err != nil {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance failed: %v", err)
	}
	if phaseID != PhaseCodingImplementation {
		t.Fatalf("saved phase=%q, want %q", phaseID, PhaseCodingImplementation)
	}
	if resp == nil || !resp.Advance || !resp.RunAgentLoop {
		t.Fatalf("non-confirm output should advance into next phase, got %#v", resp)
	}
	active := engine.GetActiveWorkflow("u1")
	if active == nil || active.CurrentPhase != PhaseCodingReview {
		t.Fatalf("current phase = %#v, want %s", active, PhaseCodingReview)
	}
}

func TestEngine_AdvancePhaseRespectsNextPhaseFormGate(t *testing.T) {
	engine, _ := newTestEngine()
	workflowType := WorkflowType("advance_form_gate_test")
	engine.GetRegistry().Register(&WorkflowTemplate{
		Type:        workflowType,
		Name:        "advance form gate test",
		Description: "test template",
		Phases: []PhaseTemplate{
			{ID: "reviewed", Name: "Reviewed", Prompt: "make reviewed output", Deliverable: "reviewed output", NeedsConfirm: true, ToolPolicy: ToolFilterDocOnly},
			{
				ID:          "collect_more",
				Name:        "Collect More",
				Prompt:      "collect more",
				Deliverable: "more context",
				InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{Name: "scope", Label: "Scope", Type: "text", Required: true}}},
				ToolPolicy:  ToolFilterDocOnly,
			},
		},
	})

	_, err := engine.StartWorkflow("u_advance_form", StructuredIntent{Category: workflowType, Summary: "test"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	makeCurrentPhaseExecutable(t, engine, "u_advance_form")
	if phaseID, err := engine.SavePhaseOutput("u_advance_form", reviewStateValidContent); err != nil || phaseID != "reviewed" {
		t.Fatalf("saved phase=%q err=%v", phaseID, err)
	}
	resp, err := engine.ApplyReviewIntent("u_advance_form", ReviewIntentConfirm, "")
	if err != nil {
		t.Fatalf("ApplyReviewIntent failed: %v", err)
	}
	if resp == nil || !resp.ShowForm || resp.RunAgentLoop || resp.FormSchema == nil {
		t.Fatalf("advance into form phase should stop at form gate, got %#v", resp)
	}
}

func TestEngine_SubmitInputPayloadPersistsEvidence(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u1", StructuredIntent{
		Category: WorkflowContractReview,
		Summary:  "review a contract",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	resp, err := engine.SubmitInputPayload("u1", &WorkflowInputPayload{
		Text: "contract text",
		Attachments: []WorkflowInputAttachment{{
			Type:     "file",
			FileName: "contract.docx",
			MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			Size:     1234,
		}},
	})
	if err != nil {
		t.Fatalf("SubmitInputPayload failed: %v", err)
	}
	if resp == nil || !resp.RunAgentLoop || resp.PhasePrompt == "" {
		t.Fatalf("expected first phase agent loop response, got %#v", resp)
	}
	ws := engine.GetActiveWorkflow("u1")
	if ws == nil || !ws.InputReceived || ws.InputPayload == nil {
		t.Fatalf("input payload was not recorded: %#v", ws)
	}
	if ws.InputPayload.Text != "contract text" || len(ws.InputPayload.Attachments) != 1 {
		t.Fatalf("unexpected input payload: %#v", ws.InputPayload)
	}
	if !containsAll(resp.PhasePrompt, "contract text", "contract.docx") {
		t.Fatalf("phase prompt does not include input evidence: %s", resp.PhasePrompt)
	}
}

func TestEngine_SubmitInputPayloadRespectsFirstPhaseFormGate(t *testing.T) {
	engine, _ := newTestEngine()
	workflowType := WorkflowType("input_form_gate_test")
	engine.GetRegistry().Register(&WorkflowTemplate{
		Type:          workflowType,
		Name:          "input form gate test",
		Description:   "test template",
		RequiresInput: &InputRequirement{Description: "source document", AcceptText: true},
		Phases: []PhaseTemplate{{
			ID:          "collect_context",
			Name:        "Collect Context",
			Description: "collect context",
			Prompt:      "use the source and collected context",
			Deliverable: "context document",
			InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{Name: "goal", Label: "Goal", Type: "text", Required: true}}},
			ToolPolicy:  ToolFilterDocOnly,
		}},
	})

	_, err := engine.StartWorkflow("u_input_form", StructuredIntent{Category: workflowType, Summary: "review source"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	resp, err := engine.SubmitInputPayload("u_input_form", &WorkflowInputPayload{Text: "source material"})
	if err != nil {
		t.Fatalf("SubmitInputPayload failed: %v", err)
	}
	if resp == nil || !resp.ShowForm || resp.RunAgentLoop {
		t.Fatalf("input submission should stop at first-phase form gate, got %#v", resp)
	}
	ws := engine.GetActiveWorkflow("u_input_form")
	if ws == nil || !ws.InputReceived || ws.InputPayload == nil || ws.InputPayload.Text != "source material" {
		t.Fatalf("input evidence should still be durable before form submission: %#v", ws)
	}
}

func TestEngine_PublicEntrypointsRejectCorruptPhaseState(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_corrupt_phase", StructuredIntent{Category: WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	engine.mu.Lock()
	engine.workflows["u_corrupt_phase"].PhaseIndex = -1
	engine.mu.Unlock()

	mustNotPanic(t, func() {
		if _, err := engine.HandleInput("u_corrupt_phase", "start"); err == nil {
			t.Fatal("HandleInput should reject corrupt negative phase index")
		}
	})
	mustNotPanic(t, func() {
		if _, err := engine.AdvancePhase("u_corrupt_phase"); err == nil {
			t.Fatal("AdvancePhase should reject corrupt negative phase index")
		}
	})
	mustNotPanic(t, func() {
		if prompt := engine.BuildPhasePrompt("u_corrupt_phase"); prompt != "" {
			t.Fatalf("BuildPhasePrompt should return empty for corrupt phase state, got %q", prompt)
		}
	})
	mustNotPanic(t, func() {
		if policy := engine.GetPhaseToolFilter("u_corrupt_phase"); policy != ToolFilterNone {
			t.Fatalf("GetPhaseToolFilter should return none for corrupt phase state, got %s", policy)
		}
	})

	engine.mu.Lock()
	engine.workflows["u_corrupt_phase"].PhaseIndex = 0
	engine.workflows["u_corrupt_phase"].CurrentPhase = "mismatched"
	engine.mu.Unlock()
	mustNotPanic(t, func() {
		if _, err := engine.HandleInput("u_corrupt_phase", "start"); err == nil {
			t.Fatal("HandleInput should reject mismatched current phase")
		}
	})
	mustNotPanic(t, func() {
		if _, err := engine.AdvancePhase("u_corrupt_phase"); err == nil {
			t.Fatal("AdvancePhase should reject mismatched current phase")
		}
	})
}

func mustNotPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic: %v", r)
		}
	}()
	fn()
}
func TestEngine_SubmitPhaseFormRespectsInputGate(t *testing.T) {
	engine, _ := newTestEngine()
	workflowType := WorkflowType("form_input_gate_test")
	engine.GetRegistry().Register(&WorkflowTemplate{
		Type:          workflowType,
		Name:          "form input gate test",
		Description:   "test template",
		RequiresInput: &InputRequirement{Description: "source document", AcceptText: true},
		Phases: []PhaseTemplate{{
			ID:          "collect_context",
			Name:        "Collect Context",
			Prompt:      "collect context",
			Deliverable: "context document",
			InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{Name: "goal", Label: "Goal", Type: "text", Required: true}}},
			ToolPolicy:  ToolFilterDocOnly,
		}},
	})
	if _, err := engine.StartWorkflow("u_form_input_gate", StructuredIntent{Category: workflowType, Summary: "review source"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if _, err := engine.SubmitPhaseForm("u_form_input_gate", map[string]interface{}{"goal": "extract risk"}); err == nil {
		t.Fatal("SubmitPhaseForm must not bypass required workflow input")
	}
	ws := engine.GetActiveWorkflow("u_form_input_gate")
	if ws == nil || ws.InputReceived || len(ws.PhaseFormData) != 0 || ws.PhaseFormSubmitted || ws.PhaseFormSkipped {
		t.Fatalf("form submission should not mutate state behind input gate: %#v", ws)
	}
}

func TestEngine_SubmitPhaseFormRejectsDuplicateSubmission(t *testing.T) {
	engine, _ := newTestEngine()
	workflowType := WorkflowType("form_duplicate_test")
	engine.GetRegistry().Register(&WorkflowTemplate{
		Type:        workflowType,
		Name:        "form duplicate test",
		Description: "test template",
		Phases: []PhaseTemplate{{
			ID:          "collect",
			Name:        "Collect",
			Prompt:      "collect context",
			Deliverable: "context document",
			InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{Name: "goal", Label: "Goal", Type: "text", Required: true}}},
			ToolPolicy:  ToolFilterDocOnly,
		}},
	})
	if _, err := engine.StartWorkflow("u_form_duplicate", StructuredIntent{Category: workflowType, Summary: "collect"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if _, err := engine.SubmitPhaseForm("u_form_duplicate", map[string]interface{}{"goal": "first"}); err != nil {
		t.Fatalf("first SubmitPhaseForm failed: %v", err)
	}
	if _, err := engine.SubmitPhaseForm("u_form_duplicate", map[string]interface{}{"goal": "second"}); err == nil {
		t.Fatal("SubmitPhaseForm should reject duplicate form submission")
	}
	ws := engine.GetActiveWorkflow("u_form_duplicate")
	if ws == nil || ws.PhaseFormData["goal"] != "first" {
		t.Fatalf("duplicate submission should not overwrite durable form data: %#v", ws)
	}
}
func TestEngine_DirectPromptAndToolsRespectWorkflowGates(t *testing.T) {
	engine, _ := newTestEngine()
	formType := WorkflowType("direct_gate_form_test")
	engine.GetRegistry().Register(&WorkflowTemplate{
		Type:        formType,
		Name:        "direct gate form test",
		Description: "test template",
		Phases: []PhaseTemplate{{
			ID:          "collect",
			Name:        "Collect",
			Prompt:      "collect form data",
			Deliverable: "collection",
			InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{Name: "goal", Label: "Goal", Type: "text", Required: true}}},
			ToolPolicy:  ToolFilterDocOnly,
		}},
	})

	if _, err := engine.StartWorkflow("u_direct_form", StructuredIntent{Category: formType, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow form: %v", err)
	}
	if prompt := engine.BuildPhasePrompt("u_direct_form"); prompt != "" {
		t.Fatalf("direct BuildPhasePrompt must not bypass form gate, got %q", prompt)
	}
	if !engine.IsPhaseExecutionBlocked("u_direct_form") {
		t.Fatal("form gate should block phase execution")
	}
	if policy := engine.GetPhaseToolFilter("u_direct_form"); policy != ToolFilterNone {
		t.Fatalf("direct tool filter must be none while form gate is pending, got %s", policy)
	}
	if policy := engine.GetActivePhaseToolFilter("u_direct_form"); policy != ToolFilterDocOnly {
		t.Fatalf("active phase tool filter should remain available while form gate is pending, got %s", policy)
	}
	if err := engine.SkipPhaseForm("u_direct_form"); err != nil {
		t.Fatalf("SkipPhaseForm: %v", err)
	}
	if prompt := engine.BuildPhasePrompt("u_direct_form"); prompt == "" {
		t.Fatal("prompt should be available after explicit form skip")
	}
	if engine.IsPhaseExecutionBlocked("u_direct_form") {
		t.Fatal("form skip should unblock phase execution")
	}
	if policy := engine.GetPhaseToolFilter("u_direct_form"); policy != ToolFilterDocOnly {
		t.Fatalf("tool filter should be restored after form skip, got %s", policy)
	}

	if _, err := engine.StartWorkflow("u_direct_input", StructuredIntent{Category: WorkflowContractReview, Summary: "review"}); err != nil {
		t.Fatalf("StartWorkflow input: %v", err)
	}
	if prompt := engine.BuildPhasePrompt("u_direct_input"); prompt != "" {
		t.Fatalf("direct BuildPhasePrompt must not bypass input gate, got %q", prompt)
	}
	if !engine.IsPhaseExecutionBlocked("u_direct_input") {
		t.Fatal("input gate should block phase execution")
	}
	if policy := engine.GetPhaseToolFilter("u_direct_input"); policy != ToolFilterNone {
		t.Fatalf("direct tool filter must be none while input gate is pending, got %s", policy)
	}
	if policy := engine.GetActivePhaseToolFilter("u_direct_input"); policy != ToolFilterDocOnly {
		t.Fatalf("active phase tool filter should remain available while input gate is pending, got %s", policy)
	}

	if _, err := engine.StartWorkflow("u_direct_review", StructuredIntent{Category: WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow review: %v", err)
	}
	saveReviewOutputForCurrentPhase(t, engine, "u_direct_review")
	if prompt := engine.BuildPhasePrompt("u_direct_review"); prompt != "" {
		t.Fatalf("direct BuildPhasePrompt must not bypass review gate, got %q", prompt)
	}
	if !engine.IsPhaseExecutionBlocked("u_direct_review") {
		t.Fatal("review gate should block phase execution")
	}
	if policy := engine.GetPhaseToolFilter("u_direct_review"); policy != ToolFilterNone {
		t.Fatalf("direct tool filter must be none while review gate is pending, got %s", policy)
	}
	if policy := engine.GetActivePhaseToolFilter("u_direct_review"); policy != ToolFilterDocOnly {
		t.Fatalf("active phase tool filter should remain available while review gate is pending, got %s", policy)
	}
}

func TestEngine_HandleInputCancelCommandDoesNotBecomeWorkflowInput(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_cancel_input_gate", StructuredIntent{Category: WorkflowContractReview, Summary: "review"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	resp, err := engine.HandleInput("u_cancel_input_gate", "cancel")
	if err != nil {
		t.Fatalf("HandleInput cancel failed: %v", err)
	}
	if resp == nil || !resp.Complete || resp.RunAgentLoop {
		t.Fatalf("cancel should complete without agent loop, got %#v", resp)
	}
	if ws := engine.GetActiveWorkflow("u_cancel_input_gate"); ws != nil {
		t.Fatalf("cancel command should remove active workflow instead of becoming input: %#v", ws)
	}
}

func TestEngine_SkipPhaseFormOnlyMutatesRealPendingForm(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_skip_no_schema", StructuredIntent{Category: WorkflowContractReview, Summary: "review"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := engine.SkipPhaseForm("u_skip_no_schema"); err != nil {
		t.Fatalf("SkipPhaseForm should no-op behind document input gate: %v", err)
	}
	if ws := engine.GetActiveWorkflow("u_skip_no_schema"); ws == nil || len(ws.PhaseFormData) != 0 || ws.PhaseFormSubmitted || ws.PhaseFormSkipped {
		t.Fatalf("SkipPhaseForm should not mark a non-form/input-gated phase skipped: %#v", ws)
	}

	if _, err := engine.SubmitInputPayload("u_skip_no_schema", &WorkflowInputPayload{Text: "source"}); err != nil {
		t.Fatalf("SubmitInputPayload failed: %v", err)
	}
	if err := engine.SkipPhaseForm("u_skip_no_schema"); err != nil {
		t.Fatalf("SkipPhaseForm should no-op on phases without forms: %v", err)
	}
	if ws := engine.GetActiveWorkflow("u_skip_no_schema"); ws == nil || len(ws.PhaseFormData) != 0 || ws.PhaseFormSubmitted || ws.PhaseFormSkipped {
		t.Fatalf("SkipPhaseForm should not mark a schema-less phase skipped: %#v", ws)
	}
}

func TestEngine_SubmitPhaseFormEmptyOptionalPayloadSatisfiesGate(t *testing.T) {
	engine, _ := newTestEngine()
	engine.registry.Register(&WorkflowTemplate{
		Type:        "empty_optional_form_gate",
		Name:        "empty optional form gate",
		Description: "test template",
		Phases: []PhaseTemplate{{
			ID:          "collect",
			Name:        "Collect",
			Prompt:      "collect optional details",
			Deliverable: "collection document",
			ToolPolicy:  ToolFilterDocOnly,
			InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{Name: "note", Label: "Note", Type: "text", Required: false}}},
		}},
	})
	if _, err := engine.StartWorkflow("u_empty_optional_form", StructuredIntent{Category: "empty_optional_form_gate", Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	resp, err := engine.HandleInput("u_empty_optional_form", "start")
	if err != nil || resp == nil || !resp.ShowForm {
		t.Fatalf("first input should show form, resp=%#v err=%v", resp, err)
	}
	resp, err = engine.SubmitPhaseForm("u_empty_optional_form", map[string]interface{}{})
	if err != nil {
		t.Fatalf("empty optional form submission should be accepted: %v", err)
	}
	if resp == nil || !resp.RunAgentLoop || strings.TrimSpace(resp.PhasePrompt) == "" {
		t.Fatalf("empty optional form submission should start the phase loop, got %#v", resp)
	}
	if phaseID, err := engine.SavePhaseOutput("u_empty_optional_form", reviewStateValidContent); err != nil || phaseID != "collect" {
		t.Fatalf("empty optional form should satisfy SavePhaseOutput gate: phase=%q err=%v", phaseID, err)
	}
	if _, err := engine.SubmitPhaseForm("u_empty_optional_form", map[string]interface{}{}); err == nil {
		t.Fatal("empty optional form submission must still be treated as a consumed one-shot gate")
	}
}
func TestEngine_SkipPhaseFormUsesExplicitSkippedState(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_skip_explicit", StructuredIntent{Category: WorkflowCoding, Summary: "build an app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := engine.SkipPhaseForm("u_skip_explicit"); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	ws := engine.GetActiveWorkflow("u_skip_explicit")
	if ws == nil || !ws.PhaseFormSkipped || ws.PhaseFormSubmitted || len(ws.PhaseFormData) != 0 {
		t.Fatalf("SkipPhaseForm should use explicit skipped state without synthetic form data: %#v", ws)
	}
	resp, err := engine.HandleInput("u_skip_explicit", "use natural language details")
	if err != nil {
		t.Fatalf("HandleInput after skipped form failed: %v", err)
	}
	if resp == nil || !resp.RunAgentLoop || resp.ShowForm {
		t.Fatalf("skipped form should satisfy the form gate and start the phase loop, got %#v", resp)
	}
}

func TestEngine_RestoreFromStoreMigratesLegacySkippedPhaseFormSentinel(t *testing.T) {
	store := &recordingRestoreStore{states: []*WorkflowState{{
		ID:            "wf-legacy-skip",
		UserID:        "u_legacy_skip",
		Type:          WorkflowCoding,
		Intent:        StructuredIntent{Category: WorkflowCoding, Summary: "build"},
		CurrentPhase:  PhaseCodingRequirements,
		PhaseIndex:    0,
		PhaseOutputs:  map[string]string{},
		GateResults:   map[string]*QualityGateResult{},
		Status:        WorkflowActive,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		PhaseFormData: map[string]interface{}{"_skipped": true},
	}}}
	engine := NewWorkflowEngine(NewWorkflowRegistry(), nil, store, nil)
	if err := engine.RestoreFromStore(); err != nil {
		t.Fatalf("RestoreFromStore failed: %v", err)
	}
	ws := engine.GetActiveWorkflow("u_legacy_skip")
	if ws == nil || !ws.PhaseFormSkipped || ws.PhaseFormSubmitted || len(ws.PhaseFormData) != 0 {
		t.Fatalf("legacy skipped form sentinel was not migrated: %#v", ws)
	}
	if len(store.saved) == 0 {
		t.Fatal("restored legacy sentinel should be repaired and persisted")
	}
}
func TestEngine_SavePhaseOutputRespectsInputAndFormGates(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_save_input_gate", StructuredIntent{Category: WorkflowContractReview, Summary: "review"}); err != nil {
		t.Fatalf("StartWorkflow input-gated workflow failed: %v", err)
	}
	if phaseID, err := engine.SavePhaseOutput("u_save_input_gate", reviewStateValidContent); err != nil || phaseID != "" {
		t.Fatalf("SavePhaseOutput must no-op behind document input gate: phase=%q err=%v", phaseID, err)
	}
	if ws := engine.GetActiveWorkflow("u_save_input_gate"); ws == nil || len(ws.PhaseOutputs) != 0 || ws.PendingReviewPhaseID != "" {
		t.Fatalf("input-gated output capture mutated workflow: %#v", ws)
	}

	engine.registry.Register(&WorkflowTemplate{
		Type: "form_save_gate",
		Phases: []PhaseTemplate{{
			ID:          "collect",
			Name:        "Collect",
			Prompt:      "collect",
			Deliverable: "draft",
			ToolPolicy:  ToolFilterDocOnly,
			InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{{Name: "topic", Label: "Topic", Type: "text", Required: true}}},
		}},
	})
	if _, err := engine.StartWorkflow("u_save_form_gate", StructuredIntent{Category: "form_save_gate", Summary: "draft"}); err != nil {
		t.Fatalf("StartWorkflow form-gated workflow failed: %v", err)
	}
	if phaseID, err := engine.SavePhaseOutput("u_save_form_gate", reviewStateValidContent); err != nil || phaseID != "" {
		t.Fatalf("SavePhaseOutput must no-op behind structured form gate: phase=%q err=%v", phaseID, err)
	}
	if ws := engine.GetActiveWorkflow("u_save_form_gate"); ws == nil || len(ws.PhaseOutputs) != 0 || ws.PendingReviewPhaseID != "" {
		t.Fatalf("form-gated output capture mutated workflow: %#v", ws)
	}
}

func containsAll(s string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(s, want) {
			return false
		}
	}
	return true
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

		makeCurrentPhaseExecutable(t, engine, userID)
		if phaseID, err := engine.SavePhaseOutput(userID, reviewStateValidContent); err != nil || phaseID != tmpl.Phases[0].ID {
			t.Fatalf("%s: saved phase=%q, want %q err=%v", tmpl.Type, phaseID, tmpl.Phases[0].ID, err)
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

func TestEngine_SavePhaseOutputCannotOverwritePendingReviewWithoutSupplement(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_review_overwrite", StructuredIntent{Category: WorkflowCoding, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	saveReviewOutputForCurrentPhase(t, engine, "u_review_overwrite")

	updatedContent := "# Phase Output\n\n- Updated item A\n- Updated item B\n- Updated item C\n\nThis regenerated document is long enough to pass the minimum quality gate and should only replace the previous document after a classified supplement intent."
	phaseID, err := engine.SavePhaseOutput("u_review_overwrite", updatedContent)
	if err != nil {
		t.Fatalf("SavePhaseOutput should ignore without error, got %v", err)
	}
	if phaseID != "" {
		t.Fatalf("SavePhaseOutput should not report a durable phase while review is pending without supplement, got %q", phaseID)
	}
	ws := engine.GetActiveWorkflow("u_review_overwrite")
	if ws == nil || ws.PhaseOutputs[ws.CurrentPhase] != reviewStateValidContent {
		t.Fatalf("pending review output was overwritten without supplement: %#v", ws)
	}
}

func TestEngine_ApplyReviewIntentSupplementAuthorizesOneRegeneration(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_review_regen", StructuredIntent{Category: WorkflowCoding, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	saveReviewOutputForCurrentPhase(t, engine, "u_review_regen")

	if _, err := engine.ApplyReviewIntent("u_review_regen", ReviewIntentSupplement, "add login"); err != nil {
		t.Fatalf("ApplyReviewIntent supplement failed: %v", err)
	}
	engine.mu.RLock()
	requested := engine.workflows["u_review_regen"].PendingReviewRevisionRequested
	engine.mu.RUnlock()
	if !requested {
		t.Fatal("supplement should mark exactly one pending review regeneration request")
	}

	updatedContent := "# Phase Output\n\n- Updated item A\n- Updated item B\n- Updated item C\n\nThis regenerated document is long enough to pass the minimum quality gate and replace the previous document after a classified supplement intent."
	phaseID, err := engine.SavePhaseOutput("u_review_regen", updatedContent)
	if err != nil || phaseID == "" {
		t.Fatalf("authorized regeneration should save output, phase=%q err=%v", phaseID, err)
	}
	ws := engine.GetActiveWorkflow("u_review_regen")
	if ws == nil || ws.PhaseOutputs[ws.CurrentPhase] != updatedContent {
		t.Fatalf("authorized regeneration did not replace output: %#v", ws)
	}
	engine.mu.RLock()
	requested = engine.workflows["u_review_regen"].PendingReviewRevisionRequested
	engine.mu.RUnlock()
	if requested {
		t.Fatal("saving regenerated output should consume the regeneration request")
	}
}
func TestEngine_RejectedRegenerationConsumesAuthorization(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_review_bad_regen", StructuredIntent{Category: WorkflowCoding, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	saveReviewOutputForCurrentPhase(t, engine, "u_review_bad_regen")
	if _, err := engine.ApplyReviewIntent("u_review_bad_regen", ReviewIntentSupplement, "make it better"); err != nil {
		t.Fatalf("ApplyReviewIntent supplement failed: %v", err)
	}

	phaseID, err := engine.SavePhaseOutput("u_review_bad_regen", "too short")
	if err != nil || phaseID != "" {
		t.Fatalf("bad regeneration should be rejected without a saved phase, phase=%q err=%v", phaseID, err)
	}
	engine.mu.RLock()
	requested := engine.workflows["u_review_bad_regen"].PendingReviewRevisionRequested
	engine.mu.RUnlock()
	if requested {
		t.Fatal("rejected regeneration must still consume the one-shot regeneration authorization")
	}

	updatedContent := "# Phase Output\n\n- Late item A\n- Late item B\n- Late item C\n\nThis late document is long enough to pass the minimum quality gate but must not overwrite review output after the one-shot regeneration authorization was consumed."
	phaseID, err = engine.SavePhaseOutput("u_review_bad_regen", updatedContent)
	if err != nil || phaseID != "" {
		t.Fatalf("late output without a fresh supplement should be ignored, phase=%q err=%v", phaseID, err)
	}
	ws := engine.GetActiveWorkflow("u_review_bad_regen")
	if ws == nil || ws.PhaseOutputs[ws.CurrentPhase] != reviewStateValidContent {
		t.Fatalf("late output overwrote pending review after consumed authorization: %#v", ws)
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

type failingWorkflowStateStore struct {
	NullStore
}

func (failingWorkflowStateStore) SaveWorkflowState(_ *WorkflowState) error {
	return errors.New("save failed")
}

type restoreFailingWorkflowStateStore struct {
	NullStore
	states []*WorkflowState
}

func (s restoreFailingWorkflowStateStore) ListActiveWorkflows() ([]*WorkflowState, error) {
	return s.states, nil
}

func (restoreFailingWorkflowStateStore) SaveWorkflowState(_ *WorkflowState) error {
	return errors.New("save failed")
}

func TestEngine_RestoreFromStorePropagatesRepairPersistenceError(t *testing.T) {
	engine, _ := newTestEngine()
	stale := &WorkflowState{
		ID:           "wf-stale",
		UserID:       "u_stale",
		Type:         WorkflowCoding,
		CurrentPhase: PhaseCodingRequirements,
		PhaseOutputs: map[string]string{},
		GateResults:  map[string]*QualityGateResult{},
		Status:       WorkflowActive,
		CreatedAt:    time.Now().Add(-2 * workflowStaleTimeout),
		UpdatedAt:    time.Now().Add(-2 * workflowStaleTimeout),
	}
	engine.store = restoreFailingWorkflowStateStore{states: []*WorkflowState{stale}}
	if err := engine.RestoreFromStore(); err == nil {
		t.Fatal("RestoreFromStore should propagate stale cancellation persistence failure")
	}
	if engine.HasActiveWorkflow("u_stale") {
		t.Fatal("restore must not publish stale workflow when cancellation cannot be persisted")
	}
}

func TestEngine_RestoreFromStorePropagatesCorruptRepairPersistenceError(t *testing.T) {
	engine, _ := newTestEngine()
	corrupt := &WorkflowState{
		ID:                   "wf-corrupt",
		UserID:               "u_corrupt",
		Type:                 WorkflowCoding,
		CurrentPhase:         PhaseCodingTechDesign,
		PhaseIndex:           1,
		PhaseOutputs:         map[string]string{},
		GateResults:          map[string]*QualityGateResult{},
		Status:               WorkflowActive,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
		PendingReviewPhaseID: PhaseCodingTechDesign,
		PhaseFormData:        map[string]interface{}{"x": "y"},
	}
	engine.store = restoreFailingWorkflowStateStore{states: []*WorkflowState{corrupt}}
	if err := engine.RestoreFromStore(); err == nil {
		t.Fatal("RestoreFromStore should propagate corrupt-state repair persistence failure")
	}
	if engine.HasActiveWorkflow("u_corrupt") {
		t.Fatal("restore must not publish repaired workflow when repair cannot be persisted")
	}
	if corrupt.PhaseIndex != 1 || corrupt.CurrentPhase != PhaseCodingTechDesign || corrupt.PendingReviewPhaseID != PhaseCodingTechDesign || corrupt.PhaseFormData["x"] != "y" {
		t.Fatalf("failed repair persistence must roll back restored state object, got %#v", corrupt)
	}
}

type recordingRestoreStore struct {
	NullStore
	states []*WorkflowState
	saved  []*WorkflowState
}

func (s *recordingRestoreStore) ListActiveWorkflows() ([]*WorkflowState, error) {
	return s.states, nil
}

func (s *recordingRestoreStore) SaveWorkflowState(state *WorkflowState) error {
	s.saved = append(s.saved, state.Clone())
	return nil
}

func TestEngine_RestoreFromStoreRepairsPhasePointerByCurrentPhase(t *testing.T) {
	engine, _ := newTestEngine()
	state := &WorkflowState{
		ID:           "wf-repair-pointer",
		UserID:       "u_restore_pointer",
		Type:         WorkflowCoding,
		CurrentPhase: PhaseCodingTechDesign,
		PhaseIndex:   -1,
		PhaseOutputs: map[string]string{PhaseCodingRequirements: reviewStateValidContent},
		GateResults:  map[string]*QualityGateResult{},
		Status:       WorkflowActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	store := &recordingRestoreStore{states: []*WorkflowState{state}}
	engine.store = store
	if err := engine.RestoreFromStore(); err != nil {
		t.Fatalf("RestoreFromStore failed: %v", err)
	}
	active := engine.GetActiveWorkflow("u_restore_pointer")
	if active == nil || active.CurrentPhase != PhaseCodingTechDesign || active.PhaseIndex != 1 {
		t.Fatalf("restore should repair phase index from current phase, got %#v", active)
	}
	if len(store.saved) != 1 || store.saved[0].PhaseIndex != 1 || store.saved[0].Status != WorkflowActive {
		t.Fatalf("repaired state should be persisted once, saved=%#v", store.saved)
	}
}

func TestEngine_RestoreFromStoreCancelsUnrepairableState(t *testing.T) {
	engine, _ := newTestEngine()
	state := &WorkflowState{
		ID:           "wf-unrepairable",
		UserID:       "u_restore_unrepairable",
		Type:         WorkflowType("removed_template"),
		CurrentPhase: "missing_phase",
		PhaseIndex:   -1,
		PhaseOutputs: map[string]string{},
		GateResults:  map[string]*QualityGateResult{},
		Status:       WorkflowActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	store := &recordingRestoreStore{states: []*WorkflowState{state}}
	engine.store = store
	if err := engine.RestoreFromStore(); err != nil {
		t.Fatalf("RestoreFromStore failed: %v", err)
	}
	if engine.HasActiveWorkflow("u_restore_unrepairable") {
		t.Fatal("unrepairable restored workflow must not be published as active")
	}
	if state.Status != WorkflowCancelled || len(store.saved) != 1 || store.saved[0].Status != WorkflowCancelled {
		t.Fatalf("unrepairable restored workflow should be cancelled durably, state=%#v saved=%#v", state, store.saved)
	}
}

func TestEngine_RestoreFromStoreResetsInputRequiredWorkflowWithoutInput(t *testing.T) {
	engine, _ := newTestEngine()
	state := &WorkflowState{
		ID:                   "wf-input-reset",
		UserID:               "u_restore_input_reset",
		Type:                 WorkflowContractReview,
		CurrentPhase:         "analysis",
		PhaseIndex:           1,
		PhaseOutputs:         map[string]string{"analysis": reviewStateValidContent},
		GateResults:          map[string]*QualityGateResult{"analysis": {Passed: true}},
		Status:               WorkflowActive,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
		PendingReviewPhaseID: "analysis",
		PhaseFormData:        map[string]interface{}{"stale": true},
	}
	store := &recordingRestoreStore{states: []*WorkflowState{state}}
	engine.store = store
	if err := engine.RestoreFromStore(); err != nil {
		t.Fatalf("RestoreFromStore failed: %v", err)
	}
	active := engine.GetActiveWorkflow("u_restore_input_reset")
	if active == nil || active.InputReceived || active.PhaseIndex != 0 || active.CurrentPhase != "contract_parsing" {
		t.Fatalf("input-required workflow should be reset to the input gate, got %#v", active)
	}
	if len(active.PhaseOutputs) != 0 || len(active.GateResults) != 0 || active.PendingReviewPhaseID != "" || len(active.PhaseFormData) != 0 {
		t.Fatalf("invalid post-input artifacts should be cleared, got %#v", active)
	}
	if len(store.saved) != 1 || store.saved[0].PhaseIndex != 0 || len(store.saved[0].PhaseOutputs) != 0 {
		t.Fatalf("input-gate repair should be persisted once, saved=%#v", store.saved)
	}
}
func TestEngine_RestoreFromStoreRehydratesPendingReview(t *testing.T) {
	engine, _ := newTestEngine()
	state := &WorkflowState{
		ID:           "wf-review-rehydrate",
		UserID:       "u_restore_review",
		Type:         WorkflowCoding,
		CurrentPhase: PhaseCodingRequirements,
		PhaseIndex:   0,
		PhaseOutputs: map[string]string{PhaseCodingRequirements: reviewStateValidContent},
		GateResults:  map[string]*QualityGateResult{},
		Status:       WorkflowActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	store := &recordingRestoreStore{states: []*WorkflowState{state}}
	engine.store = store
	if err := engine.RestoreFromStore(); err != nil {
		t.Fatalf("RestoreFromStore failed: %v", err)
	}
	if !engine.IsAwaitingReview("u_restore_review") {
		t.Fatalf("restore should rehydrate pending review for phase output, active=%#v", engine.GetActiveWorkflow("u_restore_review"))
	}
	if len(store.saved) != 1 || store.saved[0].PendingReviewPhaseID != PhaseCodingRequirements {
		t.Fatalf("review repair should be persisted once, saved=%#v", store.saved)
	}
}
func TestEngine_GetActiveWorkflowSnapshotDoesNotMutateEngine(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_snapshot", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
		Goals:    []string{"original"},
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	active := engine.GetActiveWorkflow("u_snapshot")
	if active == nil {
		t.Fatal("expected active workflow")
	}
	active.CurrentPhase = PhaseCodingImplementation
	active.PhaseIndex = 3
	active.Intent.Goals[0] = "mutated"
	active.PhaseOutputs[PhaseCodingRequirements] = "external mutation"

	again := engine.GetActiveWorkflow("u_snapshot")
	if again.CurrentPhase != PhaseCodingRequirements || again.PhaseIndex != 0 {
		t.Fatalf("GetActiveWorkflow returned mutable engine state: %#v", again)
	}
	if again.Intent.Goals[0] != "original" || again.PhaseOutputs[PhaseCodingRequirements] != "" {
		t.Fatalf("GetActiveWorkflow snapshot mutation leaked into engine: %#v", again)
	}
}

type recordingArtifactSaver struct {
	tags []string
}

func (s *recordingArtifactSaver) SaveArtifact(_ string, _ string, tags []string, _ string) error {
	s.tags = append([]string(nil), tags...)
	return nil
}

func TestEngine_StartWorkflowWithOptionsStoresProjectPath(t *testing.T) {
	engine, _ := newTestEngine()
	state, err := engine.StartWorkflowWithOptions("u_project_start", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	}, WorkflowStartOptions{ProjectPath: " D:/workprj/aicoder "})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	if state.ProjectPath != "D:/workprj/aicoder" {
		t.Fatalf("state.ProjectPath = %q", state.ProjectPath)
	}
	active := engine.GetActiveWorkflow("u_project_start")
	if active == nil || active.ProjectPath != "D:/workprj/aicoder" {
		t.Fatalf("active workflow lost project path: %#v", active)
	}
}

func TestEngine_SetProjectPathPersistsAndRollsBackOnError(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_project_update", StructuredIntent{Category: WorkflowCoding, Summary: "build an app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := engine.SetProjectPath("u_project_update", " D:/project/a  "); err != nil {
		t.Fatalf("SetProjectPath failed: %v", err)
	}
	if got := engine.GetActiveWorkflow("u_project_update").ProjectPath; got != "D:/project/a" {
		t.Fatalf("ProjectPath after update = %q", got)
	}
	engine.store = failingWorkflowStateStore{}
	if err := engine.SetProjectPath("u_project_update", "D:/project/b"); err == nil {
		t.Fatal("SetProjectPath should propagate persistence failure")
	}
	if got := engine.GetActiveWorkflow("u_project_update").ProjectPath; got != "D:/project/a" {
		t.Fatalf("failed project path persistence must roll back, got %q", got)
	}
}

func TestEngine_SavePhaseOutputTagsArtifactWithWorkflowProjectPath(t *testing.T) {
	engine, _ := newTestEngine()
	saver := &recordingArtifactSaver{}
	engine.SetArtifactSaver(saver)
	if _, err := engine.StartWorkflowWithOptions("u_project_artifact", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	}, WorkflowStartOptions{ProjectPath: "D:/project/artifact"}); err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	makeCurrentPhaseExecutable(t, engine, "u_project_artifact")
	if phaseID, err := engine.SavePhaseOutput("u_project_artifact", reviewStateValidContent); err != nil || phaseID == "" {
		t.Fatalf("SavePhaseOutput failed: phase=%q err=%v", phaseID, err)
	}
	if !containsAll(strings.Join(saver.tags, "\n"), "workflow", string(WorkflowCoding), PhaseCodingRequirements, "D:/project/artifact") {
		t.Fatalf("artifact tags did not include workflow project path: %#v", saver.tags)
	}
}

func TestEngine_GetActiveWorkflowDeepSnapshotDoesNotMutateEngine(t *testing.T) {
	engine, _ := newTestEngine()
	workflowType := WorkflowType("deep_snapshot_form_test")
	engine.GetRegistry().Register(&WorkflowTemplate{
		Type:        workflowType,
		Name:        "deep snapshot form test",
		Description: "test template",
		Phases: []PhaseTemplate{{
			ID:          "collect",
			Name:        "Collect",
			Prompt:      "collect",
			Deliverable: "doc",
			InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{
				{Name: "project_name", Label: "Project", Type: "text", Required: true},
				{Name: "tech_stack", Label: "Stack", Type: "text"},
				{Name: "description", Label: "Description", Type: "textarea"},
				{Name: "nested", Label: "Nested", Type: "object"},
			}},
			ToolPolicy: ToolFilterDocOnly,
		}},
	})
	if _, err := engine.StartWorkflow("u_deep_snapshot", StructuredIntent{Category: workflowType, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	form := map[string]interface{}{
		"project_name": "Snapshot App",
		"tech_stack":   "go",
		"description":  "build the snapshot app",
		"nested":       map[string]interface{}{"items": []interface{}{"original"}},
	}
	if _, err := engine.SubmitPhaseForm("u_deep_snapshot", form); err != nil {
		t.Fatalf("SubmitPhaseForm failed: %v", err)
	}
	active := engine.GetActiveWorkflow("u_deep_snapshot")
	active.PhaseFormData["nested"].(map[string]interface{})["items"].([]interface{})[0] = "mutated"

	again := engine.GetActiveWorkflow("u_deep_snapshot")
	got := again.PhaseFormData["nested"].(map[string]interface{})["items"].([]interface{})[0]
	if got != "original" {
		t.Fatalf("nested PhaseFormData mutation leaked into engine: %#v", again.PhaseFormData)
	}
}

func TestEngine_SubmitPayloadAndFormDoNotRetainCallerMutableObjects(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u_input_alias", StructuredIntent{Category: WorkflowContractReview, Summary: "review"}); err != nil {
		t.Fatalf("StartWorkflow input failed: %v", err)
	}
	payload := &WorkflowInputPayload{Text: "source", Attachments: []WorkflowInputAttachment{{FileName: "before.pdf"}}}
	if _, err := engine.SubmitInputPayload("u_input_alias", payload); err != nil {
		t.Fatalf("SubmitInputPayload failed: %v", err)
	}
	payload.Text = "mutated"
	payload.Attachments[0].FileName = "after.pdf"
	inputState := engine.GetActiveWorkflow("u_input_alias")
	if inputState.InputPayload.Text != "source" || inputState.InputPayload.Attachments[0].FileName != "before.pdf" {
		t.Fatalf("input payload alias leaked into engine: %#v", inputState.InputPayload)
	}

	workflowType := WorkflowType("form_alias_snapshot_test")
	engine.GetRegistry().Register(&WorkflowTemplate{
		Type:        workflowType,
		Name:        "form alias snapshot test",
		Description: "test template",
		Phases: []PhaseTemplate{{
			ID:          "collect",
			Name:        "Collect",
			Prompt:      "collect",
			Deliverable: "doc",
			InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{
				{Name: "project_name", Label: "Project", Type: "text", Required: true},
				{Name: "tech_stack", Label: "Stack", Type: "text"},
				{Name: "description", Label: "Description", Type: "textarea"},
				{Name: "list", Label: "List", Type: "object"},
			}},
			ToolPolicy: ToolFilterDocOnly,
		}},
	})
	if _, err := engine.StartWorkflow("u_form_alias", StructuredIntent{Category: workflowType, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow form failed: %v", err)
	}
	form := map[string]interface{}{"project_name": "Alias App", "tech_stack": "go", "description": "before", "list": []interface{}{"a"}}
	if _, err := engine.SubmitPhaseForm("u_form_alias", form); err != nil {
		t.Fatalf("SubmitPhaseForm failed: %v", err)
	}
	form["description"] = "after"
	form["list"].([]interface{})[0] = "b"
	formState := engine.GetActiveWorkflow("u_form_alias")
	if formState.PhaseFormData["description"] != "before" || formState.PhaseFormData["list"].([]interface{})[0] != "a" {
		t.Fatalf("form data alias leaked into engine: %#v", formState.PhaseFormData)
	}
}

func TestEngine_ResponseSchemasAndInputRequirementAreSnapshots(t *testing.T) {
	engine, _ := newTestEngine()
	workflowType := WorkflowType("schema_snapshot_test")
	engine.GetRegistry().Register(&WorkflowTemplate{
		Type:          workflowType,
		Name:          "schema snapshot test",
		Description:   "test",
		RequiresInput: &InputRequirement{Description: "upload", FileTypes: []string{"pdf"}, AcceptText: true},
		Phases: []PhaseTemplate{{
			ID:          "collect",
			Name:        "Collect",
			Prompt:      "collect",
			Deliverable: "doc",
			InputSchema: &PhaseInputSchema{
				TitleI18N: map[string]string{"zh": "上下文"},
				Fields: []PhaseInputField{{
					Name:      "goal",
					Label:     "Goal",
					LabelI18N: map[string]string{"zh": "目标"},
					Type:      "text",
					Required:  true,
					Options:   []PhaseInputOption{{Label: "A", Value: "a", LabelI18N: map[string]string{"zh": "甲"}}},
				}},
			},
			ToolPolicy: ToolFilterDocOnly,
		}},
	})
	if _, err := engine.StartWorkflow("u_schema_snapshot", StructuredIntent{Category: workflowType, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	req := engine.GetInputRequirement("u_schema_snapshot")
	req.FileTypes[0] = "mutated"
	if got := engine.GetInputRequirement("u_schema_snapshot").FileTypes[0]; got != "pdf" {
		t.Fatalf("input requirement mutation leaked into template: %q", got)
	}
	resp, err := engine.SubmitInputPayload("u_schema_snapshot", &WorkflowInputPayload{Text: "source"})
	if err != nil {
		t.Fatalf("SubmitInputPayload failed: %v", err)
	}
	resp.FormSchema.Fields[0].Label = "Mutated"
	resp.FormSchema.Fields[0].Options[0].Label = "B"
	resp.FormSchema.TitleI18N["zh"] = "已变更"
	resp.FormSchema.Fields[0].LabelI18N["zh"] = "已变更"
	resp.FormSchema.Fields[0].Options[0].LabelI18N["zh"] = "乙"
	next, err := engine.HandleInput("u_schema_snapshot", "")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}
	if next.FormSchema.Fields[0].Label != "Goal" || next.FormSchema.Fields[0].Options[0].Label != "A" || next.FormSchema.TitleI18N["zh"] != "上下文" || next.FormSchema.Fields[0].LabelI18N["zh"] != "目标" || next.FormSchema.Fields[0].Options[0].LabelI18N["zh"] != "甲" {
		t.Fatalf("form schema mutation leaked into template: %#v", next.FormSchema.Fields[0])
	}
}

func TestEngine_StartWorkflowPropagatesPersistenceError(t *testing.T) {
	engine, _ := newTestEngine()
	engine.store = failingWorkflowStateStore{}
	if _, err := engine.StartWorkflow("u_start_save_error", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	}); err == nil {
		t.Fatal("StartWorkflow should propagate workflow-state persistence failure")
	}
	if engine.HasActiveWorkflow("u_start_save_error") {
		t.Fatal("failed start persistence must not publish an active workflow")
	}
}

func TestEngine_AdvancePhasePropagatesPersistenceErrorAndRollsBack(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u_advance_save_error", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	engine.store = failingWorkflowStateStore{}
	if _, err := engine.AdvancePhase("u_advance_save_error"); err == nil {
		t.Fatal("AdvancePhase should propagate workflow-state persistence failure")
	}
	ws := engine.GetActiveWorkflow("u_advance_save_error")
	if ws == nil || ws.CurrentPhase != PhaseCodingRequirements || ws.PhaseIndex != 0 {
		t.Fatalf("failed advance persistence must roll back current phase, got %#v", ws)
	}
}

func TestEngine_SubmitInputPayloadPropagatesPersistenceError(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u_input_save_error", StructuredIntent{
		Category: WorkflowContractReview,
		Summary:  "review a contract",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	engine.store = failingWorkflowStateStore{}
	if _, err := engine.SubmitInputPayload("u_input_save_error", &WorkflowInputPayload{Text: "contract text"}); err == nil {
		t.Fatal("SubmitInputPayload should propagate workflow-state persistence failure")
	}
	ws := engine.GetActiveWorkflow("u_input_save_error")
	if ws == nil || ws.InputReceived || ws.InputPayload != nil {
		t.Fatalf("failed input persistence must roll back in-memory evidence, got %#v", ws)
	}
}

func TestEngine_SubmitPhaseFormPropagatesPersistenceError(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u_form_save_error", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	engine.store = failingWorkflowStateStore{}
	if _, err := engine.SubmitPhaseForm("u_form_save_error", map[string]interface{}{
		"project_goal": "build an app",
	}); err == nil {
		t.Fatal("SubmitPhaseForm should propagate workflow-state persistence failure")
	}
	ws := engine.GetActiveWorkflow("u_form_save_error")
	if ws == nil || len(ws.PhaseFormData) != 0 || ws.PhaseFormSubmitted || ws.PhaseFormSkipped {
		t.Fatalf("failed form persistence must roll back in-memory form data, got %#v", ws)
	}
}

func TestEngine_SavePhaseOutputPropagatesPersistenceError(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u_output_save_error", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	makeCurrentPhaseExecutable(t, engine, "u_output_save_error")
	engine.store = failingWorkflowStateStore{}
	if phaseID, err := engine.SavePhaseOutput("u_output_save_error", reviewStateValidContent); err == nil || phaseID != "" {
		t.Fatalf("SavePhaseOutput should fail without reporting a durable phase, phase=%q err=%v", phaseID, err)
	}
	ws := engine.GetActiveWorkflow("u_output_save_error")
	if ws == nil {
		t.Fatal("failed output persistence should keep workflow active")
	}
	if ws.PhaseOutputs[PhaseCodingRequirements] != "" || ws.PendingReviewPhaseID != "" {
		t.Fatalf("failed output persistence must roll back output/review state, got %#v", ws)
	}
}

func TestEngine_CancelWorkflowPropagatesPersistenceErrorAndRollsBack(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u_cancel_save_error", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	engine.store = failingWorkflowStateStore{}
	if err := engine.CancelWorkflow("u_cancel_save_error"); err == nil {
		t.Fatal("CancelWorkflow should propagate workflow-state persistence failure")
	}
	ws := engine.GetActiveWorkflow("u_cancel_save_error")
	if ws == nil || ws.Status != WorkflowActive {
		t.Fatalf("failed cancel persistence must keep workflow active, got %#v", ws)
	}
}

func TestEngine_ApplyReviewIntentCancelPropagatesPersistenceErrorAndRollsBack(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u_review_cancel_save_error", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	saveReviewOutputForCurrentPhase(t, engine, "u_review_cancel_save_error")
	engine.store = failingWorkflowStateStore{}
	if _, err := engine.ApplyReviewIntent("u_review_cancel_save_error", ReviewIntentCancel, ""); err == nil {
		t.Fatal("ApplyReviewIntent cancel should propagate workflow-state persistence failure")
	}
	ws := engine.GetActiveWorkflow("u_review_cancel_save_error")
	if ws == nil || ws.Status != WorkflowActive || ws.PendingReviewPhaseID != PhaseCodingRequirements {
		t.Fatalf("failed review cancel persistence must roll back active review state, got %#v", ws)
	}
}

func TestEngine_SkipPhaseFormPropagatesPersistenceErrorAndRollsBack(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u_skip_form_save_error", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	engine.store = failingWorkflowStateStore{}
	if err := engine.SkipPhaseForm("u_skip_form_save_error"); err == nil {
		t.Fatal("SkipPhaseForm should propagate workflow-state persistence failure")
	}
	ws := engine.GetActiveWorkflow("u_skip_form_save_error")
	if ws == nil || len(ws.PhaseFormData) != 0 || ws.PhaseFormSubmitted || ws.PhaseFormSkipped {
		t.Fatalf("failed form skip persistence must roll back form data, got %#v", ws)
	}
}

func TestEngine_SubmitPhaseFormValidatesSchemaConstraints(t *testing.T) {
	engine, _ := newTestEngine()
	minLen := 3
	maxLen := 8
	min := 1.0
	max := 5.0
	workflowType := WorkflowType("form_schema_validation_test")
	engine.GetRegistry().Register(&WorkflowTemplate{
		Type:        workflowType,
		Name:        "form schema validation test",
		Description: "test template",
		Phases: []PhaseTemplate{{
			ID:          "collect",
			Name:        "Collect",
			Prompt:      "collect",
			Deliverable: "doc",
			InputSchema: &PhaseInputSchema{Fields: []PhaseInputField{
				{Name: "name", Label: "Name", Type: "text", Required: true, MinLength: &minLen, MaxLength: &maxLen, Pattern: `^[A-Z][A-Za-z]+$`},
				{Name: "size", Label: "Size", Type: "number", Required: true, Min: &min, Max: &max},
				{Name: "mode", Label: "Mode", Type: "select", Required: true, Options: []PhaseInputOption{{Label: "Fast", Value: "fast"}, {Label: "Safe", Value: "safe"}}},
				{Name: "targets", Label: "Targets", Type: "multiselect", Options: []PhaseInputOption{{Label: "Web", Value: "web"}, {Label: "CLI", Value: "cli"}}},
				{Name: "enabled", Label: "Enabled", Type: "boolean"},
			}},
			ToolPolicy: ToolFilterDocOnly,
		}},
	})
	if _, err := engine.StartWorkflow("u_form_constraints", StructuredIntent{Category: workflowType, Summary: "test"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	_, err := engine.SubmitPhaseForm("u_form_constraints", map[string]interface{}{
		"name":       "xy",
		"size":       9,
		"mode":       "turbo",
		"targets":    []interface{}{"web", "mobile"},
		"enabled":    "yes",
		"unexpected": "schema poison",
	})
	if err == nil {
		t.Fatal("SubmitPhaseForm should reject values that violate the phase schema")
	}
	if !strings.Contains(err.Error(), "unexpected (unknown field)") {
		t.Fatalf("SubmitPhaseForm should reject unknown fields at the schema boundary, got %v", err)
	}
	ws := engine.GetActiveWorkflow("u_form_constraints")
	if ws == nil || len(ws.PhaseFormData) != 0 || ws.PhaseFormSubmitted || ws.PhaseFormSkipped {
		t.Fatalf("invalid form submission must not mutate workflow state: %#v", ws)
	}
	resp, err := engine.SubmitPhaseForm("u_form_constraints", map[string]interface{}{
		"name":    "Alpha",
		"size":    3.0,
		"mode":    "safe",
		"targets": []interface{}{"web", "cli"},
		"enabled": true,
	})
	if err != nil {
		t.Fatalf("valid SubmitPhaseForm failed: %v", err)
	}
	if resp == nil || !resp.RunAgentLoop || strings.TrimSpace(resp.PhasePrompt) == "" {
		t.Fatalf("valid form should trigger phase loop, got %#v", resp)
	}
}
func TestEngine_SubmitPhaseFormValidatesRequiredFields(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u_form_required", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if _, err := engine.SubmitPhaseForm("u_form_required", map[string]interface{}{}); err == nil {
		t.Fatal("SubmitPhaseForm should reject missing required fields")
	}
}

func TestEngine_SubmitPhaseFormRejectsPhaseWithoutSchema(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.StartWorkflow("u_form_schema", StructuredIntent{
		Category: WorkflowCoding,
		Summary:  "build an app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	engine.mu.Lock()
	ws := engine.workflows["u_form_schema"]
	ws.CurrentPhase = PhaseCodingTechDesign
	ws.PhaseIndex = 1
	engine.mu.Unlock()
	if _, err := engine.SubmitPhaseForm("u_form_schema", map[string]interface{}{"anything": "value"}); err == nil {
		t.Fatal("SubmitPhaseForm should reject phases without an input schema")
	}
}
