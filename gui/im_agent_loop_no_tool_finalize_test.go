package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestNoToolRecoverPromptsForStructuredExecutionRequirement(t *testing.T) {
	h := &IMMessageHandler{}
	phase := &agentLoopPhase{Stage: agentStageConverge, ConsecutiveNoTool: 1}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "fix failing tests"},
	}

	result := h.handleAgentLoopNoToolRecover(agentLoopNoToolRecoverOptions{
		MessageContent:        "looking at the request",
		TrimmedVisibleContent: "looking at the request",
		Phase:                 phase,
		Conversation:          conversation,
		RequiresExecution:     true,
	})

	if !result.ContinueLoop {
		t.Fatal("expected no-tool execution prompt to continue loop")
	}
	if !phase.NoToolActionPrompted {
		t.Fatal("expected phase to record execution prompt injection")
	}
	if len(result.Conversation) != len(conversation)+1 {
		t.Fatalf("conversation len = %d, want %d", len(result.Conversation), len(conversation)+1)
	}
	systemMsg, ok := result.Conversation[len(result.Conversation)-1].(map[string]string)
	if !ok || systemMsg["role"] != "system" || !strings.Contains(systemMsg["content"], "[Execution requirement]") {
		t.Fatalf("last message = %#v, want execution requirement system prompt", result.Conversation[len(result.Conversation)-1])
	}
}

func TestNoToolRecoverDoesNotPromptForNonExecutionChat(t *testing.T) {
	h := &IMMessageHandler{}
	phase := &agentLoopPhase{Stage: agentStageConverge, ConsecutiveNoTool: 1}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "explain this idea"},
	}

	result := h.handleAgentLoopNoToolRecover(agentLoopNoToolRecoverOptions{
		MessageContent:        "here is the explanation",
		TrimmedVisibleContent: "here is the explanation",
		Phase:                 phase,
		Conversation:          conversation,
		RequiresExecution:     false,
	})

	if result.ContinueLoop || result.Response != nil {
		t.Fatalf("result = %+v, want ordinary finalization path", result)
	}
	if phase.NoToolActionPrompted {
		t.Fatal("non-execution chat should not mark execution prompt injected")
	}
	if len(result.Conversation) != len(conversation) {
		t.Fatalf("conversation len = %d, want unchanged %d", len(result.Conversation), len(conversation))
	}
}

func TestNoToolRecoverDoesNotPromptForCompletedSummary(t *testing.T) {
	h := &IMMessageHandler{}
	phase := &agentLoopPhase{Stage: agentStageConverge, ConsecutiveNoTool: 1}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "\u751f\u6210 pdf \u53d1\u6211"},
	}

	result := h.handleAgentLoopNoToolRecover(agentLoopNoToolRecoverOptions{
		UserText:              "\u751f\u6210 pdf \u53d1\u6211",
		MessageContent:        "\u5df2\u5b8c\u6210\uff0c\u4ee5\u4e0b\u662f\u603b\u7ed3\uff1a2025 \u5e74 AI \u8d8b\u52bf\u96c6\u4e2d\u5728\u591a\u6a21\u6001\u3001Agent \u4e0e\u5c0f\u578b\u5316\u90e8\u7f72\u3002",
		TrimmedVisibleContent: "\u5df2\u5b8c\u6210\uff0c\u4ee5\u4e0b\u662f\u603b\u7ed3\uff1a2025 \u5e74 AI \u8d8b\u52bf\u96c6\u4e2d\u5728\u591a\u6a21\u6001\u3001Agent \u4e0e\u5c0f\u578b\u5316\u90e8\u7f72\u3002",
		Phase:                 phase,
		Conversation:          conversation,
		RequiresExecution:     true,
	})

	if result.ContinueLoop || result.Response != nil {
		t.Fatalf("result = %+v, want ordinary finalization path", result)
	}
	if phase.NoToolActionPrompted {
		t.Fatal("completed summary should not mark execution prompt injected")
	}
}

