package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestAdminCapabilityMarketPolicyDefaults(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/policy", nil)
	rec := httptest.NewRecorder()

	AdminCapabilityMarketPolicyGetHandler(settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Policy corelib.CapabilityMarketPolicy `json:"policy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Policy.EffectiveEnterpriseOnlyInstall() {
		t.Fatalf("enterprise_only_install should default to true")
	}
	if resp.Policy.EffectiveEnterpriseOnlySearch() {
		t.Fatalf("enterprise_only_search should default to false")
	}
}

func TestAdminCapabilityMarketPolicyUpdatePersistsDefaults(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	body := []byte(`{"policy":{"enterprise_only_search":true,"view_mode":"enterprise_only"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/capability-market/policy", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	AdminCapabilityMarketPolicyUpdateHandler(settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	raw, err := settings.Get(req.Context(), capabilityMarketPolicySettingKey)
	if err != nil {
		t.Fatalf("get saved policy: %v", err)
	}
	var saved corelib.CapabilityMarketPolicy
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatalf("decode saved policy: %v", err)
	}
	if !saved.EffectiveEnterpriseOnlyInstall() {
		t.Fatalf("enterprise_only_install should be defaulted before persist")
	}
	if !saved.EffectiveEnterpriseOnlySearch() {
		t.Fatalf("enterprise_only_search should be saved as true")
	}
	if saved.ViewMode != "enterprise_only" {
		t.Fatalf("view_mode=%q", saved.ViewMode)
	}
}

func TestAdminCapabilityUpsertAndManagementFlow(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	createBody := []byte(`{
		"capability_type":"mcp",
		"publisher":"acme",
		"capability_id":"billing-mcp",
		"display_name":"Billing MCP",
		"source":"enterprise_hub",
		"status":"approved",
		"version":"1.0.0",
		"manifest":{"name":"billing-mcp"},
		"type_config":{"command":"billing-mcp"},
		"pricing":{"mode":"free"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities", bytes.NewReader(createBody))
	rec := httptest.NewRecorder()
	AdminCapabilityUpsertHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created capability.CapabilitySummary
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created capability: %v", err)
	}
	if created.CurrentVersionKey == "" {
		t.Fatalf("current version key should be set")
	}

	depReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/managed-deployments", bytes.NewReader([]byte(`{"capability_ref":"`+created.ID+`","deployment_policy":"INVALID","scope":{"all_users":true}}`)))
	depRec := httptest.NewRecorder()
	AdminCapabilityManagedDeploymentCreateHandler(svc)(depRec, depReq)
	if depRec.Code != http.StatusCreated {
		t.Fatalf("deployment status=%d body=%s", depRec.Code, depRec.Body.String())
	}

	recReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/recommendations", bytes.NewReader([]byte(`{"capability_ref":"`+created.ID+`","recommendation_reason":"useful"}`)))
	recRec := httptest.NewRecorder()
	AdminCapabilityRecommendationCreateHandler(svc)(recRec, recReq)
	if recRec.Code != http.StatusCreated {
		t.Fatalf("recommendation status=%d body=%s", recRec.Code, recRec.Body.String())
	}

	secretReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/mcp-secret-requirements", bytes.NewReader([]byte(`{"capability_ref":"`+created.ID+`","version_key":"`+created.CurrentVersionKey+`","name":"api_key","storage_policy":"hub_or_local"}`)))
	secretRec := httptest.NewRecorder()
	AdminMCPSecretRequirementUpsertHandler(svc)(secretRec, secretReq)
	if secretRec.Code != http.StatusCreated {
		t.Fatalf("secret status=%d body=%s", secretRec.Code, secretRec.Body.String())
	}

	items, err := svc.ListManagedDeployments(req.Context())
	if err != nil || len(items) != 1 {
		t.Fatalf("deployments len=%d err=%v", len(items), err)
	}
	if items[0].DeploymentPolicy != "required" {
		t.Fatalf("deployment policy should normalize to required, got %q", items[0].DeploymentPolicy)
	}
	if _, err := db.ExecContext(req.Context(), `UPDATE managed_capability_deployments SET deployment_policy = ? WHERE id = ?`, "LEGACY_UNKNOWN", items[0].ID); err != nil {
		t.Fatalf("write legacy deployment policy: %v", err)
	}
	items, err = svc.ListManagedDeployments(req.Context())
	if err != nil || len(items) != 1 {
		t.Fatalf("deployments after legacy policy len=%d err=%v", len(items), err)
	}
	if items[0].DeploymentPolicy != "required" {
		t.Fatalf("legacy deployment policy should normalize to required, got %q", items[0].DeploymentPolicy)
	}
	recommendations, err := svc.ListRecommendations(req.Context())
	if err != nil || len(recommendations) != 1 {
		t.Fatalf("recommendations len=%d err=%v", len(recommendations), err)
	}

	delDepReq := httptest.NewRequest(http.MethodDelete, "/api/admin/capability-market/managed-deployments/"+items[0].ID, nil)
	delDepReq.SetPathValue("id", items[0].ID)
	delDepRec := httptest.NewRecorder()
	AdminCapabilityManagedDeploymentDeleteHandler(svc)(delDepRec, delDepReq)
	if delDepRec.Code != http.StatusOK {
		t.Fatalf("delete deployment status=%d body=%s", delDepRec.Code, delDepRec.Body.String())
	}
	items, err = svc.ListManagedDeployments(req.Context())
	if err != nil || len(items) != 0 {
		t.Fatalf("deployments after delete len=%d err=%v", len(items), err)
	}

	delRecReq := httptest.NewRequest(http.MethodDelete, "/api/admin/capability-market/recommendations/"+recommendations[0].ID, nil)
	delRecReq.SetPathValue("id", recommendations[0].ID)
	delRecRec := httptest.NewRecorder()
	AdminCapabilityRecommendationDeleteHandler(svc)(delRecRec, delRecReq)
	if delRecRec.Code != http.StatusOK {
		t.Fatalf("delete recommendation status=%d body=%s", delRecRec.Code, delRecRec.Body.String())
	}
	recommendations, err = svc.ListRecommendations(req.Context())
	if err != nil || len(recommendations) != 0 {
		t.Fatalf("recommendations after delete len=%d err=%v", len(recommendations), err)
	}

	secrets, err := svc.ListMCPSecretRequirements(req.Context(), created.ID, created.CurrentVersionKey)
	if err != nil || len(secrets) != 1 || secrets[0].Name != "api_key" {
		t.Fatalf("secrets=%v err=%v", secrets, err)
	}

	binding, err := svc.UpsertMCPSecretBinding(req.Context(), capability.MCPSecretBindingInput{UserID: "user-1", MCPServerID: created.ID, RequirementName: "api_key", Storage: "local", LocalSecretRef: "maclaw://secrets/billing", Status: "configured"})
	if err != nil {
		t.Fatalf("upsert binding: %v", err)
	}
	if binding.Storage != "local" || binding.LocalSecretRef == "" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	bindings, err := svc.ListMCPSecretBindings(req.Context(), "user-1", created.ID)
	if err != nil || len(bindings) != 1 || bindings[0].RequirementName != "api_key" {
		t.Fatalf("bindings=%v err=%v", bindings, err)
	}
}

func TestAdminCapabilityDeploymentCreateWritesAudit(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	created := createTestCapability(t, svc, "mcp", "audit-mcp", "Audit MCP", "1.0.0")
	audit := &testAdminAuditRepo{}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/managed-deployments", bytes.NewReader([]byte(`{"capability_ref":"`+created.ID+`","deployment_policy":"INVALID","scope":{"type":"group","group_id":"dept-a"}}`)))
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-audit"}))
	rec := httptest.NewRecorder()

	AdminCapabilityManagedDeploymentCreateHandler(svc, audit)(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(audit.logs) != 1 {
		t.Fatalf("expected audit log, got %d", len(audit.logs))
	}
	if audit.logs[0].Action != "capability.managed_deployment.create" {
		t.Fatalf("unexpected audit action: %s", audit.logs[0].Action)
	}
	if audit.logs[0].AdminUserID != "adm-audit" {
		t.Fatalf("unexpected admin id: %s", audit.logs[0].AdminUserID)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(audit.logs[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("decode audit payload: %v", err)
	}
	if payload["deployment_policy"] != "required" {
		t.Fatalf("audit deployment_policy should be normalized to required, got %#v", payload["deployment_policy"])
	}
}

func TestAdminAuditLogsHandlerListsAuditLogs(t *testing.T) {
	audit := &testAdminAuditRepo{}
	audit.logs = []*store.AdminAuditLog{{ID: "audit-1", TenantID: "tenant_a", AdminUserID: "adm-1", Action: "security.group.create", PayloadJSON: `{"group_id":"dept-a","name":"Dept A"}`, CreatedAt: time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)}}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit-logs?limit=10&q=dept-a&action=security.group.create&from=2026-05-15&to=2026-05-16", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	rec := httptest.NewRecorder()

	AdminAuditLogsHandler(audit)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			TenantID string         `json:"tenant_id"`
			Action   string         `json:"action"`
			Payload  map[string]any `json:"payload"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Action != "security.group.create" {
		t.Fatalf("unexpected audit items: %+v", resp.Items)
	}
	if resp.Items[0].TenantID != "tenant_a" {
		t.Fatalf("tenant id not returned: %+v", resp.Items[0])
	}
	if resp.Items[0].Payload["group_id"] != "dept-a" {
		t.Fatalf("payload not decoded: %+v", resp.Items[0].Payload)
	}
	if audit.lastFilter.Limit != 10 || audit.lastFilter.Query != "dept-a" || audit.lastFilter.Action != "security.group.create" || audit.lastFilter.TenantID != "tenant_a" || !audit.lastFilter.TenantScoped {
		t.Fatalf("filter not passed through: %+v", audit.lastFilter)
	}
	if audit.lastFilter.CreatedFrom.IsZero() || audit.lastFilter.CreatedTo.IsZero() {
		t.Fatalf("date filter not parsed: %+v", audit.lastFilter)
	}
}

func TestAdminAuditLogsHandlerRejectsInvalidDate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit-logs?from=not-a-date", nil)
	rec := httptest.NewRecorder()

	AdminAuditLogsHandler(&testAdminAuditRepo{})(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminAuditLogsHandlerRejectsInvertedDateRange(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/audit-logs?from=2026-05-16&to=2026-05-15", nil)
	rec := httptest.NewRecorder()

	AdminAuditLogsHandler(&testAdminAuditRepo{})(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type fakeMarketplaceViewerAuth struct {
	tenantID string
	userID   string
	email    string
}

func (f fakeMarketplaceViewerAuth) AuthenticateViewer(ctx context.Context, rawToken string) (*auth.ViewerPrincipal, error) {
	if rawToken != "viewer-token" {
		return nil, auth.ErrInvalidUserCredentials
	}
	return &auth.ViewerPrincipal{TenantID: f.tenantID, UserID: firstNonEmpty(f.userID, "user-1"), Email: firstNonEmpty(f.email, "user@example.com")}, nil
}

func TestCapabilityInstallIntentUsesAuthenticatedTenantOverHeader(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}

	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/paid-mcp/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"paid","price":{"amount_cents":9900}}`)))
	req.SetPathValue("id", "paid-mcp")
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("X-Tenant-ID", "tenant_b")
	rec := httptest.NewRecorder()

	CapabilityInstallIntentHandler(svc, settings, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "user-a"})(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	tenantARequests, err := svc.ListAcquisitionRequests(capability.WithTenant(context.Background(), "tenant_a"), "pending_review")
	if err != nil || len(tenantARequests) != 1 {
		t.Fatalf("tenant_a requests=%+v err=%v", tenantARequests, err)
	}
	if tenantARequests[0].RequesterUserID != "anonymous" || tenantARequests[0].SourceCapabilityKey != "paid-mcp" {
		t.Fatalf("unexpected tenant_a request: %+v", tenantARequests[0])
	}
	tenantBRequests, err := svc.ListAcquisitionRequests(capability.WithTenant(context.Background(), "tenant_b"), "pending_review")
	if err != nil {
		t.Fatalf("list tenant_b requests: %v", err)
	}
	if len(tenantBRequests) != 0 {
		t.Fatalf("spoofed header tenant should not receive request: %+v", tenantBRequests)
	}
}

type fakeCapabilityGroupResolver struct {
	chain []string
	err   error
}

func (f fakeCapabilityGroupResolver) ResolveUserGroupChain(ctx context.Context, email string) ([]string, error) {
	return f.chain, f.err
}

func (f fakeCapabilityGroupResolver) ResolveGroupChain(ctx context.Context, groupID string) ([]string, error) {
	return f.chain, f.err
}

func TestAdminUserCapabilityEffectivePoliciesResolvesPriority(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	created := createTestCapability(t, svc, "skill", "code-review", "Code Review", "1.0.0")
	createDeploymentForTest(t, svc, created.ID, "", map[string]any{"type": "global"}, "recommended")
	createDeploymentForTest(t, svc, created.ID, "", map[string]any{"type": "group", "group_id": "root"}, "required")
	createDeploymentForTest(t, svc, created.ID, "", map[string]any{"type": "group", "group_id": "dept-a"}, "blocked")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/dev@example.com/effective-policies", nil)
	req.SetPathValue("email", "dev@example.com")
	rec := httptest.NewRecorder()
	AdminUserCapabilityEffectivePoliciesHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("effective policies status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []adminEffectiveCapabilityPolicy `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode policies: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityRef != created.ID || resp.Items[0].Policy != "blocked" || resp.Items[0].Source != "group" {
		t.Fatalf("unexpected effective policies: %+v", resp.Items)
	}
}

func TestAdminUserCapabilityEffectivePoliciesUserOverride(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	created := createTestCapability(t, svc, "mcp", "billing", "Billing MCP", "1.0.0")
	createDeploymentForTest(t, svc, created.ID, "", map[string]any{"type": "group", "group_id": "dept-a"}, "blocked")
	createDeploymentForTest(t, svc, created.ID, "", map[string]any{"type": "user", "user_email": "dev@example.com"}, "required")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/dev@example.com/effective-policies", nil)
	req.SetPathValue("email", "dev@example.com")
	rec := httptest.NewRecorder()
	AdminUserCapabilityEffectivePoliciesHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("effective policies status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []adminEffectiveCapabilityPolicy `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode policies: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Policy != "required" || resp.Items[0].Source != "user" {
		t.Fatalf("unexpected user override policies: %+v", resp.Items)
	}
}

func TestAdminGroupCapabilityEffectivePoliciesResolvesInheritance(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	created := createTestCapability(t, svc, "skill", "legal-draft", "Legal Draft", "1.0.0")
	createDeploymentForTest(t, svc, created.ID, "", map[string]any{"type": "global"}, "recommended")
	createDeploymentForTest(t, svc, created.ID, "", map[string]any{"type": "group", "group_id": "root"}, "required")
	createDeploymentForTest(t, svc, created.ID, "", map[string]any{"type": "group", "group_id": "legal"}, "blocked")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/groups/legal/effective-policies", nil)
	req.SetPathValue("id", "legal")
	rec := httptest.NewRecorder()
	AdminGroupCapabilityEffectivePoliciesHandler(svc, fakeCapabilityGroupResolver{chain: []string{"legal", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("group effective policies status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items      []adminEffectiveCapabilityPolicy `json:"items"`
		GroupChain []string                         `json:"group_chain"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode policies: %v", err)
	}
	if len(resp.GroupChain) != 2 || resp.GroupChain[0] != "legal" || resp.GroupChain[1] != "root" {
		t.Fatalf("unexpected group chain: %+v", resp.GroupChain)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityRef != created.ID || resp.Items[0].Policy != "blocked" || resp.Items[0].Source != "group" {
		t.Fatalf("unexpected group effective policies: %+v", resp.Items)
	}
}

func TestAdminCapabilityEffectivePoliciesSortedDeterministically(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	alpha := createTestCapability(t, svc, "skill", "alpha", "Alpha", "1.0.0")
	beta := createTestCapability(t, svc, "skill", "beta", "Beta", "1.0.0")
	createDeploymentForTest(t, svc, beta.ID, "", map[string]any{"type": "global"}, "recommended")
	createDeploymentForTest(t, svc, alpha.ID, "", map[string]any{"type": "user", "user_email": "dev@example.com"}, "required")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/dev@example.com/effective-policies", nil)
	req.SetPathValue("email", "dev@example.com")
	rec := httptest.NewRecorder()
	AdminUserCapabilityEffectivePoliciesHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("effective policies status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []adminEffectiveCapabilityPolicy `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode policies: %v", err)
	}
	if len(resp.Items) != 2 || resp.Items[0].CapabilityRef != alpha.ID || resp.Items[1].CapabilityRef != beta.ID {
		t.Fatalf("unexpected sorted policies: %+v", resp.Items)
	}
}

func TestAdminUserCapabilityComplianceHandler(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	created := createTestCapability(t, svc, "skill", "code-review", "Code Review", "1.0.0")
	createDeploymentForTest(t, svc, created.ID, created.CurrentVersionKey, map[string]any{"type": "user", "user_email": "dev@example.com"}, "required")
	_, err := svc.UpsertUserCapabilityInventory(context.Background(), capability.UserCapabilityInventoryInput{UserID: "user-1", UserEmail: "dev@example.com", CapabilityRef: created.ID, CapabilityVersionKey: "older", CapabilityType: "skill", InstallStatus: "installed", Installed: true})
	if err != nil {
		t.Fatalf("upsert inventory: %v", err)
	}
	_, err = svc.UpsertUserCapabilityInventory(context.Background(), capability.UserCapabilityInventoryInput{UserID: "user-1", UserEmail: "dev@example.com", CapabilityRef: "extra-cap", CapabilityVersionKey: "v1", CapabilityType: "mcp", InstallStatus: "installed", Installed: true})
	if err != nil {
		t.Fatalf("upsert extra inventory: %v", err)
	}
	_, err = svc.UpsertUserCapabilityInventory(context.Background(), capability.UserCapabilityInventoryInput{UserID: "user-1", UserEmail: "dev@example.com", CapabilityRef: "another-extra", CapabilityVersionKey: "v1", CapabilityType: "skill", InstallStatus: "installed", Installed: true})
	if err != nil {
		t.Fatalf("upsert second extra inventory: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/dev@example.com/compliance", nil)
	req.SetPathValue("email", "dev@example.com")
	rec := httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
		GeneratedAt     string                                   `json:"generated_at"`
		StaleAfterHours int                                      `json:"stale_after_hours"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode compliance: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Status != "version_mismatch" || resp.Summary.VersionMismatch != 1 || resp.Summary.UnmanagedInstalled != 2 || len(resp.UnmanagedItems) != 2 || resp.UnmanagedItems[0].CapabilityRef != "another-extra" || resp.UnmanagedItems[1].CapabilityRef != "extra-cap" {
		t.Fatalf("unexpected compliance: %+v summary=%+v", resp.Items, resp.Summary)
	}
	if resp.GeneratedAt == "" || resp.StaleAfterHours != 168 {
		t.Fatalf("unexpected compliance metadata: generated_at=%q stale_after_hours=%d", resp.GeneratedAt, resp.StaleAfterHours)
	}
}

func TestAdminUserCapabilityComplianceRiskStates(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	blocked := createTestCapability(t, svc, "mcp", "blocked-tool", "Blocked Tool", "1.0.0")
	stale := createTestCapability(t, svc, "skill", "stale-skill", "Stale Skill", "1.0.0")
	createDeploymentForTest(t, svc, blocked.ID, blocked.CurrentVersionKey, map[string]any{"type": "user", "user_email": "risk@example.com"}, "blocked")
	createDeploymentForTest(t, svc, stale.ID, stale.CurrentVersionKey, map[string]any{"type": "user", "user_email": "risk@example.com"}, "required")
	_, err := svc.UpsertUserCapabilityInventory(context.Background(), capability.UserCapabilityInventoryInput{UserID: "user-1", UserEmail: "risk@example.com", CapabilityRef: blocked.ID, CapabilityVersionKey: blocked.CurrentVersionKey, CapabilityType: "mcp", InstallStatus: "installed", Installed: true})
	if err != nil {
		t.Fatalf("upsert blocked inventory: %v", err)
	}
	_, err = svc.UpsertUserCapabilityInventory(context.Background(), capability.UserCapabilityInventoryInput{UserID: "user-1", UserEmail: "risk@example.com", CapabilityRef: stale.ID, CapabilityVersionKey: stale.CurrentVersionKey, CapabilityType: "skill", InstallStatus: "installed", Installed: true, LastSeenAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatalf("upsert stale inventory: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/risk@example.com/compliance?stale_after_hours=1", nil)
	req.SetPathValue("email", "risk@example.com")
	rec := httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items   []adminCapabilityComplianceItem  `json:"items"`
		Summary adminCapabilityComplianceSummary `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode compliance: %v", err)
	}
	byRef := map[string]adminCapabilityComplianceItem{}
	for _, item := range resp.Items {
		byRef[item.CapabilityRef] = item
	}
	if byRef[blocked.ID].Status != "blocked_installed" || byRef[stale.ID].Status != "stale" || resp.Summary.BlockedInstalled != 1 || resp.Summary.Stale != 1 {
		t.Fatalf("unexpected risk compliance: items=%+v summary=%+v", resp.Items, resp.Summary)
	}
}

func TestAdminUserCapabilityComplianceUsesCurrentVersionWhenPolicyVersionEmpty(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	created := createTestCapability(t, svc, "skill", "versioned", "Versioned", "1.0.0")
	createDeploymentForTest(t, svc, created.ID, "", map[string]any{"type": "user", "user_email": "version@example.com"}, "required")
	_, err := svc.UpsertUserCapabilityInventory(context.Background(), capability.UserCapabilityInventoryInput{UserID: "user-1", UserEmail: "version@example.com", CapabilityRef: created.ID, CapabilityVersionKey: "old-version", CapabilityType: "skill", InstallStatus: "installed", Installed: true})
	if err != nil {
		t.Fatalf("upsert inventory: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/version@example.com/compliance", nil)
	req.SetPathValue("email", "version@example.com")
	rec := httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []adminCapabilityComplianceItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode compliance: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Status != "version_mismatch" || resp.Items[0].CapabilityVersionKey != created.CurrentVersionKey {
		t.Fatalf("unexpected current version compliance: %+v", resp.Items)
	}
}

func TestAdminUserCapabilityComplianceFilters(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	missing := createTestCapability(t, svc, "skill", "missing-one", "Missing One", "1.0.0")
	ok := createTestCapability(t, svc, "skill", "ok-one", "OK One", "1.0.0")
	createDeploymentForTest(t, svc, missing.ID, missing.CurrentVersionKey, map[string]any{"type": "user", "user_email": "filter@example.com"}, "required")
	createDeploymentForTest(t, svc, ok.ID, ok.CurrentVersionKey, map[string]any{"type": "user", "user_email": "filter@example.com"}, "required")
	_, err := svc.UpsertUserCapabilityInventory(context.Background(), capability.UserCapabilityInventoryInput{UserID: "user-1", UserEmail: "filter@example.com", CapabilityRef: ok.ID, CapabilityVersionKey: ok.CurrentVersionKey, CapabilityType: "skill", InstallStatus: "installed", Installed: true})
	if err != nil {
		t.Fatalf("upsert inventory: %v", err)
	}
	_, err = svc.UpsertUserCapabilityInventory(context.Background(), capability.UserCapabilityInventoryInput{UserID: "user-1", UserEmail: "filter@example.com", CapabilityRef: "extra-filter", CapabilityVersionKey: "v1", CapabilityType: "mcp", InstallStatus: "installed", Installed: true})
	if err != nil {
		t.Fatalf("upsert unmanaged inventory: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance", nil)
	req.SetPathValue("email", "filter@example.com")
	rec := httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unfiltered compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var unfilteredResp struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		FilteredSummary *adminCapabilityComplianceSummary        `json:"filtered_summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &unfilteredResp); err != nil {
		t.Fatalf("decode unfiltered compliance: %v", err)
	}
	if len(unfilteredResp.Items) != 2 || unfilteredResp.Summary.Total != 2 || unfilteredResp.Summary.UnmanagedInstalled != 1 || unfilteredResp.FilteredSummary != nil || len(unfilteredResp.UnmanagedItems) != 1 {
		t.Fatalf("unexpected unfiltered compliance: items=%+v summary=%+v filtered=%+v unmanaged=%+v", unfilteredResp.Items, unfilteredResp.Summary, unfilteredResp.FilteredSummary, unfilteredResp.UnmanagedItems)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?status=typo", nil)
	req.SetPathValue("email", "filter@example.com")
	rec = httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid filter compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var invalidFilterResp struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		FilteredSummary *adminCapabilityComplianceSummary        `json:"filtered_summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &invalidFilterResp); err != nil {
		t.Fatalf("decode invalid filter compliance: %v", err)
	}
	if len(invalidFilterResp.Items) != 2 || invalidFilterResp.Summary.Total != 2 || invalidFilterResp.FilteredSummary != nil || len(invalidFilterResp.UnmanagedItems) != 1 {
		t.Fatalf("unexpected invalid filter compliance: items=%+v summary=%+v filtered=%+v unmanaged=%+v", invalidFilterResp.Items, invalidFilterResp.Summary, invalidFilterResp.FilteredSummary, invalidFilterResp.UnmanagedItems)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?include_unmanaged=maybe", nil)
	req.SetPathValue("email", "filter@example.com")
	rec = httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invalid include compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var invalidIncludeResp struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		FilteredSummary *adminCapabilityComplianceSummary        `json:"filtered_summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &invalidIncludeResp); err != nil {
		t.Fatalf("decode invalid include compliance: %v", err)
	}
	if len(invalidIncludeResp.Items) != 2 || invalidIncludeResp.Summary.Total != 2 || invalidIncludeResp.FilteredSummary != nil || len(invalidIncludeResp.UnmanagedItems) != 1 {
		t.Fatalf("unexpected invalid include compliance: items=%+v summary=%+v filtered=%+v unmanaged=%+v", invalidIncludeResp.Items, invalidIncludeResp.Summary, invalidIncludeResp.FilteredSummary, invalidIncludeResp.UnmanagedItems)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?include_unmanaged=false", nil)
	req.SetPathValue("email", "filter@example.com")
	rec = httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("include false compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var includeFalseResp struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		FilteredSummary adminCapabilityComplianceSummary         `json:"filtered_summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &includeFalseResp); err != nil {
		t.Fatalf("decode include false compliance: %v", err)
	}
	if len(includeFalseResp.Items) != 2 || includeFalseResp.Summary.UnmanagedInstalled != 1 || includeFalseResp.FilteredSummary.Total != 2 || includeFalseResp.FilteredSummary.UnmanagedInstalled != 0 || len(includeFalseResp.UnmanagedItems) != 0 {
		t.Fatalf("unexpected include false compliance: items=%+v summary=%+v filtered=%+v unmanaged=%+v", includeFalseResp.Items, includeFalseResp.Summary, includeFalseResp.FilteredSummary, includeFalseResp.UnmanagedItems)
	}

	for _, includeValue := range []string{"0", "no", "off"} {
		req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?include_unmanaged="+includeValue, nil)
		req.SetPathValue("email", "filter@example.com")
		rec = httptest.NewRecorder()
		AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("include %s compliance status=%d body=%s", includeValue, rec.Code, rec.Body.String())
		}
		var aliasResp struct {
			Items           []adminCapabilityComplianceItem          `json:"items"`
			FilteredSummary adminCapabilityComplianceSummary         `json:"filtered_summary"`
			UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &aliasResp); err != nil {
			t.Fatalf("decode include %s compliance: %v", includeValue, err)
		}
		if len(aliasResp.Items) != 2 || aliasResp.FilteredSummary.Total != 2 || aliasResp.FilteredSummary.UnmanagedInstalled != 0 || len(aliasResp.UnmanagedItems) != 0 {
			t.Fatalf("unexpected include %s compliance: items=%+v filtered=%+v unmanaged=%+v", includeValue, aliasResp.Items, aliasResp.FilteredSummary, aliasResp.UnmanagedItems)
		}
	}

	for _, includeValue := range []string{"true", "1", "yes", "on"} {
		req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?include_unmanaged="+includeValue, nil)
		req.SetPathValue("email", "filter@example.com")
		rec = httptest.NewRecorder()
		AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("include %s compliance status=%d body=%s", includeValue, rec.Code, rec.Body.String())
		}
		var aliasResp struct {
			Items           []adminCapabilityComplianceItem          `json:"items"`
			FilteredSummary *adminCapabilityComplianceSummary        `json:"filtered_summary"`
			UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &aliasResp); err != nil {
			t.Fatalf("decode include %s compliance: %v", includeValue, err)
		}
		if len(aliasResp.Items) != 2 || aliasResp.FilteredSummary != nil || len(aliasResp.UnmanagedItems) != 1 {
			t.Fatalf("unexpected include %s compliance: items=%+v filtered=%+v unmanaged=%+v", includeValue, aliasResp.Items, aliasResp.FilteredSummary, aliasResp.UnmanagedItems)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?status=missing&include_unmanaged=false", nil)
	req.SetPathValue("email", "filter@example.com")
	rec = httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		FilteredSummary adminCapabilityComplianceSummary         `json:"filtered_summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode compliance: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityRef != missing.ID || resp.Summary.Total != 2 || resp.FilteredSummary.Total != 1 || resp.FilteredSummary.Missing != 1 || len(resp.UnmanagedItems) != 0 {
		t.Fatalf("unexpected filtered compliance: items=%+v summary=%+v unmanaged=%+v", resp.Items, resp.Summary, resp.UnmanagedItems)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?status=unmanaged_installed", nil)
	req.SetPathValue("email", "filter@example.com")
	rec = httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unmanaged compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	resp = struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		FilteredSummary adminCapabilityComplianceSummary         `json:"filtered_summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode unmanaged compliance: %v", err)
	}
	if len(resp.Items) != 0 || resp.FilteredSummary.Total != 0 || resp.FilteredSummary.UnmanagedInstalled != 1 || len(resp.UnmanagedItems) != 1 || resp.UnmanagedItems[0].CapabilityRef != "extra-filter" {
		t.Fatalf("unexpected unmanaged filtered compliance: items=%+v unmanaged=%+v", resp.Items, resp.UnmanagedItems)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?status=unmanaged_installed&include_unmanaged=false", nil)
	req.SetPathValue("email", "filter@example.com")
	rec = httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hidden unmanaged compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	resp = struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		FilteredSummary adminCapabilityComplianceSummary         `json:"filtered_summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode hidden unmanaged compliance: %v", err)
	}
	if len(resp.Items) != 0 || resp.FilteredSummary.Total != 0 || resp.FilteredSummary.UnmanagedInstalled != 0 || len(resp.UnmanagedItems) != 0 {
		t.Fatalf("unexpected hidden unmanaged filtered compliance: items=%+v filtered=%+v unmanaged=%+v", resp.Items, resp.FilteredSummary, resp.UnmanagedItems)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?status=issues", nil)
	req.SetPathValue("email", "filter@example.com")
	rec = httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("risk compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	resp = struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		FilteredSummary adminCapabilityComplianceSummary         `json:"filtered_summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode risk compliance: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityRef != missing.ID || resp.FilteredSummary.Total != 1 || resp.FilteredSummary.Missing != 1 || resp.FilteredSummary.UnmanagedInstalled != 1 || len(resp.UnmanagedItems) != 1 || resp.UnmanagedItems[0].CapabilityRef != "extra-filter" {
		t.Fatalf("unexpected risk filtered compliance: items=%+v unmanaged=%+v", resp.Items, resp.UnmanagedItems)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/filter@example.com/compliance?status=risks", nil)
	req.SetPathValue("email", "filter@example.com")
	rec = httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("risks alias compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	resp = struct {
		Items           []adminCapabilityComplianceItem          `json:"items"`
		Summary         adminCapabilityComplianceSummary         `json:"summary"`
		FilteredSummary adminCapabilityComplianceSummary         `json:"filtered_summary"`
		UnmanagedItems  []capability.UserCapabilityInventoryItem `json:"unmanaged_items"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode risks alias compliance: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityRef != missing.ID || resp.FilteredSummary.Total != 1 || resp.FilteredSummary.Missing != 1 || resp.FilteredSummary.UnmanagedInstalled != 1 || len(resp.UnmanagedItems) != 1 || resp.UnmanagedItems[0].CapabilityRef != "extra-filter" {
		t.Fatalf("unexpected risks alias filtered compliance: items=%+v unmanaged=%+v", resp.Items, resp.UnmanagedItems)
	}
}

func createTestCapability(t *testing.T, svc *capability.Service, capabilityType, capabilityID, displayName, version string) capability.CapabilitySummary {
	t.Helper()
	body := []byte(`{"capability_type":"` + capabilityType + `","publisher":"acme","capability_id":"` + capabilityID + `","display_name":"` + displayName + `","source":"enterprise_hub","status":"approved","version":"` + version + `","manifest":{"name":"` + capabilityID + `"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	AdminCapabilityUpsertHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create capability status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created capability.CapabilitySummary
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode capability: %v", err)
	}
	return created
}

func createDeploymentForTest(t *testing.T, svc *capability.Service, capabilityRef, version string, scope map[string]any, policy string) {
	t.Helper()
	scopeRaw, err := json.Marshal(scope)
	if err != nil {
		t.Fatalf("marshal scope: %v", err)
	}
	_, err = svc.CreateManagedDeployment(context.Background(), capability.ManagedDeploymentInput{CapabilityRef: capabilityRef, CapabilityVersionKey: version, ScopeJSON: string(scopeRaw), DeploymentPolicy: policy, ReinstallIfRemoved: true, RetryIntervalMinutes: 60, CreatedBy: "test", Enabled: true})
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
}

func TestMCPSecretBindingUpsertRejectsMissingHubSecret(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body := []byte(`{"mcp_server_id":"billing-api","requirement_name":"api_token","storage":"hub","hub_secret_ref":"hub://mcp-secrets/billing-api/api_token","status":"configured"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/capabilities/mcp-secret-bindings", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()

	MCPSecretBindingUpsertHandler(fakeMarketplaceViewerAuth{}, svc)(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("upsert status=%d body=%s", rec.Code, rec.Body.String())
	}
	bindings, err := svc.ListMCPSecretBindings(req.Context(), "user-1", "billing-api")
	if err != nil || len(bindings) != 0 {
		t.Fatalf("bindings=%+v err=%v", bindings, err)
	}
}

func TestUserCapabilityInventoryFlow(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body := []byte(`{"items":[{"capability_ref":"cap-1","capability_version_key":"v1","capability_type":"skill","install_status":"installed","installed":true,"metadata":{"path":"local"}},{"capability_ref":"cap-2","capability_version_key":"v2","capability_type":"mcp","install_status":"missing","installed":false}]}`)
	req := httptest.NewRequest(http.MethodPut, "/api/capabilities/inventory", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()

	UserCapabilityInventoryUpsertHandler(fakeMarketplaceViewerAuth{}, svc)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upsert inventory status=%d body=%s", rec.Code, rec.Body.String())
	}
	listReq := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/user@example.com/inventory", nil)
	listReq.SetPathValue("email", "user@example.com")
	listRec := httptest.NewRecorder()
	AdminUserCapabilityInventoryHandler(svc)(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list inventory status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var resp struct {
		Items []capability.UserCapabilityInventoryItem `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode inventory: %v", err)
	}
	byRef := map[string]capability.UserCapabilityInventoryItem{}
	for _, item := range resp.Items {
		byRef[item.CapabilityRef] = item
	}
	if len(resp.Items) != 2 || !byRef["cap-1"].Installed || byRef["cap-2"].Installed {
		t.Fatalf("unexpected inventory: %+v", resp.Items)
	}

	snapshotBody := []byte(`{"full_snapshot":true,"items":[{"capability_ref":"cap-1","capability_version_key":"v1","capability_type":"skill","installed":true}]}`)
	snapshotReq := httptest.NewRequest(http.MethodPut, "/api/capabilities/inventory", bytes.NewReader(snapshotBody))
	snapshotReq.Header.Set("Authorization", "Bearer viewer-token")
	snapshotRec := httptest.NewRecorder()
	UserCapabilityInventoryUpsertHandler(fakeMarketplaceViewerAuth{}, svc)(snapshotRec, snapshotReq)
	if snapshotRec.Code != http.StatusOK {
		t.Fatalf("snapshot inventory status=%d body=%s", snapshotRec.Code, snapshotRec.Body.String())
	}
	listRec = httptest.NewRecorder()
	AdminUserCapabilityInventoryHandler(svc)(listRec, listReq)
	if err := json.Unmarshal(listRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode snapshot inventory: %v", err)
	}
	byRef = map[string]capability.UserCapabilityInventoryItem{}
	for _, item := range resp.Items {
		byRef[item.CapabilityRef] = item
	}
	if !byRef["cap-1"].Installed || byRef["cap-2"].Installed || byRef["cap-2"].InstallStatus != "missing" {
		t.Fatalf("unexpected snapshot inventory: %+v", resp.Items)
	}

	emptySnapshotReq := httptest.NewRequest(http.MethodPut, "/api/capabilities/inventory", bytes.NewReader([]byte(`{"full_snapshot":true,"items":[]}`)))
	emptySnapshotReq.Header.Set("Authorization", "Bearer viewer-token")
	emptySnapshotRec := httptest.NewRecorder()
	UserCapabilityInventoryUpsertHandler(fakeMarketplaceViewerAuth{}, svc)(emptySnapshotRec, emptySnapshotReq)
	if emptySnapshotRec.Code != http.StatusOK {
		t.Fatalf("empty snapshot inventory status=%d body=%s", emptySnapshotRec.Code, emptySnapshotRec.Body.String())
	}
	listRec = httptest.NewRecorder()
	AdminUserCapabilityInventoryHandler(svc)(listRec, listReq)
	if err := json.Unmarshal(listRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode empty snapshot inventory: %v", err)
	}
	for _, item := range resp.Items {
		if item.Installed || item.InstallStatus != "missing" {
			t.Fatalf("expected all missing after empty snapshot: %+v", resp.Items)
		}
	}
}

func TestMCPHubSecretUpsertStoresMetadataAndBinding(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body := []byte(`{"mcp_server_id":"billing-api","requirement_name":"api_token","secret_value":"super-secret-token","metadata":{"source":"user"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/capabilities/mcp-hub-secrets", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()

	MCPHubSecretUpsertHandler(fakeMarketplaceViewerAuth{}, svc)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var secret capability.MCPHubSecret
	if err := json.Unmarshal(rec.Body.Bytes(), &secret); err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if secret.UserID != "user-1" || secret.MCPServerID != "billing-api" || secret.RequirementName != "api_token" || secret.SecretDigest == "" {
		t.Fatalf("unexpected secret metadata: %+v", secret)
	}
	if strings.Contains(rec.Body.String(), "super-secret-token") {
		t.Fatalf("secret value leaked in response: %s", rec.Body.String())
	}
	bindings, err := svc.ListMCPSecretBindings(req.Context(), "user-1", "billing-api")
	if err != nil || len(bindings) != 1 || bindings[0].Storage != "hub" || bindings[0].HubSecretRef == "" {
		t.Fatalf("bindings=%+v err=%v", bindings, err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/capabilities/mcp-hub-secrets?mcp_server_id=billing-api", nil)
	listReq.Header.Set("Authorization", "Bearer viewer-token")
	listRec := httptest.NewRecorder()
	MCPHubSecretsHandler(fakeMarketplaceViewerAuth{}, svc)(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	if strings.Contains(listRec.Body.String(), "super-secret-token") {
		t.Fatalf("secret value leaked in list response: %s", listRec.Body.String())
	}
}

func TestAdminMCPMarketplaceUpsertCreatesCapabilityAndSecrets(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body := []byte(`{
		"publisher":"acme",
		"capability_id":"billing-api",
		"display_name":"Billing API MCP",
		"description":"Company billing API tools",
		"version":"1.2.0",
		"mcp":{"id":"billing-api","name":"Billing API","endpoint_url":"https://billing.example.com/mcp","auth_type":"bearer","headers":{"X-Tenant":"acme"}},
		"secret_requirements":[{"name":"api_token","label":"API Token","storage_policy":"hub_or_local"}],
		"pricing":{"mode":"paid","amount_cents":9900},
		"license":{"seats":5}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	AdminMCPMarketplaceUpsertHandler(svc)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created capability.CapabilitySummary
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created capability: %v", err)
	}
	if created.CapabilityType != corelib.CapabilityTypeMCP || created.CurrentVersionKey == "" {
		t.Fatalf("unexpected created capability: %+v", created)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(created.MetadataJSON), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["endpoint_url"] != "https://billing.example.com/mcp" || metadata["auth_type"] != "bearer" {
		t.Fatalf("unexpected metadata: %v", metadata)
	}
	secrets, err := svc.ListMCPSecretRequirements(req.Context(), created.ID, created.CurrentVersionKey)
	if err != nil || len(secrets) != 1 || secrets[0].Name != "api_token" {
		t.Fatalf("secrets=%v err=%v", secrets, err)
	}
	versions, err := svc.ListVersions(req.Context(), created.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions=%v err=%v", versions, err)
	}
	if !strings.Contains(versions[0].PricingJSON, "paid") || !strings.Contains(versions[0].LicenseJSON, "seats") {
		t.Fatalf("pricing/license were not preserved: pricing=%s license=%s", versions[0].PricingJSON, versions[0].LicenseJSON)
	}
}

type fakeCapabilityMarketCenterStatus struct {
	state *center.RegistrationState
	err   error
}

func (f fakeCapabilityMarketCenterStatus) Status(ctx context.Context) (*center.RegistrationState, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.state, nil
}

func TestAdminBillingCustomerAccountAndLicenses(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	if err := settings.Set(context.Background(), "admin_email", `{"value":"admin@example.com"}`); err != nil {
		t.Fatalf("set admin email: %v", err)
	}
	licenseCalled := false
	accountCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/capability-market/customer-account":
			accountCalled = true
			if r.URL.Query().Get("hub_id") != "hub-1" || r.URL.Query().Get("admin_email") != "admin@example.com" {
				t.Fatalf("unexpected account query: %s", r.URL.RawQuery)
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "configured", "customer_id": "cust-hub-1", "billing_portal_url": "https://billing.example.com/portal"})
		case "/api/capability-market/billing/licenses":
			licenseCalled = true
			if r.URL.Query().Get("hub_id") != "hub-1" || r.URL.Query().Get("admin_email") != "admin@example.com" {
				t.Fatalf("unexpected query: %s", r.URL.RawQuery)
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{map[string]any{"capability_type": "mcp", "capability_id": "paid-mcp"}}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	centerStatus := fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL, HubID: "hub-1"}}

	accountReq := httptest.NewRequest(http.MethodGet, "/api/admin/billing/customer-account", nil)
	accountRec := httptest.NewRecorder()
	AdminBillingCustomerAccountHandler(settings, centerStatus)(accountRec, accountReq)
	if accountRec.Code != http.StatusOK {
		t.Fatalf("account status=%d body=%s", accountRec.Code, accountRec.Body.String())
	}
	var account map[string]any
	if err := json.Unmarshal(accountRec.Body.Bytes(), &account); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if !accountCalled {
		t.Fatalf("expected HubCenter customer account endpoint to be called")
	}
	if account["status"] != "configured" || account["admin_email"] != "admin@example.com" || account["hub_id"] != "hub-1" || account["customer_id"] != "cust-hub-1" || account["billing_portal_url"] != "https://billing.example.com/portal" {
		t.Fatalf("unexpected account: %+v", account)
	}

	licensesReq := httptest.NewRequest(http.MethodGet, "/api/admin/billing/licenses", nil)
	licensesRec := httptest.NewRecorder()
	AdminBillingLicensesHandler(settings, centerStatus)(licensesRec, licensesReq)
	if licensesRec.Code != http.StatusOK {
		t.Fatalf("licenses status=%d body=%s", licensesRec.Code, licensesRec.Body.String())
	}
	if !licenseCalled {
		t.Fatalf("expected HubCenter license endpoint to be called")
	}
	var licenses struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(licensesRec.Body.Bytes(), &licenses); err != nil {
		t.Fatalf("decode licenses: %v", err)
	}
	if len(licenses.Items) != 1 || licenses.Items[0]["capability_id"] != "paid-mcp" {
		t.Fatalf("unexpected licenses: %+v", licenses.Items)
	}
}

func TestAdminBillingForwardsTenantIDToHubCenter(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	if err := settings.Set(context.Background(), "admin_email", `{"value":"admin@example.com"}`); err != nil {
		t.Fatalf("set admin email: %v", err)
	}
	accountCalled := false
	licenseCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/capability-market/customer-account":
			accountCalled = true
			if r.URL.Query().Get("hub_id") != "hub-1" || r.URL.Query().Get("admin_email") != "admin@example.com" || r.URL.Query().Get("tenant_id") != "tenant_a" {
				t.Fatalf("unexpected account query: %s", r.URL.RawQuery)
			}
			writeJSON(w, http.StatusOK, map[string]any{"status": "configured", "tenant_id": "tenant_a"})
		case "/api/capability-market/billing/licenses":
			licenseCalled = true
			if r.URL.Query().Get("hub_id") != "hub-1" || r.URL.Query().Get("admin_email") != "admin@example.com" || r.URL.Query().Get("tenant_id") != "tenant_a" {
				t.Fatalf("unexpected licenses query: %s", r.URL.RawQuery)
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{map[string]any{"capability_id": "tenant-mcp", "tenant_id": "tenant_a"}}})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	centerStatus := fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL, HubID: "hub-1"}}
	ctx := context.WithValue(context.Background(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Email: "admin@example.com", Scope: "tenant", TenantID: "tenant_a"})

	accountReq := httptest.NewRequest(http.MethodGet, "/api/admin/billing/customer-account", nil).WithContext(ctx)
	accountRec := httptest.NewRecorder()
	AdminBillingCustomerAccountHandler(settings, centerStatus)(accountRec, accountReq)
	if accountRec.Code != http.StatusOK {
		t.Fatalf("account status=%d body=%s", accountRec.Code, accountRec.Body.String())
	}

	licensesReq := httptest.NewRequest(http.MethodGet, "/api/admin/billing/licenses", nil).WithContext(ctx)
	licensesRec := httptest.NewRecorder()
	AdminBillingLicensesHandler(settings, centerStatus)(licensesRec, licensesReq)
	if licensesRec.Code != http.StatusOK {
		t.Fatalf("licenses status=%d body=%s", licensesRec.Code, licensesRec.Body.String())
	}
	if !accountCalled || !licenseCalled {
		t.Fatalf("expected both HubCenter billing endpoints to be called, account=%v license=%v", accountCalled, licenseCalled)
	}
}
func TestAdminCapabilityExternalSearchSourcePolicy(t *testing.T) {
	forbiddenReq := httptest.NewRequest(http.MethodGet, "/api/admin/capabilities/external-search?source=unknown&type=skill", nil)
	forbiddenRec := httptest.NewRecorder()
	AdminCapabilityExternalSearchHandler(nil)(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected unknown source to be forbidden, got status=%d body=%s", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	mcpReq := httptest.NewRequest(http.MethodGet, "/api/admin/capabilities/external-search?source=github&type=mcp", nil)
	mcpRec := httptest.NewRecorder()
	AdminCapabilityExternalSearchHandler(nil)(mcpRec, mcpReq)
	if mcpRec.Code != http.StatusOK {
		t.Fatalf("mcp search status=%d body=%s", mcpRec.Code, mcpRec.Body.String())
	}
	var resp struct {
		AllowedSources []string `json:"allowed_sources"`
		Items          []any    `json:"items"`
	}
	if err := json.Unmarshal(mcpRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 0 || len(resp.AllowedSources) != 3 || resp.AllowedSources[0] != corelib.CapabilitySourceHubCenter || resp.AllowedSources[1] != corelib.CapabilitySourceClawHub || resp.AllowedSources[2] != corelib.CapabilitySourceGitHub {
		t.Fatalf("unexpected external search response: %+v", resp)
	}
}
func TestAdminCapabilityExternalSearchHubCenterMCP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capability-market/mcp" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "billing" {
			t.Fatalf("q=%q", r.URL.Query().Get("q"))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{map[string]any{"capability_id": "billing-api", "display_name": "Billing API"}}})
	}))
	defer server.Close()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/capabilities/external-search?source=hub_center&type=mcp_server&q=billing", nil)
	rec := httptest.NewRecorder()

	AdminCapabilityExternalSearchHandler(fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0]["source"] != corelib.CapabilitySourceHubCenter || resp.Items[0]["capability_type"] != corelib.CapabilityTypeMCP {
		t.Fatalf("unexpected items: %+v", resp.Items)
	}
}

func TestAdminCapabilityExternalSearchHubCenterSkill(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capability-market/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "review" {
			t.Fatalf("q=%q", r.URL.Query().Get("q"))
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{map[string]any{"id": "code-review", "name": "Code Review"}}})
	}))
	defer server.Close()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/capabilities/external-search?source=hubcenter&type=skill&q=review", nil)
	rec := httptest.NewRecorder()

	AdminCapabilityExternalSearchHandler(fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0]["source"] != corelib.CapabilitySourceHubCenter || resp.Items[0]["capability_type"] != corelib.CapabilityTypeSkill {
		t.Fatalf("unexpected items: %+v", resp.Items)
	}
}

