package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

type mockLLMCallerGUI struct {
	Response string
	Err      error
	Calls    int
}

func (m *mockLLMCallerGUI) DoSimpleLLMRequest(messages []interface{}, timeout time.Duration) (string, error) {
	m.Calls++
	if m.Err != nil {
		return "", m.Err
	}
	return m.Response, nil
}

type mockEngineCallbacksGUI struct {
	SentTexts []string
}

type failingWorkflowStateStoreGUI struct {
	workflow.NullStore
	fail bool
}

func (s *failingWorkflowStateStoreGUI) SaveWorkflowState(_ *workflow.WorkflowState) error {
	if s != nil && s.fail {
		return errors.New("save workflow state failed")
	}
	return nil
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

func TestApprovePendingWorkflowConfirmationCreatesProjectDirectory(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-workflow-start-creates-project-dir"
	projectPath := filepath.Join(t.TempDir(), "new-project", "nested")

	result := handler.approvePendingWorkflowConfirmation(userID, &pendingConfirmation{
		UserID:          userID,
		OriginalText:    "build a desktop game",
		Summary:         "build a desktop game",
		WorkflowType:    string(workflow.WorkflowCoding),
		WorkflowSummary: "build a desktop game",
		WorkflowGoals:   []string{"build a desktop game"},
		LastProjectPath: projectPath,
	}, "desktop")

	if result.Handled && result.Response != nil && result.Response.Error != "" {
		t.Fatalf("approvePendingWorkflowConfirmation returned error: %s", result.Response.Error)
	}
	info, err := os.Stat(projectPath)
	if err != nil {
		t.Fatalf("workflow start should create project directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("workflow project path should be directory, got file: %s", projectPath)
	}
	state := handler.app.workflowEngine.GetActiveWorkflow(userID)
	if state == nil {
		t.Fatal("workflow should be active after confirmation")
	}
	if state.ProjectPath != projectPath {
		t.Fatalf("workflow ProjectPath = %q, want %q", state.ProjectPath, projectPath)
	}
}

func TestSetWorkflowWorkingDirCreatesDirectoryAndPersistsProjectPath(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	adapter := NewGUIWorkflowAdapter(handler.app, engine)
	engine.SetCallbacks(adapter)
	userID := desktopUserID
	projectPath := filepath.Join(t.TempDir(), "selected-project")

	if _, err := engine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a desktop game",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	handler.app.SetWorkflowWorkingDir("  " + projectPath + "  ")

	info, err := os.Stat(projectPath)
	if err != nil {
		t.Fatalf("SetWorkflowWorkingDir should create directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("workflow working dir should be directory, got file: %s", projectPath)
	}
	if got := adapter.GetWorkingDir(); got != projectPath {
		t.Fatalf("adapter working dir = %q, want %q", got, projectPath)
	}
	state := engine.GetActiveWorkflow(userID)
	if state == nil || state.ProjectPath != projectPath {
		t.Fatalf("workflow ProjectPath = %#v, want %q", state, projectPath)
	}
}

func TestWorkflowInterceptionLowConfidenceUnknownStillReachesUnderstandingConfirmation(t *testing.T) {
	llm := &mockLLMCallerGUI{
		Response: `{"intent":{"category":"presentation_design","summary":"Create community anniversary PPT","goals":["Build a polished deck"],"constraints":[],"confidence":0.82,"ready":true},"reply":"Ready to confirm presentation workflow.","ready":true}`,
	}
	handler, _ := setupWorkflowTestHandler(llm)
	handler.unifiedClassifier = intent.New(intent.Config{})
	userID := "test-low-conf-uic-understanding-confirm"

	resp := handler.handleWorkflowInterception(userID, "create a five year community anniversary ppt", "desktop")
	if llm.Calls == 0 {
		t.Fatal("low-confidence unknown UIC result must still call IntentUnderstandingManager")
	}
	if resp == nil {
		t.Fatal("ready workflow from IntentUnderstandingManager should return confirmation response")
	}
	if handler.app.workflowEngine.HasActiveWorkflow(userID) {
		t.Fatal("workflow should wait for confirmation instead of starting immediately")
	}
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
	}, "", "zh")
	handler.confirmationStore.set(item)

	msg := &IMUserMessage{UserID: userID, Platform: "desktop", Text: buildConfirmationActionCommand("confirm", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)
	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.Handled || result.Response == nil || (!strings.Contains(result.Response.Text, "right-side panel") && !strings.Contains(result.Response.Text, "右侧面板")) {
		t.Fatalf("confirmed workflow should show the structured phase form, got %#v", result)
	}
	if result.WorkflowAgentLoop {
		t.Fatalf("form-first workflow should not start the agent loop before form submission, got %#v", result)
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
	}, "", "zh")
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
	}, "", "zh")
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
	}, "", "zh")
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
	}, "", "zh")
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
	}, "", "zh")
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

func TestWorkflowToolExecutionGuardBlocksDocOnlyCodingDelegate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "doc-only-delegate-guard-user"
	if _, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	original := runTaskWithSubAgent
	t.Cleanup(func() { runTaskWithSubAgent = original })
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		t.Fatal("doc-only workflow phase must reject delegate_task before CodingSubAgent starts")
		return nil
	}

	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		Context: &LoopContext{
			SkipNeedsConfirmGate: true,
			WorkflowAgentLoop:    true,
		},
		ToolCall: llm.ToolCall{
			ID: "call_1",
			Function: llm.ToolCallFunction{
				Name:      "delegate_task",
				Arguments: `{"agent":"coding_workflow","request":"implement the project"}`,
			},
		},
	})

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("expected policy rejection, got kind=%q text=%q", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "not allowed") {
		t.Fatalf("expected workflow policy rejection text, got %q", result.Text)
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

func TestWorkflowToolExecutionGuardRejectsSkipNeedsConfirmGateWhenWorkflowActive(t *testing.T) {
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

	if result.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("active workflow should ignore skip gate and enforce workflow policy, got kind=%q text=%q", result.FailureKind, result.Text)
	}
}

func TestWorkflowToolExecutionGuardAllowsSkipNeedsConfirmGateWithoutActiveWorkflow(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "ops-guard-skip-no-workflow-user"

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
		t.Fatalf("skip gate without active workflow should bypass workflow policy rejection, got %q", result.Text)
	}
}

