package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

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
