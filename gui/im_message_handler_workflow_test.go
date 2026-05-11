package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

type mockLLMCallerGUI struct {
	Response string
	Err      error
}

func (m *mockLLMCallerGUI) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	return m.Response, nil
}

type mockEngineCallbacksGUI struct {
	SentTexts []string
}

func (m *mockEngineCallbacksGUI) SendTextToUser(userID, text string) error {
	m.SentTexts = append(m.SentTexts, text)
	return nil
}

func (m *mockEngineCallbacksGUI) EmitPhaseUpdate(userID string, state *workflow.WorkflowState) error {
	return nil
}

func (m *mockEngineCallbacksGUI) EmitDocUpdate(userID, phaseID, content string) error {
	return nil
}

func (m *mockEngineCallbacksGUI) EmitGateResult(userID, phaseID string, result *workflow.QualityGateResult) error {
	return nil
}

func (m *mockEngineCallbacksGUI) GetLang() string { return "zh" }

func setupWorkflowTestHandler(llm workflow.LLMCaller) (*IMMessageHandler, *mockEngineCallbacksGUI) {
	registry := workflow.NewWorkflowRegistry()
	cb := &mockEngineCallbacksGUI{}
	understanding := workflow.NewIntentUnderstandingManager(workflow.NullStore{}, llm, registry)
	engine := workflow.NewWorkflowEngine(registry, understanding, workflow.NullStore{}, cb)

	app := &App{workflowEngine: engine}
	handler := &IMMessageHandler{app: app, confirmationStore: newAIConfirmationStore("")}
	return handler, cb
}

func TestBugCondition_CategoryNoneReadyTrue_ShouldNotCallStartWorkflow(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"coding","summary":"build a system","confidence":0.7,"ready":false},"reply":"need more detail","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-user-none"

	if _, err := understanding.Start(userID, "summarize a paper"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	llm.Response = `{"intent":{"category":"none","summary":"content task","confidence":0.9,"ready":true},"reply":"ok","ready":true}`
	resp := handler.handleActiveUnderstanding(engine, userID, "start")
	if resp != nil {
		t.Fatalf("category none should fall through without starting a workflow, got %#v", resp)
	}
	if engine.HasActiveWorkflow(userID) {
		t.Fatal("category none should not create an active workflow")
	}
}

func TestBugCondition_CategoryEmptyReadyTrue_ShouldNotCallStartWorkflow(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"coding","summary":"build a system","confidence":0.7,"ready":false},"reply":"need more detail","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-user-empty"

	if _, err := understanding.Start(userID, "organize meeting notes"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	llm.Response = `{"intent":{"category":"","summary":"simple task","confidence":0.5,"ready":true},"reply":"ok","ready":true}`
	resp := handler.handleActiveUnderstanding(engine, userID, "start")
	if resp != nil {
		t.Fatalf("empty category should fall through without starting a workflow, got %#v", resp)
	}
	if engine.HasActiveWorkflow(userID) {
		t.Fatal("empty category should not create an active workflow")
	}
}

func TestActiveUnderstanding_ErrorPreservesSession(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"presentation_design","summary":"make slides","confidence":0.8,"ready":false},"reply":"please add style","ready":false}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-user-understanding-error"

	if _, err := understanding.Start(userID, "make a memorial PPT"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	llm.Err = fmt.Errorf("temporary LLM failure")
	resp := handler.handleActiveUnderstanding(engine, userID, "energetic public theme")
	if resp == nil || strings.TrimSpace(resp.Text) == "" {
		t.Fatalf("expected user-visible retry guidance, got %#v", resp)
	}
	if !understanding.HasActiveSession(userID) {
		t.Fatal("active understanding session should survive transient HandleInput error")
	}
}