func TestWorkflowAttachmentBypass_AllowsRequiredWorkflowInput(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-input-attachment"
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowContractReview,
		Summary:  "review uploaded contract",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	bypass := workflowAttachmentBypass(engine, userID, []MessageAttachment{{
		Type:     "image",
		FileName: "contract.png",
		MimeType: "image/png",
		Size:     64,
	}}, "")
	if bypass {
		t.Fatal("attachment must be routed into a workflow that is waiting for required input")
	}
}

func TestRouteWorkflowIMMessageSubmitsWaitingInputAfterInterception(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-route-input-attachment"
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowContractReview,
		Summary:  "review uploaded contract",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	result := handler.routeWorkflowIMMessage(IMUserMessage{
		UserID: userID,
		Text:   "",
		Attachments: []MessageAttachment{{
			Type:     "file",
			FileName: "contract.pdf",
			MimeType: "application/pdf",
			Size:     4096,
		}},
	}, "", false, false)
	if result.Response != nil || !result.WorkflowAgentLoop {
		t.Fatalf("attachment input should start workflow agent loop without immediate response: %#v", result)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || !ws.InputReceived || ws.InputPayload == nil || len(ws.InputPayload.Attachments) != 1 {
		t.Fatalf("workflow attachment input was not persisted: %#v", ws)
	}
	if ws.InputPayload.Attachments[0].FileName != "contract.pdf" {
		t.Fatalf("unexpected attachment payload: %#v", ws.InputPayload.Attachments)
	}
}
func TestSubmitWorkflowInputIfWaitingPersistsPayloadAndStartsLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-submit-input"
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowContractReview,
		Summary:  "review uploaded contract",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp, handled := handler.submitWorkflowInputIfWaiting(engine, userID, "contract body", []MessageAttachment{{
		Type:     "file",
		FileName: "contract.docx",
		MimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Size:     2048,
	}}, "")
	if !handled || resp != nil {
		t.Fatalf("expected workflow input to be consumed and agent loop to continue, handled=%v resp=%#v", handled, resp)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || !ws.InputReceived || ws.InputPayload == nil {
		t.Fatalf("workflow input payload was not persisted: %#v", ws)
	}
	if ws.InputPayload.Text != "contract body" || len(ws.InputPayload.Attachments) != 1 {
		t.Fatalf("unexpected workflow input payload: %#v", ws.InputPayload)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("workflow agent loop marker was not set after input submission")
	}
	prompt, ok := handler.stashedPhasePrompt.Load(userID)
	if !ok || !strings.Contains(prompt.(string), "contract.docx") {
		t.Fatalf("stashed phase prompt should contain submitted input evidence, got %#v", prompt)
	}
}

func TestSubmitWorkflowInputIfWaitingStopsAtFirstPhaseFormGate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-submit-input-form-gate"
	workflowType := workflow.WorkflowType("gui_input_form_gate_test")
	engine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:          workflowType,
		Name:          "gui input form gate test",
		Description:   "test template",
		RequiresInput: &workflow.InputRequirement{Description: "source document", AcceptText: true},
		Phases: []workflow.PhaseTemplate{{
			ID:          "collect_context",
			Name:        "Collect Context",
			Prompt:      "collect context",
			Deliverable: "context document",
			InputSchema: &workflow.PhaseInputSchema{Title: "Context", Fields: []workflow.PhaseInputField{{Name: "goal", Label: "Goal", Type: "text", Required: true}}},
			ToolPolicy:  workflow.ToolFilterDocOnly,
		}},
	})
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflowType, Summary: "review source"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp, handled := handler.submitWorkflowInputIfWaiting(engine, userID, "source text", nil, "")
	if !handled || resp == nil || (!strings.Contains(resp.Text, "right-side panel") && !strings.Contains(resp.Text, "右侧面板")) {
		t.Fatalf("input should return form guidance instead of starting loop, handled=%v resp=%#v", handled, resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("form-gated input must not set workflow agent loop marker")
	}
	if _, ok := handler.stashedPhasePrompt.Load(userID); ok {
		t.Fatal("form-gated input must not stash a phase prompt before form submission")
	}
}

