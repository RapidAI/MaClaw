package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestRemoteCodingToolProviderIDMatchingIsCaseAndSpaceInsensitive(t *testing.T) {
	for _, providerID := range []string{
		" Codex:gpt-5.4 ",
		"CLAUDE:sonnet",
		"remote:opencode",
	} {
		if !isRemoteCodingToolUsageProviderID(providerID) {
			t.Fatalf("expected %q to be treated as remote coding tool diagnostic usage", providerID)
		}
	}
	for _, providerID := range []string{"", "provider-a", "maclaw-official", "custom-codex-provider"} {
		if isRemoteCodingToolUsageProviderID(providerID) {
			t.Fatalf("expected %q to remain Hub LLM provider usage", providerID)
		}
	}
}

func TestFilterRemoteCodingToolTokenUsageClonesNormalUsage(t *testing.T) {
	usage := map[string]*corelib.TokenUsageStat{
		"codex:gpt-5.4": {InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280},
		"provider-a":    {InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}

	filtered := filterRemoteCodingToolTokenUsage(usage)
	if _, ok := filtered["codex:gpt-5.4"]; ok {
		t.Fatalf("remote coding tool usage key should be filtered: %#v", filtered)
	}
	filtered["provider-a"].InputTokens = 999
	if usage["provider-a"].InputTokens != 10 {
		t.Fatalf("normal usage was not cloned: %#v", usage["provider-a"])
	}
}

func TestRegistryResponseHidesRemoteCodingToolUsagePollution(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	resp := registryResponse(req, &im.LLMProviderRegistry{
		Providers: []im.LLMProvider{
			{ID: "codex:gpt-5.4", Name: "Codex Proxy"},
			{ID: "provider-a", Name: "Provider A"},
		},
		TokenUsage: map[string]*corelib.TokenUsageStat{
			"codex:gpt-5.4": {InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280, Requests: 1},
			"provider-a":    {InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Requests: 1},
		},
	}, nil, nil)

	if len(resp.Providers) != 2 {
		t.Fatalf("providers = %#v", resp.Providers)
	}
	remoteProvider, ok := resp.Providers[0].(map[string]any)
	if !ok {
		t.Fatalf("provider payload = %#v", resp.Providers[0])
	}
	if remoteProvider["lb_group"] != "x1" || remoteProvider["lb_group_size"] != 2 {
		t.Fatalf("lb annotation = %#v, want shared x1 group", remoteProvider)
	}
	remoteUsage, ok := remoteProvider["usage"].(corelib.TokenUsageStat)
	if !ok {
		t.Fatalf("remote usage payload = %#v", remoteProvider["usage"])
	}
	if remoteUsage.TotalTokens != 0 || remoteUsage.Requests != 0 {
		t.Fatalf("remote coding tool usage should be hidden from registry response: %#v", remoteUsage)
	}
	normalProvider, ok := resp.Providers[1].(map[string]any)
	if !ok {
		t.Fatalf("provider payload = %#v", resp.Providers[1])
	}
	normalUsage, ok := normalProvider["usage"].(corelib.TokenUsageStat)
	if !ok || normalUsage.TotalTokens != 15 || normalUsage.Requests != 1 {
		t.Fatalf("normal provider usage missing from registry response: %#v", normalProvider["usage"])
	}
}

func TestLLMProviderTokenPricingRoundTripThroughAdminRegistry(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID: "provider-priced", Name: "Priced provider", APIURL: "https://example.com/v1", Model: "test-model",
	}}}); err != nil {
		t.Fatalf("seed provider registry: %v", err)
	}
	inputWindowPrice := 1.25
	outputWindowPrice := 4.5
	request := im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID:     "provider-priced",
		Name:   "Priced provider",
		APIURL: "https://example.com/v1",
		Model:  "test-model",
		TokenPricing: llmpool.TokenPricing{
			InputCreditsPer10K:  1,
			OutputCreditsPer10K: 3,
			InputRMBPer10K:      0.02,
			OutputRMBPer10K:     0.06,
			Timezone:            "Asia/Shanghai",
			PriceSchedule: []llmpool.TokenPriceWindow{{
				ID:                  "night",
				Days:                []int{1, 2, 3, 4, 5},
				Start:               "00:00",
				End:                 "08:00",
				InputCreditsPer10K:  &inputWindowPrice,
				OutputCreditsPer10K: &outputWindowPrice,
			}},
		},
	}}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("save status = %d body=%s", rr.Code, rr.Body.String())
	}

	reg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	provider := reg.FindProvider("provider-priced")
	if provider == nil || len(provider.TokenPricing.PriceSchedule) != 1 {
		t.Fatalf("stored token pricing = %#v", provider)
	}
	if provider.TokenPricing.PriceSchedule[0].ID != "night" || provider.TokenPricing.PriceSchedule[0].InputCreditsPer10K == nil || *provider.TokenPricing.PriceSchedule[0].InputCreditsPer10K != inputWindowPrice {
		t.Fatalf("stored time-of-use token price = %#v", provider.TokenPricing.PriceSchedule[0])
	}

	accessCtrl := llmservice.NewTenantLLMAccessControl(nil)
	accessCtrl.UpdateFromHeartbeat(store.DefaultTenantID, &llmservice.TenantAuthorizationStatus{
		TenantID: store.DefaultTenantID, AllowExternalProviders: true,
	})
	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	getRR := httptest.NewRecorder()
	GetLLMProvidersHandler(system, accessCtrl).ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRR.Code, getRR.Body.String())
	}
	var response struct {
		Providers []struct {
			ID           string               `json:"id"`
			TokenPricing llmpool.TokenPricing `json:"token_pricing"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Providers) != 1 || response.Providers[0].ID != "provider-priced" || len(response.Providers[0].TokenPricing.PriceSchedule) != 1 {
		t.Fatalf("response token pricing = %#v", response)
	}
}

func TestUpdateLLMProvidersHandlerRejectsInvalidTokenPricing(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	request := im.LLMProviderRegistry{Providers: []im.LLMProvider{{
		ID: "provider-invalid", Name: "Invalid provider", APIURL: "https://example.com/v1", Model: "test-model",
		TokenPricing: llmpool.TokenPricing{InputCreditsPer10K: 1, OutputCreditsPer10K: 2, PriceSchedule: []llmpool.TokenPriceWindow{{ID: "missing-timezone", Start: "00:00", End: "08:00"}}},
	}}}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/llm/providers", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "INVALID_LLM_TOKEN_PRICING") {
		t.Fatalf("status = %d body=%s, want invalid pricing error", rr.Code, rr.Body.String())
	}
}

func TestUpdateLLMProvidersHandlerDropsRemoteCodingToolUsagePollution(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		Providers: []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
		TokenUsage: map[string]*corelib.TokenUsageStat{
			"codex:gpt-5.4": {InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280, Requests: 1},
			"provider-a":    {InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Requests: 1},
		},
	}); err != nil {
		t.Fatalf("save old provider registry: %v", err)
	}

	payload, err := json.Marshal(im.LLMProviderRegistry{
		Providers: []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	reg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	if _, ok := reg.TokenUsage["codex:gpt-5.4"]; ok {
		t.Fatalf("remote coding tool usage key should be dropped on update: %#v", reg.TokenUsage)
	}
	if stat := reg.TokenUsage["provider-a"]; stat == nil || stat.TotalTokens != 15 || stat.Requests != 1 {
		t.Fatalf("normal provider usage should be preserved: %#v", stat)
	}
}

func TestUpdateLLMProvidersHandlerRejectsNewProviderWithoutComputeGrant(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	payload, err := json.Marshal(im.LLMProviderRegistry{
		Providers: []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, llmservice.NewTenantLLMAccessControl(nil)).ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want 403", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "LLM_EXTERNAL_PROVIDER_NOT_GRANTED") {
		t.Fatalf("body missing grant error: %s", rr.Body.String())
	}
}

func TestUpdateLLMProvidersHandlerAllowsNewProviderWithComputeGrant(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	accessCtrl := llmservice.NewTenantLLMAccessControl(nil)
	accessCtrl.UpdateFromHeartbeat(store.DefaultTenantID, &llmservice.TenantAuthorizationStatus{
		TenantID:               store.DefaultTenantID,
		AllowExternalProviders: true,
	})
	payload, err := json.Marshal(im.LLMProviderRegistry{
		Providers: []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, accessCtrl).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUpdateLLMProvidersHandlerRefreshesStaleComputeGrant(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	client := llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub_default",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{Transport: maclawComputeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/llm/v1/authorization" {
			t.Fatalf("path = %q, want authorization endpoint", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"hub_id":"hub_default","tenant_id":"default","allow_external_providers":true}`)),
			Request:    r,
		}, nil
	})}
	accessCtrl := llmservice.NewTenantLLMAccessControl(client)
	accessCtrl.UpdateFromHeartbeat(store.DefaultTenantID, &llmservice.TenantAuthorizationStatus{
		TenantID:               store.DefaultTenantID,
		AllowExternalProviders: false,
	})
	payload, err := json.Marshal(im.LLMProviderRegistry{
		Providers: []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, accessCtrl).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestUpdateLLMProvidersHandlerStillRejectsAfterRefreshDeniesComputeGrant(t *testing.T) {
	system := newTestLLMServiceSystemSettings()
	client := llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub_default",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{Transport: maclawComputeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"hub_id":"hub_default","tenant_id":"default","allow_external_providers":false}`)),
			Request:    r,
		}, nil
	})}
	accessCtrl := llmservice.NewTenantLLMAccessControl(client)
	accessCtrl.UpdateFromHeartbeat(store.DefaultTenantID, &llmservice.TenantAuthorizationStatus{
		TenantID:               store.DefaultTenantID,
		AllowExternalProviders: false,
	})
	payload, err := json.Marshal(im.LLMProviderRegistry{
		Providers: []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, accessCtrl).ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s, want 403", rr.Code, rr.Body.String())
	}
}

