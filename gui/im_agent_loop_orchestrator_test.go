package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestApplyAgentLoopTaskOrchestratorStepUsesNextReadyTask(t *testing.T) {
	registry := NewTaskOrchestratorRegistry()
	orch := registry.GetOrCreate("u1")
	orch.ExternalChecker = &fakeExternalChecker{available: true}
	orch.Activate([]*TaskItem{
		{Index: 0, Title: "Blocked task", DependsOn: []int{1}},
		{Index: 1, Title: "Ready task"},
	}, "", "", "/proj", "claude")
	h := &IMMessageHandler{taskOrchestratorRegistry: registry}
	tools := []map[string]interface{}{
		toolDef("create_session", "create session", nil, nil),
		toolDef("bash", "bash", nil, nil),
	}

	result := h.applyAgentLoopTaskOrchestratorStep("u1", nil, tools, nil, false)

	names := map[string]bool{}
	for _, item := range result.Tools {
		names[tool.ExtractToolName(item)] = true
	}
	if names["create_session"] {
		t.Fatalf("ready coding task should strip create_session tool, got %#v", names)
	}
	if !names["bash"] {
		t.Fatalf("ready coding task should keep direct coding tools, got %#v", names)
	}
	if len(result.Conversation) != 1 {
		t.Fatalf("expected one task injection, got %#v", result.Conversation)
	}
	injection, _ := result.Conversation[0].(map[string]string)
	content := injection["content"]
	if !strings.Contains(content, `"Ready task"`) || strings.Contains(content, `executing task 1/2`) {
		t.Fatalf("injection should target ready task, got %q", content)
	}
}

func TestApplyAgentLoopTaskOrchestratorStepBlockedByWorkflowPhase(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.taskOrchestratorRegistry = NewTaskOrchestratorRegistry()
	userID := "orchestrator-injection-doc-policy-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	orch := h.getTaskOrchestrator(userID)
	orch.Activate([]*TaskItem{{Index: 0, Title: "Ready coding task"}}, "req", "design", "/proj", "")
	tools := []map[string]interface{}{
		toolDef("bash", "bash", nil, nil),
	}

	result := h.applyAgentLoopTaskOrchestratorStep(userID, nil, tools, nil, false)

	if len(result.Conversation) != 0 {
		t.Fatalf("workflow-blocked orchestrator must not inject coding task prompt, got %#v", result.Conversation)
	}
	if !sameToolNames(result.Tools, tools) {
		t.Fatalf("workflow block should leave tool set unchanged here, got %#v", result.Tools)
	}
	if orch.IsActive() {
		t.Fatal("stale orchestrator must be deactivated when workflow phase is not execution")
	}
}

func TestApplyAgentLoopTaskOrchestratorStepUsesRuntimePolicyOwner(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.taskOrchestratorRegistry = NewTaskOrchestratorRegistry()
	desktopID := "desktop-orchestrator-doc-policy-owner"
	remoteOwnerID := "remote:mobile-orchestrator-owner"
	if _, err := h.app.workflowEngine.StartWorkflow(desktopID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build desktop"}); err != nil {
		t.Fatalf("StartWorkflow desktop failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm desktop failed: %v", err)
	}
	h.lastUserID = desktopID
	desktopOrch := h.getTaskOrchestrator(desktopID)
	desktopOrch.Activate([]*TaskItem{{Index: 0, Title: "Desktop coding task"}}, "req", "design", "/desktop", "")
	remoteOrch := h.getTaskOrchestrator(remoteOwnerID)
	remoteOrch.Activate([]*TaskItem{{Index: 0, Title: "Remote coding task"}}, "req", "design", "/remote", "")
	ctx := &LoopContext{Runtime: RuntimeContext{RequestID: "req-orchestrator", PolicyOwnerID: remoteOwnerID}}
	tools := []map[string]interface{}{toolDef("bash", "bash", nil, nil)}

	result := h.applyAgentLoopTaskOrchestratorStep(desktopID, ctx, tools, nil, false)

	if len(result.Conversation) != 1 {
		t.Fatalf("runtime owner orchestrator should inject one prompt, got %#v", result.Conversation)
	}
	injection, _ := result.Conversation[0].(map[string]string)
	content := injection["content"]
	if !strings.Contains(content, "Remote coding task") || strings.Contains(content, "Desktop coding task") {
		t.Fatalf("orchestrator injection should use runtime owner, got %q", content)
	}
	if !desktopOrch.IsActive() {
		t.Fatal("desktop orchestrator must not be deactivated by runtime-owned step")
	}
	if !remoteOrch.IsActive() {
		t.Fatal("remote orchestrator should remain active after injection")
	}
}

func sameToolNames(a, b []map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if tool.ExtractToolName(a[i]) != tool.ExtractToolName(b[i]) {
			return false
		}
	}
	return true
}
