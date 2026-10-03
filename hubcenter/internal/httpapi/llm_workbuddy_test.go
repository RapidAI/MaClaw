package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

func TestAdminWorkBuddyLoginRejectsUnknownEdition(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers/workbuddy/login", strings.NewReader(`{"edition":"nope"}`))
	rec := httptest.NewRecorder()
	adminStartWorkBuddyLogin(svc)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAdminListRedactsWorkBuddySecrets(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:                    "workbuddy-global",
		Name:                  "WorkBuddy International",
		APIURL:                "https://www.workbuddy.ai/v2",
		APIKey:                "access-token",
		Protocol:              "openai",
		AuthKind:              llmpool.ProviderAuthWorkBuddy,
		WorkBuddyEdition:      "global",
		WorkBuddyRefreshToken: "refresh-token",
		WorkBuddyUserID:       "user-1",
		WorkBuddyEnterpriseID: "ent-1",
		WorkBuddyDomain:       "dept",
		Models:                []string{"gpt-5.4"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	rec := httptest.NewRecorder()
	adminListLLMProviders(svc)(rec, httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "access-token") || strings.Contains(body, "refresh-token") || strings.Contains(body, "user-1") || strings.Contains(body, "ent-1") {
		t.Fatalf("secrets leaked: %s", body)
	}
	var payload struct {
		Providers []struct {
			ID               string `json:"id"`
			AuthKind         string `json:"auth_kind"`
			WorkBuddyEdition string `json:"workbuddy_edition"`
			HasAPIKey        bool   `json:"has_api_key"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Providers) != 1 || payload.Providers[0].AuthKind != llmpool.ProviderAuthWorkBuddy || payload.Providers[0].WorkBuddyEdition != "global" || !payload.Providers[0].HasAPIKey {
		t.Fatalf("providers = %#v", payload.Providers)
	}
}

func TestAdminAddWorkBuddyRequiresLogin(t *testing.T) {
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers", strings.NewReader(`{
		"id":"workbuddy-cn","name":"WorkBuddy","auth_kind":"workbuddy","workbuddy_edition":"china","models":["glm-5.3"]
	}`))
	rec := httptest.NewRecorder()
	adminAddLLMProvider(svc)(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "login") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
