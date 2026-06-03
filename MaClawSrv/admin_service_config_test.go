package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func containsAllowedValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestAdminServiceConfigSchemaDraftAndValidate(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-config/schema", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("schema status = %d body = %s", w.Code, w.Body.String())
	}
	var schema struct {
		Items []adminServiceConfigSchemaField `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	if len(schema.Items) == 0 {
		t.Fatalf("expected schema items")
	}
	var foundSecurityPolicy bool
	for _, item := range schema.Items {
		if item.Key == "security_policy_mode" {
			foundSecurityPolicy = true
			if item.EnvKey != "MACLAW_SECURITY_POLICY_MODE" || item.Type != "enum" || !containsAllowedValue(item.AllowedValues, "developer") {
				t.Fatalf("unexpected security policy schema field: %#v", item)
			}
		}
	}
	if !foundSecurityPolicy {
		t.Fatalf("schema missing security_policy_mode")
	}

	body := `{"values":{"http_addr":"127.0.0.1:19090","allow_insecure_http":false,"admin_web_default_locale":"en-US","sandbox_mode":"bwrap","sandbox_install_policy":"suggest","sandbox_report_retention":30,"sandbox_startup_diagnose":true},"reason":"prepare admin settings"}`
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update draft status = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Draft      adminServiceConfigDraft            `json:"draft"`
		Validation adminServiceConfigValidationResult `json:"validation"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode draft update: %v", err)
	}
	if !out.Validation.Valid || !out.Validation.RestartRequired || out.Draft.Values["http_addr"] != "127.0.0.1:19090" {
		t.Fatalf("unexpected draft update: %#v", out)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-config/validate", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("validate draft status = %d body = %s", w.Code, w.Body.String())
	}
}

func TestAdminServiceConfigRejectsReadOnlyAndInvalidValues(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	body := `{"values":{"admin_secret":"should-not-write","sandbox_mode":"docker"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid draft status = %d body = %s", w.Code, w.Body.String())
	}
	var result adminServiceConfigValidationResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode invalid result: %v", err)
	}
	if result.Valid || len(result.Errors) < 2 {
		t.Fatalf("expected validation errors: %#v", result)
	}
}

func TestAdminServiceConfigExportPlanUsesDraft(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	body := `{"values":{"http_addr":"127.0.0.1:19090","allow_insecure_http":false,"enable_scheduler":true,"admin_web_default_locale":"zh-CN"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update draft status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-config/export-plan", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export plan status = %d body = %s", w.Code, w.Body.String())
	}
	var plan adminServiceConfigExportPlan
	if err := json.NewDecoder(w.Body).Decode(&plan); err != nil {
		t.Fatalf("decode export plan: %v", err)
	}
	if plan.WillExecute || !plan.RequiresManualApply || !plan.RestartRequired {
		t.Fatalf("unexpected execution flags: %#v", plan)
	}
	if !containsString(plan.DotEnvContent, "MACLAW_HTTP_ADDR=127.0.0.1:19090") || !containsString(plan.SystemdDropInContent, "Environment=\"MACLAW_ENABLE_SCHEDULER=true\"") {
		t.Fatalf("unexpected export content dotenv=%q systemd=%q", plan.DotEnvContent, plan.SystemdDropInContent)
	}
}

func TestAdminServiceConfigSandboxInstallPolicyWarning(t *testing.T) {
	body := map[string]any{"sandbox_install_policy": "run"}
	out := validateAdminServiceConfigValues(body)
	if !out.Valid || len(out.Warnings) == 0 {
		t.Fatalf("expected valid config with warning for install run policy: %#v", out)
	}
	if out.Normalized["sandbox_install_policy"] != "run" {
		t.Fatalf("unexpected normalized install policy: %#v", out.Normalized)
	}

	bad := validateAdminServiceConfigValues(map[string]any{"sandbox_install_policy": "always"})
	if bad.Valid {
		t.Fatalf("expected invalid sandbox install policy")
	}
}

