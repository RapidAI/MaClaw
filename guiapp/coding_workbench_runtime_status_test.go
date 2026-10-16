package guiapp

import (
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestCodingWorkbenchStepRuntimeStatusProjection(t *testing.T) {
	cases := map[string]agentruntime.JobStatus{
		codingStepPending:    agentruntime.JobStatusPending,
		codingStepRunning:    agentruntime.JobStatusRunning,
		codingStepPassed:     agentruntime.JobStatusSucceeded,
		codingStepFailed:     agentruntime.JobStatusFailed,
		codingStepVerifyFail: agentruntime.JobStatusFailed,
		codingStepSkipped:    agentruntime.JobStatusCanceled,
	}
	for status, want := range cases {
		data, err := json.Marshal(codingWorkbenchStepStatus{Status: status})
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			RuntimeStatus agentruntime.JobStatus `json:"runtime_status"`
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got.RuntimeStatus != want {
			t.Errorf("status %q projected to %q, want %q", status, got.RuntimeStatus, want)
		}
	}
}
