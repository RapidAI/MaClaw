package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestForwardAuthorizedModelRequestOrdersProvidersByProviderScopedParams(t *testing.T) {
	var docHits atomic.Int32
	var toolHits atomic.Int32

	docServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		docHits.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "doc-response",
			"object": "chat.completion",
			"model":  "doc-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "doc"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
		})
	}))
	defer docServer.Close()

	toolServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		toolHits.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "tool-response",
			"object": "chat.completion",
			"model":  "tool-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "tool"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
		})
	}))
	defer toolServer.Close()

	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "provider-doc", APIURL: docServer.URL, Model: "doc-model"},
		{ID: "provider-tools", APIURL: toolServer.URL, Model: "tool-model"},
	}}
	model := &llmservice.AuthorizedModel{
		Name:             "auto",
		ProviderIDs:      []string{"provider-doc", "provider-tools"},
		CapabilityTags:   []string{"document", "tools"},
		Priority:         10,
		ResolutionTier:   1,
		CreditMultiplier: 1,
		ProviderCapabilityTags: map[string][]string{
			"provider-doc":   {"document"},
			"provider-tools": {"tools"},
		},
		ProviderPriorities: map[string]int{
			"provider-doc":   20,
			"provider-tools": 60,
		},
		ProviderResolutionTiers: map[string]int{
			"provider-doc":   2,
			"provider-tools": 1,
		},
		ProviderCreditMultipliers: map[string]float64{
			"provider-doc":   1.2,
			"provider-tools": 1,
		},
		ProviderServiceGroups: map[string][]string{
			"provider-doc":   {"group-doc"},
			"provider-tools": {"group-tools"},
		},
	}
	body := map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "Use tools to search and fetch the answer."}},
		"tools":    []any{map[string]any{"type": "function"}},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)

	respBody, statusCode, usedProviderID, chargedServiceGroupIDs, err := forwardAuthorizedModelRequest(request, reg, model, body, "auto")
	if err != nil {
		t.Fatalf("forwardAuthorizedModelRequest() error = %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("statusCode = %d, want 200", statusCode)
	}
	if usedProviderID != "provider-tools" {
		t.Fatalf("usedProviderID = %q, want provider-tools", usedProviderID)
	}
	if docHits.Load() != 0 {
		t.Fatalf("doc provider should not be tried first, hits = %d", docHits.Load())
	}
	if toolHits.Load() != 1 {
		t.Fatalf("tool provider hits = %d, want 1", toolHits.Load())
	}
	if len(chargedServiceGroupIDs) != 1 || chargedServiceGroupIDs[0] != "group-tools" {
		t.Fatalf("chargedServiceGroupIDs = %#v", chargedServiceGroupIDs)
	}
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["model"] != "auto" {
		t.Fatalf("response model = %#v, want auto", payload["model"])
	}
}

func TestLLMProviderUpstreamHTTPClientUsesConfiguredTimeout(t *testing.T) {
	client := llmProviderUpstreamHTTPClient(corelib.MaclawLLMConfig{TimeoutSec: 7})
	if client == nil {
		t.Fatal("client is nil")
	}
	if client.Timeout != time.Duration(corelib.MinAgentTimeoutSec)*time.Second {
		t.Fatalf("client.Timeout = %s, want %ds", client.Timeout, corelib.MinAgentTimeoutSec)
	}

	defaultClient := llmProviderUpstreamHTTPClient(corelib.MaclawLLMConfig{})
	if defaultClient.Timeout != time.Duration(corelib.DefaultLLMTimeoutSec)*time.Second {
		t.Fatalf("default client.Timeout = %s, want %ds", defaultClient.Timeout, corelib.DefaultLLMTimeoutSec)
	}

	streamClient := llmProviderUpstreamStreamHTTPClient(corelib.MaclawLLMConfig{TimeoutSec: 7})
	if streamClient == nil {
		t.Fatal("stream client is nil")
	}
	if streamClient.Timeout != 0 {
		t.Fatalf("stream client Timeout = %s, want 0 to avoid cutting off long streams", streamClient.Timeout)
	}
}
func TestParseUsageStatsIncludesPromptCache(t *testing.T) {
	payload := []byte(`{"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":48},"cache_creation_input_tokens":12}}`)
	usage := parseUsageStats(payload)
	if usage.InputTokens != 120 || usage.OutputTokens != 30 || usage.TotalTokens != 150 {
		t.Fatalf("unexpected basic usage: %#v", usage)
	}
	if usage.CachedInputTokens != 48 {
		t.Fatalf("CachedInputTokens = %d, want 48", usage.CachedInputTokens)
	}
	if usage.CacheWriteTokens != 12 {
		t.Fatalf("CacheWriteTokens = %d, want 12", usage.CacheWriteTokens)
	}
	if usage.Requests != 1 {
		t.Fatalf("Requests = %d, want 1", usage.Requests)
	}
	if usage.CachedRequests != 1 {
		t.Fatalf("CachedRequests = %d, want 1", usage.CachedRequests)
	}
}