func TestWorkflowFormSubmitContinuesSameWorkflowUser(t *testing.T) {
	userID := "desktop-user:C:/Users/ma139"
	registry := workflow.NewWorkflowRegistry()
	understanding := workflow.NewIntentUnderstandingManager(workflow.NullStore{}, &mockLLMCallerGUI{}, registry)
	engine := workflow.NewWorkflowEngine(registry, understanding, workflow.NullStore{}, &mockEngineCallbacksGUI{})
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	handler := NewIMMessageHandlerStandalone(StandaloneConfig{
		WorkflowEngine:    engine,
		UnifiedClassifier: intent.New(intent.Config{}),
		LLMConfigFunc: func() corelib.MaclawLLMConfig {
			return corelib.MaclawLLMConfig{URL: "http://localhost:8080/v1", Model: "test-model", Key: "test-key"}
		},
	})
	defer handler.memory.Stop()
	app := &App{workflowEngine: engine, remoteSessions: NewRemoteSessionManager(nil)}
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	handler.app = app

	resp := app.handleWorkflowFormAgentViewSubmit(workflow.PhaseCodingRequirements, map[string]interface{}{
		workflowFormUserIDField:     userID,
		workflowFormWorkflowIDField: engine.GetActiveWorkflow(userID).ID,
		"project_name":              "snake",
		"tech_stack":                "cpp",
		"description":               "graphical game",
	}, "req-workflow-form")
	if resp == nil || resp.Error != "" {
		t.Fatalf("form submit failed: %#v", resp)
	}
	if !resp.Deferred || resp.RequestID != "req-workflow-form" {
		t.Fatalf("form submit continuation should preserve request id and defer response, got %#v", resp)
	}
	for i := 0; i < 50; i++ {
		if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("direct workflow continuation should consume marker for the same user")
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || !ws.PhaseFormSubmitted {
		t.Fatalf("form should be submitted on original workflow user, got %#v", ws)
	}
	if ws := engine.GetActiveWorkflow(desktopUserID); ws != nil {
		t.Fatalf("form submit must not fork workflow onto generic desktop user: %#v", ws)
	}
}

func TestWorkflowFormSubmitRejectsHiddenPhaseMismatch(t *testing.T) {
	userID := "desktop-user:C:/Users/ma139"
	registry := workflow.NewWorkflowRegistry()
	understanding := workflow.NewIntentUnderstandingManager(workflow.NullStore{}, &mockLLMCallerGUI{}, registry)
	engine := workflow.NewWorkflowEngine(registry, understanding, workflow.NullStore{}, &mockEngineCallbacksGUI{})
	state, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	handler := NewIMMessageHandlerStandalone(StandaloneConfig{WorkflowEngine: engine})
	defer handler.memory.Stop()
	app := &App{workflowEngine: engine, remoteSessions: NewRemoteSessionManager(nil)}
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	handler.app = app

	resp := app.handleWorkflowFormAgentViewSubmit(workflow.PhaseCodingRequirements, map[string]interface{}{
		workflowFormPhaseField:      "stale_phase",
		workflowFormUserIDField:     userID,
		workflowFormWorkflowIDField: state.ID,
		"project_name":              "snake",
	}, "req-workflow-form")
	if resp == nil || resp.Error == "" {
		t.Fatalf("phase mismatch should fail, got %#v", resp)
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || ws.PhaseFormSubmitted {
		t.Fatalf("phase-mismatched submit must not mutate workflow form state, got %#v", ws)
	}
}

func TestWorkflowFormLifecyclePayloadWithFallbackPreservesSubmittedIdentity(t *testing.T) {
	payload := workflowFormLifecyclePayloadWithFallback("wf-new", workflow.PhaseCodingRequirements, "desktop-user:C:/new", map[string]interface{}{
		workflowFormWorkflowIDField: "wf-old",
		workflowFormPhaseField:      workflow.PhaseCodingRequirements,
		workflowFormUserIDField:     "desktop-user:C:/old",
	})
	if payload["workflow_id"] != "wf-old" || payload["workflow_user_id"] != "desktop-user:C:/old" {
		t.Fatalf("submitted workflow identity must win over server fallback, got %#v", payload)
	}
}

func TestWorkflowFormDismissDoesNotClearWhenSkipPersistenceFails(t *testing.T) {
	userID := "desktop-user:C:/Users/ma139"
	registry := workflow.NewWorkflowRegistry()
	understanding := workflow.NewIntentUnderstandingManager(workflow.NullStore{}, &mockLLMCallerGUI{}, registry)
	store := &failingWorkflowStateStoreGUI{}
	engine := workflow.NewWorkflowEngine(registry, understanding, store, &mockEngineCallbacksGUI{})
	state, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	store.fail = true

	handler := NewIMMessageHandlerStandalone(StandaloneConfig{WorkflowEngine: engine})
	defer handler.memory.Stop()
	app := &App{workflowEngine: engine, remoteSessions: NewRemoteSessionManager(nil)}
	client := NewRemoteHubClient(app, app.remoteSessions)
	client.imHandler = handler
	app.remoteSessions.SetHubClient(client)
	handler.app = app
	beforeSeq := app.agentViewSeq()

	resp, err := app.DismissAgentView(AgentViewDismissPayload{ViewID: "workflow:form:" + workflow.PhaseCodingRequirements, Data: map[string]interface{}{
		workflowFormUserIDField:     userID,
		workflowFormWorkflowIDField: state.ID,
		workflowFormPhaseField:      workflow.PhaseCodingRequirements,
	}})
	if err == nil {
		t.Fatalf("dismiss should surface skip persistence failure, resp=%#v", resp)
	}
	if got := app.agentViewSeq(); got != beforeSeq {
		t.Fatalf("failed dismiss must not emit a clear lifecycle event, seq=%d want %d", got, beforeSeq)
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || ws.PhaseFormSkipped || ws.PhaseFormSubmitted || len(ws.PhaseFormData) != 0 {
		t.Fatalf("failed dismiss must not mutate workflow form gate, got %#v", ws)
	}
}

func TestSubmitWorkflowInputIfWaitingIMUsesTextFormGate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-submit-input-form-gate-im"
	workflowType := workflow.WorkflowType("gui_input_form_gate_im_test")
	engine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:          workflowType,
		Name:          "gui input form gate im test",
		Description:   "test template",
		RequiresInput: &workflow.InputRequirement{Description: "source document", AcceptText: true},
		Phases: []workflow.PhaseTemplate{{
			ID:          "collect_context",
			Name:        "Collect Context",
			Prompt:      "collect context",
			Deliverable: "context document",
			InputSchema: &workflow.PhaseInputSchema{Title: "Context", Fields: []workflow.PhaseInputField{{Name: "goal", Label: "Goal", Type: "text", Required: true}}},
			ToolPolicy:  workflow.ToolFilterDocOnly,
		}},
	})
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflowType, Summary: "review source"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp, handled := handler.submitWorkflowInputIfWaiting(engine, userID, "source text", nil, "weixin")
	if !handled || resp == nil {
		t.Fatalf("IM input should return text form guidance, handled=%v resp=%#v", handled, resp)
	}
	if strings.Contains(resp.Text, "right-side panel") {
		t.Fatalf("IM input form guidance must not mention desktop side panel: %q", resp.Text)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("IM form-gated input must not set workflow agent loop marker")
	}
}

func TestResolveWorkflowFormUserIDUsesHiddenUserAfterLoopClearsLastUserID(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "desktop-user:C:/Users/ma139"
	state, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	handler.lastUserID = ""

	got := resolveWorkflowFormUserID(handler, engine, state.CurrentPhase, map[string]interface{}{
		workflowFormUserIDField: userID,
	})
	if got != userID {
		t.Fatalf("expected hidden userID %q, got %q", userID, got)
	}
}

func TestResolveWorkflowFormUserIDFallsBackToSingleActivePhase(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "desktop-user:C:/Users/ma139"
	state, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	handler.lastUserID = ""

	got := resolveWorkflowFormUserID(handler, engine, state.CurrentPhase, nil)
	if got != userID {
		t.Fatalf("expected phase fallback userID %q, got %q", userID, got)
	}
}

func TestWorkflowFormMatchesActiveWorkflowRejectsStaleDismiss(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "desktop-user:C:/Users/ma139"
	state, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	if workflowFormMatchesActiveWorkflow(engine, userID, state.CurrentPhase, map[string]interface{}{
		workflowFormWorkflowIDField: "stale-workflow-id",
	}) {
		t.Fatal("stale workflow form dismiss should not match active workflow")
	}
	if workflowFormMatchesActiveWorkflow(engine, userID, state.CurrentPhase, map[string]interface{}{
		workflowFormPhaseField:      "stale_phase",
		workflowFormWorkflowIDField: state.ID,
	}) {
		t.Fatal("stale workflow phase dismiss should not match active workflow")
	}
	if !workflowFormMatchesActiveWorkflow(engine, userID, state.CurrentPhase, map[string]interface{}{
		workflowFormPhaseField:      state.CurrentPhase,
		workflowFormWorkflowIDField: state.ID,
	}) {
		t.Fatal("current workflow form dismiss should match active workflow")
	}
}