func TestAdminServiceConfigSandboxReportRetention(t *testing.T) {
	out := validateAdminServiceConfigValues(map[string]any{"sandbox_report_retention": float64(30)})
	if !out.Valid || out.Normalized["sandbox_report_retention"] != 30 {
		t.Fatalf("unexpected valid retention config: %#v", out)
	}
	bad := validateAdminServiceConfigValues(map[string]any{"sandbox_report_retention": float64(0)})
	if bad.Valid {
		t.Fatalf("expected invalid retention below range")
	}
}
func TestAdminServiceConfigEnvironmentIsRedacted(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("MACLAW_HTTP_ADDR", "127.0.0.1:18181")
	t.Setenv("MACLAW_TLS_KEY_FILE", filepath.Join(dataRoot, "secret-key.pem"))
	t.Setenv("MACLAW_TLS_CERT_FILE", filepath.Join(dataRoot, "certs", "server.pem"))
	t.Setenv("MACLAW_LOG_FILE", filepath.Join(dataRoot, "logs", "service.log"))
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-config/environment", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("environment status = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Items      []adminServiceConfigEnvironmentItem `json:"items"`
		Configured int                                 `json:"configured"`
		Total      int                                 `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode environment: %v", err)
	}
	if out.Configured < 2 || out.Total == 0 {
		t.Fatalf("unexpected environment counts: %#v", out)
	}
	if containsString(w.Body.String(), dataRoot) || containsString(w.Body.String(), filepath.ToSlash(dataRoot)) || containsString(w.Body.String(), "secret-key.pem") {
		t.Fatalf("environment leaked local path or key path: %s", w.Body.String())
	}
	foundHTTP := false
	foundSecret := false
	foundCert := false
	foundLog := false
	for _, item := range out.Items {
		switch item.Key {
		case "http_addr":
			foundHTTP = item.Configured && item.Value == "127.0.0.1:18181"
		case "tls_key_file":
			foundSecret = item.Configured && item.Sensitive && item.Value != filepath.Join(dataRoot, "secret-key.pem") && item.Value != ""
		case "tls_cert_file":
			foundCert = item.Configured && item.Value == filepath.Join(filepath.Base(dataRoot), "certs", "server.pem")
		case "log_file":
			foundLog = item.Configured && item.Value == filepath.Join(filepath.Base(dataRoot), "logs", "service.log")
		}
	}
	if !foundHTTP || !foundSecret || !foundCert || !foundLog {
		t.Fatalf("expected redacted managed environment items: %#v", out.Items)
	}
}

func TestAdminServiceConfigEffectiveRedactsLocalPaths(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("MACLAW_TLS_CERT_FILE", filepath.Join(dataRoot, "certs", "server.pem"))
	t.Setenv("MACLAW_TLS_KEY_FILE", filepath.Join(dataRoot, "certs", "server-key.pem"))
	t.Setenv("MACLAW_LOG_FILE", filepath.Join(dataRoot, "logs", "service.log"))
	t.Setenv("MACLAW_ENABLE_SCHEDULER", "true")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-config/effective", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("effective status = %d body = %s", w.Code, w.Body.String())
	}
	if containsString(w.Body.String(), dataRoot) || containsString(w.Body.String(), filepath.ToSlash(dataRoot)) || containsString(w.Body.String(), "server-key.pem") {
		t.Fatalf("effective config leaked local path or key path: %s", w.Body.String())
	}
	var out struct {
		Fields map[string]adminConfigField `json:"fields"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode effective: %v", err)
	}
	if out.Fields["data_root"].Value != filepath.Base(dataRoot) || out.Fields["tls_cert_file"].Value != filepath.Join(filepath.Base(dataRoot), "certs", "server.pem") || out.Fields["log_file"].Value != filepath.Join(filepath.Base(dataRoot), "logs", "service.log") {
		t.Fatalf("expected redacted path basenames: %#v", out.Fields)
	}
	if out.Fields["tls_key_file"].Value != true || !out.Fields["tls_key_file"].Sensitive {
		t.Fatalf("expected sensitive key field to be boolean configured flag: %#v", out.Fields["tls_key_file"])
	}
	if _, ok := out.Fields["enable_scheduler"]; !ok {
		t.Fatalf("expected enable_scheduler in effective config fields: %#v", out.Fields)
	}
}