func TestApplyProviderUsageCostUsesProviderPricing(t *testing.T) {
	usage := corelib.TokenUsageStat{InputTokens: 1_000_000, OutputTokens: 500_000, TotalTokens: 1_500_000}
	provider := &im.LLMProvider{InputPricePerMTokensRMB: 3, OutputPricePerMTokensRMB: 6}
	priced := applyProviderUsageCost(usage, provider)
	if priced.InputCostRMB != 3 || priced.OutputCostRMB != 3 || priced.TotalCostRMB != 6 {
		t.Fatalf("cost = input %.4f output %.4f total %.4f, want 3/3/6", priced.InputCostRMB, priced.OutputCostRMB, priced.TotalCostRMB)
	}
	if priced.InputPricePerMTokensRMB != 3 || priced.OutputPricePerMTokensRMB != 6 {
		t.Fatalf("prices = %.4f/%.4f, want 3/6", priced.InputPricePerMTokensRMB, priced.OutputPricePerMTokensRMB)
	}
}

func TestForwardAuthorizedModelRequestUsesLocalCacheWhenAvailable(t *testing.T) {
	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "upstream"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer server.Close()

	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a"}}
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "repeat this"}}}
	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1 << 20})
	usage := corelib.TokenUsageStat{InputTokens: 12, OutputTokens: 5, TotalTokens: 17}
	resp := []byte(`{"id":"cached","model":"auto","choices":[{"index":0,"message":{"role":"assistant","content":"cached"},"finish_reason":"stop"}]}`)
	if err := putCachedAuthorizedModelResponse(context.Background(), cache, model, body, "auto", resp, http.StatusOK, "provider-a", []string{"group-a"}, usage, defaultHubLLMPromptCacheConfig()); err != nil {
		t.Fatalf("put cache response: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	respBody, statusCode, providerID, serviceGroupIDs, cachedUsage, cacheHit, err := forwardAuthorizedModelRequestWithCache(req, reg, model, body, "auto", cache, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("forwardAuthorizedModelRequestWithCache() error = %v", err)
	}
	if !cacheHit {
		t.Fatalf("expected local cache hit")
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("upstream should not be called on local cache hit, hits = %d", upstreamHits.Load())
	}
	if statusCode != http.StatusOK || providerID != "provider-a" {
		t.Fatalf("unexpected cache result: status=%d provider=%q", statusCode, providerID)
	}
	if len(serviceGroupIDs) != 1 || serviceGroupIDs[0] != "group-a" {
		t.Fatalf("serviceGroupIDs = %#v", serviceGroupIDs)
	}
	if cachedUsage.TotalTokens != usage.TotalTokens {
		t.Fatalf("cachedUsage = %#v", cachedUsage)
	}
	var gotPayload map[string]any
	var wantPayload map[string]any
	if err := json.Unmarshal(respBody, &gotPayload); err != nil {
		t.Fatalf("unmarshal cached response: %v", err)
	}
	if err := json.Unmarshal(resp, &wantPayload); err != nil {
		t.Fatalf("unmarshal expected response: %v", err)
	}
	if gotPayload["id"] != wantPayload["id"] || gotPayload["model"] != wantPayload["model"] {
		t.Fatalf("respBody = %s", string(respBody))
	}
	status, err := cache.Status(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("cache status: %v", err)
	}
	if status.MemoryHits != 1 {
		t.Fatalf("memory hits = %d, want 1", status.MemoryHits)
	}
}