func TestWorkflowFormLifecyclePayloadFallsBackToServerContext(t *testing.T) {
	userID := "desktop-user:C:/work"
	payload := workflowFormLifecyclePayloadFor("wf-current", workflow.PhaseCodingRequirements, userID, map[string]interface{}{})
	if payload["workflow_id"] != "wf-current" {
		t.Fatalf("workflow_id = %#v", payload["workflow_id"])
	}
	if payload["workflow_phase"] != workflow.PhaseCodingRequirements {
		t.Fatalf("workflow_phase = %#v", payload["workflow_phase"])
	}
	if payload["workflow_user_id"] != userID {
		t.Fatalf("workflow_user_id = %#v", payload["workflow_user_id"])
	}
}

func TestWorkflowFormLifecyclePayloadPreservesSubmittedContext(t *testing.T) {
	userID := "desktop-user:C:/work"
	payload := workflowFormLifecyclePayloadFor("", "", "", map[string]interface{}{
		workflowFormWorkflowIDField: "wf-submitted",
		workflowFormPhaseField:      workflow.PhaseCodingRequirements,
		workflowFormUserIDField:     userID,
	})
	if payload["workflow_id"] != "wf-submitted" {
		t.Fatalf("workflow_id = %#v", payload["workflow_id"])
	}
	if payload["workflow_phase"] != workflow.PhaseCodingRequirements {
		t.Fatalf("workflow_phase = %#v", payload["workflow_phase"])
	}
	if payload["workflow_user_id"] != userID {
		t.Fatalf("workflow_user_id = %#v", payload["workflow_user_id"])
	}
}

func TestBuildFormSubmissionSummaryLocalizesChinese(t *testing.T) {
	previousLang, _ := agentViewCurrentLang.Load().(string)
	t.Cleanup(func() { setAgentViewLang(previousLang) })

	setAgentViewLang("zh-Hans")
	got := buildFormSubmissionSummary(map[string]interface{}{"goal": "build app"})
	if !strings.Contains(got, "\u7528\u6237\u5df2\u63d0\u4ea4\u5de5\u4f5c\u6d41\u8868\u5355\u6570\u636e\uff1a") || !strings.Contains(got, "goal: build app") {
		t.Fatalf("expected Chinese workflow form summary, got %q", got)
	}

	setAgentViewLang("en-US")
	got = buildFormSubmissionSummary(map[string]interface{}{"goal": "build app"})
	if !strings.HasPrefix(got, "The user submitted workflow form data: ") {
		t.Fatalf("expected English workflow form summary, got %q", got)
	}
}

func TestWorkflowFormSchemaLocalizesBusinessPlanForChineseUI(t *testing.T) {
	schema := &workflow.PhaseInputSchema{
		Title:     "Business plan brief",
		TitleI18N: map[string]string{"zh": "商业计划简报"},
		Fields: []workflow.PhaseInputField{
			{Name: "project_name", Label: "Project or company name", LabelI18N: map[string]string{"zh": "项目或公司名称"}, Type: "text", Required: true, Placeholder: "Example: AI customer support SaaS platform"},
			{Name: "target_audience", Label: "Target reader", Type: "select", Required: true, Options: []workflow.PhaseInputOption{{Label: "Investors (angel/VC/PE)", Value: "investor", LabelI18N: map[string]string{"zh": "投资人（天使/VC/PE）"}}}},
			{Name: "core_description", Label: "Project summary", Type: "textarea", Required: true, Placeholder: "In 2-3 sentences, explain what it does, what problem it solves, and who it serves.", PlaceholderI18N: map[string]string{"zh": "用 2-3 句话说明它做什么、解决什么问题、服务谁。"}},
		},
	}
	localized := localizeWorkflowPhaseInputSchema(schema, "zh-Hans")
	if localized.Title != "商业计划简报" {
		t.Fatalf("expected localized title, got %q", localized.Title)
	}
	fields := map[string]workflow.PhaseInputField{}
	for _, field := range localized.Fields {
		fields[field.Name] = field
	}
	if got := fields["project_name"].Label; got != "项目或公司名称" {
		t.Fatalf("project_name label = %q", got)
	}
	if got := fields["core_description"].Placeholder; got != "用 2-3 句话说明它做什么、解决什么问题、服务谁。" {
		t.Fatalf("core_description placeholder = %q", got)
	}
	if got := fields["target_audience"].Options[0].Label; got != "投资人（天使/VC/PE）" {
		t.Fatalf("target_audience option = %q", got)
	}
	if schema.Title != "Business plan brief" {
		t.Fatalf("localization mutated original schema title: %q", schema.Title)
	}
}

func TestWorkflowFormSchemaLocalizesWithExplicitMetadataBeforeFallback(t *testing.T) {
	schema := &workflow.PhaseInputSchema{
		Title:     "Unknown English title",
		TitleI18N: map[string]string{"zh": "显式标题"},
		Fields: []workflow.PhaseInputField{{
			Name:            "custom",
			Label:           "Unknown English label",
			LabelI18N:       map[string]string{"zh": "显式字段"},
			Type:            "text",
			Description:     "Unknown English description",
			DescriptionI18N: map[string]string{"zh": "显式说明"},
			Placeholder:     "Unknown English placeholder",
			PlaceholderI18N: map[string]string{"zh": "显式占位"},
			Options:         []workflow.PhaseInputOption{{Label: "Unknown English option", Value: "x", LabelI18N: map[string]string{"zh": "显式选项"}}},
		}},
	}

	localized := localizeWorkflowPhaseInputSchema(schema, "zh-CN")
	if localized.Title != "显式标题" || localized.Fields[0].Label != "显式字段" || localized.Fields[0].Description != "显式说明" || localized.Fields[0].Placeholder != "显式占位" || localized.Fields[0].Options[0].Label != "显式选项" {
		t.Fatalf("explicit i18n metadata not applied: %#v", localized)
	}
	if schema.Fields[0].Label != "Unknown English label" || schema.Fields[0].Options[0].Label != "Unknown English option" {
		t.Fatalf("localization mutated original schema: %#v", schema.Fields[0])
	}
}

