package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

func newIndustryManagementTestHandler(t *testing.T) *IndustryManagementHandlers {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// SQLite :memory: databases are connection-local. The handlers perform
	// successive queries, so pin this test database to one connection.
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	marketStore, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	return NewIndustryManagementHandlers(&SkillMarketHandlers{store: marketStore}, nil)
}

func TestIndustryCatalogueRevisionChangesOnlyForEffectiveContent(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, err := h.db.ExecContext(ctx, `INSERT INTO industry_catalog_industries(id,code,name,status,created_at,updated_at) VALUES('i1','finance','Finance','active','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.db.ExecContext(ctx, `INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,definition_json,package_hash,status,created_at,updated_at) VALUES('a1','l1','e1','1.0.0',0,'Analyst','{"name":"Analyst","system_prompt":"prompt"}','hash','ready','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.db.ExecContext(ctx, `INSERT INTO industry_catalog_bindings(industry_id,asset_id,status,created_at,updated_at) VALUES('i1','a1','active','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.db.ExecContext(ctx, `INSERT INTO hub_tenant_industry_assignments(hub_id,tenant_id,industry_id,created_at,updated_at) VALUES('h1','t1','i1','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.refreshCataloguesForIndustry(ctx, "i1"); err != nil {
		t.Fatal(err)
	}
	var revision int
	if err := h.db.QueryRow(`SELECT revision FROM hub_tenant_industry_catalogs WHERE hub_id='h1' AND tenant_id='t1'`).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("first revision = %d, err=%v, want 1", revision, err)
	}
	if err := h.refreshCataloguesForIndustry(ctx, "i1"); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow(`SELECT revision FROM hub_tenant_industry_catalogs WHERE hub_id='h1' AND tenant_id='t1'`).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("stable revision = %d, err=%v, want 1", revision, err)
	}
	if _, err := h.db.Exec(`UPDATE industry_catalog_assets SET status='revoked' WHERE id='a1'`); err != nil {
		t.Fatal(err)
	}
	if err := h.refreshCataloguesForAsset(ctx, "a1"); err != nil {
		t.Fatal(err)
	}
	if err := h.db.QueryRow(`SELECT revision FROM hub_tenant_industry_catalogs WHERE hub_id='h1' AND tenant_id='t1'`).Scan(&revision); err != nil || revision != 2 {
		t.Fatalf("revoked revision = %d, err=%v, want 2", revision, err)
	}
	experts, err := h.catalogue(ctx, "h1", "t1")
	if err != nil || len(experts) != 0 {
		t.Fatalf("revoked catalogue = %#v, err=%v, want empty", experts, err)
	}
}

func TestIndustryAssetRevokeRequiresReasonAndAudits(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	_, err := h.db.Exec(`INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,definition_json,package_hash,status,created_at,updated_at) VALUES('a1','l1','e1','1',0,'Name','{"name":"Name","system_prompt":"prompt"}','hash','ready','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.refreshCataloguesForAsset(context.Background(), "a1"); err != nil {
		t.Fatalf("pre-revoke catalogue refresh: %v", err)
	}
	// Handler uses PathValue, so invoke a parameterized request directly.
	req := httptest.NewRequest(http.MethodPost, "/assets/a1/revoke", strings.NewReader(`{"reason":"copyright withdrawal"}`))
	req.SetPathValue("id", "a1")
	res := httptest.NewRecorder()
	h.revokeAsset(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("revoke status=%d body=%s", res.Code, res.Body.String())
	}
	var status string
	if err := h.db.QueryRow(`SELECT status FROM industry_catalog_assets WHERE id='a1'`).Scan(&status); err != nil || status != "revoked" {
		t.Fatalf("asset status=%q err=%v", status, err)
	}
	var action, reason string
	if err := h.db.QueryRow(`SELECT action,reason FROM industry_catalog_audit_events WHERE target_id='a1'`).Scan(&action, &reason); err != nil || action != "asset.revoked" || reason != "copyright withdrawal" {
		t.Fatalf("audit action=%q reason=%q err=%v", action, reason, err)
	}
	var reply map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &reply); err != nil || reply["status"] != "revoked" {
		t.Fatalf("revoke reply=%s err=%v", res.Body.String(), err)
	}
}