func TestLLMV1ChatCompletionsHandlerRejectsGrantRequiredServiceWithoutCredits(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "grant-required@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "grant-required@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "upstream", "model": "auto"})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "LLM_SERVICE_CREDITS_REQUIRED") {
		t.Fatalf("expected credits required error, body = %s", rr.Body.String())
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("expected upstream to be blocked, hits = %d", upstreamHits.Load())
	}
}

func TestLLMV1ChatCompletionsHandlerReportsPeriodLimitRetry(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "period-limited@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "period-limited@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-1",
			Email:          "period-limited@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    1,
			PeriodLimits:   llmservice.CreditPeriodLimits{Daily: 10},
			PeriodUsage:    llmservice.CreditPeriodUsage{Daily: llmservice.GrantUsageWindow{WindowStart: dayStart, CreditsUsed: 10}},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "upstream", "model": "auto"})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "LLM_SERVICE_PERIOD_LIMITED" {
		t.Fatalf("expected period limit code, body = %s", rr.Body.String())
	}
	if resp["retry_after_at"] == "" || resp["retry_after_seconds"] == nil || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("expected retry metadata, header=%q body=%s", rr.Header().Get("Retry-After"), rr.Body.String())
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("expected upstream to be blocked, hits = %d", upstreamHits.Load())
	}
}

func TestLLMV1ChatCompletionsHandlerReportsExpiredGrant(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "expired-grant@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "expired-grant@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-1",
			Email:          "expired-grant@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-48 * time.Hour),
			ExpiresAt:      now.Add(-time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    10,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "upstream", "model": "auto"})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "LLM_SERVICE_GRANT_EXPIRED" {
		t.Fatalf("expected expired grant code, body = %s", rr.Body.String())
	}
	if !strings.Contains(strings.ToLower(fmt.Sprint(resp["message"])), "expired") {
		t.Fatalf("expected expired message, body = %s", rr.Body.String())
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("expected upstream to be blocked, hits = %d", upstreamHits.Load())
	}
}

func TestLLMV1ChatCompletionsHandlerReportsPeriodLimitRetryForUnlimitedGrant(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "unlimited-period-limited@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "unlimited-period-limited@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-1",
			Email:          "unlimited-period-limited@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "admin",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   0,
			PeriodLimits:   llmservice.CreditPeriodLimits{Daily: 10},
			PeriodUsage:    llmservice.CreditPeriodUsage{Daily: llmservice.GrantUsageWindow{WindowStart: dayStart, CreditsUsed: 10}},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "upstream", "model": "auto"})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "LLM_SERVICE_PERIOD_LIMITED" {
		t.Fatalf("expected period limit code, body = %s", rr.Body.String())
	}
	if resp["retry_after_at"] == "" || resp["retry_after_seconds"] == nil || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("expected retry metadata, header=%q body=%s", rr.Header().Get("Retry-After"), rr.Body.String())
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("expected upstream to be blocked, hits = %d", upstreamHits.Load())
	}
}

