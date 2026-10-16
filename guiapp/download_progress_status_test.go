package guiapp

import (
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestDownloadProgressIncludesRuntimeStatus(t *testing.T) {
	payload, err := json.Marshal(DownloadProgress{Status: downloadProgressStatusVerifying})
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

func TestDownloadProgressNamedStatus(t *testing.T) {
	if got := downloadProgressNamedStatus(10, ""); got != downloadProgressStatusDownloading {
		t.Fatalf("running status=%q", got)
	}
	if got := downloadProgressNamedStatus(100, ""); got != downloadProgressStatusCompleted {
		t.Fatalf("completed status=%q", got)
	}
	if got := downloadProgressNamedStatus(100, "checksum failed"); got != downloadProgressStatusError {
		t.Fatalf("error status=%q", got)
	}
}
