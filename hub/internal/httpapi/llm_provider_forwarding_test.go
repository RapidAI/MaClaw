package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmcache"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	openai "github.com/openai/openai-go"
	openaioption "github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
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

func TestForwardAuthorizedModelRequestRoutesVirtualMaClawProviderWithTenant(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	var seenTenant string
	var seenAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		seenTenant = r.Header.Get("X-Tenant-ID")
		seenAuth = r.Header.Get("Authorization")
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "maclaw-official",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5},
		})
	}))
	defer server.Close()

	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: server.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	reg := &im.LLMProviderRegistry{}
	model := &llmservice.AuthorizedModel{
		Name:            "auto",
		ProviderIDs:     []string{llmservice.MaClawOfficialProviderID},
		ServiceGroupIDs: []string{llmservice.MaClawOfficialServiceGroupID},
		ProviderServiceGroups: map[string][]string{
			llmservice.MaClawOfficialProviderID: {llmservice.MaClawOfficialServiceGroupID},
		},
	}
	body := map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))

	respBody, statusCode, providerID, serviceGroupIDs, usage, cacheHit, err := forwardAuthorizedModelRequestWithCache(req, reg, model, body, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("forwardAuthorizedModelRequestWithCache() error = %v", err)
	}
	if statusCode != http.StatusOK || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q", statusCode, providerID)
	}
	if cacheHit {
		t.Fatal("unexpected cache hit")
	}
	if seenTenant != "tenant_acme" {
		t.Fatalf("X-Tenant-ID = %q, want tenant_acme", seenTenant)
	}
	if seenAuth != "Bearer machine-token" {
		t.Fatalf("Authorization = %q", seenAuth)
	}
	if len(serviceGroupIDs) != 1 || serviceGroupIDs[0] != llmservice.MaClawOfficialServiceGroupID {
		t.Fatalf("serviceGroupIDs = %#v", serviceGroupIDs)
	}
	if usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", usage)
	}
	if !bytes.Contains(respBody, []byte("maclaw-official")) {
		t.Fatalf("respBody = %s", string(respBody))
	}
}

func TestForwardAuthorizedModelRequestRetriesMaClawOfficialWithSanitizedBody(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, hasStreamOptions := got["stream_options"]; hasStreamOptions {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "stream_options unsupported"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "maclaw-official-sanitized",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 4, "completion_tokens": 2, "total_tokens": 6},
		})
	}))
	defer server.Close()

	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: server.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	reg := &im.LLMProviderRegistry{}
	model := &llmservice.AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{llmservice.MaClawOfficialProviderID},
	}
	body := map[string]any{
		"model":          "auto",
		"messages":       []any{map[string]any{"role": "user", "content": "hello"}},
		"stream_options": map[string]any{"include_usage": true},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))

	respBody, statusCode, providerID, _, usage, _, err := forwardAuthorizedModelRequestWithCache(req, reg, model, body, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("forwardAuthorizedModelRequestWithCache() error = %v", err)
	}
	if statusCode != http.StatusOK || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q", statusCode, providerID)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
	if usage.TotalTokens != 6 {
		t.Fatalf("usage = %#v", usage)
	}
	if !bytes.Contains(respBody, []byte("maclaw-official-sanitized")) {
		t.Fatalf("respBody = %s", string(respBody))
	}
}

func TestForwardAuthorizedModelRequestDoesNotDowngradeStructuredContract(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, hasResponseFormat := got["response_format"]; !hasResponseFormat {
			t.Fatalf("structured response contract was removed: %#v", got)
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "response_format unsupported"}})
	}))
	defer server.Close()

	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: server.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	reg := &im.LLMProviderRegistry{}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}
	body := map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "classify this request"}},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "intent_tree", "schema": map[string]any{"type": "object"}},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))

	_, statusCode, providerID, _, _, _, err := forwardAuthorizedModelRequestWithCache(req, reg, model, body, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("forwardAuthorizedModelRequestWithCache() error = %v", err)
	}
	if statusCode != http.StatusBadRequest || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q, want 400/%q", statusCode, providerID, llmservice.MaClawOfficialProviderID)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1; structured contract must not be retried without response_format", attempts.Load())
	}
}

func TestForwardAuthorizedResponsesRequestDoesNotDowngradeStructuredContract(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, hasResponseFormat := got["response_format"]; !hasResponseFormat {
			t.Fatalf("Responses structured contract was removed during chat conversion: %#v", got)
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "response_format unsupported"}})
	}))
	defer server.Close()

	SetMaClawModule(&llmservice.MaClawModule{Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: server.URL,
		HubID:        "hub-1",
		MachineToken: "machine-token",
	})})

	responsesBody := map[string]any{
		"model": "auto",
		"input": "classify this request",
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "intent_tree", "strict": true,
			"schema": map[string]any{"type": "object", "properties": map[string]any{"top": map[string]any{"type": "array"}}},
		}},
	}
	chatBody, _, err := corelib.OpenAICompatResponsesRequestToChat(responsesBody)
	if err != nil {
		t.Fatalf("convert responses request: %v", err)
	}
	reg := &im.LLMProviderRegistry{}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/responses", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))

	_, statusCode, providerID, _, _, _, rawResponses, err := forwardAuthorizedResponsesRequestWithCache(req, reg, model, responsesBody, chatBody, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("forwardAuthorizedResponsesRequestWithCache() error = %v", err)
	}
	if statusCode != http.StatusBadRequest || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q, want 400/%q", statusCode, providerID, llmservice.MaClawOfficialProviderID)
	}
	if rawResponses {
		t.Fatal("rawResponses = true, want official chat-compatible path")
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1; structured contract must not be retried without response_format", attempts.Load())
	}
}

func TestForwardAuthorizedModelRequestPreservesMaClawUnavailableStatus(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)
	SetMaClawModule(nil)

	reg := &im.LLMProviderRegistry{}
	model := &llmservice.AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{llmservice.MaClawOfficialProviderID},
	}
	body := map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))

	respBody, statusCode, providerID, _, _, _, err := forwardAuthorizedModelRequestWithCache(req, reg, model, body, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("forwardAuthorizedModelRequestWithCache() error = %v", err)
	}
	if statusCode != http.StatusServiceUnavailable || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q, want 503/%q", statusCode, providerID, llmservice.MaClawOfficialProviderID)
	}
	if !strings.Contains(string(respBody), "not configured") {
		t.Fatalf("respBody = %s", string(respBody))
	}
}

func TestStreamAuthorizedModelRequestRoutesVirtualMaClawProviderWithTenant(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	var seenTenant string
	var seenAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		seenTenant = r.Header.Get("X-Tenant-ID")
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-official\",\"object\":\"chat.completion.chunk\",\"model\":\"auto\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: server.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	reg := &im.LLMProviderRegistry{}
	model := &llmservice.AuthorizedModel{
		Name:            "auto",
		ProviderIDs:     []string{llmservice.MaClawOfficialProviderID},
		ServiceGroupIDs: []string{llmservice.MaClawOfficialServiceGroupID},
		ProviderServiceGroups: map[string][]string{
			llmservice.MaClawOfficialProviderID: {llmservice.MaClawOfficialServiceGroupID},
		},
	}
	body := map[string]any{
		"model":    "auto",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))
	rec := httptest.NewRecorder()

	statusCode, providerID, serviceGroupIDs, usage, wroteStream, err := streamAuthorizedModelRequest(rec, req, reg, model, body, "auto", nil)
	if err != nil {
		t.Fatalf("streamAuthorizedModelRequest() error = %v", err)
	}
	if statusCode != http.StatusOK || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q", statusCode, providerID)
	}
	if !wroteStream {
		t.Fatal("wroteStream = false")
	}
	if seenTenant != "tenant_acme" {
		t.Fatalf("X-Tenant-ID = %q, want tenant_acme", seenTenant)
	}
	if seenAuth != "Bearer machine-token" {
		t.Fatalf("Authorization = %q", seenAuth)
	}
	if len(serviceGroupIDs) != 1 || serviceGroupIDs[0] != llmservice.MaClawOfficialServiceGroupID {
		t.Fatalf("serviceGroupIDs = %#v", serviceGroupIDs)
	}
	if usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", usage)
	}
	if !strings.Contains(rec.Body.String(), "chatcmpl-official") {
		t.Fatalf("stream body = %s", rec.Body.String())
	}
}

