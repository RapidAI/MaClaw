package agentruntime

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestSemanticToolExecutionResultClassifiesTerminalMarkers(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		outcome agent.ToolExecutionOutcome
	}{
		{name: "success", text: "done", outcome: agent.ToolExecutionOutcomeOK},
		{name: "rejected", text: "[system rejected] stale_surface", outcome: agent.ToolExecutionOutcomeError},
		{name: "unknown", text: "[system unknown] receipt unavailable", outcome: agent.ToolExecutionOutcomeError},
		{name: "legacy error", text: "Error: adapter failed", outcome: agent.ToolExecutionOutcomeError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SemanticToolExecutionResult(tc.text)
			if got.Result != tc.text || got.Outcome != tc.outcome {
				t.Fatalf("result=%#v, want text=%q outcome=%q", got, tc.text, tc.outcome)
			}
		})
	}
}
