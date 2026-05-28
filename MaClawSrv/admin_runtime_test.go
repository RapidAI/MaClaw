package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	schedulerPayload := `[{"id":"task-1","name":"Task","action":"Do it path=` + filepath.ToSlash(dataRoot) + ` token=scheduler-action-secret","hour":1,"minute":2,"day_of_week":-1,"day_of_month":-1,"status":"active","created_at":"` + next.Add(-time.Hour).Format(time.RFC3339Nano) + `","last_run_at":"` + next.Add(-30*time.Minute).Format(time.RFC3339Nano) + `","next_run_at":"` + next.Format(time.RFC3339Nano) + `","run_count":1,"last_result":"wrote ` + filepath.ToSlash(dataRoot) + ` api_key=scheduler-result-secret","last_error":"failed in ` + filepath.ToSlash(dataRoot) + ` token=scheduler-error-secret"}]`
	if err := os.WriteFile(filepath.Join(dataRoot, "scheduled_tasks.json"), []byte(schedulerPayload), 0o600); err != nil {
		t.Fatalf("write scheduled tasks: %v", err)
	}
	t.Setenv("MACLAW_ENABLE_SCHEDULER", "true")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := saveSandboxReport(dataRoot, sandboxDiagnoseReport{ReportID: "sandbox_report_runtime", Status: "pass", Summary: "runtime report", GeneratedAt: time.Now().UTC(), EffectiveBackend: "bwrap", Raw: map[string]interface{}{"path": dataRoot}}); err != nil {
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
	if strings.Contains(runtimeStatus.DataRoot, dataRoot) || strings.Contains(runtimeStatus.RuntimeConfigDir, dataRoot) || strings.Contains(runtimeStatus.Scheduler.Path, dataRoot) {
		t.Fatalf("runtime status should redact local paths: %#v", runtimeStatus)
	}
	if runtimeStatus.LastSandboxReport.Raw != nil {
		t.Fatalf("runtime status should redact sandbox report raw diagnostics: %#v", runtimeStatus.LastSandboxReport.Raw)
	}
	for _, check := range runtimeStatus.Readiness.Checks {
		if strings.Contains(check.Path, dataRoot) {
			t.Fatalf("runtime readiness check path should be redacted: %#v", check)
		}
	}
	for _, source := range runtimeStatus.LogSources {
		if strings.Contains(source.Path, dataRoot) {
			t.Fatalf("runtime log source path should be redacted: %#v", source)
		}
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
	if strings.Contains(schedulerStatus.Path, dataRoot) {
		t.Fatalf("scheduler status path should be redacted: %#v", schedulerStatus)
	}
	schedulerJSON, err := json.Marshal(schedulerStatus)
	if err != nil {
		t.Fatalf("marshal scheduler status: %v", err)
	}
	for _, leaked := range []string{dataRoot, filepath.ToSlash(dataRoot), "scheduler-action-secret", "scheduler-result-secret", "scheduler-error-secret"} {
		if strings.Contains(string(schedulerJSON), leaked) {
			t.Fatalf("scheduler recent tasks should redact %q, got %s", leaked, schedulerJSON)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/readiness", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin readiness status = %d body = %s", w.Code, w.Body.String())
	}
	var readiness readinessReport
	if err := json.NewDecoder(w.Body).Decode(&readiness); err != nil {
		t.Fatalf("decode admin readiness: %v", err)
	}
	if strings.Contains(readiness.DataRoot, dataRoot) {
		t.Fatalf("admin readiness data root should be redacted: %#v", readiness)
	}
	for _, check := range readiness.Checks {
		if strings.Contains(check.Path, dataRoot) {
			t.Fatalf("admin readiness check path should be redacted: %#v", check)
		}
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

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/jobs/"+job.ID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin get job = %d body = %s", w.Code, w.Body.String())
	}
	var got asyncJobRecord
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	if got.ID != job.ID || got.TenantID != "tenant-a" || got.UserID != "user-a" || got.Kind != "admin.test" {
		t.Fatalf("unexpected job detail: %#v", got)
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

func TestAdminSupportBundleIncludesRedactedServiceDiagnostics(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dataRoot, "logs"), 0o700); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "logs", "maclaw_srv.log"), []byte("ERROR token=super-secret Authorization: Bearer abc.def {\"api_key\":\"json-secret\",\"apikey\":\"json-compact-key\",\"apisecret\":\"json-compact-secret\"} apikey=compact-key apisecret:compact-secret path="+dataRoot+" slash_path="+filepath.ToSlash(dataRoot)+"\n"), 0o600); err != nil {
		t.Fatalf("write service log: %v", err)
	}
	if err := saveAdminServiceConfigDraft(dataRoot, adminServiceConfigDraft{Values: map[string]any{"tls_key_file": "super-key.pem", "tls_cert_file": filepath.Join(dataRoot, "certs", "server.pem"), "log_file": filepath.Join(dataRoot, "logs", "maclaw_srv.log"), "sandbox_mode": "bwrap"}}); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	baseAuditTime := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 25; i++ {
		if err := svc.RecordAuditEvent(context.Background(), agentservice.AuditEvent{ActorType: "admin", Action: "admin.owner_required_failed", ResourceType: "admin_authorization", ResourceID: fmt.Sprintf("/api/v1/admin/noise/%02d", i), CreatedAt: baseAuditTime.Add(time.Duration(i) * time.Minute)}); err != nil {
			t.Fatalf("RecordAuditEvent noise %d: %v", i, err)
		}
	}
	if err := svc.RecordAuditEvent(context.Background(), agentservice.AuditEvent{ActorType: "admin", Action: "auth.token_failed", ResourceType: "credential", ResourceID: "cred-1", CreatedAt: baseAuditTime.Add(2 * time.Hour), Metadata: map[string]string{"token": "super-secret", "auth_header": "support-auth-token", "author": "visible-author", "path": dataRoot, "slash_path": filepath.ToSlash(dataRoot)}}); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/support-bundle?download=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("support bundle status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "maclawsrv-support-bundle-") || !strings.Contains(got, ".json") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	var bundle adminSupportBundle
	if err := json.NewDecoder(w.Body).Decode(&bundle); err != nil {
		t.Fatalf("decode support bundle: %v", err)
	}
	if bundle.GeneratedAt.IsZero() || bundle.Runtime.Process.PID == 0 || bundle.Dashboard.GeneratedAt.IsZero() {
		t.Fatalf("unexpected support bundle basics: %#v", bundle)
	}
	if bundle.DataRootName == "" || !bundle.DataRootRedacted || strings.Contains(bundle.Runtime.DataRoot, dataRoot) || strings.Contains(bundle.Runtime.Readiness.DataRoot, dataRoot) {
		t.Fatalf("expected redacted data root in support bundle: %#v", bundle)
	}
	if len(bundle.Redactions) == 0 || bundle.ServiceConfig["environment"] == nil || bundle.SecurityRisks["recent"] == nil {
		t.Fatalf("expected support diagnostics sections: %#v", bundle)
	}
	if bundle.SecurityRisks["total"].(float64) <= float64(len(bundle.SecurityRisks["recent"].([]any))) || bundle.Counts["risk_events"] <= len(bundle.SecurityRisks["recent"].([]any)) {
		t.Fatalf("expected support risk totals to include events beyond recent limit: %#v counts=%#v", bundle.SecurityRisks, bundle.Counts)
	}
	if got := bundle.RecentAuditEvents[0].Metadata["token"]; got != "[redacted]" {
		t.Fatalf("expected redacted audit token, got %#v", bundle.RecentAuditEvents[0].Metadata)
	}
	if got := bundle.RecentAuditEvents[0].Metadata["auth_header"]; got != "[redacted]" {
		t.Fatalf("expected redacted audit auth_header, got %#v", bundle.RecentAuditEvents[0].Metadata)
	}
	if got := bundle.RecentAuditEvents[0].Metadata["author"]; got != "visible-author" {
		t.Fatalf("expected benign author metadata to remain visible, got %#v", bundle.RecentAuditEvents[0].Metadata)
	}
	if got := bundle.RecentAuditEvents[0].Metadata["path"]; strings.Contains(got, dataRoot) {
		t.Fatalf("expected redacted audit path, got %q", got)
	}
	if strings.Contains(bundle.RecentLogErrors[0].Line.Text, "super-secret") || strings.Contains(bundle.RecentLogErrors[0].Line.Text, "abc.def") || strings.Contains(bundle.RecentLogErrors[0].Line.Text, "json-secret") || strings.Contains(bundle.RecentLogErrors[0].Line.Text, dataRoot) {
		t.Fatalf("expected redacted log line, got %q", bundle.RecentLogErrors[0].Line.Text)
	}
	serviceDraft, ok := bundle.ServiceConfig["draft"].(map[string]any)
	if !ok {
		t.Fatalf("expected service draft map, got %#v", bundle.ServiceConfig["draft"])
	}
	values, ok := serviceDraft["values"].(map[string]any)
	if !ok || values["tls_key_file"] != "[redacted]" {
		t.Fatalf("expected redacted sensitive service draft, got %#v", serviceDraft)
	}
	if values["tls_cert_file"] != filepath.Join(filepath.Base(dataRoot), "certs", "server.pem") || values["log_file"] != filepath.Join(filepath.Base(dataRoot), "logs", "maclaw_srv.log") {
		t.Fatalf("expected redacted service draft paths, got %#v", values)
	}
	serviceValidation, ok := bundle.ServiceConfig["validation"].(map[string]any)
	if !ok {
		t.Fatalf("expected service validation map, got %#v", bundle.ServiceConfig["validation"])
	}
	validationNormalized, ok := serviceValidation["normalized"].(map[string]any)
	if !ok || validationNormalized["tls_cert_file"] != filepath.Join(filepath.Base(dataRoot), "certs", "server.pem") || validationNormalized["log_file"] != filepath.Join(filepath.Base(dataRoot), "logs", "maclaw_srv.log") {
		t.Fatalf("expected redacted service validation paths, got %#v", serviceValidation)
	}
	encodedBundle, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal support bundle: %v", err)
	}
	encodedText := string(encodedBundle)
	for _, sensitive := range []string{dataRoot, filepath.ToSlash(dataRoot), "super-secret", "support-auth-token", "abc.def", "json-secret", "json-compact-key", "json-compact-secret", "compact-key", "compact-secret", "super-key.pem"} {
		if strings.Contains(encodedText, sensitive) {
			t.Fatalf("support bundle still contains sensitive value %q: %s", sensitive, encodedText)
		}
	}
	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.support_bundle_downloaded"})
	if err != nil {
		t.Fatalf("ListAuditEvents support bundle: %v", err)
	}
	if len(events) == 0 || events[0].Metadata["download"] != "true" {
		t.Fatalf("expected support bundle audit event, got %#v", events)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/support-bundle?download=maybe", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid support bundle download status = %d body = %s", w.Code, w.Body.String())
	}
}
