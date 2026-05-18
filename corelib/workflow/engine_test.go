package workflow

import (
	"testing"
)

func TestEngine_NewCommandClearsWorkflowState(t *testing.T) {
	engine, _ := newTestEngine()
	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	_, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if !engine.HasActiveWorkflow("u1") {
		t.Fatal("expected active workflow")
	}

	// Simulate /new by cancelling the workflow
	err = engine.CancelWorkflow("u1")
	if err != nil {
		t.Fatalf("CancelWorkflow failed: %v", err)
	}
	if engine.HasActiveWorkflow("u1") {
		t.Error("workflow should be cleared after /new (cancel)")
	}
}

func TestEngine_CancelWorkflow(t *testing.T) {
	engine, _ := newTestEngine()
	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	_, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// Add some phase outputs to the engine-owned state; StartWorkflow returns a snapshot.
	engine.mu.Lock()
	ownedState := engine.workflows["u1"]
	ownedState.PhaseOutputs["requirements"] = "需求文档内容"
	engine.mu.Unlock()

	err = engine.CancelWorkflow("u1")
	if err != nil {
		t.Fatalf("CancelWorkflow failed: %v", err)
	}

	if engine.HasActiveWorkflow("u1") {
		t.Error("workflow should not be active after cancel")
	}
	if ownedState.Status != WorkflowCancelled {
		t.Errorf("expected cancelled status, got %s", ownedState.Status)
	}
	if ownedState.PhaseOutputs["requirements"] != "需求文档内容" {
		t.Error("phase outputs should be preserved after cancel")
	}
}

func TestEngine_CancelNoWorkflow(t *testing.T) {
	engine, _ := newTestEngine()
	err := engine.CancelWorkflow("nonexistent")
	if err == nil {
		t.Error("expected error when cancelling non-existent workflow")
	}
}

func TestEngine_CompleteWorkflowLifecycle(t *testing.T) {
	engine, cb := newTestEngine()
	registry := NewWorkflowRegistry()
	tmpl := registry.Match(WorkflowCoding)

	intent := StructuredIntent{Category: WorkflowCoding, Summary: "做一个CRM"}
	state, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// Verify initial state
	if state.PhaseIndex != 0 {
		t.Errorf("expected PhaseIndex=0, got %d", state.PhaseIndex)
	}
	if state.CurrentPhase != "requirements" {
		t.Errorf("expected CurrentPhase=requirements, got %s", state.CurrentPhase)
	}
	if state.Status != WorkflowActive {
		t.Errorf("expected active status, got %s", state.Status)
	}

	// Advance through all phases
	for i := 0; i < len(tmpl.Phases); i++ {
		ws := engine.GetActiveWorkflow("u1")
		if ws == nil {
			// Should only be nil after the last phase completed
			break
		}

		phase := tmpl.Phases[ws.PhaseIndex]

		if phase.NeedsConfirm {
			// Send normal input first (triggers RunAgentLoop, no advance)
			resp, err := engine.HandleInput("u1", "这是阶段输入内容")
			if err != nil {
				t.Fatalf("HandleInput at phase %d failed: %v", i, err)
			}
			if resp.Advance {
				t.Errorf("phase %d: non-confirm input should not advance NeedsConfirm phase", i)
			}

			saveReviewOutputForCurrentPhase(t, engine, "u1")
			resp, err = engine.HandleInput("u1", "确认")
			if err != nil {
				t.Fatalf("HandleInput confirm at phase %d failed: %v", i, err)
			}
			if !resp.PendingConfirm {
				t.Errorf("phase %d: HandleInput should request review intent classification", i)
			}
			resp, err = engine.ApplyReviewIntent("u1", ReviewIntentConfirm, "")
			if err != nil {
				t.Fatalf("ApplyReviewIntent confirm at phase %d failed: %v", i, err)
			}
			if !resp.Advance {
				t.Errorf("phase %d: classified confirm should advance NeedsConfirm phase", i)
			}
		} else {
			// Non-NeedsConfirm phase (e.g., implementation): advance via the engine-owned state.
			// In real usage, the GUI/TUI caller decides when to advance.
			engine.mu.Lock()
			owned := engine.workflows["u1"]
			_, err := engine.advancePhase("u1", owned, tmpl)
			engine.mu.Unlock()
			if err != nil {
				t.Fatalf("advancePhase at phase %d failed: %v", i, err)
			}
		}
	}

	// Workflow should be completed
	if engine.HasActiveWorkflow("u1") {
		t.Error("workflow should not be active after completion")
	}

	// Callbacks should have been called
	if len(cb.PhaseUpdates) == 0 {
		t.Error("expected phase update callbacks")
	}
}