func TestIndustryAssetListDoesNotLeakDefinition(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	_, err := h.db.Exec(`INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,description,definition_json,package_hash,status,created_at,updated_at) VALUES('a1','l1','e1','1',0,'Name','safe metadata','{"name":"Name","system_prompt":"secret prompt"}','hash','ready','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/assets", nil)
	res := httptest.NewRecorder()
	h.listEligibleAssets(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("asset list status=%d body=%s", res.Code, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "secret prompt") {
		t.Fatalf("asset list leaked definition: %s", res.Body.String())
	}
}

func TestGeneralIndustryIsBuiltInAndUsedWhenTenantHasNoExplicitSetting(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	var general industryRecord
	if err := h.db.QueryRow(`SELECT id,code,name,status FROM industry_catalog_industries WHERE id=?`, generalIndustryID).Scan(&general.ID, &general.Code, &general.Name, &general.Status); err != nil {
		t.Fatalf("general industry missing: %v", err)
	}
	if general.Code != generalIndustryCode || general.Name != "通用行业" || general.Status != "active" {
		t.Fatalf("unexpected general industry: %#v", general)
	}
	_, err := h.db.Exec(`INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,definition_json,package_hash,status,created_at,updated_at) VALUES('general-a1','general-l1','general-e1','1',0,'General analyst','{"name":"General analyst","system_prompt":"prompt"}','hash','ready','now','now')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO industry_catalog_bindings(industry_id,asset_id,status,created_at,updated_at) VALUES(?, 'general-a1', 'active', 'now', 'now')`, generalIndustryID); err != nil {
		t.Fatal(err)
	}
	experts, err := h.catalogue(context.Background(), "h-general", "t-general")
	if err != nil || len(experts) != 1 || experts[0].AssetID != "general-a1" || len(experts[0].Industries) != 1 || experts[0].Industries[0].ID != generalIndustryID {
		t.Fatalf("fallback catalogue=%#v err=%v", experts, err)
	}
	req := httptest.NewRequest(http.MethodGet, "/hubs/h-general/tenants/t-general/industries", nil)
	req.SetPathValue("hubId", "h-general")
	req.SetPathValue("tenantId", "t-general")
	res := httptest.NewRecorder()
	h.listTenantIndustries(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("list tenant industries status=%d body=%s", res.Code, res.Body.String())
	}
	var reply struct {
		IndustryIDs  []string         `json:"industry_ids"`
		Industries   []industryRecord `json:"industries"`
		UsingDefault bool             `json:"using_default"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &reply); err != nil || !reply.UsingDefault || len(reply.IndustryIDs) != 0 || len(reply.Industries) != 1 || reply.Industries[0].ID != generalIndustryID {
		t.Fatalf("fallback tenant reply=%s parsed=%#v err=%v", res.Body.String(), reply, err)
	}
}

