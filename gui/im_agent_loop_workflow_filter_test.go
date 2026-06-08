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
	if plainNames["task"] || plainNames["bash"] || !plainNames["read_file"] {
		t.Fatalf("active workflow should keep phase policy even with plain SkipNeedsConfirmGate, got %#v", plainNames)
	}

	workflowLoop := handler.prepareAgentLoopTools(userID, "build a project", &LoopContext{SkipNeedsConfirmGate: true, WorkflowAgentLoop: true}, agentLoopPhase{})
	workflowNames := toolNameSetForWorkflowFilterTest(workflowLoop.Tools)
	if workflowNames["task"] || workflowNames["bash"] || workflowNames["write_file"] || workflowNames["edit_file"] || !workflowNames["read_file"] {
		t.Fatalf("doc-only workflow phase should expose only planning-safe tools despite SkipNeedsConfirmGate, got %#v", workflowNames)
	}
	if workflowLoop.WorkflowDecision != workflowToolFilterDecision(workflow.ToolFilterDocOnly) {
		t.Fatalf("workflow decision = %q, want %q", workflowLoop.WorkflowDecision, workflow.ToolFilterDocOnly)
	}
}

func TestEnsureWorkflowRequiredToolsRestoresDocContextToolsBeforePolicyFilter(t *testing.T) {
	allTools := []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("list_directory", "list directory", nil, nil),
		toolDef("send_file", "send file", nil, nil),
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

	if !names["read_file"] || !names["list_directory"] || !names["send_file"] {
		t.Fatalf("doc-only workflow required tools missing after merge/filter: %#v", names)
	}
	if names["task"] || names["write_file"] || names["edit_file"] {
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
	if !names["read_file"] || names["write_file"] || names["edit_file"] || names["bash"] {
		t.Fatalf("doc-only workflow phase should keep implementation tools out of doc-only tool set, got %#v", names)
	}
	if names["task"] {
		t.Fatalf("workflow filter must still remove disallowed tools, got %#v", names)
	}
}

func TestFullWorkflowPhasePinsLocalCodingTools(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-full-phase-pins-local-tools-user"
	workflowType := workflow.WorkflowType("full_policy_local_tools")
	if err := handler.app.workflowEngine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:        workflowType,
		Name:        "full policy local tools",
		Description: "test template",
		Phases: []workflow.PhaseTemplate{{
			ID:            "implementation",
			Name:          "Implementation",
			Prompt:        "implement the project",
			Deliverable:   "working code",
			ToolPolicy:    workflow.ToolFilterFull,
			Kind:          workflow.PhaseKindExecution,
			MutationScope: workflow.MutationScopeProject,
		}},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	if _, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflowType, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("bash", "bash", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("list_directory", "list directory", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
		toolDef("task", "task", nil, nil),
	})

	filtered := handler.applyWorkflowToolFilterWithCatalog(userID,
		[]map[string]interface{}{toolDef("task", "task", nil, nil)},
		handler.getTools(),
	)
	names := toolNameSetForWorkflowFilterTest(filtered)
	for _, name := range []string{"bash", "read_file", "list_directory", "write_file", "edit_file"} {
		if !names[name] {
			t.Fatalf("full execution phase must keep local coding tool %s available, got %#v", name, names)
		}
	}
	if !names["task"] {
		t.Fatalf("full policy should preserve routed non-local tools too, got %#v", names)
	}
}

func TestArtifactWorkflowPhaseDoesNotExposeProjectMutationTools(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-artifact-scope-tools-user"
	workflowType := workflow.WorkflowType("artifact_scope_tools")
	if err := handler.app.workflowEngine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:        workflowType,
		Name:        "artifact scope tools",
		Description: "test template",
		Phases: []workflow.PhaseTemplate{{
			ID:            "generate",
			Name:          "Generate",
			Prompt:        "generate artifact",
			Deliverable:   "artifact",
			ToolPolicy:    workflow.ToolFilterFull,
			Kind:          workflow.PhaseKindArtifactGeneration,
			MutationScope: workflow.MutationScopeArtifact,
		}},
	}); err != nil {
		t.Fatalf("Register workflow template: %v", err)
	}
	if _, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflowType, Summary: "make deck"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("bash", "bash", nil, nil),
		toolDef("read_file", "read file", nil, nil),
		toolDef("list_directory", "list directory", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
		toolDef("task", "task", nil, nil),
		toolDef("office", "office", nil, nil),
		toolDef("generate_pdf", "generate pdf", nil, nil),
		toolDef("send_file", "send file", nil, nil),
	})

	filtered := handler.applyWorkflowToolFilterWithCatalog(userID,
		[]map[string]interface{}{toolDef("task", "task", nil, nil), toolDef("edit_file", "edit file", nil, nil)},
		handler.getTools(),
	)
	names := toolNameSetForWorkflowFilterTest(filtered)
	for _, name := range []string{"write_file", "office", "generate_pdf", "send_file"} {
		if !names[name] {
			t.Fatalf("artifact phase should expose %s, got %#v", name, names)
		}
	}
	for _, name := range []string{"edit_file", "task"} {
		if names[name] {
			t.Fatalf("artifact phase should not expose project mutation tool %s, got %#v", name, names)
		}
	}
	if allowed, reason := handler.isWorkflowToolCallAllowedForOwner(userID, "write_file", `{"path":"deck.pptx","content":"body"}`); !allowed {
		t.Fatalf("artifact write should pass: %s", reason)
	}
	if allowed, _ := handler.isWorkflowToolCallAllowedForOwner(userID, "write_file", `{"path":"src/main.go","content":"package main"}`); allowed {
		t.Fatal("artifact phase should reject source writes")
	}
	if allowed, _ := handler.isWorkflowToolCallAllowedForOwner(userID, "bash", `{"command":"touch src/main.go"}`); allowed {
		t.Fatal("artifact phase should reject mutating bash")
	}
}