func TestEngine_OpsMaintenanceExecutionDoesNotActivateOrchestrator(t *testing.T) {
	engine, _ := newTestEngine()
	if _, err := engine.StartWorkflow("u1", StructuredIntent{
		Category: WorkflowOpsMaintenance,
		Summary:  "restart a bounded service after risk approval",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	// Confirm every document/risk phase. The final full-tool phase should run
	// through the controlled execution prompt, not the generic task orchestrator.
	for i := 0; i < 4; i++ {
		saveReviewOutputForCurrentPhase(t, engine, "u1")
		resp, err := engine.ApplyReviewIntent("u1", ReviewIntentConfirm, "")
		if err != nil {
			t.Fatalf("ApplyReviewIntent phase %d failed: %v", i, err)
		}
		if i < 3 && !resp.Advance {
			t.Fatalf("phase %d should advance", i)
		}
		if i == 3 {
			if !resp.Advance || !resp.RunAgentLoop {
				t.Fatalf("risk policy confirmation should enter controlled execution, got advance=%v run=%v", resp.Advance, resp.RunAgentLoop)
			}
			if resp.ActivateOrchestrator {
				t.Fatal("ops controlled execution must not activate the generic task orchestrator")
			}
			if resp.ToolFilter != ToolFilterOpsControlled {
				t.Fatalf("controlled execution should use ops-controlled tools, got %s", resp.ToolFilter)
			}
		}
	}
}

func TestEngine_LLMNotConfiguredDegradation(t *testing.T) {
	// Engine with nil understanding manager — should still work for direct workflow start
	registry := NewWorkflowRegistry()
	cb := &MockEngineCallbacks{}
	engine := NewWorkflowEngine(registry, nil, NullStore{}, cb)

	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	state, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow without LLM should work: %v", err)
	}
	if state.Status != WorkflowActive {
		t.Errorf("expected active, got %s", state.Status)
	}

	// HasActiveUnderstanding should return false with nil understanding manager
	if engine.HasActiveUnderstanding("u1") {
		t.Error("should return false with nil understanding manager")
	}
}

func TestEngine_SQLiteUnavailableDegradation(t *testing.T) {
	// Engine with NullStore — pure in-memory mode
	registry := NewWorkflowRegistry()
	cb := &MockEngineCallbacks{}
	llm := &MockLLMCaller{Response: `{"reply":"ok","ready":false}`}
	iu := NewIntentUnderstandingManager(NullStore{}, llm, registry)
	engine := NewWorkflowEngine(registry, iu, NullStore{}, cb)

	intent := StructuredIntent{Category: WorkflowCoding, Summary: "test"}
	state, err := engine.StartWorkflow("u1", intent)
	if err != nil {
		t.Fatalf("StartWorkflow with NullStore should work: %v", err)
	}
	if state.Status != WorkflowActive {
		t.Errorf("expected active, got %s", state.Status)
	}

	saveReviewOutputForCurrentPhase(t, engine, "u1")

	resp, err := engine.HandleInput("u1", "确认")
	if err != nil {
		t.Fatalf("HandleInput with NullStore failed: %v", err)
	}
	if !resp.PendingConfirm {
		t.Error("HandleInput should request review intent classification")
	}
	resp, err = engine.ApplyReviewIntent("u1", ReviewIntentConfirm, "")
	if err != nil {
		t.Fatalf("ApplyReviewIntent with NullStore failed: %v", err)
	}
	if !resp.Advance {
		t.Error("classified confirm should advance phase")
	}

	// RestoreFromStore with NullStore should be no-op
	err = engine.RestoreFromStore()
	if err != nil {
		t.Errorf("RestoreFromStore with NullStore should not error: %v", err)
	}

	// CleanupExpired with NullStore should be no-op
	if err := engine.CleanupExpired(); err != nil {
		t.Errorf("CleanupExpired with NullStore should not error: %v", err)
	}
}

func TestEngine_HandleInputNoActiveWorkflow(t *testing.T) {
	engine, _ := newTestEngine()
	_, err := engine.HandleInput("nonexistent", "hello")
	if err == nil {
		t.Error("expected error for non-existent workflow")
	}
}

func TestEngine_StartWorkflowInvalidTemplate(t *testing.T) {
	engine, _ := newTestEngine()
	intent := StructuredIntent{Category: "nonexistent_type", Summary: "test"}
	_, err := engine.StartWorkflow("u1", intent)
	if err == nil {
		t.Error("expected error for non-existent template")
	}
}

func TestEngine_GetFilter(t *testing.T) {
	engine, _ := newTestEngine()
	filter := engine.GetFilter()
	if filter == nil {
		t.Error("GetFilter should return non-nil QuickFilter")
	}
}

func TestEngine_BuildPhasePromptNoWorkflow(t *testing.T) {
	engine, _ := newTestEngine()
	prompt := engine.BuildPhasePrompt("nonexistent")
	if prompt != "" {
		t.Error("BuildPhasePrompt should return empty for non-existent workflow")
	}
}

func TestEngine_GetPhaseToolFilterNoWorkflow(t *testing.T) {
	engine, _ := newTestEngine()
	policy := engine.GetPhaseToolFilter("nonexistent")
	if policy != ToolFilterNone {
		t.Errorf("expected ToolFilterNone, got %s", policy)
	}
}

func setWorkflowPhaseForTest(t *testing.T, engine *WorkflowEngine, userID, phaseID string) *WorkflowState {
	t.Helper()
	engine.mu.Lock()
	defer engine.mu.Unlock()
	state := engine.workflows[userID]
	if state == nil {
		t.Fatalf("workflow not found for %s", userID)
	}
	tmpl := engine.GetRegistry().Match(state.Type)
	if tmpl == nil {
		t.Fatalf("template not found for %s", state.Type)
	}
	for i := range tmpl.Phases {
		if tmpl.Phases[i].ID == phaseID {
			state.PhaseIndex = i
			state.CurrentPhase = phaseID
			return state
		}
	}
	t.Fatalf("phase %q not found for %s", phaseID, state.Type)
	return nil
}

func TestEngine_GetOpsApprovedCommandsFromRiskPolicyOutput(t *testing.T) {
	registry := NewWorkflowRegistry()
	RegisterBuiltinTemplates(registry)
	engine := NewWorkflowEngine(registry, nil, nil, nil)
	state, err := engine.StartWorkflow("u_ops_manifest", StructuredIntent{Category: WorkflowOpsMaintenance})
	if err != nil {
		t.Fatalf("StartWorkflow: %v", err)
	}
	state = setWorkflowPhaseForTest(t, engine, state.UserID, "controlled_execution")
	state.PhaseOutputs["risk_policy"] = `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	got := engine.GetOpsApprovedCommands("u_ops_manifest")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %#v", len(got), got)
	}
	if got[0].Tool != "bash" || got[0].Command != "systemctl restart nginx" {
		t.Fatalf("unexpected command manifest: %#v", got)
	}
}

func TestEngine_GetOpsApprovedCommandsOnlyDuringControlledExecution(t *testing.T) {
	registry := NewWorkflowRegistry()
	RegisterBuiltinTemplates(registry)
	engine := NewWorkflowEngine(registry, nil, nil, nil)
	state, err := engine.StartWorkflow("u_ops_manifest_before_execution", StructuredIntent{Category: WorkflowOpsMaintenance})
	if err != nil {
		t.Fatalf("StartWorkflow: %v", err)
	}
	state = setWorkflowPhaseForTest(t, engine, state.UserID, "risk_policy")
	state.PhaseOutputs["risk_policy"] = `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	if got := engine.GetOpsApprovedCommands("u_ops_manifest_before_execution"); len(got) != 0 {
		t.Fatalf("risk policy output should not expose approved commands before controlled_execution: %#v", got)
	}
	state = setWorkflowPhaseForTest(t, engine, state.UserID, "controlled_execution")
	if got := engine.GetOpsApprovedCommands("u_ops_manifest_before_execution"); len(got) != 1 {
		t.Fatalf("controlled_execution should expose approved commands, got %#v", got)
	}
}

func TestEngine_GetOpsApprovedCommandsIgnoresDeniedRiskPolicy(t *testing.T) {
	registry := NewWorkflowRegistry()
	RegisterBuiltinTemplates(registry)
	engine := NewWorkflowEngine(registry, nil, nil, nil)
	state, err := engine.StartWorkflow("u_ops_denied_manifest", StructuredIntent{Category: WorkflowOpsMaintenance})
	if err != nil {
		t.Fatalf("StartWorkflow: %v", err)
	}
	state = setWorkflowPhaseForTest(t, engine, state.UserID, "controlled_execution")
	state.PhaseOutputs["risk_policy"] = `
decision: deny
risk_level: L4
approval_required: none
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	if got := engine.GetOpsApprovedCommands("u_ops_denied_manifest"); len(got) != 0 {
		t.Fatalf("denied policy should not expose approved commands: %#v", got)
	}
}

func TestEngine_GetOpsApprovedCommandsPreservesPolicyStrengthMetadata(t *testing.T) {
	registry := NewWorkflowRegistry()
	RegisterBuiltinTemplates(registry)
	engine := NewWorkflowEngine(registry, nil, nil, nil)
	state, err := engine.StartWorkflow("u_ops_close_all_manifest", StructuredIntent{Category: WorkflowOpsMaintenance})
	if err != nil {
		t.Fatalf("StartWorkflow: %v", err)
	}
	state = setWorkflowPhaseForTest(t, engine, state.UserID, "controlled_execution")
	state.PhaseOutputs["risk_policy"] = `
decision: approval_required
risk_level: L3
approval_required: double
allowed_commands:
  - tool: ssh
    action: close_all
    command: "all"
`
	got := engine.GetOpsApprovedCommands("u_ops_close_all_manifest")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %#v", len(got), got)
	}
	if got[0].Action != "close_all" || got[0].RiskLevel != OpsRiskLevelL3 || got[0].ApprovalRequirement != OpsApprovalRequirementDouble {
		t.Fatalf("approved close_all lost policy strength metadata: %#v", got[0])
	}
}
