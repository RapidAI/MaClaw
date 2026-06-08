package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreintent "github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
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

func TestDirectCodingSubAgentRouteRequiresSemanticBugFixIntent(t *testing.T) {
	original := unifiedClassifierPtr.Load()
	defer unifiedClassifierPtr.Store(original)
	unifiedClassifierPtr.Store(nil)

	h := &IMMessageHandler{}
	msg := IMUserMessage{UserID: "u1", Text: "fix the startup bug"}
	if h.shouldRouteBugFixToDirectCodingSubAgent(msg, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("direct CodingSubAgent route must not infer bug-fix intent without a semantic classifier")
	}
}

func TestDirectCodingSubAgentRouteAcceptsSemanticBugFixIntent(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h := &IMMessageHandler{unifiedClassifier: uic}
	attachCodeProjectToRouteHandler(t, h)
	msg := IMUserMessage{UserID: "u1", Text: "repair the failing startup path in this project"}

	if !h.shouldRouteBugFixToDirectCodingSubAgent(msg, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("semantic bug-fix intent should route directly to CodingSubAgent")
	}
}

func TestDirectCodingSubAgentRouteBlockedByActiveDocOnlyWorkflow(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.unifiedClassifier = uic
	attachCodeProjectToRouteHandler(t, h)
	userID := "direct-route-doc-policy-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	if h.shouldRouteBugFixToDirectCodingSubAgent(IMUserMessage{UserID: userID, Text: "repair the startup bug"}, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("active doc-only workflow phase must block direct CodingSubAgent routing")
	}
}

func TestDirectCodingSubAgentRouteBlockedByActivePlanningWorkflow(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.unifiedClassifier = uic
	attachCodeProjectToRouteHandler(t, h)
	userID := "direct-route-planning-policy-user"
	state, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	state.PhaseIndex = 2
	state.CurrentPhase = workflow.PhaseCodingTaskBreakdown

	if h.shouldRouteBugFixToDirectCodingSubAgent(IMUserMessage{UserID: userID, Text: "repair the startup bug"}, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("active planning workflow phase must block direct CodingSubAgent routing")
	}
}

func TestDirectCodingSubAgentRouteDoesNotInheritSingleActiveWorkflowPolicy(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.unifiedClassifier = uic
	attachCodeProjectToRouteHandler(t, h)
	userID := "direct-route-single-active-policy-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	if !h.shouldRouteBugFixToDirectCodingSubAgent(IMUserMessage{Text: "repair the startup bug"}, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("direct CodingSubAgent routing without explicit owner must not inherit single active doc-only workflow phase")
	}
}

func TestDirectCodingSubAgentRouteBlockedByNonOrchestratorFullWorkflowPhase(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.unifiedClassifier = uic
	workflowType := workflow.WorkflowType("full_non_orchestrator_route")
	h.app.workflowEngine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type: workflowType,
		Name: "full non orchestrator route",
		Phases: []workflow.PhaseTemplate{{
			ID:                  "generate_artifact",
			Name:                "Generate artifact",
			Prompt:              "generate artifact",
			Deliverable:         "artifact",
			ToolPolicy:          workflow.ToolFilterFull,
			DisableOrchestrator: true,
		}},
	})
	userID := "direct-route-full-disabled-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflowType, Summary: "generate"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}

	if h.shouldRouteBugFixToDirectCodingSubAgent(IMUserMessage{UserID: userID, Text: "repair the startup bug"}, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("full non-orchestrator workflow phase must block direct CodingSubAgent routing")
	}
}

func TestDirectCodingSubAgentRouteAllowedInImplementationWorkflowPhase(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.unifiedClassifier = uic
	attachCodeProjectToRouteHandler(t, h)
	userID := "direct-route-implementation-user"
	state, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	tmpl := h.app.workflowEngine.GetRegistry().Match(workflow.WorkflowCoding)
	for i, phase := range tmpl.Phases {
		if phase.ID == workflow.PhaseCodingImplementation {
			state.PhaseIndex = i
			state.CurrentPhase = phase.ID
			break
		}
	}

	if !h.shouldRouteBugFixToDirectCodingSubAgent(IMUserMessage{UserID: userID, Text: "repair the startup bug"}, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("implementation workflow phase should allow direct CodingSubAgent routing")
	}
}