func TestDocOnlyWorkflowPhaseBlocksImplementationTools(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-doc-only-blocks-implementation-tools-user"
	workflowType := workflow.WorkflowType("doc_only_policy_boundary")
	handler.app.workflowEngine.GetRegistry().Register(&workflow.WorkflowTemplate{
		Type:        workflowType,
		Name:        "doc only policy boundary",
		Description: "test template",
		Phases: []workflow.PhaseTemplate{{
			ID:          "analysis",
			Name:        "Analysis",
			Prompt:      "write analysis",
			Deliverable: "analysis doc",
			ToolPolicy:  workflow.ToolFilterDocOnly,
		}},
	})
	_, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflowType,
		Summary:  "analyze project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("list_directory", "list directory", nil, nil),
		toolDef("bash", "bash", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
		toolDef("task", "task", nil, nil),
		toolDef("delegate_task", "delegate", nil, nil),
	})

	filtered := handler.applyWorkflowToolFilter(userID, handler.getTools())
	names := toolNameSetForWorkflowFilterTest(filtered)
	for _, blocked := range []string{"bash", "write_file", "edit_file", "task", "delegate_task"} {
		if names[blocked] {
			t.Fatalf("%s must not be exposed in doc-only workflow phase; got %#v", blocked, names)
		}
	}
	for _, allowed := range []string{"read_file", "list_directory"} {
		if !names[allowed] {
			t.Fatalf("%s should remain available for planning context; got %#v", allowed, names)
		}
	}

	for _, blocked := range []string{"bash", "write_file", "delegate_task"} {
		if handler.isWorkflowToolAllowed(userID, blocked) {
			t.Fatalf("%s execution must be blocked in doc-only workflow phase", blocked)
		}
		if ok, _ := handler.isWorkflowToolCallAllowed(userID, blocked, `{}`); ok {
			t.Fatalf("%s concrete call must be blocked in doc-only workflow phase", blocked)
		}
	}
}