func TestStreamAuthorizedModelRequestDoesNotDowngradeTools(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, hasTools := got["tools"]; !hasTools {
			t.Fatalf("tool contract was removed: %#v", got)
		}
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "tools unsupported"}})
	}))
	defer server.Close()

	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: server.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	reg := &im.LLMProviderRegistry{}
	model := &llmservice.AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{llmservice.MaClawOfficialProviderID},
	}
	body := map[string]any{
		"model":    "auto",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
		"tools": []any{map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "lookup",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))
	rec := httptest.NewRecorder()

	statusCode, providerID, _, usage, wroteStream, err := streamAuthorizedModelRequest(rec, req, reg, model, body, "auto", nil)
	if statusCode != http.StatusBadRequest || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q, want 400/%q", statusCode, providerID, llmservice.MaClawOfficialProviderID)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts = %d, want 1; tool contract must not be retried without tools", attempts.Load())
	}
	if !wroteStream {
		t.Fatal("wroteStream = false; the explicit upstream capability error should be relayed")
	}
	if usage.TotalTokens != 0 {
		t.Fatalf("usage = %#v", usage)
	}
	if err != nil {
		t.Fatalf("streamAuthorizedModelRequest() error = %v", err)
	}
}

func TestForwardAuthorizedModelRequestDoesNotFallbackAfterMaClawAuthorizationDenied(t *testing.T) {
	llmservice.ResetRequestProviderWRR()
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "authorization denied: no active authorization"}})
	}))
	defer server.Close()
	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: server.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "fallback",
			"model": "fallback-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "fallback"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer fallback.Close()

	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-fallback", APIURL: fallback.URL, Model: "fallback-model"}}}
	model := &llmservice.AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{llmservice.MaClawOfficialProviderID, "provider-fallback"},
	}
	body := map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))

	respBody, statusCode, providerID, _, _, _, err := forwardAuthorizedModelRequestWithCache(req, reg, model, body, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("forwardAuthorizedModelRequestWithCache() error = %v", err)
	}
	if statusCode != http.StatusForbidden || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q, want 403/%q", statusCode, providerID, llmservice.MaClawOfficialProviderID)
	}
	if fallbackHits.Load() != 0 {
		t.Fatalf("fallback provider should not be called after official auth denial, hits = %d", fallbackHits.Load())
	}
	status, code, detail := providerAuthOrRateError(statusCode, providerID, respBody)
	if status != http.StatusForbidden || code != "LLM_OFFICIAL_AUTHORIZATION_DENIED" || !strings.Contains(detail, "authorization denied") {
		t.Fatalf("classified error = %d/%s/%s", status, code, detail)
	}
}

func TestStreamAuthorizedModelRequestDoesNotFallbackAfterMaClawAuthorizationDenied(t *testing.T) {
	llmservice.ResetRequestProviderWRR()
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "authorization denied: no active authorization"}})
	}))
	defer server.Close()
	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: server.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer fallback.Close()

	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-fallback", APIURL: fallback.URL, Model: "fallback-model"}}}
	model := &llmservice.AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{llmservice.MaClawOfficialProviderID, "provider-fallback"},
	}
	body := map[string]any{
		"model":    "auto",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))
	rec := httptest.NewRecorder()

	statusCode, providerID, _, _, wroteStream, err := streamAuthorizedModelRequest(rec, req, reg, model, body, "auto", nil)
	if err == nil {
		t.Fatal("streamAuthorizedModelRequest() error = nil, want official auth denial")
	}
	if statusCode != http.StatusForbidden || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q, want 403/%q", statusCode, providerID, llmservice.MaClawOfficialProviderID)
	}
	if wroteStream {
		t.Fatal("wroteStream = true, want false")
	}
	if fallbackHits.Load() != 0 {
		t.Fatalf("fallback provider should not be called after official auth denial, hits = %d", fallbackHits.Load())
	}
	status, code, detail, ok := llmEndpointUpstreamAuthOrRateError(statusCode, providerID, err)
	if !ok || status != http.StatusForbidden || code != "LLM_OFFICIAL_AUTHORIZATION_DENIED" || !strings.Contains(detail, "authorization denied") {
		t.Fatalf("classified stream error = ok:%v %d/%s/%s", ok, status, code, detail)
	}
}

func TestForwardAuthorizedResponsesRequestDoesNotFallbackAfterMaClawAuthorizationDenied(t *testing.T) {
	llmservice.ResetRequestProviderWRR()
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "authorization denied: no active authorization"}})
	}))
	defer server.Close()
	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: server.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "fallback",
			"model": "fallback-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "fallback"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer fallback.Close()

	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-fallback", APIURL: fallback.URL, Model: "fallback-model"}}}
	model := &llmservice.AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{llmservice.MaClawOfficialProviderID, "provider-fallback"},
	}
	responsesBody := map[string]any{
		"model": "auto",
		"input": "hello",
	}
	chatBody := map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/responses", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))

	respBody, statusCode, providerID, _, _, _, rawResponses, err := forwardAuthorizedResponsesRequestWithCache(req, reg, model, responsesBody, chatBody, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("forwardAuthorizedResponsesRequestWithCache() error = %v", err)
	}
	if statusCode != http.StatusForbidden || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q, want 403/%q", statusCode, providerID, llmservice.MaClawOfficialProviderID)
	}
	if rawResponses {
		t.Fatal("rawResponses = true, want chat-compatible official path")
	}
	if fallbackHits.Load() != 0 {
		t.Fatalf("fallback provider should not be called after official auth denial, hits = %d", fallbackHits.Load())
	}
	status, code, detail := providerAuthOrRateError(statusCode, providerID, respBody)
	if status != http.StatusForbidden || code != "LLM_OFFICIAL_AUTHORIZATION_DENIED" || !strings.Contains(detail, "authorization denied") {
		t.Fatalf("classified responses error = %d/%s/%s", status, code, detail)
	}
}

func TestStreamAuthorizedResponsesRequestDoesNotFallbackAfterMaClawAuthorizationDenied(t *testing.T) {
	llmservice.ResetRequestProviderWRR()
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": "authorization denied: no active authorization"}})
	}))
	defer server.Close()
	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: server.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	var fallbackHits atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer fallback.Close()

	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-fallback", APIURL: fallback.URL, Model: "fallback-model"}}}
	model := &llmservice.AuthorizedModel{
		Name:        "auto",
		ProviderIDs: []string{llmservice.MaClawOfficialProviderID, "provider-fallback"},
	}
	responsesBody := map[string]any{
		"model":  "auto",
		"input":  "hello",
		"stream": true,
	}
	chatBody := map[string]any{
		"model":    "auto",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/responses", nil)
	req = req.WithContext(store.WithTenant(req.Context(), "tenant_acme"))
	rec := httptest.NewRecorder()

	statusCode, providerID, _, _, wroteStream, err := streamAuthorizedResponsesRequest(rec, req, reg, model, responsesBody, chatBody, "auto", "auto", nil)
	if err == nil {
		t.Fatal("streamAuthorizedResponsesRequest() error = nil, want official auth denial")
	}
	if statusCode != http.StatusForbidden || providerID != llmservice.MaClawOfficialProviderID {
		t.Fatalf("status/provider = %d/%q, want 403/%q", statusCode, providerID, llmservice.MaClawOfficialProviderID)
	}
	if wroteStream {
		t.Fatal("wroteStream = true, want false")
	}
	if fallbackHits.Load() != 0 {
		t.Fatalf("fallback provider should not be called after official auth denial, hits = %d", fallbackHits.Load())
	}
	status, code, detail, ok := llmEndpointUpstreamAuthOrRateError(statusCode, providerID, err)
	if !ok || status != http.StatusForbidden || code != "LLM_OFFICIAL_AUTHORIZATION_DENIED" || !strings.Contains(detail, "authorization denied") {
		t.Fatalf("classified responses stream error = ok:%v %d/%s/%s", ok, status, code, detail)
	}
}

