package main

import (
	"strings"
	"testing"
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