func TestPlanningWorkflowPhaseAllowsInspectionButBlocksImplementationTools(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "workflow-planning-policy-boundary-user"
	state, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	state.PhaseIndex = 2
	state.CurrentPhase = workflow.PhaseCodingTaskBreakdown
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("list_directory", "list directory", nil, nil),
		toolDef("bash", "bash", nil, nil),
		toolDef("send_file", "send file", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("edit_file", "edit file", nil, nil),
		toolDef("task", "task", nil, nil),
		toolDef("delegate_task", "delegate", nil, nil),
	})

	filtered := handler.applyWorkflowToolFilter(userID, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("task", "task", nil, nil),
	})
	names := toolNameSetForWorkflowFilterTest(filtered)
	for _, allowed := range []string{"bash", "read_file", "list_directory", "send_file"} {
		if !names[allowed] {
			t.Fatalf("%s should remain available in planning workflow phase; got %#v", allowed, names)
		}
	}
	for _, blocked := range []string{"write_file", "edit_file", "task", "delegate_task"} {
		if names[blocked] {
			t.Fatalf("%s must not be exposed in planning workflow phase; got %#v", blocked, names)
		}
		if handler.isWorkflowToolAllowed(userID, blocked) {
			t.Fatalf("%s execution must be blocked in planning workflow phase", blocked)
		}
	}
	if !handler.isWorkflowToolAllowed(userID, "bash") {
		t.Fatal("planning workflow phase should allow bash at tool-name gate")
	}
	if ok, reason := handler.isWorkflowToolCallAllowed(userID, "bash", `{"command":"rg -n \"TODO\""}`); !ok {
		t.Fatalf("planning workflow phase should allow read-only bash call: %s", reason)
	}
	if ok, _ := handler.isWorkflowToolCallAllowed(userID, "bash", `{"command":"touch generated.go"}`); ok {
		t.Fatal("planning workflow phase must block mutating bash calls")
	}
}

func TestDirectToolExecutionDoesNotInheritSingleActiveWorkflowPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "single-active-direct-tool-policy-user"
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

	result := handler.executeToolDetailed("write_file", `{"path":"out.txt","content":"x"}`, nil)
	if result.FailureKind == toolFailurePolicyRejected || strings.Contains(result.Text, "workflow tool policy") {
		t.Fatalf("direct write_file without explicit owner must not inherit single active workflow policy, got %+v", result)
	}
}

func TestDirectToolExecutionDoesNotInheritLastUserWorkflowPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "last-user-direct-tool-policy-user"
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
	handler.lastUserID = userID

	result := handler.executeToolDetailed("write_file", `{"path":"out.txt","content":"x"}`, nil)
	if result.FailureKind == toolFailurePolicyRejected || strings.Contains(result.Text, "workflow tool policy") {
		t.Fatalf("direct write_file without explicit owner must not inherit lastUserID workflow policy, got %+v", result)
	}
}

func TestWorkflowPolicyUserIDEmptyOwnerDoesNotGuessGlobalState(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "last-user-policy-owner"
	if _, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	handler.lastUserID = userID
	handler.currentLoopCtx = &LoopContext{Runtime: RuntimeContext{RequestID: "req-other", PolicyOwnerID: userID}}

	if got := handler.workflowPolicyUserID(""); got != "" {
		t.Fatalf("workflowPolicyUserID(empty) = %q, want empty isolation boundary", got)
	}
}