func TestLLMV1ChatCompletionsHandlerReportsPeriodLimitForExplicitModelWhenOtherRouteAvailable(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "explicit-limited@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "limited-group",
			Name:         "Limited Group",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models: []llmservice.ModelServiceModel{{
				Name:        "limited",
				ProviderIDs: []string{"provider-limited"},
			}},
		}, {
			ID:           "active-group",
			Name:         "Active Group",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models: []llmservice.ModelServiceModel{{
				Name:        "active",
				ProviderIDs: []string{"provider-active"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "explicit-limited@example.com",
			ServiceGroupIDs: []string{"limited-group", "active-group"},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-limited",
			Email:          "explicit-limited@example.com",
			ServiceGroupID: "limited-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    1,
			PeriodLimits:   llmservice.CreditPeriodLimits{Daily: 10},
			PeriodUsage:    llmservice.CreditPeriodUsage{Daily: llmservice.GrantUsageWindow{WindowStart: dayStart, CreditsUsed: 10}},
		}, {
			ID:             "grant-active",
			Email:          "explicit-limited@example.com",
			ServiceGroupID: "active-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    1,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var limitedHits atomic.Int32
	var activeHits atomic.Int32
	limitedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limitedHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "limited", "model": "limited"})
	}))
	defer limitedServer.Close()
	activeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "active", "model": "active"})
	}))
	defer activeServer.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "provider-limited", APIURL: limitedServer.URL, Model: "limited-upstream"},
		{ID: "provider-active", APIURL: activeServer.URL, Model: "active-upstream"},
	}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "limited", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "LLM_SERVICE_PERIOD_LIMITED" {
		t.Fatalf("expected explicit limited model to report period limit, body = %s", rr.Body.String())
	}
	if resp["retry_after_at"] == "" || resp["retry_after_seconds"] == nil || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("expected retry metadata, header=%q body=%s", rr.Header().Get("Retry-After"), rr.Body.String())
	}
	if limitedHits.Load() != 0 || activeHits.Load() != 0 {
		t.Fatalf("expected no upstream calls for explicitly limited model, limited=%d active=%d", limitedHits.Load(), activeHits.Load())
	}
}

func TestLLMV1ChatCompletionsHandlerReportsQueuedGrantRetry(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "queued-grant@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	startsAt := now.Add(2 * time.Hour).Truncate(time.Second)
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "queued-grant@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-1",
			Email:          "queued-grant@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreditsTotal:   100,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "upstream", "model": "auto"})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "LLM_SERVICE_GRANT_QUEUED" {
		t.Fatalf("expected queued grant code, body = %s", rr.Body.String())
	}
	if resp["retry_after_at"] == "" || resp["retry_after_seconds"] == nil || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("expected retry metadata, header=%q body=%s", rr.Header().Get("Retry-After"), rr.Body.String())
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("expected upstream to be blocked, hits = %d", upstreamHits.Load())
	}
}

func TestLLMV1ChatCompletionsHandlerAllowsQueuedGrantWhenCurrentGrantExhausted(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "exhausted-then-queued@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	startsAt := now.Add(2 * time.Hour).Truncate(time.Second)
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "exhausted-then-queued@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-old",
			Email:          "exhausted-then-queued@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-24 * time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "grant-next",
			Email:          "exhausted-then-queued@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreditsTotal:   100,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 12000, "completion_tokens": 8000, "total_tokens": 20000},
		})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] != "upstream" {
		t.Fatalf("expected upstream response, body = %s", rr.Body.String())
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected upstream to be used once, hits = %d", upstreamHits.Load())
	}
	serviceReg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load service registry: %v", err)
	}
	if len(serviceReg.Grants) != 2 {
		t.Fatalf("expected 2 grants, got %#v", serviceReg.Grants)
	}
	if !serviceReg.Grants[1].StartsAt.Before(startsAt) || !serviceReg.Grants[1].ExpiresAt.Before(startsAt.Add(24*time.Hour)) {
		t.Fatalf("expected queued grant to shift earlier, got %#v", serviceReg.Grants[1])
	}
	if serviceReg.Grants[1].CreditsUsed != 2 {
		t.Fatalf("expected shifted grant to be charged 2 credits, got %#v", serviceReg.Grants[1])
	}
}

