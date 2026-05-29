package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

func TestAdminCapabilityMarketMCPUpsertAndList(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	body := []byte(`{
		"publisher":"vendor-one",
		"capability_id":"billing-mcp",
		"display_name":"Billing MCP",
		"description":"Billing API tools",
		"version":"2.0.0",
		"pricing":{"mode":"paid","amount_cents":9900},
		"mcp":{"id":"billing-mcp","name":"Billing MCP","endpoint_url":"https://billing.example.com/mcp","auth_type":"bearer"},
		"secret_requirements":[{"name":"api_token","label":"API Token"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	AdminCapabilityMarketMCPUpsertHandler(settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created CapabilityMarketMCPEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.CapabilityType != corelib.CapabilityTypeMCP || created.Source != corelib.CapabilitySourceHubCenter {
		t.Fatalf("unexpected created entry: %+v", created)
	}
	if created.VersionKey == "" || len(created.SecretRequirements) != 1 || created.SecretRequirements[0].StoragePolicy != "hub_or_local" {
		t.Fatalf("unexpected normalized entry: %+v", created)
	}
	if created.SecretRequirements[0].Required == nil || !*created.SecretRequirements[0].Required {
		t.Fatalf("secret requirements should default to required=true: %+v", created.SecretRequirements[0])
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/capability-market/mcp?q=billing", nil)
	listRec := httptest.NewRecorder()
	CapabilityMarketMCPListHandler(settings)(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Items []CapabilityMarketMCPEntry `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Items) != 1 || listResp.Items[0].CapabilityID != "billing-mcp" {
		t.Fatalf("unexpected list response: %+v", listResp)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/capability-market/mcp/billing-mcp", nil)
	detailReq.SetPathValue("id", "billing-mcp")
	detailRec := httptest.NewRecorder()
	CapabilityMarketMCPDetailHandler(settings)(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}
}

func TestCapabilityMarketMCPPurchaseAllowsEmptyBodyForFreeCapability(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	body := []byte(`{"capability_id":"free-mcp","display_name":"Free MCP","mcp":{"endpoint_url":"https://free.example.com/mcp"}}`)
	upsertReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/mcp", bytes.NewReader(body))
	upsertRec := httptest.NewRecorder()
	AdminCapabilityMarketMCPUpsertHandler(settings)(upsertRec, upsertReq)
	if upsertRec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", upsertRec.Code, upsertRec.Body.String())
	}

	purchaseReq := httptest.NewRequest(http.MethodPost, "/api/capability-market/mcp/free-mcp/purchase", nil)
	purchaseReq.SetPathValue("id", "free-mcp")
	purchaseRec := httptest.NewRecorder()
	CapabilityMarketMCPPurchaseHandler(settings)(purchaseRec, purchaseReq)
	if purchaseRec.Code != http.StatusOK {
		t.Fatalf("purchase status=%d body=%s", purchaseRec.Code, purchaseRec.Body.String())
	}
}

