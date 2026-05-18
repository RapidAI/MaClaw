package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestAdminLogSourcesAndRead(t *testing.T) {
	dataRoot := t.TempDir()
	logPath := filepath.Join(dataRoot, "logs", "maclaw_srv.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	content := "2026-01-01 info service ready\n2026-01-01 ERROR failed token=secret-value\n2026-01-01 warn slow path\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	t.Setenv("MACLAW_LOG_FILE", logPath)
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/sources", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("log sources status = %d body = %s", w.Code, w.Body.String())
	}
	var sources struct {
		Items []adminLogSource `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&sources); err != nil {
		t.Fatalf("decode sources: %v", err)
	}
	foundService := false
	for _, item := range sources.Items {
		if item.ID == "service" && item.Exists {
			foundService = true
		}
	}
	if !foundService {
		t.Fatalf("unexpected sources: %#v", sources)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/service/tail?tail=10&level=error", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("log read status = %d body = %s", w.Code, w.Body.String())
	}
	var out adminLogReadResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode log read: %v", err)
	}
	if len(out.Lines) != 1 || out.Lines[0].Level != "error" || out.Lines[0].Text == "" {
		t.Fatalf("unexpected log lines: %#v", out)
	}
	if out.Lines[0].Text == "2026-01-01 ERROR failed token=secret-value" || !containsString(out.Lines[0].Text, "token=<redacted>") {
		t.Fatalf("expected redacted token, got %q", out.Lines[0].Text)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/service?tail=10&q=slow", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query log read status = %d body = %s", w.Code, w.Body.String())
	}
	var queryOut adminLogReadResponse
	if err := json.NewDecoder(w.Body).Decode(&queryOut); err != nil {
		t.Fatalf("decode query log read: %v", err)
	}
	if len(queryOut.Lines) != 1 || queryOut.Lines[0].Level != "warn" || !containsString(queryOut.Lines[0].Text, "slow path") {
		t.Fatalf("unexpected query log lines: %#v", queryOut)
	}
}

func TestAdminLogReadMissingSourceIsBounded(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/not_a_source", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("unsafe source status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminLogReadValidationCapAndRedaction(t *testing.T) {
	dataRoot := t.TempDir()
	logPath := filepath.Join(dataRoot, "logs", "maclaw_srv.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	content := strings.Join([]string{
		"info boot",
		"warn api_secret=secret-value authorization: Bearer bearer-token",
		"error password:secret-value apikey=key-value",
	}, "\n") + "\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	t.Setenv("MACLAW_LOG_FILE", logPath)
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	for _, path := range []string{"/api/v1/admin/logs/service?tail=0", "/api/v1/admin/logs/service?tail=abc", "/api/v1/admin/logs/service?level=debug"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d body = %s", path, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/service?tail=9999", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("capped log read status = %d body = %s", w.Code, w.Body.String())
	}
	var out adminLogReadResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode capped log read: %v", err)
	}
	if out.Tail != maxAdminLogTailLines {
		t.Fatalf("expected tail cap %d, got %d", maxAdminLogTailLines, out.Tail)
	}
	joined := ""
	for _, line := range out.Lines {
		joined += line.Text + "\n"
	}
	for _, secret := range []string{"secret-value", "bearer-token", "key-value"} {
		if strings.Contains(joined, secret) {
			t.Fatalf("expected redacted %q in logs, got %q", secret, joined)
		}
	}
	if !strings.Contains(joined, "api_secret=<redacted>") || !strings.Contains(joined, "authorization:<redacted>") || !strings.Contains(joined, "apikey=<redacted>") {
		t.Fatalf("expected redaction markers, got %q", joined)
	}
}

func TestRedactLogLineCoversAuthorizationVariants(t *testing.T) {
	cases := []string{
		`warn Authorization: Bearer abc.def token=plain-secret {"api_key":"json-secret"}`,
		`warn authorization=Bearer secret-token api_secret=secret-value`,
		`warn auth: Bearer other-token password:secret-value`,
	}
	for _, tc := range cases {
		got := redactLogLine(tc)
		for _, secret := range []string{"abc.def", "plain-secret", "json-secret", "secret-token", "secret-value", "other-token"} {
			if strings.Contains(got, secret) {
				t.Fatalf("expected %q to be redacted from %q, got %q", secret, tc, got)
			}
		}
		if !strings.Contains(got, "<redacted>") {
			t.Fatalf("expected redaction marker for %q, got %q", tc, got)
		}
	}
}

func TestAdminRecentLogErrorsAcrossSources(t *testing.T) {
	dataRoot := t.TempDir()
	serviceLog := filepath.Join(dataRoot, "logs", "maclaw_srv.log")
	schedulerLog := filepath.Join(dataRoot, "logs", "scheduler.log")
	if err := os.MkdirAll(filepath.Dir(serviceLog), 0o700); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(serviceLog, []byte("info ok\nerror failed token=service-secret\n"), 0o600); err != nil {
		t.Fatalf("write service log: %v", err)
	}
	if err := os.WriteFile(schedulerLog, []byte("warn slow password=scheduler-secret\n"), 0o600); err != nil {
		t.Fatalf("write scheduler log: %v", err)
	}
	t.Setenv("MACLAW_LOG_FILE", serviceLog)
	t.Setenv("MACLAW_SCHEDULER_LOG_FILE", schedulerLog)
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/errors/recent?limit=10", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("recent errors status = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Items []adminRecentLogLine `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode recent errors: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].SourceID != "service" || out.Items[0].Line.Level != "error" {
		t.Fatalf("unexpected recent errors: %#v", out)
	}
	if strings.Contains(out.Items[0].Line.Text, "service-secret") {
		t.Fatalf("expected redacted recent error: %#v", out.Items[0])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/errors/recent?limit=10&include_warn=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("recent errors with warnings status = %d body = %s", w.Code, w.Body.String())
	}
	out.Items = nil
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode recent errors with warnings: %v", err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("expected error and warning, got %#v", out)
	}
	for _, item := range out.Items {
		if strings.Contains(item.Line.Text, "scheduler-secret") || strings.Contains(item.Line.Text, "service-secret") {
			t.Fatalf("expected redacted recent item: %#v", item)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/errors/recent?limit=0", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid recent limit status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminLogDownloadIsRedactedText(t *testing.T) {
	dataRoot := t.TempDir()
	logPath := filepath.Join(dataRoot, "logs", "maclaw_srv.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("info ready\nerror failed token=download-secret\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	t.Setenv("MACLAW_LOG_FILE", logPath)
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs/service/download?tail=10&level=error", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("log download status = %d body = %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "text/plain") {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "maclawsrv-service-log.txt") {
		t.Fatalf("Content-Disposition = %q", got)
	}
	body := w.Body.String()
	if !strings.Contains(body, "token=<redacted>") || strings.Contains(body, "download-secret") {
		t.Fatalf("expected redacted download body, got %q", body)
	}
}

func TestAdminLogRotateRequiresConfirmAndCreatesFreshFile(t *testing.T) {
	dataRoot := t.TempDir()
	logPath := filepath.Join(dataRoot, "logs", "maclaw_srv.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(logPath, []byte("info ready\nerror failed token=rotate-secret\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	t.Setenv("MACLAW_LOG_FILE", logPath)
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/logs/service/rotate", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("rotate without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/logs/service/rotate?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate with confirm status = %d body = %s", w.Code, w.Body.String())
	}
	var out adminLogRotateResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode rotate: %v", err)
	}
	if !out.Rotated || !out.CreatedNew || out.RotatedTo == "" {
		t.Fatalf("unexpected rotate response: %#v", out)
	}
	fresh, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read fresh log: %v", err)
	}
	if len(fresh) != 0 {
		t.Fatalf("expected fresh empty log, got %q", string(fresh))
	}
	rotated, err := os.ReadFile(out.RotatedTo)
	if err != nil {
		t.Fatalf("read rotated log: %v", err)
	}
	if !strings.Contains(string(rotated), "rotate-secret") {
		t.Fatalf("expected original content in rotated log, got %q", string(rotated))
	}
}
func TestAdminLogSearchAcrossSources(t *testing.T) {
	dataRoot := t.TempDir()
	serviceLog := filepath.Join(dataRoot, "logs", "maclaw_srv.log")
	schedulerLog := filepath.Join(dataRoot, "logs", "scheduler.log")
	if err := os.MkdirAll(filepath.Dir(serviceLog), 0o700); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.WriteFile(serviceLog, []byte("info ready\nerror database token=service-secret\n"), 0o600); err != nil {
		t.Fatalf("write service log: %v", err)
	}
	if err := os.WriteFile(schedulerLog, []byte("warn database slow password=scheduler-secret\ninfo done\n"), 0o600); err != nil {
		t.Fatalf("write scheduler log: %v", err)
	}
	t.Setenv("MACLAW_LOG_FILE", serviceLog)
	t.Setenv("MACLAW_SCHEDULER_LOG_FILE", schedulerLog)
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/logs/search", strings.NewReader(`{"q":"database","limit":10}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("log search status = %d body = %s", w.Code, w.Body.String())
	}
	var out adminLogSearchResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode log search: %v", err)
	}
	if len(out.Items) != 2 || out.Query != "database" || out.Limit != 10 {
		t.Fatalf("unexpected log search: %#v", out)
	}
	for _, item := range out.Items {
		if strings.Contains(item.Line.Text, "service-secret") || strings.Contains(item.Line.Text, "scheduler-secret") {
			t.Fatalf("expected redacted search item: %#v", item)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/logs/search", strings.NewReader(`{"sources":["service"],"level":"error","limit":10}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("log source search status = %d body = %s", w.Code, w.Body.String())
	}
	out = adminLogSearchResponse{}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode source log search: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].SourceID != "service" || out.Items[0].Line.Level != "error" {
		t.Fatalf("unexpected source log search: %#v", out)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/logs/search", strings.NewReader(`{"sources":["missing"],"q":"database"}`))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing source search status = %d body = %s", w.Code, w.Body.String())
	}
}

func containsString(s, sub string) bool { return strings.Contains(s, sub) }