func TestDirectCodingSubAgentRouteRejectsCloudflareLoginOpsIntent(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"non_coding","score":0.95,"reason":"operational login guidance, not code repair"}]}`, nil
	}})
	h := &IMMessageHandler{unifiedClassifier: uic}
	msg := IMUserMessage{UserID: "u1", Text: "Cloudflare OAuth login is unavailable; any other operational options?"}

	if h.shouldRouteBugFixToDirectCodingSubAgent(msg, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("ops/login guidance must not route directly to CodingSubAgent")
	}
}

func TestDirectCodingSubAgentRouteRejectsAskUserContinuation(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h := &IMMessageHandler{unifiedClassifier: uic}
	msg := IMUserMessage{UserID: "u1", Text: "submit failed and the content disappeared"}

	if h.shouldRouteBugFixToDirectCodingSubAgent(msg, &LoopContext{Kind: LoopKindChat, IsAskUserResponse: true}) {
		t.Fatal("ask_user continuations must stay in the current interaction context, not route to CodingSubAgent")
	}
}

func TestDirectCodingSubAgentRouteRejectsBrowserPublicationExecutionContext(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"submit flow failure"}]}`, nil
	}})
	h := &IMMessageHandler{unifiedClassifier: uic}
	msg := IMUserMessage{UserID: "u1", Text: "log into Zhihu and publish a post; submit failed"}

	if h.shouldRouteBugFixToDirectCodingSubAgent(msg, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("browser publication tasks must not route to CodingSubAgent even if classified as bug_fix")
	}
}

func TestDirectCodingSubAgentRouteRejectsBrowserPublicationFollowUp(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"submit flow failure"}]}`, nil
	}})
	h := &IMMessageHandler{memory: agent.NewConversationMemory(), unifiedClassifier: uic}
	h.memory.Save("u1", []agent.ConversationEntry{{Role: "user", Content: "log into Zhihu and publish a post"}})
	msg := IMUserMessage{UserID: "u1", Text: "submit failed and the content disappeared"}

	if h.shouldRouteBugFixToDirectCodingSubAgent(msg, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("follow-ups to browser publication tasks must not route to CodingSubAgent")
	}
}

func TestCodingContextDoesNotMatchAppInsideDisappeared(t *testing.T) {
	if hasExplicitCodingImplementationContext("submit failed and the content disappeared") {
		t.Fatal("disappeared must not count as explicit app/code context")
	}
	if hasExplicitCodingImplementationContext("test zhihu publish flow") {
		t.Fatal("generic test wording must not count as explicit code context")
	}
	if !hasExplicitCodingImplementationContext("fix the app submit bug") {
		t.Fatal("explicit app/code words should still count")
	}
}

func TestCodingSubAgentAdmissionBlocksBrowserPublicationTestFlow(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	h := &IMMessageHandler{}
	reason := h.codingSubAgentAdmissionBlockReason(codingSubAgentAdmissionInput{
		Text:                        "test zhihu publish flow",
		ProjectPath:                 project,
		RequireExistingCodeEvidence: true,
	})

	if !strings.Contains(reason, "browser publication") {
		t.Fatalf("reason = %q, want browser publication block", reason)
	}
}

func TestDirectCodingSubAgentRouteRejectsChineseBrowserPublicationFollowUp(t *testing.T) {
	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"submit flow failure"}]}`, nil
	}})
	h := &IMMessageHandler{memory: agent.NewConversationMemory(), unifiedClassifier: uic}
	h.memory.Save("u1", []agent.ConversationEntry{{Role: "user", Content: "\u767b\u5f55\u77e5\u4e4e\u5e76\u53d1\u5e03\u6587\u7ae0"}})
	msg := IMUserMessage{UserID: "u1", Text: "\u6ca1\u6709\u6210\u529f\uff0c\u6bcf\u6b21\u90fd\u662f\u586b\u5145\u5b8c\u5185\u5bb9\uff0c\u7136\u540e\u63d0\u4ea4\u65f6\u5c31\u6d88\u5931\u4e86"}

	if h.shouldRouteBugFixToDirectCodingSubAgent(msg, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("Chinese browser publication follow-ups must not route to CodingSubAgent")
	}
}

