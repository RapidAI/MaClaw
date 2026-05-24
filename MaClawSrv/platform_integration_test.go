package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestPlatformVirtualEmployeeProvisionCreatesRuntimeInstance(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)

	tenant, user, inst := platformRuntimeForTest(t, svc, "emp-001")
	if tenant.ID == "" || user.ID == "" || inst.ID == "" {
		t.Fatalf("missing runtime binding: tenant=%#v user=%#v instance=%#v", tenant, user, inst)
	}
	if inst.Metadata["ve_employee_id"] != "emp-001" || inst.Metadata["llm_service_group_id"] != "group-legal" {
		t.Fatalf("unexpected instance metadata: %#v", inst.Metadata)
	}
	cfg, err := svc.GetUserConfig(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if cfg.AppConfig.MaclawLLMCurrentProvider != "hub-llm" || len(cfg.AppConfig.MaclawLLMProviders) != 1 || cfg.AppConfig.MaclawLLMProviders[0].URL != "https://hub.example.test/llm" || cfg.AppConfig.MaclawLLMProviders[0].Key == "" || cfg.AppConfig.MaclawLLMProviders[0].Key == "managed-by-hub" || cfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("expected Hub LLM provider config, got %#v", cfg.AppConfig)
	}
}

func TestPlatformVirtualEmployeeProvisionAllowsMissingHubLLMKeyAsAttention(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-missing-key",
		"tenant_id":            "hub-tenant-missing-key",
		"platform_tenant_id":   "tenant-missing-key",
		"name":                 "Missing Key Employee",
		"virtual_email":        "missing_key@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"llm_service_group_id": "group-missing-key",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"attention"`) {
		t.Fatalf("provision missing key status=%d body=%s", w.Code, w.Body.String())
	}
	_, _, inst := platformRuntimeForTest(t, svc, "emp-missing-key")
	if inst.Readiness.Ready || inst.Readiness.ConfigValid {
		t.Fatalf("missing key instance should exist but need config attention: %#v", inst.Readiness)
	}
}

func TestPlatformVirtualEmployeeProvisionUsesAutoModelForHubLLM(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)

	tenant, user, _ := platformRuntimeForTest(t, svc, "emp-001")
	cfg, err := svc.GetUserConfig(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if cfg.AppConfig.MaclawLLMModel != "auto" {
		t.Fatalf("Hub LLM model should be auto, got %#v", cfg.AppConfig.MaclawLLMModel)
	}
	if len(cfg.AppConfig.MaclawLLMProviders) != 1 || cfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("Hub provider model should be auto, got %#v", cfg.AppConfig.MaclawLLMProviders)
	}
}

func TestPlatformLLMModelDoesNotTreatServiceGroupAsModel(t *testing.T) {
	if got := platformLLMModelFromRequest(platformVirtualEmployeeRequest{DefaultLLM: "group-legal", LLMServiceGroupID: "group-legal", HubLLMEndpoint: "https://hub.example/api/llm/v1"}); got != "auto" {
		t.Fatalf("Hub service group must not become virtual employee model, got %q", got)
	}
	if got := platformLLMModelFromRequest(platformVirtualEmployeeRequest{LLMServiceGroupID: "group-legal"}); got != "auto" {
		t.Fatalf("service group without explicit model must not become virtual employee model, got %q", got)
	}
	if got := platformSourceUserLLMModelFromRequest(platformSourceUserRequest{DefaultLLM: "group-display", LLMServiceGroupID: "group-display", HubLLMEndpoint: "https://hub.example/api/llm/v1"}); got != "auto" {
		t.Fatalf("Hub service group must not become source-user model, got %q", got)
	}
	if got := platformSourceUserLLMModelFromRequest(platformSourceUserRequest{LLMModel: "gpt-local", LLMServiceGroupID: "group-display"}); got != "gpt-local" {
		t.Fatalf("explicit model should win, got %q", got)
	}
}

func TestPlatformVirtualEmployeeSourceUserRepairsLegacyServiceGroupModel(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	tenant, user, _ := platformRuntimeForTest(t, svc, "emp-001")
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	cfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	legacy := cfg.AppConfig
	legacy.MaclawLLMModel = "group-legal"
	for i := range legacy.MaclawLLMProviders {
		legacy.MaclawLLMProviders[i].Model = "group-legal"
	}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, legacy); err != nil {
		t.Fatalf("UpdateUserConfig legacy: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/emp-001/runtime-status?tenant_id=tenant-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", w.Code, w.Body.String())
	}
	cfg, err = svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig repaired: %v", err)
	}
	if cfg.AppConfig.MaclawLLMModel != "auto" {
		t.Fatalf("legacy service-group model should be repaired to auto, got %#v", cfg.AppConfig.MaclawLLMModel)
	}
	if len(cfg.AppConfig.MaclawLLMProviders) != 1 || cfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("legacy provider service-group model should be repaired to auto, got %#v", cfg.AppConfig.MaclawLLMProviders)
	}
}

