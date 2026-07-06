package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreintent "github.com/RapidAI/CodeClaw/corelib/intent"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func attachCodeProjectToRouteHandler(t *testing.T, h *IMMessageHandler) string {
	t.Helper()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/route\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	if h.app == nil {
		h.app = &App{testHomeDir: t.TempDir()}
	}
	if h.app.testHomeDir == "" {
		h.app.testHomeDir = t.TempDir()
	}
	if err := h.app.SaveConfig(corelib.AppConfig{
		CurrentProject: "route-test",
		Projects:       []corelib.ProjectConfig{{Id: "route-test", Path: project}},
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	return project
}

func TestSubAgentRouteKeepsIncompleteRunsActiveForResume(t *testing.T) {
	if shouldDeactivateSubAgentOrchestratorAfterRun(false, false) {
		t.Fatal("incomplete non-cancelled SubAgent run should remain active for retry/resume")
	}
	if shouldSaveSubAgentWorkflowOutput(false, false, true) {
		t.Fatal("incomplete SubAgent run must not be saved as workflow phase output")
	}
}

func TestSubAgentRouteFinalizesCompletedOrCancelledRuns(t *testing.T) {
	if !shouldDeactivateSubAgentOrchestratorAfterRun(true, false) {
		t.Fatal("completed SubAgent run should deactivate orchestrator")
	}
	if !shouldSaveSubAgentWorkflowOutput(true, false, true) {
		t.Fatal("completed non-cancelled SubAgent run should save workflow output")
	}
	if shouldSaveSubAgentWorkflowOutput(true, false, false) {
		t.Fatal("completed SubAgent run with no passed tasks must not save workflow output")
	}
	if !shouldDeactivateSubAgentOrchestratorAfterRun(false, true) {
		t.Fatal("cancelled SubAgent run should deactivate orchestrator")
	}
	if shouldSaveSubAgentWorkflowOutput(true, true, true) {
		t.Fatal("cancelled SubAgent run must not save workflow output")
	}
}

func TestSubAgentRouteRequiresPassedIntegrationBeforeWorkflowSave(t *testing.T) {
	if !subAgentIntegrationPassed(&CodingSubAgentResult{Status: TaskExecPassed}) {
		t.Fatal("passed integration should allow workflow save")
	}
	if subAgentIntegrationPassed(&CodingSubAgentResult{Status: TaskExecFailed, Error: "build failed"}) {
		t.Fatal("failed integration must block workflow save")
	}
	if subAgentIntegrationPassed(nil) {
		t.Fatal("nil integration result must block workflow save")
	}
}

func TestCodingSubAgentLegacyRouteDisabled(t *testing.T) {
	// bug_fix intent no longer bypasses workflow templates into CodingSubAgent.
	// routeSubAgentExecution only routes when orchestrator is active (ShouldUseSubAgent).
	// Without an active orchestrator, the function returns (nil, history, false).
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h := &IMMessageHandler{memory: agent.NewConversationMemory(), unifiedClassifier: uic}
	attachCodeProjectToRouteHandler(t, h)
	msg := IMUserMessage{UserID: "u1", Text: "repair the failing startup path in this project"}

	resp, _, handled := h.routeSubAgentExecution(msg, nil, &LoopContext{Kind: LoopKindChat}, nil, nil, nil)
	if handled || resp != nil {
		t.Fatal("bug_fix intent without active orchestrator must not route to CodingSubAgent")
	}
}

func TestRouteSubAgentExecutionBlockedByActiveDocOnlyWorkflow(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		t.Fatal("doc-only workflow phase must reject orchestrator SubAgent route before it starts")
		return nil
	}

	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.memory = agent.NewConversationMemory()
	h.taskOrchestratorRegistry = NewTaskOrchestratorRegistry()
	userID := "orchestrator-doc-policy-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	orch := h.getTaskOrchestrator(userID)
	orch.Activate([]*TaskItem{{Index: 1, Title: "T1", Description: "edit file"}}, "req", "design", ".", "")

	resp, history, handled := h.routeSubAgentExecution(IMUserMessage{UserID: userID, Text: "continue"}, nil, &LoopContext{Kind: LoopKindChat}, nil, nil, nil)
	if handled || resp != nil || len(history) != 0 {
		t.Fatalf("subagent route = handled=%v resp=%#v history=%#v, want blocked pass-through", handled, resp, history)
	}
	if orch.IsActive() {
		t.Fatal("stale task orchestrator must be deactivated when workflow phase blocks SubAgent")
	}
}

func TestRouteSubAgentExecutionUsesRuntimePolicyOwner(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	var ranTaskTitle string
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		ranTaskTitle = task.Title
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: task.Title + " done"}
	}

	h := &IMMessageHandler{memory: agent.NewConversationMemory(), taskOrchestratorRegistry: NewTaskOrchestratorRegistry()}
	desktopID := "desktop-subagent-doc-policy-owner"
	remoteOwnerID := "remote:mobile-subagent-owner"
	h.lastUserID = desktopID
	desktopOrch := h.getTaskOrchestrator(desktopID)
	desktopOrch.Activate([]*TaskItem{{Index: 0, Title: "Desktop task", Description: "desktop"}}, "req", "design", ".", "")
	remoteOrch := h.getTaskOrchestrator(remoteOwnerID)
	remoteOrch.Activate([]*TaskItem{{Index: 0, Title: "Remote task", Description: "remote"}}, "req", "design", ".", "")
	ctx := &LoopContext{Kind: LoopKindChat, Runtime: RuntimeContext{RequestID: "req-subagent", PolicyOwnerID: remoteOwnerID}}

	resp, history, handled := h.routeSubAgentExecution(IMUserMessage{UserID: desktopID, Text: "continue"}, nil, ctx, nil, nil, nil)
	if !handled || resp == nil || len(history) != 1 {
		t.Fatalf("runtime-owned subagent route = handled=%v resp=%#v history=%#v", handled, resp, history)
	}
	if ranTaskTitle != "Remote task" {
		t.Fatalf("subagent should run runtime owner task, got %q", ranTaskTitle)
	}
	if !desktopOrch.IsActive() {
		t.Fatal("desktop orchestrator must not be deactivated by remote route")
	}
	if remoteOrch.IsActive() {
		t.Fatal("completed remote orchestrator should deactivate after run")
	}
}
