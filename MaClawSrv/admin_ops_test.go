package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
		{method: http.MethodPost, path: "/api/v1/admin/sandbox/detect", want: http.StatusOK},
		{method: http.MethodGet, path: "/api/v1/admin/sandbox/install-plan?backend=bwrap", want: http.StatusOK},
		{method: http.MethodPost, path: "/api/v1/admin/sandbox/diagnose", body: `{"write_report":true}`, want: 0},
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
}