func TestNoToolRecoverPromptsLocalStoredInfoRecall(t *testing.T) {
	h := &IMMessageHandler{}
	phase := &agentLoopPhase{Stage: agentStageConverge, ConsecutiveNoTool: 1}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "\u77e5\u9053api2\u670d\u52a1\u5668\u4fe1\u606f\u5417\uff1f"},
	}

	result := h.handleAgentLoopNoToolRecover(agentLoopNoToolRecoverOptions{
		UserText:              "\u77e5\u9053api2\u670d\u52a1\u5668\u4fe1\u606f\u5417\uff1f",
		MessageContent:        "\u6839\u636e\u8bb0\u5fc6\uff0c\u5f53\u524d\u6ca1\u6709\u5173\u4e8eapi2\u670d\u52a1\u5668\u7684\u5b58\u50a8\u4fe1\u606f\u3002",
		TrimmedVisibleContent: "\u6839\u636e\u8bb0\u5fc6\uff0c\u5f53\u524d\u6ca1\u6709\u5173\u4e8eapi2\u670d\u52a1\u5668\u7684\u5b58\u50a8\u4fe1\u606f\u3002",
		Phase:                 phase,
		Conversation:          conversation,
	})

	if !result.ContinueLoop {
		t.Fatal("expected local stored info no-tool answer to continue loop")
	}
	if !phase.LocalInfoRecallPrompted {
		t.Fatal("expected phase to record local info recall prompt")
	}
	systemMsg, ok := result.Conversation[len(result.Conversation)-1].(map[string]string)
	if !ok || systemMsg["role"] != "system" {
		t.Fatalf("last message = %#v, want system prompt", result.Conversation[len(result.Conversation)-1])
	}
	for _, want := range []string{"memory(action=\"recall\")", "knowledge_search"} {
		if !strings.Contains(systemMsg["content"], want) {
			t.Fatalf("local info recall prompt missing %q:\n%s", want, systemMsg["content"])
		}
	}
}

func TestNoToolBranchDoesNotFinalizeCompletedLocalStoredInfoWithoutLookup(t *testing.T) {
	h := &IMMessageHandler{}
	phase := &agentLoopPhase{Stage: agentStageConverge}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "\u77e5\u9053api2\u670d\u52a1\u5668\u4fe1\u606f\u5417\uff1f"},
	}

	result := h.handleAgentLoopNoToolBranch(agentLoopNoToolBranchOptions{
		UserText:                 "\u77e5\u9053api2\u670d\u52a1\u5668\u4fe1\u606f\u5417\uff1f",
		MessageContent:           "\u5df2\u5b8c\u6210\uff0c\u4ee5\u4e0b\u662f\u603b\u7ed3\uff1a\u672a\u627e\u5230api2\u670d\u52a1\u5668\u4fe1\u606f\u3002",
		Choice:                   llm.Choice{Message: llm.Message{Role: "assistant", Content: "\u5df2\u5b8c\u6210\uff0c\u4ee5\u4e0b\u662f\u603b\u7ed3\uff1a\u672a\u627e\u5230api2\u670d\u52a1\u5668\u4fe1\u606f\u3002"}},
		Phase:                    phase,
		Conversation:             conversation,
		LengthContinuationBuffer: &strings.Builder{},
	})

	if result.ReadyToFinalize {
		t.Fatal("local stored info answer must not finalize before recall/search")
	}
	if !result.ContinueLoop {
		t.Fatal("expected local stored info answer to continue for recall/search")
	}
	if !phase.LocalInfoRecallPrompted {
		t.Fatal("expected local info recall prompt")
	}
}

