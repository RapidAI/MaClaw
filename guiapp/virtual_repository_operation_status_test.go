package guiapp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestVirtualRepositoryOperationRuntimeStatus(t *testing.T) {
	cases := map[string]agentruntime.JobStatus{
		"pending":         agentruntime.JobStatusPending,
		"running":         agentruntime.JobStatusRunning,
		"success":         agentruntime.JobStatusSucceeded,
		"failed":          agentruntime.JobStatusFailed,
		"cancelled":       agentruntime.JobStatusCanceled,
		"partial_success": agentruntime.JobStatusUnknown,
	}
	for status, want := range cases {
		if got := virtualRepositoryOperationRuntimeStatus(status); got != want {
			t.Errorf("runtime status for %q=%q, want %q", status, got, want)
		}
	}
}

func TestMarshalVirtualRepositoryOperationJobIncludesRuntimeStatus(t *testing.T) {
	job := &virtualRepositoryOperationJob{result: VirtualRepositoryOperationResult{JobID: "job-1", Status: "partial_success"}}
	raw, err := marshalVirtualRepositoryOperationJob(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, _ := payload["runtime_status"].(string); got != string(agentruntime.JobStatusUnknown) {
		t.Fatalf("runtime_status=%q, want unknown (payload=%s)", got, strings.TrimSpace(raw))
	}
}
