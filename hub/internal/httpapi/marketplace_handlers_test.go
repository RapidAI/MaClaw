package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/capability"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	maclawappcontract "github.com/RapidAI/CodeClaw/internal/maclawappcontract"
	maclawapptest "github.com/RapidAI/CodeClaw/internal/testfixtures"
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
	if resp.Policy.EffectiveEnterpriseOnlyInstall() {
		t.Fatalf("enterprise_only_install should default to false")
	}
	if resp.Policy.EffectiveEnterpriseOnlySearch() {
		t.Fatalf("enterprise_only_search should default to false")
	}
	if resp.Policy.EffectivePreferredUploadTarget() != corelib.CapabilitySourceHubCenter {
		t.Fatalf("preferred_upload_target=%q, want hubcenter", resp.Policy.EffectivePreferredUploadTarget())
	}
}

func TestAdminCapabilityMarketPolicyUpdatePersistsDefaults(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	body := []byte(`{"policy":{"enterprise_only_search":true,"view_mode":"enterprise_only","preferred_upload_target":"enterprise_hub"}}`)
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
	if saved.EffectiveEnterpriseOnlyInstall() {
		t.Fatalf("enterprise_only_install should default to false when not explicitly set")
	}
	if !saved.EffectiveEnterpriseOnlySearch() {
		t.Fatalf("enterprise_only_search should be saved as true")
	}
	if saved.ViewMode != "enterprise_only" {
		t.Fatalf("view_mode=%q", saved.ViewMode)
	}
	if saved.EffectivePreferredUploadTarget() != corelib.CapabilitySourceEnterpriseHub {
		t.Fatalf("preferred_upload_target=%q", saved.EffectivePreferredUploadTarget())
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

func TestAdminCapabilityPolicyCreateCanonicalizesCapabilityRef(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	created := createTestCapability(t, svc, "skill", "canonical-skill", "Canonical Skill", "1.0.0")

	depReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/managed-deployments", bytes.NewReader([]byte(`{"capability_ref":"`+created.CapabilityID+`","deployment_policy":"required","scope":{"all_users":true}}`)))
	depRec := httptest.NewRecorder()
	AdminCapabilityManagedDeploymentCreateHandler(svc)(depRec, depReq)
	if depRec.Code != http.StatusCreated {
		t.Fatalf("deployment status=%d body=%s", depRec.Code, depRec.Body.String())
	}

	recReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/recommendations", bytes.NewReader([]byte(`{"capability_ref":"`+created.GlobalKey+`","recommendation_reason":"useful","scope":{"type":"group","group_id":"dept-a"}}`)))
	recRec := httptest.NewRecorder()
	AdminCapabilityRecommendationCreateHandler(svc)(recRec, recReq)
	if recRec.Code != http.StatusCreated {
		t.Fatalf("recommendation status=%d body=%s", recRec.Code, recRec.Body.String())
	}

	deployments, err := svc.ListManagedDeployments(context.Background())
	if err != nil || len(deployments) != 1 {
		t.Fatalf("deployments len=%d err=%v", len(deployments), err)
	}
	if deployments[0].CapabilityRef != created.ID {
		t.Fatalf("deployment capability_ref should be canonical id %q, got %q", created.ID, deployments[0].CapabilityRef)
	}
	recommendations, err := svc.ListRecommendations(context.Background())
	if err != nil || len(recommendations) != 1 {
		t.Fatalf("recommendations len=%d err=%v", len(recommendations), err)
	}
	if recommendations[0].CapabilityRef != created.ID {
		t.Fatalf("recommendation capability_ref should be canonical id %q, got %q", created.ID, recommendations[0].CapabilityRef)
	}

	viewerReq := httptest.NewRequest(http.MethodGet, "/api/capabilities/managed-deployments", nil)
	viewerReq.Header.Set("Authorization", "Bearer viewer-token")
	viewerRec := httptest.NewRecorder()
	CapabilityManagedDeploymentsHandler(svc, fakeMarketplaceViewerAuth{tenantID: store.DefaultTenantID, email: "dev@example.com"})(viewerRec, viewerReq)
	if viewerRec.Code != http.StatusOK {
		t.Fatalf("viewer deployment status=%d body=%s", viewerRec.Code, viewerRec.Body.String())
	}
	var viewerResp struct {
		Items []capability.Deployment `json:"items"`
	}
	if err := json.Unmarshal(viewerRec.Body.Bytes(), &viewerResp); err != nil {
		t.Fatalf("decode viewer deployments: %v", err)
	}
	if len(viewerResp.Items) != 1 || viewerResp.Items[0].CapabilityRef != created.ID {
		t.Fatalf("viewer deployments should expose canonical id: %+v", viewerResp.Items)
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

func TestCapabilitySkillSubmitAndDownloadAreTenantScoped(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	dataDir := t.TempDir()
	body, contentType := makeEnterpriseSkillUploadBody(t, map[string]string{
		"skill.yaml": "name: tenant-skill\ndescription: tenant upload\ntriggers:\n  - tenant\nsteps:\n  - action: bash\n    params:\n      command: echo ok\n",
		"README.md":  "tenant docs",
	})

	submitReq := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body)
	submitReq.Host = "enterprise.example"
	submitReq.Header.Set("Authorization", "Bearer viewer-token")
	submitReq.Header.Set("Content-Type", contentType)
	submitReq.Header.Set("X-Forwarded-Proto", "https")
	submitRec := httptest.NewRecorder()
	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "user-a", email: "dev@example.com"}, dataDir)(submitRec, submitReq)
	if submitRec.Code != http.StatusOK {
		t.Fatalf("submit status=%d body=%s", submitRec.Code, submitRec.Body.String())
	}

	items, err := svc.List(capability.WithTenant(context.Background(), "tenant_a"), corelib.CapabilityTypeSkill)
	if err != nil || len(items) != 1 {
		t.Fatalf("tenant_a capabilities=%+v err=%v", items, err)
	}
	if items[0].CapabilityID != "tenant-skill" || items[0].Source != corelib.CapabilitySourceEnterpriseHub || items[0].Status != "approved" {
		t.Fatalf("unexpected capability: %+v", items[0])
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(items[0].MetadataJSON), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["hub_url"] != "https://enterprise.example" || metadata["uploaded_by"] != "user-a" {
		t.Fatalf("metadata=%+v", metadata)
	}

	unauthReq := httptest.NewRequest(http.MethodGet, "/api/v1/skills/tenant-skill/download", nil)
	unauthReq.SetPathValue("id", "tenant-skill")
	unauthRec := httptest.NewRecorder()
	CapabilitySkillDownloadHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, dataDir)(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth download status=%d body=%s", unauthRec.Code, unauthRec.Body.String())
	}

	wrongTenantReq := httptest.NewRequest(http.MethodGet, "/api/v1/skills/tenant-skill/download", nil)
	wrongTenantReq.SetPathValue("id", "tenant-skill")
	wrongTenantReq.Header.Set("Authorization", "Bearer viewer-token")
	wrongTenantRec := httptest.NewRecorder()
	CapabilitySkillDownloadHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_b"}, dataDir)(wrongTenantRec, wrongTenantReq)
	if wrongTenantRec.Code != http.StatusNotFound {
		t.Fatalf("wrong tenant download status=%d body=%s", wrongTenantRec.Code, wrongTenantRec.Body.String())
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/v1/skills/tenant-skill/download", nil)
	downloadReq.SetPathValue("id", "tenant-skill")
	downloadReq.Header.Set("Authorization", "Bearer viewer-token")
	downloadRec := httptest.NewRecorder()
	CapabilitySkillDownloadHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, dataDir)(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download status=%d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
	var downloaded struct {
		ID    string            `json:"id"`
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(downloadRec.Body.Bytes(), &downloaded); err != nil {
		t.Fatalf("decode download: %v", err)
	}
	if downloaded.ID != "tenant-skill" || downloaded.Files["skill.yaml"] == "" || downloaded.Files["README.md"] == "" {
		t.Fatalf("downloaded=%+v", downloaded)
	}
	writeEnterpriseSkillPackageZip(t, enterpriseSkillPackageRoot(dataDir, "tenant_a"), safeEnterpriseSkillFileName("tenant-skill", shortEnterpriseSkillDigest("tenant-skill"))+"-stray.zip", map[string]string{
		"skill.yaml": "name: tenant-skill\ndescription: stray\n",
		"README.md":  "stray docs",
	})
	strayRec := httptest.NewRecorder()
	CapabilitySkillDownloadHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, dataDir)(strayRec, downloadReq)
	if strayRec.Code != http.StatusOK {
		t.Fatalf("download after stray package status=%d body=%s", strayRec.Code, strayRec.Body.String())
	}
	var afterStray struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(strayRec.Body.Bytes(), &afterStray); err != nil {
		t.Fatalf("decode stray download: %v", err)
	}
	readme, err := base64.StdEncoding.DecodeString(afterStray.Files["README.md"])
	if err != nil || string(readme) != "tenant docs" {
		t.Fatalf("download used wrong package readme=%q err=%v", string(readme), err)
	}
	_, err = svc.UpsertCapability(capability.WithTenant(context.Background(), "tenant_a"), capability.UpsertCapabilityInput{
		CapabilityType: corelib.CapabilityTypeSkill,
		Publisher:      "legacy",
		CapabilityID:   "tenant-skill",
		GlobalKey:      corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":legacy:tenant-skill",
		DisplayName:    "Legacy Tenant Skill",
		Description:    "legacy duplicate without package file",
		Source:         corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:      "legacy",
		Status:         "approved",
		MetadataJSON:   `{"skill_id":"tenant-skill"}`,
	})
	if err != nil {
		t.Fatalf("create legacy duplicate: %v", err)
	}
	legacyRec := httptest.NewRecorder()
	CapabilitySkillDownloadHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, dataDir)(legacyRec, downloadReq)
	if legacyRec.Code != http.StatusOK {
		t.Fatalf("download with legacy duplicate status=%d body=%s", legacyRec.Code, legacyRec.Body.String())
	}
	writeEnterpriseSkillPackageZip(t, enterpriseSkillPackageRoot(dataDir, "tenant_a"), "other.zip", map[string]string{
		"skill.yaml": "name: other-skill\ndescription: wrong package\n",
		"README.md":  "wrong docs",
	})
	badPackageMetadata := map[string]any{"skill_id": "tenant-skill", "package_file": "other.zip"}
	_, err = svc.UpsertCapability(capability.WithTenant(context.Background(), "tenant_a"), capability.UpsertCapabilityInput{
		CapabilityType: corelib.CapabilityTypeSkill,
		Publisher:      "legacy",
		CapabilityID:   "tenant-skill",
		GlobalKey:      corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":legacy-bad-package:tenant-skill",
		DisplayName:    "Legacy Bad Package Tenant Skill",
		Description:    "legacy duplicate with wrong package file",
		Source:         corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:      "legacy",
		Status:         "approved",
		MetadataJSON:   jsonObjectString(badPackageMetadata),
	})
	if err != nil {
		t.Fatalf("create bad package duplicate: %v", err)
	}
	badDuplicateRec := httptest.NewRecorder()
	CapabilitySkillDownloadHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, dataDir)(badDuplicateRec, downloadReq)
	if badDuplicateRec.Code != http.StatusOK {
		t.Fatalf("download with bad package duplicate status=%d body=%s", badDuplicateRec.Code, badDuplicateRec.Body.String())
	}
	var afterBadDuplicate struct {
		Files map[string]string `json:"files"`
	}
	if err := json.Unmarshal(badDuplicateRec.Body.Bytes(), &afterBadDuplicate); err != nil {
		t.Fatalf("decode bad duplicate download: %v", err)
	}
	readme, err = base64.StdEncoding.DecodeString(afterBadDuplicate.Files["README.md"])
	if err != nil || string(readme) != "tenant docs" {
		t.Fatalf("download with bad duplicate used wrong package readme=%q err=%v", string(readme), err)
	}
	metadata["package_file"] = "other.zip"
	_, err = svc.UpsertCapability(capability.WithTenant(context.Background(), "tenant_a"), capability.UpsertCapabilityInput{
		ID:             items[0].ID,
		CapabilityType: corelib.CapabilityTypeSkill,
		Publisher:      items[0].Publisher,
		CapabilityID:   items[0].CapabilityID,
		DisplayName:    items[0].DisplayName,
		Description:    items[0].Description,
		Source:         corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:      items[0].ManagedBy,
		Status:         "approved",
		MetadataJSON:   jsonObjectString(metadata),
	})
	if err != nil {
		t.Fatalf("point package to wrong skill: %v", err)
	}
	mismatchRec := httptest.NewRecorder()
	CapabilitySkillDownloadHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, dataDir)(mismatchRec, downloadReq)
	if mismatchRec.Code != http.StatusNotFound {
		t.Fatalf("mismatched package download status=%d body=%s", mismatchRec.Code, mismatchRec.Body.String())
	}
	if err := json.Unmarshal([]byte(items[0].MetadataJSON), &metadata); err != nil {
		t.Fatalf("restore metadata: %v", err)
	}

	_, err = svc.UpsertCapability(capability.WithTenant(context.Background(), "tenant_a"), capability.UpsertCapabilityInput{
		ID:             items[0].ID,
		CapabilityType: corelib.CapabilityTypeSkill,
		Publisher:      items[0].Publisher,
		CapabilityID:   items[0].CapabilityID,
		DisplayName:    items[0].DisplayName,
		Description:    items[0].Description,
		Source:         corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:      items[0].ManagedBy,
		Status:         "draft",
		MetadataJSON:   jsonObjectString(metadata),
	})
	if err != nil {
		t.Fatalf("mark draft: %v", err)
	}
	draftRec := httptest.NewRecorder()
	CapabilitySkillDownloadHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, dataDir)(draftRec, downloadReq)
	if draftRec.Code != http.StatusNotFound {
		t.Fatalf("draft capability download status=%d body=%s", draftRec.Code, draftRec.Body.String())
	}
}