func TestNoToolBranchRecoversTruncatedWriteFileBeforeFinalize(t *testing.T) {
	h := &IMMessageHandler{}
	phase := &agentLoopPhase{Stage: agentStageConverge}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "write a script"},
	}
	recorded := false

	result := h.handleAgentLoopNoToolBranch(agentLoopNoToolBranchOptions{
		UserText:                 "write a script",
		MessageContent:           "I'll write the script now:",
		Choice:                   llm.Choice{FinishReason: "length", TruncatedToolNames: []string{"write_file"}},
		Phase:                    phase,
		Conversation:             conversation,
		Tools:                    []map[string]interface{}{toolDef("write_file", "write", nil, nil)},
		LengthContinuationBuffer: &strings.Builder{},
		RecordSystemMessages:     func(int, []interface{}) { recorded = true },
	})

	if !result.ContinueLoop {
		t.Fatal("truncated write_file must continue the loop instead of finalizing text")
	}
	if result.ReadyToFinalize || result.Response != nil {
		t.Fatalf("result finalized despite truncated tool call: %+v", result)
	}
	if phase.TruncationRetries != 1 {
		t.Fatalf("TruncationRetries = %d, want 1", phase.TruncationRetries)
	}
	if !recorded {
		t.Fatal("expected recovery system message to be recorded")
	}
	if len(result.Conversation) != len(conversation)+1 {
		t.Fatalf("conversation len = %d, want %d", len(result.Conversation), len(conversation)+1)
	}
	systemMsg, ok := result.Conversation[len(result.Conversation)-1].(map[string]string)
	if !ok || systemMsg["role"] != "system" || !strings.Contains(systemMsg["content"], "write_file") {
		t.Fatalf("last message = %#v, want write_file recovery system hint", result.Conversation[len(result.Conversation)-1])
	}
}

func TestNoToolReplyHeuristicDoesNotTreatPromisedSummaryAsComplete(t *testing.T) {
	intent, ok := classifyAgentNoToolReplyByHeuristic("I will send the summary shortly.")
	if !ok || intent != agentNoToolReplyPromise {
		t.Fatalf("intent=%q ok=%v, want promise", intent, ok)
	}

	intent, ok = classifyAgentNoToolReplyByHeuristic("\u5df2\u5b8c\u6210\uff0c\u4ee5\u4e0b\u662f\u603b\u7ed3\uff1a\u5df2\u5904\u7406\u3002")
	if !ok || intent != agentNoToolReplyComplete {
		t.Fatalf("intent=%q ok=%v, want complete", intent, ok)
	}

	intent, ok = classifyAgentNoToolReplyByHeuristic("done")
	if ok || intent != agentNoToolReplyUnknown {
		t.Fatalf("intent=%q ok=%v, want unknown for bare done", intent, ok)
	}

	intent, ok = classifyAgentNoToolReplyByHeuristic("\u6587\u4ef6\u5df2\u53d1\u7ed9\u4f60\u3002")
	if ok || intent != agentNoToolReplyUnknown {
		t.Fatalf("intent=%q ok=%v, want unknown for completed send statement", intent, ok)
	}

	intent, ok = classifyAgentNoToolReplyByHeuristic("\u5df2\u5b8c\u6210\uff0c\u7a0d\u540e\u53d1\u7ed9\u4f60\u3002")
	if !ok || intent != agentNoToolReplyPromise {
		t.Fatalf("intent=%q ok=%v, want promise for delayed send", intent, ok)
	}

	intent, ok = classifyAgentNoToolReplyByHeuristic("\u5df2\u5b8c\u6210\uff0c\u4ee5\u4e0b\u662f\u603b\u7ed3\uff1a\u7a0d\u540e\u53ef\u4ee5\u7ee7\u7eed\u4f18\u5316\u3002")
	if !ok || intent != agentNoToolReplyComplete {
		t.Fatalf("intent=%q ok=%v, want complete when summary marker is present", intent, ok)
	}
}

func TestNoToolBranchRequiresExecutionFromStructuredContext(t *testing.T) {
	// With no context and no phase, should return false
	if noToolBranchRequiresExecution(nil, nil) {
		t.Fatal("nil context and nil phase should not require execution")
	}
	// Workflow agent loop (non-doc phase) should require execution
	if !noToolBranchRequiresExecution(&LoopContext{WorkflowAgentLoop: true}, nil) {
		t.Fatal("workflow agent loop should require execution")
	}
	// Workflow doc phase should NOT require execution
	if noToolBranchRequiresExecution(&LoopContext{WorkflowAgentLoop: true, WorkflowDocPhase: true}, nil) {
		t.Fatal("workflow doc phase should not require execution")
	}
	// Skill preference should require execution
	if !noToolBranchRequiresExecution(nil, &agentLoopPhase{ForceSkillPreference: true}) {
		t.Fatal("skill preference should require execution")
	}
}