func TestExplicitTenantIndustriesReplaceAndCanReturnToGeneralFallback(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, row := range []struct{ id, code, name string }{{"finance-i1", "finance", "Finance"}} {
		if _, err := h.db.Exec(`INSERT INTO industry_catalog_industries(id,code,name,status,created_at,updated_at) VALUES(?,?,?,'active','now','now')`, row.id, row.code, row.name); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct{ id, listing, name string }{{"general-a1", "general-l1", "General"}, {"finance-a1", "finance-l1", "Finance"}} {
		if _, err := h.db.Exec(`INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,definition_json,package_hash,status,created_at,updated_at) VALUES(?,?,?,'1',0,?,'{"name":"Name","system_prompt":"prompt"}','hash','ready','now','now')`, row.id, row.listing, row.id, row.name); err != nil {
			t.Fatal(err)
		}
	}
	for _, binding := range [][2]string{{generalIndustryID, "general-a1"}, {"finance-i1", "finance-a1"}} {
		if _, err := h.db.Exec(`INSERT INTO industry_catalog_bindings(industry_id,asset_id,status,created_at,updated_at) VALUES(?,?,'active','now','now')`, binding[0], binding[1]); err != nil {
			t.Fatal(err)
		}
	}
	if experts, err := h.catalogue(ctx, "h1", "t1"); err != nil || len(experts) != 1 || experts[0].AssetID != "general-a1" {
		t.Fatalf("initial fallback=%#v err=%v", experts, err)
	}
	put := func(body string) {
		req := httptest.NewRequest(http.MethodPut, "/hubs/h1/tenants/t1/industries", strings.NewReader(body))
		req.SetPathValue("hubId", "h1")
		req.SetPathValue("tenantId", "t1")
		res := httptest.NewRecorder()
		h.replaceTenantIndustries(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("replace industries status=%d body=%s", res.Code, res.Body.String())
		}
	}
	put(`{"industry_ids":["finance-i1"]}`)
	if experts, err := h.catalogue(ctx, "h1", "t1"); err != nil || len(experts) != 1 || experts[0].AssetID != "finance-a1" {
		t.Fatalf("explicit catalogue=%#v err=%v", experts, err)
	}
	put(`{"industry_ids":[]}`)
	if experts, err := h.catalogue(ctx, "h1", "t1"); err != nil || len(experts) != 1 || experts[0].AssetID != "general-a1" {
		t.Fatalf("returned fallback=%#v err=%v", experts, err)
	}
}

func TestTenantIndustryAssignmentAuditUsesCanonicalSelection(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO industry_catalog_industries(id,code,name,status,created_at,updated_at) VALUES('finance-i1','finance','Finance','active','now','now')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/hubs/h1/tenants/t1/industries", strings.NewReader(`{"industry_ids":[" finance-i1 ","","finance-i1"]}`))
	req.SetPathValue("hubId", "h1")
	req.SetPathValue("tenantId", "t1")
	res := httptest.NewRecorder()
	h.replaceTenantIndustries(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("replace tenant industries status=%d body=%s", res.Code, res.Body.String())
	}
	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM hub_tenant_industry_assignments WHERE hub_id='h1' AND tenant_id='t1' AND industry_id='finance-i1'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("persisted assignment count=%d err=%v", count, err)
	}
	var afterJSON string
	if err := h.db.QueryRow(`SELECT after_json FROM industry_catalog_audit_events WHERE action='tenant.industries.replaced'`).Scan(&afterJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(afterJSON, `"industry_ids":["finance-i1"]`) {
		t.Fatalf("audit should contain canonical industry IDs: %s", afterJSON)
	}
}

func TestGeneralIndustryBindingRefreshesExistingFallbackCatalogue(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := h.refreshTenantCatalogueRevision(ctx, "h1", "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,definition_json,package_hash,status,created_at,updated_at) VALUES('general-a1','general-l1','general-e1','1',0,'General','{"name":"General","system_prompt":"prompt"}','hash','ready','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO industry_catalog_bindings(industry_id,asset_id,status,created_at,updated_at) VALUES(?, 'general-a1', 'active', 'now', 'now')`, generalIndustryID); err != nil {
		t.Fatal(err)
	}
	if err := h.refreshCataloguesForIndustry(ctx, generalIndustryID); err != nil {
		t.Fatal(err)
	}
	var revision int
	if err := h.db.QueryRow(`SELECT revision FROM hub_tenant_industry_catalogs WHERE hub_id='h1' AND tenant_id='t1'`).Scan(&revision); err != nil || revision != 2 {
		t.Fatalf("general binding revision=%d err=%v, want 2", revision, err)
	}
}

func TestBindingOrderIsContiguousAfterDuplicateAndBlankInput(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO industry_catalog_industries(id,code,name,status,created_at,updated_at) VALUES('finance-i1','finance','Finance','active','now','now')`); err != nil {
		t.Fatal(err)
	}
	for _, assetID := range []string{"asset-a", "asset-b"} {
		if _, err := h.db.Exec(`INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,definition_json,package_hash,status,created_at,updated_at) VALUES(?,?,?,'1',0,?,'{"name":"Name","system_prompt":"prompt"}','hash','ready','now','now')`, assetID, assetID+"-listing", assetID+"-expert", assetID); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPut, "/industries/finance-i1/bindings", strings.NewReader(`{"asset_ids":["asset-a","","asset-a","asset-b"]}`))
	req.SetPathValue("id", "finance-i1")
	res := httptest.NewRecorder()
	h.replaceBindings(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("replace bindings status=%d body=%s", res.Code, res.Body.String())
	}
	rows, err := h.db.Query(`SELECT asset_id,display_order FROM industry_catalog_bindings WHERE industry_id='finance-i1' ORDER BY display_order`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var assetID string
		var order int
		if err := rows.Scan(&assetID, &order); err != nil {
			t.Fatal(err)
		}
		got = append(got, assetID+":"+fmt.Sprint(order))
	}
	if strings.Join(got, ",") != "asset-a:0,asset-b:1" {
		t.Fatalf("binding order=%v", got)
	}
	var afterJSON string
	if err := h.db.QueryRow(`SELECT after_json FROM industry_catalog_audit_events WHERE action='industry.bindings.replaced'`).Scan(&afterJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(afterJSON, `"asset_ids":["asset-a","asset-b"]`) {
		t.Fatalf("audit should contain canonical asset IDs: %s", afterJSON)
	}
}

func TestGeneralIndustryCannotBeDisabled(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/industries/industry_general", strings.NewReader(`{"status":"disabled"}`))
	req.SetPathValue("id", generalIndustryID)
	res := httptest.NewRecorder()
	h.patchIndustry(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "GENERAL_INDUSTRY_REQUIRED") {
		t.Fatalf("disable general status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestGeneralIndustryMetadataIsSystemManaged(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/industries/industry_general", strings.NewReader(`{"name":"Tenant default","description":"custom","icon":"x","sort_order":99}`))
	req.SetPathValue("id", generalIndustryID)
	res := httptest.NewRecorder()
	h.patchIndustry(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "GENERAL_INDUSTRY_MANAGED") {
		t.Fatalf("update general metadata status=%d body=%s", res.Code, res.Body.String())
	}
	var got industryRecord
	if err := h.db.QueryRow(`SELECT id,code,name,description,icon,sort_order,status FROM industry_catalog_industries WHERE id=?`, generalIndustryID).Scan(&got.ID, &got.Code, &got.Name, &got.Description, &got.Icon, &got.SortOrder, &got.Status); err != nil {
		t.Fatal(err)
	}
	if got.Code != generalIndustryCode || got.Name != "通用行业" || got.Description != "租户未配置行业时使用的系统默认行业" || got.Icon != "🌐" || got.SortOrder != -1000 || got.Status != "active" {
		t.Fatalf("general industry was modified: %#v", got)
	}
}

func TestSchemaInitializationRepairsGeneralIndustry(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE industry_catalog_industries (id TEXT PRIMARY KEY, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', icon TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO industry_catalog_industries(id,code,name,description,icon,sort_order,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, generalIndustryID, "renamed", "Deprecated", "manual", "x", 42, "disabled", "old", "old"); err != nil {
		t.Fatal(err)
	}
	h := &IndustryManagementHandlers{db: db}
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	var got industryRecord
	if err := db.QueryRow(`SELECT id,code,name,description,icon,sort_order,status FROM industry_catalog_industries WHERE id=?`, generalIndustryID).Scan(&got.ID, &got.Code, &got.Name, &got.Description, &got.Icon, &got.SortOrder, &got.Status); err != nil {
		t.Fatal(err)
	}
	if got.Code != generalIndustryCode || got.Name != "通用行业" || got.Description != "租户未配置行业时使用的系统默认行业" || got.Icon != "🌐" || got.SortOrder != -1000 || got.Status != "active" {
		t.Fatalf("general industry was not repaired: %#v", got)
	}
}

func TestTenantIndustryStatusMaterializesGeneralFallbackRevision(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,definition_json,package_hash,status,created_at,updated_at) VALUES('general-a1','general-l1','general-e1','1',0,'General','{"name":"General","system_prompt":"prompt"}','hash','ready','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO industry_catalog_bindings(industry_id,asset_id,status,created_at,updated_at) VALUES(?, 'general-a1', 'active', 'now', 'now')`, generalIndustryID); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/hubs/h1/tenants/t1/industry-expert-status", nil)
	req.SetPathValue("hubId", "h1")
	req.SetPathValue("tenantId", "t1")
	res := httptest.NewRecorder()
	h.tenantIndustryStatus(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("tenant status=%d body=%s", res.Code, res.Body.String())
	}
	var reply struct {
		Revision    int64 `json:"revision"`
		ExpertCount int   `json:"expert_count"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &reply); err != nil || reply.Revision != 1 || reply.ExpertCount != 1 {
		t.Fatalf("tenant status reply=%s parsed=%#v err=%v", res.Body.String(), reply, err)
	}
}

func TestIndustryPatchAuditsPriorValues(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Exec(`INSERT INTO industry_catalog_industries(id,code,name,status,created_at,updated_at) VALUES('finance-i1','finance','Finance','active','now','now')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/industries/finance-i1", strings.NewReader(`{"name":"Finance updated","reason":"correct label"}`))
	req.SetPathValue("id", "finance-i1")
	res := httptest.NewRecorder()
	h.patchIndustry(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", res.Code, res.Body.String())
	}
	var beforeJSON, afterJSON string
	if err := h.db.QueryRow(`SELECT before_json,after_json FROM industry_catalog_audit_events WHERE target_id='finance-i1'`).Scan(&beforeJSON, &afterJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(beforeJSON, `"name":"Finance"`) || !strings.Contains(afterJSON, `"name":"Finance updated"`) {
		t.Fatalf("unexpected audit before=%s after=%s", beforeJSON, afterJSON)
	}
}

func TestIndustrySchemaFailureIsStableAcrossCalls(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE industry_catalog_industries (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	h := &IndustryManagementHandlers{db: db}
	first := h.ensureSchema()
	second := h.ensureSchema()
	if first == nil || second == nil || first.Error() != second.Error() {
		t.Fatalf("schema errors first=%v second=%v; failure must remain visible", first, second)
	}
}

func TestCanonicalCatalogHashIsStableAndDoesNotMutateInput(t *testing.T) {
	experts := []catalogExpert{{
		AssetID:    "asset-b",
		Industries: []catalogIndustry{{ID: "industry-z", Name: "Z"}, {ID: "industry-a", Name: "A"}},
	}, {
		AssetID:    "asset-a",
		Industries: []catalogIndustry{{ID: "industry-b", Name: "B"}},
	}}
	before, err := json.Marshal(experts)
	if err != nil {
		t.Fatal(err)
	}
	first := canonicalCatalogHash(experts)
	second := canonicalCatalogHash([]catalogExpert{{AssetID: "asset-a", Industries: []catalogIndustry{{ID: "industry-b", Name: "B"}}}, {AssetID: "asset-b", Industries: []catalogIndustry{{ID: "industry-a", Name: "A"}, {ID: "industry-z", Name: "Z"}}}})
	after, err := json.Marshal(experts)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || string(before) != string(after) {
		t.Fatalf("hash stable=%t inputUnchanged=%t before=%s after=%s", first == second, string(before) == string(after), before, after)
	}
}

func TestTenantCannotExplicitlySelectGeneralIndustry(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPut, "/hubs/h1/tenants/t1/industries", strings.NewReader(`{"industry_ids":["industry_general"]}`))
	req.SetPathValue("hubId", "h1")
	req.SetPathValue("tenantId", "t1")
	res := httptest.NewRecorder()
	h.replaceTenantIndustries(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), "GENERAL_INDUSTRY_IMPLICIT") {
		t.Fatalf("select general status=%d body=%s", res.Code, res.Body.String())
	}
	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM hub_tenant_industry_assignments WHERE hub_id='h1' AND tenant_id='t1'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("general must not create assignment count=%d err=%v", count, err)
	}
}