func TestWorkflowFormSchemaAppliesExplicitEnglishMetadata(t *testing.T) {
	schema := &workflow.PhaseInputSchema{
		Title:     "Default title",
		TitleI18N: map[string]string{"en": "English title", "en-US": "US title"},
		Fields: []workflow.PhaseInputField{{
			Name:            "custom",
			Label:           "Default label",
			LabelI18N:       map[string]string{"en": "English label"},
			Type:            "select",
			Description:     "Default description",
			DescriptionI18N: map[string]string{"en": "English description"},
			Placeholder:     "Default placeholder",
			PlaceholderI18N: map[string]string{"en": "English placeholder"},
			Options:         []workflow.PhaseInputOption{{Label: "Default option", Value: "x", LabelI18N: map[string]string{"en": "English option"}}},
		}},
	}

	localized := localizeWorkflowPhaseInputSchema(schema, "en-US")
	if localized.Title != "US title" || localized.Fields[0].Label != "English label" || localized.Fields[0].Description != "English description" || localized.Fields[0].Placeholder != "English placeholder" || localized.Fields[0].Options[0].Label != "English option" {
		t.Fatalf("explicit English i18n metadata not applied: %#v", localized)
	}
}

func TestWorkflowFormSchemaEmptyLangDefaultsChineseFallback(t *testing.T) {
	schema := &workflow.PhaseInputSchema{Title: "Business plan brief"}
	localized := localizeWorkflowPhaseInputSchema(schema, "")
	if localized.Title != "商业计划简报" {
		t.Fatalf("empty lang should use Chinese fallback, got %q", localized.Title)
	}
}

func TestWorkflowInterceptionKeepsPendingWorkflowAgentLoopAgainstUICBypass(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-form-submit-loop"
	if _, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	handler.unifiedClassifier = testIntentClassifier(string(intent.LabelNonCoding))
	handler.workflowAgentLoopMarker.Store(userID, true)
	handler.stashedPhasePrompt.Store(userID, "phase prompt")

	resp := handler.handleWorkflowInterception(userID, "The user submitted workflow form data: goal: build app", "desktop")
	if resp != nil {
		t.Fatalf("pending workflow agent loop should not return immediate response: %#v", resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("pending workflow agent loop marker was cleared by UIC bypass")
	}
	if _, ok := handler.stashedPhasePrompt.Load(userID); !ok {
		t.Fatal("stashed phase prompt was cleared by UIC bypass")
	}
}

func TestWorkflowFormGateShortNonWorkflowBypassesFormLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-form-short-nonworkflow"
	handler.workflowAgentLoopMarker.Store(userID, true)
	if _, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp := handler.handleWorkflowInterception(userID, "服务器信息", "desktop")
	if resp != nil {
		t.Fatalf("short non-workflow input should bypass workflow form, got %#v", resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("form-gate chat bypass should clear stale workflow loop marker")
	}
	if ws := engine.GetActiveWorkflow(userID); ws == nil || ws.PhaseFormSubmitted || ws.PhaseFormSkipped {
		t.Fatalf("bypass should leave workflow form gate untouched, got %#v", ws)
	}
}

func TestWorkflowFormGateDirectRunSkipsFormAndStartsLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-form-direct-run"
	if _, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp := handler.handleWorkflowInterception(userID, "直接写", "desktop")
	if resp != nil {
		t.Fatalf("direct-run form skip should start workflow loop without immediate response, got %#v", resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("direct-run form skip should mark workflow agent loop")
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || !ws.PhaseFormSkipped {
		t.Fatalf("direct-run form skip should persist skipped form gate, got %#v", ws)
	}
}

func TestWorkflowFormGateCancelCancelsWorkflow(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-form-cancel"
	if _, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	resp := handler.handleWorkflowInterception(userID, "取消", "desktop")
	if resp == nil || resp.Text == "" || resp.RunID != "" {
		t.Fatalf("cancel should return workflow cancellation text, got %#v", resp)
	}
	if ws := engine.GetActiveWorkflow(userID); ws != nil {
		t.Fatalf("cancel should clear active workflow, got %#v", ws)
	}
}

func TestCaptureWorkflowDocAfterAgentLoopAutoAdvancesNonConfirmPhase(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-auto-advance-capture"
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a desktop app",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := engine.AdvancePhase(userID); err != nil {
			t.Fatalf("AdvancePhase %d failed: %v", i, err)
		}
	}
	engineState := engine.GetActiveWorkflow(userID)
	if engineState == nil || engineState.CurrentPhase != workflow.PhaseCodingImplementation {
		t.Fatalf("expected implementation phase before capture, got %#v", engineState)
	}

	handler.captureWorkflowDocAfterAgentLoop(IMUserMessage{UserID: userID}, nil, &IMAgentResponse{Text: reviewStateValidContentGUI()}, true)

	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != workflow.PhaseCodingReview {
		t.Fatalf("non-confirm phase should auto-advance to review, got %#v", ws)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("next phase agent loop marker was not set")
	}
	if prompt, ok := handler.stashedPhasePrompt.Load(userID); !ok || strings.TrimSpace(prompt.(string)) == "" {
		t.Fatalf("next phase prompt was not stashed, got %#v", prompt)
	}
}

func TestCaptureWorkflowDocAfterAgentLoopUsesRuntimePolicyOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	desktopID := "desktop-post-loop-capture-owner"
	remoteOwnerID := "remote:mobile-post-loop-capture-owner"
	if _, err := engine.StartWorkflow(desktopID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "desktop build"}); err != nil {
		t.Fatalf("StartWorkflow desktop failed: %v", err)
	}
	if _, err := engine.StartWorkflow(remoteOwnerID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "remote build"}); err != nil {
		t.Fatalf("StartWorkflow remote failed: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := engine.AdvancePhase(remoteOwnerID); err != nil {
			t.Fatalf("AdvancePhase remote %d failed: %v", i, err)
		}
	}
	ctx := &LoopContext{ID: "post-loop-capture", RunID: "trace-post-loop-capture", Runtime: RuntimeContext{RequestID: "req-post-loop-capture", PolicyOwnerID: remoteOwnerID}}

	handler.captureWorkflowDocAfterAgentLoop(IMUserMessage{UserID: desktopID}, ctx, &IMAgentResponse{Text: reviewStateValidContentGUI()}, true)

	remoteWS := engine.GetActiveWorkflow(remoteOwnerID)
	if remoteWS == nil || remoteWS.CurrentPhase != workflow.PhaseCodingReview {
		t.Fatalf("remote owner workflow should capture and advance, got %#v", remoteWS)
	}
	desktopWS := engine.GetActiveWorkflow(desktopID)
	if desktopWS == nil || desktopWS.PhaseOutputs[workflow.PhaseCodingRequirements] != "" {
		t.Fatalf("desktop workflow must not receive runtime-owned capture, got %#v", desktopWS)
	}
	if _, ok := handler.workflowReviewExperienceContext.Load(remoteOwnerID); !ok {
		t.Fatal("review experience context should be stored under runtime owner")
	}
	if _, ok := handler.workflowReviewExperienceContext.Load(desktopID); ok {
		t.Fatal("review experience context must not be stored under message user")
	}
}