func TestUserRequestRequiresToolExecutionMatchesEnglishWordsOnly(t *testing.T) {
	if !userRequestRequiresToolExecution("run failing tests") {
		t.Fatal("run as a standalone word should require tool execution")
	}
	for _, text := range []string{
		"explain runtime behavior",
		"what is a brunch menu",
		"describe preinstall options",
	} {
		if userRequestRequiresToolExecution(text) {
			t.Fatalf("userRequestRequiresToolExecution(%q)=true, want false", text)
		}
	}
}

func TestNoToolRecoverCodingWorkflowImplementationPromptsDelegateTask(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "no-tool-coding-workflow-implementation"
	state, err := h.app.workflowEngine.StartWorkflowWithOptions(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "implement approved tasks",
	}, workflow.WorkflowStartOptions{ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	moveWorkflowStateToPhase(t, h.app.workflowEngine, state, workflow.PhaseCodingImplementation)
	phase := &agentLoopPhase{Stage: agentStageConverge, ConsecutiveNoTool: 1}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "start implementation"},
	}

	result := h.handleAgentLoopNoToolRecover(agentLoopNoToolRecoverOptions{
		Context:               &LoopContext{WorkflowAgentLoop: true},
		UserID:                userID,
		MessageContent:        "I will implement the tasks.",
		TrimmedVisibleContent: "I will implement the tasks.",
		Phase:                 phase,
		Conversation:          conversation,
		RequiresExecution:     true,
	})

	if !result.ContinueLoop {
		t.Fatal("expected coding implementation no-tool recovery to continue loop")
	}
	systemMsg, ok := result.Conversation[len(result.Conversation)-1].(map[string]string)
	if !ok || systemMsg["role"] != "system" {
		t.Fatalf("last message = %#v, want system prompt", result.Conversation[len(result.Conversation)-1])
	}
	for _, want := range []string{"CodingSubAgent", "delegate_task(agent=\"coding_workflow\"", "approved task IDs"} {
		if !strings.Contains(systemMsg["content"], want) {
			t.Fatalf("coding implementation no-tool prompt missing %q:\n%s", want, systemMsg["content"])
		}
	}
	for _, bad := range []string{"file generation", "write_file"} {
		if strings.Contains(systemMsg["content"], bad) {
			t.Fatalf("coding implementation no-tool prompt should not suggest %q:\n%s", bad, systemMsg["content"])
		}
	}
}