func TestUpdateLLMProvidersHandlerUsesCurrentComputeGrantWhenCapturedNil(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	system := newTestLLMServiceSystemSettings()
	accessCtrl := llmservice.NewTenantLLMAccessControl(nil)
	accessCtrl.UpdateFromHeartbeat(store.DefaultTenantID, &llmservice.TenantAuthorizationStatus{
		TenantID:               store.DefaultTenantID,
		AllowExternalProviders: true,
	})
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: accessCtrl})

	payload, err := json.Marshal(im.LLMProviderRegistry{
		Providers: []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/providers", bytes.NewReader(payload))
	rr := httptest.NewRecorder()
	UpdateLLMProvidersHandler(system, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestGetLLMProvidersHandlerHidesProvidersWithoutComputeGrant(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		CurrentProviderID: "provider-a",
		Providers:         []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	rr := httptest.NewRecorder()
	GetLLMProvidersHandler(system, llmservice.NewTenantLLMAccessControl(nil)).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		CurrentProviderID string   `json:"current_provider_id"`
		Providers         []any    `json:"providers"`
		AvailableModels   []string `json:"available_models"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.CurrentProviderID != "" || len(payload.Providers) != 0 || len(payload.AvailableModels) != 0 {
		t.Fatalf("providers should be hidden without compute grant: %#v", payload)
	}
}

func TestGetLLMProvidersHandlerShowsProvidersWithComputeGrant(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		CurrentProviderID: "provider-a",
		Providers:         []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	accessCtrl := llmservice.NewTenantLLMAccessControl(nil)
	accessCtrl.UpdateFromHeartbeat(store.DefaultTenantID, &llmservice.TenantAuthorizationStatus{
		TenantID:               store.DefaultTenantID,
		AllowExternalProviders: true,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	rr := httptest.NewRecorder()
	GetLLMProvidersHandler(system, accessCtrl).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		CurrentProviderID string   `json:"current_provider_id"`
		Providers         []any    `json:"providers"`
		AvailableModels   []string `json:"available_models"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.CurrentProviderID != "provider-a" || len(payload.Providers) != 1 || len(payload.AvailableModels) != 1 {
		t.Fatalf("providers should be visible with compute grant: %#v", payload)
	}
}

func TestGetLLMProvidersHandlerRefreshesStaleComputeGrant(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		CurrentProviderID: "provider-a",
		Providers:         []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	client := llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: "https://hubcenter.example.com",
		HubID:        "hub_default",
		MachineToken: "secret",
	})
	client.HTTPClient = &http.Client{Transport: maclawComputeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/llm/v1/authorization" {
			t.Fatalf("path = %q, want authorization endpoint", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"hub_id":"hub_default","tenant_id":"default","allow_external_providers":true}`)),
			Request:    r,
		}, nil
	})}
	accessCtrl := llmservice.NewTenantLLMAccessControl(client)
	accessCtrl.UpdateFromHeartbeat(store.DefaultTenantID, &llmservice.TenantAuthorizationStatus{
		TenantID:               store.DefaultTenantID,
		AllowExternalProviders: false,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	rr := httptest.NewRecorder()
	GetLLMProvidersHandler(system, accessCtrl).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		CurrentProviderID string `json:"current_provider_id"`
		Providers         []any  `json:"providers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.CurrentProviderID != "provider-a" || len(payload.Providers) != 1 {
		t.Fatalf("providers should be visible after refreshed compute grant: %#v", payload)
	}
}

func TestGetLLMProvidersHandlerUsesCurrentComputeGrantWhenCapturedNil(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		CurrentProviderID: "provider-a",
		Providers:         []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	accessCtrl := llmservice.NewTenantLLMAccessControl(nil)
	accessCtrl.UpdateFromHeartbeat(store.DefaultTenantID, &llmservice.TenantAuthorizationStatus{
		TenantID:               store.DefaultTenantID,
		AllowExternalProviders: true,
	})
	SetMaClawModule(&llmservice.MaClawModule{AccessCtrl: accessCtrl})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/providers", nil)
	rr := httptest.NewRecorder()
	GetLLMProvidersHandler(system, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		CurrentProviderID string `json:"current_provider_id"`
		Providers         []any  `json:"providers"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.CurrentProviderID != "provider-a" || len(payload.Providers) != 1 {
		t.Fatalf("providers should be visible with current compute grant: %#v", payload)
	}
}

func TestEnqueueLLMUsageIgnoresRemoteCodingToolProviders(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		TokensPerCredit: 10000,
		Grants: []llmservice.Grant{{
			ID:             "grant-1",
			Email:          "remote@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(time.Hour),
			CreatedAt:      now,
			CreditsTotal:   10,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{TokenUsage: map[string]*corelib.TokenUsageStat{}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	globalLLMUsageAccumulator.mu.Lock()
	savedPending := globalLLMUsageAccumulator.pending
	globalLLMUsageAccumulator.pending = map[store.SystemSettingsRepository]*pendingSystemUsage{}
	globalLLMUsageAccumulator.mu.Unlock()
	defer func() {
		globalLLMUsageAccumulator.mu.Lock()
		globalLLMUsageAccumulator.pending = savedPending
		globalLLMUsageAccumulator.mu.Unlock()
	}()

	enqueueLLMUsage(system, "codex:gpt-5.4", corelib.TokenUsageStat{InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280, Requests: 1}, "remote@example.com", []string{"coding-basic"}, []string{"group-a"}, 1)
	globalLLMUsageAccumulator.flush(ctx)

	providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	if stat := providerReg.TokenUsage["codex:gpt-5.4"]; stat != nil && (stat.TotalTokens != 0 || stat.Requests != 0) {
		t.Fatalf("remote coding tool usage leaked into Hub LLM usage: %#v", stat)
	}
	serviceReg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load service registry: %v", err)
	}
	if len(serviceReg.Grants) != 1 || serviceReg.Grants[0].CreditsUsed != 0 {
		t.Fatalf("remote coding tool usage charged credits: %#v", serviceReg.Grants)
	}
}

func TestEnqueueLLMUsageChargesBuiltinProviderServiceGroupCredits(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		TokensPerCredit: 10000,
		Grants: []llmservice.Grant{{
			ID:             "grant-1",
			Email:          "remote@example.com",
			ServiceGroupID: llmservice.MaClawOfficialServiceGroupID,
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(time.Hour),
			CreatedAt:      now,
			CreditsTotal:   10,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{TokenUsage: map[string]*corelib.TokenUsageStat{}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	globalLLMUsageAccumulator.mu.Lock()
	savedPending := globalLLMUsageAccumulator.pending
	globalLLMUsageAccumulator.pending = map[store.SystemSettingsRepository]*pendingSystemUsage{}
	globalLLMUsageAccumulator.mu.Unlock()
	defer func() {
		globalLLMUsageAccumulator.mu.Lock()
		globalLLMUsageAccumulator.pending = savedPending
		globalLLMUsageAccumulator.mu.Unlock()
	}()

	enqueueLLMUsage(system, llmservice.MaClawOfficialProviderID, corelib.TokenUsageStat{InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280, Requests: 1}, "remote@example.com", []string{llmservice.MaClawOfficialServiceGroupID}, []string{"group-a"}, 1)
	globalLLMUsageAccumulator.flush(ctx)

	providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	if stat := providerReg.TokenUsage[llmservice.MaClawOfficialProviderID]; stat == nil || stat.TotalTokens != 1280 || stat.Requests != 1 {
		t.Fatalf("builtin provider usage not recorded: %#v", stat)
	}
	serviceReg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load service registry: %v", err)
	}
	if len(serviceReg.Grants) != 1 || serviceReg.Grants[0].CreditsUsed != 1 {
		t.Fatalf("builtin provider usage credits = %#v, want 1 credit charged", serviceReg.Grants)
	}
}

func TestFlushCreditChargesRecordsReferralRewardUsageOnce(t *testing.T) {
	services := newAdminRouterTestContext(t)
	ctx := context.Background()
	repo := services.store.UserReferrals
	system := userReferralMetricSystemSettings{SystemSettingsRepository: services.store.System, repo: repo}
	invalidateLLMRuntimeCaches(system)
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{Grants: []llmservice.Grant{{
		ID: "used-referral-grant", UserID: "used-invitee", Email: "used-invitee@example.com", ServiceGroupID: "group-a", Source: "user_referral",
		StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-time.Hour), CreditsTotal: 10,
	}}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}
	for range 2 {
		if err := flushCreditCharges(ctx, system, map[string]*pendingCreditCharge{"charge": {userID: "used-invitee", email: "used-invitee@example.com", serviceGroupIDs: []string{"group-a"}, credits: 1}}); err != nil {
			t.Fatalf("flush charges: %v", err)
		}
	}
	items, err := repo.ListDailyMetrics(ctx, store.DefaultTenantID, now, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Event == userReferralMetricRewardUsed {
			if item.Count != 1 {
				t.Fatalf("usage metric count=%d, want 1", item.Count)
			}
			return
		}
	}
	t.Fatalf("missing reward usage metric: %#v", items)
}

func TestUsageChargeRetryKeepsDistinctServiceGroupRoutes(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{Grants: []llmservice.Grant{
		{ID: "grant-a", Email: "user@example.com", ServiceGroupID: "group-a", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 10},
		{ID: "grant-b", Email: "user@example.com", ServiceGroupID: "group-b", Source: "card", StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), CreditsTotal: 10},
	}}); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	chargeA := &pendingCreditCharge{email: "user@example.com", serviceGroupIDs: []string{"group-a"}, credits: 2}
	chargeB := &pendingCreditCharge{email: "user@example.com", serviceGroupIDs: []string{"group-b"}, credits: 3}
	if creditChargeKey(chargeA) == creditChargeKey(chargeB) {
		t.Fatal("distinct service-group routes must use distinct retry keys")
	}
	if err := flushCreditCharges(ctx, system, map[string]*pendingCreditCharge{
		creditChargeKey(chargeA): chargeA,
		creditChargeKey(chargeB): chargeB,
	}); err != nil {
		t.Fatalf("flush charges: %v", err)
	}
	saved, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if saved.Grants[0].CreditsUsed != 2 || saved.Grants[1].CreditsUsed != 3 {
		t.Fatalf("route charges were cross-applied: %#v", saved.Grants)
	}
}

func TestFlushProviderUsageSkipsPersistedRemoteCodingToolKeys(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{TokenUsage: map[string]*corelib.TokenUsageStat{}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	err := flushProviderUsage(ctx, system, map[string]corelib.TokenUsageStat{
		"codex:gpt-5.4": {InputTokens: 1200, OutputTokens: 80, TotalTokens: 1280, Requests: 1},
		"provider-a":    {InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Requests: 1},
	})
	if err != nil {
		t.Fatalf("flush provider usage: %v", err)
	}

	providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	if _, ok := providerReg.TokenUsage["codex:gpt-5.4"]; ok {
		t.Fatalf("remote coding tool usage key should not be persisted: %#v", providerReg.TokenUsage)
	}
	if stat := providerReg.TokenUsage["provider-a"]; stat == nil || stat.TotalTokens != 15 || stat.Requests != 1 {
		t.Fatalf("normal provider usage was not persisted: %#v", stat)
	}
}

func TestEnqueueLLMUsageWritesProviderScopedReport(t *testing.T) {
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		Providers:  []im.LLMProvider{{ID: "provider-a", Name: "Provider A", APIURL: "https://example.com/v1", Model: "test-model"}},
		TokenUsage: map[string]*corelib.TokenUsageStat{},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	globalLLMUsageAccumulator.mu.Lock()
	savedPending := globalLLMUsageAccumulator.pending
	globalLLMUsageAccumulator.pending = map[store.SystemSettingsRepository]*pendingSystemUsage{}
	globalLLMUsageAccumulator.mu.Unlock()
	defer func() {
		globalLLMUsageAccumulator.mu.Lock()
		globalLLMUsageAccumulator.pending = savedPending
		globalLLMUsageAccumulator.mu.Unlock()
	}()

	enqueueLLMUsage(system, "provider-a", corelib.TokenUsageStat{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, Requests: 1}, "user@example.com", nil, nil, 0)
	globalLLMUsageAccumulator.flush(ctx)

	rep, err := loadLLMUsageReports(ctx, system)
	if err != nil {
		t.Fatalf("load usage reports: %v", err)
	}
	monthKey := ""
	for dayKey := range rep.Days {
		if len(dayKey) >= len("2006-01") {
			monthKey = dayKey[:len("2006-01")]
			break
		}
	}
	if monthKey == "" {
		t.Fatalf("usage report day was not persisted: %#v", rep.Days)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/usage-report?scope=provider&period=monthly&month="+monthKey, nil)
	rr := httptest.NewRecorder()
	GetLLMUsageReportHandler(system, nil).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp llmUsageReportResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Scope != "provider" || resp.Summary.TotalTokens != 15 || len(resp.Rows) != 1 {
		t.Fatalf("provider scoped report not written: summary=%+v rows=%#v", resp.Summary, resp.Rows)
	}
	if resp.Rows[0].ID != "provider-a" || resp.Rows[0].Name != "Provider A" || resp.Rows[0].TotalTokens != 15 {
		t.Fatalf("provider report row = %#v", resp.Rows[0])
	}
}
