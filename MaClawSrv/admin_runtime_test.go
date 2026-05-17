package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdminRuntimeStatusAndSchedulerStatus(t *testing.T) {
	dataRoot := t.TempDir()
	next := time.Now().UTC().Add(time.Hour)
	schedulerPayload := `[{"id":"task-1","name":"Task","action":"Do it","hour":1,"minute":2,"day_of_week":-1,"day_of_month":-1,"status":"active","created_at":"` + next.Add(-time.Hour).Format(time.RFC3339Nano) + `","next_run_at":"` + next.Format(time.RFC3339Nano) + `","run_count":0}]`
	if err := os.WriteFile(filepath.Join(dataRoot, "scheduled_tasks.json"), []byte(schedulerPayload), 0o600); err != nil {
		t.Fatalf("write scheduled tasks: %v", err)
	}
	t.Setenv("MACLAW_ENABLE_SCHEDULER", "true")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := saveSandboxReport(dataRoot, sandboxDiagnoseReport{ReportID: "sandbox_report_runtime", Status: "pass", Summary: "runtime report", GeneratedAt: time.Now().UTC(), EffectiveBackend: "bwrap"}); err != nil {
		t.Fatalf("save sandbox report: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/status", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	assertAdminSecurityHeaders(t, w.Result())
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status = %d body = %s", w.Code, w.Body.String())
	}
	var runtimeStatus adminRuntimeStatus
	if err := json.NewDecoder(w.Body).Decode(&runtimeStatus); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if runtimeStatus.Process.PID == 0 || runtimeStatus.Readiness.Status == "" || !runtimeStatus.Scheduler.Enabled || runtimeStatus.Scheduler.TaskCount != 1 {
		t.Fatalf("unexpected runtime status: %#v", runtimeStatus)
	}
	if runtimeStatus.LastSandboxReport == nil || runtimeStatus.LastSandboxReport.ReportID != "sandbox_report_runtime" {
		t.Fatalf("expected latest sandbox report in runtime status: %#v", runtimeStatus.LastSandboxReport)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/scheduler/status", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("scheduler status = %d body = %s", w.Code, w.Body.String())
	}
	var schedulerStatus adminSchedulerStatus
	if err := json.NewDecoder(w.Body).Decode(&schedulerStatus); err != nil {
		t.Fatalf("decode scheduler status: %v", err)
	}
	if schedulerStatus.TaskCount != 1 || len(schedulerStatus.RecentTasks) != 1 || schedulerStatus.ByStatus["active"] != 1 {
		t.Fatalf("unexpected scheduler status: %#v", schedulerStatus)
	}
}

func TestAdminRuntimeGCRecordsAudit(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/runtime/gc", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime gc = %d body = %s", w.Code, w.Body.String())
	}
	var out adminRuntimeGCResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode runtime gc: %v", err)
	}
	if out.GeneratedAt.IsZero() || out.Before.NumGC > out.After.NumGC {
		t.Fatalf("unexpected runtime gc response: %#v", out)
	}
	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.runtime_gc"})
	if err != nil {
		t.Fatalf("ListAuditEvents runtime gc: %v", err)
	}
	if len(events) == 0 || events[0].Metadata["after_alloc_bytes"] == "" {
		t.Fatalf("expected runtime gc audit event, got %#v", events)
	}
}
func TestAdminRuntimeGoroutinesDumpIsTextAndAudited(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/goroutines?debug=1&download=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("goroutine dump status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "maclawsrv-goroutines-") || !strings.Contains(got, ".txt") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "goroutine profile") && !strings.Contains(body, "goroutine") {
		t.Fatalf("unexpected goroutine dump body: %q", body)
	}
	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.runtime_goroutines"})
	if err != nil {
		t.Fatalf("ListAuditEvents runtime goroutines: %v", err)
	}
	if len(events) == 0 || events[0].Metadata["download"] != "true" {
		t.Fatalf("expected runtime goroutine audit event, got %#v", events)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/goroutines?debug=3", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid goroutine debug status = %d body = %s", w.Code, w.Body.String())
	}
}
func TestAdminRuntimeProfileIsTextAndAudited(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/profiles/heap?debug=1&gc=true&download=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("heap profile status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "maclawsrv-heap-profile-") || !strings.Contains(got, ".txt") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "heap profile") && !strings.Contains(body, "heap") {
		t.Fatalf("unexpected heap profile body: %q", body)
	}
	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.runtime_profile"})
	if err != nil {
		t.Fatalf("ListAuditEvents runtime profile: %v", err)
	}
	if len(events) == 0 || events[0].ResourceID != "heap" || events[0].Metadata["gc"] != "true" {
		t.Fatalf("expected runtime profile audit event, got %#v", events)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime/profiles/cmdline?debug=1", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsupported profile status = %d body = %s", w.Code, w.Body.String())
	}
}
func TestAdminCanListAndCancelAsyncJobsAcrossTenants(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	block := make(chan struct{})
	done := make(chan struct{})
	job := server.jobs.createUserJob("admin.test", agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}, func(ctx context.Context) (any, error) {
		defer close(done)
		<-block
		return map[string]string{"ok": "true"}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs?kind=admin.test&limit=10", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin jobs = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Items []asyncJobRecord `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != job.ID {
		t.Fatalf("unexpected jobs: %#v", out)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/jobs/"+job.ID+"/cancel", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin cancel job = %d body = %s", w.Code, w.Body.String())
	}
	close(block)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("job did not finish after unblock")
	}
}
