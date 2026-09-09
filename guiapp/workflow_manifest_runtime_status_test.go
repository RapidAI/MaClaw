package guiapp

import (
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestWorkflowManifestRuntimeStatusUsesSharedVocabulary(t *testing.T) {
	manifest := WorkflowManifest{Status: "cancelled", RuntimeStatus: agentruntime.ProjectLegacyJobStatus("cancelled")}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload["runtime_status"]; got != string(agentruntime.JobStatusCanceled) {
		t.Fatalf("runtime_status=%v, want %q", got, agentruntime.JobStatusCanceled)
	}
}