func TestLegacyExplicitGeneralAssignmentUsesImplicitFallback(t *testing.T) {
	h := newIndustryManagementTestHandler(t)
	if err := h.ensureSchema(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := h.db.Exec(`INSERT INTO industry_catalog_industries(id,code,name,status,created_at,updated_at) VALUES('finance-i1','finance','Finance','active','now','now')`); err != nil {
		t.Fatal(err)
	}
	for _, assetID := range []string{"general-a1", "finance-a1"} {
		if _, err := h.db.Exec(`INSERT INTO industry_catalog_assets(id,listing_id,source_expert_id,version,price,name,definition_json,package_hash,status,created_at,updated_at) VALUES(?,?,?,'1',0,?,'{"name":"Name","system_prompt":"prompt"}','hash','ready','now','now')`, assetID, assetID+"-listing", assetID+"-expert", assetID); err != nil {
			t.Fatal(err)
		}
	}
	for _, binding := range [][2]string{{generalIndustryID, "general-a1"}, {"finance-i1", "finance-a1"}} {
		if _, err := h.db.Exec(`INSERT INTO industry_catalog_bindings(industry_id,asset_id,status,created_at,updated_at) VALUES(?,?,'active','now','now')`, binding[0], binding[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.db.Exec(`INSERT INTO hub_tenant_industry_assignments(hub_id,tenant_id,industry_id,created_at,updated_at) VALUES('h1','t1',?,'now','now')`, generalIndustryID); err != nil {
		t.Fatal(err)
	}
	if experts, err := h.catalogue(ctx, "h1", "t1"); err != nil || len(experts) != 1 || experts[0].AssetID != "general-a1" {
		t.Fatalf("legacy general fallback=%#v err=%v", experts, err)
	}
	if _, err := h.db.Exec(`INSERT INTO hub_tenant_industry_assignments(hub_id,tenant_id,industry_id,created_at,updated_at) VALUES('h1','t1','finance-i1','now','now')`); err != nil {
		t.Fatal(err)
	}
	if experts, err := h.catalogue(ctx, "h1", "t1"); err != nil || len(experts) != 1 || experts[0].AssetID != "finance-a1" {
		t.Fatalf("legacy general must not combine with explicit industry: %#v err=%v", experts, err)
	}
}