func TestAdminCapabilityExternalSearchHubCenterSkillItemsFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capability-market/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "sheet" {
			t.Fatalf("q=%q", r.URL.Query().Get("q"))
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{map[string]any{"capability_id": "sheet-review", "display_name": "Sheet Review"}}})
	}))
	defer server.Close()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/capabilities/external-search?source=hub_center&type=skill&q=sheet", nil)
	rec := httptest.NewRecorder()

	AdminCapabilityExternalSearchHandler(fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("unexpected items: %+v", resp.Items)
	}
	item := resp.Items[0]
	if item["id"] != "sheet-review" || item["capability_id"] != "sheet-review" || item["name"] != "Sheet Review" || item["display_name"] != "Sheet Review" {
		t.Fatalf("unexpected normalized identity: %+v", item)
	}
	if item["source"] != corelib.CapabilitySourceHubCenter || item["capability_type"] != corelib.CapabilityTypeSkill || item["pricing"] != corelib.CapabilityPricingFree {
		t.Fatalf("unexpected normalized metadata: %+v", item)
	}
}

func TestAdminCapabilityImportIntentIgnoresClientEnterpriseOnlySearch(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	searchOnly := true
	policy := corelib.DefaultCapabilityMarketPolicy()
	policy.EnterpriseOnlySearch = &searchOnly
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := settings.Set(context.Background(), capabilityMarketPolicySettingKey, string(raw)); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/review-skill" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": "review-skill", "name": "Review Skill", "version": "1.0.0"})
	}))
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/import-intent", bytes.NewReader([]byte(`{"capability_type":"skill","capability_id":"review-skill","source":"hubcenter","pricing":"free"}`)))
	rec := httptest.NewRecorder()
	AdminCapabilityImportIntentHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "completed")
	if err != nil || len(items) != 1 || items[0].RequestKind != "import" {
		t.Fatalf("requests=%+v err=%v", items, err)
	}
}
func TestAdminCapabilityImportIntentRejectsFailedFreeImportRequest(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/import-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","capability_id":"missing-mcp","source":"hubcenter","pricing":"free"}`)))
	rec := httptest.NewRecorder()
	AdminCapabilityImportIntentHandler(svc, settings)(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "rejected")
	if err != nil || len(items) != 1 || items[0].RequestKind != "import" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if !strings.Contains(items[0].ApprovalJSON, "import_failed") {
		t.Fatalf("approval json = %s", items[0].ApprovalJSON)
	}
}
func TestAdminCapabilityImportIntentImportsFreeHubCenterSkill(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/skills/review-skill" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":          "review-skill",
			"name":        "Review Skill",
			"description": "Code review helper",
			"version":     "1.4.0",
			"author":      "vendor",
			"trust_level": "community",
		})
	}))
	defer server.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/import-intent", bytes.NewReader([]byte(`{"capability_type":"skill","capability_id":"review-skill","source":"hubcenter","pricing":"free"}`)))
	rec := httptest.NewRecorder()

	AdminCapabilityImportIntentHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Capability capability.CapabilitySummary `json:"capability"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Capability.CapabilityType != corelib.CapabilityTypeSkill || resp.Capability.Source != corelib.CapabilitySourceEnterpriseHub || resp.Capability.RelationToOrigin != "mirrored" {
		t.Fatalf("unexpected capability: %+v", resp.Capability)
	}
	versions, err := svc.ListVersions(req.Context(), resp.Capability.ID)
	if err != nil || len(versions) != 1 || versions[0].Version != "1.4.0" {
		t.Fatalf("versions=%v err=%v", versions, err)
	}
}

func TestAdminCapabilityImportIntentImportsFreeClawHubSkill(t *testing.T) {
	db := openCapabilityTestDB(t)
	defer db.Close()
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	body := []byte(`{
		"capability_type":"skill",
		"capability_id":"review-writer",
		"display_name":"Review Writer",
		"description":"Drafts code reviews",
		"version":"1.2.0",
		"source":"clawhub",
		"pricing":"free",
		"metadata":{"id":"review-writer","name":"Review Writer","author":"community"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/import-intent", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	AdminCapabilityImportIntentHandler(svc, settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Capability capability.CapabilitySummary `json:"capability"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Capability.CapabilityType != corelib.CapabilityTypeSkill || resp.Capability.Source != corelib.CapabilitySourceEnterpriseHub || resp.Capability.RelationToOrigin != "mirrored" {
		t.Fatalf("unexpected capability: %+v", resp.Capability)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(resp.Capability.MetadataJSON), &metadata); err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if metadata["origin_source"] != corelib.CapabilitySourceClawHub || metadata["skill_id"] != "review-writer" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}
}

