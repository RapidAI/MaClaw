package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func TestPrepareAgentLoopToolsWorkflowAgentLoopStillAppliesWorkflowFilter(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-agent-loop-filter-user"
	_, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("bash", "bash", nil, nil),
		toolDef("task", "task", nil, nil),
	})

	plainSkip := handler.prepareAgentLoopTools(userID, "build a project", &LoopContext{SkipNeedsConfirmGate: true}, agentLoopPhase{})
	plainNames := toolNameSetForWorkflowFilterTest(plainSkip.Tools)
	if !plainNames["task"] {
		t.Fatalf("plain SkipNeedsConfirmGate should bypass workflow filter, got %#v", plainNames)
	}

	workflowLoop := handler.prepareAgentLoopTools(userID, "build a project", &LoopContext{SkipNeedsConfirmGate: true, WorkflowAgentLoop: true}, agentLoopPhase{})
	workflowNames := toolNameSetForWorkflowFilterTest(workflowLoop.Tools)
	if workflowNames["task"] || !workflowNames["read_file"] || !workflowNames["bash"] {
		t.Fatalf("workflow agent loop should apply doc-only filter despite SkipNeedsConfirmGate, got %#v", workflowNames)
	}
	if workflowLoop.WorkflowDecision != workflowToolFilterDecision(workflow.ToolFilterDocOnly) {
		t.Fatalf("workflow decision = %q, want %q", workflowLoop.WorkflowDecision, workflow.ToolFilterDocOnly)
	}
}

func TestEnsureWorkflowRequiredToolsRestoresDocWriteFileBeforePolicyFilter(t *testing.T) {
	allTools := []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
		toolDef("task", "task", nil, nil),
	}
	routed := []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("task", "task", nil, nil),
	}

	merged := ensureWorkflowRequiredTools(workflow.ToolFilterDocOnly, routed, allTools)
	filtered := workflow.FilterToolDefinitions(workflow.ToolFilterDocOnly, merged)
	names := toolNameSetForWorkflowFilterTest(filtered)

	if !names["write_file"] || !names["read_file"] || !names["edit_file"] {
		t.Fatalf("doc-only workflow required tools missing after merge/filter: %#v", names)
	}
	if names["task"] {
		t.Fatalf("workflow policy must still remove disallowed tools after merge, got %#v", names)
	}

	nonWorkflow := ensureWorkflowRequiredTools(workflow.ToolFilterNone, routed, allTools)
	if len(nonWorkflow) != len(routed) {
		t.Fatalf("non-restricted policy should not add tools, got %#v", toolNameSetForWorkflowFilterTest(nonWorkflow))
	}
}

func TestApplyWorkflowToolFilterRestoresDocRequiredTools(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-filter-restores-doc-tools-user"
	_, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
		toolDef("task", "task", nil, nil),
	})

	filtered := handler.applyWorkflowToolFilter(userID, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("task", "task", nil, nil),
	})
	names := toolNameSetForWorkflowFilterTest(filtered)
	if !names["write_file"] || !names["read_file"] || !names["edit_file"] {
		t.Fatalf("workflow filter should restore required doc tools from full catalog, got %#v", names)
	}
	if names["task"] {
		t.Fatalf("workflow filter must still remove disallowed tools, got %#v", names)
	}
}

func TestApplyWorkflowToolFilterNoneDoesNotForceCatalogBuild(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("write_file", "write file", nil, nil),
	})
	tools := []map[string]interface{}{toolDef("read_file", "read file", nil, nil)}

	filtered := handler.applyWorkflowToolFilter("no-active-workflow-user", tools)
	names := toolNameSetForWorkflowFilterTest(filtered)
	if !names["read_file"] || names["write_file"] {
		t.Fatalf("no active workflow should leave routed tools unchanged, got %#v", names)
	}
}

func TestWorkflowAgentLoopStillHonorsNeedsConfirmGateAfterSkip(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-agent-loop-needs-confirm-user"
	_, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	gateConfig := codingToolGateConfig{}
	plainSkip := handler.shouldNeedsConfirmToolBranch(&LoopContext{SkipNeedsConfirmGate: true}, userID, 1, gateConfig)
	if plainSkip {
		t.Fatal("plain SkipNeedsConfirmGate should bypass NeedsConfirm for non-workflow continuation")
	}
	workflowLoop := handler.shouldNeedsConfirmToolBranch(&LoopContext{SkipNeedsConfirmGate: true, WorkflowAgentLoop: true}, userID, 1, gateConfig)
	if !workflowLoop {
		t.Fatal("workflow agent loop must keep NeedsConfirm gate active despite SkipNeedsConfirmGate")
	}
}