func TestProviderAuthOrRateErrorClassifiesMaClawAuthorization(t *testing.T) {
	status, code, detail := providerAuthOrRateError(http.StatusForbidden, llmservice.MaClawOfficialProviderID, []byte(`{"error":{"message":"authorization denied: no active authorization"}}`))
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", status)
	}
	if code != "LLM_OFFICIAL_AUTHORIZATION_DENIED" {
		t.Fatalf("code = %q", code)
	}
	if !strings.Contains(detail, "tenant authorization") || !strings.Contains(detail, "authorization denied") {
		t.Fatalf("detail = %q", detail)
	}
}

func TestLLMEndpointUpstreamAuthOrRateErrorClassifiesMaClawAuthorization(t *testing.T) {
	status, code, detail, ok := llmEndpointUpstreamAuthOrRateError(http.StatusForbidden, llmservice.MaClawOfficialProviderID, errors.New("authorization denied: no active authorization"))
	if !ok {
		t.Fatal("ok = false")
	}
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", status)
	}
	if code != "LLM_OFFICIAL_AUTHORIZATION_DENIED" {
		t.Fatalf("code = %q", code)
	}
	if !strings.Contains(detail, "tenant authorization") || !strings.Contains(detail, "authorization denied") {
		t.Fatalf("detail = %q", detail)
	}
}

func TestProviderUnavailableErrorClassifiesMaClawOfficial(t *testing.T) {
	status, code, detail := providerUnavailableError(http.StatusServiceUnavailable, llmservice.MaClawOfficialProviderID, []byte(`{"error":{"message":"all providers failed"}}`))
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", status)
	}
	if code != "LLM_OFFICIAL_UNAVAILABLE" {
		t.Fatalf("code = %q", code)
	}
	if !strings.Contains(detail, "official service") || !strings.Contains(detail, "all providers failed") {
		t.Fatalf("detail = %q", detail)
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
	if !strings.Contains(rr.Body.String(), `"model":"auto"`) || !strings.Contains(rr.Body.String(), "data: [DONE]") || !strings.Contains(rr.Body.String(), "event: message") {
		t.Fatalf("unexpected stream body: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), ": keepalive") {
		t.Fatalf("stream body leaked upstream heartbeat comment: %s", rr.Body.String())
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

func TestWriteOpenAIStreamResponseFiltersCommentOnlyHeartbeats(t *testing.T) {
	body := ": ping\n\n" +
		"event: ping\n\n" +
		"data:\n\n" +
		"data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n" +
		"data: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	rec := httptest.NewRecorder()
	_, wroteStream, err := writeOpenAIStreamResponse(rec, resp, &im.LLMProvider{ID: "provider-a"}, &llmservice.AuthorizedModel{Name: "auto"}, "auto", nil)
	if err != nil {
		t.Fatalf("writeOpenAIStreamResponse() error = %v", err)
	}
	if !wroteStream {
		t.Fatal("wroteStream = false")
	}
	out := rec.Body.String()
	if strings.Contains(out, ": ping") {
		t.Fatalf("stream output leaked heartbeat comment: %q", out)
	}
	if strings.Contains(out, "event: ping") {
		t.Fatalf("stream output leaked heartbeat event: %q", out)
	}
	if strings.Contains(out, "data:\n\n") {
		t.Fatalf("stream output leaked empty data heartbeat: %q", out)
	}
	if !strings.Contains(out, `"content":"ok"`) || !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("stream output = %q, want data chunk and DONE", out)
	}
}

func TestWriteRawResponsesStreamResponseFiltersCommentOnlyHeartbeats(t *testing.T) {
	body := ": ping\n\n" +
		"event: ping\n\n" +
		"data:\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
		": keepalive\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	rec := httptest.NewRecorder()
	usage, wroteStream, err := writeRawResponsesStreamResponse(rec, resp, &im.LLMProvider{ID: "provider-a"}, &llmservice.AuthorizedModel{Name: "auto"}, nil)
	if err != nil {
		t.Fatalf("writeRawResponsesStreamResponse() error = %v", err)
	}
	if !wroteStream {
		t.Fatal("wroteStream = false")
	}
	out := rec.Body.String()
	if strings.Contains(out, ": ping") || strings.Contains(out, ": keepalive") {
		t.Fatalf("responses stream output leaked heartbeat comment: %q", out)
	}
	if strings.Contains(out, "event: ping") {
		t.Fatalf("responses stream output leaked heartbeat event: %q", out)
	}
	if strings.Contains(out, "data:\n\n") {
		t.Fatalf("responses stream output leaked empty data heartbeat: %q", out)
	}
	if !strings.Contains(out, "event: response.output_text.delta") || !strings.Contains(out, `"delta":"ok"`) || !strings.Contains(out, "event: response.completed") {
		t.Fatalf("responses stream output = %q, want real SSE events", out)
	}
	if usage.InputTokens != 2 || usage.OutputTokens != 3 || usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v, want 2/3/5 tokens", usage)
	}
}

func TestLLMV1ChatCompletionsHandlerFlushesStreamHeadersBeforeFirstToken(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "stream-header@example.com")
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
			Email:           "stream-header@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	releaseFirstToken := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-releaseFirstToken:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte("data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()
	defer close(releaseFirstToken)

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	hub := httptest.NewServer(LLMV1ChatCompletionsHandler(identity, system, nil))
	defer hub.Close()

	bodyBytes, err := json.Marshal(map[string]any{
		"model":    "auto",
		"stream":   true,
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, hub.URL+"/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")

	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		resp, err := hub.Client().Do(req)
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	select {
	case err := <-errCh:
		t.Fatalf("stream request failed before first token: %v", err)
	case resp := <-respCh:
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		if !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("content-type = %q, want text/event-stream", resp.Header.Get("Content-Type"))
		}
		if !strings.Contains(resp.Header.Get("Cache-Control"), "no-transform") {
			t.Fatalf("cache-control = %q, want no-transform", resp.Header.Get("Cache-Control"))
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("hub did not flush stream headers before the first upstream token")
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
					"role":              "assistant",
					"content":           "json fallback",
					"reasoning_content": "checking official path",
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
	if !strings.Contains(rr.Body.String(), "json fallback") || !strings.Contains(rr.Body.String(), "checking official path") || !strings.Contains(rr.Body.String(), `"reasoning_content"`) || !strings.Contains(rr.Body.String(), "data: [DONE]") || !strings.Contains(rr.Body.String(), `"model":"auto"`) || !strings.Contains(rr.Body.String(), `"index":0`) || !strings.Contains(rr.Body.String(), `"finish_reason":"tool_calls"`) {
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

func TestSynthesizeOpenAIStreamChunkAcceptsGoStyleReasoningContent(t *testing.T) {
	chunk, _, err := synthesizeOpenAIStreamChunk([]byte(`{
		"id":"json-upstream",
		"choices":[{
			"message":{"role":"assistant","content":"json fallback","ReasoningContent":"go style thinking"},
			"finish_reason":"stop"
		}]
	}`), "auto")
	if err != nil {
		t.Fatalf("synthesizeOpenAIStreamChunk: %v", err)
	}
	body := string(chunk)
	if !strings.Contains(body, `"reasoning_content":"go style thinking"`) {
		t.Fatalf("synthesized chunk missing go-style reasoning_content: %s", body)
	}
}

func TestLLMV1ChatCompletionsHandlerReturnsOpenAIErrorWhenStreamUpstream503Empty(t *testing.T) {
	globalProviderResilience.reset()
	defer globalProviderResilience.reset()
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "stream-503@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-stream-503"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "stream-503@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-stream-503", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	bodyBytes, err := json.Marshal(map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
		"stream":   true,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if rr.Body.Len() == 0 {
		t.Fatal("expected OpenAI-compatible error body, got empty body")
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error body: %v body=%s", err, rr.Body.String())
	}
	errObj := payload["error"].(map[string]any)
	if errObj["code"] != "LLM_UPSTREAM_FAILED" || !strings.Contains(fmt.Sprint(errObj["message"]), "temporarily unavailable") {
		t.Fatalf("error object = %#v", errObj)
	}
}

func TestLLMV1ChatCompletionsHandlerReturnsRateLimitWhenStreamUpstream429(t *testing.T) {
	globalProviderResilience.reset()
	defer globalProviderResilience.reset()
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "stream-429@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-stream-429"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "stream-429@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": map[string]any{"message": "too many upstream requests", "type": "rate_limit_error"},
		})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-stream-429", APIURL: server.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	bodyBytes, err := json.Marshal(map[string]any{
		"model":    "auto",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
		"stream":   true,
	})
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
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error body: %v body=%s", err, rr.Body.String())
	}
	errObj := payload["error"].(map[string]any)
	if errObj["code"] != "LLM_UPSTREAM_RATE_LIMITED" || !strings.Contains(fmt.Sprint(errObj["message"]), "rate limited") {
		t.Fatalf("error object = %#v", errObj)
	}
}