func TestPlatformVirtualEmployeeSourceUserReusesProvisionedRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, provisionedInstance := platformRuntimeForTest(t, svc, "emp-001")

	payload := map[string]any{"tenant_id": "tenant-001", "source_user": map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Contract Reviewer", "title": "Review contract risks", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		TenantID   string `json:"tenant_id"`
		UserID     string `json:"user_id"`
		InstanceID string `json:"instance_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.TenantID != provisionedTenant.ID || out.UserID != provisionedUser.ID || out.InstanceID != provisionedInstance.ID {
		t.Fatalf("source-user launch should reuse provisioned runtime binding, got=%#v tenant=%s user=%s inst=%s", out, provisionedTenant.ID, provisionedUser.ID, provisionedInstance.ID)
	}
	users, err := svc.ListUsers(t.Context(), provisionedTenant.ID, agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected no duplicate runtime user, got %#v", users)
	}
}

func TestPlatformVirtualEmployeeSourceUserGetEndpointsReuseProvisionedRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, provisionedInstance := platformRuntimeForTest(t, svc, "emp-001")

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/emp-001/assistant-instances?tenant_id=tenant-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant instances status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		TenantID string                  `json:"tenant_id"`
		UserID   string                  `json:"user_id"`
		Items    []agentservice.Instance `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.TenantID != provisionedTenant.ID || out.UserID != provisionedUser.ID || len(out.Items) != 1 || out.Items[0].ID != provisionedInstance.ID {
		t.Fatalf("GET should reuse provisioned binding, got=%#v tenant=%s user=%s inst=%s", out, provisionedTenant.ID, provisionedUser.ID, provisionedInstance.ID)
	}
	users, err := svc.ListUsers(t.Context(), provisionedTenant.ID, agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("expected no duplicate runtime user, got %#v", users)
	}
}