func TestCapabilityMaclawAppSubmitCreatesPendingReviewCapability(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body := []byte(`{
		"package": {
			"schema": "maclaw.app.pack.v1",
			"privateMarker": "x_maclaw_apps",
			"apps": [{
				"schema": "maclaw.app.v1",
				"privateMarker": "x_maclaw_apps",
				"app": {
					"id": "contract-archive-app",
					"name": "Contract Archive",
					"description": "Archive reviewed contracts",
					"kind": "tool_app",
					"version": "3",
						"ui": {
							"schema": "maclaw.app.ui.v1",
							"entry": "tool_workspace",
							"generated": true,
						"layouts": {
							"tool_workspace": {
								"template": "document_workspace",
								"density": "compact",
								"primaryRegion": "file_queue",
								"outputRegion": "output_panel",
								"navigation": ["input", "output"],
								"list": {"columns": ["title", "status"]},
								"regions": [
									{"id": "file_queue", "role": "input", "placement": "left"},
									{"id": "output_panel", "role": "output", "placement": "right"}
								]
								}
							}
						},
						"binding": {
							"appSkill": {"id": "contract-app-skill", "version": "3.0.0", "source": "hub"},
							"dependencies": {"skills": [
								{"id": "contract-app-skill", "version": "3.0.0", "kind": "runtime_skill", "required": true, "source": "hub"},
								{"id": "contract-approval", "version": "1.2.0", "kind": "workflow_skill", "required": true, "source": "hub", "capabilities": ["approval.workflow"]}
							]},
							"workflow": {
								"schema": "maclaw.app.workflow.v1",
								"submitNode": "contract.submit",
								"approvalNode": "contract.legal_review",
								"resultNode": "contract.result",
								"attentionNode": "contract.attention",
								"statusMapping": {"pending": "approval_pending", "approved": "approved", "rejected": "rejected", "attention": "attention", "requiresInput": "requires_input"}
							}
						},
						"governance": {
							"resultContract": {"primary": "artifact", "types": ["artifact", "content"]},
							"workflowContract": {"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "contract-approval", "objectRole": "contract", "requiredOutputs": ["workflow_result", "approval_instance", "outputs", "artifacts"]},
							"testEvidence": {
								"runId": "run-contract",
								"testProtocolFingerprint": "proto-contract",
								"primaryResult": "artifact",
								"resultPayload": {"business_status": "archived", "business_record": {"id": "contract-1"}},
								"outputs": [{"kind": "document", "title": "Archived contract", "artifact_id": "artifact-contract"}],
								"artifacts": [{"id": "artifact-contract", "uri": "artifact://contract/archive.pdf", "name": "archive.pdf"}],
								"resultCoverage": {"ok": true, "primary": "artifact", "coveredTypes": ["artifact", "content"], "missingTypes": []},
								"approvalInstance": {"approvalID": "approval-contract-1", "recordID": "contract-1", "datasetID": "legal.contracts", "objectRole": "contract", "approvalEvent": "contract.submitted", "approvalWorkflowID": "contract-approval", "status": "approved", "currentNode": "contract.result", "workflowSkillId": "contract-approval", "workflowVersion": "1.2.0", "businessStatus": "archived", "resultStatus": "approved", "resultPayload": {"approval_result": "approved", "business_status": "archived", "business_record": {"id": "contract-1"}}, "outputs": [{"kind": "approval_result", "title": "Approval", "text": "approved", "status": "approved"}], "artifacts": [{"id": "artifact-contract", "uri": "artifact://contract/archive.pdf", "name": "archive.pdf"}], "approvalInstanceViewVerified": true}
							},
						"dependencyVerification": {"schema": "maclaw.app.install_plan.v1", "runId": "dep-run-contract", "verifiedAt": "2026-06-29T12:00:00Z", "dependencyCount": 2, "requiredCount": 2, "installedCount": 2, "missingCount": 0, "blockedCount": 0, "ok": true, "blocked": false, "skills": [{"id": "contract-app-skill", "version": "3.0.0", "kind": "runtime_skill", "install_ref": "hub://skills/contract-app-skill@3.0.0"}, {"id": "contract-approval", "version": "1.2.0", "kind": "workflow_skill", "install_ref": "hub://skills/contract-approval@1.2.0"}], "installPlan": {"schema": "maclaw.app.install_plan.v1", "source": "hub", "required_skill_count": 2}}
					}
				}
			}]
		},
		"source_submission_id": "local-review-contract"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/maclaw-apps/submit", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()

	CapabilityMaclawAppSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "author-a", email: "author@example.com"})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("submit status=%d body=%s", rec.Code, rec.Body.String())
	}
	var submitResp struct {
		Schema        string           `json:"schema"`
		Status        string           `json:"status"`
		PackageSHA256 string           `json:"package_sha256"`
		AppCount      int              `json:"app_count"`
		Submissions   []map[string]any `json:"submissions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &submitResp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if submitResp.Schema != "maclaw.app.hub_submission.v1" || submitResp.Status != "pending_review" || submitResp.AppCount != 1 || submitResp.PackageSHA256 == "" || len(submitResp.Submissions) != 1 {
		t.Fatalf("unexpected submit response: %+v", submitResp)
	}
	items, err := svc.List(capability.WithTenant(context.Background(), "tenant_a"), corelib.CapabilityTypeSkill)
	if err != nil || len(items) != 1 {
		t.Fatalf("tenant_a capabilities=%+v err=%v", items, err)
	}
	item := items[0]
	if item.CapabilityID != "contract-archive-app" || item.Status != "pending_review" || item.Source != corelib.CapabilitySourceEnterpriseHub || item.CurrentVersionKey == "" {
		t.Fatalf("unexpected maclaw app capability: %+v", item)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["is_maclaw_app"] != true || metadata["product_kind"] != "maclaw_app_skill" || metadata["maclaw_app_id"] != "contract-archive-app" || metadata["review_state"] != "pending_review" {
		t.Fatalf("missing maclaw app metadata: %+v", metadata)
	}
	if metadata["source_submission_id"] != "local-review-contract" || metadata["uploaded_by"] != "author-a" || metadata["publisher_email"] != "author@example.com" {
		t.Fatalf("missing submitter metadata: %+v", metadata)
	}
	layout := metadata["workspace_layout"].(map[string]any)
	if layout["template"] != "document_workspace" || layout["primaryRegion"] != "file_queue" || layout["outputRegion"] != "output_panel" {
		t.Fatalf("workspace layout metadata=%+v", layout)
	}
	regions, ok := layout["regions"].([]any)
	if !ok || len(regions) != 2 {
		t.Fatalf("workspace layout should preserve regions: %+v", layout)
	}
	if metadata["workspace_layout_primary_region"] != "file_queue" || metadata["workspace_layout_output_region"] != "output_panel" || metadata["workspace_layout_region_count"] != float64(2) {
		t.Fatalf("workspace layout summaries=%+v", metadata)
	}
	appSkill := metadata["app_skill"].(map[string]any)
	if appSkill["id"] != "contract-app-skill" || metadata["app_skill_id"] != "contract-app-skill" || metadata["app_skill_version"] != "3.0.0" {
		t.Fatalf("app skill metadata=%+v", metadata)
	}
	dependencies, ok := metadata["dependencies"].([]any)
	if !ok || len(dependencies) != 2 || metadata["skill_dependency_count"] != float64(2) || metadata["required_skill_dependency_count"] != float64(2) {
		t.Fatalf("dependency metadata=%+v", metadata)
	}
	workflowIDs, ok := metadata["workflow_skill_ids"].([]any)
	if !ok || len(workflowIDs) != 1 || workflowIDs[0] != "contract-approval" {
		t.Fatalf("workflow skill metadata=%+v", metadata)
	}
	dependencyVerification, ok := metadata["dependency_verification"].(map[string]any)
	if !ok || dependencyVerification["schema"] != "maclaw.app.install_plan.v1" || dependencyVerification["runId"] != "dep-run-contract" {
		t.Fatalf("dependency verification metadata=%+v", metadata)
	}
	if metadata["dependency_verification_schema"] != "maclaw.app.install_plan.v1" || metadata["dependency_verification_run_id"] != "dep-run-contract" || metadata["dependency_verification_ok"] != true || metadata["dependency_verification_blocked"] != false {
		t.Fatalf("dependency verification summaries missing ids/status: %+v", metadata)
	}
	if metadata["dependency_verification_dependency_count"] != float64(2) || metadata["dependency_verification_required_count"] != float64(2) || metadata["dependency_verification_installed_count"] != float64(2) || metadata["dependency_verification_missing_count"] != float64(0) || metadata["dependency_verification_blocked_count"] != float64(0) {
		t.Fatalf("dependency verification summaries missing counts: %+v", metadata)
	}
	if verifiedSkills, ok := metadata["dependency_verification_skills"].([]any); !ok || len(verifiedSkills) != 2 || metadata["dependency_verification_skill_count"] != float64(2) {
		t.Fatalf("dependency verification should expose checked skills: %+v", metadata)
	}
	if installPlan, ok := metadata["dependency_verification_install_plan"].(map[string]any); !ok || installPlan["source"] != "hub" || installPlan["required_skill_count"] != float64(2) {
		t.Fatalf("dependency verification should expose install plan: %+v", metadata)
	}
	resultContract := metadata["result_contract"].(map[string]any)
	if resultContract["primary"] != "artifact" {
		t.Fatalf("result contract metadata=%+v", resultContract)
	}
	workflowContract := metadata["workflow_contract"].(map[string]any)
	if workflowContract["workflowSkillId"] != "contract-approval" || workflowContract["objectRole"] != "contract" {
		t.Fatalf("workflow contract metadata=%+v", workflowContract)
	}
	workflowMapping := metadata["workflow_mapping"].(map[string]any)
	if workflowMapping["approvalNode"] != "contract.legal_review" || metadata["workflow_approval_node"] != "contract.legal_review" || metadata["workflow_result_node"] != "contract.result" {
		t.Fatalf("workflow mapping metadata=%+v", metadata)
	}
	testEvidence := metadata["maclaw_app_test_evidence"].(map[string]any)
	if testEvidence["runId"] != "run-contract" || testEvidence["testProtocolFingerprint"] != "proto-contract" {
		t.Fatalf("test evidence metadata=%+v", testEvidence)
	}
	if outputs, ok := testEvidence["outputs"].([]any); !ok || len(outputs) != 1 {
		t.Fatalf("test evidence should preserve outputs: %+v", testEvidence)
	}
	if artifacts, ok := testEvidence["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("test evidence should preserve artifacts: %+v", testEvidence)
	}
	if metadata["test_evidence_run_id"] != "run-contract" || metadata["test_evidence_test_protocol_fingerprint"] != "proto-contract" || metadata["test_evidence_primary_result"] != "artifact" {
		t.Fatalf("test evidence summary metadata missing ids/results: %+v", metadata)
	}
	if metadata["test_evidence_output_count"] != float64(1) || metadata["test_evidence_artifact_count"] != float64(1) {
		t.Fatalf("test evidence summary metadata missing counts: %+v", metadata)
	}
	if payload, ok := metadata["test_evidence_result_payload"].(map[string]any); !ok || payload["business_status"] != "archived" {
		t.Fatalf("test evidence summary metadata should expose result payload: %+v", metadata)
	}
	if coverage, ok := metadata["test_evidence_result_coverage"].(map[string]any); !ok || coverage["ok"] != true || coverage["primary"] != "artifact" {
		t.Fatalf("test evidence summary metadata should expose result coverage: %+v", metadata)
	}
	if metadata["test_evidence_result_coverage_ok"] != true || metadata["test_evidence_result_coverage_primary"] != "artifact" {
		t.Fatalf("test evidence coverage summary missing: %+v", metadata)
	}
	if covered, ok := metadata["test_evidence_covered_types"].([]any); !ok || len(covered) != 2 || covered[0] != "artifact" || covered[1] != "content" {
		t.Fatalf("test evidence covered types summary missing: %+v", metadata)
	}
	for key, want := range map[string]any{
		"test_evidence_approval_current_node": "contract.result",
		"test_evidence_workflow_skill_id":     "contract-approval",
		"test_evidence_workflow_version":      "1.2.0",
		"test_evidence_business_status":       "archived",
		"test_evidence_result_status":         "approved",
		"test_evidence_dataset_id":            "legal.contracts",
		"test_evidence_object_role":           "contract",
		"test_evidence_approval_event":        "contract.submitted",
		"test_evidence_approval_workflow_id":  "contract-approval",
	} {
		if got := metadata[key]; got != want {
			t.Fatalf("test evidence approval summary %s=%#v want %#v; metadata=%+v", key, got, want, metadata)
		}
	}
	approvalInstance := testEvidence["approvalInstance"].(map[string]any)
	if approvalInstance["approvalID"] != "approval-contract-1" || approvalInstance["recordID"] != "contract-1" || approvalInstance["status"] != "approved" {
		t.Fatalf("test evidence should preserve approval instance: %+v", testEvidence)
	}
	if approvalInstance["currentNode"] != "contract.result" || approvalInstance["workflowSkillId"] != "contract-approval" || approvalInstance["businessStatus"] != "archived" || approvalInstance["resultStatus"] != "approved" {
		t.Fatalf("test evidence should preserve approval instance workflow/result fields: %+v", approvalInstance)
	}
	if payload, ok := approvalInstance["resultPayload"].(map[string]any); !ok || payload["business_status"] != "archived" {
		t.Fatalf("test evidence should preserve approval instance result payload: %+v", approvalInstance)
	}
	if outputs, ok := approvalInstance["outputs"].([]any); !ok || len(outputs) != 1 {
		t.Fatalf("test evidence should preserve approval instance outputs: %+v", approvalInstance)
	}
	if artifacts, ok := approvalInstance["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("test evidence should preserve approval instance artifacts: %+v", approvalInstance)
	}
	versions, err := svc.ListVersions(capability.WithTenant(context.Background(), "tenant_a"), item.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("submitted versions=%+v err=%v", versions, err)
	}
	var storedEntry map[string]any
	if err := json.Unmarshal([]byte(versions[0].ManifestJSON), &storedEntry); err != nil {
		t.Fatalf("decode stored manifest: %v", err)
	}
	if storedEntry["schema"] != "maclaw.app.v1" || storedEntry["privateMarker"] != "x_maclaw_apps" {
		t.Fatalf("stored manifest should preserve app entry envelope: %+v", storedEntry)
	}
	storedApp, _ := storedEntry["app"].(map[string]any)
	storedUI, _ := storedApp["ui"].(map[string]any)
	storedLayouts, _ := storedUI["layouts"].(map[string]any)
	storedLayout, _ := storedLayouts["tool_workspace"].(map[string]any)
	if storedLayout["primaryRegion"] != "file_queue" || storedLayout["outputRegion"] != "output_panel" {
		t.Fatalf("stored manifest should preserve workspace placement: %+v", storedLayout)
	}
	if storedRegions, ok := storedLayout["regions"].([]any); !ok || len(storedRegions) != 2 {
		t.Fatalf("stored manifest should preserve workspace regions: %+v", storedLayout)
	}
	storedBinding, _ := storedApp["binding"].(map[string]any)
	storedDependencies, _ := storedBinding["dependencies"].(map[string]any)
	if storedSkills, ok := storedDependencies["skills"].([]any); !ok || len(storedSkills) != 2 {
		t.Fatalf("stored manifest should preserve declared skill dependencies: %+v", storedDependencies)
	}
	storedWorkflow, _ := storedBinding["workflow"].(map[string]any)
	if storedWorkflow["approvalNode"] != "contract.legal_review" || storedWorkflow["resultNode"] != "contract.result" {
		t.Fatalf("stored manifest should preserve workflow mapping: %+v", storedWorkflow)
	}
	storedGovernance, _ := storedApp["governance"].(map[string]any)
	storedDependencyVerification, _ := storedGovernance["dependencyVerification"].(map[string]any)
	if storedDependencyVerification["runId"] != "dep-run-contract" || storedDependencyVerification["dependencyCount"] != float64(2) {
		t.Fatalf("stored manifest should preserve dependency verification evidence: %+v", storedGovernance)
	}
	storedResultContract, _ := storedGovernance["resultContract"].(map[string]any)
	if storedResultContract["primary"] != "artifact" {
		t.Fatalf("stored manifest should preserve result contract: %+v", storedResultContract)
	}
	storedTestEvidence, _ := storedGovernance["testEvidence"].(map[string]any)
	if storedOutputs, ok := storedTestEvidence["outputs"].([]any); !ok || len(storedOutputs) != 1 {
		t.Fatalf("stored manifest should preserve test evidence outputs: %+v", storedTestEvidence)
	}
	storedApprovalInstance, _ := storedTestEvidence["approvalInstance"].(map[string]any)
	if storedApprovalInstance["currentNode"] != "contract.result" || storedApprovalInstance["workflowSkillId"] != "contract-approval" {
		t.Fatalf("stored manifest should preserve approval instance workflow fields: %+v", storedApprovalInstance)
	}
	if storedPayload, ok := storedApprovalInstance["resultPayload"].(map[string]any); !ok || storedPayload["business_status"] != "archived" {
		t.Fatalf("stored manifest should preserve approval instance result payload: %+v", storedApprovalInstance)
	}
	tenantBItems, err := svc.List(capability.WithTenant(context.Background(), "tenant_b"), corelib.CapabilityTypeSkill)
	if err != nil || len(tenantBItems) != 0 {
		t.Fatalf("tenant_b capabilities=%+v err=%v", tenantBItems, err)
	}
}

func TestCapabilityMaclawAppSubmitRejectsUnreadyPackage(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(pkg map[string]any)
		wantMessage string
	}{
		{
			name: "missing test evidence",
			mutate: func(pkg map[string]any) {
				delete(readyMaclawAppGovernance(pkg), "testEvidence")
			},
			wantMessage: "test evidence is required",
		},
		{
			name: "missing result contract",
			mutate: func(pkg map[string]any) {
				delete(readyMaclawAppGovernance(pkg), "resultContract")
			},
			wantMessage: "result contract is required",
		},
		{
			name: "dependency verification blocked",
			mutate: func(pkg map[string]any) {
				verification := readyMaclawAppGovernance(pkg)["dependencyVerification"].(map[string]any)
				verification["ok"] = false
				verification["blocked"] = true
				verification["missingCount"] = 1
				verification["blockedCount"] = 1
			},
			wantMessage: "dependency verification failed",
		},
		{
			name: "approval app missing workflow contract",
			mutate: func(pkg map[string]any) {
				delete(readyMaclawAppGovernance(pkg), "workflowContract")
			},
			wantMessage: "approval app requires workflow contract",
		},
		{
			name: "approval app workflow contract missing required outputs",
			mutate: func(pkg map[string]any) {
				contract := readyMaclawAppGovernance(pkg)["workflowContract"].(map[string]any)
				contract["requiredOutputs"] = []any{"workflow_result", "approval_instance"}
			},
			wantMessage: "workflow contract must include required output outputs",
		},
		{
			name: "workspace layout fingerprint mismatch",
			mutate: func(pkg map[string]any) {
				layout := readyMaclawAppGovernance(pkg)["workspaceLayout"].(map[string]any)
				layout["entry"] = "approval_workspace"
				layout["template"] = "classic_split"
				layout["density"] = "comfortable"
				layout["fingerprint"] = "deadbeef"
			},
			wantMessage: "workspace layout fingerprint does not match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openCapabilityTestDB(t)
			svc := capability.NewService(db)
			pkg := readyEnterpriseApprovalMaclawAppSubmitPackage()
			tt.mutate(pkg)
			body, err := json.Marshal(map[string]any{"package": pkg, "source_submission_id": "local-review-ready-gate"})
			if err != nil {
				t.Fatalf("encode package: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/capabilities/maclaw-apps/submit", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer viewer-token")
			rec := httptest.NewRecorder()

			CapabilityMaclawAppSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "author-a", email: "author@example.com"})(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("submit status=%d body=%s", rec.Code, rec.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if resp["code"] != "MACLAW_APP_PACKAGE_NOT_READY" || !strings.Contains(stringFromAny(resp["message"]), tt.wantMessage) {
				t.Fatalf("unexpected error response=%+v want message containing %q", resp, tt.wantMessage)
			}
			issues, ok := resp["review_issues"].([]any)
			if !ok || len(issues) == 0 {
				t.Fatalf("error response should include review issues: %+v", resp)
			}
			items, err := svc.List(capability.WithTenant(context.Background(), "tenant_a"), corelib.CapabilityTypeSkill)
			if err != nil {
				t.Fatalf("list capabilities: %v", err)
			}
			if len(items) != 0 {
				t.Fatalf("unready package should not create capability: %+v", items)
			}
		})
	}
}

func readyEnterpriseApprovalMaclawAppSubmitPackage() map[string]any {
	return maclawapptest.ReadyEnterpriseApprovalMaclawAppSubmitPackage()
}

func readyMaclawAppEntry(pkg map[string]any) map[string]any {
	apps := pkg["apps"].([]any)
	return apps[0].(map[string]any)
}
func readyMaclawAppGovernance(pkg map[string]any) map[string]any {
	apps := pkg["apps"].([]any)
	entry := apps[0].(map[string]any)
	app := entry["app"].(map[string]any)
	return app["governance"].(map[string]any)
}
func TestCapabilityMaclawAppPackageDownloadReturnsApprovedPack(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), "tenant_a")
	manifest := map[string]any{
		"schema":        "maclaw.app.v1",
		"privateMarker": "x_maclaw_apps",
		"app": map[string]any{
			"id":          "download-app",
			"name":        "Download App",
			"description": "Downloaded from Hub",
			"kind":        "tool_app",
			"binding": map[string]any{
				"skill": map[string]any{
					"id":                "download-app-skill",
					"version":           "1.0.0",
					"source":            "hub",
					"appDefinitionFile": "maclaw.app.json",
					"inputMode":         "form",
				},
				"dependencies": map[string]any{
					"skills": []any{
						map[string]any{
							"id":          "download-app-skill",
							"version":     "1.0.0",
							"kind":        "app_skill",
							"required":    true,
							"source":      "hub",
							"install_ref": "cap-download-app-skill",
						},
						map[string]any{
							"id":           "download-workflow",
							"version":      "1.0.0",
							"kind":         "workflow_skill",
							"required":     true,
							"source":       "hub",
							"install_ref":  "cap-download-workflow",
							"capabilities": []any{"approval.workflow"},
						},
					},
				},
			},
			"ui": map[string]any{
				"schema":    "maclaw.app.ui.v1",
				"entry":     "tool_workspace",
				"generated": true,
				"layouts": map[string]any{
					"tool_workspace": map[string]any{
						"template":      "document_workspace",
						"density":       "compact",
						"primaryRegion": "file_queue",
						"outputRegion":  "output_panel",
						"regions": []any{
							map[string]any{"id": "file_queue", "role": "input", "placement": "left"},
							map[string]any{"id": "output_panel", "role": "output", "placement": "right"},
						},
					},
				},
			},
			"governance": map[string]any{
				"workflowContract": map[string]any{"schema": "maclaw.app.workflow_contract.v1", "workflowSkillId": "download-workflow", "objectRole": "download_record", "requiredOutputs": []any{"workflow_result", "approval_instance", "outputs", "artifacts"}},
				"testEvidence": map[string]any{
					"runId": "run-download",
					"outputs": []any{
						map[string]any{"kind": "table", "title": "Rows", "data": map[string]any{"rows": []any{map[string]any{"id": "download-1"}}}},
					},
					"artifacts": []any{map[string]any{"id": "artifact-download", "uri": "artifact://download/result.pdf", "name": "result.pdf"}},
					"approvalInstance": map[string]any{
						"approvalID":                   "approval-download-1",
						"recordID":                     "download-1",
						"datasetID":                    "downloads",
						"objectRole":                   "download_record",
						"approvalEvent":                "download.submitted",
						"approvalWorkflowID":           "download-workflow",
						"status":                       "approved",
						"currentNode":                  "download.result",
						"workflowSkillId":              "download-workflow",
						"workflowVersion":              "1.0.0",
						"businessStatus":               "ready",
						"resultStatus":                 "approved",
						"resultPayload":                map[string]any{"approval_result": "approved", "business_status": "ready", "business_record": map[string]any{"id": "download-1"}},
						"outputs":                      []any{map[string]any{"kind": "approval_result", "title": "Approval", "text": "approved", "status": "approved"}},
						"artifacts":                    []any{map[string]any{"id": "artifact-download", "uri": "artifact://download/result.pdf", "name": "result.pdf"}},
						"approvalInstanceViewVerified": true,
					},
				},
			},
		},
	}
	seeded, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType: corelib.CapabilityTypeSkill,
		Publisher:      "author@example.com",
		CapabilityID:   "download-app",
		GlobalKey:      corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":maclaw-app:download-app",
		DisplayName:    "Download App",
		Description:    "Downloaded from Hub",
		Source:         corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:      "maclaw_app_upload",
		Status:         "published",
		MetadataJSON: jsonObjectString(map[string]any{
			"is_maclaw_app":     true,
			"product_kind":      "maclaw_app_skill",
			"maclaw_app_id":     "download-app",
			"review_state":      "published",
			"reviewer":          "hub-admin",
			"reviewed_at":       "2026-06-17T02:30:00Z",
			"approved_at":       "2026-06-17T02:30:00Z",
			"published_at":      "2026-06-17T03:30:00Z",
			"published_by":      "release-admin",
			"release_channel":   "stable",
			"package_signature": enterpriseMaclawAppPackageSignature("pkg-sha", "enterprise_hub:skill:maclaw-app:download-app@pkg", "2026-06-17T03:30:00Z", "release-admin"),
			"risk_level":        "low",
			"approved_scopes":   []string{"app.run", "file.read"},
			"package_sha256":    "pkg-sha",
			"review_evidence": map[string]any{"download-app": map[string]any{
				"run_id":                        "run-download-reviewed",
				"test_protocol_fingerprint":     "proto-download-reviewed",
				"result_contract_primary":       "approval_result",
				"result_coverage_primary":       "approval_result",
				"result_coverage_covered_count": 2,
				"result_coverage_missing_count": 0,
				"output_count":                  1,
				"artifact_count":                1,
				"approval_status":               "approved",
				"current_node":                  "download.result",
			}},
			"workspace_layout": map[string]any{"template": "document_workspace"},
		}),
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:download-app@pkg",
		PackageChecksum:   "pkg-sha",
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(map[string]any{"package_format": "maclaw.app.pack.v1", "app_id": "download-app"}),
		CompatibilityJSON: jsonObjectString(map[string]any{"requires_maclaw_app_runtime": true}),
		VersionStatus:     "published",
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("seed maclaw app: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities/maclaw-apps/"+seeded.ID+"/package", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.SetPathValue("id", seeded.ID)
	rec := httptest.NewRecorder()

	CapabilityMaclawAppPackageHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "installer", email: "installer@example.com"})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("download status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pkg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pkg); err != nil {
		t.Fatalf("decode package: %v", err)
	}
	if pkg["schema"] != "maclaw.app.pack.v1" || pkg["privateMarker"] != "x_maclaw_apps" || pkg["package_sha256"] != "pkg-sha" {
		t.Fatalf("unexpected package header: %+v", pkg)
	}
	topLevelSignature, ok := pkg["package_signature"].(map[string]any)
	if !ok {
		t.Fatalf("download package should expose top-level package signature: %+v", pkg["package_signature"])
	}
	assertMaclawAppPackageEd25519Signature(t, topLevelSignature)
	topReviewEvidence := anyMapFromMap(pkg, "review_evidence")
	topDownloadEvidence := anyMapFromMap(topReviewEvidence, "download-app")
	if topDownloadEvidence["run_id"] != "run-download-reviewed" || topDownloadEvidence["approval_status"] != "approved" || topDownloadEvidence["current_node"] != "download.result" {
		t.Fatalf("download package should expose top-level review evidence: %+v", pkg["review_evidence"])
	}
	resolved, ok := pkg["resolved_dependencies"].([]any)
	if !ok || len(resolved) != 2 {
		t.Fatalf("resolved dependencies=%+v", pkg["resolved_dependencies"])
	}
	depByID := map[string]map[string]any{}
	for _, raw := range resolved {
		dep, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("resolved dependency should be object: %+v", raw)
		}
		depByID[stringFromAny(dep["id"])] = dep
	}
	if depByID["download-app-skill"]["install_ref"] != "cap-download-app-skill" || depByID["download-app-skill"]["kind"] != "app_skill" {
		t.Fatalf("download app skill dependency=%+v", depByID["download-app-skill"])
	}
	workflowDep := depByID["download-workflow"]
	if workflowDep["install_ref"] != "cap-download-workflow" || workflowDep["kind"] != "workflow_skill" {
		t.Fatalf("download workflow dependency=%+v", workflowDep)
	}
	appIDs, ok := workflowDep["app_ids"].([]any)
	if !ok || len(appIDs) != 1 || appIDs[0] != "download-app" {
		t.Fatalf("download workflow app ids=%+v", workflowDep["app_ids"])
	}
	capabilities, ok := workflowDep["capabilities"].([]any)
	if !ok || len(capabilities) != 1 || capabilities[0] != "approval.workflow" {
		t.Fatalf("download workflow capabilities=%+v", workflowDep["capabilities"])
	}
	apps, ok := pkg["apps"].([]any)
	if !ok || len(apps) != 1 {
		t.Fatalf("package apps=%+v", pkg["apps"])
	}
	entry, _ := apps[0].(map[string]any)
	app, _ := entry["app"].(map[string]any)
	if app["id"] != "download-app" || app["name"] != "Download App" {
		t.Fatalf("unexpected app entry: %+v", app)
	}
	ui, _ := app["ui"].(map[string]any)
	layouts, _ := ui["layouts"].(map[string]any)
	layout, _ := layouts["tool_workspace"].(map[string]any)
	if layout["primaryRegion"] != "file_queue" || layout["outputRegion"] != "output_panel" {
		t.Fatalf("download package should preserve workspace placement: %+v", layout)
	}
	if regions, ok := layout["regions"].([]any); !ok || len(regions) != 2 {
		t.Fatalf("download package should preserve workspace regions: %+v", layout)
	}
	governance, _ := app["governance"].(map[string]any)
	workflowContract, _ := governance["workflowContract"].(map[string]any)
	if workflowContract["workflowSkillId"] != "download-workflow" || workflowContract["objectRole"] != "download_record" {
		t.Fatalf("download package should preserve workflow contract: %+v", workflowContract)
	}
	testEvidence, _ := governance["testEvidence"].(map[string]any)
	if outputs, ok := testEvidence["outputs"].([]any); !ok || len(outputs) != 1 {
		t.Fatalf("download package should preserve test evidence outputs: %+v", testEvidence)
	}
	if artifacts, ok := testEvidence["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("download package should preserve test evidence artifacts: %+v", testEvidence)
	}
	approvalInstance, _ := testEvidence["approvalInstance"].(map[string]any)
	if approvalInstance["approvalID"] != "approval-download-1" || approvalInstance["recordID"] != "download-1" || approvalInstance["status"] != "approved" {
		t.Fatalf("download package should preserve approval instance evidence: %+v", testEvidence)
	}
	if approvalInstance["currentNode"] != "download.result" || approvalInstance["workflowSkillId"] != "download-workflow" || approvalInstance["businessStatus"] != "ready" || approvalInstance["resultStatus"] != "approved" {
		t.Fatalf("download package should preserve approval instance workflow/result fields: %+v", approvalInstance)
	}
	if payload, ok := approvalInstance["resultPayload"].(map[string]any); !ok || payload["business_status"] != "ready" {
		t.Fatalf("download package should preserve approval instance result payload: %+v", approvalInstance)
	}
	if outputs, ok := approvalInstance["outputs"].([]any); !ok || len(outputs) != 1 {
		t.Fatalf("download package should preserve approval instance outputs: %+v", approvalInstance)
	}
	if artifacts, ok := approvalInstance["artifacts"].([]any); !ok || len(artifacts) != 1 {
		t.Fatalf("download package should preserve approval instance artifacts: %+v", approvalInstance)
	}
	submission, _ := governance["submission"].(map[string]any)
	if submission["channel"] != "hub" || submission["status"] != "published" || submission["reviewer"] != "hub-admin" || submission["published_by"] != "release-admin" || submission["capability_id"] != seeded.ID {
		t.Fatalf("submission metadata=%+v", submission)
	}
	submissionReviewEvidence := anyMapFromMap(submission, "review_evidence")
	submissionDownloadEvidence := anyMapFromMap(submissionReviewEvidence, "download-app")
	if submissionDownloadEvidence["run_id"] != "run-download-reviewed" || submissionDownloadEvidence["result_coverage_covered_count"] != float64(2) {
		t.Fatalf("download package should preserve submission review evidence: %+v", submission)
	}
	scopes, ok := submission["approved_scopes"].([]any)
	if !ok || len(scopes) != 2 || scopes[0] != "app.run" {
		t.Fatalf("approved scopes=%+v", submission["approved_scopes"])
	}
	signature, ok := submission["package_signature"].(map[string]any)
	if !ok || signature["schema"] != "maclaw.app.package_signature.v1" || signature["package_sha256"] != "pkg-sha" {
		t.Fatalf("package signature=%+v", submission["package_signature"])
	}
	if signature["algorithm"] != "ed25519" || signature["public_key_fingerprint"] == "" || signature["signature_base64"] == "" {
		t.Fatalf("download package should expose verifiable ed25519 signature metadata: %+v", signature)
	}
}
func TestAdminCapabilityMaclawAppReviewApprovesCurrentVersion(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), "tenant_a")
	manifest := readyMaclawAppEntry(readyEnterpriseApprovalMaclawAppSubmitPackage())
	manifestApp := manifest["app"].(map[string]any)
	manifestApp["id"] = "review-app"
	manifestApp["name"] = "Review App"
	seeded, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType: corelib.CapabilityTypeSkill,
		Publisher:      "author@example.com",
		CapabilityID:   "review-app",
		GlobalKey:      corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":maclaw-app:review-app",
		DisplayName:    "Review App",
		Description:    "Review me",
		Source:         corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:      "maclaw_app_upload",
		Status:         "pending_review",
		MetadataJSON: jsonObjectString(map[string]any{
			"is_maclaw_app":    true,
			"product_kind":     "maclaw_app_skill",
			"maclaw_app_id":    "review-app",
			"review_state":     "pending_review",
			"workspace_layout": map[string]any{"template": "classic_split"},
		}),
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:review-app@abc",
		PackageChecksum:   "abc",
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(map[string]any{"package_format": "maclaw.app.pack.v1"}),
		CompatibilityJSON: jsonObjectString(map[string]any{"requires_maclaw_app_runtime": true}),
		VersionStatus:     "pending_review",
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("seed maclaw app: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/maclaw-apps/review-app/approve", bytes.NewReader([]byte(`{"reviewer":"admin-a","risk_level":"low","approved_scopes":["app.run","app.run"]}`)))
	req.Header.Set("X-Tenant-ID", "tenant_a")
	req.SetPathValue("id", seeded.ID)
	rec := httptest.NewRecorder()

	AdminCapabilityMaclawAppReviewHandler(svc, "approve")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", rec.Code, rec.Body.String())
	}
	item, err := svc.Get(ctx, "review-app")
	if err != nil {
		t.Fatalf("get approved app: %v", err)
	}
	if item.Status != "approved" {
		t.Fatalf("capability status=%q", item.Status)
	}
	versions, err := svc.ListVersions(ctx, item.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	if versions[0].Status != "approved" {
		t.Fatalf("version status=%q", versions[0].Status)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["review_state"] != "approved" || metadata["reviewer"] != "admin-a" || metadata["approved_at"] == "" || metadata["risk_level"] != "low" {
		t.Fatalf("approval metadata=%+v", metadata)
	}
	scopes, ok := metadata["approved_scopes"].([]any)
	if !ok || len(scopes) != 1 || scopes[0] != "app.run" {
		t.Fatalf("approved scopes=%+v", metadata["approved_scopes"])
	}
}

func TestAdminCapabilityMaclawAppReviewBlocksUnreadyApproval(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), "tenant_a")
	pkg := readyEnterpriseApprovalMaclawAppSubmitPackage()
	manifest := readyMaclawAppEntry(pkg)
	manifestApp := manifest["app"].(map[string]any)
	manifestApp["id"] = "review-unready-app"
	manifestApp["name"] = "Review Unready App"
	delete(readyMaclawAppGovernance(pkg), "testEvidence")
	seeded, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType: corelib.CapabilityTypeSkill,
		Publisher:      "author@example.com",
		CapabilityID:   "review-unready-app",
		GlobalKey:      corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":maclaw-app:review-unready-app",
		DisplayName:    "Review Unready App",
		Source:         corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:      "maclaw_app_upload",
		Status:         "pending_review",
		MetadataJSON: jsonObjectString(map[string]any{
			"is_maclaw_app": true,
			"product_kind":  "maclaw_app_skill",
			"maclaw_app_id": "review-unready-app",
			"review_state":  "pending_review",
		}),
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:review-unready-app@abc",
		PackageChecksum:   "abc",
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(map[string]any{"package_format": "maclaw.app.pack.v1"}),
		CompatibilityJSON: jsonObjectString(map[string]any{"requires_maclaw_app_runtime": true}),
		VersionStatus:     "pending_review",
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("seed unready maclaw app: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/maclaw-apps/review-unready-app/approve", bytes.NewReader([]byte(`{"reviewer":"admin-a"}`)))
	req.Header.Set("X-Tenant-ID", "tenant_a")
	req.SetPathValue("id", seeded.ID)
	rec := httptest.NewRecorder()

	AdminCapabilityMaclawAppReviewHandler(svc, "approve")(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("approve status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "MACLAW_APP_PACKAGE_NOT_READY" || !strings.Contains(stringFromAny(resp["message"]), "test evidence is required") {
		t.Fatalf("unexpected response=%+v", resp)
	}
	item, err := svc.Get(ctx, seeded.ID)
	if err != nil {
		t.Fatalf("get capability: %v", err)
	}
	if item.Status != "pending_review" {
		t.Fatalf("unready approval should keep pending status, got %q", item.Status)
	}
}
func TestCapabilityMaclawAppSubmitApprovePublishListAndDownloadGoldenPath(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	pkg := readyEnterpriseApprovalMaclawAppSubmitPackage()
	submitBody, err := json.Marshal(map[string]any{"package": pkg, "source_submission_id": "local-golden-approval"})
	if err != nil {
		t.Fatalf("encode submit package: %v", err)
	}
	submitReq := httptest.NewRequest(http.MethodPost, "/api/capabilities/maclaw-apps/submit", bytes.NewReader(submitBody))
	submitReq.Header.Set("Authorization", "Bearer viewer-token")
	submitRec := httptest.NewRecorder()
	CapabilityMaclawAppSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "author-a", email: "author@example.com"})(submitRec, submitReq)
	if submitRec.Code != http.StatusOK {
		t.Fatalf("submit status=%d body=%s", submitRec.Code, submitRec.Body.String())
	}
	items, err := svc.List(capability.WithTenant(context.Background(), "tenant_a"), corelib.CapabilityTypeSkill)
	if err != nil || len(items) != 1 {
		t.Fatalf("submitted capabilities=%+v err=%v", items, err)
	}
	capabilityID := items[0].ID

	approveReq := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/maclaw-apps/"+capabilityID+"/approve", bytes.NewReader([]byte(`{"reviewer":"hub-admin","risk_level":"low","approved_scopes":["app.run","app.install"]}`)))
	approveReq.Header.Set("X-Tenant-ID", "tenant_a")
	approveReq.SetPathValue("id", capabilityID)
	approveRec := httptest.NewRecorder()
	AdminCapabilityMaclawAppReviewHandler(svc, "approve")(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	publishReq := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/maclaw-apps/"+capabilityID+"/publish", bytes.NewReader([]byte(`{"publisher":"release-admin","release_channel":"stable"}`)))
	publishReq.Header.Set("X-Tenant-ID", "tenant_a")
	publishReq.SetPathValue("id", capabilityID)
	publishRec := httptest.NewRecorder()
	AdminCapabilityMaclawAppPublishHandler(svc)(publishRec, publishReq)
	if publishRec.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", publishRec.Code, publishRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/capabilities?type=skill", nil)
	listReq.Header.Set("Authorization", "Bearer viewer-token")
	listRec := httptest.NewRecorder()
	CapabilityListHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "installer", email: "installer@example.com"})(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Items []capability.CapabilitySummary `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Items) != 1 || listResp.Items[0].ID != capabilityID || listResp.Items[0].Status != "published" {
		t.Fatalf("published marketplace list should expose one published app: %+v", listResp.Items)
	}
	metadata := mapFromRawJSON(json.RawMessage(listResp.Items[0].MetadataJSON))
	if metadata["review_state"] != "published" || metadata["published_by"] != "release-admin" || metadata["package_signature"] == nil {
		t.Fatalf("published marketplace metadata should preserve review state and signature: %+v", metadata)
	}
	reviewEvidence := anyMapFromMap(metadata, "review_evidence")
	appReviewEvidence := anyMapFromMap(reviewEvidence, "approval-ready-app")
	if appReviewEvidence["run_id"] != "run-ready-approval" || appReviewEvidence["approval_status"] != "approved" || appReviewEvidence["current_node"] != "expense.result" {
		t.Fatalf("published marketplace metadata should expose review evidence for GUI preview: %+v", metadata["review_evidence"])
	}
	if metadata["workspace_layout_primary_region"] != "center" || metadata["workspace_layout_output_region"] != "bottom" || metadata["skill_dependency_count"] != float64(2) {
		t.Fatalf("published marketplace metadata should expose layout and dependency summaries: %+v", metadata)
	}
	if metadata["test_evidence_result_coverage_primary"] != "approval_result" || metadata["test_evidence_output_count"] != float64(1) || metadata["test_evidence_artifact_count"] != float64(1) {
		t.Fatalf("published marketplace metadata should expose result coverage and output summaries: %+v", metadata)
	}

	downloadReq := httptest.NewRequest(http.MethodGet, "/api/capabilities/maclaw-apps/"+capabilityID+"/package", nil)
	downloadReq.Header.Set("Authorization", "Bearer viewer-token")
	downloadReq.SetPathValue("id", capabilityID)
	downloadRec := httptest.NewRecorder()
	CapabilityMaclawAppPackageHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "installer", email: "installer@example.com"})(downloadRec, downloadReq)
	if downloadRec.Code != http.StatusOK {
		t.Fatalf("download status=%d body=%s", downloadRec.Code, downloadRec.Body.String())
	}
	var downloaded map[string]any
	if err := json.Unmarshal(downloadRec.Body.Bytes(), &downloaded); err != nil {
		t.Fatalf("decode downloaded package: %v", err)
	}
	topReviewEvidence := anyMapFromMap(downloaded, "review_evidence")
	topAppEvidence := anyMapFromMap(topReviewEvidence, "approval-ready-app")
	if topAppEvidence["run_id"] != "run-ready-approval" || downloaded["package_signature"] == nil {
		t.Fatalf("downloaded package should carry review evidence and package signature: %+v", downloaded)
	}
	capabilityBlock := anyMapFromMap(downloaded, "capability")
	if downloaded["source"] != "enterprise_hub" || capabilityBlock["id"] != capabilityID || capabilityBlock["status"] != "published" || capabilityBlock["current_version_key"] == "" {
		t.Fatalf("downloaded package should carry published Hub capability identity: %+v", downloaded)
	}
	resolvedDependencies := anySliceFromMap(downloaded, "resolved_dependencies")
	if len(resolvedDependencies) != 2 {
		t.Fatalf("downloaded package should carry resolved app/workflow Skill dependencies: %+v", downloaded["resolved_dependencies"])
	}
	apps := anySliceFromMap(downloaded, "apps")
	if len(apps) != 1 {
		t.Fatalf("downloaded package should contain one app: %+v", downloaded["apps"])
	}
	entry := apps[0].(map[string]any)
	app := entry["app"].(map[string]any)
	governance := app["governance"].(map[string]any)
	submission := governance["submission"].(map[string]any)
	if submission["status"] != "published" || submission["capability_id"] != capabilityID || submission["published_by"] != "release-admin" || submission["package_signature"] == nil {
		t.Fatalf("downloaded app entry should carry Hub submission identity: %+v", submission)
	}
	submissionReviewEvidence := anyMapFromMap(submission, "review_evidence")
	if anyMapFromMap(submissionReviewEvidence, "approval-ready-app")["run_id"] != "run-ready-approval" {
		t.Fatalf("downloaded app submission should preserve review evidence: %+v", submission)
	}
	assertDownloadedMaclawAppPackageSatisfiesGUIInstallContract(t, downloaded, capabilityID)

	downloadHandler := CapabilityMaclawAppPackageHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "installer", email: "installer@example.com"})
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/capabilities/maclaw-apps/"+capabilityID+"/package" {
			t.Fatalf("unexpected black-box download path: %s", r.URL.Path)
		}
		r.SetPathValue("id", capabilityID)
		downloadHandler(w, r)
	}))
	defer downloadServer.Close()
	blackBoxDownloaded, err := maclawappcontract.DownloadGUIInstallHubPackage(context.Background(), downloadServer.Client(), downloadServer.URL, "viewer-token", capabilityID)
	if err != nil {
		t.Fatalf("shared GUI HTTP client should consume real Hub handler output: %v", err)
	}
	blackBoxCapability := anyMapFromMap(blackBoxDownloaded, "capability")
	if blackBoxCapability["id"] != capabilityID || anyMapFromMap(blackBoxDownloaded, "package_signature")["public_key_fingerprint"] == "" {
		t.Fatalf("black-box shared GUI client should preserve Hub identity and signature: %+v", blackBoxDownloaded)
	}
	selectedPackage, err := maclawappcontract.SelectHubPackageApps(blackBoxDownloaded, []string{"market-approval-ready-app"})
	if err != nil {
		t.Fatalf("shared GUI package selector should accept real Hub handler output: %v", err)
	}
	assertDownloadedMaclawAppPackageSatisfiesGUIInstallContract(t, selectedPackage, capabilityID)
	selectedApps := anySliceFromMap(selectedPackage, "apps")
	if len(selectedApps) != 1 || anyMapFromMap(anyMapFromMap(selectedApps[0].(map[string]any), "app"), "governance") == nil {
		t.Fatalf("shared GUI package selector should keep the selected app entry: %+v", selectedPackage["apps"])
	}
	selectedDeps := anySliceFromMap(selectedPackage, "resolved_dependencies")
	selectedGovernance := anyMapFromMap(anyMapFromMap(selectedApps[0].(map[string]any), "app"), "governance")
	selectedVerification := anyMapFromMap(selectedGovernance, "dependencyVerification")
	selectedVerificationDeps := anySliceFromMap(selectedVerification, "dependencies")
	if len(selectedDeps) != 2 || len(selectedVerificationDeps) != 2 || selectedVerification["blocked"] == true {
		t.Fatalf("shared GUI selector should preserve non-blocking dependency verification and resolved dependencies, deps=%+v verification=%+v", selectedDeps, selectedVerification)
	}
	var workflowDep map[string]any
	for _, rawDep := range selectedVerificationDeps {
		dep, _ := rawDep.(map[string]any)
		if dep["id"] == "approval-ready-workflow" {
			workflowDep = dep
			break
		}
	}
	if workflowDep["id"] != "approval-ready-workflow" || workflowDep["install_ref"] == "" {
		t.Fatalf("shared GUI selector should preserve workflow dependency install ref: %+v", selectedVerificationDeps)
	}
}

func assertDownloadedMaclawAppPackageSatisfiesGUIInstallContract(t *testing.T, pkg map[string]any, capabilityID string) {
	t.Helper()
	if err := maclawappcontract.ValidateGUIInstallHubPackage(pkg, capabilityID); err != nil {
		t.Fatalf("downloaded package should satisfy GUI install contract: %v\npackage=%+v", err, pkg)
	}
	fingerprint, err := maclawappcontract.VerifyHubPackageSignature(pkg)
	if err != nil {
		t.Fatalf("downloaded package signature should verify for GUI install trust: %v\npackage=%+v", err, pkg)
	}
	if strings.TrimSpace(fingerprint) == "" {
		t.Fatalf("downloaded package signature should expose trusted public-key fingerprint: %+v", pkg["package_signature"])
	}
}

func TestAdminCapabilityMaclawAppPublishPublishesApprovedVersion(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), "tenant_a")
	manifest := readyMaclawAppEntry(readyEnterpriseApprovalMaclawAppSubmitPackage())
	manifestApp := manifest["app"].(map[string]any)
	manifestApp["id"] = "publish-app"
	manifestApp["name"] = "Publish App"
	seeded, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType: corelib.CapabilityTypeSkill,
		Publisher:      "author@example.com",
		CapabilityID:   "publish-app",
		GlobalKey:      corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":maclaw-app:publish-app",
		DisplayName:    "Publish App",
		Source:         corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:      "maclaw_app_upload",
		Status:         "approved",
		MetadataJSON: jsonObjectString(map[string]any{
			"is_maclaw_app":   true,
			"product_kind":    "maclaw_app_skill",
			"maclaw_app_id":   "publish-app",
			"review_state":    "approved",
			"reviewer":        "admin-a",
			"reviewed_at":     "2026-06-30T01:00:00Z",
			"approved_at":     "2026-06-30T01:00:00Z",
			"approved_scopes": []string{"app.run"},
			"package_sha256":  "pkg-publish-sha",
		}),
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:publish-app@pkg",
		PackageChecksum:   "pkg-publish-sha",
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(map[string]any{"package_format": "maclaw.app.pack.v1"}),
		CompatibilityJSON: jsonObjectString(map[string]any{"requires_maclaw_app_runtime": true}),
		VersionStatus:     "approved",
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("seed publish app: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/maclaw-apps/publish-app/publish", bytes.NewReader([]byte(`{"publisher":"release-admin","release_channel":"stable","notes":"ready for enterprise rollout"}`)))
	req.Header.Set("X-Tenant-ID", "tenant_a")
	req.SetPathValue("id", seeded.ID)
	rec := httptest.NewRecorder()

	AdminCapabilityMaclawAppPublishHandler(svc)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", rec.Code, rec.Body.String())
	}
	item, err := svc.Get(ctx, seeded.ID)
	if err != nil {
		t.Fatalf("get published app: %v", err)
	}
	if item.Status != "published" {
		t.Fatalf("capability status=%q", item.Status)
	}
	versions, err := svc.ListVersions(ctx, item.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	if versions[0].Status != "published" {
		t.Fatalf("version status=%q", versions[0].Status)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["review_state"] != "published" || metadata["published_by"] != "release-admin" || metadata["published_at"] == "" || metadata["release_channel"] != "stable" {
		t.Fatalf("publish metadata=%+v", metadata)
	}
	signature, ok := metadata["package_signature"].(map[string]any)
	if !ok || signature["schema"] != "maclaw.app.package_signature.v1" || signature["package_sha256"] != "pkg-publish-sha" || signature["version_key"] != "enterprise_hub:skill:maclaw-app:publish-app@pkg" {
		t.Fatalf("package signature=%+v", metadata["package_signature"])
	}
	assertMaclawAppPackageEd25519Signature(t, signature)
}

func assertMaclawAppPackageEd25519Signature(t *testing.T, signature map[string]any) {
	t.Helper()
	if signature["algorithm"] != "ed25519" {
		t.Fatalf("signature algorithm=%v", signature["algorithm"])
	}
	payload := stringFromAny(signature["payload"])
	publicKeyBytes, err := base64.StdEncoding.DecodeString(stringFromAny(signature["public_key_base64"]))
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(stringFromAny(signature["signature_base64"]))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if len(publicKeyBytes) != ed25519.PublicKeySize || len(signatureBytes) != ed25519.SignatureSize {
		t.Fatalf("unexpected ed25519 sizes public=%d signature=%d", len(publicKeyBytes), len(signatureBytes))
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKeyBytes), []byte(payload), signatureBytes) {
		t.Fatalf("ed25519 package signature did not verify: %+v", signature)
	}
	if fingerprint := stringFromAny(signature["public_key_fingerprint"]); !strings.HasPrefix(fingerprint, "sha256:") || len(fingerprint) != len("sha256:")+64 {
		t.Fatalf("public key fingerprint=%q", fingerprint)
	}
}
func TestAdminCapabilityMaclawAppPublishRequiresApprovedReadyApp(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), "tenant_a")
	manifest := readyMaclawAppEntry(readyEnterpriseApprovalMaclawAppSubmitPackage())
	seeded, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType:    corelib.CapabilityTypeSkill,
		Publisher:         "author@example.com",
		CapabilityID:      "publish-pending-app",
		GlobalKey:         corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":maclaw-app:publish-pending-app",
		DisplayName:       "Publish Pending App",
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "maclaw_app_upload",
		Status:            "pending_review",
		MetadataJSON:      jsonObjectString(map[string]any{"is_maclaw_app": true, "product_kind": "maclaw_app_skill", "maclaw_app_id": "publish-pending-app", "review_state": "pending_review"}),
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:publish-pending-app@pkg",
		PackageChecksum:   "pkg-pending-sha",
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(map[string]any{"package_format": "maclaw.app.pack.v1"}),
		CompatibilityJSON: jsonObjectString(map[string]any{"requires_maclaw_app_runtime": true}),
		VersionStatus:     "pending_review",
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("seed pending app: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/maclaw-apps/publish-pending-app/publish", bytes.NewReader([]byte(`{"publisher":"release-admin"}`)))
	req.Header.Set("X-Tenant-ID", "tenant_a")
	req.SetPathValue("id", seeded.ID)
	rec := httptest.NewRecorder()

	AdminCapabilityMaclawAppPublishHandler(svc)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("publish pending status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "MACLAW_APP_NOT_APPROVED" {
		t.Fatalf("unexpected response=%+v", resp)
	}
}

func TestCapabilityMaclawAppPackageDownloadRequiresPublishedStatus(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), "tenant_a")
	manifest := readyMaclawAppEntry(readyEnterpriseApprovalMaclawAppSubmitPackage())
	seeded, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType:    corelib.CapabilityTypeSkill,
		Publisher:         "author@example.com",
		CapabilityID:      "approved-not-published-app",
		GlobalKey:         corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":maclaw-app:approved-not-published-app",
		DisplayName:       "Approved Not Published App",
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "maclaw_app_upload",
		Status:            "approved",
		MetadataJSON:      jsonObjectString(map[string]any{"is_maclaw_app": true, "product_kind": "maclaw_app_skill", "maclaw_app_id": "approved-not-published-app", "review_state": "approved"}),
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:approved-not-published-app@pkg",
		PackageChecksum:   "pkg-approved-sha",
		ManifestJSON:      jsonObjectString(manifest),
		TypeConfigJSON:    jsonObjectString(map[string]any{"package_format": "maclaw.app.pack.v1"}),
		CompatibilityJSON: jsonObjectString(map[string]any{"requires_maclaw_app_runtime": true}),
		VersionStatus:     "approved",
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("seed approved app: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/capabilities/maclaw-apps/"+seeded.ID+"/package", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.SetPathValue("id", seeded.ID)
	rec := httptest.NewRecorder()

	CapabilityMaclawAppPackageHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"})(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("download approved-only status=%d body=%s", rec.Code, rec.Body.String())
	}
}
func TestAdminCapabilityMaclawAppReviewRejectsWithIssues(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	ctx := capability.WithTenant(context.Background(), "tenant_a")

	seeded, err := svc.UpsertCapability(ctx, capability.UpsertCapabilityInput{
		CapabilityType:    corelib.CapabilityTypeSkill,
		Publisher:         "author@example.com",
		CapabilityID:      "reject-app",
		GlobalKey:         corelib.CapabilitySourceEnterpriseHub + ":" + corelib.CapabilityTypeSkill + ":maclaw-app:reject-app",
		DisplayName:       "Reject App",
		Source:            corelib.CapabilitySourceEnterpriseHub,
		ManagedBy:         "maclaw_app_upload",
		Status:            "pending_review",
		MetadataJSON:      jsonObjectString(map[string]any{"is_maclaw_app": true, "product_kind": "maclaw_app_skill", "maclaw_app_id": "reject-app", "review_state": "pending_review"}),
		Version:           "1",
		VersionKey:        "enterprise_hub:skill:maclaw-app:reject-app@abc",
		ManifestJSON:      jsonObjectString(map[string]any{"schema": "maclaw.app.v1"}),
		TypeConfigJSON:    jsonObjectString(map[string]any{"package_format": "maclaw.app.pack.v1"}),
		CompatibilityJSON: jsonObjectString(map[string]any{"requires_maclaw_app_runtime": true}),
		VersionStatus:     "pending_review",
		SetCurrentVersion: true,
	})
	if err != nil {
		t.Fatalf("seed maclaw app: %v", err)
	}
	body := []byte(`{"reason":"test evidence is missing from the submitted package","review_issues":[{"path":"app.governance.testEvidence","severity":"error","message":"missing test evidence","suggestion":"run the app test protocol"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capabilities/maclaw-apps/"+seeded.ID+"/reject", bytes.NewReader(body))
	req.Header.Set("X-Tenant-ID", "tenant_a")
	req.SetPathValue("id", seeded.ID)
	rec := httptest.NewRecorder()

	AdminCapabilityMaclawAppReviewHandler(svc, "reject")(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("reject status=%d body=%s", rec.Code, rec.Body.String())
	}
	item, err := svc.Get(ctx, "reject-app")
	if err != nil {
		t.Fatalf("get rejected app: %v", err)
	}
	if item.Status != "review_failed" {
		t.Fatalf("capability status=%q", item.Status)
	}
	versions, err := svc.ListVersions(ctx, item.ID)
	if err != nil || len(versions) != 1 || versions[0].Status != "review_failed" {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(item.MetadataJSON), &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["review_state"] != "review_failed" || metadata["review_reason"] == "" {
		t.Fatalf("rejection metadata=%+v", metadata)
	}
	issues, ok := metadata["review_issues"].([]any)
	if !ok || len(issues) != 1 {
		t.Fatalf("review issues=%+v", metadata["review_issues"])
	}
}

func TestCapabilitySkillSubmitUpsertsSameSkillAcrossUploaders(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	dataDir := t.TempDir()

	body1, contentType1 := makeEnterpriseSkillUploadBody(t, map[string]string{
		"skill.yaml": "name: shared-skill\ndescription: first upload\n",
		"README.md":  "first docs",
	})
	req1 := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body1)
	req1.Header.Set("Authorization", "Bearer viewer-token")
	req1.Header.Set("Content-Type", contentType1)
	rec1 := httptest.NewRecorder()
	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "user-a", email: "a@example.com"}, dataDir)(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first submit status=%d body=%s", rec1.Code, rec1.Body.String())
	}

	body2, contentType2 := makeEnterpriseSkillUploadBody(t, map[string]string{
		"skill.yaml": "name: shared-skill\ndescription: second upload\n",
		"README.md":  "second docs",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body2)
	req2.Header.Set("Authorization", "Bearer viewer-token")
	req2.Header.Set("Content-Type", contentType2)
	rec2 := httptest.NewRecorder()
	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "user-b", email: "b@example.com"}, dataDir)(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second submit status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	items, err := svc.List(capability.WithTenant(context.Background(), "tenant_a"), corelib.CapabilityTypeSkill)
	if err != nil || len(items) != 1 {
		t.Fatalf("capabilities=%+v err=%v", items, err)
	}
	if items[0].Description != "second upload" || items[0].Publisher != "b@example.com" {
		t.Fatalf("capability not updated by second submit: %+v", items[0])
	}
	if items[0].GlobalKey != corelib.CapabilitySourceEnterpriseHub+":"+corelib.CapabilityTypeSkill+":shared-skill" {
		t.Fatalf("global_key=%q", items[0].GlobalKey)
	}
}

func TestCapabilitySkillSubmitIsIdempotentForSamePackage(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	dataDir := t.TempDir()
	files := map[string]string{
		"skill.yaml": "name: repeat-skill\ndescription: repeat upload\n",
		"README.md":  "repeat docs",
	}

	for i := 0; i < 2; i++ {
		body, contentType := makeEnterpriseSkillUploadBody(t, files)
		req := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body)
		req.Header.Set("Authorization", "Bearer viewer-token")
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", userID: "user-a", email: "a@example.com"}, dataDir)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("submit %d status=%d body=%s", i+1, rec.Code, rec.Body.String())
		}
	}

	items, err := svc.List(capability.WithTenant(context.Background(), "tenant_a"), corelib.CapabilityTypeSkill)
	if err != nil || len(items) != 1 {
		t.Fatalf("capabilities=%+v err=%v", items, err)
	}
	metadata := mapFromRawJSON(json.RawMessage(items[0].MetadataJSON))
	packageFile := strings.TrimSpace(stringFromMap(metadata, "package_file"))
	if packageFile == "" {
		t.Fatalf("metadata missing package_file: %+v", metadata)
	}
	if _, err := enterpriseSkillPackagePath(dataDir, "tenant_a", packageFile); err != nil {
		t.Fatalf("package missing after repeat upload: %v", err)
	}
}

