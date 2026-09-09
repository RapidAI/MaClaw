package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/excel"
)

func TestAsyncQueryReturnsHandleAndJobStatus(t *testing.T) {
	p := filepath.Join(t.TempDir(), "data.xlsx")
	if err := excel.WriteFile(p, excel.WriteData{Sheets: []excel.WriteSheet{{Name: "Sheet1", Rows: [][]excel.WriteCell{{{Value: "name"}, {Value: "amount"}}, {{Value: "a"}, {Value: 3}}}}}}); err != nil {
		t.Fatal(err)
	}
	m := NewManager([]Profile{{ID: "x", Type: SourceExcel, FilePath: p}}, nil)
	defer m.Close()
	m.SetResultStoreDir(t.TempDir())
	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "owner", SessionID: "session"})
	connID, _, err := m.ConnectFor(ctx, "x", "owner", "session")
	if err != nil {
		t.Fatal(err)
	}
	raw := HandleTool(ctx, m, map[string]interface{}{
		"action": "query", "connection_id": connID, "async": true,
		"sql": "select name, amount from sheet", "limit": 10,
	})
	if !strings.Contains(raw, `"job_id"`) {
		t.Fatalf("async submit = %s", raw)
	}
	job := mustToolResult[AsyncJobStatus](t, raw)
	if job.JobID == "" {
		t.Fatal("missing job_id")
	}
	deadline := time.Now().Add(5 * time.Second)
	var status AsyncJobStatus
	for time.Now().Before(deadline) {
		status = mustToolResult[AsyncJobStatus](t, HandleTool(ctx, m, map[string]interface{}{
			"action": "job_status", "job_id": job.JobID,
		}))
		if status.Status == "done" || status.Status == "error" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status.Status != "done" {
		t.Fatalf("job status = %+v", status)
	}
	if status.ResultHandle == "" {
		t.Fatal("expected encrypted result_handle")
	}
	page := mustToolResult[QueryResult](t, HandleTool(ctx, m, map[string]interface{}{
		"action": "query", "connection_id": connID, "cursor": status.ResultHandle, "limit": 10,
	}))
	if page.RowCount < 1 || page.Rows[0][0] != "a" {
		t.Fatalf("paged async result = %+v", page)
	}
}

func TestRunAsyncQueryDoesNotStoreAfterCancel(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	job := &asyncJob{ID: "db-job-cancel", Status: "cancelled", OwnerID: "owner", SessionID: "session", ConnectionID: "conn-1"}
	m.runAsyncQuery(context.Background(), job, &toolTestAdapter{}, QueryRequest{SQL: "select 1"}, "owner", "session", "conn-1")
	status := job.snapshot()
	if status.ResultHandle != "" || status.Status != "cancelled" {
		t.Fatalf("cancelled async query stored a handle: %+v", status)
	}
	m.mu.Lock()
	n := len(m.results)
	m.mu.Unlock()
	if n != 0 {
		t.Fatalf("cancelled async query persisted %d result handles", n)
	}
}

func TestExpireAsyncJobsKeepsCancelledStatus(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	cancelled := false
	job := &asyncJob{
		ID:        "db-job-keep",
		Status:    "running",
		OwnerID:   "owner",
		SessionID: "session",
		Expires:   time.Now().Add(-time.Second),
		cancel:    func() { cancelled = true },
	}
	m.mu.Lock()
	m.jobs[job.ID] = job
	m.expireAsyncJobsLocked()
	m.mu.Unlock()
	if !cancelled {
		t.Fatal("expired running job must be cancelled")
	}
	status, err := m.AsyncJobStatus(job.ID, "owner", "session")
	if err != nil {
		t.Fatalf("cancelled job must remain pollable: %v", err)
	}
	if status.Status != "cancelled" {
		t.Fatalf("status = %+v", status)
	}
}

func TestAdapterIdleSkipsActiveAsync(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	m.SetIdleTTL(time.Nanosecond)
	adapter := &toolTestAdapter{}
	m.mu.Lock()
	m.items["conn-1"] = adapter
	m.lastUsed["conn-1"] = time.Now().Add(-time.Hour)
	m.bindings["conn-1"] = connectionBinding{ownerID: "owner", sessionID: "session"}
	m.jobs["db-job-1"] = &asyncJob{ID: "db-job-1", Status: "running", ConnectionID: "conn-1"}
	m.mu.Unlock()
	got, ok := m.Adapter("conn-1")
	if !ok || got != adapter {
		t.Fatal("idle eviction must not close a connection with a running async query")
	}
}

func TestAsyncJobStatusOwnerIsolation(t *testing.T) {
	m := NewManager(nil, nil)
	defer m.Close()
	ctx := WithRequestScope(context.Background(), RequestScope{OwnerID: "a", SessionID: "s"})
	got := HandleTool(ctx, m, map[string]interface{}{"action": "job_status", "job_id": "db-job-missing"})
	if !strings.Contains(got, "not found") && !strings.Contains(got, "permission") {
		t.Fatalf("got %s", got)
	}
}
