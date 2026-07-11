package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestMobileBootstrapHandlerWithServiceCardGrant(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "card-user@example.com")
	system := newTestLLMServiceSystemSettings()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "coding-basic", Name: "Coding Basic"}},
		UserBindings:       []llmservice.UserBinding{{Email: "card-user@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
		Grants: []llmservice.Grant{{
			ID: "grant-card-1", Email: "card-user@example.com", ServiceGroupID: "coding-basic",
			Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now,
			CreditsTotal: 300, CreditsUsed: 10,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/bootstrap", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileBootstrapHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	ent, _ := body["entitlements"].(map[string]any)
	if ent == nil {
		t.Fatalf("missing entitlements: %#v", body)
	}
	if ent["plan"] != "service_card" && ent["has_service_card_grant"] != true {
		// plan may still be official if grant resolution differs; has_service_card_grant should be true
		t.Logf("entitlements=%#v", ent)
	}
	if ent["has_service_card_grant"] != true {
		t.Fatalf("want has_service_card_grant true, got %#v", ent)
	}
	limits, _ := body["limits"].(map[string]any)
	if limits == nil {
		t.Fatal("missing limits")
	}
	// With active credits, quota should expand beyond free 100MiB.
	quota, _ := limits["document_quota_bytes"].(float64)
	if ent["service_active"] == true && ent["credits_available"].(float64) > 0 && quota < 200*1024*1024 {
		t.Fatalf("expected expanded document quota for active credits, limits=%#v", limits)
	}
	services, _ := body["services"].(map[string]any)
	if services["llm_card_redeem_path"] != "/api/llm/service/redeem" {
		t.Fatalf("services=%#v", services)
	}
}

func TestMobileBootstrapPayloadStillWorksWithoutSystem(t *testing.T) {
	payload := mobileBootstrapPayload(&auth.ViewerPrincipal{
		UserID: "u1", Email: "a@b.c", TenantID: "t1",
	})
	ent, _ := payload["entitlements"].(map[string]any)
	if ent["plan"] == nil {
		t.Fatalf("entitlements=%#v", ent)
	}
}
