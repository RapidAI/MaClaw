package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
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

func TestNoToolBranchRequiresExecutionFromStructuredContext(t *testing.T) {
	if !noToolBranchRequiresExecution(nil, codingToolGateConfig{intent: intentCoding}, nil) {
		t.Fatal("coding maintenance/bugfix intent should require execution")
	}
	if noToolBranchRequiresExecution(nil, codingToolGateConfig{intent: intentCoding, active: true}, nil) {
		t.Fatal("active new-project needs-confirm gate should not force tool execution before doc review")
	}
	if !noToolBranchRequiresExecution(nil, codingToolGateConfig{intent: intentSSH}, nil) {
		t.Fatal("ssh intent should require execution")
	}
	if !noToolBranchRequiresExecution(&LoopContext{WorkflowAgentLoop: true}, codingToolGateConfig{}, nil) {
		t.Fatal("workflow agent loop should require execution")
	}
	if !noToolBranchRequiresExecution(nil, codingToolGateConfig{}, &agentLoopPhase{ForceSkillPreference: true}) {
		t.Fatal("skill preference should require execution")
	}
	if noToolBranchRequiresExecution(nil, codingToolGateConfig{intent: intentNonCoding}, nil) {
		t.Fatal("non-coding chat should not require execution")
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