func TestAdminCapabilityImportIntentImportsFreeHubCenterMCP(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capability-market/mcp/billing-api" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":                  "billing-api",
			"capability_id":       "billing-api",
			"publisher":           "acme",
			"display_name":        "Billing API MCP",
			"description":         "Billing tools",
			"version":             "1.2.3",
			"pricing":             map[string]any{"mode": "free"},
			"mcp":                 map[string]any{"id": "billing-api", "name": "Billing API", "endpoint_url": "https://billing.example.com/mcp", "auth_type": "bearer"},
			"secret_requirements": []any{map[string]any{"name": "api_token", "storage_policy": "hub_or_local"}},
		})
	}))
	defer server.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/import-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","capability_id":"billing-api","source":"hubcenter","pricing":"free"}`)))
	rec := httptest.NewRecorder()

	AdminCapabilityImportIntentHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Action     string                       `json:"action"`
		RequestID  string                       `json:"request_id"`
		Capability capability.CapabilitySummary `json:"capability"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Action != corelib.CapabilityInstallCreateImportRequest || resp.Capability.CapabilityType != corelib.CapabilityTypeMCP {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Capability.Source != corelib.CapabilitySourceEnterpriseHub || resp.Capability.RelationToOrigin != "mirrored" {
		t.Fatalf("unexpected imported capability: %+v", resp.Capability)
	}
	versions, err := svc.ListVersions(req.Context(), resp.Capability.ID)
	if err != nil || len(versions) != 1 || versions[0].Version != "1.2.3" {
		t.Fatalf("versions=%v err=%v", versions, err)
	}
	secrets, err := svc.ListMCPSecretRequirements(req.Context(), resp.Capability.ID, resp.Capability.CurrentVersionKey)
	if err != nil || len(secrets) != 1 || secrets[0].Name != "api_token" {
		t.Fatalf("secrets=%v err=%v", secrets, err)
	}
	acq, err := svc.GetAcquisitionRequest(req.Context(), resp.RequestID)
	if err != nil || acq.Status != "completed" || acq.ResultCapabilityID != resp.Capability.ID {
		t.Fatalf("acquisition=%+v err=%v", acq, err)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/import-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","capability_id":"billing-api","source":"hubcenter","pricing":"free"}`)))
	secondRec := httptest.NewRecorder()
	AdminCapabilityImportIntentHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
	var secondResp struct {
		Action     string                       `json:"action"`
		Capability capability.CapabilitySummary `json:"capability"`
	}
	if err := json.Unmarshal(secondRec.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if secondResp.Action != corelib.CapabilityInstallFromEnterpriseHub || secondResp.Capability.ID != resp.Capability.ID {
		t.Fatalf("unexpected second response: %+v", secondResp)
	}
	requests, err := svc.ListAcquisitionRequests(req.Context(), "")
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests=%+v err=%v", requests, err)
	}
}