func TestPlatformVirtualEmployeeSourceUserReusesLegacyProvisionedRuntimeWithoutTenantMetadata(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, provisionedInstance := platformRuntimeForTest(t, svc, "emp-001")
	legacyMetadata := map[string]string{}
	for key, value := range provisionedInstance.Metadata {
		if strings.HasPrefix(key, "ve_") && strings.Contains(key, "tenant") {
			continue
		}
		legacyMetadata[key] = value
	}
	if _, err := svc.UpdateInstance(t.Context(), agentservice.Principal{TenantID: provisionedTenant.ID, UserID: provisionedUser.ID}, provisionedInstance.ID, agentservice.UpdateInstanceInput{Metadata: legacyMetadata}); err != nil {
		t.Fatalf("UpdateInstance: %v", err)
	}

	payload := map[string]any{"tenant_id": "tenant-001", "source_user": map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Contract Reviewer", "title": "Review contract risks", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		TenantID   string `json:"tenant_id"`
		UserID     string `json:"user_id"`
		InstanceID string `json:"instance_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.TenantID != provisionedTenant.ID || out.UserID != provisionedUser.ID || out.InstanceID != provisionedInstance.ID {
		t.Fatalf("legacy source-user launch should reuse provisioned runtime binding, got=%#v tenant=%s user=%s inst=%s", out, provisionedTenant.ID, provisionedUser.ID, provisionedInstance.ID)
	}
}

func TestPlatformSourceUserLinkRefreshesHubLLMViewerToken(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	provisionedTenant, provisionedUser, _ := platformRuntimeForTest(t, svc, "emp-001")

	payload := map[string]any{
		"tenant_id":            "tenant-001",
		"hub_llm_endpoint":     "https://hub.example.test/api/llm/v1",
		"hub_llm_viewer_token": "viewer-token",
		"llm_service_group_id": "ve-service",
		"source_user":          map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Contract Reviewer", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/emp-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	cfg, err := svc.GetUserConfig(t.Context(), agentservice.Principal{TenantID: provisionedTenant.ID, UserID: provisionedUser.ID})
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if cfg.AppConfig.MaclawLLMUrl != "https://hub.example.test/api/llm/v1" || cfg.AppConfig.MaclawLLMKey == "" || cfg.AppConfig.MaclawLLMKey == "managed-by-hub" || cfg.AppConfig.MaclawLLMModel != "auto" {
		t.Fatalf("source-user LLM config not refreshed: %#v", cfg.AppConfig)
	}
	if cfg.AppConfig.MaclawLLMCurrentProvider != "hub-llm" || len(cfg.AppConfig.MaclawLLMProviders) != 1 || cfg.AppConfig.MaclawLLMProviders[0].URL != "https://hub.example.test/api/llm/v1" || cfg.AppConfig.MaclawLLMProviders[0].Key == "" || cfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("source-user LLM provider config not refreshed: %#v", cfg.AppConfig)
	}
}

func TestPlatformVirtualEmployeeSourceUserInstanceCanBeProvisionedLater(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Contract Reviewer", "title": "Review contract risks", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/emp-001/assistant-instances", map[string]any{"tenant_id": "tenant-001", "source_user": sourceUser, "name": "Early assistant"}, http.StatusCreated)
	_, _, early := platformRuntimeForTest(t, svc, "emp-001")
	if early.Metadata["ve_source_user_id"] != "emp-001" || early.Metadata["ve_employee_id"] != "emp-001" {
		t.Fatalf("early virtual source-user instance missing dual identity metadata: %#v", early.Metadata)
	}

	provisionPlatformEmployeeForTest(t, server)
	_, _, provisioned := platformRuntimeForTest(t, svc, "emp-001")
	if provisioned.ID != early.ID {
		t.Fatalf("provision should reuse early virtual source-user instance, early=%s provisioned=%s", early.ID, provisioned.ID)
	}
	if provisioned.Metadata["llm_service_group_id"] != "group-legal" || provisioned.Metadata["ve_source_user_id"] != "emp-001" {
		t.Fatalf("provision should merge metadata, got %#v", provisioned.Metadata)
	}
}

func TestPlatformDeleteVirtualEmployeeDeletesAllAssistantInstances(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	tenant, user, _ := platformRuntimeForTest(t, svc, "emp-001")
	sourceUser := map[string]any{"id": "emp-001", "external_id": "contract_reviewer", "email": "contract_reviewer@example.test", "display_name": "Contract Reviewer", "title": "Review contract risks", "account_type": "virtual_employee", "provider": "virtualemployee-platform", "is_virtual_employee": true}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/emp-001/assistant-instances", map[string]any{"tenant_id": "tenant-001", "source_user": sourceUser, "name": "Second assistant"}, http.StatusCreated)
	if _, err := svc.CreateInstance(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}, agentservice.CreateInstanceInput{Name: "Legacy local instance", Metadata: map[string]string{"legacy": "true"}}); err != nil {
		t.Fatalf("CreateInstance legacy: %v", err)
	}
	instances, err := svc.ListInstances(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 3 {
		t.Fatalf("expected provisioned, assistant, and legacy instance before delete, got %#v", instances)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		DeletedInstances   int  `json:"deleted_instances"`
		RemainingInstances int  `json:"remaining_instances"`
		UserDeleted        bool `json:"user_deleted"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if out.DeletedInstances != 3 || out.RemainingInstances != 0 || !out.UserDeleted {
		t.Fatalf("delete should remove all employee assistant instances and user, got %#v", out)
	}
	if _, err := svc.GetUser(t.Context(), tenant.ID, user.ID); err == nil {
		t.Fatal("managed user should be deleted after all employee instances are removed")
	}
}

func TestPlatformDeleteVirtualEmployeeDeletesManagedUserWhenInstanceAlreadyMissing(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	tenant, user, inst := platformRuntimeForTest(t, svc, "emp-001")
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if err := svc.DeleteInstance(t.Context(), principal, inst.ID); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	body, _ := json.Marshal(map[string]any{"employee_id": "emp-001", "tenant_id": "tenant-001", "platform_tenant_id": "tenant-001", "virtual_email": user.Email})
	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-001", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete orphan status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		UserDeleted bool `json:"user_deleted"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if !out.UserDeleted {
		t.Fatalf("managed user should be deleted when runtime instance is already missing: %#v", out)
	}
	if _, err := svc.GetUser(t.Context(), tenant.ID, user.ID); err == nil {
		t.Fatal("managed user should be deleted")
	}
}
func TestPlatformRuntimeReportAcceptsBearerAdminSecret(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/api/platform/runtime/report", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("report status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Status  string         `json:"status"`
		Summary map[string]any `json:"summary"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Status != "ok" || out.Summary == nil {
		t.Fatalf("unexpected report: %#v", out)
	}
}

func TestPlatformSourceUserAssistantLinkCreatesScopedLaunch(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{"tenant_id": "ve-tenant-a", "source_user": map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}}
	seedPlatformSourceUserConfigForTest(t, server, "/api/platform/source-users/src-001/assistant-link", payload)
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		URL         string `json:"url"`
		AccessToken string `json:"access_token"`
		TenantID    string `json:"tenant_id"`
		UserID      string `json:"user_id"`
		InstanceID  string `json:"instance_id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.URL == "" || out.TenantID == "" || out.UserID == "" || out.InstanceID == "" {
		t.Fatalf("incomplete launch response: %#v", out)
	}
	if out.AccessToken != "" {
		t.Fatalf("access_token should only be carried inside launch url")
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("launch response should be no-store, got %q", w.Header().Get("Cache-Control"))
	}
	launch, err := url.Parse(out.URL)
	if err != nil {
		t.Fatalf("parse launch URL: %v", err)
	}
	if launch.Query().Get("token") != "" || launch.Query().Get("launch_token") == "" {
		t.Fatalf("launch URL should use one-time launch_token only: %s", out.URL)
	}
	exchangeBody, _ := json.Marshal(map[string]any{"launch_token": launch.Query().Get("launch_token")})
	req = httptest.NewRequest(http.MethodPost, "/api/v1/web/exchange", bytes.NewReader(exchangeBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", w.Code, w.Body.String())
	}
	var exchanged struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(w.Body).Decode(&exchanged); err != nil {
		t.Fatalf("decode exchange: %v", err)
	}
	principal, err := svc.Authenticate(exchanged.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate exchanged token: %v", err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/web/exchange", bytes.NewReader(exchangeBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("one-time launch token should not exchange twice, status=%d body=%s", w.Code, w.Body.String())
	}
	createdEvents, err := svc.ListAuditEvents(t.Context(), agentservice.ListAuditEventsInput{Action: "web.launch_token.created", ResourceType: "web_launch_token"})
	if err != nil {
		t.Fatalf("ListAuditEvents created: %v", err)
	}
	if len(createdEvents) != 1 || createdEvents[0].TenantID != out.TenantID || createdEvents[0].UserID != out.UserID || createdEvents[0].Metadata["launch_token_hash_prefix"] == "" {
		t.Fatalf("unexpected launch token created audit: %#v", createdEvents)
	}
	exchangedEvents, err := svc.ListAuditEvents(t.Context(), agentservice.ListAuditEventsInput{Action: "web.launch_token.exchanged", ResourceType: "web_launch_token"})
	if err != nil {
		t.Fatalf("ListAuditEvents exchanged: %v", err)
	}
	if len(exchangedEvents) != 1 || exchangedEvents[0].TenantID != out.TenantID || exchangedEvents[0].UserID != out.UserID || exchangedEvents[0].Metadata["source_user_id"] != "src-001" {
		t.Fatalf("unexpected launch token exchanged audit: %#v", exchangedEvents)
	}
	rejectedEvents, err := svc.ListAuditEvents(t.Context(), agentservice.ListAuditEventsInput{Action: "web.launch_token.rejected", ResourceType: "web_launch_token"})
	if err != nil {
		t.Fatalf("ListAuditEvents rejected: %v", err)
	}
	if len(rejectedEvents) != 1 || rejectedEvents[0].Metadata["reason"] == "" || rejectedEvents[0].Metadata["launch_token_hash_prefix"] == "" {
		t.Fatalf("unexpected launch token rejected audit: %#v", rejectedEvents)
	}
	if principal.TenantID != out.TenantID || principal.UserID != out.UserID {
		t.Fatalf("token principal mismatch: %#v out=%#v", principal, out)
	}
	instances, err := svc.ListInstances(t.Context(), *principal)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 1 || instances[0].Metadata["ve_source_user_id"] != "src-001" {
		t.Fatalf("unexpected source user instances: %#v", instances)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-001/assistant-instances?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(out.InstanceID)) {
		t.Fatalf("assistant instances status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformSourceUserAssistantLinkSanitizesForwardedLaunchURL(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{"tenant_id": "ve-tenant-a", "source_user": map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}}
	seedPlatformSourceUserConfigForTest(t, server, "/api/platform/source-users/src-001/assistant-link", payload)
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("X-Forwarded-Proto", "javascript")
	req.Header.Set("X-Forwarded-Host", "evil.example\\@bad")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	launch, err := url.Parse(out.URL)
	if err != nil {
		t.Fatalf("parse launch URL: %v", err)
	}
	if launch.Scheme != "http" || launch.Host != "127.0.0.1" || launch.Query().Get("launch_token") == "" {
		t.Fatalf("forwarded launch URL was not sanitized: %s", out.URL)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "maclaw.example.test:18443")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant link with safe forwarded host status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode safe response: %v", err)
	}
	launch, err = url.Parse(out.URL)
	if err != nil {
		t.Fatalf("parse safe launch URL: %v", err)
	}
	if launch.Scheme != "https" || launch.Host != "maclaw.example.test:18443" {
		t.Fatalf("safe forwarded launch URL was not preserved: %s", out.URL)
	}
}

func TestPlatformSourceUserInstancesShareAndPreserveUserConfig(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{"tenant_id": "ve-tenant-a", "source_user": map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": payload["source_user"], "name": "One"}, http.StatusCreated)
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": payload["source_user"], "name": "Two"}, http.StatusCreated)

	tenant, user := platformSourceRuntimeUserForTest(t, svc, "src-001")
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}

	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-link", payload, http.StatusOK)
	cfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if cfg.AppConfig.MaclawLLMUrl != testLLMConfig().MaclawLLMUrl || cfg.AppConfig.MaclawLLMModel != testLLMConfig().MaclawLLMModel {
		t.Fatalf("source user launch should preserve shared user config, got %#v", cfg.AppConfig)
	}
	instances, err := svc.ListInstances(t.Context(), principal)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 2 || instances[0].UserID != instances[1].UserID {
		t.Fatalf("expected multiple instances under one shared user, got %#v", instances)
	}
}

func TestPlatformSourceUserDefaultConfigPreservesExistingNonLLMConfig(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}
	payload := map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "One"}
	body, _ := json.Marshal(payload)
	var in platformSourceUserRequest
	if err := json.Unmarshal(body, &in); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	binding, err := server.platformSourceUserBindingFromRequest(httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-instances", bytes.NewReader(body)), in)
	if err != nil {
		t.Fatalf("source binding: %v", err)
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	preserved := corelib.AppConfig{
		MCPServers:               []corelib.MCPServerEntry{{ID: "mcp-a", Name: "MCP A", EndpointURL: "https://mcp.example.test/sse", AuthType: "none", Source: corelib.MCPSourceManual}},
		MaclawAgentMaxIterations: 7,
	}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, preserved); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}

	if err := server.ensurePlatformSourceUserDefaultConfig(httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-instances", nil), principal); err != nil {
		t.Fatalf("ensure default config: %v", err)
	}
	cfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	if len(cfg.AppConfig.MCPServers) != 1 || cfg.AppConfig.MCPServers[0].Name != "MCP A" || cfg.AppConfig.MaclawAgentMaxIterations != 7 {
		t.Fatalf("source user default LLM config should preserve non-LLM config, got %#v", cfg.AppConfig)
	}
	if cfg.AppConfig.MaclawLLMUrl == "" || cfg.AppConfig.MaclawLLMKey == "" || cfg.AppConfig.MaclawLLMModel == "" {
		t.Fatalf("expected source user default LLM placeholders, got %#v", cfg.AppConfig)
	}
}
func TestPlatformSourceUserAssistantLinkRejectsUnknownInstance(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "One"}, http.StatusCreated)

	body, _ := json.Marshal(map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "instance_id": "missing-instance"})
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/src-001/assistant-link", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound || !bytes.Contains(w.Body.Bytes(), []byte("assistant instance not found")) {
		t.Fatalf("unknown instance link status=%d body=%s", w.Code, w.Body.String())
	}
	createdEvents, err := svc.ListAuditEvents(t.Context(), agentservice.ListAuditEventsInput{Action: "web.launch_token.created", ResourceType: "web_launch_token"})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(createdEvents) != 0 {
		t.Fatalf("unknown instance should not mint launch token, got %#v", createdEvents)
	}
}

