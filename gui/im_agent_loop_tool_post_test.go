package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestPostToolBranchFinalizesVisibleContentAfterCompressContext(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	defer h.memory.Stop()
	history := []agent.ConversationEntry{{Role: "assistant", Content: "weather result", ToolCalls: []llm.ToolCall{{ID: "call-compress", Function: llm.ToolCallFunction{Name: "compress_context"}}}}}
	phase := &agentLoopPhase{Stage: agentStageExecute}

	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		UserID:                     desktopUserID,
		MessageContent:             "weather result",
		AssistantHadVisibleContent: true,
		ToolCalls:                  []llm.ToolCall{{ID: "call-compress", Function: llm.ToolCallFunction{Name: "compress_context"}}},
		ToolOutcomes:               []toolOutcome{toolOutcomeSucceeded},
		History:                    history,
		Phase:                      phase,
		StreamDone:                 true,
	})

	if result.Response == nil {
		t.Fatal("expected visible assistant content to finalize after compress_context")
	}
	if result.Response.Text != "weather result" {
		t.Fatalf("response text = %q, want visible assistant content", result.Response.Text)
	}
	if phase.Stage != agentStageFinalize {
		t.Fatalf("phase stage = %q, want finalize", phase.Stage)
	}
	if !result.PostStreamReturnPrepTime {
		t.Fatal("expected post-stream return prep marker")
	}
}

func TestPostToolBranchDoesNotFinalizeVisibleContentAfterAnswerChangingTool(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	defer h.memory.Stop()

	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		UserID:                     desktopUserID,
		MessageContent:             "let me check",
		AssistantHadVisibleContent: true,
		ToolCalls:                  []llm.ToolCall{{ID: "call-bash", Function: llm.ToolCallFunction{Name: "bash"}}},
		ToolOutcomes:               []toolOutcome{toolOutcomeSucceeded},
	})

	if result.Response != nil {
		t.Fatalf("answer-changing tools must continue loop, got response %q", result.Response.Text)
	}
}

func TestPostToolBranchDoesNotFinalizeMixedResponseNeutralAndAnswerChangingTools(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	defer h.memory.Stop()

	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		UserID:                     desktopUserID,
		MessageContent:             "weather result",
		AssistantHadVisibleContent: true,
		ToolCalls: []llm.ToolCall{
			{ID: "call-compress", Function: llm.ToolCallFunction{Name: "compress_context"}},
			{ID: "call-bash", Function: llm.ToolCallFunction{Name: "bash"}},
		},
		ToolOutcomes: []toolOutcome{toolOutcomeSucceeded, toolOutcomeSucceeded},
	})

	if result.Response != nil {
		t.Fatalf("mixed tool groups must continue loop, got response %q", result.Response.Text)
	}
}

func TestPostToolBranchDoesNotFinalizeFailedPostTurnTool(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	defer h.memory.Stop()

	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		UserID:                     desktopUserID,
		MessageContent:             "weather result",
		AssistantHadVisibleContent: true,
		ToolCalls:                  []llm.ToolCall{{ID: "call-compress", Function: llm.ToolCallFunction{Name: "compress_context"}}},
		ToolOutcomes:               []toolOutcome{toolOutcomeFailed},
	})

	if result.Response != nil {
		t.Fatalf("failed post-turn tool should continue recovery loop, got response %q", result.Response.Text)
	}
}

func TestPostToolBranchDoesNotFinalizeUncertainPostTurnTool(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	defer h.memory.Stop()

	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		UserID:                     desktopUserID,
		MessageContent:             "weather result",
		AssistantHadVisibleContent: true,
		ToolCalls:                  []llm.ToolCall{{ID: "call-compress", Function: llm.ToolCallFunction{Name: "compress_context"}}},
		ToolOutcomes:               []toolOutcome{toolOutcomeUncertain},
	})

	if result.Response != nil {
		t.Fatalf("uncertain post-turn tool should continue recovery loop, got response %q", result.Response.Text)
	}
}

func TestPostToolBranchDoesNotFinalizeReasoningOnlyContent(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	defer h.memory.Stop()

	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		UserID:                     desktopUserID,
		MessageContent:             "private reasoning fallback",
		AssistantHadVisibleContent: false,
		ToolCalls:                  []llm.ToolCall{{ID: "call-compress", Function: llm.ToolCallFunction{Name: "compress_context"}}},
		ToolOutcomes:               []toolOutcome{toolOutcomeSucceeded},
	})

	if result.Response != nil {
		t.Fatalf("reasoning-only tool turn must not be emitted as final text, got response %q", result.Response.Text)
	}
}

func TestPostToolBranchFinalizesWithLengthContinuationAfterCompressContext(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	defer h.memory.Stop()

	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		UserID:                     desktopUserID,
		MessageContent:             "second chunk",
		AssistantHadVisibleContent: true,
		LengthContinuationText:     "first chunk ",
		ToolCalls:                  []llm.ToolCall{{ID: "call-compress", Function: llm.ToolCallFunction{Name: "compress_context"}}},
		ToolOutcomes:               []toolOutcome{toolOutcomeSucceeded},
	})

	if result.Response == nil {
		t.Fatal("expected visible assistant content to finalize after compress_context")
	}
	if result.Response.Text != "first chunk second chunk" {
		t.Fatalf("response text = %q, want assembled continuation", result.Response.Text)
	}
}
