package main

import (
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreintent "github.com/RapidAI/CodeClaw/corelib/intent"
)

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
	msg := IMUserMessage{UserID: "u1", Text: "repair the failing startup path in this project"}

	if !h.shouldRouteBugFixToDirectCodingSubAgent(msg, &LoopContext{Kind: LoopKindChat}) {
		t.Fatal("semantic bug-fix intent should route directly to CodingSubAgent")
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
