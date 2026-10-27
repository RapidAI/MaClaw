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

func TestVirtualRepositoryOperationProgressOmitsItems(t *testing.T) {
	job := &virtualRepositoryOperationJob{
		targetCount: 3,
		result: VirtualRepositoryOperationResult{
			JobID:  "job-progress",
			Status: "running",
			Items: []VirtualRepositoryOperationItemResult{
				{NodeID: "one", Status: "success", Output: "first output"},
				{NodeID: "two", Status: "failed", Error: "second error"},
			},
		},
	}
	raw, err := marshalVirtualRepositoryOperationJobProgress(job)
	if err != nil {
		t.Fatalf("marshal progress: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode progress: %v", err)
	}
	if _, ok := payload["items"]; ok {
		t.Fatalf("progress contains items: %s", raw)
	}
	if payload["completed"] != float64(2) || payload["target_count"] != float64(3) {
		t.Fatalf("progress counts = %v/%v, want 2/3", payload["completed"], payload["target_count"])
	}
	if got, _ := payload["runtime_status"].(string); got != string(agentruntime.JobStatusRunning) {
		t.Fatalf("runtime_status=%q, want running", got)
	}
	fullRaw, err := marshalVirtualRepositoryOperationJob(job)
	if err != nil {
		t.Fatalf("marshal full result: %v", err)
	}
	var full VirtualRepositoryOperationResult
	if err := json.Unmarshal([]byte(fullRaw), &full); err != nil {
		t.Fatalf("decode full result: %v", err)
	}
	if len(full.Items) != 2 || full.Items[0].Output != "first output" || full.Completed != 2 || full.TargetCount != 3 {
		t.Fatalf("full result changed: %#v", full)
	}
}

func TestVirtualRepositoryOperationItemsPagination(t *testing.T) {
	job := &virtualRepositoryOperationJob{result: VirtualRepositoryOperationResult{
		JobID:  "test-item-pages",
		Status: "running",
		Items: []VirtualRepositoryOperationItemResult{
			{NodeID: "one", Status: "success", Output: "one"},
			{NodeID: "two", Status: "success", Output: "two"},
			{NodeID: "three", Status: "failed", Error: "three"},
		},
	}}
	virtualRepositoryOperationJobs.Lock()
	virtualRepositoryOperationJobs.items[job.result.JobID] = job
	virtualRepositoryOperationJobs.Unlock()
	t.Cleanup(func() {
		virtualRepositoryOperationJobs.Lock()
		delete(virtualRepositoryOperationJobs.items, job.result.JobID)
		virtualRepositoryOperationJobs.Unlock()
	})
	a := &App{}
	raw, err := a.GetVirtualRepositoryOperationItems(job.result.JobID, 0, 2)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	var first VirtualRepositoryOperationItemsPage
	if err := json.Unmarshal([]byte(raw), &first); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if first.Offset != 0 || first.NextOffset != 2 || first.Total != 3 || len(first.Items) != 2 || first.Items[1].NodeID != "two" {
		t.Fatalf("first page = %#v", first)
	}
	raw, err = a.GetVirtualRepositoryOperationItems(job.result.JobID, first.NextOffset, 200)
	if err != nil {
		t.Fatalf("last page: %v", err)
	}
	var last VirtualRepositoryOperationItemsPage
	if err := json.Unmarshal([]byte(raw), &last); err != nil {
		t.Fatalf("decode last page: %v", err)
	}
	if last.Offset != 2 || last.NextOffset != 3 || last.Total != 3 || len(last.Items) != 1 || last.Items[0].Error != "three" {
		t.Fatalf("last page = %#v", last)
	}
	if _, err := a.GetVirtualRepositoryOperationItems(job.result.JobID, 4, 1); err == nil {
		t.Fatal("offset beyond completed count should fail")
	}
	if _, err := a.GetVirtualRepositoryOperationItems(job.result.JobID, 0, 0); err == nil {
		t.Fatal("non-positive limit should fail")
	}
	fullRaw, err := a.GetVirtualRepositoryOperation(job.result.JobID)
	if err != nil {
		t.Fatalf("full result: %v", err)
	}
	var full VirtualRepositoryOperationResult
	if err := json.Unmarshal([]byte(fullRaw), &full); err != nil {
		t.Fatalf("decode full result: %v", err)
	}
	if len(full.Items) != 3 || full.Items[2].Error != "three" {
		t.Fatalf("full operation endpoint lost items: %#v", full)
	}
}
