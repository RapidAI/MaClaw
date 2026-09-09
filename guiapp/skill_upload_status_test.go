package guiapp

import (
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestSkillUploadRuntimeStatusProjection(t *testing.T) {
	cases := map[skillUploadStatus]agentruntime.JobStatus{
		skillUploadStatusPending:         agentruntime.JobStatusPending,
		skillUploadStatusUploading:       agentruntime.JobStatusRunning,
		skillUploadStatusUploaded:        agentruntime.JobStatusSucceeded,
		skillUploadStatusFailed:          agentruntime.JobStatusFailed,
		skillUploadStatusBlocked:         agentruntime.JobStatusFailed,
		skillUploadStatusRemoteSubmitted: agentruntime.JobStatusUnknown,
	}
	for status, want := range cases {
		if got := status.RuntimeStatusValue(); got != want {
			t.Errorf("RuntimeStatusValue(%q)=%q, want %q", status, got, want)
		}
	}
	item := SkillUploadQueueItem{ID: "upload-1", Status: skillUploadStatusUploading}
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if got, _ := decoded["runtime_status"].(string); got != string(agentruntime.JobStatusRunning) {
		t.Fatalf("runtime_status=%q, want running", got)
	}
}
