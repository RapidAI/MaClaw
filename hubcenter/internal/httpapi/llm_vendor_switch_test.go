package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

// Switching a provider from one account login to another must not carry the
// previous account's access token across. The admin form always renders the
// key field empty, so the handler restores the stored token from the existing
// row; restoring it for a different auth kind would leave a credential the new
// upstream never issued, and every chat request would fail against it.
func TestAdminUpdateLLMProviderSwitchVendorDropsAccessToken(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		seed   llmpool.ProviderConfig
		update string
		kind   string
	}{
		{
			name: "trae to qoder",
			seed: llmpool.ProviderConfig{
				ID: "p", Name: "Trae 国内版", APIURL: "https://trae-api-cn.mchost.guru",
				APIKey: "trae-access", Protocol: "openai", AuthKind: llmpool.ProviderAuthTrae,
				TraeEdition: "china", TraeRefreshToken: "trae-refresh", TraeUserID: "tu",
				TraeMachineID: "tm", TraeDeviceID: "td", Models: []string{"gpt-5"},
			},
			update: `{"name":"Qoder 国内版","api_url":"https://api2-v2.qoder.sh/model/v1","auth_kind":"qoder","qoder_edition":"china","models":["qwen3.8-max"]}`,
			kind:   llmpool.ProviderAuthQoder,
		},
		{
			name: "trae to lobsterai",
			seed: llmpool.ProviderConfig{
				ID: "p", Name: "Trae 国内版", APIURL: "https://trae-api-cn.mchost.guru",
				APIKey: "trae-access", Protocol: "openai", AuthKind: llmpool.ProviderAuthTrae,
				TraeEdition: "china", TraeRefreshToken: "trae-refresh", TraeUserID: "tu",
				TraeMachineID: "tm", TraeDeviceID: "td", Models: []string{"gpt-5"},
			},
			update: `{"name":"LobsterAI","api_url":"https://lobsterai-server.youdao.com","auth_kind":"lobsterai","models":["glm-5.3"]}`,
			kind:   llmpool.ProviderAuthLobsterAI,
		},
		{
			name: "workbuddy to qoder",
			seed: llmpool.ProviderConfig{
				ID: "p", Name: "WorkBuddy", APIURL: "https://copilot.tencent.com/v2",
				APIKey: "wb-access", Protocol: "openai", AuthKind: llmpool.ProviderAuthWorkBuddy,
				WorkBuddyEdition: "china", WorkBuddyRefreshToken: "wb-refresh",
				WorkBuddyUserID: "wu", Models: []string{"glm-5.3"},
			},
			update: `{"name":"Qoder 国内版","api_url":"https://api2-v2.qoder.sh/model/v1","auth_kind":"qoder","qoder_edition":"china","models":["qwen3.8-max"]}`,
			kind:   llmpool.ProviderAuthQoder,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
			if err := svc.AddProvider(ctx, tc.seed); err != nil {
				t.Fatalf("add: %v", err)
			}
			req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/p", strings.NewReader(tc.update))
			req.SetPathValue("id", "p")
			rec := httptest.NewRecorder()
			adminUpdateLLMProvider(svc)(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			stored, err := svc.GetProvider(ctx, "p")
			if err != nil || stored == nil {
				t.Fatalf("stored err=%v provider=%#v", err, stored)
			}
			if stored.AuthKind != tc.kind {
				t.Fatalf("auth kind = %q want %q", stored.AuthKind, tc.kind)
			}
			if stored.APIKey == tc.seed.APIKey {
				t.Fatalf("access token %q was carried across the vendor switch", stored.APIKey)
			}
			if stored.APIKey != "" {
				t.Fatalf("access token = %q, want empty until the new login is applied", stored.APIKey)
			}
		})
	}
}

