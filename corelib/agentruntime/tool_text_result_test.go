package agentruntime

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestToolTextResultClassifiesSharedMarkers(t *testing.T) {
	cases := []struct {
		text    string
		outcome agent.ToolExecutionOutcome
	}{
		{text: "normal output", outcome: agent.ToolExecutionOutcomeOK},
		{text: "命令超时（30 秒）", outcome: agent.ToolExecutionOutcomeTimeout},
		{text: "[错误] 工具执行失败", outcome: agent.ToolExecutionOutcomeError},
		{text: "unknown memory action", outcome: agent.ToolExecutionOutcomeError},
		{text: "output\nError: printed by command", outcome: agent.ToolExecutionOutcomeOK},
		{text: "tool execution interrupted: context canceled", outcome: agent.ToolExecutionOutcomeError},
	}
	for _, tc := range cases {
		got := ToolTextResult(tc.text)
		if got.Result != tc.text || got.Outcome != tc.outcome {
			t.Errorf("ToolTextResult(%q)=%#v, want outcome=%q", tc.text, got, tc.outcome)
		}
	}
}
