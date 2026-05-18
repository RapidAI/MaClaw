package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

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
	t.Setenv("MACLAW_HTTP_ADDR", "127.0.0.1:18181")
	t.Setenv("MACLAW_TLS_KEY_FILE", "secret-key.pem")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
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
	foundHTTP := false
	foundSecret := false
	for _, item := range out.Items {
		switch item.Key {
		case "http_addr":
			foundHTTP = item.Configured && item.Value == "127.0.0.1:18181"
		case "tls_key_file":
			foundSecret = item.Configured && item.Sensitive && item.Value != "secret-key.pem" && item.Value != ""
		}
	}
	if !foundHTTP || !foundSecret {
		t.Fatalf("expected redacted managed environment items: %#v", out.Items)
	}
}
func TestAdminServiceConfigDiffComparesDraftToEnvironment(t *testing.T) {
	t.Setenv("MACLAW_HTTP_ADDR", "127.0.0.1:18080")
	t.Setenv("MACLAW_TLS_KEY_FILE", "old-secret.pem")
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)

	body := `{"values":{"http_addr":"127.0.0.1:19090","tls_key_file":"new-secret.pem"}}`
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
	if out.Changed != 2 || out.Total != 2 || !out.Valid.Valid {
		t.Fatalf("unexpected diff counts: %#v", out)
	}
	for _, item := range out.Items {
		if !item.Changed {
			t.Fatalf("expected changed item: %#v", item)
		}
		if item.Key == "tls_key_file" && (item.Current == "old-secret.pem" || item.Desired == "new-secret.pem") {
			t.Fatalf("expected redacted sensitive diff: %#v", item)
		}
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