func TestProjectPathHasExistingCodeEvidence(t *testing.T) {
	empty := t.TempDir()
	if projectPathHasExistingCodeEvidence(empty) {
		t.Fatal("empty directory must not count as existing code evidence")
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	if !projectPathHasExistingCodeEvidence(project) {
		t.Fatal("project marker file must count as existing code evidence")
	}
}

func TestCodingSubAgentAdmissionBlocksBrowserPublicationWithoutCodeContext(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	h := &IMMessageHandler{}
	reason := h.codingSubAgentAdmissionBlockReason(codingSubAgentAdmissionInput{
		Text:                        "log into zhihu and publish a post",
		ProjectPath:                 project,
		RequireExistingCodeEvidence: true,
	})

	if !strings.Contains(reason, "browser publication") {
		t.Fatalf("reason = %q, want browser publication block", reason)
	}
}

func TestCodingSubAgentAdmissionAllowsBrowserNamedCodeFix(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	h := &IMMessageHandler{}
	reason := h.codingSubAgentAdmissionBlockReason(codingSubAgentAdmissionInput{
		Text:                        "fix the zhihu publish bug in the app code",
		ProjectPath:                 project,
		RequireExistingCodeEvidence: true,
	})

	if reason != "" {
		t.Fatalf("reason = %q, want code fix admitted", reason)
	}
}

func TestCodingSubAgentAdmissionBlocksAmbiguousBrowserFix(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	h := &IMMessageHandler{}
	reason := h.codingSubAgentAdmissionBlockReason(codingSubAgentAdmissionInput{
		Text:                        "fix zhihu publish submit failed",
		ProjectPath:                 project,
		RequireExistingCodeEvidence: true,
	})

	if !strings.Contains(reason, "browser publication") {
		t.Fatalf("reason = %q, want ambiguous browser fix blocked", reason)
	}
}

func TestCodingSubAgentAdmissionAllowsSubmitFailureInCodeProjectWithoutBrowserHistory(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module example.com/x\n"), 0o644); err != nil {
		t.Fatalf("WriteFile go.mod: %v", err)
	}
	h := &IMMessageHandler{}
	reason := h.codingSubAgentAdmissionBlockReason(codingSubAgentAdmissionInput{
		Text:                        "submit failed and the content disappeared",
		ProjectPath:                 project,
		RequireExistingCodeEvidence: true,
	})

	if reason != "" {
		t.Fatalf("reason = %q, want code project submit failure admitted", reason)
	}
}

func TestDirectCodingSubAgentRouteRejectsKeywordLayerResult(t *testing.T) {
	msg := IMUserMessage{Text: "fix the startup bug"}
	result := GateIntentResult{Intent: GateIntentBugFix, Confidence: 0.95, Layer: 1, Reason: "keyword match"}

	if shouldRouteGateResultToDirectCodingSubAgent(result, msg, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("layer-1 keyword-only result must not route directly to CodingSubAgent")
	}
}

func TestDirectCodingSubAgentRouteRejectsDegradedBugFixIntent(t *testing.T) {
	msg := IMUserMessage{Text: "fix the startup bug"}
	result := GateIntentResult{Intent: GateIntentBugFix, Confidence: 0.95, Layer: 2, Degraded: true, Reason: "embedding-only fallback"}

	if shouldRouteGateResultToDirectCodingSubAgent(result, msg, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("degraded semantic result must not route directly to CodingSubAgent")
	}
}

func TestDirectCodingSubAgentRouteRejectsNonBugFixCodingIntents(t *testing.T) {
	msg := IMUserMessage{Text: "build or maintain this project"}
	for _, intent := range []GateIntent{GateIntentNewProject, GateIntentMaintenance} {
		result := GateIntentResult{Intent: intent, Confidence: 0.95, Layer: 3, Reason: "semantic coding, not direct bug fix"}
		if shouldRouteGateResultToDirectCodingSubAgent(result, msg, &LoopContext{Kind: LoopKindChat}) {
			t.Fatalf("%s must not route directly to CodingSubAgent", intent)
		}
	}
}

