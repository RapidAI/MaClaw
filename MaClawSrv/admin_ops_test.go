package main

import (
	"bytes"
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

func TestAdminOpsEndpoints(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	cases := []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{method: http.MethodGet, path: "/api/v1/admin/service-config/effective", want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/admin/i18n/locales", want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/admin/i18n/messages?locale=en-US", want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/admin/sandbox/status", want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/admin/sandbox/config", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/admin/sandbox/detect", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/admin/sandbox/switch", body: `{"mode":"auto"}`, want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/admin/sandbox/install-plan?backend=bwrap", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/admin/sandbox/install", body: `{"backend":"bwrap"}`, want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/admin/sandbox/diagnose", body: `{"write_report":true}`, want: 0},
		{method: http.MethodGet, path: "/api/v1/admin/sandbox/events", want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/admin/sandbox/support-bundle", want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/admin/sandbox/profiles", want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/admin/sandbox/reports", want: http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if tc.want != 0 && w.Code != tc.want {
			t.Fatalf("%s %s status = %d want %d body = %s", tc.method, tc.path, w.Code, tc.want, w.Body.String())
		}
		if tc.want == 0 && w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s status = %d body = %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestAdminI18NLocaleNormalization(t *testing.T) {
	t.Setenv("MACLAW_ADMIN_WEB_DEFAULT_LOCALE", "en")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/i18n/locales", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"default_locale":"en-US"`) || !strings.Contains(w.Body.String(), `"label"`) {
		t.Fatalf("locales status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/i18n/messages?locale=zh_CN", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"locale":"zh-CN"`) || !strings.Contains(w.Body.String(), "sandbox.diagnose.title") {
		t.Fatalf("normalized messages status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/i18n/messages?locale=es", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "enabled_locales") {
		t.Fatalf("unsupported locale status = %d body = %s", w.Code, w.Body.String())
	}
}
func TestAdminSandboxStartupDiagnoseWhenEnabled(t *testing.T) {
	t.Setenv("MACLAW_SANDBOX_STARTUP_DIAGNOSE", "true")
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	_ = NewHTTPServer(svc, "admin-secret", nil)

	deadline := time.Now().Add(3 * time.Second)
	for {
		reports, err := listSandboxReports(dataRoot)
		if err != nil {
			t.Fatalf("list reports: %v", err)
		}
		if len(reports) > 0 {
			events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{ResourceID: reports[0].ReportID})
			if err != nil {
				t.Fatalf("ListAuditEvents startup diagnose: %v", err)
			}
			for _, event := range events {
				if event.Action == "admin.sandbox_startup_diagnose_completed" || event.Action == "admin.sandbox_startup_diagnose_failed" {
					return
				}
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("startup diagnose did not persist a report and audit event")
		}
		time.Sleep(25 * time.Millisecond)
	}
}
func TestAdminSandboxConfigCanSwitchRuntimeMode(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/sandbox/config", bytes.NewBufferString(`{"mode":"none"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsafe sandbox mode without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/sandbox/config", bytes.NewBufferString(`{"mode":"none","strict":true,"confirm_unsafe":true,"reason":"debug sandbox failure"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update sandbox config status = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Config adminSandboxConfigResponse `json:"config"`
		Status sandboxStatus              `json:"status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode sandbox config update: %v", err)
	}
	if out.Config.Mode.Value != "none" || out.Config.Mode.Source != "runtime_config" || out.Status.Mode != "none" || !out.Status.Strict {
		t.Fatalf("unexpected sandbox config update: %#v", out)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/sandbox/switch", bytes.NewBufferString(`{"mode":"auto","reason":"switch back"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("switch sandbox status = %d body = %s", w.Code, w.Body.String())
	}
	var switchOut struct {
		Config   adminSandboxConfigResponse `json:"config"`
		Status   sandboxStatus              `json:"status"`
		Diagnose sandboxDiagnoseReport      `json:"diagnose"`
	}
	if err := json.NewDecoder(w.Body).Decode(&switchOut); err != nil {
		t.Fatalf("decode switch sandbox: %v", err)
	}
	if switchOut.Config.Mode.Value != "auto" || switchOut.Diagnose.ReportID == "" {
		t.Fatalf("unexpected switch response: %#v", switchOut)
	}
	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.sandbox_backend_switched"})
	if err != nil {
		t.Fatalf("ListAuditEvents sandbox switch: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected sandbox switch audit event")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/sandbox/config", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get sandbox config status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/sandbox/rollback", bytes.NewBufferString(`{"reason":"rollback sandbox config"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rollback sandbox config status = %d body = %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode rollback sandbox config: %v", err)
	}
	if out.Config.Mode.Source == "runtime_config" || out.Status.Mode == "none" || out.Status.ModeSource == "runtime_config" || out.Status.Strict {
		t.Fatalf("unexpected rollback sandbox config: %#v", out)
	}
}

func TestAdminSandboxDiagnosePersistsReport(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sandbox/diagnose", bytes.NewBufferString(`{"write_report":true,"include_mcp_stdio_test":true}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
		t.Fatalf("diagnose status = %d body = %s", w.Code, w.Body.String())
	}
	var report sandboxDiagnoseReport
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode diagnose: %v", err)
	}
	if report.ReportID == "" || len(report.Checks) == 0 {
		t.Fatalf("unexpected report: %#v", report)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/sandbox/reports/"+report.ReportID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get report status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/sandbox/reports/"+report.ReportID, nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DELETE sandbox report without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/sandbox/reports/"+report.ReportID+"?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE sandbox report status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminSandboxDiagnoseFailsWhenProfileMissing(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sandbox/diagnose", bytes.NewBufferString(`{"profile":"missing","write_report":false}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("diagnose missing profile status = %d body = %s", w.Code, w.Body.String())
	}
	var report sandboxDiagnoseReport
	if err := json.NewDecoder(w.Body).Decode(&report); err != nil {
		t.Fatalf("decode diagnose: %v", err)
	}
	found := false
	for _, check := range report.Checks {
		if check.ID == "profile_load" && check.Status == "fail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing failed profile_load check: %#v", report.Checks)
	}
}

func TestAdminSandboxInstallDefaultsToPrintOnly(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sandbox/install", bytes.NewBufferString(`{"backend":"bwrap"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sandbox install print-only status = %d body = %s", w.Code, w.Body.String())
	}
	var out sandboxInstallResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode sandbox install: %v", err)
	}
	if out.Mode != "print_only" || out.Executed || out.Plan.WillExecute || out.Backend != "bwrap" || len(out.Plan.Commands) == 0 {
		t.Fatalf("unexpected print-only install response: %#v", out)
	}
}

func TestAdminSandboxInstallRunRequiresConfirmAndPolicy(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sandbox/install", bytes.NewBufferString(`{"backend":"bwrap","mode":"run"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("sandbox install run without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/sandbox/install", bytes.NewBufferString(`{"backend":"bwrap","mode":"run","confirm":true}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("sandbox install run without policy status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestSandboxReportRetentionPrunesOldReports(t *testing.T) {
	t.Setenv("MACLAW_SANDBOX_REPORT_RETENTION", "2")
	dataRoot := t.TempDir()
	base := time.Now().UTC()
	for i := 0; i < 4; i++ {
		report := sandboxDiagnoseReport{ReportID: newSandboxReportID(), Status: "pass", GeneratedAt: base.Add(time.Duration(i) * time.Minute)}
		if err := saveSandboxReport(dataRoot, report); err != nil {
			t.Fatalf("save report %d: %v", i, err)
		}
	}
	reports, err := listSandboxReports(dataRoot)
	if err != nil {
		t.Fatalf("list reports: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("reports len = %d want 2: %#v", len(reports), reports)
	}
	if !reports[0].GeneratedAt.After(reports[1].GeneratedAt) {
		t.Fatalf("reports not sorted newest first: %#v", reports)
	}
	entries, err := os.ReadDir(sandboxReportDir(dataRoot))
	if err != nil {
		t.Fatalf("read report dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("report files len = %d want 2", len(entries))
	}
}

func TestAdminSandboxDiagnosticsWriteAuditEvents(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/admin/sandbox/detect"},
		{method: http.MethodPost, path: "/api/v1/admin/sandbox/smoke-test"},
		{method: http.MethodPost, path: "/api/v1/admin/sandbox/diagnose", body: `{"write_report":false}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK && w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s status = %d body = %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}

	for _, action := range []string{"admin.sandbox_detected", "admin.sandbox_diagnose_started"} {
		events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: action})
		if err != nil {
			t.Fatalf("ListAuditEvents %s: %v", action, err)
		}
		if len(events) == 0 {
			t.Fatalf("expected audit action %s", action)
		}
	}
	completed, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.sandbox_diagnose_completed"})
	if err != nil {
		t.Fatalf("ListAuditEvents diagnose completed: %v", err)
	}
	failed, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.sandbox_diagnose_failed"})
	if err != nil {
		t.Fatalf("ListAuditEvents diagnose failed: %v", err)
	}
	if len(completed)+len(failed) == 0 {
		t.Fatalf("expected diagnose completion audit")
	}
}

func TestAdminSandboxSupportBundleIncludesTroubleshootingData(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := saveAdminRuntimeConfig(dataRoot, adminRuntimeConfig{SandboxMode: "bwrap", Reason: "debug token=super-secret path=" + dataRoot}); err != nil {
		t.Fatalf("save runtime config: %v", err)
	}
	if err := saveSandboxReport(dataRoot, sandboxDiagnoseReport{ReportID: "sandbox_report_bundle", Status: "warn", GeneratedAt: time.Now().UTC(), EffectiveBackend: "bwrap"}); err != nil {
		t.Fatalf("save report: %v", err)
	}
	if err := svc.RecordAuditEvent(context.Background(), agentservice.AuditEvent{ActorType: "admin", Action: "admin.sandbox_diagnose_completed", ResourceType: "sandbox_report", ResourceID: "sandbox_report_bundle", Metadata: map[string]string{"status": "warn", "effective_backend": "bwrap", "token": "super-secret", "path": dataRoot}}); err != nil {
		t.Fatalf("RecordAuditEvent sandbox: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/sandbox/support-bundle", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("support bundle status = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Reports       []sandboxDiagnoseReport    `json:"reports"`
		Events        []agentservice.AuditEvent  `json:"events"`
		Config        adminSandboxConfigResponse `json:"config"`
		InstallPlan   sandboxInstallPlan         `json:"install_plan"`
		SecurityRisks struct {
			GeneratedAt time.Time        `json:"generated_at"`
			Filters     map[string]any   `json:"filters"`
			Total       int              `json:"total"`
			Counts      map[string]int   `json:"counts"`
			KindCounts  map[string]int   `json:"kind_counts"`
			Recent      []adminRiskEvent `json:"recent"`
		} `json:"security_risks"`
		DataRootName     string               `json:"data_root_name"`
		DataRootRedacted bool                 `json:"data_root_redacted"`
		DataRoot         string               `json:"data_root"`
		LogSources       []adminLogSource     `json:"log_sources"`
		RecentLogErrors  []adminRecentLogLine `json:"recent_log_errors"`
		Redactions       []string             `json:"redactions"`
		ReportCount      int                  `json:"report_count"`
		EventCount       int                  `json:"event_count"`
		ProfileCount     int                  `json:"profile_count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode support bundle: %v", err)
	}
	if out.DataRoot != "" || !out.DataRootRedacted || out.DataRootName == "" || len(out.Redactions) == 0 || out.Redactions[0] != "data_root" || out.ReportCount == 0 || len(out.Reports) == 0 || out.Reports[0].ReportID != "sandbox_report_bundle" || out.EventCount == 0 || len(out.Events) == 0 || out.InstallPlan.Backend == "" || out.SecurityRisks.GeneratedAt.IsZero() || out.SecurityRisks.Filters["source"] != "sandbox_support_bundle" || out.SecurityRisks.Filters["limit"].(float64) != 10 || out.SecurityRisks.Total == 0 || out.SecurityRisks.KindCounts["sandbox_not_strict"] == 0 || len(out.LogSources) == 0 {
		t.Fatalf("unexpected support bundle: %#v", out)
	}
	if out.Events[0].Metadata["token"] != "[redacted]" || strings.Contains(out.Events[0].Metadata["path"], dataRoot) {
		t.Fatalf("expected redacted sandbox event metadata: %#v", out.Events[0].Metadata)
	}
	if strings.Contains(out.Config.Reason, "super-secret") || strings.Contains(out.Config.Reason, dataRoot) {
		t.Fatalf("expected redacted sandbox config reason, got %q", out.Config.Reason)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/sandbox/support-bundle?download=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("support bundle download status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "maclawsrv-sandbox-support-bundle-") || !strings.Contains(got, ".json") {
		t.Fatalf("support bundle download content disposition = %q", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/sandbox/support-bundle?download=maybe", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid support bundle download status = %d body = %s", w.Code, w.Body.String())
	}

	downloadEvents, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.sandbox_support_bundle_downloaded"})
	if err != nil {
		t.Fatalf("ListAuditEvents support bundle download: %v", err)
	}
	if len(downloadEvents) == 0 || downloadEvents[0].Metadata["download"] != "true" {
		t.Fatalf("expected support bundle download audit event, got %#v", downloadEvents)
	}

	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.sandbox_support_bundle_generated"})
	if err != nil {
		t.Fatalf("ListAuditEvents support bundle: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("expected support bundle audit event")
	}
}
func TestAdminSandboxEventsFiltersSandboxAudit(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := svc.RecordAuditEvent(context.Background(), agentservice.AuditEvent{ActorType: "admin", Action: "admin.sandbox_diagnose_completed", ResourceType: "sandbox_report", ResourceID: "sandbox_report_1", Metadata: map[string]string{"status": "pass", "effective_backend": "bwrap"}}); err != nil {
		t.Fatalf("RecordAuditEvent sandbox: %v", err)
	}
	if err := svc.RecordAuditEvent(context.Background(), agentservice.AuditEvent{ActorType: "admin", Action: "admin.logs_read", ResourceType: "log_source", ResourceID: "service"}); err != nil {
		t.Fatalf("RecordAuditEvent other: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/sandbox/events?status=pass&backend=bwrap", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sandbox events status = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Items []agentservice.AuditEvent `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode sandbox events: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].Action != "admin.sandbox_diagnose_completed" {
		t.Fatalf("unexpected sandbox events: %#v", out.Items)
	}
}

func TestValidateSandboxProfileNormalizesCaseAndWarnings(t *testing.T) {
	valid := validateSandboxProfile(sandboxProfile{Name: "local", Backend: "BWRAP", Network: "HOST"})
	if !valid.Valid || len(valid.Warnings) == 0 {
		t.Fatalf("expected case-normalized host profile warning: %#v", valid)
	}

	invalid := validateSandboxProfile(sandboxProfile{Name: "bad", Backend: "Auto"})
	if invalid.Valid {
		t.Fatalf("expected Auto backend to be invalid: %#v", invalid)
	}
}

func TestAdminSandboxProfilesCanValidateAndPersist(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	body := `{"backend":"bwrap","network":"disabled","readonly_paths":["/usr"],"writable_paths":["/tmp"],"env_allowlist":["PATH"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/sandbox/profiles/local/validate", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("validate profile status = %d body = %s", w.Code, w.Body.String())
	}
	var validation sandboxProfileValidation
	if err := json.NewDecoder(w.Body).Decode(&validation); err != nil {
		t.Fatalf("decode validation: %v", err)
	}
	if !validation.Valid {
		t.Fatalf("unexpected invalid profile: %#v", validation)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/sandbox/profiles/local", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put profile status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/sandbox/profiles/local", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "bwrap") {
		t.Fatalf("get profile status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/sandbox/profiles/local", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("DELETE profile without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/sandbox/profiles/local?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE profile status = %d body = %s", w.Code, w.Body.String())
	}

	updated, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.sandbox_profile_updated"})
	if err != nil {
		t.Fatalf("ListAuditEvents profile updated: %v", err)
	}
	deleted, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.sandbox_profile_deleted"})
	if err != nil {
		t.Fatalf("ListAuditEvents profile deleted: %v", err)
	}
	if len(updated) == 0 || len(deleted) == 0 {
		t.Fatalf("expected profile update/delete audit events, got updated=%d deleted=%d", len(updated), len(deleted))
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/sandbox/profiles/bad", bytes.NewBufferString(`{"backend":"none"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad profile status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminSecurityRiskEvents(t *testing.T) {
	t.Setenv("MACLAW_ALLOW_INSECURE_HTTP", "true")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	ctx := context.Background()
	if err := svc.RecordAuditEvent(ctx, agentservice.AuditEvent{ActorType: "admin", Action: "auth.token_failed", ResourceType: "credential", ResourceID: filepath.Join(svc.DataRoot(), "cred-1"), Metadata: map[string]string{"token": "secret-token", "path": svc.DataRoot(), "api_key": "secret-api-key"}}); err != nil {
		t.Fatalf("RecordAuditEvent auth failure: %v", err)
	}
	if err := svc.RecordAuditEvent(ctx, agentservice.AuditEvent{ActorType: "admin", Action: "admin.sandbox_diagnose_failed", ResourceType: "sandbox_report", ResourceID: "report-1", Metadata: map[string]string{"status": "fail"}}); err != nil {
		t.Fatalf("RecordAuditEvent sandbox failure: %v", err)
	}
	if err := svc.RecordAuditEvent(ctx, agentservice.AuditEvent{ActorType: "admin", Action: "admin.snapshot_created", ResourceType: "snapshot", ResourceID: "snapshot-1", Metadata: map[string]string{"include_secrets": "true"}}); err != nil {
		t.Fatalf("RecordAuditEvent snapshot risk: %v", err)
	}
	if err := svc.RecordAuditEvent(ctx, agentservice.AuditEvent{ActorType: "admin", Action: "admin.service_state_exported", ResourceType: "service_state", ResourceID: "service", Metadata: map[string]string{"include_secrets": "true"}}); err != nil {
		t.Fatalf("RecordAuditEvent export risk: %v", err)
	}
	if err := svc.RecordAuditEvent(ctx, agentservice.AuditEvent{ActorType: "admin", Action: "admin.service_state_imported", ResourceType: "service_state", ResourceID: "service", Metadata: map[string]string{"dry_run": "false"}}); err != nil {
		t.Fatalf("RecordAuditEvent import risk: %v", err)
	}
	for _, event := range []agentservice.AuditEvent{
		{ActorType: "admin", Action: "admin.credential_created", ResourceType: "credential", ResourceID: "cred-created"},
		{ActorType: "admin", Action: "admin.credential_secret_rotated", ResourceType: "credential", ResourceID: "cred-rotated"},
		{ActorType: "admin", Action: "admin.sandbox_config_updated", ResourceType: "sandbox", ResourceID: "bwrap"},
		{ActorType: "admin", Action: "admin.service_config_draft_updated", ResourceType: "service_config", ResourceID: "draft"},
		{ActorType: "admin", Action: "admin.knowledge_access_cross_tenant_updated", ResourceType: "knowledge_access", ResourceID: "cross_tenant"},
		{ActorType: "admin", Action: "admin.skill_sources_global_updated", ResourceType: "skill_source_policy", ResourceID: "global"},
		{ActorType: "admin", Action: "admin.support_bundle_downloaded", ResourceType: "support_bundle", ResourceID: "service"},
		{ActorType: "admin", Action: "admin.logs_rotate", ResourceType: "log_source", ResourceID: "service"},
		{ActorType: "admin", Action: "admin.job_cancel", ResourceType: "job", ResourceID: "job-1"},
		{ActorType: "admin", Action: "admin.runtime_gc", ResourceType: "runtime", ResourceID: "process"},
		{ActorType: "admin", Action: "admin.owner_required_failed", ResourceType: "admin_authorization", ResourceID: "/api/v1/admin/sandbox/switch"},
	} {
		if err := svc.RecordAuditEvent(ctx, event); err != nil {
			t.Fatalf("RecordAuditEvent %s: %v", event.Action, err)
		}
	}
	if err := svc.RecordAuditEvent(ctx, agentservice.AuditEvent{ActorType: "admin", Action: "admin.logs_read", ResourceType: "log_source", ResourceID: "service"}); err != nil {
		t.Fatalf("RecordAuditEvent unrelated: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/risk-events?severity=high&limit=20", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("risk events status = %d body = %s", w.Code, w.Body.String())
	}
	var high struct {
		GeneratedAt time.Time        `json:"generated_at"`
		Filters     map[string]any   `json:"filters"`
		Items       []adminRiskEvent `json:"items"`
		Total       int              `json:"total"`
		Counts      map[string]int   `json:"counts"`
		KindCounts  map[string]int   `json:"kind_counts"`
	}
	if err := json.NewDecoder(w.Body).Decode(&high); err != nil {
		t.Fatalf("decode high risk events: %v", err)
	}
	if high.GeneratedAt.IsZero() || high.Filters["severity"] != "high" || high.Filters["limit"].(float64) != 20 || high.Total == 0 || high.Counts["high"] == 0 || high.KindCounts["sandbox_failed"] == 0 {
		t.Fatalf("expected high risk events and kind counts, got %#v", high)
	}
	seenSandboxFailure := false
	seenInsecureHTTP := false
	seenImportRisk := false
	seenExportRisk := false
	seenSnapshotRisk := false
	for _, item := range high.Items {
		if item.Severity != "high" {
			t.Fatalf("severity filter returned non-high item: %#v", item)
		}
		switch item.Kind {
		case "sandbox_failed":
			seenSandboxFailure = true
		case "insecure_http":
			seenInsecureHTTP = true
		case "service_state_imported":
			seenImportRisk = true
		case "service_state_secrets_exported":
			seenExportRisk = true
		case "snapshot_secrets_created":
			seenSnapshotRisk = true
		case "auth_failed":
			t.Fatalf("medium auth failure should not be returned by high severity filter: %#v", item)
		}
	}
	if !seenSandboxFailure || !seenInsecureHTTP || !seenImportRisk || !seenExportRisk || !seenSnapshotRisk {
		t.Fatalf("expected sandbox failure and insecure HTTP risks, got %#v", high.Items)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/risk-events?severity=debug", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid risk severity status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/risk-events?limit=0", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid risk limit status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/risk-events?severity=HIGH&limit=20", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("uppercase risk severity status = %d body = %s", w.Code, w.Body.String())
	}
	var uppercaseSeverity struct {
		Items []adminRiskEvent `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&uppercaseSeverity); err != nil {
		t.Fatalf("decode uppercase risk events: %v", err)
	}
	if uppercaseSeverity.Total == 0 || len(uppercaseSeverity.Items) == 0 {
		t.Fatalf("expected uppercase severity filter to match high risk events, got %#v", uppercaseSeverity)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/risk-events?since=2026-01-02T00:00:00Z&until=2026-01-01T00:00:00Z", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid risk time range status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/summary?since=2026-01-02T00:00:00Z&until=2026-01-01T00:00:00Z", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid summary time range status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/risk-events?kind=AUTH_FAILED", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("kind-filtered risk events status = %d body = %s", w.Code, w.Body.String())
	}
	var kindFiltered struct {
		Filters    map[string]any   `json:"filters"`
		Items      []adminRiskEvent `json:"items"`
		Total      int              `json:"total"`
		KindCounts map[string]int   `json:"kind_counts"`
	}
	if err := json.NewDecoder(w.Body).Decode(&kindFiltered); err != nil {
		t.Fatalf("decode kind-filtered risk events: %v", err)
	}
	if kindFiltered.Filters["kind"] != "auth_failed" || kindFiltered.Total == 0 || len(kindFiltered.Items) == 0 || kindFiltered.KindCounts["auth_failed"] == 0 {
		t.Fatalf("expected auth_failed risk events and kind counts, got %#v", kindFiltered)
	}
	joinedKindFiltered, err := json.Marshal(kindFiltered.Items)
	if err != nil {
		t.Fatalf("marshal kind-filtered risk events: %v", err)
	}
	if strings.Contains(string(joinedKindFiltered), "secret-token") || strings.Contains(string(joinedKindFiltered), "secret-api-key") || strings.Contains(string(joinedKindFiltered), svc.DataRoot()) {
		t.Fatalf("expected risk event metadata and paths to be redacted, got %s", joinedKindFiltered)
	}
	for _, item := range kindFiltered.Items {
		if item.Kind != "auth_failed" {
			t.Fatalf("kind filter returned wrong risk event: %#v", item)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/risk-events?limit=20", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("all risk events status = %d body = %s", w.Code, w.Body.String())
	}
	var all struct {
		Items []adminRiskEvent `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&all); err != nil {
		t.Fatalf("decode all risk events: %v", err)
	}
	seenKinds := map[string]bool{}
	for _, item := range all.Items {
		seenKinds[item.Kind] = true
		if item.Action == "admin.logs_read" {
			t.Fatalf("unrelated audit event should not become a risk event: %#v", item)
		}
	}
	for _, kind := range []string{"auth_failed", "admin_authorization_denied", "credential_created", "credential_rotated", "sandbox_admin_changed", "service_config_changed", "knowledge_policy_changed", "skill_source_policy_changed", "diagnostics_bundle_downloaded", "log_rotated", "job_canceled", "runtime_gc"} {
		if !seenKinds[kind] {
			t.Fatalf("expected %s risk in unfiltered list, got %#v", kind, all.Items)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/security/summary", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("security summary status = %d body = %s", w.Code, w.Body.String())
	}
	var summary struct {
		GeneratedAt time.Time        `json:"generated_at"`
		Filters     map[string]any   `json:"filters"`
		Status      string           `json:"status"`
		Total       int              `json:"total"`
		Counts      map[string]int   `json:"counts"`
		KindCounts  map[string]int   `json:"kind_counts"`
		Recent      []adminRiskEvent `json:"recent"`
	}
	if err := json.NewDecoder(w.Body).Decode(&summary); err != nil {
		t.Fatalf("decode security summary: %v", err)
	}
	if summary.GeneratedAt.IsZero() || summary.Filters == nil || summary.Status != "critical" || summary.Total == 0 || summary.Counts["high"] == 0 || summary.KindCounts["auth_failed"] == 0 || len(summary.Recent) == 0 {
		t.Fatalf("unexpected security summary: %#v", summary)
	}
}