func TestPlatformSourceUserRuntimeStatusSummarizesInstancesAndConfig(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "One"}, http.StatusCreated)
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "Two"}, http.StatusCreated)

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-001/runtime-status?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		SourceUserID  string `json:"source_user_id"`
		InstanceCount int    `json:"instance_count"`
		ConfigStatus  struct {
			Valid bool `json:"valid"`
		} `json:"config_status"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if out.SourceUserID != "src-001" || out.InstanceCount != 2 || !out.ConfigStatus.Valid {
		t.Fatalf("unexpected runtime status: %#v", out)
	}
}

func TestPlatformSourceUserRuntimeStatusIgnoresOtherSourceInstancesOnSameUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sourceUser := map[string]any{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sourceUser, "name": "One"}, http.StatusCreated)

	tenant, user := platformSourceRuntimeUserForTest(t, svc, "src-001")
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.CreateInstance(t.Context(), principal, agentservice.CreateInstanceInput{Name: "Other source", Metadata: map[string]string{"ve_source_user_id": "src-other", "ve_platform_tenant_id": "ve-tenant-a"}}); err != nil {
		t.Fatalf("CreateInstance other source: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-001/runtime-status?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("runtime status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		InstanceCount int `json:"instance_count"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode runtime status: %v", err)
	}
	if out.InstanceCount != 1 {
		t.Fatalf("expected only src-001 instances, got %#v", out)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/platform/source-users/src-001/assistant-instances?tenant_id=ve-tenant-a", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assistant instances=%d body=%s", w.Code, w.Body.String())
	}
	var list struct {
		Items []agentservice.Instance `json:"items"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode assistant instances: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].Metadata["ve_source_user_id"] != "src-001" {
		t.Fatalf("expected scoped assistant instances, got %#v", list.Items)
	}
}

func TestPlatformSourceUsersRuntimeStatusBatch(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	sources := []map[string]any{
		{"id": "src-001", "external_id": "real-a", "email": "real-a@example.test", "display_name": "Real A"},
		{"id": "src-002", "external_id": "real-b", "email": "real-b@example.test", "display_name": "Real B"},
	}
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-001/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sources[0], "name": "One"}, http.StatusCreated)
	postPlatformJSONForTest(t, server, "/api/platform/source-users/src-002/assistant-instances", map[string]any{"tenant_id": "ve-tenant-a", "source_user": sources[1], "name": "Two"}, http.StatusCreated)

	body, _ := json.Marshal(map[string]any{"tenant_id": "ve-tenant-a", "source_users": sources})
	req := httptest.NewRequest(http.MethodPost, "/api/platform/source-users/runtime-status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("batch runtime status=%d body=%s", w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	items, ok := out["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("unexpected batch response: %#v", out)
	}
	first, _ := items[0].(map[string]any)
	second, _ := items[1].(map[string]any)
	if first["source_user_id"] != "src-001" || second["source_user_id"] != "src-002" || first["instance_count"].(float64) != 1 || second["instance_count"].(float64) != 1 {
		t.Fatalf("unexpected batch items: %#v", items)
	}
}

func TestPlatformRuntimeReportUsesPlatformEmployeeIDs(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)

	req := httptest.NewRequest(http.MethodGet, "/api/platform/runtime/report", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("report status=%d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Users []struct {
			EmployeeID    string `json:"employee_id"`
			RuntimeUserID string `json:"runtime_user_id"`
			RuntimeStatus string `json:"runtime_status"`
			VirtualEmail  string `json:"virtual_email"`
		} `json:"users"`
		Instances []struct {
			EmployeeID    string `json:"employee_id"`
			RuntimeUserID string `json:"runtime_user_id"`
		} `json:"instances"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Users) != 1 || out.Users[0].EmployeeID != "emp-001" || out.Users[0].RuntimeUserID == "" || out.Users[0].RuntimeUserID == "emp-001" || out.Users[0].RuntimeStatus != "ready" {
		t.Fatalf("unexpected platform user report: %#v", out.Users)
	}
	if len(out.Instances) != 1 || out.Instances[0].EmployeeID != "emp-001" || out.Instances[0].RuntimeUserID != out.Users[0].RuntimeUserID {
		t.Fatalf("unexpected platform instance report: %#v", out.Instances)
	}
}

