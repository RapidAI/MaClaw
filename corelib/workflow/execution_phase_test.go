package workflow

import "testing"

func TestIsExecutionOrchestratorPhase(t *testing.T) {
	cases := []struct {
		name  string
		phase PhaseTemplate
		want  bool
	}{
		{name: "full execution", phase: PhaseTemplate{ToolPolicy: ToolFilterFull}, want: true},
		{name: "confirmation phase", phase: PhaseTemplate{ToolPolicy: ToolFilterFull, NeedsConfirm: true}, want: false},
		{name: "doc phase", phase: PhaseTemplate{ToolPolicy: ToolFilterDocOnly}, want: false},
		{name: "controlled ops", phase: PhaseTemplate{ToolPolicy: ToolFilterOpsControlled}, want: false},
		{name: "explicit opt out", phase: PhaseTemplate{ToolPolicy: ToolFilterFull, DisableOrchestrator: true}, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsExecutionOrchestratorPhase(tc.phase)
			if got != tc.want {
				t.Fatalf("IsExecutionOrchestratorPhase(%#v)=%v, want %v", tc.phase, got, tc.want)
			}
		})
	}
}