func TestPreservation_ValidWorkflowCategory_AsksBeforeStartWorkflow(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"coding","summary":"build a system","confidence":0.7,"ready":false},"reply":"need more detail","ready":false}`,
	}
	handler, cb := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	understanding := engine.GetUnderstanding()
	userID := "test-preservation-coding"

	if _, err := understanding.Start(userID, "help me build a project"); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	llm.Response = `{"intent":{"category":"coding","summary":"user confirmed start","confidence":0.9,"ready":true},"reply":"ok, start working","ready":true}`
	resp := handler.handleActiveUnderstanding(engine, userID, "start")
	if resp == nil || resp.Confirmation == nil {
		t.Fatalf("coding workflow should ask before startup, got %#v", resp)
	}
	if engine.HasActiveWorkflow(userID) {
		t.Fatal("workflow should not start before user confirmation")
	}
	if got := handler.confirmationStore.get(userID); got == nil || got.WorkflowType != string(workflow.WorkflowCoding) {
		t.Fatalf("expected pending workflow confirmation, got %#v", got)
	}
	if len(cb.SentTexts) != 0 {
		t.Fatal("workflow startup overview should wait for confirmation")
	}
}

func TestWorkflowConfirmation_ApproveStartsWorkflow(t *testing.T) {
	handler, cb := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-confirm-start"
	item := buildPendingWorkflowConfirmation(userID, "build a project", workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "build a project",
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "")
	handler.confirmationStore.set(item)

	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: buildConfirmationActionCommand("confirm", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)
	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if result.Handled {
		t.Fatalf("non-input workflow should continue into agent loop after startup, got handled response %#v", result.Response)
	}
	if !result.ConfirmedResume || !result.WorkflowAgentLoop {
		t.Fatalf("expected confirmed workflow agent loop marker, got %#v", result)
	}
	if !engine.HasActiveWorkflow(userID) {
		t.Fatal("confirmed workflow should create an active workflow")
	}
	if len(cb.SentTexts) == 0 {
		t.Fatal("confirmed workflow startup should send an overview message")
	}
}

func TestWorkflowConfirmation_DirectExecutionSkipsWorkflowOnce(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-confirm-direct"
	item := buildPendingWorkflowConfirmation(userID, "build a project", workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "build a project",
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "")
	handler.confirmationStore.set(item)
	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: buildConfirmationActionCommand("cancel", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)

	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.SkipWorkflowOnce || !result.SkipExecutionConfirm {
		t.Fatalf("expected direct execution to skip workflow once, got %#v", result)
	}
	if trimmed != "build a project" || msg.Text != "build a project" {
		t.Fatalf("expected original text restored for agent loop, got trimmed=%q msg=%q", trimmed, msg.Text)
	}
	if handler.app.workflowEngine.HasActiveWorkflow(userID) {
		t.Fatal("direct execution should not start workflow")
	}
	if shouldRunWorkflowInterception(false, result.SkipWorkflowOnce, handler.app.workflowEngine, *msg, false) {
		t.Fatal("direct execution must skip workflow interception for the replayed task")
	}
	if !shouldRunWorkflowInterception(false, false, handler.app.workflowEngine, IMUserMessage{UserID: userID, Platform: "desktop", Text: "build a project"}, false) {
		t.Fatal("normal foreground messages with a workflow engine should still run workflow interception")
	}
}

func TestWorkflowConfirmation_DirectExecutionUsesSummaryWhenGoalsMissing(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-confirm-direct-summary"
	item := buildPendingWorkflowConfirmation(userID, "start", workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "Build the login flow and tests",
		Confidence: 0.9,
	}, "")
	handler.confirmationStore.set(item)
	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: buildConfirmationActionCommand("cancel", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)

	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.SkipWorkflowOnce || !result.SkipExecutionConfirm {
		t.Fatalf("expected direct execution to skip workflow once, got %#v", result)
	}
	if trimmed != "Build the login flow and tests" {
		t.Fatalf("expected direct execution to preserve semantic summary, got %q", trimmed)
	}
}

func TestWorkflowConfirmation_FreeformRevisionReentersRouting(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-confirm-revise"
	item := buildPendingWorkflowConfirmation(userID, "build a project", workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "")
	handler.confirmationStore.set(item)
	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: "Actually make it a presentation workflow"}
	trimmed := strings.TrimSpace(msg.Text)

	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if result.Handled || result.SkipWorkflowOnce || result.ConfirmedResume || !result.ReprocessAsFreshTask || !result.SkipExecutionConfirm {
		t.Fatalf("free-form revision should re-enter routing, got %#v", result)
	}
	if !strings.Contains(trimmed, "build a project") || !strings.Contains(trimmed, "Actually make it a presentation workflow") {
		t.Fatalf("expected revised routing text to include original task and clarification, got %q", trimmed)
	}
	if got := handler.confirmationStore.get(userID); got != nil {
		t.Fatalf("expected old pending workflow confirmation to be cleared, got %#v", got)
	}
}

func TestWorkflowConfirmation_ExplicitGoalTakesPrecedenceOverSummary(t *testing.T) {
	item := buildPendingWorkflowConfirmation("u1", "build a project", workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "Internal routing metadata",
		Goals:      []string{"build a project"},
		Confidence: 0.92,
	}, "")
	if strings.Contains(item.Summary, "Internal routing metadata") {
		t.Fatalf("workflow confirmation should ignore summary when explicit goal exists: %q", item.Summary)
	}
	if item.OriginalText != "build a project" {
		t.Fatalf("expected original task text from goal, got %q", item.OriginalText)
	}
}

func TestWorkflowConfirmation_NormalizesEmptyGoals(t *testing.T) {
	item := buildPendingWorkflowConfirmation("u1", "fallback text", workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "summary text",
		Goals:      []string{"", "  real task  "},
		Confidence: 0.92,
	}, "")
	if item.OriginalText != "real task" {
		t.Fatalf("expected first non-empty goal as original text, got %q", item.OriginalText)
	}
	if len(item.WorkflowGoals) != 1 || item.WorkflowGoals[0] != "real task" {
		t.Fatalf("expected normalized workflow goals, got %#v", item.WorkflowGoals)
	}
}

func TestFilterToolsForOpsControlledAllowsOnlyOperationalTools(t *testing.T) {
	tools := []map[string]interface{}{
		toolDef("ssh", "ssh", nil, nil),
		toolDef("bash", "bash", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("task", "task", nil, nil),
		toolDef("create_session", "create session", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
	}

	filtered := workflow.FilterToolDefinitions(workflow.ToolFilterOpsControlled, tools)
	names := make(map[string]bool, len(filtered))
	for _, tool := range filtered {
		names[extractToolName(tool)] = true
	}

	for _, allowed := range []string{"ssh", "bash", "read_file"} {
		if !names[allowed] {
			t.Fatalf("expected ops-controlled filter to keep %s, got %#v", allowed, names)
		}
	}
	for _, blocked := range []string{"task", "create_session", "edit_file"} {
		if names[blocked] {
			t.Fatalf("expected ops-controlled filter to block %s, got %#v", blocked, names)
		}
	}
}

func TestApplyWorkflowToolFilterUsesActiveOpsPhasePolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-filter-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(workflow.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}

	tools := []map[string]interface{}{
		toolDef("bash", "bash", nil, nil),
		toolDef("task", "task", nil, nil),
	}
	filtered := handler.applyWorkflowToolFilter(userID, tools)
	names := make(map[string]bool, len(filtered))
	for _, tool := range filtered {
		names[extractToolName(tool)] = true
	}
	if !names["bash"] || names["task"] {
		t.Fatalf("expected active ops phase to keep bash and block task, got %#v", names)
	}
}

func TestWorkflowToolExecutionGuardBlocksDisallowedTool(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(workflow.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "task",
				Arguments: `{"action":"run"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "not allowed") {
		t.Fatalf("expected rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardBlocksHighRiskCommandArguments(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-command-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(workflow.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "bash",
				Arguments: `{"command":"rm -rf / --no-preserve-root"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "reviewed runbook") {
		t.Fatalf("expected high-risk rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardBlocksCommandOutsideApprovedManifest(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-manifest-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	state.PhaseOutputs["risk_policy"] = `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: bash
    command: "systemctl restart nginx"
`
	tmpl := handler.app.workflowEngine.GetRegistry().Match(workflow.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "bash",
				Arguments: `{"command":"systemctl restart mysql"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "approved risk-policy") {
		t.Fatalf("expected approved manifest rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardBlocksMutatingCommandWithoutManifest(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-no-manifest-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(workflow.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "bash",
				Arguments: `{"command":"systemctl restart nginx"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "allowed_commands") {
		t.Fatalf("expected missing manifest rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardBlocksSSHUploadWithoutManifest(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-ssh-upload-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(workflow.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "ssh",
				Arguments: `{"action":"upload","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "allowed_commands") {
		t.Fatalf("expected missing manifest rejection text, got %q", result.Text)
	}
}

func TestWorkflowToolExecutionGuardAllowsApprovedSSHUpload(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-ssh-upload-approved-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	state.PhaseOutputs["risk_policy"] = `
decision: approval_required
risk_level: L2
approval_required: single
allowed_commands:
  - tool: ssh
    action: upload
    target: prod-session
    command: "apply.sh -> /tmp/apply.sh"
`
	tmpl := handler.app.workflowEngine.GetRegistry().Match(workflow.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "ssh",
				Arguments: `{"action":"upload","session_id":"prod-session","local_path":"other.sh","remote_path":"/tmp/apply.sh"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected unapproved descriptor rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "approved risk-policy") {
		t.Fatalf("expected approved manifest rejection text, got %q", result.Text)
	}

	result = handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		ToolCall: llm.ToolCall{
			ID: "call_2",
			Function: llm.ToolCallFunction{
				Name:      "ssh",
				Arguments: `{"action":"upload","session_id":"staging-session","local_path":"apply.sh","remote_path":"/tmp/apply.sh"}`,
			},
		},
	})
	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected wrong-target rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
}

func TestWorkflowToolExecutionGuardHonorsSkipNeedsConfirmGate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-skip-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowOpsMaintenance,
		Summary:  "server maintenance",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := handler.app.workflowEngine.GetRegistry().Match(workflow.WorkflowOpsMaintenance)
	for i, phase := range tmpl.Phases {
		if phase.ID == "controlled_execution" {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID:           userID,
		SkipWorkflowGate: true,
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "task",
				Arguments: `{"action":"run"}`,
			},
		},
	})

	if result.FailureKind == toolFailurePolicyRejected {
		t.Fatalf("skip gate should bypass workflow policy rejection, got %q", result.Text)
	}
}
