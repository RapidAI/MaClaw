package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestNormalizeAgentLoopToolArgumentsJSON(t *testing.T) {
	if got := normalizeAgentLoopToolArgumentsJSON(""); got != "{}" {
		t.Fatalf("empty args normalize to %q, want {}", got)
	}
	if got := normalizeAgentLoopToolArgumentsJSON(" \n\t "); got != "{}" {
		t.Fatalf("blank args normalize to %q, want {}", got)
	}
	if got := normalizeAgentLoopToolArgumentsJSON(`{"path":"README.md"}`); got != `{"path":"README.md"}` {
		t.Fatalf("non-empty args changed to %q", got)
	}
}

func TestExecuteToolDetailedBlankArgumentsDoNotParseFail(t *testing.T) {
	h := &IMMessageHandler{}
	result := h.executeToolDetailedWithRuntimeState("", false, "", "unknown_tool_for_blank_args", " \n\t ", "", nil)
	if result.FailureKind == toolFailureArgumentParse {
		t.Fatalf("blank args should not cause argument parse failure: %+v", result)
	}
	if result.Outcome != toolOutcomeFailed || result.FailureKind != toolFailureUnknownTool {
		t.Fatalf("result = %+v, want unknown tool failure", result)
	}
}

func TestExecuteAgentLoopToolCallRejectsInvalidJSONBeforeRegistry(t *testing.T) {
	h := &IMMessageHandler{}
	result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
		ToolCall: llm.ToolCall{ID: "call_bad_json", Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: `{"path":"a.go","content":"` + strings.Repeat("x", 9000),
		}},
	})
	if result.FailureKind != toolFailureArgumentParse {
		t.Fatalf("failure kind = %q text=%q, want argument parse", result.FailureKind, result.Text)
	}
	if !strings.Contains(result.Text, "appears truncated") || strings.Contains(result.Text, "argument parse failed:") {
		t.Fatalf("result should guide truncated JSON retry without generic parse text: %q", result.Text)
	}
}

func TestExecuteAgentLoopToolCallRejectsNonObjectJSONBeforeRegistry(t *testing.T) {
	h := &IMMessageHandler{}
	for _, args := range []string{`[]`, `null`, `"text"`} {
		result := h.executeAgentLoopToolCall(agentLoopToolExecutionOptions{
			ToolCall: llm.ToolCall{ID: "call_non_object", Function: llm.ToolCallFunction{
				Name:      "read_file",
				Arguments: args,
			}},
		})
		if result.FailureKind != toolFailureArgumentParse {
			t.Fatalf("args %s failure kind = %q text=%q, want argument parse", args, result.FailureKind, result.Text)
		}
		if !strings.Contains(result.Text, "valid JSON object") || strings.Contains(strings.ToLower(result.Text), "argument parse failed") {
			t.Fatalf("result should guide JSON object retry for %s: %q", args, result.Text)
		}
	}
}