func TestPlatformVirtualEmployeeProvisionIgnoresUnknownFields(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":           "emp-extra",
		"tenant_id":             "hub-tenant-extra",
		"platform_tenant_id":    "tenant-extra",
		"name":                  "Extra Field Employee",
		"handle":                "extra_field_employee",
		"virtual_email":         "extra_field_employee@example.test",
		"skill_tags":            []string{"extra"},
		"hub_llm_endpoint":      "https://hub.example.test/llm",
		"hub_llm_api_key":       "test-hub-key",
		"llm_service_group_id":  "group-extra",
		"future_platform_field": "kept-compatible",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformVirtualEmployeeProvisionUsesTenantDisplayName(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-display",
		"tenant_id":            "hub-tenant-display",
		"platform_tenant_id":   "tenant-display",
		"tenant_name":          "Tenant Display",
		"tenant_code":          "sample-local",
		"hub_tenant_code":      "sample-hub",
		"name":                 "Display Employee",
		"handle":               "display_employee",
		"virtual_email":        "display_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].Name != "VE Platform Tenant Display (sample-hub)" {
		t.Fatalf("expected readable VE Platform tenant name, got %#v", tenants)
	}
}

func TestPlatformVirtualEmployeeProvisionRenamesLegacyTenant(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	legacy, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform hub-tenant-display", DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-display",
		"tenant_id":            "hub-tenant-display",
		"platform_tenant_id":   "tenant-display",
		"tenant_name":          "Tenant Display",
		"hub_tenant_code":      "sample-hub",
		"name":                 "Display Employee",
		"handle":               "display_employee",
		"virtual_email":        "display_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].ID != legacy.ID || tenants[0].Name != "VE Platform Tenant Display (sample-hub)" {
		t.Fatalf("expected legacy tenant to be renamed and reused, got %#v", tenants)
	}
}