func TestWorkflowReviewConfirmInvalidCodingTaskBreakdownRegenerates(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-invalid-task-breakdown-confirm"
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a desktop game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := engine.AdvancePhase(userID); err != nil {
			t.Fatalf("AdvancePhase %d failed: %v", i, err)
		}
	}
	invalidBreakdown := "# Temple game implementation complete\n\n" +
		"## Project structure\n\nThe project contains CMakeLists.txt, src/main.cpp, src/Game.cpp, and assets.\n\n" +
		"## Features\n\n- Movement is implemented.\n- Collision is implemented.\n- Audio is implemented.\n\n" +
		"## Build\n\nRun cmake and build the executable. This is intentionally a completion report, not a T-numbered task list."
	if phaseID, err := engine.SavePhaseOutput(userID, invalidBreakdown); err != nil || phaseID != workflow.PhaseCodingTaskBreakdown {
		t.Fatalf("SavePhaseOutput phase=%q err=%v", phaseID, err)
	}

	resp := handler.applyWorkflowReviewIntent(engine, userID, workflow.ReviewIntentConfirm, "继续", "desktop")
	if resp != nil {
		t.Fatalf("invalid task breakdown confirm should trigger regeneration loop, got immediate response %#v", resp)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != workflow.PhaseCodingTaskBreakdown || ws.PendingReviewPhaseID != workflow.PhaseCodingTaskBreakdown || !ws.PendingReviewRevisionRequested {
		t.Fatalf("invalid task breakdown should stay pending regeneration, got %#v", ws)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("invalid task breakdown confirm should schedule regeneration agent loop")
	}
	if handler.taskOrchestratorRegistry != nil {
		if orch := handler.taskOrchestratorRegistry.Get(userID); orch != nil && orch.IsActive() {
			t.Fatal("invalid task breakdown must not activate task orchestrator")
		}
	}
}