func TestMoveEnterpriseSkillPackageIntoPlaceReplacesWrongExistingFile(t *testing.T) {
	dir := t.TempDir()
	finalPath := filepath.Join(dir, "skill.zip")
	tmpPath := filepath.Join(dir, "upload.zip")
	if err := os.WriteFile(finalPath, []byte("wrong package"), 0o644); err != nil {
		t.Fatalf("write final: %v", err)
	}
	if err := os.WriteFile(tmpPath, []byte("right package"), 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	checksum, err := fileSHA256Hex(tmpPath)
	if err != nil {
		t.Fatalf("checksum tmp: %v", err)
	}

	if err := moveEnterpriseSkillPackageIntoPlace(tmpPath, finalPath, checksum); err != nil {
		t.Fatalf("moveEnterpriseSkillPackageIntoPlace() error = %v", err)
	}
	data, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if string(data) != "right package" {
		t.Fatalf("final data = %q", string(data))
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("tmp should be gone, stat err=%v", err)
	}
}

func TestCapabilitySkillSubmitAllowsExportPayloadWithinUploadLimit(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	files := map[string]string{"skill.yaml": "name: bulky-skill\ndescription: bulky\n"}
	for i := 0; i < 5; i++ {
		files["docs/part"+strconv.Itoa(i)+".txt"] = strings.Repeat("x", 220<<10)
	}
	body, contentType := makeEnterpriseSkillUploadBody(t, files)
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, t.TempDir())(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCapabilitySkillSubmitExportsBinaryResource(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	dataDir := t.TempDir()
	resource := strings.Repeat("x", 300<<10)
	files := map[string]string{
		"skill.yaml":       "name: resource-skill\ndescription: carries local resource\n",
		"README.md":        "docs",
		"assets/model.bin": resource,
	}
	body, contentType := makeEnterpriseSkillUploadBody(t, files)
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, dataDir)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	items, err := svc.List(capability.WithTenant(context.Background(), "tenant_a"), corelib.CapabilityTypeSkill)
	if err != nil || len(items) != 1 {
		t.Fatalf("capabilities=%+v err=%v", items, err)
	}
	metadata := mapFromRawJSON(json.RawMessage(items[0].MetadataJSON))
	packageFile := strings.TrimSpace(stringFromMap(metadata, "package_file"))
	packagePath, err := enterpriseSkillPackagePath(dataDir, "tenant_a", packageFile)
	if err != nil {
		t.Fatalf("package path: %v", err)
	}
	meta, err := readEnterpriseSkillPackageMeta(packagePath)
	if err != nil {
		t.Fatalf("readEnterpriseSkillPackageMeta() error = %v", err)
	}
	encoded := meta.Files["assets/model.bin"]
	if encoded == "" {
		t.Fatalf("download metadata missing binary resource: %+v", meta.Files)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(decoded) != resource {
		t.Fatalf("binary resource mismatch len=%d err=%v", len(decoded), err)
	}
}

func TestCapabilitySkillSubmitStoresManifestWithoutExportingAsFile(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	dataDir := t.TempDir()
	manifest := `{"skill_name":"manifest-skill","quality":{"score":100}}`
	files := map[string]string{
		"skill.yaml":                  "name: manifest-skill\ndescription: carries manifest\n",
		"README.md":                   "docs",
		"skill_package_manifest.json": manifest,
	}
	body, contentType := makeEnterpriseSkillUploadBody(t, files)
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, dataDir)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	items, err := svc.List(capability.WithTenant(context.Background(), "tenant_a"), corelib.CapabilityTypeSkill)
	if err != nil || len(items) != 1 {
		t.Fatalf("capabilities=%+v err=%v", items, err)
	}
	versions, err := svc.ListVersions(capability.WithTenant(context.Background(), "tenant_a"), items[0].ID)
	if err != nil || len(versions) == 0 {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	if strings.TrimSpace(versions[0].ManifestJSON) != manifest {
		t.Fatalf("ManifestJSON = %s, want %s", versions[0].ManifestJSON, manifest)
	}
	metadata := mapFromRawJSON(json.RawMessage(items[0].MetadataJSON))
	packageFile := strings.TrimSpace(stringFromMap(metadata, "package_file"))
	packagePath, err := enterpriseSkillPackagePath(dataDir, "tenant_a", packageFile)
	if err != nil {
		t.Fatalf("package path: %v", err)
	}
	meta, err := readEnterpriseSkillPackageMeta(packagePath)
	if err != nil {
		t.Fatalf("readEnterpriseSkillPackageMeta() error = %v", err)
	}
	if _, ok := meta.Files["skill_package_manifest.json"]; ok {
		t.Fatalf("manifest should not be exported as install file: %+v", meta.Files)
	}
}

func TestCapabilitySkillSubmitRequiresValidSkillDefinition(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body, contentType := makeEnterpriseSkillUploadBody(t, map[string]string{
		"README.md": "docs only is not a skill package",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, t.TempDir())(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "skill.yaml") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCapabilitySkillSubmitRejectsSkillDefinitionWithoutName(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body, contentType := makeEnterpriseSkillUploadBody(t, map[string]string{
		"skill.yaml": "description: missing name\nsteps:\n  - action: bash\n    params:\n      command: echo ok\n",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, t.TempDir())(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "must declare name") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCapabilitySkillSubmitRejectsMissingBundledFileReference(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body, contentType := makeEnterpriseSkillUploadBody(t, map[string]string{
		"skill.yaml": "name: missing-file-skill\ndescription: missing bundled script\nsteps:\n  - action: bash\n    params:\n      command: python {baseDir}/scripts/missing.py\n",
		"README.md":  "docs",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, t.TempDir())(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Missing bundled files") || !strings.Contains(rec.Body.String(), "missing.py") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCapabilitySkillSubmitRejectsUnsafeZipPath(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body, contentType := makeEnterpriseSkillUploadBody(t, map[string]string{
		"skill.yaml":    "name: unsafe-path-skill\ndescription: unsafe zip path\n",
		"../secret.txt": "nope",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/skills/submit", body)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	CapabilitySkillSubmitHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a"}, t.TempDir())(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unsafe path") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func makeEnterpriseSkillUploadBody(t *testing.T, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)
	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("zip", "skill.zip")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := fw.Write(zipBuf.Bytes()); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return &body, mw.FormDataContentType()
}

func writeEnterpriseSkillPackageZip(t *testing.T, dir, name string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("create stray package: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for path, content := range files {
		fw, err := zw.Create(path)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", path, err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %s: %v", path, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close stray package: %v", err)
	}
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

func TestCapabilityScopeSpecificityInfersScopeType(t *testing.T) {
	userScope, _ := json.Marshal(map[string]any{"user_emails": []any{"dev@example.com"}})
	if score, source := capabilityScopeSpecificity(string(userScope), "dev@example.com", []string{"dept-a", "root"}); score != 1000 || source != "user" {
		t.Fatalf("user scope score=%d source=%q", score, source)
	}
	groupScope, _ := json.Marshal(map[string]any{"group_ids": []any{"root", "dept-a"}})
	if score, source := capabilityScopeSpecificity(string(groupScope), "dev@example.com", []string{"dept-a", "root"}); score != 102 || source != "group" {
		t.Fatalf("group scope score=%d source=%q", score, source)
	}
}

func TestViewerCapabilityDeploymentListUsesEffectiveRequiredPolicies(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	global := createTestCapability(t, svc, "skill", "global", "Global Skill", "1.0.0")
	dept := createTestCapability(t, svc, "skill", "dept", "Dept Skill", "1.0.0")
	blocked := createTestCapability(t, svc, "skill", "blocked", "Blocked Skill", "1.0.0")
	createDeploymentForTest(t, svc, global.ID, "", map[string]any{"type": "global"}, "required")
	deptScope, _ := json.Marshal(map[string]any{"type": "group", "group_id": "dept-a"})
	if _, err := svc.CreateManagedDeployment(context.Background(), capability.ManagedDeploymentInput{CapabilityRef: dept.ID, ScopeJSON: string(deptScope), DeploymentPolicy: "required", ReinstallIfRemoved: false, RetryIntervalMinutes: 60, CreatedBy: "test", Enabled: true}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	createDeploymentForTest(t, svc, blocked.ID, "", map[string]any{"type": "group", "group_id": "dept-a"}, "blocked")

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities/managed-deployments", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	CapabilityManagedDeploymentsHandler(svc, fakeMarketplaceViewerAuth{tenantID: store.DefaultTenantID, email: "dev@example.com"}, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("viewer deployments status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []capability.Deployment `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode deployments: %v", err)
	}
	got := map[string]bool{}
	for _, item := range resp.Items {
		got[item.CapabilityRef] = true
		if item.DeploymentPolicy != "required" {
			t.Fatalf("viewer deployment should only include required policies: %+v", item)
		}
		if item.CapabilityRef == dept.ID && item.ReinstallIfRemoved {
			t.Fatalf("viewer deployment should preserve reinstall flag: %+v", item)
		}
	}
	if !got[global.ID] || !got[dept.ID] || got[blocked.ID] || len(resp.Items) != 2 {
		t.Fatalf("unexpected viewer deployments: %+v", resp.Items)
	}
}

func TestViewerCapabilityDeploymentListRecommendationDoesNotShadowRequired(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	required := createTestCapability(t, svc, "skill", "required", "Required Skill", "1.0.0")
	createDeploymentForTest(t, svc, required.ID, "", map[string]any{"type": "global"}, "required")
	scopeJSON, _ := json.Marshal(map[string]any{"type": "user", "user_email": "dev@example.com"})
	if _, err := svc.CreateRecommendation(context.Background(), capability.RecommendationInput{CapabilityRef: required.ID, ScopeJSON: string(scopeJSON), Reason: "nice to have", AllowUserDismiss: true, Enabled: true}); err != nil {
		t.Fatalf("create recommendation: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities/managed-deployments", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	CapabilityManagedDeploymentsHandler(svc, fakeMarketplaceViewerAuth{tenantID: store.DefaultTenantID, email: "dev@example.com"}, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("viewer deployments status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []capability.Deployment `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode deployments: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityRef != required.ID || resp.Items[0].DeploymentPolicy != "required" {
		t.Fatalf("recommendation should not shadow required deployment: %+v", resp.Items)
	}
}

func TestViewerCapabilityDeploymentListRejectsInvalidAuth(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	cap := createTestCapability(t, svc, "skill", "global", "Global Skill", "1.0.0")
	createDeploymentForTest(t, svc, cap.ID, "", map[string]any{"type": "global"}, "required")

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities/managed-deployments", nil)
	req.Header.Set("Authorization", "Bearer bad-token")
	rec := httptest.NewRecorder()
	CapabilityManagedDeploymentsHandler(svc, fakeMarketplaceViewerAuth{tenantID: store.DefaultTenantID, email: "dev@example.com"}, fakeCapabilityGroupResolver{chain: []string{"dept-a"}})(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("viewer deployments status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestViewerCapabilityDeploymentListUsesAuthenticatedTenant(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	tenantACtx := capability.WithTenant(context.Background(), "tenant_a")
	tenantBCtx := capability.WithTenant(context.Background(), "tenant_b")

	capA, err := svc.UpsertCapability(tenantACtx, capability.UpsertCapabilityInput{CapabilityType: "skill", Publisher: "acme", CapabilityID: "tenant-a-skill", DisplayName: "Tenant A Skill", Source: "enterprise_hub", Status: "approved", Version: "1.0.0", VersionKey: "1.0.0", SetCurrentVersion: true})
	if err != nil {
		t.Fatalf("create tenant A capability: %v", err)
	}
	capB, err := svc.UpsertCapability(tenantBCtx, capability.UpsertCapabilityInput{CapabilityType: "skill", Publisher: "acme", CapabilityID: "tenant-b-skill", DisplayName: "Tenant B Skill", Source: "enterprise_hub", Status: "approved", Version: "1.0.0", VersionKey: "1.0.0", SetCurrentVersion: true})
	if err != nil {
		t.Fatalf("create tenant B capability: %v", err)
	}
	scope, _ := json.Marshal(map[string]any{"type": "global"})
	if _, err := svc.CreateManagedDeployment(tenantACtx, capability.ManagedDeploymentInput{CapabilityRef: capA.ID, ScopeJSON: string(scope), DeploymentPolicy: "required", ReinstallIfRemoved: true, RetryIntervalMinutes: 60, Enabled: true}); err != nil {
		t.Fatalf("create tenant A deployment: %v", err)
	}
	if _, err := svc.CreateManagedDeployment(tenantBCtx, capability.ManagedDeploymentInput{CapabilityRef: capB.ID, ScopeJSON: string(scope), DeploymentPolicy: "required", ReinstallIfRemoved: true, RetryIntervalMinutes: 60, Enabled: true}); err != nil {
		t.Fatalf("create tenant B deployment: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities/managed-deployments", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	req.Header.Set("X-Tenant-ID", "tenant_b")
	rec := httptest.NewRecorder()
	CapabilityManagedDeploymentsHandler(svc, fakeMarketplaceViewerAuth{tenantID: "tenant_a", email: "dev@example.com"}, fakeCapabilityGroupResolver{})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("viewer deployments status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []capability.Deployment `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode deployments: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityRef != capA.ID || resp.Items[0].CapabilityRef == capB.ID {
		t.Fatalf("viewer deployments should use authenticated tenant, got %+v", resp.Items)
	}
}

func TestAdminCapabilityListUsesAuthenticatedAdminTenant(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	tenantACtx := capability.WithTenant(context.Background(), "tenant_a")
	tenantBCtx := capability.WithTenant(context.Background(), "tenant_b")

	capA, err := svc.UpsertCapability(tenantACtx, capability.UpsertCapabilityInput{CapabilityType: "skill", Publisher: "acme", CapabilityID: "tenant-a-skill", DisplayName: "Tenant A Skill", Source: "enterprise_hub", Status: "approved", Version: "1.0.0", VersionKey: "1.0.0", SetCurrentVersion: true})
	if err != nil {
		t.Fatalf("create tenant A capability: %v", err)
	}
	capB, err := svc.UpsertCapability(tenantBCtx, capability.UpsertCapabilityInput{CapabilityType: "skill", Publisher: "acme", CapabilityID: "tenant-b-skill", DisplayName: "Tenant B Skill", Source: "enterprise_hub", Status: "approved", Version: "1.0.0", VersionKey: "1.0.0", SetCurrentVersion: true})
	if err != nil {
		t.Fatalf("create tenant B capability: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capabilities", nil)
	req = req.WithContext(context.WithValue(req.Context(), adminUserContextKey, &store.AdminUser{ID: "adm-a", Scope: "tenant", TenantID: "tenant_a"}))
	rec := httptest.NewRecorder()
	AdminCapabilityListHandler(svc)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin capability list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []capability.CapabilitySummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != capA.ID || resp.Items[0].ID == capB.ID {
		t.Fatalf("admin capability list should use authenticated tenant, got %+v", resp.Items)
	}
}

func TestViewerCapabilityRecommendationsUseEffectiveRecommendations(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	recommended := createTestCapability(t, svc, "mcp", "rec", "Recommended MCP", "1.0.0")
	required := createTestCapability(t, svc, "mcp", "req", "Required MCP", "1.0.0")
	scopeJSON, _ := json.Marshal(map[string]any{"type": "group", "group_id": "dept-a"})
	if _, err := svc.CreateRecommendation(context.Background(), capability.RecommendationInput{CapabilityRef: recommended.ID, ScopeJSON: string(scopeJSON), Reason: "test reason", AllowUserDismiss: false, Enabled: true}); err != nil {
		t.Fatalf("create recommendation: %v", err)
	}
	createDeploymentForTest(t, svc, required.ID, "", map[string]any{"type": "group", "group_id": "dept-a"}, "required")

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities/recommended", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	CapabilityRecommendationsHandler(svc, fakeMarketplaceViewerAuth{tenantID: store.DefaultTenantID, email: "dev@example.com"}, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("viewer recommendations status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []capability.Recommendation `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode recommendations: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityRef != recommended.ID || resp.Items[0].CapabilityRef == required.ID || resp.Items[0].Reason != "test reason" || resp.Items[0].AllowUserDismiss {
		t.Fatalf("unexpected viewer recommendations: %+v", resp.Items)
	}
}

func TestViewerCapabilityRecommendationsIncludeLegacyRecommendedDeployments(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	legacy := createTestCapability(t, svc, "skill", "legacy-rec", "Legacy Recommended Skill", "1.0.0")
	createDeploymentForTest(t, svc, legacy.ID, legacy.CurrentVersionKey, map[string]any{"type": "group", "group_id": "dept-a"}, "recommended")

	req := httptest.NewRequest(http.MethodGet, "/api/capabilities/recommended", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	CapabilityRecommendationsHandler(svc, fakeMarketplaceViewerAuth{tenantID: store.DefaultTenantID, email: "dev@example.com"}, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("viewer recommendations status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []capability.Recommendation `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode recommendations: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityRef != legacy.ID || resp.Items[0].CapabilityVersionKey != legacy.CurrentVersionKey {
		t.Fatalf("legacy recommended deployment should appear as recommendation: %+v", resp.Items)
	}
}

func TestEffectiveCapabilityPolicyRecommendedDeploymentDoesNotShadowSpecificRecommendation(t *testing.T) {
	current := adminEffectiveCapabilityPolicy{CapabilityRef: "cap-1", Kind: "deployment", Policy: "recommended", Specificity: 0}
	candidate := adminEffectiveCapabilityPolicy{CapabilityRef: "cap-1", Kind: "recommendation", Policy: "recommended", Specificity: 1000}
	if !effectiveCapabilityPolicyBeats(candidate, current) {
		t.Fatal("specific recommendation should beat generic recommended deployment")
	}
	if effectiveCapabilityPolicyBeats(current, candidate) {
		t.Fatal("generic recommended deployment should not beat specific recommendation")
	}
}

func TestAdminCapabilityPolicyListHandlersReturnRawTenantPolicies(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	capA := createTestCapability(t, svc, "skill", "deploy", "Deploy Skill", "1.0.0")
	capB := createTestCapability(t, svc, "mcp", "recommend", "Recommended MCP", "1.0.0")
	createDeploymentForTest(t, svc, capA.ID, "", map[string]any{"type": "global"}, "required")
	if _, err := svc.CreateRecommendation(context.Background(), capability.RecommendationInput{CapabilityRef: capB.ID, ScopeJSON: `{"type":"global"}`, Reason: "admin list", AllowUserDismiss: true, Enabled: true}); err != nil {
		t.Fatalf("create recommendation: %v", err)
	}

	depReq := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/managed-deployments", nil)
	depRec := httptest.NewRecorder()
	AdminCapabilityManagedDeploymentListHandler(svc)(depRec, depReq)
	if depRec.Code != http.StatusOK {
		t.Fatalf("deployment list status=%d body=%s", depRec.Code, depRec.Body.String())
	}
	var depResp struct {
		Items []capability.Deployment `json:"items"`
	}
	if err := json.Unmarshal(depRec.Body.Bytes(), &depResp); err != nil {
		t.Fatalf("decode deployments: %v", err)
	}
	if len(depResp.Items) != 1 || depResp.Items[0].CapabilityRef != capA.ID {
		t.Fatalf("unexpected deployments: %+v", depResp.Items)
	}

	recReq := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/recommendations", nil)
	recRec := httptest.NewRecorder()
	AdminCapabilityRecommendationListHandler(svc)(recRec, recReq)
	if recRec.Code != http.StatusOK {
		t.Fatalf("recommendation list status=%d body=%s", recRec.Code, recRec.Body.String())
	}
	var recResp struct {
		Items []capability.Recommendation `json:"items"`
	}
	if err := json.Unmarshal(recRec.Body.Bytes(), &recResp); err != nil {
		t.Fatalf("decode recommendations: %v", err)
	}
	if len(recResp.Items) != 1 || recResp.Items[0].CapabilityRef != capB.ID || !recResp.Items[0].AllowUserDismiss {
		t.Fatalf("unexpected recommendations: %+v", recResp.Items)
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

func TestAdminUserCapabilityComplianceNeedsConfig(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)

	created := createTestCapability(t, svc, "mcp", "needs-config", "Needs Config MCP", "1.0.0")
	createDeploymentForTest(t, svc, created.ID, created.CurrentVersionKey, map[string]any{"type": "user", "user_email": "config@example.com"}, "required")
	_, err := svc.UpsertUserCapabilityInventory(context.Background(), capability.UserCapabilityInventoryInput{UserID: "user-1", UserEmail: "config@example.com", CapabilityRef: created.ID, CapabilityVersionKey: created.CurrentVersionKey, CapabilityType: "mcp", InstallStatus: "needs_config", Installed: false})
	if err != nil {
		t.Fatalf("upsert needs config inventory: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/capability-market/users/config@example.com/compliance?status=needs_config", nil)
	req.SetPathValue("email", "config@example.com")
	rec := httptest.NewRecorder()
	AdminUserCapabilityComplianceHandler(svc, fakeCapabilityGroupResolver{chain: []string{"dept-a", "root"}})(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("compliance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items           []adminCapabilityComplianceItem  `json:"items"`
		Summary         adminCapabilityComplianceSummary `json:"summary"`
		FilteredSummary adminCapabilityComplianceSummary `json:"filtered_summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode compliance: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Status != "needs_config" || resp.Items[0].Installed || resp.Summary.NeedsConfig != 1 || resp.FilteredSummary.NeedsConfig != 1 {
		t.Fatalf("unexpected needs_config compliance: items=%+v summary=%+v filtered=%+v", resp.Items, resp.Summary, resp.FilteredSummary)
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

func TestUserCapabilityInventoryInfersInstalledFromStatus(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	body := []byte(`{"items":[{"capability_ref":"cap-missing","install_status":"missing"},{"capability_ref":"cap-config","install_status":"needs_config"},{"capability_ref":"cap-installed","install_status":"installed"}]}`)
	req := httptest.NewRequest(http.MethodPut, "/api/capabilities/inventory", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	UserCapabilityInventoryUpsertHandler(fakeMarketplaceViewerAuth{}, svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert inventory status=%d body=%s", rec.Code, rec.Body.String())
	}
	items, err := svc.ListUserCapabilityInventory(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("list inventory: %v", err)
	}
	byRef := map[string]capability.UserCapabilityInventoryItem{}
	for _, item := range items {
		byRef[item.CapabilityRef] = item
	}
	if byRef["cap-missing"].Installed || byRef["cap-config"].Installed || !byRef["cap-installed"].Installed {
		t.Fatalf("unexpected installed inference: %+v", byRef)
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
	if resp.Action != corelib.CapabilityInstallExternalDirect || resp.Capability.CapabilityType != corelib.CapabilityTypeMCP {
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

func TestCapabilityInstallIntentAllowsFreeDirectWithoutHubCenterImport(t *testing.T) {
	db := openCapabilityTestDB(t)
	svc := capability.NewService(db)
	settings := &testSystemSettingsRepo{}

	req := httptest.NewRequest(http.MethodPost, "/api/capabilities/missing-mcp/install-intent", bytes.NewReader([]byte(`{"capability_type":"mcp","source":"hubcenter","pricing":"free"}`)))
	req.SetPathValue("id", "missing-mcp")
	rec := httptest.NewRecorder()
	CapabilityInstallIntentHandler(svc, settings)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Action != corelib.CapabilityInstallExternalDirect {
		t.Fatalf("action = %q, want %q", resp.Action, corelib.CapabilityInstallExternalDirect)
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
	if resp.Action != corelib.CapabilityInstallExternalDirect {
		t.Fatalf("action = %q, want %q", resp.Action, corelib.CapabilityInstallExternalDirect)
	}
	items, err := svc.ListAcquisitionRequests(context.Background(), "")
	if err != nil || len(items) != 0 {
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
	if resp.Action != corelib.CapabilityInstallExternalDirect || resp.RequestID != "" || resp.Capability.ID == "" {
		t.Fatalf("unexpected response: %+v", resp)
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
	if err != nil || len(requests) != 0 {
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
	enterpriseOnlyInstall := true
	policy := corelib.DefaultCapabilityMarketPolicy()
	policy.EnterpriseOnlyInstall = &enterpriseOnlyInstall
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := settings.Set(context.Background(), capabilityMarketPolicySettingKey, string(raw)); err != nil {
		t.Fatalf("set policy: %v", err)
	}

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