func TestOpenAISDKClientCanCallHubChatCompletionsEndpoint(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "chat-sdk@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-chat-sdk"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "chat-sdk@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["model"] != "test-model" {
			t.Fatalf("upstream model = %#v, want test-model", upstreamBody["model"])
		}
		if upstreamBody["stream"] != false {
			t.Fatalf("upstream stream = %#v, want false", upstreamBody["stream"])
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "chatcmpl-hub-chat-sdk",
			"object": "chat.completion",
			"model":  "test-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "hub chat sdk ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-chat-sdk", APIURL: upstream.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/chat/completions", LLMV1ChatCompletionsHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	resp, err := client.Chat.Completions.New(context.Background(), openai.ChatCompletionNewParams{
		Model: shared.ChatModel("auto"),
		Messages: []openai.ChatCompletionMessageParamUnion{{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("hi"),
				},
			},
		}},
		MaxTokens: openai.Int(32),
	})
	if err != nil {
		t.Fatalf("openai SDK Chat.Completions.New failed: %v", err)
	}
	if resp.ID != "chatcmpl-hub-chat-sdk" {
		t.Fatalf("response id = %q, want chatcmpl-hub-chat-sdk", resp.ID)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hub chat sdk ok" {
		t.Fatalf("choices = %#v", resp.Choices)
	}
	if resp.Usage.PromptTokens != 2 || resp.Usage.CompletionTokens != 3 || resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v, want 2/3/5", resp.Usage)
	}
}

func TestOpenAISDKClientCanStreamHubChatCompletionsEndpoint(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "chat-sdk-stream@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{"provider-chat-sdk-stream"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "chat-sdk-stream@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["stream"] != true {
			t.Fatalf("upstream stream = %#v, want true", upstreamBody["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hub \"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chunk-2\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"stream\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-chat-sdk-stream", APIURL: upstream.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/chat/completions", LLMV1ChatCompletionsHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	stream := client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model: shared.ChatModel("auto"),
		Messages: []openai.ChatCompletionMessageParamUnion{{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("hi"),
				},
			},
		}},
	})
	var text strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			text.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK Chat.Completions.NewStreaming failed: %v", err)
	}
	if text.String() != "hub stream" {
		t.Fatalf("stream text = %q, want hub stream", text.String())
	}
}

func TestOpenAISDKClientCanStreamHubChatCompletionsViaMaClawOfficial(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "chat-sdk-official-stream@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           llmservice.MaClawOfficialServiceGroupID,
			Name:         "MaClaw Official",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{llmservice.MaClawOfficialProviderID},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "chat-sdk-official-stream@example.com",
			ServiceGroupIDs: []string{llmservice.MaClawOfficialServiceGroupID},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var hubCenterHits atomic.Int32
	hubCenter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hubCenterHits.Add(1)
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			t.Fatalf("hubcenter path = %q, want /api/llm/v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("X-Hub-ID"); got != "hub-1" {
			t.Fatalf("X-Hub-ID = %q, want hub-1", got)
		}
		if got := r.Header.Get("X-Tenant-ID"); got == "" {
			t.Fatal("X-Tenant-ID is empty")
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode hubcenter body: %v", err)
		}
		if upstreamBody["stream"] != true {
			t.Fatalf("hubcenter stream = %#v, want true", upstreamBody["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: {\"id\":\"official-1\",\"object\":\"chat.completion.chunk\",\"model\":\"auto\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"official \"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"official-2\",\"object\":\"chat.completion.chunk\",\"model\":\"auto\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"stream\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer hubCenter.Close()

	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: hubCenter.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/chat/completions", LLMV1ChatCompletionsHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	stream := client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model: shared.ChatModel("auto"),
		Messages: []openai.ChatCompletionMessageParamUnion{{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("hi"),
				},
			},
		}},
	})
	var text strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			text.WriteString(chunk.Choices[0].Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK official streaming failed: %v", err)
	}
	if text.String() != "official stream" {
		t.Fatalf("stream text = %q, want official stream", text.String())
	}
	if hubCenterHits.Load() != 1 {
		t.Fatalf("hubcenter hits = %d, want 1", hubCenterHits.Load())
	}
}

func TestOpenAISDKClientReceivesMaClawOfficialStreamError(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "chat-sdk-official-stream-error@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           llmservice.MaClawOfficialServiceGroupID,
			Name:         "MaClaw Official",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "auto",
				ProviderIDs: []string{llmservice.MaClawOfficialProviderID},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "chat-sdk-official-stream-error@example.com",
			ServiceGroupIDs: []string{llmservice.MaClawOfficialServiceGroupID},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	hubCenter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": ping\n\n"))
		_, _ = w.Write([]byte("event: ping\n\n"))
		_, _ = w.Write([]byte("data: {\"error\":{\"message\":\"upstream provider exhausted\",\"type\":\"server_error\",\"code\":503}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer hubCenter.Close()

	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: hubCenter.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/chat/completions", LLMV1ChatCompletionsHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	stream := client.Chat.Completions.NewStreaming(context.Background(), openai.ChatCompletionNewParams{
		Model: shared.ChatModel("auto"),
		Messages: []openai.ChatCompletionMessageParamUnion{{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfString: openai.String("hi"),
				},
			},
		}},
	})
	for stream.Next() {
		t.Fatalf("unexpected stream chunk before error: %#v", stream.Current())
	}
	err := stream.Err()
	if err == nil {
		t.Fatal("stream.Err() = nil, want upstream provider error")
	}
	errText := err.Error()
	if !strings.Contains(errText, "upstream provider exhausted") {
		t.Fatalf("stream.Err() = %q, want upstream provider exhausted", errText)
	}
	if strings.Contains(errText, "unexpected end of JSON input") {
		t.Fatalf("stream.Err() exposed JSON parser failure instead of provider error: %q", errText)
	}
}