func TestWorkflowReviewFastConfirmBypassesPendingUserReply(t *testing.T) {
	llm := &mockLLMCallerGUI{Response: "other"}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	userID := "test-review-pending-reply-priority"
	workflowType := workflow.WorkflowType("gui_review_pending_reply_priority")
	engine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:        workflowType,
		Name:        "review pending reply priority",
		Description: "test template",
		Phases: []workflow.PhaseTemplate{
			{ID: "plan", Name: "Plan", Prompt: "make plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: workflow.ToolFilterDocOnly},
			{ID: "execute", Name: "Execute", Prompt: "execute", Deliverable: "execution", ToolPolicy: workflow.ToolFilterFull},
		},
	})
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflowType,
		Summary:  "build a desktop game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, reviewStateValidContentGUI()); err != nil || phaseID != "plan" {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance phase=%q err=%v", phaseID, err)
	}
	if !engine.IsAwaitingReview(userID) {
		t.Fatal("workflow should be awaiting review before user confirmation")
	}
	handler.pendingUserReply.Store(userID, &pendingUserReplyState{Question: "Please create the project directory first?", Timestamp: time.Now()})

	trimmed := "开工"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})

	if result.HasPendingUserReply || result.PendingUserReplyContext != "" {
		t.Fatalf("pending prose reply must not capture workflow review confirmation: %#v", result)
	}
	if !result.WorkflowAgentLoop {
		t.Fatalf("confirmed review should schedule next workflow phase loop: %#v", result)
	}
	if llm.Calls != 0 {
		t.Fatalf("short confirmation should use deterministic fast path, LLM calls=%d", llm.Calls)
	}
	if _, ok := handler.pendingUserReply.Load(userID); ok {
		t.Fatal("stale pending user reply should be cleared when workflow review owns the turn")
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != "execute" || engine.IsAwaitingReview(userID) {
		t.Fatalf("workflow should advance to design after fast confirm, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}
}

func TestWorkflowReviewExecutionRequestDoesNotStartAgentLoop(t *testing.T) {
	llm := &mockLLMCallerGUI{Response: "confirm"}
	handler, _ := setupWorkflowTestHandler(llm)
	engine := handler.app.workflowEngine
	userID := "test-review-execution-request-blocked"
	workflowType := workflow.WorkflowType("gui_review_execution_request_blocked")
	engine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:        workflowType,
		Name:        "review execution request blocked",
		Description: "test template",
		Phases: []workflow.PhaseTemplate{
			{ID: "plan", Name: "Plan", Prompt: "make plan", Deliverable: "plan", NeedsConfirm: true, ToolPolicy: workflow.ToolFilterDocOnly},
			{ID: "execute", Name: "Execute", Prompt: "execute", Deliverable: "execution", ToolPolicy: workflow.ToolFilterFull},
		},
	})
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflowType, Summary: "build a desktop game"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, reviewStateValidContentGUI()); err != nil || phaseID != "plan" {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance phase=%q err=%v", phaseID, err)
	}

	trimmed := "你直接创建一个"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})

	if !result.Handled || result.Response == nil || strings.TrimSpace(result.Response.Text) == "" {
		t.Fatalf("execution request during review should return workflow barrier response, got %#v", result)
	}
	if !strings.Contains(result.Response.Text, "不能创建目录") || !strings.Contains(result.Response.Text, "确认") {
		t.Fatalf("execution request should explain workflow review boundary, got %q", result.Response.Text)
	}
	if result.WorkflowAgentLoop {
		t.Fatalf("execution request during review must not start agent loop: %#v", result)
	}
	if llm.Calls != 0 {
		t.Fatalf("execution request should be classified deterministically, LLM calls=%d", llm.Calls)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("execution request during review must not stash a workflow agent loop marker")
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != "plan" || !engine.IsAwaitingReview(userID) {
		t.Fatalf("workflow should remain awaiting plan review, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}
}

func TestDetectWorkflowReviewIntentFast(t *testing.T) {
	for _, text := range []string{"\u786e\u8ba4", "\u7ee7\u7eed", "\u5f00\u5de5", "\u597d\u7684", "\u5408\u7406\uff0c\u7ee7\u7eed", "\u5f00\u59cb\u7f16\u7801", "\u5f00\u59cb\u7f16\u7801\u5427", "\u786e\u8ba4\u5f00\u59cb\u5b9e\u73b0"} {
		got, ok := detectWorkflowReviewIntentFast(text)
		if !ok || got != workflow.ReviewIntentConfirm {
			t.Fatalf("detectWorkflowReviewIntentFast(%q)=(%q,%v), want confirm,true", text, got, ok)
		}
	}
	for _, text := range []string{"开工", "继续", "继续推进", "OK", "go ahead", "start"} {
		got, ok := detectWorkflowReviewIntentFast(text)
		if !ok || got != workflow.ReviewIntentConfirm {
			t.Fatalf("detectWorkflowReviewIntentFast(%q)=(%q,%v), want confirm,true", text, got, ok)
		}
	}
	if got, ok := detectWorkflowReviewIntentFast("你直接创建一个"); ok || got != workflow.ReviewIntentOther {
		t.Fatalf("specific file operation should not be fast-confirmed, got (%q,%v)", got, ok)
	}
	if !detectWorkflowReviewBlockedExecutionIntent("你直接创建一个") || !detectWorkflowReviewBlockedExecutionIntent("创建目录 d:\\workprj\\snake5") {
		t.Fatal("execution-like review replies should be blocked before agent tools")
	}
	if detectWorkflowReviewBlockedExecutionIntent("继续推进") || detectWorkflowReviewBlockedExecutionIntent("修改一下设计文档") {
		t.Fatal("confirmations and document edits must not be treated as execution requests")
	}
}

func TestCodingWorkflowTaskBreakdownConfirmStartsImplementationWithLocalTools(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-coding-task-breakdown-confirm-starts-implementation"
	_, err := engine.StartWorkflowWithOptions(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a desktop game",
		Goals:    []string{"build a desktop game"},
	}, workflow.WorkflowStartOptions{ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("requirements")); err != nil || phaseID != workflow.PhaseCodingRequirements {
		t.Fatalf("SavePhaseOutput requirements phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, workflow.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("requirements confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("tech_design")); err != nil || phaseID != workflow.PhaseCodingTechDesign {
		t.Fatalf("SavePhaseOutput tech_design phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, workflow.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("tech design confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, executableCodingBreakdown); err != nil || phaseID != workflow.PhaseCodingTaskBreakdown {
		t.Fatalf("SavePhaseOutput task_breakdown phase=%q err=%v", phaseID, err)
	}
	if !engine.IsAwaitingReview(userID) {
		t.Fatal("task breakdown should await review before implementation")
	}

	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("bash", "bash", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("list_directory", "list directory", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
		toolDef("task", "task", nil, nil),
	})

	trimmed := "\u5f00\u59cb\u7f16\u7801"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})
	if result.Response != nil || !result.WorkflowAgentLoop {
		t.Fatalf("task breakdown confirmation should start implementation agent loop, got %#v", result)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != workflow.PhaseCodingImplementation || engine.IsAwaitingReview(userID) {
		t.Fatalf("workflow should advance to implementation after task breakdown confirm, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}

	toolSet := handler.prepareAgentLoopTools(userID, trimmed, &LoopContext{SkipNeedsConfirmGate: true, WorkflowAgentLoop: true}, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(toolSet.Tools)
	for _, name := range []string{"bash", "read_file", "list_directory", "write_file", "edit_file"} {
		if !names[name] {
			t.Fatalf("implementation workflow loop must expose local coding tool %s, got %#v", name, names)
		}
	}
	if toolSet.WorkflowDecision != workflowToolFilterDecision(workflow.ToolFilterFull) {
		t.Fatalf("workflow decision = %q, want %q", toolSet.WorkflowDecision, workflow.ToolFilterFull)
	}
	for _, name := range []string{"bash", "write_file", "edit_file"} {
		if !handler.isWorkflowToolAllowedForOwner(userID, name) {
			t.Fatalf("implementation workflow execution gate must allow %s", name)
		}
	}
	outPath := filepath.Join(t.TempDir(), "impl", "created.txt")
	handler.registry = NewToolRegistry()
	registerBuiltinTools(handler.registry, handler)
	writeResult := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: userID,
		Context: &LoopContext{WorkflowAgentLoop: true, Runtime: RuntimeContext{
			RequestID:     "req-implementation-write",
			PolicyOwnerID: userID,
		}},
		ToolCall: llm.ToolCall{ID: "call_write", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: fmt.Sprintf(`{"path":%q,"content":"ok"}`, outPath),
		}},
	})
	if writeResult.FailureKind == toolFailurePolicyRejected || strings.Contains(writeResult.Text, "workflow tool policy") {
		t.Fatalf("implementation workflow write_file execution must not be workflow-policy rejected, got %+v", writeResult)
	}
	if data, err := os.ReadFile(outPath); err != nil || string(data) != "ok" {
		t.Fatalf("implementation write_file did not create expected file: data=%q err=%v result=%+v", string(data), err, writeResult)
	}
}

func TestCodingWorkflowTaskBreakdownContinuePushStartsImplementation(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-coding-task-breakdown-continue-push-starts-implementation"
	_, err := engine.StartWorkflowWithOptions(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a desktop game",
		Goals:    []string{"build a desktop game"},
	}, workflow.WorkflowStartOptions{ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("requirements")); err != nil || phaseID != workflow.PhaseCodingRequirements {
		t.Fatalf("SavePhaseOutput requirements phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, workflow.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("requirements confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, substantialWorkflowDoc("tech_design")); err != nil || phaseID != workflow.PhaseCodingTechDesign {
		t.Fatalf("SavePhaseOutput tech_design phase=%q err=%v", phaseID, err)
	}
	if resp := handler.applyWorkflowReviewIntent(engine, userID, workflow.ReviewIntentConfirm, "\u786e\u8ba4", "desktop"); resp != nil {
		t.Fatalf("tech design confirm should schedule next phase loop without immediate response, got %#v", resp)
	}
	handler.workflowAgentLoopMarker.Delete(userID)
	if phaseID, err := engine.SavePhaseOutput(userID, executableCodingBreakdown); err != nil || phaseID != workflow.PhaseCodingTaskBreakdown {
		t.Fatalf("SavePhaseOutput task_breakdown phase=%q err=%v", phaseID, err)
	}

	trimmed := "\u7ee7\u7eed\u63a8\u8fdb"
	result := handler.resolveIMEntryContext(imEntryContextOptions{
		Message: &IMUserMessage{UserID: userID, Text: trimmed, Platform: "desktop"},
		Trimmed: &trimmed,
	})
	if result.Response != nil || !result.WorkflowAgentLoop {
		t.Fatalf("continue-push should confirm task breakdown and start implementation loop, got %#v", result)
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != workflow.PhaseCodingImplementation || engine.IsAwaitingReview(userID) {
		t.Fatalf("workflow should advance to implementation after continue-push, got %#v awaiting=%v", ws, engine.IsAwaitingReview(userID))
	}
}

func TestCaptureWorkflowDocRejectsInvalidCodingTaskBreakdown(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-invalid-task-breakdown-capture"
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a desktop game",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := engine.AdvancePhase(userID); err != nil {
			t.Fatalf("AdvancePhase %d failed: %v", i, err)
		}
	}
	invalidBreakdown := "# Implementation complete\n\n" +
		"## Files\n\nCMakeLists.txt and src/main.cpp were created.\n\n" +
		"## Features\n\n- Gameplay loop done.\n- Renderer done.\n- Build instructions included.\n\n" +
		"This is long enough to look substantial but it is not a T-numbered executable task list."

	handler.captureWorkflowDocAfterAgentLoop(IMUserMessage{UserID: userID}, nil, &IMAgentResponse{Text: invalidBreakdown}, true)

	ws := engine.GetActiveWorkflow(userID)
	if ws == nil {
		t.Fatal("workflow should remain active")
	}
	if ws.PhaseOutputs[workflow.PhaseCodingTaskBreakdown] != "" || ws.PendingReviewPhaseID != "" {
		t.Fatalf("invalid task breakdown should not be saved or enter review, got output=%q pending=%q", ws.PhaseOutputs[workflow.PhaseCodingTaskBreakdown], ws.PendingReviewPhaseID)
	}
}

func reviewStateValidContentGUI() string {
	return "# Phase Output\n\n- Functional item A\n- Functional item B\n- Functional item C\n\nThis document is long enough to pass the minimum quality gate and exercise GUI capture auto-advance behavior."
}

func TestWorkflowReviewAdvanceIMUsesTextFormGate(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-review-advance-im-form"
	workflowType := workflow.WorkflowType("gui_review_form_gate_im_test")
	engine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:        workflowType,
		Name:        "gui review form gate im test",
		Description: "test template",
		Phases: []workflow.PhaseTemplate{
			{ID: "reviewed", Name: "Reviewed", Prompt: "make reviewed output", Deliverable: "reviewed output", NeedsConfirm: true, ToolPolicy: workflow.ToolFilterDocOnly},
			{
				ID:          "collect_more",
				Name:        "Collect More",
				Prompt:      "collect more",
				Deliverable: "more context",
				InputSchema: &workflow.PhaseInputSchema{Title: "More", Fields: []workflow.PhaseInputField{{Name: "scope", Label: "Scope", Type: "text", Required: true}}},
				ToolPolicy:  workflow.ToolFilterDocOnly,
			},
		},
	})
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflowType, Summary: "test"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if phaseID, err := engine.SavePhaseOutput(userID, reviewStateValidContentGUI()); err != nil || phaseID != "reviewed" {
		t.Fatalf("saved phase=%q err=%v", phaseID, err)
	}

	resp := handler.applyWorkflowReviewIntent(engine, userID, workflow.ReviewIntentConfirm, "确认", "weixin")
	if resp == nil || strings.Contains(resp.Text, "right-side panel") || !strings.Contains(resp.Text, "1.") {
		t.Fatalf("IM review advance should return numbered text form guidance, got %#v", resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); ok {
		t.Fatal("review advance into form phase must not start agent loop before form details")
	}
}

func TestWorkflowConfirmation_IMChannelUsesTextFormGuidance(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	engine := handler.app.workflowEngine
	userID := "test-confirm-start-im"
	item := buildPendingWorkflowConfirmation(userID, "build a project", workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "build a project",
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "", "zh")
	handler.confirmationStore.set(item)

	msg := &IMUserMessage{UserID: userID, Platform: "weixin", Text: buildConfirmationActionCommand("confirm", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)
	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.Handled || result.Response == nil {
		t.Fatalf("confirmed IM workflow should return text guidance, got %#v", result)
	}
	if strings.Contains(result.Response.Text, "right-side panel") {
		t.Fatalf("IM workflow must not ask for a desktop-only side panel, got %q", result.Response.Text)
	}
	if !strings.Contains(result.Response.Text, "1.") {
		t.Fatalf("IM workflow should provide numbered text guidance, got %q", result.Response.Text)
	}
	if result.WorkflowAgentLoop {
		t.Fatalf("form guidance should wait for user-provided fields before agent loop, got %#v", result)
	}
	if !engine.HasActiveWorkflow(userID) {
		t.Fatal("confirmed workflow should create an active workflow")
	}
}

func TestWorkflowConfirmation_IMChannelGuidanceThenTextStartsPhaseLoop(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "test-confirm-start-im-next"
	item := buildPendingWorkflowConfirmation(userID, "build a project", workflow.StructuredIntent{
		Category:   workflow.WorkflowCoding,
		Summary:    "build a project",
		Goals:      []string{"build a project"},
		Confidence: 0.9,
	}, "", "zh")
	handler.confirmationStore.set(item)

	msg := &IMUserMessage{UserID: userID, Platform: "weixin", Text: buildConfirmationActionCommand("confirm", item.ID), UIAction: true}
	trimmed := strings.TrimSpace(msg.Text)
	result := handler.handlePendingExecutionConfirmation(msg, &trimmed)
	if !result.Handled || result.Response == nil || result.WorkflowAgentLoop {
		t.Fatalf("expected IM text guidance before agent loop, got %#v", result)
	}

	resp := handler.handleWorkflowInterception(userID, "1. Build a Go service\n2. Use SQLite\n3. Add tests", "weixin")
	if resp != nil {
		t.Fatalf("IM form reply should fall through to agent loop, got %#v", resp)
	}
	if _, ok := handler.workflowAgentLoopMarker.Load(userID); !ok {
		t.Fatal("IM form reply should set workflow agent loop marker")
	}
	prompt, ok := handler.stashedPhasePrompt.Load(userID)
	if !ok || strings.TrimSpace(prompt.(string)) == "" {
		t.Fatalf("IM form reply should stash phase prompt, got %#v", prompt)
	}
}