func TestAgentLoopToolExecutionDoesNotInheritSingleActiveWorkflowPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "single-active-agent-loop-policy-user"
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

	progressEmitted := false
	recordedToolName := ""
	result := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		SkipWorkflowGate: true,
		ToolCall: llm.ToolCall{ID: "call_write", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"out.txt","content":"x"}`,
		}},
		SendToolProgress: func(string) { progressEmitted = true },
		RecordToolCall:   func(_ string, name string, _ string) { recordedToolName = name },
	})
	if result.FailureKind == toolFailurePolicyRejected || strings.Contains(result.Text, "workflow tool policy") {
		t.Fatalf("agent loop write_file without explicit owner must not inherit single active workflow policy, got %+v", result)
	}
	if !progressEmitted {
		t.Fatal("agent loop should proceed to normal execution path when no explicit policy owner is present")
	}
	if recordedToolName != "write_file" {
		t.Fatalf("agent loop should record normal tool execution, got %q", recordedToolName)
	}
}

func TestAgentLoopToolExecutionDoesNotInheritOtherSessionWorkflowPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := "desktop-doc-only-owner"
	weixinID := "o9cq802UzUN9ln7xyVX8S3V93w5g@im.wechat"
	_, err := handler.app.workflowEngine.StartWorkflow(desktopID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}

	desktopResult := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID: desktopID,
		ToolCall: llm.ToolCall{ID: "call_desktop_write", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"out.txt","content":"x"}`,
		}},
	})
	if desktopResult.FailureKind != toolFailurePolicyRejected {
		t.Fatalf("desktop owner should still be blocked by its doc-only workflow, got %+v", desktopResult)
	}

	weixinResult := handler.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		UserID:           weixinID,
		SkipWorkflowGate: true,
		ToolCall: llm.ToolCall{ID: "call_weixin_write", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"out.txt","content":"x"}`,
		}},
	})
	if weixinResult.FailureKind == toolFailurePolicyRejected && strings.Contains(weixinResult.Text, "workflow tool policy") {
		t.Fatalf("weixin session must not inherit desktop doc-only workflow policy, got %+v", weixinResult)
	}
}

func TestPrepareAgentLoopToolsUsesRuntimePolicyOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := "desktop-prepare-tools-doc-only-owner"
	weixinID := "o9cq802UzUN9ln7xyVX8S3V93w5g@im.wechat"
	remoteOwnerID := "remote:mobile-prepare-tools-owner"
	_, err := handler.app.workflowEngine.StartWorkflow(desktopID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "build a project",
	})
	if err != nil {
		t.Fatalf("StartWorkflow desktop failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm desktop failed: %v", err)
	}
	handler.lastUserID = desktopID
	handler.toolDefGen = NewToolDefinitionGenerator(nil, []map[string]interface{}{
		toolDef("read_file", "read file", nil, nil),
		toolDef("write_file", "write file", nil, nil),
		toolDef("bash", "bash", nil, nil),
	})
	ctx := &LoopContext{SkipNeedsConfirmGate: true, Runtime: RuntimeContext{RequestID: "req-weixin-tools", PolicyOwnerID: remoteOwnerID}}

	toolSet := handler.prepareAgentLoopTools(weixinID, "write code", ctx, agentLoopPhase{})
	names := toolNameSetForWorkflowFilterTest(toolSet.Tools)
	if !names["write_file"] || !names["bash"] {
		t.Fatalf("runtime-owned weixin loop must not inherit desktop doc-only tool list, got %#v", names)
	}
	if toolSet.WorkflowDecision != workflowToolFilterSkippedConfirmBypass {
		t.Fatalf("workflow decision = %q, want skipped confirm bypass", toolSet.WorkflowDecision)
	}

	_, err = handler.app.workflowEngine.StartWorkflow(remoteOwnerID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "remote build",
	})
	if err != nil {
		t.Fatalf("StartWorkflow remote failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(remoteOwnerID); err != nil {
		t.Fatalf("SkipPhaseForm remote failed: %v", err)
	}

	toolSet = handler.prepareAgentLoopTools(weixinID, "write code", ctx, agentLoopPhase{})
	names = toolNameSetForWorkflowFilterTest(toolSet.Tools)
	if names["write_file"] || names["bash"] || !names["read_file"] {
		t.Fatalf("runtime owner workflow must drive weixin tool list, got %#v", names)
	}
	if toolSet.WorkflowDecision != workflowToolFilterDecision(workflow.ToolFilterDocOnly) {
		t.Fatalf("workflow decision = %q, want %q", toolSet.WorkflowDecision, workflow.ToolFilterDocOnly)
	}
}

func TestLoopCommandCycleHonorsWorkflowPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "loop-cycle-doc-only-policy-user"
	if _, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	cb := &loopCycleCallbacks{parent: &guiLoopCommandCallbacks{handler: handler, userID: userID}}

	tools := agent.FilterToolDefinitionsByAuthorizer(cb, cb.BuildTools("fix"))
	names := toolNameSetForWorkflowFilterTest(tools)
	if names["write_file"] || names["edit_file"] || names["bash"] {
		t.Fatalf("loop command cycle must not expose implementation tools during doc-only workflow phase, got %#v", names)
	}
	if !names["read_file"] || !cb.IsToolAllowed("read_file") {
		t.Fatalf("loop command cycle should keep read_file available, got %#v", names)
	}
	if cb.IsToolAllowed("write_file") {
		t.Fatal("loop command cycle must reject write_file during doc-only workflow phase")
	}
}

func TestLoopCommandCycleWithoutOwnerDoesNotInheritLastUserWorkflowPolicy(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "loop-cycle-last-user-policy-user"
	if _, err := handler.app.workflowEngine.StartWorkflow(userID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(userID); err != nil {
		t.Fatalf("SkipPhaseForm failed: %v", err)
	}
	handler.lastUserID = userID
	cb := &loopCycleCallbacks{parent: &guiLoopCommandCallbacks{handler: handler}}

	if !cb.IsToolAllowed("write_file") {
		t.Fatal("loop command cycle without explicit owner must not inherit lastUserID workflow policy")
	}
	if ok, reason := cb.IsToolCallAllowed("write_file", `{"path":"out.txt","content":"x"}`); !ok {
		t.Fatalf("loop command call without explicit owner must not inherit lastUserID workflow policy: %s", reason)
	}
	result := cb.ExecuteTool("write_file", `{"content":"x"}`)
	if strings.Contains(result, "workflow tool policy") {
		t.Fatalf("loop command execution without explicit owner must not inherit lastUserID workflow policy, got %q", result)
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
	if !plainSkip {
		t.Fatal("active workflow must keep NeedsConfirm gate active despite plain SkipNeedsConfirmGate")
	}
	workflowLoop := handler.shouldNeedsConfirmToolBranch(&LoopContext{SkipNeedsConfirmGate: true, WorkflowAgentLoop: true}, userID, 1, gateConfig)
	if !workflowLoop {
		t.Fatal("workflow agent loop must keep NeedsConfirm gate active despite SkipNeedsConfirmGate")
	}
}

func TestNeedsConfirmToolBranchUsesRuntimePolicyOwner(t *testing.T) {
	handler, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	desktopID := "desktop-needs-confirm-doc-only-owner"
	remoteOwnerID := "remote:mobile-needs-confirm-owner"
	if _, err := handler.app.workflowEngine.StartWorkflow(desktopID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "build"}); err != nil {
		t.Fatalf("StartWorkflow desktop failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(desktopID); err != nil {
		t.Fatalf("SkipPhaseForm desktop failed: %v", err)
	}
	handler.lastUserID = desktopID
	ctx := &LoopContext{SkipNeedsConfirmGate: true, Runtime: RuntimeContext{RequestID: "req-needs-confirm", PolicyOwnerID: remoteOwnerID}}

	if got := handler.shouldNeedsConfirmToolBranch(ctx, desktopID, 1, codingToolGateConfig{}); got {
		t.Fatal("tool-branch NeedsConfirm must not inherit desktop workflow when runtime owner has no workflow")
	}

	if _, err := handler.app.workflowEngine.StartWorkflow(remoteOwnerID, workflow.StructuredIntent{Category: workflow.WorkflowCoding, Summary: "remote build"}); err != nil {
		t.Fatalf("StartWorkflow remote failed: %v", err)
	}
	if err := handler.app.workflowEngine.SkipPhaseForm(remoteOwnerID); err != nil {
		t.Fatalf("SkipPhaseForm remote failed: %v", err)
	}
	if got := handler.shouldNeedsConfirmToolBranch(ctx, desktopID, 1, codingToolGateConfig{}); !got {
		t.Fatal("tool-branch NeedsConfirm should follow active runtime-owner workflow")
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
	if names["task"] || names["browser"] || names["bash"] || !names["read_file"] {
		t.Fatalf("awaiting-review workflow filter must use planning-safe active phase policy, got %#v", names)
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