func TestLLMV1ResponsesHandlerForwardsThroughSharedOpenAICompatLayer(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-binding@example.com")
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
			Email:           "responses-binding@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["model"] != "test-model" {
			t.Fatalf("upstream model = %#v, want test-model", upstreamBody["model"])
		}
		if _, ok := upstreamBody["input"]; ok {
			t.Fatalf("responses input leaked to chat upstream: %#v", upstreamBody)
		}
		tools, _ := upstreamBody["tools"].([]any)
		if len(tools) != 1 {
			t.Fatalf("upstream tools = %#v, want one tool", upstreamBody["tools"])
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "chat-upstream",
			"object": "chat.completion",
			"model":  "test-model",
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "get_ticket",
							"arguments": `{"id":"T-1"}`,
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

	bodyBytes, err := json.Marshal(map[string]any{
		"model": "auto",
		"input": []any{map[string]any{
			"type":    "message",
			"role":    "user",
			"content": []any{map[string]any{"type": "input_text", "text": "lookup ticket"}},
		}},
		"tools": []any{map[string]any{
			"type":        "function",
			"name":        "get_ticket",
			"description": "get ticket",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}}, "required": []any{"id"}},
		}},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/responses", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ResponsesHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected upstream to be called once, hits = %d", upstreamHits.Load())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["object"] != "response" {
		t.Fatalf("object = %#v, want response", got["object"])
	}
	output, _ := got["output"].([]any)
	if len(output) != 1 {
		t.Fatalf("output = %#v, want one function_call", got["output"])
	}
	call, _ := output[0].(map[string]any)
	if call["type"] != "function_call" || call["name"] != "get_ticket" || !strings.Contains(fmt.Sprint(call["arguments"]), "T-1") {
		t.Fatalf("unexpected function_call output: %#v", call)
	}
}

func TestOpenAISDKClientCanCallHubResponsesEndpoint(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-sdk@example.com")
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
			Email:           "responses-sdk@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["model"] != "test-model" {
			t.Fatalf("upstream model = %#v, want test-model", upstreamBody["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "chatcmpl-hub-responses-sdk",
			"object": "chat.completion",
			"model":  "test-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "hub sdk ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/responses", LLMV1ResponsesHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	resp, err := client.Responses.New(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("hi"),
		},
		MaxOutputTokens: openai.Int(32),
	})
	if err != nil {
		t.Fatalf("openai SDK Responses.New failed: %v", err)
	}
	if resp.ID != "chatcmpl-hub-responses-sdk" {
		t.Fatalf("response id = %q, want chatcmpl-hub-responses-sdk", resp.ID)
	}
	if resp.CreatedAt == 0 {
		t.Fatalf("CreatedAt = 0, want created_at field populated")
	}
	if got := resp.OutputText(); got != "hub sdk ok" {
		t.Fatalf("OutputText = %q, want hub sdk ok", got)
	}
	if resp.Usage.InputTokens != 2 || resp.Usage.OutputTokens != 3 || resp.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %+v, want 2/3/5", resp.Usage)
	}
}

func TestOpenAISDKClientCanForceFunctionToolChoiceOnHubResponsesEndpoint(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-sdk-tool-choice@example.com")
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
			Email:           "responses-sdk-tool-choice@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		toolChoice, _ := upstreamBody["tool_choice"].(map[string]any)
		fn, _ := toolChoice["function"].(map[string]any)
		if toolChoice["type"] != "function" || fn["name"] != "get_ticket" {
			t.Fatalf("upstream tool_choice = %#v, want forced get_ticket", upstreamBody["tool_choice"])
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "chatcmpl-hub-tool-choice",
			"object": "chat.completion",
			"model":  "test-model",
			"choices": []any{map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "get_ticket",
							"arguments": `{"id":"T-1"}`,
						},
					}},
				},
				"finish_reason": "tool_calls",
			}},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/responses", LLMV1ResponsesHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	resp, err := client.Responses.New(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("lookup"),
		},
		Tools: []responses.ToolUnionParam{{
			OfFunction: &responses.FunctionToolParam{
				Name:        "get_ticket",
				Description: openai.String("get ticket"),
				Parameters:  map[string]any{"type": "object"},
			},
		}},
		ToolChoice: responses.ResponseNewParamsToolChoiceUnion{
			OfFunctionTool: &responses.ToolChoiceFunctionParam{Name: "get_ticket"},
		},
	})
	if err != nil {
		t.Fatalf("openai SDK Responses.New failed: %v", err)
	}
	if len(resp.Output) != 1 || resp.Output[0].Type != "function_call" {
		t.Fatalf("response output = %+v, want function_call", resp.Output)
	}
}

func TestOpenAISDKClientCanStreamHubResponsesEndpoint(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-sdk-stream@example.com")
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
			Email:           "responses-sdk-stream@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["stream"] != true {
			t.Fatalf("upstream stream = %#v, want true", upstreamBody["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hub\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" stream\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/responses", LLMV1ResponsesHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("hi"),
		},
	})
	var got strings.Builder
	var completed bool
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case responses.ResponseTextDeltaEvent:
			got.WriteString(event.Delta)
		case responses.ResponseCompletedEvent:
			completed = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK Responses.NewStreaming failed: %v", err)
	}
	if got.String() != "hub stream" {
		t.Fatalf("stream content = %q, want hub stream", got.String())
	}
	if !completed {
		t.Fatal("stream did not emit response.completed")
	}
}

func TestOpenAISDKClientCanStreamHubResponsesEndpointWhenUpstreamReturnsJSON(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-sdk-stream-json@example.com")
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
			Email:           "responses-sdk-stream-json@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["stream"] != true {
			t.Fatalf("upstream stream = %#v, want true", upstreamBody["stream"])
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "chatcmpl-json-fallback",
			"object": "chat.completion",
			"model":  "test-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "json fallback stream"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/responses", LLMV1ResponsesHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("hi"),
		},
	})
	var got strings.Builder
	var completed bool
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case responses.ResponseTextDeltaEvent:
			got.WriteString(event.Delta)
		case responses.ResponseCompletedEvent:
			completed = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK Responses.NewStreaming failed: %v", err)
	}
	if got.String() != "json fallback stream" {
		t.Fatalf("stream content = %q, want json fallback stream", got.String())
	}
	if !completed {
		t.Fatal("stream did not emit response.completed")
	}
}