func TestLLMV1ChatCompletionsHandlerPrioritizesQueuedDenialForAuto(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "auto-queued@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	startsAt := now.Add(2 * time.Hour).Truncate(time.Second)
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "exhausted-group",
			Name:         "Exhausted",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models:       []llmservice.ModelServiceModel{{Name: "exhausted", ProviderIDs: []string{"provider-exhausted"}}},
		}, {
			ID:           "queued-group",
			Name:         "Queued",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models:       []llmservice.ModelServiceModel{{Name: "queued", ProviderIDs: []string{"provider-queued"}}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "auto-queued@example.com",
			ServiceGroupIDs: []string{"exhausted-group", "queued-group"},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-exhausted",
			Email:          "auto-queued@example.com",
			ServiceGroupID: "exhausted-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    100,
		}, {
			ID:             "grant-queued",
			Email:          "auto-queued@example.com",
			ServiceGroupID: "queued-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreditsTotal:   100,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream should not be called")
	}))
	defer server.Close()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "provider-exhausted", APIURL: server.URL, Model: "exhausted-upstream"},
		{ID: "provider-queued", APIURL: server.URL, Model: "queued-upstream"},
	}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "LLM_SERVICE_GRANT_QUEUED" {
		t.Fatalf("expected queued denial to win over exhausted, body = %s", rr.Body.String())
	}
	if resp["retry_after_at"] == "" || resp["retry_after_seconds"] == nil || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("expected retry metadata, header=%q body=%s", rr.Header().Get("Retry-After"), rr.Body.String())
	}
}

func TestLLMV1ChatCompletionsHandlerPrioritizesPeriodLimitDenialForAuto(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "auto-limited@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	startsAt := now.Add(2 * time.Hour).Truncate(time.Second)
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "queued-group",
			Name:         "Queued",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models:       []llmservice.ModelServiceModel{{Name: "queued", ProviderIDs: []string{"provider-queued"}}},
		}, {
			ID:           "limited-group",
			Name:         "Limited",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
			Models:       []llmservice.ModelServiceModel{{Name: "limited", ProviderIDs: []string{"provider-limited"}}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "auto-limited@example.com",
			ServiceGroupIDs: []string{"queued-group", "limited-group"},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-queued",
			Email:          "auto-limited@example.com",
			ServiceGroupID: "queued-group",
			Source:         "card",
			StartsAt:       startsAt,
			ExpiresAt:      startsAt.Add(24 * time.Hour),
			CreditsTotal:   100,
		}, {
			ID:             "grant-limited",
			Email:          "auto-limited@example.com",
			ServiceGroupID: "limited-group",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreditsTotal:   100,
			CreditsUsed:    1,
			PeriodLimits:   llmservice.CreditPeriodLimits{Daily: 10},
			PeriodUsage:    llmservice.CreditPeriodUsage{Daily: llmservice.GrantUsageWindow{WindowStart: dayStart, CreditsUsed: 10}},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream should not be called")
	}))
	defer server.Close()
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{ID: "provider-queued", APIURL: server.URL, Model: "queued-upstream"},
		{ID: "provider-limited", APIURL: server.URL, Model: "limited-upstream"},
	}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["code"] != "LLM_SERVICE_PERIOD_LIMITED" {
		t.Fatalf("expected period limit denial to win over queued, body = %s", rr.Body.String())
	}
	if resp["retry_after_at"] == "" || resp["retry_after_seconds"] == nil || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("expected retry metadata, header=%q body=%s", rr.Header().Get("Retry-After"), rr.Body.String())
	}
}

func TestLLMV1ChatCompletionsHandlerAllowsFreeServiceWithoutCredits(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "free-binding@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "free-binding@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "free-ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected upstream to be called once, hits = %d", upstreamHits.Load())
	}
}