// The same vendor saving without a new login must keep its access token: the
// admin form omits the credential on every save, and dropping it would log the
// provider out.
func TestAdminUpdateLLMProviderSameVendorKeepsAccessToken(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "qoder-cn", Name: "Qoder 国内版", APIURL: "https://api2-v2.qoder.sh/model/v1",
		APIKey: "qoder-access", Protocol: "openai", AuthKind: llmpool.ProviderAuthQoder,
		QoderEdition: "china", QoderRefreshToken: "qoder-refresh", QoderUserID: "qu",
		QoderMachineID: "qm", Models: []string{"qwen3.8-max"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/qoder-cn",
		strings.NewReader(`{"name":"Qoder 国内版","api_url":"https://api2-v2.qoder.sh/model/v1","auth_kind":"qoder","qoder_edition":"china","models":["qwen3.8-max"]}`))
	req.SetPathValue("id", "qoder-cn")
	rec := httptest.NewRecorder()
	adminUpdateLLMProvider(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := svc.GetProvider(ctx, "qoder-cn")
	if err != nil || stored == nil {
		t.Fatalf("stored err=%v provider=%#v", err, stored)
	}
	if stored.APIKey != "qoder-access" {
		t.Fatalf("access token = %q, want the stored one", stored.APIKey)
	}
	if stored.QoderRefreshToken != "qoder-refresh" || stored.QoderUserID != "qu" || stored.QoderMachineID != "qm" {
		t.Fatalf("credential = %#v", stored)
	}
}

// A provider converted to a static key must not borrow the vendor token the
// handler would otherwise restore, or the row would authenticate with an
// account the admin deliberately dropped.
func TestAdminUpdateLLMProviderToStaticKeyDropsAccessToken(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "trae-cn", Name: "Trae 国内版", APIURL: "https://trae-api-cn.mchost.guru",
		APIKey: "trae-access", Protocol: "openai", AuthKind: llmpool.ProviderAuthTrae,
		TraeEdition: "china", TraeRefreshToken: "trae-refresh", TraeUserID: "tu",
		TraeMachineID: "tm", TraeDeviceID: "td", Models: []string{"gpt-5"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/trae-cn",
		strings.NewReader(`{"name":"Trae 国内版","api_url":"https://trae-api-cn.mchost.guru","api_key":"sk-manual","protocol":"openai","auth_kind":"api_key","models":["gpt-5"]}`))
	req.SetPathValue("id", "trae-cn")
	rec := httptest.NewRecorder()
	adminUpdateLLMProvider(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := svc.GetProvider(ctx, "trae-cn")
	if err != nil || stored == nil {
		t.Fatalf("stored err=%v provider=%#v", err, stored)
	}
	if stored.APIKey != "sk-manual" {
		t.Fatalf("api key = %q, want the static key the admin typed", stored.APIKey)
	}
	if stored.AuthKind == llmpool.ProviderAuthTrae {
		t.Fatalf("auth kind = %q, want the vendor account dropped", stored.AuthKind)
	}
	if stored.TraeRefreshToken != "" || stored.TraeUserID != "" || stored.TraeMachineID != "" || stored.TraeDeviceID != "" {
		t.Fatalf("vendor credential survived = %#v", stored)
	}
}

// An update that omits auth_kind entirely (the array-move payload) must keep
// both the vendor account and its access token.
func TestAdminUpdateLLMProviderWithoutAuthKindKeepsAccount(t *testing.T) {
	ctx := context.Background()
	svc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID: "trae-cn", Name: "Trae 国内版", APIURL: "https://trae-api-cn.mchost.guru",
		APIKey: "trae-access", Protocol: "openai", AuthKind: llmpool.ProviderAuthTrae,
		TraeEdition: "china", TraeRefreshToken: "trae-refresh", TraeUserID: "tu",
		TraeMachineID: "tm", TraeDeviceID: "td", Models: []string{"gpt-5"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers/trae-cn",
		strings.NewReader(`{"name":"Trae 国内版","api_url":"https://trae-api-cn.mchost.guru","protocol":"openai","models":["gpt-5"]}`))
	req.SetPathValue("id", "trae-cn")
	rec := httptest.NewRecorder()
	adminUpdateLLMProvider(svc)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := svc.GetProvider(ctx, "trae-cn")
	if err != nil || stored == nil {
		t.Fatalf("stored err=%v provider=%#v", err, stored)
	}
	if stored.AuthKind != llmpool.ProviderAuthTrae {
		t.Fatalf("auth kind = %q, want the stored account kept", stored.AuthKind)
	}
	if stored.APIKey != "trae-access" || stored.TraeRefreshToken != "trae-refresh" {
		t.Fatalf("credential = %#v", stored)
	}
	if stored.TraeUserID != "tu" || stored.TraeMachineID != "tm" || stored.TraeDeviceID != "td" {
		t.Fatalf("device identity = %#v", stored)
	}
}

