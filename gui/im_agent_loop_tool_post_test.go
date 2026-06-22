package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestPostToolBranchContinuesLoopAfterCompressContext(t *testing.T) {
	// compress_context should NEVER finalize the loop. The LLM compresses context
	// to free space for upcoming work. The loop must continue so the LLM can
	// deliver on its promise (e.g. "let me now generate the report").
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	defer h.memory.Stop()
	history := []agent.ConversationEntry{{Role: "assistant", Content: "让我提供综合评估报告", ToolCalls: []llm.ToolCall{{ID: "call-compress", Function: llm.ToolCallFunction{Name: "compress_context"}}}}}
	phase := &agentLoopPhase{Stage: agentStageExecute}

	result := h.handleAgentLoopPostToolBranch(agentLoopPostToolBranchOptions{
		UserID:                     desktopUserID,
		MessageContent:             "让我提供综合评估报告",
		AssistantHadVisibleContent: true,
		ToolCalls:                  []llm.ToolCall{{ID: "call-compress", Function: llm.ToolCallFunction{Name: "compress_context"}}},
		ToolOutcomes:               []toolOutcome{toolOutcomeSucceeded},
		History:                    history,
		Phase:                      phase,
		StreamDone:                 true,
	})

	if result.Response != nil {
		t.Fatalf("compress_context must not finalize loop, got response %q", result.Response.Text)
	}
	if phase.Stage == agentStageFinalize {
		t.Fatal("phase must not be set to finalize after compress_context")
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

func TestPostToolBranchContinuesLoopAfterMixedCompressAndOtherTools(t *testing.T) {
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

func TestPostToolBranchContinuesAfterFailedCompressContext(t *testing.T) {
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
		t.Fatalf("failed compress_context should continue loop, got response %q", result.Response.Text)
	}
}

func TestPostToolBranchContinuesAfterUncertainCompressContext(t *testing.T) {
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
		t.Fatalf("uncertain compress_context should continue loop, got response %q", result.Response.Text)
	}
}

func TestPostToolBranchContinuesAfterReasoningOnlyCompressContext(t *testing.T) {
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
		t.Fatalf("reasoning-only compress_context must not finalize, got response %q", result.Response.Text)
	}
}

func TestPostToolBranchContinuesWithLengthContinuationAfterCompressContext(t *testing.T) {
	// Even with length-continuation text accumulated, compress_context must not finalize.
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

	if result.Response != nil {
		t.Fatalf("compress_context with length continuation must not finalize, got response %q", result.Response.Text)
	}
}