func TestRedactSupportBundleValueHandlesCrossPlatformAbsolutePaths(t *testing.T) {
	cases := map[string]string{
		`C:\Users\alice\AppData\maclaw\secret.pem`: "secret.pem",
		`D:/workprj/aicoder/data/logs/service.log`: "service.log",
		`/var/lib/maclawsrv/certs/server.pem`:      "server.pem",
	}
	for in, want := range cases {
		if got := redactSupportBundleValue("", in); got != want {
			t.Fatalf("redactSupportBundleValue(%q) = %q, want %q", in, got, want)
		}
	}
	dataRoot := `D:\workprj\aicoder\data`
	if got := redactSupportBundleValue(dataRoot, `D:/workprj/aicoder/data/certs/server.pem`); got != `data/certs/server.pem` {
		t.Fatalf("expected data-root replacement for slash variant, got %q", got)
	}
}

func TestAdminServiceConfigDiffComparesDraftToEnvironment(t *testing.T) {
	dataRoot := t.TempDir()
	t.Setenv("MACLAW_HTTP_ADDR", "127.0.0.1:18080")
	t.Setenv("MACLAW_TLS_KEY_FILE", filepath.Join(dataRoot, "old-secret.pem"))
	t.Setenv("MACLAW_TLS_CERT_FILE", filepath.Join(dataRoot, "certs", "old.pem"))
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	desiredCert := filepath.Join(dataRoot, "certs", "new.pem")
	payload, err := json.Marshal(adminServiceConfigDraftRequest{Values: map[string]any{"http_addr": "127.0.0.1:19090", "tls_key_file": filepath.Join(dataRoot, "new-secret.pem"), "tls_cert_file": desiredCert}})
	if err != nil {
		t.Fatalf("marshal draft request: %v", err)
	}
	body := string(payload)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update draft status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-config/diff", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("diff status = %d body = %s", w.Code, w.Body.String())
	}
	var out struct {
		Items   []adminServiceConfigDiffItem       `json:"items"`
		Changed int                                `json:"changed"`
		Total   int                                `json:"total"`
		Valid   adminServiceConfigValidationResult `json:"validation"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode diff: %v", err)
	}
	if out.Changed != 3 || out.Total != 3 || !out.Valid.Valid {
		t.Fatalf("unexpected diff counts: %#v", out)
	}
	if containsString(w.Body.String(), dataRoot) || containsString(w.Body.String(), filepath.ToSlash(dataRoot)) || containsString(w.Body.String(), "old-secret.pem") || containsString(w.Body.String(), "new-secret.pem") {
		t.Fatalf("diff leaked local path or key path: %s", w.Body.String())
	}
	for _, item := range out.Items {
		if !item.Changed {
			t.Fatalf("expected changed item: %#v", item)
		}
		if item.Key == "tls_key_file" && (item.Current == filepath.Join(dataRoot, "old-secret.pem") || item.Desired == filepath.Join(dataRoot, "new-secret.pem")) {
			t.Fatalf("expected redacted sensitive diff: %#v", item)
		}
		if item.Key == "tls_cert_file" && (item.Current != filepath.Join(filepath.Base(dataRoot), "certs", "old.pem") || item.Desired != filepath.Join(filepath.Base(dataRoot), "certs", "new.pem")) {
			t.Fatalf("expected redacted path diff: %#v", item)
		}
	}
}

func TestAdminServiceConfigDraftAndValidateRedactSensitiveValues(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	certPath := filepath.Join(dataRoot, "certs", "server.pem")
	bodyBytes, err := json.Marshal(adminServiceConfigDraftRequest{Values: map[string]any{"tls_key_file": filepath.Join(dataRoot, "private-key.pem"), "tls_cert_file": certPath}})
	if err != nil {
		t.Fatalf("marshal draft request: %v", err)
	}
	body := string(bodyBytes)
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update draft status = %d body = %s", w.Code, w.Body.String())
	}
	if containsString(w.Body.String(), dataRoot) || containsString(w.Body.String(), filepath.ToSlash(dataRoot)) || containsString(w.Body.String(), "private-key.pem") {
		t.Fatalf("sensitive draft value leaked: %s", w.Body.String())
	}
	var updated struct {
		Draft      adminServiceConfigDraft            `json:"draft"`
		Validation adminServiceConfigValidationResult `json:"validation"`
	}
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode draft update: %v", err)
	}
	if updated.Draft.Values["tls_key_file"] != "[redacted]" || updated.Validation.Normalized["tls_key_file"] != "[redacted]" {
		t.Fatalf("expected redacted sensitive response: %#v", updated)
	}
	if updated.Draft.Values["tls_cert_file"] != filepath.Join(filepath.Base(dataRoot), "certs", "server.pem") || updated.Validation.Normalized["tls_cert_file"] != filepath.Join(filepath.Base(dataRoot), "certs", "server.pem") {
		t.Fatalf("expected path values to be redacted: %#v", updated)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-config/validate", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("validate status = %d body = %s", w.Code, w.Body.String())
	}
	if containsString(w.Body.String(), dataRoot) || containsString(w.Body.String(), filepath.ToSlash(dataRoot)) || containsString(w.Body.String(), "private-key.pem") {
		t.Fatalf("sensitive validate value leaked: %s", w.Body.String())
	}
}

func TestAdminServiceConfigExportPlanRedactsSensitiveResponse(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	bodyBytes, err := json.Marshal(adminServiceConfigDraftRequest{Values: map[string]any{"tls_key_file": filepath.Join(dataRoot, "private-key.pem"), "tls_cert_file": filepath.Join(dataRoot, "certs", "server.pem")}})
	if err != nil {
		t.Fatalf("marshal export request: %v", err)
	}
	body := string(bodyBytes)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-config/export-plan", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export plan status = %d body = %s", w.Code, w.Body.String())
	}
	bodyText := w.Body.String()
	if containsString(bodyText, dataRoot) || containsString(bodyText, filepath.ToSlash(dataRoot)) || containsString(bodyText, strings.ReplaceAll(dataRoot, `\`, `\\`)) || containsString(bodyText, "private-key.pem") {
		t.Fatalf("sensitive export plan value leaked: %s", bodyText)
	}
	var plan adminServiceConfigExportPlan
	if err := json.NewDecoder(bytes.NewBufferString(bodyText)).Decode(&plan); err != nil {
		t.Fatalf("decode export plan: %v", err)
	}
	if !containsString(plan.DotEnvContent, "MACLAW_TLS_KEY_FILE=<redacted>") || !containsString(plan.SystemdDropInContent, "MACLAW_TLS_KEY_FILE=<redacted>") {
		t.Fatalf("expected redacted manual apply content: dotenv=%q systemd=%q", plan.DotEnvContent, plan.SystemdDropInContent)
	}
	if containsString(plan.DotEnvContent, dataRoot) || containsString(plan.DotEnvContent, strings.ReplaceAll(dataRoot, `\`, `\\`)) || !containsString(plan.DotEnvContent, dotEnvQuote(filepath.Join(filepath.Base(dataRoot), "certs", "server.pem"))) {
		t.Fatalf("expected manual apply content to redact local path: %q", plan.DotEnvContent)
	}
	if plan.Validation.Normalized["tls_cert_file"] != filepath.Join(filepath.Base(dataRoot), "certs", "server.pem") {
		t.Fatalf("expected export validation path to be redacted: %#v", plan.Validation.Normalized)
	}
	foundCert := false
	for _, item := range plan.Env {
		if item.Key == "tls_cert_file" {
			foundCert = item.Value == filepath.Join(filepath.Base(dataRoot), "certs", "server.pem")
		}
	}
	if !foundCert {
		t.Fatalf("expected exported env plan path to be redacted: %#v", plan.Env)
	}
}

func TestAdminServiceConfigRejectsRedactedSensitivePlaceholder(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	placeholderBody := `{"values":{"tls_key_file":"[redacted]"}}`
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPatch, "/api/v1/admin/service-config/draft"},
		{http.MethodPost, "/api/v1/admin/service-config/validate"},
		{http.MethodPost, "/api/v1/admin/service-config/export-plan"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(placeholderBody))
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s %s status = %d body = %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if !containsString(w.Body.String(), "without an existing draft value") {
			t.Fatalf("expected redacted placeholder error, got %s", w.Body.String())
		}
	}

	seedCert := filepath.Join(dataRoot, "certs", "server.pem")
	seedBody, err := json.Marshal(adminServiceConfigDraftRequest{Values: map[string]any{"tls_key_file": "/secret/private-key.pem", "tls_cert_file": seedCert}})
	if err != nil {
		t.Fatalf("marshal seed request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBuffer(seedBody))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("seed draft status = %d body = %s", w.Code, w.Body.String())
	}
	redactedCert := filepath.Join(filepath.Base(dataRoot), "certs", "server.pem")
	mergeBody, err := json.Marshal(adminServiceConfigDraftRequest{Values: map[string]any{"tls_key_file": "[redacted]", "tls_cert_file": redactedCert, "http_addr": "127.0.0.1:19090"}})
	if err != nil {
		t.Fatalf("marshal merge request: %v", err)
	}
	req = httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBuffer(mergeBody))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || containsString(w.Body.String(), "/secret/private-key.pem") {
		t.Fatalf("placeholder merge status = %d body = %s", w.Code, w.Body.String())
	}
	draft, err := loadAdminServiceConfigDraft(svc.DataRoot())
	if err != nil {
		t.Fatalf("load draft: %v", err)
	}
	if draft.Values["tls_key_file"] != "/secret/private-key.pem" || draft.Values["tls_cert_file"] != seedCert || draft.Values["http_addr"] != "127.0.0.1:19090" {
		t.Fatalf("expected placeholders to preserve existing sensitive/path values: %#v", draft.Values)
	}
}

func TestAdminServiceConfigPathPlaceholderCanPreserveEnvironmentValue(t *testing.T) {
	dataRoot := t.TempDir()
	envCert := filepath.Join(dataRoot, "certs", "env-server.pem")
	t.Setenv("MACLAW_TLS_CERT_FILE", envCert)
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	redactedCert := filepath.Join(filepath.Base(dataRoot), "certs", "env-server.pem")
	body, err := json.Marshal(adminServiceConfigDraftRequest{Values: map[string]any{"tls_cert_file": redactedCert, "http_addr": "127.0.0.1:19090"}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBuffer(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || containsString(w.Body.String(), dataRoot) || containsString(w.Body.String(), filepath.ToSlash(dataRoot)) {
		t.Fatalf("update draft status = %d body = %s", w.Code, w.Body.String())
	}
	draft, err := loadAdminServiceConfigDraft(dataRoot)
	if err != nil {
		t.Fatalf("load draft: %v", err)
	}
	if draft.Values["tls_cert_file"] != envCert || draft.Values["http_addr"] != "127.0.0.1:19090" {
		t.Fatalf("expected redacted path placeholder to preserve env value: %#v", draft.Values)
	}
}

func TestAdminServiceConfigRawPathSubmissionDoesNotRequireDraft(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	rawCert := filepath.Join(dataRoot, "certs", "raw-server.pem")
	body, err := json.Marshal(adminServiceConfigDraftRequest{Values: map[string]any{"tls_cert_file": rawCert}})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-config/validate", bytes.NewBuffer(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || containsString(w.Body.String(), dataRoot) || containsString(w.Body.String(), filepath.ToSlash(dataRoot)) {
		t.Fatalf("validate raw path status = %d body = %s", w.Code, w.Body.String())
	}
	if _, err := loadAdminServiceConfigDraft(dataRoot); err != nil {
		t.Fatalf("raw path validation should not require a draft: %v", err)
	}
}

func TestAdminServiceConfigDraftReasonIsRedacted(t *testing.T) {
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	reason := `rotate token=abc123 from ` + filepath.Join(dataRoot, "private", "tls.key")
	body, err := json.Marshal(adminServiceConfigDraftRequest{Values: map[string]any{"http_addr": "127.0.0.1:19090"}, Reason: reason})
	if err != nil {
		t.Fatalf("marshal draft request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBuffer(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update draft status = %d body = %s", w.Code, w.Body.String())
	}
	if containsString(w.Body.String(), "abc123") || containsString(w.Body.String(), dataRoot) || containsString(w.Body.String(), filepath.ToSlash(dataRoot)) {
		t.Fatalf("draft response leaked reason details: %s", w.Body.String())
	}
	draft, err := loadAdminServiceConfigDraft(dataRoot)
	if err != nil {
		t.Fatalf("load draft: %v", err)
	}
	if containsString(draft.Reason, "abc123") || containsString(draft.Reason, dataRoot) || containsString(draft.Reason, filepath.ToSlash(dataRoot)) {
		t.Fatalf("draft persisted unredacted reason: %q", draft.Reason)
	}
	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.service_config_draft_updated"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) == 0 || containsString(events[0].Metadata["reason"], "abc123") || containsString(events[0].Metadata["reason"], dataRoot) {
		t.Fatalf("audit metadata leaked reason: %#v", events)
	}
}

func TestAdminServiceConfigDraftCanBeCleared(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	body := `{"values":{"http_addr":"127.0.0.1:19090","sandbox_mode":"bwrap"},"reason":"temporary debug"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/service-config/draft", bytes.NewBufferString(body))
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update draft status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-config/draft", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("clear without confirm status = %d body = %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/service-config/draft?confirm=true", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("clear draft status = %d body = %s", w.Code, w.Body.String())
	}
	var cleared struct {
		Status     string                             `json:"status"`
		Draft      adminServiceConfigDraft            `json:"draft"`
		Validation adminServiceConfigValidationResult `json:"validation"`
	}
	if err := json.NewDecoder(w.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode clear draft: %v", err)
	}
	if cleared.Status != "cleared" || len(cleared.Draft.Values) != 0 || !cleared.Validation.Valid {
		t.Fatalf("unexpected clear response: %#v", cleared)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/service-config/draft", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get draft status = %d body = %s", w.Code, w.Body.String())
	}
	var current struct {
		Draft adminServiceConfigDraft `json:"draft"`
	}
	if err := json.NewDecoder(w.Body).Decode(&current); err != nil {
		t.Fatalf("decode current draft: %v", err)
	}
	if len(current.Draft.Values) != 0 {
		t.Fatalf("expected empty draft after clear: %#v", current.Draft.Values)
	}

	events, err := svc.ListAuditEvents(context.Background(), agentservice.ListAuditEventsInput{Action: "admin.service_config_draft_cleared"})
	if err != nil {
		t.Fatalf("ListAuditEvents draft clear: %v", err)
	}
	if len(events) == 0 || events[0].ResourceType != "service_config" || events[0].ResourceID != "draft" {
		t.Fatalf("expected draft clear audit event, got %#v", events)
	}
}