func TestEmptyNoToolRecoverCodingWorkflowImplementationPromptsDelegateTask(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "empty-no-tool-coding-workflow-implementation"
	state, err := h.app.workflowEngine.StartWorkflowWithOptions(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "implement approved tasks",
	}, workflow.WorkflowStartOptions{ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	moveWorkflowStateToPhase(t, h.app.workflowEngine, state, workflow.PhaseCodingImplementation)
	phase := &agentLoopPhase{Stage: agentStageConverge, ConsecutiveNoTool: 1}

	result := h.handleAgentLoopNoToolRecover(agentLoopNoToolRecoverOptions{
		Context:               &LoopContext{WorkflowAgentLoop: true},
		UserID:                userID,
		MessageContent:        "",
		TrimmedVisibleContent: "",
		Phase:                 phase,
		RequiresExecution:     true,
	})

	if !result.ContinueLoop {
		t.Fatal("expected empty coding implementation response to recover")
	}
	if phase.RecoverReason != agentRecoverEmptyFinalResponse {
		t.Fatalf("recover reason = %q, want %q", phase.RecoverReason, agentRecoverEmptyFinalResponse)
	}
	for _, want := range []string{"CodingSubAgent", "delegate_task(agent=\"coding_workflow\"", "approved task IDs"} {
		if !strings.Contains(phase.RecoverPrompt, want) {
			t.Fatalf("empty coding implementation recover prompt missing %q:\n%s", want, phase.RecoverPrompt)
		}
	}
	for _, bad := range []string{"file generation", "write_file"} {
		if strings.Contains(phase.RecoverPrompt, bad) {
			t.Fatalf("empty coding implementation recover prompt should not suggest %q:\n%s", bad, phase.RecoverPrompt)
		}
	}
}

func TestNoToolHardCapCodingWorkflowImplementationReportsHandoffFailure(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.app.testHomeDir = t.TempDir()
	h.memory = agent.NewConversationMemory()
	defer h.memory.Stop()

	userID := "hardcap-no-tool-coding-workflow-implementation"
	state, err := h.app.workflowEngine.StartWorkflowWithOptions(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "implement approved tasks",
	}, workflow.WorkflowStartOptions{ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	moveWorkflowStateToPhase(t, h.app.workflowEngine, state, workflow.PhaseCodingImplementation)

	phase := &agentLoopPhase{Stage: agentStageConverge, ConsecutiveNoTool: maxConsecutiveNoTool + 1}
	result := h.maybeExitAgentLoopForNoToolHardCap(
		&LoopContext{WorkflowAgentLoop: true},
		userID,
		"I cannot write files because there are no tools.",
		"",
		phase,
		nil,
		false,
		func(*IMAgentResponse) {},
		func(*IMAgentResponse) {},
	)

	if result.Response == nil {
		t.Fatal("expected coding implementation hard-cap response")
	}
	for _, want := range []string{"Workflow tooling error", "CodingSubAgent", "delegate_task(agent=\"coding_workflow\""} {
		if !strings.Contains(result.Response.Text, want) {
			t.Fatalf("hard-cap response missing %q:\n%s", want, result.Response.Text)
		}
	}
	for _, bad := range []string{"there are no tools", "I cannot write files"} {
		if strings.Contains(result.Response.Text, bad) {
			t.Fatalf("hard-cap response should not echo model hallucination %q:\n%s", bad, result.Response.Text)
		}
	}
}

func TestToolAvailabilityHallucinationCodingWorkflowImplementationPromptsDelegateTask(t *testing.T) {
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	userID := "hallucinated-delegate-tool-coding-workflow-implementation"
	state, err := h.app.workflowEngine.StartWorkflowWithOptions(userID, workflow.StructuredIntent{
		Category: workflow.WorkflowCoding,
		Summary:  "implement approved tasks",
	}, workflow.WorkflowStartOptions{ProjectPath: t.TempDir()})
	if err != nil {
		t.Fatalf("StartWorkflowWithOptions failed: %v", err)
	}
	moveWorkflowStateToPhase(t, h.app.workflowEngine, state, workflow.PhaseCodingImplementation)

	phase := &agentLoopPhase{Stage: agentStageConverge}
	conversation := []interface{}{
		map[string]string{"role": "user", "content": "start implementation"},
	}
	result := h.handleAgentLoopNoToolBranch(agentLoopNoToolBranchOptions{
		Context:        &LoopContext{WorkflowAgentLoop: true},
		UserID:         userID,
		MessageContent: "I do not have delegate_task tool available.",
		Choice: llm.Choice{Message: llm.Message{
			Role:    "assistant",
			Content: "I do not have delegate_task tool available.",
		}},
		Phase:                    phase,
		Conversation:             conversation,
		Tools:                    testToolDefs("read_file", "list_directory", "delegate_task"),
		LengthContinuationBuffer: &strings.Builder{},
		AttachLLMTelemetry:       func(*IMAgentResponse) {},
		AttachVisibleArtifacts:   func(*IMAgentResponse) {},
	})

	if !result.ContinueLoop {
		t.Fatal("expected hallucinated delegate_task absence to recover")
	}
	if len(result.Conversation) != len(conversation)+1 {
		t.Fatalf("conversation len = %d, want %d", len(result.Conversation), len(conversation)+1)
	}
	correction := msgContent(result.Conversation[len(result.Conversation)-1])
	for _, want := range []string{"CodingSubAgent", "delegate_task(agent=\"coding_workflow\"", "approved task IDs"} {
		if !strings.Contains(correction, want) {
			t.Fatalf("coding implementation tool correction missing %q:\n%s", want, correction)
		}
	}
}