func TestAdminCapabilityMarketMCPUpsertPreservesOptionalSecret(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	body := []byte(`{
		"capability_id":"optional-secret-mcp",
		"display_name":"Optional Secret MCP",
		"mcp":{"endpoint_url":"https://optional.example.com/mcp"},
		"secret_requirements":[{"name":"optional_token","required":false,"storage_policy":"local"}]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/mcp", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	AdminCapabilityMarketMCPUpsertHandler(settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created CapabilityMarketMCPEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if len(created.SecretRequirements) != 1 || created.SecretRequirements[0].Required == nil || *created.SecretRequirements[0].Required {
		t.Fatalf("optional secret requirement should stay required=false: %+v", created.SecretRequirements)
	}
}

func TestCapabilityMarketCustomerAccountUsesSettingsAndRequest(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	if err := settings.Set(context.Background(), "admin_email", `{"value":"owner@example.com"}`); err != nil {
		t.Fatalf("set admin email: %v", err)
	}
	if err := settings.Set(context.Background(), capabilityMarketPublicBaseURLKey, `{"value":"https://center.example.com/"}`); err != nil {
		t.Fatalf("set public base url: %v", err)
	}
	if err := settings.Set(context.Background(), capabilityMarketCustomerAccountKey, `{"customer_id":"cust-1","billing_email":"billing@example.com","billing_portal_url":"https://billing.example.com/portal"}`); err != nil {
		t.Fatalf("set customer account: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/capability-market/customer-account?hub_id=hub-1&admin_email=admin@example.com", nil)
	rec := httptest.NewRecorder()
	CapabilityMarketCustomerAccountHandler(settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var account CapabilityMarketCustomerAccount
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if account.Status != "configured" || account.CustomerID != "cust-1" || account.HubID != "hub-1" || account.AdminEmail != "admin@example.com" {
		t.Fatalf("unexpected account: %+v", account)
	}
	if account.BillingEmail != "billing@example.com" || account.BillingPortalURL != "https://billing.example.com/portal" || account.LoginURL != "https://center.example.com/admin" || account.RenewalURL != "https://center.example.com/marketplace" {
		t.Fatalf("unexpected urls/email: %+v", account)
	}
}

func TestCapabilityMarketCustomerAccountUsesTenantVirtualHub(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	req := httptest.NewRequest(http.MethodGet, "/api/capability-market/customer-account?hub_id=hub-1&tenant_id=tenant-a&admin_email=admin@example.com", nil)
	rec := httptest.NewRecorder()
	CapabilityMarketCustomerAccountHandler(settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var account CapabilityMarketCustomerAccount
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if account.CustomerID != "hub-1:tenant-a" || account.HubID != "hub-1" || account.TenantID != "tenant-a" || account.IdentitySource != "request" {
		t.Fatalf("unexpected tenant virtual hub account: %+v", account)
	}
}

func TestCapabilityMarketCustomerAccountUsesDefaultTenantVirtualHub(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	req := httptest.NewRequest(http.MethodGet, "/api/capability-market/customer-account?hub_id=hub-1&tenant_id=tenant_default&admin_email=admin@example.com", nil)
	rec := httptest.NewRecorder()
	CapabilityMarketCustomerAccountHandler(settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var account CapabilityMarketCustomerAccount
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if account.CustomerID != "hub-1:tenant_default" || account.HubID != "hub-1" || account.TenantID != "tenant_default" || account.IdentitySource != "request" {
		t.Fatalf("unexpected default tenant virtual hub account: %+v", account)
	}
}

func TestCapabilityMarketCustomerAccountFallsBackToAdminEmail(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	if err := settings.Set(context.Background(), "admin_email", `{"value":"owner@example.com"}`); err != nil {
		t.Fatalf("set admin email: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/capability-market/customer-account", nil)
	rec := httptest.NewRecorder()
	CapabilityMarketCustomerAccountHandler(settings)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var account CapabilityMarketCustomerAccount
	if err := json.Unmarshal(rec.Body.Bytes(), &account); err != nil {
		t.Fatalf("decode account: %v", err)
	}
	if account.Status != "configured" || account.CustomerID != "owner@example.com" || account.AdminEmail != "owner@example.com" || account.IdentitySource != "settings" {
		t.Fatalf("unexpected fallback account: %+v", account)
	}
	if account.TenantID != "" {
		t.Fatalf("fallback account without hub tenant context should not invent tenant id: %+v", account)
	}
}
func TestCapabilityMarketMCPPurchaseRequiresAdminEmailForPaid(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	upsertReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/mcp", bytes.NewReader([]byte(`{"capability_id":"paid-mcp","display_name":"Paid MCP","pricing":{"mode":"paid","amount_cents":9900},"mcp":{"endpoint_url":"https://paid.example.com/mcp"}}`)))
	upsertRec := httptest.NewRecorder()
	AdminCapabilityMarketMCPUpsertHandler(settings)(upsertRec, upsertReq)
	if upsertRec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", upsertRec.Code, upsertRec.Body.String())
	}

	missingReq := httptest.NewRequest(http.MethodPost, "/api/capability-market/mcp/paid-mcp/purchase", bytes.NewReader([]byte(`{"hub_id":"hub-1"}`)))
	missingReq.SetPathValue("id", "paid-mcp")
	missingRec := httptest.NewRecorder()
	CapabilityMarketMCPPurchaseHandler(settings)(missingRec, missingReq)
	if missingRec.Code != http.StatusBadRequest {
		t.Fatalf("missing admin status=%d body=%s", missingRec.Code, missingRec.Body.String())
	}

	purchaseReq := httptest.NewRequest(http.MethodPost, "/api/capability-market/mcp/paid-mcp/purchase", bytes.NewReader([]byte(`{"hub_id":"hub-1","tenant_id":"tenant-a","admin_email":"admin@example.com","request_id":"req-1"}`)))
	purchaseReq.SetPathValue("id", "paid-mcp")
	purchaseRec := httptest.NewRecorder()
	CapabilityMarketMCPPurchaseHandler(settings)(purchaseRec, purchaseReq)
	if purchaseRec.Code != http.StatusOK {
		t.Fatalf("purchase status=%d body=%s", purchaseRec.Code, purchaseRec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(purchaseRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode purchase: %v", err)
	}
	if resp["status"] != "purchased" || resp["admin_email"] != "admin@example.com" || resp["tenant_id"] != "tenant-a" {
		t.Fatalf("unexpected purchase response: %+v", resp)
	}
	licensesReq := httptest.NewRequest(http.MethodGet, "/api/capability-market/billing/licenses?hub_id=hub-1&tenant_id=tenant-a", nil)
	licensesRec := httptest.NewRecorder()
	CapabilityMarketBillingLicensesHandler(settings)(licensesRec, licensesReq)
	if licensesRec.Code != http.StatusOK {
		t.Fatalf("licenses status=%d body=%s", licensesRec.Code, licensesRec.Body.String())
	}
	var licenses struct {
		Items []CapabilityMarketMCPPurchaseRecord `json:"items"`
	}
	if err := json.Unmarshal(licensesRec.Body.Bytes(), &licenses); err != nil {
		t.Fatalf("decode licenses: %v", err)
	}
	if len(licenses.Items) != 1 || licenses.Items[0].CapabilityID != "paid-mcp" || licenses.Items[0].HubID != "hub-1" || licenses.Items[0].TenantID != "tenant-a" {
		t.Fatalf("unexpected licenses: %+v", licenses.Items)
	}
	otherTenantReq := httptest.NewRequest(http.MethodGet, "/api/capability-market/billing/licenses?hub_id=hub-1&tenant_id=tenant-b", nil)
	otherTenantRec := httptest.NewRecorder()
	CapabilityMarketBillingLicensesHandler(settings)(otherTenantRec, otherTenantReq)
	var otherTenant struct {
		Items []CapabilityMarketMCPPurchaseRecord `json:"items"`
	}
	if err := json.Unmarshal(otherTenantRec.Body.Bytes(), &otherTenant); err != nil {
		t.Fatalf("decode other tenant licenses: %v", err)
	}
	if len(otherTenant.Items) != 0 {
		t.Fatalf("expected tenant-b to be isolated from tenant-a purchase, got %+v", otherTenant.Items)
	}
	skillProvider := fakeSkillLicenseProvider{items: []CapabilityMarketLicenseRecord{{CapabilityType: corelib.CapabilityTypeSkill, CapabilityID: "paid-skill", Source: corelib.CapabilitySourceHubCenter, PurchaseID: "pur-skill", BuyerEmail: "admin@example.com", AdminEmail: "admin@example.com", Status: "active"}}}
	combinedReq := httptest.NewRequest(http.MethodGet, "/api/capability-market/billing/licenses?hub_id=hub-1&tenant_id=tenant-a&admin_email=admin@example.com", nil)
	combinedRec := httptest.NewRecorder()
	CapabilityMarketBillingLicensesHandler(settings, skillProvider)(combinedRec, combinedReq)
	if combinedRec.Code != http.StatusOK {
		t.Fatalf("combined licenses status=%d body=%s", combinedRec.Code, combinedRec.Body.String())
	}
	var combined struct {
		Items []CapabilityMarketLicenseRecord `json:"items"`
	}
	if err := json.Unmarshal(combinedRec.Body.Bytes(), &combined); err != nil {
		t.Fatalf("decode combined licenses: %v", err)
	}
	if len(combined.Items) != 2 {
		t.Fatalf("expected MCP + Skill licenses, got %+v", combined.Items)
	}
	for _, item := range combined.Items {
		if item.HubID != "hub-1" || item.TenantID != "tenant-a" {
			t.Fatalf("expected combined licenses to carry tenant virtual hub identity, got %+v", combined.Items)
		}
	}
}

func TestCapabilityMarketMCPPurchaseRequestIDIsTenantScoped(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	upsertReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/mcp", bytes.NewReader([]byte(`{"capability_id":"paid-mcp","display_name":"Paid MCP","pricing":{"mode":"paid"},"mcp":{"endpoint_url":"https://paid.example.com/mcp"}}`)))
	upsertRec := httptest.NewRecorder()
	AdminCapabilityMarketMCPUpsertHandler(settings)(upsertRec, upsertReq)
	if upsertRec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", upsertRec.Code, upsertRec.Body.String())
	}

	for _, tenantID := range []string{"tenant-a", "tenant-b"} {
		body, err := json.Marshal(map[string]string{"hub_id": "hub-1", "tenant_id": tenantID, "admin_email": "admin@example.com", "request_id": "same-request"})
		if err != nil {
			t.Fatalf("marshal purchase: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/capability-market/mcp/paid-mcp/purchase", bytes.NewReader(body))
		req.SetPathValue("id", "paid-mcp")
		rec := httptest.NewRecorder()
		CapabilityMarketMCPPurchaseHandler(settings)(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("purchase %s status=%d body=%s", tenantID, rec.Code, rec.Body.String())
		}
	}

	licensesReq := httptest.NewRequest(http.MethodGet, "/api/capability-market/billing/licenses?hub_id=hub-1", nil)
	licensesRec := httptest.NewRecorder()
	CapabilityMarketBillingLicensesHandler(settings)(licensesRec, licensesReq)
	var licenses struct {
		Items []CapabilityMarketMCPPurchaseRecord `json:"items"`
	}
	if err := json.Unmarshal(licensesRec.Body.Bytes(), &licenses); err != nil {
		t.Fatalf("decode licenses: %v", err)
	}
	if len(licenses.Items) != 2 {
		t.Fatalf("expected duplicate request_id to coexist across tenants, got %+v", licenses.Items)
	}
	seen := map[string]bool{}
	for _, item := range licenses.Items {
		seen[item.TenantID] = true
	}
	if !seen["tenant-a"] || !seen["tenant-b"] {
		t.Fatalf("missing tenant purchases: %+v", licenses.Items)
	}
}

func TestCapabilityMarketMCPPurchaseNormalizesDefaultTenant(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	upsertReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/mcp", bytes.NewReader([]byte(`{"capability_id":"paid-mcp","display_name":"Paid MCP","pricing":{"mode":"paid"},"mcp":{"endpoint_url":"https://paid.example.com/mcp"}}`)))
	upsertRec := httptest.NewRecorder()
	AdminCapabilityMarketMCPUpsertHandler(settings)(upsertRec, upsertReq)
	if upsertRec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", upsertRec.Code, upsertRec.Body.String())
	}

	purchaseReq := httptest.NewRequest(http.MethodPost, "/api/capability-market/mcp/paid-mcp/purchase", bytes.NewReader([]byte(`{"hub_id":"hub-1","tenant_id":"tenant_default","admin_email":"admin@example.com","request_id":"default-request"}`)))
	purchaseReq.SetPathValue("id", "paid-mcp")
	purchaseRec := httptest.NewRecorder()
	CapabilityMarketMCPPurchaseHandler(settings)(purchaseRec, purchaseReq)
	if purchaseRec.Code != http.StatusOK {
		t.Fatalf("purchase status=%d body=%s", purchaseRec.Code, purchaseRec.Body.String())
	}
	if !strings.Contains(purchaseRec.Body.String(), `"tenant_id":"tenant_default"`) {
		t.Fatalf("purchase response should expose tenant_default, body=%s", purchaseRec.Body.String())
	}

	licensesReq := httptest.NewRequest(http.MethodGet, "/api/capability-market/billing/licenses?hub_id=hub-1&tenant_id=tenant_default", nil)
	licensesRec := httptest.NewRecorder()
	skillProvider := fakeSkillLicenseProvider{items: []CapabilityMarketLicenseRecord{{CapabilityType: corelib.CapabilityTypeSkill, CapabilityID: "paid-skill", Source: corelib.CapabilitySourceHubCenter, PurchaseID: "pur-skill", BuyerEmail: "admin@example.com", AdminEmail: "admin@example.com", Status: "active"}}}
	CapabilityMarketBillingLicensesHandler(settings, skillProvider)(licensesRec, licensesReq)
	if licensesRec.Code != http.StatusOK {
		t.Fatalf("licenses status=%d body=%s", licensesRec.Code, licensesRec.Body.String())
	}
	var licenses struct {
		Items []CapabilityMarketMCPPurchaseRecord `json:"items"`
	}
	if err := json.Unmarshal(licensesRec.Body.Bytes(), &licenses); err != nil {
		t.Fatalf("decode licenses: %v", err)
	}
	if len(licenses.Items) != 2 {
		t.Fatalf("expected MCP + Skill default tenant licenses, got %+v body=%s", licenses.Items, licensesRec.Body.String())
	}
	for _, item := range licenses.Items {
		if item.TenantID != "tenant_default" {
			t.Fatalf("default tenant license should round-trip with public id, got %+v body=%s", licenses.Items, licensesRec.Body.String())
		}
	}
}

func TestCapabilityMarketBillingLicensesFiltersGenericSkillLicensesByTenant(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	skillProvider := fakeGenericSkillLicenseProvider{items: []CapabilityMarketLicenseRecord{
		{CapabilityType: corelib.CapabilityTypeSkill, CapabilityID: "tenant-a-skill", Source: corelib.CapabilitySourceHubCenter, PurchaseID: "pur-a", HubID: "hub-1", TenantID: "tenant-a", BuyerEmail: "admin@example.com", AdminEmail: "admin@example.com", Status: "active"},
		{CapabilityType: corelib.CapabilityTypeSkill, CapabilityID: "tenant-b-skill", Source: corelib.CapabilitySourceHubCenter, PurchaseID: "pur-b", HubID: "hub-1", TenantID: "tenant-b", BuyerEmail: "admin@example.com", AdminEmail: "admin@example.com", Status: "active"},
		{CapabilityType: corelib.CapabilityTypeSkill, CapabilityID: "unscoped-skill", Source: corelib.CapabilitySourceHubCenter, PurchaseID: "pur-unscoped", BuyerEmail: "admin@example.com", AdminEmail: "admin@example.com", Status: "active"},
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/capability-market/billing/licenses?hub_id=hub-1&tenant_id=tenant-a&admin_email=admin@example.com", nil)
	rec := httptest.NewRecorder()
	CapabilityMarketBillingLicensesHandler(settings, skillProvider)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []CapabilityMarketLicenseRecord `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode licenses: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].CapabilityID != "tenant-a-skill" || resp.Items[0].TenantID != "tenant-a" {
		t.Fatalf("tenant filter leaked generic skill licenses: %+v body=%s", resp.Items, rec.Body.String())
	}
}