func TestPlatformVirtualEmployeeProvisionRenamesTenantWhenHubNameChanges(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	legacy, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform Old Tenant (sample-hub)", DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-display",
		"tenant_id":            "hub-tenant-display",
		"platform_tenant_id":   "tenant-display",
		"tenant_name":          "New Tenant",
		"hub_tenant_code":      "sample-hub",
		"name":                 "Display Employee",
		"handle":               "display_employee",
		"virtual_email":        "display_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].ID != legacy.ID || tenants[0].Name != "VE Platform New Tenant (sample-hub)" {
		t.Fatalf("expected managed tenant to be renamed without duplicate, got %#v", tenants)
	}
}

func TestPlatformVirtualEmployeeProvisionStoresTenantIdentityMetadata(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	_, _, inst := platformRuntimeForTest(t, svc, "emp-001")
	if inst.Metadata["ve_hub_tenant_id"] != "hub-tenant-001" || inst.Metadata["ve_platform_tenant_id"] != "tenant-001" {
		t.Fatalf("missing tenant identity metadata: %#v", inst.Metadata)
	}
	if _, ok := inst.Metadata["ve_hub_tenant_code"]; ok {
		t.Fatalf("empty metadata values should be omitted: %#v", inst.Metadata)
	}
}

func TestPlatformVirtualEmployeeProvisionFallsBackToEmployeeIDEmail(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	for _, id := range []string{"emp-fallback-one", "emp fallback two"} {
		payload := map[string]any{
			"employee_id":          id,
			"tenant_id":            "hub-tenant-fallback",
			"platform_tenant_id":   "tenant-fallback",
			"tenant_name":          "Fallback Tenant",
			"name":                 "Fallback Employee",
			"hub_llm_endpoint":     "https://hub.example.test/llm",
			"hub_llm_api_key":      "test-hub-key",
			"llm_service_group_id": "group-fallback",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("provision %s status=%d body=%s", id, w.Code, w.Body.String())
		}
	}
	users, err := svc.ListUsers(t.Context(), platformRuntimeTenantIDForTest(t, svc, "hub-tenant-fallback"), agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 || users[0].Email == "@ve-platform.local" || users[1].Email == "@ve-platform.local" || users[0].Email == users[1].Email {
		t.Fatalf("expected stable distinct fallback emails, got %#v", users)
	}
}

func TestPlatformVirtualEmployeeProvisionFallbackEmailAvoidsSanitizedCollisions(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	for _, id := range []string{"emp/a", "emp a"} {
		payload := map[string]any{
			"employee_id":          id,
			"tenant_id":            "hub-tenant-collision",
			"platform_tenant_id":   "tenant-collision",
			"tenant_name":          "Collision Tenant",
			"name":                 "Collision Employee",
			"handle":               "emp a",
			"hub_llm_endpoint":     "https://hub.example.test/llm",
			"hub_llm_api_key":      "test-hub-key",
			"llm_service_group_id": "group-collision",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("provision %s status=%d body=%s", id, w.Code, w.Body.String())
		}
	}
	users, err := svc.ListUsers(t.Context(), platformRuntimeTenantIDForTest(t, svc, "hub-tenant-collision"), agentservice.ListUsersAdminInput{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 || users[0].Email == users[1].Email {
		t.Fatalf("expected collision-safe fallback emails, got %#v", users)
	}
}

func TestPlatformVirtualEmployeeProvisionRefreshesExistingRuntimeIdentity(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	tenant, user, inst := platformRuntimeForTest(t, svc, "emp-001")
	if inst.Metadata["ve_hub_tenant_code"] != "" {
		t.Fatalf("test setup expected empty hub tenant code: %#v", inst.Metadata)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	currentCfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig: %v", err)
	}
	preservedApp := currentCfg.AppConfig
	preservedApp.MCPServers = []corelib.MCPServerEntry{{ID: "mcp-a", Name: "MCP A", EndpointURL: "https://mcp.example.test/sse", AuthType: "none", Source: corelib.MCPSourceManual}}
	preservedApp.MaclawAgentMaxIterations = 9
	if _, err := svc.UpdateUserConfig(t.Context(), principal, preservedApp); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	payload := map[string]any{
		"employee_id":           "emp-001",
		"tenant_id":             "hub-tenant-001",
		"platform_tenant_id":    "tenant-001",
		"tenant_name":           "Tenant Display",
		"tenant_code":           "local-code",
		"hub_tenant_code":       "hub-code",
		"name":                  "Updated Reviewer",
		"handle":                "contract_reviewer",
		"virtual_email":         "contract_reviewer@example.test",
		"skill_description":     "Updated contract risk review",
		"hub_llm_endpoint":      "https://hub.example.test/llm",
		"hub_llm_api_key":       "test-hub-key",
		"llm_service_group_id":  "group-updated",
		"custom_runtime_marker": "ignored",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reprovision status=%d body=%s", w.Code, w.Body.String())
	}
	_, updatedUser, updatedInst := platformRuntimeForTest(t, svc, "emp-001")
	if updatedUser.ID != user.ID || updatedUser.Name != "Updated Reviewer" {
		t.Fatalf("expected managed user to be refreshed, got %#v", updatedUser)
	}
	if updatedInst.ID != inst.ID || updatedInst.Name != "Updated Reviewer" || updatedInst.Description != "Updated contract risk review" {
		t.Fatalf("expected instance profile refresh, got %#v", updatedInst)
	}
	if updatedInst.Metadata["ve_hub_tenant_code"] != "hub-code" || updatedInst.Metadata["ve_tenant_code"] != "local-code" || updatedInst.Metadata["llm_service_group_id"] != "group-updated" {
		t.Fatalf("expected refreshed metadata, got %#v", updatedInst.Metadata)
	}
	updatedCfg, err := svc.GetUserConfig(t.Context(), principal)
	if err != nil {
		t.Fatalf("GetUserConfig after reprovision: %v", err)
	}
	if len(updatedCfg.AppConfig.MCPServers) != 1 || updatedCfg.AppConfig.MCPServers[0].Name != "MCP A" || updatedCfg.AppConfig.MaclawAgentMaxIterations != 9 {
		t.Fatalf("expected non-LLM user config to be preserved, got %#v", updatedCfg.AppConfig)
	}
	if updatedCfg.AppConfig.MaclawLLMCurrentProvider != "hub-llm" || len(updatedCfg.AppConfig.MaclawLLMProviders) != 1 || updatedCfg.AppConfig.MaclawLLMProviders[0].URL != "https://hub.example.test/llm" || updatedCfg.AppConfig.MaclawLLMProviders[0].Model != "auto" {
		t.Fatalf("expected refreshed Hub LLM provider config, got %#v", updatedCfg.AppConfig)
	}
}

func TestPlatformVirtualEmployeeProvisionDoesNotMatchEmptyTenantIdentity(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	oldTenant, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform Old", DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	oldUser, err := svc.CreateUser(t.Context(), agentservice.CreateUserInput{TenantID: oldTenant.ID, Name: "Old User", Email: "old@example.test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: oldTenant.ID, UserID: oldUser.ID}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	if _, err := svc.CreateInstance(t.Context(), principal, agentservice.CreateInstanceInput{Name: "Old Instance", Metadata: map[string]string{"ve_employee_id": "old-emp"}}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-no-identity",
		"tenant_id":            "hub-tenant-new",
		"tenant_name":          "New Tenant",
		"name":                 "No Identity Employee",
		"handle":               "no_identity_employee",
		"virtual_email":        "no_identity_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("expected a new tenant instead of empty-identity match, got %#v", tenants)
	}
}

func TestPlatformVirtualEmployeeProvisionDoesNotReuseUnmanagedTenantByName(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	manual, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform 缂傚倸鍊搁崐鎼佸磹妞嬪孩濯奸柡灞诲劚绾惧鏌熼幑鎰靛殭缂佺嫏鍥ㄧ厽闁归偊鍘界紞鎴炪亜閵夈儺鍎忛棁澶愭煕韫囨洖甯跺┑顔碱槺缁辨帗娼忛妸銉х懖閻庡灚婢樼€氼喗绂掗敃鍌涘殟闁靛／鈧崑鎾淬偅閸愨斁鎷?(sample-hub)"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-display",
		"tenant_id":            "hub-tenant-display",
		"platform_tenant_id":   "tenant-display",
		"tenant_name":          "Tenant Display",
		"hub_tenant_code":      "sample-hub",
		"name":                 "Display Employee",
		"handle":               "display_employee",
		"virtual_email":        "display_employee@example.test",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-display",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	if len(tenants) != 2 {
		t.Fatalf("expected unmanaged tenant not to be reused, got %#v", tenants)
	}
	if got, err := svc.GetTenant(t.Context(), manual.ID); err != nil || got.DeleteProtected {
		t.Fatalf("manual tenant should remain unmanaged, got %#v err=%v", got, err)
	}
}

func TestPlatformVirtualEmployeeProvisionAcceptsStringSkillTags(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	payload := map[string]any{
		"employee_id":          "emp-string-tags",
		"tenant_id":            "hub-tenant-tags",
		"platform_tenant_id":   "tenant-tags",
		"name":                 "String Tags Employee",
		"handle":               "string_tags_employee",
		"virtual_email":        "string_tags_employee@example.test",
		"skill_tags":           "contract, review闂傚倸鍊搁崐鐑芥倿閿旈敮鍋撶粭娑樻噽閻瑩鏌熼悜姗嗘畷闁稿孩顨婇弻锝嗘償閳惰姤妾痳act",
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-tags",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformRuntimeFollowupEndpoints(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	paths := []string{
		"/api/platform/virtual-employees/emp-001/knowledge/imports",
		"/api/platform/virtual-employees/emp-001/migrations/imports",
		"/api/platform/sync/jobs/job-001/run",
		"/api/platform/sync/conflicts/conflict-001/resolve",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"id":"x","employee_id":"emp-001"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, req)
		if w.Code < 200 || w.Code >= 300 {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
	}
}

func TestPlatformRuntimeFollowupRejectsMissingEmployee(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees/missing/knowledge/imports", bytes.NewBufferString(`{"id":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing employee status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformVirtualEmployeeDeleteRemovesRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	bindingTenant, bindingUser, _ := platformRuntimeForTest(t, svc, "emp-001")

	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(t.Context(), bindingTenant.ID, bindingUser.ID); err == nil {
		t.Fatalf("expected runtime user to be deleted")
	}
	missing := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees/emp-001/knowledge/imports", bytes.NewBufferString(`{"id":"x"}`))
	missing.Header.Set("Content-Type", "application/json")
	missing.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, missing)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected followup missing after delete, status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPlatformVirtualEmployeeDeleteKeepsSharedRuntimeUser(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(t.Context(), agentservice.CreateTenantInput{Name: "VE Platform hub-tenant-001"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(t.Context(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Shared User", Email: "contract_reviewer@example.test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(t.Context(), principal, testLLMConfig()); err != nil {
		t.Fatalf("UpdateUserConfig: %v", err)
	}
	extra, err := svc.CreateInstance(t.Context(), principal, agentservice.CreateInstanceInput{Name: "Manual Instance"})
	if err != nil {
		t.Fatalf("CreateInstance extra: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	provisionPlatformEmployeeForTest(t, server)
	_, _, veInst := platformRuntimeForTest(t, svc, "emp-001")
	if veInst.UserID != user.ID {
		t.Fatalf("expected platform provision to reuse shared user, got %s want %s", veInst.UserID, user.ID)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/platform/virtual-employees/emp-001", nil)
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := svc.GetUser(t.Context(), tenant.ID, user.ID); err != nil {
		t.Fatalf("shared user should be kept: %v", err)
	}
	instances, err := svc.ListInstances(t.Context(), principal)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(instances) != 1 || instances[0].ID != extra.ID {
		t.Fatalf("expected only extra instance to remain: %#v", instances)
	}
}

func provisionPlatformEmployeeForTest(t *testing.T, server *HTTPServer) {
	t.Helper()
	payload := map[string]any{
		"employee_id":          "emp-001",
		"tenant_id":            "hub-tenant-001",
		"platform_tenant_id":   "tenant-001",
		"name":                 "Contract Reviewer",
		"handle":               "contract_reviewer",
		"virtual_email":        "contract_reviewer@example.test",
		"skill_description":    "Review contract risks",
		"skill_tags":           []string{"contract", "review"},
		"hub_llm_endpoint":     "https://hub.example.test/llm",
		"hub_llm_api_key":      "test-hub-key",
		"llm_service_group_id": "group-legal",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/platform/virtual-employees", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("provision status=%d body=%s", w.Code, w.Body.String())
	}
}

func postPlatformJSONForTest(t *testing.T, server *HTTPServer, path string, payload map[string]any, want int) {
	t.Helper()
	seedPlatformSourceUserConfigForTest(t, server, path, payload)
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MaClaw-Admin-Secret", "admin-secret")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != want {
		t.Fatalf("%s status=%d want=%d body=%s", path, w.Code, want, w.Body.String())
	}
}

func seedPlatformSourceUserConfigForTest(t *testing.T, server *HTTPServer, path string, payload map[string]any) {
	t.Helper()
	if !strings.Contains(path, "/api/platform/source-users/") || !strings.Contains(path, "/assistant") {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal source-user payload: %v", err)
	}
	var in platformSourceUserRequest
	if err := json.Unmarshal(body, &in); err != nil || strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.SourceUser.ID) == "" {
		return
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	binding, err := server.platformSourceUserBindingFromRequest(req, in)
	if err != nil {
		t.Fatalf("seed source-user binding: %v", err)
	}
	if _, err := server.svc.UpdateUserConfig(t.Context(), agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}, testLLMConfig()); err != nil {
		t.Fatalf("seed source-user config: %v", err)
	}
}

func platformSourceRuntimeUserForTest(t *testing.T, svc *agentservice.Service, sourceUserID string) (agentservice.Tenant, agentservice.User) {
	t.Helper()
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	for _, tenant := range tenants {
		users, err := svc.ListUsers(t.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		for _, user := range users {
			instances, err := svc.ListInstances(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
			if err != nil {
				t.Fatalf("ListInstances: %v", err)
			}
			for _, inst := range instances {
				if inst.Metadata["ve_source_user_id"] == sourceUserID {
					return tenant, user
				}
			}
		}
	}
	t.Fatalf("runtime source user %s not found", sourceUserID)
	return agentservice.Tenant{}, agentservice.User{}
}

func platformRuntimeForTest(t *testing.T, svc *agentservice.Service, employeeID string) (agentservice.Tenant, agentservice.User, agentservice.Instance) {
	t.Helper()
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	for _, tenant := range tenants {
		users, err := svc.ListUsers(t.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		for _, user := range users {
			instances, err := svc.ListInstances(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
			if err != nil {
				t.Fatalf("ListInstances: %v", err)
			}
			for _, inst := range instances {
				if inst.Metadata["ve_employee_id"] == employeeID {
					return tenant, user, inst
				}
			}
		}
	}
	t.Fatalf("runtime for employee %s not found", employeeID)
	return agentservice.Tenant{}, agentservice.User{}, agentservice.Instance{}
}

func platformRuntimeTenantIDForTest(t *testing.T, svc *agentservice.Service, hubTenantID string) string {
	t.Helper()
	tenants, err := svc.ListTenants(t.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		t.Fatalf("ListTenants: %v", err)
	}
	for _, tenant := range tenants {
		users, err := svc.ListUsers(t.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		for _, user := range users {
			instances, err := svc.ListInstances(t.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
			if err != nil {
				t.Fatalf("ListInstances: %v", err)
			}
			for _, inst := range instances {
				if inst.Metadata["ve_hub_tenant_id"] == hubTenantID {
					return tenant.ID
				}
			}
		}
	}
	t.Fatalf("runtime tenant for hub tenant %s not found", hubTenantID)
	return ""
}
