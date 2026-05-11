package main

import (
	"testing"

	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
)

func TestNormalizeSkillPipelineStepStatusMapsCoreStatuses(t *testing.T) {
	tests := []struct {
		name string
		in   cskill.PipelineStepStatus
		want skillStepStatus
	}{
		{name: "completed", in: cskill.PipelineStepStatusCompleted, want: skillStepStatusSuccess},
		{name: "failed", in: cskill.PipelineStepStatusFailed, want: skillStepStatusFailed},
		{name: "skipped", in: cskill.PipelineStepStatusSkipped, want: skillStepStatusSkipped},
		{name: "cancelled", in: cskill.PipelineStepStatusCancelled, want: skillStepStatusSkipped},
		{name: "unknown", in: cskill.PipelineStepStatus("other"), want: skillStepStatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSkillPipelineStepStatus(tt.in).StepStatus()
			if got != tt.want {
				t.Fatalf("StepStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
