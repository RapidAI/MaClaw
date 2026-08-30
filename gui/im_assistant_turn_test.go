package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestBuildAgentLoopAssistantTurn_KeepsReasoningWithMidTextRolePrefix(t *testing.T) {
	reasoning := "先分析请求。\nBrowser: 需要打开目标页面\nTool: 再执行 bash 命令"
	h := &IMMessageHandler{}
	turn := h.buildAgentLoopAssistantTurn(&LoopContext{}, llm.Choice{
		Message: llm.Message{
			Role:             "assistant",
			Content:          "done",
			ReasoningContent: reasoning,
		},
		FinishReason: "stop",
	})
	if turn.Reasoning != reasoning {
		t.Fatalf("reasoning was truncated by role-prefix stripping: got %q, want %q", turn.Reasoning, reasoning)
	}
	if got := turn.HistoryEntry.ReasoningContent; got != reasoning {
		t.Fatalf("history reasoning = %q, want %q", got, reasoning)
	}
	if got := turn.Message["reasoning_content"]; got != reasoning {
		t.Fatalf("message reasoning_content = %q, want %q", got, reasoning)
	}
}

func TestBuildAgentLoopAssistantTurn_StripsLeadingReasoningRolePrefix(t *testing.T) {
	h := &IMMessageHandler{}
	turn := h.buildAgentLoopAssistantTurn(&LoopContext{}, llm.Choice{
		Message: llm.Message{
			Role:             "assistant",
			Content:          "done",
			ReasoningContent: "Tool: 开头的角色前缀应剥离",
		},
		FinishReason: "stop",
	})
	if want := "开头的角色前缀应剥离"; turn.Reasoning != want {
		t.Fatalf("reasoning = %q, want %q", turn.Reasoning, want)
	}
}