func TestLLMV1ChatCompletionsHandlerStreamsOpenAICompatResponse(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "stream-binding@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "stream-binding@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["stream"] != true {
			t.Fatalf("upstream stream = %#v, want true", upstreamBody["stream"])
		}
		if upstreamBody["model"] != "test-model" {
			t.Fatalf("upstream model = %#v, want test-model", upstreamBody["model"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": keepalive\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hel\"}}]}\n\n"))
		_, _ = w.Write([]byte("event: message\nid: chunk-2\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chunk-2\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"total_tokens\":15}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	globalLLMUsageAccumulator.mu.Lock()
	savedPending := globalLLMUsageAccumulator.pending
	globalLLMUsageAccumulator.pending = map[store.SystemSettingsRepository]*pendingSystemUsage{}
	globalLLMUsageAccumulator.mu.Unlock()
	defer func() {
		globalLLMUsageAccumulator.mu.Lock()
		globalLLMUsageAccumulator.pending = savedPending
		globalLLMUsageAccumulator.mu.Unlock()
	}()

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}, "stream": true, "stream_options": map[string]any{"include_usage": true}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", rr.Header().Get("Content-Type"))
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected upstream to be called once, hits = %d", upstreamHits.Load())
	}
	if !strings.Contains(rr.Body.String(), `"model":"auto"`) || !strings.Contains(rr.Body.String(), "data: [DONE]") || !strings.Contains(rr.Body.String(), "event: message") || !strings.Contains(rr.Body.String(), ": keepalive") {
		t.Fatalf("unexpected stream body: %s", rr.Body.String())
	}

	globalLLMUsageAccumulator.flush(ctx)
	providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	stat := providerReg.TokenUsage["provider-a"]
	if stat == nil || stat.InputTokens != 10 || stat.OutputTokens != 5 || stat.TotalTokens != 15 || stat.Requests != 1 {
		t.Fatalf("unexpected stream usage: %#v", stat)
	}
}

func TestLLMV1ChatCompletionsHandlerSynthesizesStreamWhenUpstreamReturnsJSON(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "json-stream-binding@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "json-stream-binding@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "json-upstream",
			"object": "chat.completion",
			"model":  "test-model",
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "json fallback",
					"tool_calls": []any{map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "lookup",
							"arguments": `{"q":"hello"}`,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
			"usage": map[string]any{"prompt_tokens": 8, "completion_tokens": 4, "total_tokens": 12},
		})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	globalLLMUsageAccumulator.mu.Lock()
	savedPending := globalLLMUsageAccumulator.pending
	globalLLMUsageAccumulator.pending = map[store.SystemSettingsRepository]*pendingSystemUsage{}
	globalLLMUsageAccumulator.mu.Unlock()
	defer func() {
		globalLLMUsageAccumulator.mu.Lock()
		globalLLMUsageAccumulator.pending = savedPending
		globalLLMUsageAccumulator.mu.Unlock()
	}()

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}, "stream": true})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", rr.Header().Get("Content-Type"))
	}
	if !strings.Contains(rr.Body.String(), "json fallback") || !strings.Contains(rr.Body.String(), "data: [DONE]") || !strings.Contains(rr.Body.String(), `"model":"auto"`) || !strings.Contains(rr.Body.String(), `"index":0`) || !strings.Contains(rr.Body.String(), `"finish_reason":"tool_calls"`) {
		t.Fatalf("unexpected synthesized stream body: %s", rr.Body.String())
	}

	globalLLMUsageAccumulator.flush(ctx)
	providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	stat := providerReg.TokenUsage["provider-a"]
	if stat == nil || stat.InputTokens != 8 || stat.OutputTokens != 4 || stat.TotalTokens != 12 || stat.Requests != 1 {
		t.Fatalf("unexpected synthesized stream usage: %#v", stat)
	}
}