func TestCapabilityInstallIntentRejectsFailedFreeImportRequest(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}

	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/missing-mcp/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"free"}`)))
	req.SetPathValue("id", "missing-mcp")
	rec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "rejected")
	if err != nil || len(items) != 1 || items[0].RequestKind != "import" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	if !strings.Contains(items[0].ApprovalJSON, "import_failed") {
		t.Fatalf("approval json = %s", items[0].ApprovalJSON)
	}
}
func TestCapabilityInstallIntentNormalizesSourceBeforeFreeHubCenterImport(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capability-market/mcp/free-mcp" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"capability_id": "free-mcp",
			"display_name":  "Free MCP",
			"version":       "1.0.0",
			"mcp":           map[string]any{"id": "free-mcp", "name": "Free MCP", "endpoint_url": "https://example.com/mcp"},
		})
	}))
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/free-mcp/install-intent", bytes.NewReader([]byte(`{"capability_type":"MCP","source":"HubCenter","pricing":"Free"}`)))
	req.SetPathValue("id", "free-mcp")
	rec := httptest.NewRecorder()

	CapabilityInstallIntentHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Action     string                        `json:"action"`
		RequestID  string                        `json:"request_id"`
		Capability *capability.CapabilitySummary `json:"capability"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Capability == nil || resp.Capability.ID == "" {
		t.Fatalf("expected imported capability, got %+v", resp)
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "completed")
	if err != nil || len(items) != 1 || items[0].Source != corelib.CapabilitySourceHubCenter || items[0].CapabilityType != corelib.CapabilityTypeMCP {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
func TestCapabilityInstallIntentImportsFreeHubCenterMCP(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capability-market/mcp/free-mcp" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":            "free-mcp",
			"capability_id": "free-mcp",
			"publisher":     "acme",
			"display_name":  "Free MCP",
			"description":   "Free tools",
			"version":       "1.0.0",
			"pricing":       map[string]any{"mode": "free"},
			"mcp":           map[string]any{"id": "free-mcp", "name": "Free MCP", "endpoint_url": "https://free.example.com/mcp", "auth_type": "none"},
		})
	}))
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/free-mcp/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"free"}`)))
	req.SetPathValue("id", "free-mcp")
	rec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Action     string                       `json:"action"`
		RequestID  string                       `json:"request_id"`
		Capability capability.CapabilitySummary `json:"capability"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Action != corelib.CapabilityInstallCreateImportRequest || resp.RequestID == "" || resp.Capability.ID == "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	acq, err := svc.GetAcquisitionRequest(context.Background(), resp.RequestID)
	if err != nil || acq.Status != "completed" || acq.ResultCapabilityID != resp.Capability.ID {
		t.Fatalf("acquisition=%+v err=%v", acq, err)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/capabilities/free-mcp/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"free"}`)))
	secondReq.SetPathValue("id", "free-mcp")
	secondRec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
	var secondResp struct {
		Action     string                       `json:"action"`
		Capability capability.CapabilitySummary `json:"capability"`
	}
	if err := json.Unmarshal(secondRec.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if secondResp.Action != corelib.CapabilityInstallFromEnterpriseHub || secondResp.Capability.ID != resp.Capability.ID {
		t.Fatalf("unexpected second response: %+v", secondResp)
	}
	requests, err := svc.ListAcquisitionRequests(context.Background(), "")
	if err != nil || len(requests) != 1 {
		t.Fatalf("requests=%+v err=%v", requests, err)
	}
}
func TestCapabilityInstallIntentUsesHubPolicy(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	searchOnly := true
	policy := corelib.DefaultCapabilityMarketPolicy()
	policy.EnterpriseOnlySearch = &searchOnly
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := settings.Set(context.Background(), capabilityMarketPolicySettingKey, string(raw)); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/external/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","capability_id":"external","source":"hubcenter","pricing":"free"}`)))
	rec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCapabilityInstallIntentDoesNotReuseDraftExternalCapability(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	if _, err := svc.UpsertCapability(context.Background(), capability.UpsertCapabilityInput{CapabilityType: corelib.CapabilityTypeMCP, CapabilityID: "draft-mcp", DisplayName: "Draft MCP", Source: corelib.CapabilitySourceEnterpriseHub, Status: "draft", MetadataJSON: `{"origin_source":"hubcenter","server_id":"draft-mcp"}`}); err != nil {
		t.Fatalf("seed draft capability: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/draft-mcp/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"paid","price":{"amount_cents":9900}}`)))
	req.SetPathValue("id", "draft-mcp")
	rec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["action"] != corelib.CapabilityInstallCreatePurchaseRequest {
		t.Fatalf("action=%v", resp["action"])
	}
}
func TestCapabilityInstallIntentDoesNotTreatNativeEnterpriseCapabilityAsPurchasedExternal(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	if _, err := svc.UpsertCapability(context.Background(), capability.UpsertCapabilityInput{CapabilityType: corelib.CapabilityTypeMCP, CapabilityID: "paid-one", DisplayName: "Native Paid One", Source: corelib.CapabilitySourceEnterpriseHub, Status: "approved"}); err != nil {
		t.Fatalf("seed native capability: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/paid-one/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"paid","price":{"amount_cents":9900}}`)))
	req.SetPathValue("id", "paid-one")
	rec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["action"] != corelib.CapabilityInstallCreatePurchaseRequest {
		t.Fatalf("action=%v", resp["action"])
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "pending_review")
	if err != nil || len(items) != 1 || items[0].RequestKind != "purchase" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
func TestCapabilityInstallIntentReusesOpenPurchaseRequest(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}

	body := []byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"paid","price":{"amount_cents":9900},"license":{"seats":5}}`)
	firstReq := httptest.NewRequest(http.MethodPost, "/api/capabilities/paid-one/install-intent", bytes.NewReader(body))
	firstReq.SetPathValue("id", "paid-one")
	firstRec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(firstRec, firstReq)
	if firstRec.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}
	var firstResp map[string]any
	if err := json.Unmarshal(firstRec.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/capabilities/paid-one/install-intent", bytes.NewReader(body))
	secondReq.SetPathValue("id", "paid-one")
	secondRec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(secondRec, secondReq)
	if secondRec.Code != http.StatusAccepted {
		t.Fatalf("second status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
	var secondResp map[string]any
	if err := json.Unmarshal(secondRec.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if firstResp["request_id"] == "" || secondResp["request_id"] != firstResp["request_id"] {
		t.Fatalf("request ids first=%v second=%v", firstResp["request_id"], secondResp["request_id"])
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "pending_review")
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}

func TestFindOpenAcquisitionRequestNormalizesStatusSourceAndKind(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	reqID, err := svc.CreateAcquisitionRequest(context.Background(), capability.AcquisitionRequestInput{CapabilityType: corelib.CapabilityTypeMCP, Source: "HubCenter", SourceCapabilityKey: "paid-case", SourceVersionKey: "v1", RequestKind: "Purchase"})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if err := svc.ApproveAcquisitionRequest(context.Background(), reqID, "admin", `{"mode":"test"}`); err != nil {
		t.Fatalf("approve request: %v", err)
	}

	found := findOpenAcquisitionRequest(context.Background(), svc, capabilityInstallIntentRequest{CapabilityType: corelib.CapabilityTypeMCP, Source: "hubcenter", CapabilityID: "paid-case", Version: "v1"}, "purchase")
	if found == nil || found.ID != reqID {
		t.Fatalf("found=%+v want %s", found, reqID)
	}
}
func TestCapabilityInstallIntentReusesOpenImportRequest(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}

	body := []byte(`{"capability_type":"skill","source":"github","pricing":"free","user_reason":"please import"}`)
	firstReq := httptest.NewRequest(http.MethodPost, "/api/capabilities/owner%2Frepo%2Fskill/install-intent", bytes.NewReader(body))
	firstReq.SetPathValue("id", "owner/repo/skill")
	firstRec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(firstRec, firstReq)
	if firstRec.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}
	var firstResp map[string]any
	if err := json.Unmarshal(firstRec.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/capabilities/owner%2Frepo%2Fskill/install-intent", bytes.NewReader(body))
	secondReq.SetPathValue("id", "owner/repo/skill")
	secondRec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(secondRec, secondReq)
	if secondRec.Code != http.StatusAccepted {
		t.Fatalf("second status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
	var secondResp map[string]any
	if err := json.Unmarshal(secondRec.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if firstResp["request_id"] == "" || secondResp["request_id"] != firstResp["request_id"] {
		t.Fatalf("request ids first=%v second=%v", firstResp["request_id"], secondResp["request_id"])
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "pending_review")
	if err != nil || len(items) != 1 || items[0].RequestKind != "import" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
func TestAdminCapabilityImportIntentReusesOpenPurchaseRequest(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	body := []byte(`{"capability_type":"mcp","capability_id":"paid-mcp","source":"hubcenter","pricing":"paid","price":{"amount_cents":9900}}`)

	firstReq := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/import-intent", bytes.NewReader(body))
	firstRec := httptest.NewRecorder()
	AdminCapabilityImportIntentHandler(svc, settings)(firstRec, firstReq)
	if firstRec.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", firstRec.Code, firstRec.Body.String())
	}
	var firstResp map[string]any
	if err := json.Unmarshal(firstRec.Body.Bytes(), &firstResp); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/import-intent", bytes.NewReader(body))
	secondRec := httptest.NewRecorder()
	AdminCapabilityImportIntentHandler(svc, settings)(secondRec, secondReq)
	if secondRec.Code != http.StatusAccepted {
		t.Fatalf("second status=%d body=%s", secondRec.Code, secondRec.Body.String())
	}
	var secondResp map[string]any
	if err := json.Unmarshal(secondRec.Body.Bytes(), &secondResp); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if firstResp["request_id"] == "" || secondResp["request_id"] != firstResp["request_id"] {
		t.Fatalf("request ids first=%v second=%v", firstResp["request_id"], secondResp["request_id"])
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "pending_review")
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
func TestCapabilityInstallIntentDirectOrPurchase(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	enterpriseOnlyInstall := false
	policy := corelib.DefaultCapabilityMarketPolicy()
	policy.EnterpriseOnlyInstall = &enterpriseOnlyInstall
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := settings.Set(context.Background(), capabilityMarketPolicySettingKey, string(raw)); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	freeReq := httptest.NewRequest(http.MethodPost, "/api/capabilities/free-one/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"free"}`)))
	freeReq.SetPathValue("id", "free-one")
	freeRec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(freeRec, freeReq)
	if freeRec.Code != http.StatusOK {
		t.Fatalf("free status=%d body=%s", freeRec.Code, freeRec.Body.String())
	}
	var freeResp map[string]any
	if err := json.Unmarshal(freeRec.Body.Bytes(), &freeResp); err != nil {
		t.Fatalf("decode free response: %v", err)
	}
	if freeResp["action"] != corelib.CapabilityInstallExternalDirect {
		t.Fatalf("free action=%v", freeResp["action"])
	}

	paidReq := httptest.NewRequest(http.MethodPost, "/api/capabilities/paid-one/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"paid","price":{"amount_cents":9900},"license":{"seats":5}}`)))
	paidReq.SetPathValue("id", "paid-one")
	paidRec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(paidRec, paidReq)
	if paidRec.Code != http.StatusAccepted {
		t.Fatalf("paid status=%d body=%s", paidRec.Code, paidRec.Body.String())
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "pending_review")
	if err != nil || len(items) != 1 {
		t.Fatalf("pending items=%v err=%v", items, err)
	}
	if items[0].RequestKind != "purchase" || items[0].PriceJSON == "{}" || items[0].LicenseJSON == "{}" {
		t.Fatalf("unexpected acquisition request: %+v", items[0])
	}
}

func TestCapabilityInstallIntentDirectFreeHubCenterMCPImportsCapability(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	enterpriseOnlyInstall := false
	policy := corelib.DefaultCapabilityMarketPolicy()
	policy.EnterpriseOnlyInstall = &enterpriseOnlyInstall
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := settings.Set(context.Background(), capabilityMarketPolicySettingKey, string(raw)); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capability-market/mcp/free-mcp" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":              "free-mcp",
			"capability_id":   "free-mcp",
			"capability_type": corelib.CapabilityTypeMCP,
			"display_name":    "Free MCP",
			"description":     "Free external MCP",
			"pricing":         map[string]any{"mode": "free"},
			"mcp":             map[string]any{"id": "free-mcp", "name": "Free MCP", "endpoint_url": "https://free.example.com/mcp", "auth_type": "none", "headers": map[string]any{"X-Tenant": "acme"}},
		})
	}))
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/free-mcp/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"free"}`)))
	req.SetPathValue("id", "free-mcp")
	rec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL}})(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Action     string                       `json:"action"`
		Capability capability.CapabilitySummary `json:"capability"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Action != corelib.CapabilityInstallExternalDirect || resp.Capability.ID == "" || resp.Capability.CapabilityType != corelib.CapabilityTypeMCP {
		t.Fatalf("unexpected response: %+v", resp)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(resp.Capability.MetadataJSON), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	headers, _ := metadata["headers"].(map[string]any)
	if headers["X-Tenant"] != "acme" {
		t.Fatalf("imported MCP headers were not preserved: %#v", metadata)
	}
	pricing, _ := metadata["pricing"].(map[string]any)
	if pricing["mode"] != corelib.CapabilityPricingFree {
		t.Fatalf("imported MCP pricing was not preserved: %#v", metadata)
	}
}