func TestOpenAISDKClientCanStreamHubResponsesEndpointWithRawResponsesProvider(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-sdk-stream-raw@example.com")
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
			Email:           "responses-sdk-stream-raw@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("upstream path = %q, want /v1/responses", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["model"] != "test-model" || upstreamBody["stream"] != true {
			t.Fatalf("unexpected upstream body: %#v", upstreamBody)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		events := []struct {
			name string
			body map[string]any
		}{
			{"response.created", map[string]any{"type": "response.created", "sequence_number": 1, "response": map[string]any{"id": "resp_raw_stream", "object": "response", "created_at": float64(time.Now().Unix()), "status": "in_progress", "model": "test-model", "output": []any{}, "usage": map[string]any{}}}},
			{"response.output_item.added", map[string]any{"type": "response.output_item.added", "sequence_number": 2, "output_index": 0, "item": map[string]any{"id": "msg_raw", "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}}},
			{"response.content_part.added", map[string]any{"type": "response.content_part.added", "sequence_number": 3, "item_id": "msg_raw", "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}}},
			{"response.output_text.delta", map[string]any{"type": "response.output_text.delta", "sequence_number": 4, "item_id": "msg_raw", "output_index": 0, "content_index": 0, "delta": "raw stream", "logprobs": []any{}}},
			{"response.output_text.done", map[string]any{"type": "response.output_text.done", "sequence_number": 5, "item_id": "msg_raw", "output_index": 0, "content_index": 0, "text": "raw stream", "logprobs": []any{}}},
			{"response.content_part.done", map[string]any{"type": "response.content_part.done", "sequence_number": 6, "item_id": "msg_raw", "output_index": 0, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "raw stream", "annotations": []any{}}}},
			{"response.output_item.done", map[string]any{"type": "response.output_item.done", "sequence_number": 7, "output_index": 0, "item": map[string]any{"id": "msg_raw", "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "raw stream", "annotations": []any{}}}}}},
			{"response.completed", map[string]any{"type": "response.completed", "sequence_number": 8, "response": map[string]any{"id": "resp_raw_stream", "object": "response", "created_at": float64(time.Now().Unix()), "status": "completed", "model": "test-model", "output": []any{map[string]any{"id": "msg_raw", "type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "raw stream", "annotations": []any{}}}}}, "usage": map[string]any{"input_tokens": 2, "output_tokens": 3, "total_tokens": 5}}}},
		}
		for _, event := range events {
			data, _ := json.Marshal(event.body)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.name, data)
		}
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model", WireAPI: "responses"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/responses", LLMV1ResponsesHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("hi"),
		},
	})
	var got strings.Builder
	var completed bool
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case responses.ResponseTextDeltaEvent:
			got.WriteString(event.Delta)
		case responses.ResponseCompletedEvent:
			completed = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK Responses.NewStreaming failed: %v", err)
	}
	if got.String() != "raw stream" {
		t.Fatalf("stream content = %q, want raw stream", got.String())
	}
	if !completed {
		t.Fatal("stream did not emit response.completed")
	}
}

func TestOpenAISDKClientCanStreamHubResponsesEndpointWithRawResponsesJSONFallback(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-sdk-stream-raw-json@example.com")
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
			Email:           "responses-sdk-stream-raw-json@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("upstream path = %q, want /v1/responses", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["model"] != "test-model" || upstreamBody["stream"] != true {
			t.Fatalf("unexpected upstream body: %#v", upstreamBody)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "resp_raw_json",
			"object": "response",
			"model":  "test-model",
			"status": "completed",
			"output": []any{map[string]any{
				"id":      "msg_raw_json",
				"type":    "message",
				"status":  "completed",
				"role":    "assistant",
				"content": []any{map[string]any{"type": "output_text", "text": "raw json stream", "annotations": []any{}}},
			}},
			"usage": map[string]any{"input_tokens": 2, "output_tokens": 3, "total_tokens": 5},
		})
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model", WireAPI: "responses"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/responses", LLMV1ResponsesHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("hi"),
		},
	})
	var got strings.Builder
	var completed bool
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case responses.ResponseTextDeltaEvent:
			got.WriteString(event.Delta)
		case responses.ResponseCompletedEvent:
			completed = true
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK Responses.NewStreaming failed: %v", err)
	}
	if got.String() != "raw json stream" {
		t.Fatalf("stream content = %q, want raw json stream", got.String())
	}
	if !completed {
		t.Fatal("stream did not emit response.completed")
	}
}

func TestOpenAISDKClientCanStreamHubResponsesToolCalls(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-sdk-stream-tools@example.com")
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
			Email:           "responses-sdk-stream-tools@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		tools, _ := upstreamBody["tools"].([]any)
		if len(tools) != 1 || upstreamBody["stream"] != true {
			t.Fatalf("unexpected upstream body: %#v", upstreamBody)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_read\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"main.go\\\"}\"}}]}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/responses", LLMV1ResponsesHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("read main"),
		},
		Tools: []responses.ToolUnionParam{{
			OfFunction: &responses.FunctionToolParam{
				Name:        "read_file",
				Description: openai.String("read file"),
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
					"required":   []string{"path"},
				},
			},
		}},
	})
	var delta, doneArgs string
	var completed bool
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case responses.ResponseFunctionCallArgumentsDeltaEvent:
			delta += event.Delta
		case responses.ResponseFunctionCallArgumentsDoneEvent:
			doneArgs = event.Arguments
		case responses.ResponseCompletedEvent:
			completed = true
			if len(event.Response.Output) != 1 || event.Response.Output[0].Type != "function_call" {
				t.Fatalf("completed output = %+v, want one function_call", event.Response.Output)
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK Responses.NewStreaming failed: %v", err)
	}
	if delta != `{"path":"main.go"}` || doneArgs != `{"path":"main.go"}` {
		t.Fatalf("tool args delta=%q done=%q, want JSON args", delta, doneArgs)
	}
	if !completed {
		t.Fatal("stream did not emit response.completed")
	}
}

func TestOpenAISDKClientCanStreamHubResponsesLegacyFunctionCall(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-sdk-stream-legacy-function@example.com")
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
			Email:           "responses-sdk-stream-legacy-function@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("upstream path = %q, want /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"function_call\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\"}}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"function_call\":{\"arguments\":\"\\\"main.go\\\"}\"}}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"function_call\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: upstream.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/llm/v1/responses", LLMV1ResponsesHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	stream := client.Responses.NewStreaming(context.Background(), responses.ResponseNewParams{
		Model: shared.ResponsesModel("auto"),
		Input: responses.ResponseNewParamsInputUnion{
			OfString: openai.String("read main"),
		},
		Tools: []responses.ToolUnionParam{{
			OfFunction: &responses.FunctionToolParam{
				Name:        "read_file",
				Description: openai.String("read file"),
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
					"required":   []string{"path"},
				},
			},
		}},
	})
	var delta, doneArgs string
	var completed bool
	for stream.Next() {
		switch event := stream.Current().AsAny().(type) {
		case responses.ResponseFunctionCallArgumentsDeltaEvent:
			delta += event.Delta
		case responses.ResponseFunctionCallArgumentsDoneEvent:
			doneArgs = event.Arguments
		case responses.ResponseCompletedEvent:
			completed = true
			if len(event.Response.Output) != 1 || event.Response.Output[0].Type != "function_call" {
				t.Fatalf("completed output = %+v, want one function_call", event.Response.Output)
			}
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai SDK Responses.NewStreaming failed: %v", err)
	}
	if delta != `{"path":"main.go"}` || doneArgs != `{"path":"main.go"}` {
		t.Fatalf("tool args delta=%q done=%q, want JSON args", delta, doneArgs)
	}
	if !completed {
		t.Fatal("stream did not emit response.completed")
	}
}

func TestLLMV1ResponsesHandlerPassesThroughResponsesWireProvider(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "responses-raw-binding@example.com")
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
			Email:           "responses-raw-binding@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	var upstreamHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits.Add(1)
		if r.URL.Path != "/v1/responses" {
			t.Fatalf("upstream path = %q, want /v1/responses", r.URL.Path)
		}
		var upstreamBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&upstreamBody); err != nil {
			t.Fatalf("decode upstream body: %v", err)
		}
		if upstreamBody["model"] != "test-model" {
			t.Fatalf("upstream model = %#v, want test-model", upstreamBody["model"])
		}
		if upstreamBody["previous_response_id"] != "resp_prev" {
			t.Fatalf("previous_response_id = %#v, want resp_prev; body=%#v", upstreamBody["previous_response_id"], upstreamBody)
		}
		if _, ok := upstreamBody["messages"]; ok {
			t.Fatalf("chat messages leaked to responses upstream: %#v", upstreamBody)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":     "resp_raw",
			"object": "response",
			"model":  "test-model",
			"status": "completed",
			"output": []any{map[string]any{
				"type":    "message",
				"id":      "msg_1",
				"role":    "assistant",
				"status":  "completed",
				"content": []any{map[string]any{"type": "output_text", "text": "raw ok"}},
			}},
			"usage": map[string]any{"input_tokens": 9, "output_tokens": 4, "total_tokens": 13},
		})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model", WireAPI: "responses"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	bodyBytes, err := json.Marshal(map[string]any{
		"model":                "auto",
		"previous_response_id": "resp_prev",
		"input":                "continue",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/responses", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LLMV1ResponsesHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if upstreamHits.Load() != 1 {
		t.Fatalf("expected upstream to be called once, hits = %d", upstreamHits.Load())
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["id"] != "resp_raw" || got["object"] != "response" {
		t.Fatalf("unexpected raw responses envelope: %#v", got)
	}
}