func TestLLMV1ChatCompletionsHandlerUsesLocalCacheWithoutEnqueueingUsage(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "cached-handler@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		TokensPerCredit: 10000,
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-1",
			Email:          "cached-handler@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreatedAt:      now,
			CreditsTotal:   10,
			CreditsUsed:    2,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{"id": "upstream", "model": "auto"})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1 << 20})
	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "repeat this"}}}
	resp := []byte(`{"id":"cached","model":"auto","choices":[{"index":0,"message":{"role":"assistant","content":"cached"},"finish_reason":"stop"}]}`)
	usage := corelib.TokenUsageStat{InputTokens: 20, OutputTokens: 10, TotalTokens: 30, Requests: 1}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a"}, ProviderServiceGroups: map[string][]string{"provider-a": {"coding-basic"}}}
	if err := putCachedAuthorizedModelResponse(ctx, cache, model, body, "auto", resp, http.StatusOK, "provider-a", []string{"coding-basic"}, usage, defaultHubLLMPromptCacheConfig()); err != nil {
		t.Fatalf("put cache response: %v", err)
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

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil, cache).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-MaClaw-Local-Cache") != "hit" {
		t.Fatalf("expected local cache hit header, got %q", rr.Header().Get("X-MaClaw-Local-Cache"))
	}
	if rr.Header().Get("X-MaClaw-Upstream-Provider") != "provider-a" {
		t.Fatalf("unexpected upstream provider header: %q", rr.Header().Get("X-MaClaw-Upstream-Provider"))
	}
	if rr.Header().Get("X-MaClaw-Authorized-Model") != "auto" {
		t.Fatalf("unexpected authorized model header: %q", rr.Header().Get("X-MaClaw-Authorized-Model"))
	}
	if upstreamHits.Load() != 0 {
		t.Fatalf("upstream should not be called on local cache hit, hits = %d", upstreamHits.Load())
	}

	globalLLMUsageAccumulator.flush(ctx)

	providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	if stat := providerReg.TokenUsage["provider-a"]; stat != nil && (stat.TotalTokens != 0 || stat.Requests != 0) {
		t.Fatalf("expected no provider usage to be recorded, got %#v", stat)
	}

	serviceReg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load service registry: %v", err)
	}
	if len(serviceReg.Grants) != 1 || serviceReg.Grants[0].CreditsUsed != 2 {
		t.Fatalf("expected credits to remain unchanged, got %#v", serviceReg.Grants)
	}
}