func TestAdminCapabilityMarketMCPDelete(t *testing.T) {
	settings := &capabilityMarketSettingsRepo{values: map[string]string{}}
	upsertReq := httptest.NewRequest(http.MethodPost, "/api/admin/capability-market/mcp", bytes.NewReader([]byte(`{"capability_id":"tmp","display_name":"Tmp","mcp":{"endpoint_url":"https://tmp.example.com/mcp"}}`)))
	upsertRec := httptest.NewRecorder()
	AdminCapabilityMarketMCPUpsertHandler(settings)(upsertRec, upsertReq)
	if upsertRec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d body=%s", upsertRec.Code, upsertRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/capability-market/mcp/tmp", nil)
	deleteReq.SetPathValue("id", "tmp")
	deleteRec := httptest.NewRecorder()
	AdminCapabilityMarketMCPDeleteHandler(settings)(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	listRec := httptest.NewRecorder()
	CapabilityMarketMCPListHandler(settings)(listRec, httptest.NewRequest(http.MethodGet, "/api/capability-market/mcp", nil))
	var listResp struct {
		Items []CapabilityMarketMCPEntry `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Items) != 0 {
		t.Fatalf("expected empty list, got %+v", listResp.Items)
	}
}

func TestAdminCapabilityMarketExternalSearchPolicy(t *testing.T) {
	forbiddenReq := httptest.NewRequest(http.MethodGet, "/api/admin/capabilities/external-search?source=hub_center&type=skill", nil)
	forbiddenRec := httptest.NewRecorder()
	AdminCapabilityMarketExternalSearchHandler()(forbiddenRec, forbiddenReq)
	if forbiddenRec.Code != http.StatusForbidden {
		t.Fatalf("expected hubcenter source to be forbidden, got status=%d body=%s", forbiddenRec.Code, forbiddenRec.Body.String())
	}

	mcpReq := httptest.NewRequest(http.MethodGet, "/api/admin/capabilities/external-search?source=clawhub&type=mcp", nil)
	mcpRec := httptest.NewRecorder()
	AdminCapabilityMarketExternalSearchHandler()(mcpRec, mcpReq)
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
	if len(resp.Items) != 0 || len(resp.AllowedSources) != 2 || resp.AllowedSources[0] != corelib.CapabilitySourceClawHub || resp.AllowedSources[1] != corelib.CapabilitySourceGitHub {
		t.Fatalf("unexpected external search response: %+v", resp)
	}
}

type fakeSkillLicenseProvider struct {
	items []CapabilityMarketLicenseRecord
}

func (f fakeSkillLicenseProvider) CapabilityMarketSkillLicenses(ctx context.Context, buyerEmail string) ([]CapabilityMarketLicenseRecord, error) {
	items := make([]CapabilityMarketLicenseRecord, 0, len(f.items))
	for _, item := range f.items {
		if buyerEmail == "" || item.BuyerEmail == buyerEmail || item.AdminEmail == buyerEmail {
			items = append(items, item)
		}
	}
	return items, nil
}

func (f fakeSkillLicenseProvider) CapabilityMarketSkillLicensesForTenant(ctx context.Context, buyerEmail, hubID, tenantID string) ([]CapabilityMarketLicenseRecord, error) {
	items, err := f.CapabilityMarketSkillLicenses(ctx, buyerEmail)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].HubID = strings.TrimSpace(hubID)
		items[i].TenantID = strings.TrimSpace(tenantID)
	}
	return items, nil
}

type fakeGenericSkillLicenseProvider struct {
	items []CapabilityMarketLicenseRecord
}

func (f fakeGenericSkillLicenseProvider) CapabilityMarketSkillLicenses(ctx context.Context, buyerEmail string) ([]CapabilityMarketLicenseRecord, error) {
	items := make([]CapabilityMarketLicenseRecord, 0, len(f.items))
	for _, item := range f.items {
		if buyerEmail == "" || item.BuyerEmail == buyerEmail || item.AdminEmail == buyerEmail {
			items = append(items, item)
		}
	}
	return items, nil
}

type capabilityMarketSettingsRepo struct {
	values map[string]string
}

func (r *capabilityMarketSettingsRepo) Set(_ context.Context, key, valueJSON string) error {
	r.values[key] = valueJSON
	return nil
}

func (r *capabilityMarketSettingsRepo) Get(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}

func (r *capabilityMarketSettingsRepo) List(_ context.Context) ([]*store.SystemSettingEntry, error) {
	items := make([]*store.SystemSettingEntry, 0, len(r.values))
	for key, value := range r.values {
		items = append(items, &store.SystemSettingEntry{Key: key, ValueJSON: value})
	}
	return items, nil
}
