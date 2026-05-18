package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "modernc.org/sqlite"
)

func newSkillMarketConfigTestHandlers(t *testing.T) *SkillMarketHandlers {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	return &SkillMarketHandlers{store: store}
}

func decodeTrialConfigResponse(t *testing.T, rr *httptest.ResponseRecorder) map[string]int {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]int
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}

func TestSkillMarketTrialConfigDefaults(t *testing.T) {
	h := newSkillMarketConfigTestHandlers(t)
	rr := httptest.NewRecorder()
	h.GetTrialConfig(rr, httptest.NewRequest(http.MethodGet, "/api/v1/admin/config/trial", nil))

	payload := decodeTrialConfigResponse(t, rr)
	if payload["trial_duration_days"] != 7 {
		t.Fatalf("trial_duration_days=%d, want 7", payload["trial_duration_days"])
	}
	if payload["auto_publish_threshold"] != 5 {
		t.Fatalf("auto_publish_threshold=%d, want 5", payload["auto_publish_threshold"])
	}
	if payload["max_uploads_per_hour"] != 0 {
		t.Fatalf("max_uploads_per_hour=%d, want 0", payload["max_uploads_per_hour"])
	}
}

func TestSkillMarketTrialConfigRoundTrip(t *testing.T) {
	h := newSkillMarketConfigTestHandlers(t)
	body := strings.NewReader(`{"trial_duration_days":14,"auto_publish_threshold":8,"max_uploads_per_hour":3}`)
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/trial", body)
	putReq.Header.Set("Content-Type", "application/json")
	putRR := httptest.NewRecorder()
	h.UpdateTrialConfig(putRR, putReq)
	if putRR.Code != http.StatusOK {
		t.Fatalf("expected update 200, got %d body=%s", putRR.Code, putRR.Body.String())
	}

	rr := httptest.NewRecorder()
	h.GetTrialConfig(rr, httptest.NewRequest(http.MethodGet, "/api/v1/admin/config/trial", nil))
	payload := decodeTrialConfigResponse(t, rr)
	if payload["trial_duration_days"] != 14 || payload["auto_publish_threshold"] != 8 || payload["max_uploads_per_hour"] != 3 {
		t.Fatalf("unexpected config payload: %#v", payload)
	}
}

func TestSkillMarketTrialConfigFallsBackForInvalidStoredValues(t *testing.T) {
	h := newSkillMarketConfigTestHandlers(t)
	ctx := context.Background()
	if err := h.store.SetConfig(ctx, "trial_duration_days", "bad"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetConfig(ctx, "auto_publish_threshold", "-1"); err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetConfig(ctx, "max_skill_uploads_per_hour", "bad"); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	h.GetTrialConfig(rr, httptest.NewRequest(http.MethodGet, "/api/v1/admin/config/trial", nil))
	payload := decodeTrialConfigResponse(t, rr)
	if payload["trial_duration_days"] != 7 || payload["auto_publish_threshold"] != 5 || payload["max_uploads_per_hour"] != 0 {
		t.Fatalf("unexpected fallback payload: %#v", payload)
	}
}

func TestSkillMarketTrialConfigRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "zero trial duration", body: `{"trial_duration_days":0}`},
		{name: "zero auto publish threshold", body: `{"auto_publish_threshold":0}`},
		{name: "negative max uploads", body: `{"max_uploads_per_hour":-1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newSkillMarketConfigTestHandlers(t)
			req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/config/trial", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			h.UpdateTrialConfig(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rr.Code, rr.Body.String())
			}

			readRR := httptest.NewRecorder()
			h.GetTrialConfig(readRR, httptest.NewRequest(http.MethodGet, "/api/v1/admin/config/trial", nil))
			payload := decodeTrialConfigResponse(t, readRR)
			if payload["trial_duration_days"] != 7 || payload["auto_publish_threshold"] != 5 || payload["max_uploads_per_hour"] != 0 {
				t.Fatalf("invalid request changed config: %#v", payload)
			}
		})
	}
}

func newSkillMarketPurchaseListTestHandlers(t *testing.T) *SkillMarketHandlers {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatal(err)
	}
	refundSvc := skillmarket.NewRefundService(store, nil, nil)
	return &SkillMarketHandlers{store: store, refundSvc: refundSvc}
}

func seedPurchaseRecord(t *testing.T, h *SkillMarketHandlers, id, buyerEmail, skillID string, tenant ...string) {
	t.Helper()
	hubID, tenantID := "", ""
	if len(tenant) > 0 {
		hubID = tenant[0]
	}
	if len(tenant) > 1 {
		tenantID = tenant[1]
	}
	err := h.store.CreatePurchase(context.Background(), &skillmarket.PurchaseRecord{
		ID:               id,
		HubID:            hubID,
		TenantID:         tenantID,
		BuyerEmail:       buyerEmail,
		BuyerID:          strings.ReplaceAll(buyerEmail, "@", "_"),
		SkillID:          skillID,
		PurchasedVersion: 1,
		PurchaseType:     "purchase",
		AmountPaid:       100,
		PlatformFee:      30,
		SellerEarning:    70,
		SellerID:         "seller-1",
		Status:           "active",
		CreatedAt:        time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCapabilityMarketSkillLicensesForTenantFiltersPurchases(t *testing.T) {
	h := newSkillMarketPurchaseListTestHandlers(t)
	seedPurchaseRecord(t, h, "pur-tenant-a", "admin@example.com", "skill.alpha", "hub-1", "tenant-a")
	seedPurchaseRecord(t, h, "pur-tenant-b", "admin@example.com", "skill.beta", "hub-1", "tenant-b")

	items, err := h.CapabilityMarketSkillLicensesForTenant(context.Background(), "admin@example.com", "hub-1", "tenant-a")
	if err != nil {
		t.Fatalf("CapabilityMarketSkillLicensesForTenant: %v", err)
	}
	if len(items) != 1 || items[0].CapabilityID != "skill.alpha" || items[0].HubID != "hub-1" || items[0].TenantID != "tenant-a" {
		t.Fatalf("unexpected tenant skill licenses: %+v", items)
	}
}

func TestAdminListPurchasesSupportsLegacyFilterEmail(t *testing.T) {
	h := newSkillMarketPurchaseListTestHandlers(t)
	seedPurchaseRecord(t, h, "pur-1", "alice@example.com", "skill.alpha")
	seedPurchaseRecord(t, h, "pur-2", "bob@example.com", "skill.beta")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/purchases?filter=alice@example.com", nil)
	h.AdminListPurchases(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Records []skillmarket.PurchaseRecord `json:"records"`
		Total   int                          `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 1 || len(payload.Records) != 1 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Records[0].BuyerEmail != "alice@example.com" {
		t.Fatalf("buyer=%s, want alice@example.com", payload.Records[0].BuyerEmail)
	}
}

func TestAdminListPurchasesSupportsLegacyFilterSkillID(t *testing.T) {
	h := newSkillMarketPurchaseListTestHandlers(t)
	seedPurchaseRecord(t, h, "pur-3", "alice@example.com", "skill.alpha")
	seedPurchaseRecord(t, h, "pur-4", "bob@example.com", "skill.beta")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/purchases?filter=skill.beta", nil)
	h.AdminListPurchases(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Records []skillmarket.PurchaseRecord `json:"records"`
		Total   int                          `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Total != 1 || len(payload.Records) != 1 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Records[0].SkillID != "skill.beta" {
		t.Fatalf("skill_id=%s, want skill.beta", payload.Records[0].SkillID)
	}
}