func TestRouteDirectCodingSubAgentExecutionRunsCodingSubAgent(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	var called bool
	var tokenCallbackForwarded bool
	var gotTask *TaskItem
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		called = true
		tokenCallbackForwarded = onToken != nil
		gotTask = task
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "bug fixed"}
	}

	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h := &IMMessageHandler{memory: agent.NewConversationMemory(), unifiedClassifier: uic}
	resp, history, handled := h.routeDirectCodingSubAgentExecution(
		IMUserMessage{UserID: "u1", Text: "fix the startup bug"},
		nil,
		&LoopContext{Kind: LoopKindChat},
		nil,
		nil,
		func(string) {},
	)
	if !handled || !called {
		t.Fatal("expected direct route to execute CodingSubAgent")
	}
	if !tokenCallbackForwarded {
		t.Fatal("expected direct route to forward token callback to CodingSubAgent")
	}
	if resp == nil || resp.Text != "bug fixed" {
		t.Fatalf("response = %#v, want CodingSubAgent summary", resp)
	}
	if len(history) != 1 || history[0].Content != "bug fixed" {
		t.Fatalf("history = %#v, want saved CodingSubAgent result", history)
	}
	if gotTask == nil || gotTask.Description != "fix the startup bug" {
		t.Fatalf("task = %#v, want original request", gotTask)
	}
}

func TestRouteDirectCodingSubAgentExecutionUsesProjectScopedOwnerPath(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()

	ownerProject := t.TempDir()
	if err := os.WriteFile(filepath.Join(ownerProject, "go.mod"), []byte("module example.com/owner\n"), 0o644); err != nil {
		t.Fatalf("WriteFile owner go.mod: %v", err)
	}
	currentProject := t.TempDir()
	var gotProjectPath string
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		gotProjectPath = projectPath
		return &CodingSubAgentResult{Status: TaskExecPassed, Summary: "bug fixed"}
	}

	uic := coreintent.New(coreintent.Config{LLMFunc: func(systemPrompt, userText string) (string, error) {
		return `{"top":[{"skill":"bug_fix","score":0.91,"reason":"existing code repair"}]}`, nil
	}})
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.memory = agent.NewConversationMemory()
	h.unifiedClassifier = uic
	if err := h.app.SaveConfig(corelib.AppConfig{
		CurrentProject: "current",
		Projects:       []corelib.ProjectConfig{{Id: "current", Path: currentProject}},
	}); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	ownerID := projectSessionOwnerID(ownerProject)
	ctx := &LoopContext{Kind: LoopKindChat, Runtime: RuntimeContext{RequestID: "req-owner-project", PolicyOwnerID: ownerID}}

	resp, _, handled := h.routeDirectCodingSubAgentExecution(
		IMUserMessage{UserID: desktopUserID, Text: "repair the startup bug in this project"},
		nil,
		ctx,
		nil,
		nil,
		nil,
	)
	if !handled || resp == nil || resp.Text != "bug fixed" {
		t.Fatalf("direct route = handled=%v resp=%#v", handled, resp)
	}
	if gotProjectPath != normalizeProjectSessionPath(ownerProject) {
		t.Fatalf("CodingSubAgent projectPath = %q, want owner project %q", gotProjectPath, normalizeProjectSessionPath(ownerProject))
	}
}

func TestRouteDirectCodingSubAgentExecutionBlockedByActiveDocOnlyWorkflow(t *testing.T) {
	original := runTaskWithSubAgent
	defer func() { runTaskWithSubAgent = original }()
	runTaskWithSubAgent = func(handler *IMMessageHandler, cfg corelib.MaclawLLMConfig, httpClient *http.Client, task *TaskItem, projectPath, reqCtx, designCtx string, prevOutputs []string, loopCtx *LoopContext, onToken func(string), onProgress func(string)) *CodingSubAgentResult {
		t.Fatal("doc-only workflow phase must reject direct CodingSubAgent before it starts")
		return nil
	}

	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.memory = agent.NewConversationMemory()
	userID := "direct-exec-doc-policy-user"
	if _, err := h.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build app"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := h.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	resp, history, handled := h.routeDirectCodingSubAgentExecution(IMUserMessage{UserID: userID, Text: "fix the startup bug"}, nil, &LoopContext{Kind: LoopKindChat}, nil, nil, nil)
	if handled || resp != nil || len(history) != 0 {
		t.Fatalf("direct route = handled=%v resp=%#v history=%#v, want blocked pass-through", handled, resp, history)
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