func TestLLMV1ChatCompletionsHandlerMissEnqueuesUsageAndCredits(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "cache-miss@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	now := time.Now().UTC()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		TokensPerCredit: 10000,
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:   "coding-basic",
			Name: "Coding Basic",
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-a"},
				ProviderConfigs: []llmservice.ModelServiceProviderConfig{{
					ProviderID:       "provider-a",
					CreditMultiplier: 2,
				}},
			}},
		}},
		Grants: []llmservice.Grant{{
			ID:             "grant-1",
			Email:          "cache-miss@example.com",
			ServiceGroupID: "coding-basic",
			Source:         "card",
			StartsAt:       now.Add(-time.Hour),
			ExpiresAt:      now.Add(24 * time.Hour),
			CreatedAt:      now,
			CreditsTotal:   10,
			CreditsUsed:    1,
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "fresh"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 12000, "completion_tokens": 8000, "total_tokens": 20000},
		})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model", InputPricePerMTokensRMB: 100, OutputPricePerMTokensRMB: 150}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1 << 20})
	globalLLMUsageAccumulator.mu.Lock()
	savedPending := globalLLMUsageAccumulator.pending
	globalLLMUsageAccumulator.pending = map[store.SystemSettingsRepository]*pendingSystemUsage{}
	globalLLMUsageAccumulator.mu.Unlock()
	defer func() {
		globalLLMUsageAccumulator.mu.Lock()
		globalLLMUsageAccumulator.pending = savedPending
		globalLLMUsageAccumulator.mu.Unlock()
	}()

	body := map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "miss path"}}}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil, cache).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-MaClaw-Local-Cache") != "" {
		t.Fatalf("did not expect local cache hit header, got %q", rr.Header().Get("X-MaClaw-Local-Cache"))
	}
	if rr.Header().Get("X-MaClaw-Upstream-Provider") != "provider-a" {
		t.Fatalf("unexpected upstream provider header: %q", rr.Header().Get("X-MaClaw-Upstream-Provider"))
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected upstream to be called once, hits = %d", upstreamHits.Load())
	}

	serviceReg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load service registry before usage flush: %v", err)
	}
	if len(serviceReg.Grants) != 1 || serviceReg.Grants[0].CreditsUsed != 5 {
		t.Fatalf("expected credits to be charged immediately, got %#v", serviceReg.Grants)
	}

	globalLLMUsageAccumulator.flush(ctx)

	providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load provider registry: %v", err)
	}
	stat := providerReg.TokenUsage["provider-a"]
	if stat == nil {
		t.Fatal("expected provider usage to be recorded")
	}
	if stat.InputTokens != 12000 || stat.OutputTokens != 8000 || stat.TotalTokens != 20000 || stat.Requests != 1 {
		t.Fatalf("unexpected provider usage: %#v", stat)
	}
	if stat.InputCostRMB != 1.2 || stat.OutputCostRMB != 1.2 || stat.TotalCostRMB != 2.4 {
		t.Fatalf("unexpected provider display cost: %#v", stat)
	}

	serviceReg, err = llmservice.LoadRegistry(ctx, system)
	if err != nil {
		t.Fatalf("load service registry: %v", err)
	}
	if len(serviceReg.Grants) != 1 {
		t.Fatalf("expected single grant, got %#v", serviceReg.Grants)
	}
	if serviceReg.Grants[0].CreditsUsed != 5 {
		t.Fatalf("expected credits used to increase to 5, got %#v", serviceReg.Grants[0])
	}
}
func TestLLMV1ChatCompletionsHandlerReturnsTooManyRequestsWhenProviderQueueFull(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "queue-full@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	providerID := "provider-queue-full"
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{providerID},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "queue-full@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	started := make(chan struct{}, 2)
	releaseUpstream := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-releaseUpstream
		writeJSON(w, http.StatusOK, map[string]any{"id": "upstream", "model": "auto"})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: providerID, APIURL: server.URL, Model: "test-model", MaxConcurrency: 1, MaxQueueWaiters: 1}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	makeReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")
		return req
	}

	rr1 := httptest.NewRecorder()
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr1, makeReq())
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first upstream request did not start")
	}

	rr2 := httptest.NewRecorder()
	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr2, makeReq())
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snap := globalProviderConcurrency.snapshot(providerID, 1, 1, 0)
		if snap.QueueWaiters == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snap := globalProviderConcurrency.snapshot(providerID, 1, 1, 0); snap.QueueWaiters != 1 {
		t.Fatalf("expected one queued waiter, got %+v", snap)
	}

	rr3 := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr3, makeReq())
	if rr3.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", rr3.Code, rr3.Body.String())
	}
	if !strings.Contains(rr3.Body.String(), "LLM_PROVIDER_QUEUE_FULL") {
		t.Fatalf("expected queue full error, body = %s", rr3.Body.String())
	}

	close(releaseUpstream)
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not finish")
	}
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("second request did not finish")
	}
}

func TestLLMV1ChatCompletionsHandlerReturnsServiceUnavailableWhenProviderQueueTimesOut(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "queue-timeout@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	providerID := "provider-queue-timeout"
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{providerID},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "queue-timeout@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	started := make(chan struct{}, 1)
	releaseUpstream := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-releaseUpstream
		writeJSON(w, http.StatusOK, map[string]any{"id": "upstream", "model": "auto"})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: providerID, APIURL: server.URL, Model: "test-model", MaxConcurrency: 1, MaxQueueWaiters: 2, QueueTimeoutMS: 50}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	makeReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")
		return req
	}

	rr1 := httptest.NewRecorder()
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr1, makeReq())
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first upstream request did not start")
	}

	rr2 := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr2, makeReq())
	if rr2.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rr2.Code, rr2.Body.String())
	}
	if !strings.Contains(rr2.Body.String(), "LLM_PROVIDER_QUEUE_TIMEOUT") {
		t.Fatalf("expected queue timeout error, body = %s", rr2.Body.String())
	}

	close(releaseUpstream)
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("first request did not finish")
	}
}