func TestLLMV1ModelHandlerReturnsAuthorizedModel(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "model-binding@example.com")
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
			Email:           "model-binding@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: "https://example.test/v1", Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	req := httptest.NewRequest(http.MethodGet, "/api/llm/v1/models/auto", nil)
	req.SetPathValue("model", "auto")
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rr := httptest.NewRecorder()

	LLMV1ModelHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"id":"auto"`) || !strings.Contains(rr.Body.String(), `"object":"model"`) || !strings.Contains(rr.Body.String(), `"created":0`) {
		t.Fatalf("unexpected model body: %s", rr.Body.String())
	}
}

func TestLLMV1ModelsHandlerReturnsOpenAIModelObjects(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "models-list@example.com")
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
			Email:           "models-list@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: "https://example.test/v1", Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	req := httptest.NewRequest(http.MethodGet, "/api/llm/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rr := httptest.NewRecorder()

	LLMV1ModelsHandler(identity, system, nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode models response: %v", err)
	}
	if got.Object != "list" || len(got.Data) != 1 {
		t.Fatalf("unexpected models response: %#v", got)
	}
	if got.Data[0].ID != "auto" || got.Data[0].Object != "model" || got.Data[0].Created != 0 || got.Data[0].OwnedBy != "hub" {
		t.Fatalf("unexpected model object: %#v", got.Data[0])
	}
}

func TestOpenAISDKClientCanListAndGetHubModels(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "models-sdk@example.com")
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
			Email:           "models-sdk@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: "https://example.test/v1", Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/llm/v1/models", LLMV1ModelsHandler(identity, system, nil))
	mux.HandleFunc("GET /api/llm/v1/models/{model}", LLMV1ModelHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	page, err := client.Models.List(context.Background())
	if err != nil {
		t.Fatalf("openai SDK Models.List failed: %v", err)
	}
	if page == nil || len(page.Data) != 1 || page.Data[0].ID != "auto" || page.Data[0].Object != "model" {
		t.Fatalf("models page = %+v, want one auto model", page)
	}
	model, err := client.Models.Get(context.Background(), "auto")
	if err != nil {
		t.Fatalf("openai SDK Models.Get failed: %v", err)
	}
	if model.ID != "auto" || model.Object != "model" {
		t.Fatalf("model = %+v, want auto model", model)
	}
}

func TestOpenAISDKClientCanGetHubModelWithSlashID(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "models-sdk-slash@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models: []llmservice.ModelServiceModel{{
				Name:        "qax-codegen/Auto",
				ProviderIDs: []string{"provider-a"},
			}},
		}},
		UserBindings: []llmservice.UserBinding{{
			Email:           "models-sdk-slash@example.com",
			ServiceGroupIDs: []string{"coding-basic"},
		}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: "https://example.test/v1", Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/llm/v1/models", LLMV1ModelsHandler(identity, system, nil))
	mux.HandleFunc("GET /api/llm/v1/models/{model...}", LLMV1ModelHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	page, err := client.Models.List(context.Background())
	if err != nil {
		t.Fatalf("openai SDK Models.List failed: %v", err)
	}
	if page == nil || len(page.Data) != 1 || page.Data[0].ID != "qax-codegen/Auto" {
		t.Fatalf("models page = %+v, want qax-codegen/Auto", page)
	}
	model, err := client.Models.Get(context.Background(), "qax-codegen/Auto")
	if err != nil {
		t.Fatalf("openai SDK Models.Get failed: %v", err)
	}
	if model.ID != "qax-codegen/Auto" || model.Object != "model" {
		t.Fatalf("model = %+v, want qax-codegen/Auto model", model)
	}
}

func TestOpenAISDKClientGetsOpenAIErrorShapeForMissingHubModel(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "models-sdk-missing@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		UserBindings: []llmservice.UserBinding{{Email: "models-sdk-missing@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: "https://example.test/v1", Model: "test-model"}}}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/llm/v1/models/{model...}", LLMV1ModelHandler(identity, system, nil))
	server := httptest.NewServer(mux)
	defer server.Close()

	client := openai.NewClient(
		openaioption.WithBaseURL(server.URL+"/api/llm/v1"),
		openaioption.WithAPIKey(viewerToken),
	)
	_, err := client.Models.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected missing model error")
	}
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %T %v, want openai.Error", err, err)
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Code != "LLM_MODEL_NOT_FOUND" {
		t.Fatalf("openai error = status:%d code:%q message:%q", apiErr.StatusCode, apiErr.Code, apiErr.Message)
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
	if len(serviceReg.Grants) != 1 || serviceReg.Grants[0].CreditsUsed != 2 || len(serviceReg.BillingReservations) != 0 || len(serviceReg.BillingLedger) != 0 {
		t.Fatalf("expected credits to remain unchanged, got %#v", serviceReg.Grants)
	}
}

func TestLLMV1ChatCompletionsHandlerPromptCacheIsTenantScoped(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	tokenA := issueViewerTokenForTenant(t, identity, "tenant_a", "same@example.com")
	tokenB := issueViewerTokenForTenant(t, identity, "tenant_b", "same@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()

	saveTenantService := func(tenantID string) {
		t.Helper()
		if err := llmservice.SaveRegistry(ctx, scopedSystemSettingsForTenant(tenantID, system), &llmservice.Registry{
			ModelServiceGroups: []llmservice.ModelServiceGroup{{
				ID:           "coding-basic",
				Name:         "Coding Basic",
				AccessPolicy: llmservice.AccessPolicyFree,
				Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
			}},
			UserBindings: []llmservice.UserBinding{{Email: "same@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
		}); err != nil {
			t.Fatalf("save service registry %s: %v", tenantID, err)
		}
	}
	saveTenantService("tenant_a")
	saveTenantService("tenant_b")

	var hitsA atomic.Int32
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsA.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "tenant-a-upstream",
			"model": "test-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "tenant-a"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
		})
	}))
	defer serverA.Close()
	var hitsB atomic.Int32
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "tenant-b-upstream",
			"model": "test-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "tenant-b"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
		})
	}))
	defer serverB.Close()

	if err := im.SaveLLMProviderRegistry(ctx, scopedSystemSettingsForTenant("tenant_a", system), &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: serverA.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save tenant_a provider registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, scopedSystemSettingsForTenant("tenant_b", system), &im.LLMProviderRegistry{Providers: []im.LLMProvider{{ID: "provider-a", APIURL: serverB.URL, Model: "test-model"}}}); err != nil {
		t.Fatalf("save tenant_b provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	cache := llmcache.New(nil, llmcache.Config{MemoryMaxEntries: 8, MemoryMaxBytes: 1 << 20})
	handler := LLMV1ChatCompletionsHandler(identity, system, nil, cache)
	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "same prompt"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	call := func(token string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
		}
		return rr.Body.String()
	}

	respA := call(tokenA)
	respB := call(tokenB)

	if hitsA.Load() != 1 || hitsB.Load() != 1 {
		t.Fatalf("tenant upstream hits = a:%d b:%d, want both 1", hitsA.Load(), hitsB.Load())
	}
	if !strings.Contains(respA, "tenant-a") || !strings.Contains(respB, "tenant-b") {
		t.Fatalf("tenant cache leaked response: a=%s b=%s", respA, respB)
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
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyGrantRequired,
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

func TestLLMV1HandlersChargeMaClawOfficialCredits(t *testing.T) {
	const officialCardServiceGroupID = "redeem"
	tests := []struct {
		name            string
		path            string
		body            map[string]any
		upstreamContent string
		wantBodyParts   []string
	}{
		{
			name: "chat completions",
			path: "/api/llm/v1/chat/completions",
			body: map[string]any{
				"model":    "auto",
				"messages": []any{map[string]any{"role": "user", "content": "official path"}},
			},
			upstreamContent: "official",
			wantBodyParts:   []string{"official"},
		},
		{
			name: "chat completions stream",
			path: "/api/llm/v1/chat/completions",
			body: map[string]any{
				"model":    "auto",
				"stream":   true,
				"messages": []any{map[string]any{"role": "user", "content": "official stream path"}},
			},
			upstreamContent: "official stream",
			wantBodyParts:   []string{"official stream", "data: [DONE]"},
		},
		{
			name:            "responses",
			path:            "/api/llm/v1/responses",
			body:            map[string]any{"model": "auto", "input": "official responses path"},
			upstreamContent: "official responses",
			wantBodyParts:   []string{"official responses"},
		},
		{
			name:            "responses stream",
			path:            "/api/llm/v1/responses",
			body:            map[string]any{"model": "auto", "input": "official responses stream path", "stream": true},
			upstreamContent: "official responses stream",
			wantBodyParts:   []string{"official responses stream", "response.completed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := GetMaClawModule()
			defer SetMaClawModule(previous)

			identity, _, _ := newHTTPAPITestServices(t)
			email := "official-charge-" + strings.ReplaceAll(tt.name, " ", "-") + "@example.com"
			viewerToken, _ := issueViewerToken(t, identity, email)
			ctx := context.Background()
			system := newTestLLMServiceSystemSettings()
			now := time.Now().UTC()
			if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
				TokensPerCredit: 10000,
				ModelServiceGroups: []llmservice.ModelServiceGroup{{
					ID:           officialCardServiceGroupID,
					Name:         "MaClaw Compute",
					AccessPolicy: llmservice.AccessPolicyGrantRequired,
					Models: []llmservice.ModelServiceModel{{
						Name:        "auto",
						ProviderIDs: []string{llmservice.MaClawOfficialProviderID},
					}},
				}},
				Grants: []llmservice.Grant{{
					ID:             "grant-official-" + strings.ReplaceAll(tt.name, " ", "-"),
					Email:          email,
					ServiceGroupID: officialCardServiceGroupID,
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
			if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{}); err != nil {
				t.Fatalf("save provider registry: %v", err)
			}

			var seenTenant string
			var seenAuth string
			var seenRequestID string
			var hubCenterHits atomic.Int32
			hubCenter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hubCenterHits.Add(1)
				if r.URL.Path != "/api/llm/v1/chat/completions" {
					http.NotFound(w, r)
					return
				}
				seenTenant = r.Header.Get("X-Tenant-ID")
				seenAuth = r.Header.Get("Authorization")
				seenRequestID = r.Header.Get("X-MaClaw-Request-ID")
				header := make(http.Header)
				header.Set(llmpool.ProviderIDHeader, "official-provider-a")
				header.Set(llmpool.TokenPricingSnapshotHeader, mustEncodeTokenPricingSnapshot(t, llmpool.TokenPricingSnapshot{
					ProviderID:    "official-provider-a",
					UpstreamModel: "opencode-1",
					InputTokens:   1,
					OutputTokens:  1,
					Pricing: llmpool.ResolvedTokenPricing{TokenPricing: llmpool.TokenPricing{
						InputCreditsPer10K:  1000,
						OutputCreditsPer10K: 1000,
					}},
				}))
				for key, values := range header {
					w.Header()[key] = values
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"id":    "hubcenter-official",
					"model": "auto",
					"choices": []any{map[string]any{
						"index":         0,
						"message":       map[string]any{"role": "assistant", "content": tt.upstreamContent},
						"finish_reason": "stop",
					}},
					"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
				})
			}))
			defer hubCenter.Close()
			SetMaClawModule(&llmservice.MaClawModule{
				Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
					HubCenterURL: hubCenter.URL,
					HubID:        "hub-1",
					MachineToken: "machine-token",
				}),
			})

			globalLLMUsageAccumulator.mu.Lock()
			savedPending := globalLLMUsageAccumulator.pending
			globalLLMUsageAccumulator.pending = map[store.SystemSettingsRepository]*pendingSystemUsage{}
			globalLLMUsageAccumulator.mu.Unlock()
			defer func() {
				globalLLMUsageAccumulator.mu.Lock()
				globalLLMUsageAccumulator.pending = savedPending
				globalLLMUsageAccumulator.mu.Unlock()
			}()

			bodyBytes, err := json.Marshal(tt.body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(bodyBytes))
			req.Header.Set("Authorization", "Bearer "+viewerToken)
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			switch tt.path {
			case "/api/llm/v1/chat/completions":
				LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rr, req)
			case "/api/llm/v1/responses":
				LLMV1ResponsesHandler(identity, system, nil).ServeHTTP(rr, req)
			default:
				t.Fatalf("unsupported path %q", tt.path)
			}

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
			}
			if stream, _ := tt.body["stream"].(bool); stream && !strings.Contains(rr.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatalf("content-type = %q, want text/event-stream", rr.Header().Get("Content-Type"))
			}
			for _, part := range tt.wantBodyParts {
				if !strings.Contains(rr.Body.String(), part) {
					t.Fatalf("response body missing %q: %s", part, rr.Body.String())
				}
			}
			if rr.Header().Get("X-MaClaw-Upstream-Provider") != llmservice.MaClawOfficialProviderID {
				t.Fatalf("unexpected upstream provider header: %q", rr.Header().Get("X-MaClaw-Upstream-Provider"))
			}
			if hubCenterHits.Load() != 1 {
				t.Fatalf("expected HubCenter to be called once, hits = %d", hubCenterHits.Load())
			}
			if seenTenant != store.DefaultTenantID {
				t.Fatalf("X-Tenant-ID = %q, want %q", seenTenant, store.DefaultTenantID)
			}
			if seenAuth != "Bearer machine-token" {
				t.Fatalf("Authorization = %q", seenAuth)
			}
			if !strings.HasPrefix(seenRequestID, "llm_") {
				t.Fatalf("X-MaClaw-Request-ID = %q, want Hub request id", seenRequestID)
			}

			serviceReg, err := llmservice.LoadRegistry(ctx, system)
			if err != nil {
				t.Fatalf("load service registry: %v", err)
			}
			if len(serviceReg.Grants) != 1 || serviceReg.Grants[0].CreditsUsed != 1.2 {
				t.Fatalf("expected official credits to be charged immediately, got %#v", serviceReg.Grants)
			}

			globalLLMUsageAccumulator.flush(ctx)
			providerReg, err := im.LoadLLMProviderRegistry(ctx, system)
			if err != nil {
				t.Fatalf("load provider registry: %v", err)
			}
			stat := providerReg.TokenUsage[llmservice.MaClawOfficialProviderID]
			if stat == nil || stat.TotalTokens != 2 || stat.Requests != 1 {
				t.Fatalf("unexpected official provider usage: %#v", stat)
			}
		})
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