func TestPrepareAgentLoopToolsAwaitingReviewUsesActivePhaseFilter(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-review-active-filter-user"
	engine := handler.app.workflowEngine
	_, err := engine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := engine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	if _, _, err := engine.SavePhaseOutputAndMaybeAdvance(userID, substantialWorkflowDoc("requirements")); err != nil {
		t.Fatalf("SavePhaseOutputAndMaybeAdvance failed: %v", err)
	}
	if policy := engine.GetPhaseToolFilter(userID); policy != workflow.ToolFilterNone {
		t.Fatalf("execution filter should be none while awaiting review, got %s", policy)
	}

	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("bash", "bash", nil, nil),
		toolDef("task", "task", nil, nil),
		toolDef("browser", "browser", nil, nil),
	})

	toolSet := handler.prepareAgentLoopTools(userID, "continue", &LoopContext{SkipNeedsConfirmGate: true}, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(toolSet.Tools)
	if names["task"] || names["browser"] || !names["read_file"] || !names["bash"] {
		t.Fatalf("awaiting-review workflow filter must use active phase policy, got %#v", names)
	}
	if toolSet.WorkflowDecision != workflowToolFilterDecision(workflow.ToolFilterDocOnly) {
		t.Fatalf("workflow decision = %q, want %q", toolSet.WorkflowDecision, workflow.ToolFilterDocOnly)
	}

	execResult := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID:   userID,
		ToolCall: toolCallNamed("read_file"),
	})
	if execResult.Outcome != toolOutcomeFailed || execResult.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("blocked review phase should reject even doc-only tool execution, got %#v", execResult)
	}
	progressEmitted := false
	recordedToolName := ""
	browserResult := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID:           userID,
		ToolCall:         toolCallNamed("browser"),
		SendToolProgress: func(string) { progressEmitted = true },
		RecordToolCall:   func(_ string, name string, _ string) { recordedToolName = name },
	})
	if progressEmitted {
		t.Fatal("workflow-blocked tools must not emit user-facing tool progress")
	}
	if recordedToolName != "" {
		t.Fatalf("workflow-blocked browser call must not be recorded into trajectory context, got %q", recordedToolName)
	}
	if strings.Contains(strings.ToLower(browserResult.Text), "browser") {
		t.Fatalf("workflow-blocked browser result must not reinforce role-prefix token, got %q", browserResult.Text)
	}
}

func TestPrepareAgentLoopToolsBlockedPhaseWithNoPolicyExposesNoTools(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-blocked-none-filter-user"
	engine := handler.app.workflowEngine
	workflowType := workflow.WorkflowType("blocked_none_policy")
	engine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:        workflowType,
		Name:        "blocked none policy",
		Description: "test template",
		Phases: []workflow.PhaseTemplate{{
			ID:          "collect",
			Name:        "Collect",
			Prompt:      "collect input",
			Deliverable: "input",
			InputSchema: &workflow.PhaseInputSchema{Fields: []workflow.PhaseInputField{{Name: "goal", Label: "Goal", Type: "text", Required: true}}},
			ToolPolicy:  workflow.ToolFilterNone,
		}},
	})
	if _, err := engine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflowType, Summary: "collect"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if !engine.IsPhaseExecutionBlocked(userID) {
		t.Fatal("input form should block phase execution")
	}

	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("browser", "browser", nil, nil),
	})

	toolSet := handler.prepareAgentLoopTools(userID, "continue", &LoopContext{SkipNeedsConfirmGate: true}, agentLoopPhase{})
	if len(toolSet.Tools) != 0 {
		t.Fatalf("blocked phase with no usable policy must expose no tools, got %#v", toolNameSetForWorkflowFilterTest(toolSet.Tools))
	}
}

func TestWorkflowGateHelpersTolerateNilLoopContext(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-nil-context-user"
	if _, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("browser", "browser", nil, nil),
	})

	toolSet := handler.prepareAgentLoopTools(userID, "continue", nil, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(toolSet.Tools)
	if names["browser"] || !names["read_file"] {
		t.Fatalf("nil context should keep workflow filter active, got %#v", names)
	}
	if shouldSkipCodingGate(nil, codingToolGateConfig{intent: intentCoding}) {
		t.Fatal("nil context must not skip coding gate")
	}
	if handler.shouldNeedsConfirmToolBranch(nil, userID, 1, codingToolGateConfig{}) != handler.app.workflowEngine.IsPhaseNeedsConfirm(userID) {
		t.Fatal("nil context should not alter NeedsConfirm decision")
	}
}

func TestPolicyRejectedBrowserToolCallRedactedFromConversation(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	tc := toolCallNamed("browser")
	conversation := []interface{}{map[string]interface{}{
		"role":       "assistant",
		"content":    "",
		"tool_calls": []llm.ToolCall{tc},
	}}
	history := []agent.ConversationEntry{{Role: "assistant", ToolCalls: []llm.ToolCall{tc}}}
	recordedResultID := ""

	result := handler.commitAgentLoopToolResult(agentLoopToolCommitOptions{
		ToolCall:        tc,
		TruncatedResult: workflowPolicyToolRejectedText(tc.Function.Name),
		Execution: toolExecutionResult{
			Text:        workflowPolicyToolRejectedText(tc.Function.Name),
			ToolName:    tc.Function.Name,
			Outcome:     toolOutcomeFailed,
			FailureKind: toolFailurePolicyRejected,
		},
		Conversation: conversation,
		History:      history,
		RecordToolResult: func(id string, _ interface{}) {
			recordedResultID = id
		},
	})

	text := strings.ToLower(fmt.Sprintf("%#v %#v %s", result.Conversation, result.History, recordedResultID))
	for _, leaked := range []string{`name:"browser"`, "call_browser"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("policy-rejected browser call should redact %q from model context, got %s", leaked, text)
		}
	}
	if !strings.Contains(text, "blocked_tool") {
		t.Fatalf("expected redacted tool name marker, got %s", text)
	}

	untypedConversation := []interface{}{map[string]interface{}{
		"role": "assistant",
		"tool_calls": []map[string]interface{}{{
			"id":       "call_browser_map",
			"function": map[string]interface{}{"name": "browser"},
		}},
	}}
	untypedConversation = redactRolePrefixRiskToolCallInConversation(untypedConversation, "call_browser_map", "blocked_tool_call_map")
	untypedText := strings.ToLower(fmt.Sprintf("%#v", untypedConversation))
	if strings.Contains(untypedText, "browser") || strings.Contains(untypedText, "call_browser_map") {
		t.Fatalf("map-shaped browser tool call should be redacted, got %s", untypedText)
	}
}

func toolNameSetForWorkflowFilterTest(tools []map[string]interface{}) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[extractToolName(tool)] = true
	}
	return names
}