func TestAdminCapabilityApprovePaidHubCenterSkillCompletesPurchaseAndImport(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	if err := settings.Set(context.Background(), "admin_email", `{"value":"admin@example.com"}`); err != nil {
		t.Fatalf("set admin email: %v", err)
	}
	purchaseCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/capability-market/capabilities/paid-skill/download":
			purchaseCalled = true
			if r.URL.Query().Get("email") != "admin@example.com" {
				t.Fatalf("email=%q", r.URL.Query().Get("email"))
			}
			writeJSON(w, http.StatusOK, map[string]any{"skill_id": "paid-skill", "amount_paid": 9900, "encrypted_data": "secret-package"})
		case "/api/v1/skills/paid-skill":
			writeJSON(w, http.StatusOK, map[string]any{
				"id":          "paid-skill",
				"name":        "Paid Skill",
				"description": "Paid helper",
				"version":     "2.1.0",
				"author":      "vendor",
				"price":       9900,
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	reqID, err := svc.CreateAcquisitionRequest(context.Background(), capability.AcquisitionRequestInput{CapabilityType: corelib.CapabilityTypeSkill, Source: corelib.CapabilitySourceHubCenter, SourceCapabilityKey: "paid-skill", RequestKind: "purchase", PriceJSON: `{"amount_cents":9900}`})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/approve", bytes.NewReader([]byte(`{"approval":{"admin":"ok"}}`)))
	req.SetPathValue("id", reqID)
	rec := httptest.NewRecorder()

	AdminCapabilityApproveAcquisitionHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL, HubID: "hub-paid"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !purchaseCalled {
		t.Fatalf("expected skill purchase endpoint to be called")
	}
	acq, err := svc.GetAcquisitionRequest(req.Context(), reqID)
	if err != nil || acq.Status != "completed" || acq.ResultCapabilityID == "" || strings.Contains(acq.PurchaseJSON, "encrypted_data") {
		t.Fatalf("acquisition=%+v err=%v", acq, err)
	}
	created, err := svc.Get(req.Context(), acq.ResultCapabilityID)
	if err != nil || created.CapabilityType != corelib.CapabilityTypeSkill || created.Source != corelib.CapabilitySourceEnterpriseHub {
		t.Fatalf("created=%+v err=%v", created, err)
	}
}

func TestAdminCapabilityApprovePaidHubCenterMCPRequiresAdminEmailBeforePurchase(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	purchaseCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		purchaseCalled = true
		writeJSON(w, http.StatusOK, map[string]any{"purchase_id": "pur-1", "status": "purchased"})
	}))
	defer server.Close()
	reqID, err := svc.CreateAcquisitionRequest(context.Background(), capability.AcquisitionRequestInput{CapabilityType: corelib.CapabilityTypeMCP, Source: corelib.CapabilitySourceHubCenter, SourceCapabilityKey: "paid-mcp", RequestKind: "purchase", PriceJSON: `{"amount_cents":9900}`})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/approve", bytes.NewReader([]byte(`{"approval":{"admin":"ok"}}`)))
	req.SetPathValue("id", reqID)
	rec := httptest.NewRecorder()

	AdminCapabilityApproveAcquisitionHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL, HubID: "hub-paid"}})(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("approve status=%d body=%s", rec.Code, rec.Body.String())
	}
	if purchaseCalled {
		t.Fatalf("purchase endpoint should not be called without admin email")
	}
	acq, err := svc.GetAcquisitionRequest(req.Context(), reqID)
	if err != nil || acq.Status != "approved" {
		t.Fatalf("acquisition=%+v err=%v", acq, err)
	}
}
func TestAdminCapabilityApprovePaidHubCenterMCPCompletesPurchaseAndImport(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}
	if err := settings.Set(context.Background(), "admin_email", `{"value":"admin@example.com"}`); err != nil {
		t.Fatalf("set admin email: %v", err)
	}
	purchaseCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/capability-market/mcp/paid-mcp/purchase":
			purchaseCalled = true
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode purchase request: %v", err)
			}
			if body["admin_email"] != "admin@example.com" || body["hub_id"] != "hub-paid" {
				t.Fatalf("unexpected purchase request: %+v", body)
			}
			writeJSON(w, http.StatusOK, map[string]any{"purchase_id": "pur-1", "status": "purchased", "license": map[string]any{"seats": 10}})
		case "/api/capability-market/mcp/paid-mcp":
			writeJSON(w, http.StatusOK, map[string]any{
				"capability_id": "paid-mcp",
				"publisher":     "vendor",
				"display_name":  "Paid MCP",
				"version":       "3.0.0",
				"pricing":       map[string]any{"mode": "paid", "amount_cents": 9900},
				"mcp":           map[string]any{"id": "paid-mcp", "name": "Paid MCP", "endpoint_url": "https://paid.example.com/mcp"},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	reqID, err := svc.CreateAcquisitionRequest(context.Background(), capability.AcquisitionRequestInput{CapabilityType: corelib.CapabilityTypeMCP, Source: corelib.CapabilitySourceHubCenter, SourceCapabilityKey: "paid-mcp", RequestKind: "purchase", PriceJSON: `{"amount_cents":9900}`})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/approve", bytes.NewReader([]byte(`{"approval":{"admin":"ok"}}`)))
	req.SetPathValue("id", reqID)
	rec := httptest.NewRecorder()

	AdminCapabilityApproveAcquisitionHandler(svc, settings, fakeCapabilityMarketCenterStatus{state: &center.RegistrationState{ActiveBaseURL: server.URL, HubID: "hub-paid"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !purchaseCalled {
		t.Fatalf("expected purchase endpoint to be called")
	}
	acq, err := svc.GetAcquisitionRequest(req.Context(), reqID)
	if err != nil || acq.Status != "completed" || acq.ResultCapabilityID == "" || acq.PurchaseJSON == "{}" {
		t.Fatalf("acquisition=%+v err=%v", acq, err)
	}
	created, err := svc.Get(req.Context(), acq.ResultCapabilityID)
	if err != nil || created.Source != corelib.CapabilitySourceEnterpriseHub || created.RelationToOrigin != "mirrored" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
}

func TestAdminCapabilityAcquisitionApprovalFlow(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	reqID, err := svc.CreateAcquisitionRequest(context.Background(), capability.AcquisitionRequestInput{CapabilityType: "skill", Source: "hubcenter", SourceCapabilityKey: "paid-one", RequestKind: "purchase", PriceJSON: `{"amount_cents":9900}`})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/acquisition-requests/"+reqID, nil)
	detailReq.SetPathValue("id", reqID)
	detailRec := httptest.NewRecorder()
	AdminCapabilityAcquisitionRequestDetailHandler(svc)(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}
	var detail capability.AcquisitionRequest
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.PriceJSON == "{}" || detail.RequestKind != "purchase" {
		t.Fatalf("unexpected detail: %+v", detail)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/approve", bytes.NewReader([]byte(`{"approval":{"admin":"ok"}}`)))
	req.SetPathValue("id", reqID)
	rec := httptest.NewRecorder()
	AdminCapabilityApproveAcquisitionHandler(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", rec.Code, rec.Body.String())
	}

	items, err := svc.ListAcquisitionRequests(req.Context(), "approved")
	if err != nil || len(items) != 1 || items[0].ID != reqID {
		t.Fatalf("approved items=%v err=%v", items, err)
	}
}

func TestAdminCapabilityAcquisitionCompletedRequestIsTerminal(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	reqID, err := svc.CreateAcquisitionRequest(context.Background(), capability.AcquisitionRequestInput{CapabilityType: "skill", Source: "hubcenter", SourceCapabilityKey: "done-skill", RequestKind: "purchase"})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if err := svc.CompleteAcquisitionRequest(context.Background(), reqID, "capability-1", `{"mode":"test"}`); err != nil {
		t.Fatalf("complete request: %v", err)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/approve", bytes.NewReader([]byte(`{"approval":{"admin":"late"}}`)))
	approveReq.SetPathValue("id", reqID)
	approveRec := httptest.NewRecorder()
	AdminCapabilityApproveAcquisitionHandler(svc)(approveRec, approveReq)
	if approveRec.Code != http.StatusConflict {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	rejectReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/reject", bytes.NewReader([]byte(`{"approval":{"admin":"late"}}`)))
	rejectReq.SetPathValue("id", reqID)
	rejectRec := httptest.NewRecorder()
	AdminCapabilityRejectAcquisitionHandler(svc)(rejectRec, rejectReq)
	if rejectRec.Code != http.StatusConflict {
		t.Fatalf("reject status=%d body=%s", rejectRec.Code, rejectRec.Body.String())
	}

	completeReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/complete", bytes.NewReader([]byte(`{"result_capability_id":"capability-2"}`)))
	completeReq.SetPathValue("id", reqID)
	completeRec := httptest.NewRecorder()
	AdminCapabilityCompleteAcquisitionHandler(svc)(completeRec, completeReq)
	if completeRec.Code != http.StatusConflict {
		t.Fatalf("complete status=%d body=%s", completeRec.Code, completeRec.Body.String())
	}

	if err := svc.RejectAcquisitionRequest(context.Background(), reqID, "admin", `{"mode":"late"}`); !errors.Is(err, capability.ErrInvalidState) {
		t.Fatalf("terminal service reject err=%v", err)
	}

	item, err := svc.GetAcquisitionRequest(context.Background(), reqID)
	if err != nil || item.Status != "completed" || item.ResultCapabilityID != "capability-1" {
		t.Fatalf("item=%+v err=%v", item, err)
	}
}
func TestAdminCapabilityCompleteRequiresResultCapabilityID(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	reqID, err := svc.CreateAcquisitionRequest(context.Background(), capability.AcquisitionRequestInput{CapabilityType: "skill", Source: "hubcenter", SourceCapabilityKey: "manual-complete", RequestKind: "purchase"})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/complete", bytes.NewReader([]byte(`{"purchase":{"mode":"manual"}}`)))
	req.SetPathValue("id", reqID)
	rec := httptest.NewRecorder()
	AdminCapabilityCompleteAcquisitionHandler(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	item, err := svc.GetAcquisitionRequest(context.Background(), reqID)
	if err != nil || item.Status != "pending_review" || item.ResultCapabilityID != "" {
		t.Fatalf("item=%+v err=%v", item, err)
	}
}
func TestAdminCapabilityAcquisitionRejectedRequestCannotBeApprovedOrCompleted(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	reqID, err := svc.CreateAcquisitionRequest(context.Background(), capability.AcquisitionRequestInput{CapabilityType: "skill", Source: "hubcenter", SourceCapabilityKey: "rejected-skill", RequestKind: "purchase"})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if err := svc.RejectAcquisitionRequest(context.Background(), reqID, "admin", `{"mode":"test"}`); err != nil {
		t.Fatalf("reject request: %v", err)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/approve", bytes.NewReader([]byte(`{"approval":{"admin":"late"}}`)))
	approveReq.SetPathValue("id", reqID)
	approveRec := httptest.NewRecorder()
	AdminCapabilityApproveAcquisitionHandler(svc)(approveRec, approveReq)
	if approveRec.Code != http.StatusConflict {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	rejectReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/reject", bytes.NewReader([]byte(`{"approval":{"admin":"again"}}`)))
	rejectReq.SetPathValue("id", reqID)
	rejectRec := httptest.NewRecorder()
	AdminCapabilityRejectAcquisitionHandler(svc)(rejectRec, rejectReq)
	if rejectRec.Code != http.StatusConflict {
		t.Fatalf("reject status=%d body=%s", rejectRec.Code, rejectRec.Body.String())
	}

	completeReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/acquisition-requests/"+reqID+"/complete", bytes.NewReader([]byte(`{"result_capability_id":"capability-1"}`)))
	completeReq.SetPathValue("id", reqID)
	completeRec := httptest.NewRecorder()
	AdminCapabilityCompleteAcquisitionHandler(svc)(completeRec, completeReq)
	if completeRec.Code != http.StatusConflict {
		t.Fatalf("complete status=%d body=%s", completeRec.Code, completeRec.Body.String())
	}

	if err := svc.CompleteAcquisitionRequest(context.Background(), reqID, "capability-2", `{"mode":"late"}`); !errors.Is(err, capability.ErrInvalidState) {
		t.Fatalf("terminal service complete err=%v", err)
	}

	item, err := svc.GetAcquisitionRequest(context.Background(), reqID)
	if err != nil || item.Status != "rejected" || item.ResultCapabilityID != "" {
		t.Fatalf("item=%+v err=%v", item, err)
	}
}

func openCapabilityTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlite.RunMigrations(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestAdminMCPMarketplaceUpsertRouteIsWired(t *testing.T) {
	router, _ := newAdminRouterTestServices(t)
	globalToken := issueHubAdminToken(t, router)
	token := issueTenantAdminToken(t, router, globalToken, "acme", "market-admin")

	globalResp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/capability-market/mcp", map[string]any{}, globalToken)
	if globalResp.Code != http.StatusForbidden {
		t.Fatalf("global upsert route status=%d body=%s", globalResp.Code, globalResp.Body.String())
	}

	resp := doHubAdminJSONRequest(t, router, http.MethodPost, "/api/admin/capability-market/mcp", map[string]any{
		"publisher":     "acme",
		"capability_id": "billing-api",
		"display_name":  "Billing API MCP",
		"version":       "1.0.0",
		"mcp": map[string]any{
			"id":           "billing-api",
			"name":         "Billing API",
			"endpoint_url": "https://billing.example.com/mcp",
			"auth_type":    "bearer",
		},
		"secret_requirements": []map[string]any{{"name": "api_token", "storage_policy": "hub_or_local"}},
		"pricing":             map[string]any{"mode": "free"},
	}, token)
	if resp.Code != http.StatusOK {
		t.Fatalf("upsert route status=%d body=%s", resp.Code, resp.Body.String())
	}
	var created capability.CapabilitySummary
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.CapabilityType != corelib.CapabilityTypeMCP || created.CurrentVersionKey == "" {
		t.Fatalf("unexpected created capability: %+v", created)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/capabilities?type=mcp", nil)
	listReq.Header.Set("X-Tenant-ID", "tenant_acme")
	listResp := httptest.NewRecorder()
	router.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	if !strings.Contains(listResp.Body.String(), "billing-api") {
		t.Fatalf("created capability not visible in list: %s", listResp.Body.String())
	}

	editResp := doHubAdminJSONRequest(t, router, http.MethodPut, "/api/admin/capability-market/mcp", map[string]any{
		"publisher":     "acme",
		"capability_id": "billing-api",
		"display_name":  "Billing API MCP Edited",
		"version":       "1.0.1",
		"mcp": map[string]any{
			"id":           "billing-api",
			"name":         "Billing API",
			"endpoint_url": "https://billing.example.com/mcp",
			"auth_type":    "bearer",
		},
		"pricing": map[string]any{"mode": "free"},
	}, token)
	if editResp.Code != http.StatusOK {
		t.Fatalf("edit route status=%d body=%s", editResp.Code, editResp.Body.String())
	}
}
