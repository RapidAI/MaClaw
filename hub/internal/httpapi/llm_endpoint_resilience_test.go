package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestLLMEndpointUserRateLimitCanBeTenantScoped(t *testing.T) {
	limiter := newLLMEndpointUserLimiter()
	reg := &im.LLMProviderRegistry{UserRateLimitPerMinute: 1, UserRateLimitBurst: 1}
	if !limiter.allowForRegistry("tenant_a\x00same@example.com", reg) {
		t.Fatal("first tenant_a request should be allowed")
	}
	if limiter.allowForRegistry("tenant_a\x00same@example.com", reg) {
		t.Fatal("second tenant_a request should be rate limited")
	}
	if !limiter.allowForRegistry("tenant_b\x00same@example.com", reg) {
		t.Fatal("tenant_b should have an independent rate bucket")
	}
}
func TestLLMV1ChatCompletionsHandlerUserRateLimit(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "ratelimit@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	globalLLMEndpointUserLimiter.reset()
	globalProviderResilience.reset()
	defer globalLLMEndpointUserLimiter.reset()
	defer globalProviderResilience.reset()
	invalidateLLMRuntimeCaches(system)

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		UserBindings: []llmservice.UserBinding{{Email: "ratelimit@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":    "upstream",
			"model": "auto",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		UserRateLimitPerMinute: 1,
		UserRateLimitBurst:     1,
		Providers:              []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	handler := LLMV1ChatCompletionsHandler(identity, system, nil)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusOK {
			t.Fatalf("first request status = %d, body = %s", rec.Code, rec.Body.String())
		}
		if i == 1 {
			if rec.Code != http.StatusTooManyRequests {
				t.Fatalf("second request status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte("LLM_ENDPOINT_USER_RATE_LIMITED")) {
				t.Fatalf("expected user rate limit error, body = %s", rec.Body.String())
			}
		}
	}
}

func TestForwardAuthorizedModelRequestWithCacheProviderCircuitBreakerAndRecovery(t *testing.T) {
	globalProviderResilience.reset()
	defer globalProviderResilience.reset()

	var providerAHits atomic.Int32
	var providerAShouldFail atomic.Bool
	providerAShouldFail.Store(true)
	providerA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerAHits.Add(1)
		if providerAShouldFail.Load() {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": "boom"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "provider-a",
			"model":   "auto",
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "from-a"}}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
		})
	}))
	defer providerA.Close()

	var providerBHits atomic.Int32
	providerB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerBHits.Add(1)
		writeJSON(w, http.StatusOK, map[string]any{
			"id":      "provider-b",
			"model":   "auto",
			"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "from-b"}}},
			"usage":   map[string]any{"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3},
		})
	}))
	defer providerB.Close()

	reg := &im.LLMProviderRegistry{Providers: []im.LLMProvider{
		{
			ID:                       "provider-a",
			Name:                     "Provider A",
			APIURL:                   providerA.URL,
			Model:                    "model-a",
			CircuitBreakerThreshold:  1,
			CircuitBreakerCooldownMS: 60000,
			FailureBackoffBaseMS:     1000,
			FailureBackoffMaxMS:      1000,
		},
		{
			ID:                       "provider-b",
			Name:                     "Provider B",
			APIURL:                   providerB.URL,
			Model:                    "model-b",
			CircuitBreakerThreshold:  3,
			CircuitBreakerCooldownMS: 60000,
			FailureBackoffBaseMS:     100,
			FailureBackoffMaxMS:      500,
		},
	}}
	model := &llmservice.AuthorizedModel{Name: "auto", ProviderIDs: []string{"provider-a", "provider-b"}}
	body := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hello"}}}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader([]byte(`{"model":"auto"}`)))

	respBody, statusCode, providerID, _, _, _, err := forwardAuthorizedModelRequestWithCache(req, reg, model, body, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("first request error = %v", err)
	}
	if statusCode != http.StatusOK || providerID != "provider-b" || !bytes.Contains(respBody, []byte("from-b")) {
		t.Fatalf("first request routed to %q status=%d body=%s", providerID, statusCode, string(respBody))
	}
	if providerAHits.Load() != 3 || providerBHits.Load() != 1 {
		t.Fatalf("unexpected hit counts after failover: a=%d b=%d", providerAHits.Load(), providerBHits.Load())
	}

	respBody, statusCode, providerID, _, _, _, err = forwardAuthorizedModelRequestWithCache(req, reg, model, body, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("second request error = %v", err)
	}
	if providerID != "provider-b" || !bytes.Contains(respBody, []byte("from-b")) {
		t.Fatalf("second request routed to %q body=%s", providerID, string(respBody))
	}
	if providerAHits.Load() != 3 {
		t.Fatalf("provider-a should have been skipped while circuit open, hits=%d", providerAHits.Load())
	}

	providerAShouldFail.Store(false)
	globalProviderResilience.reset()

	respBody, statusCode, providerID, _, _, _, err = forwardAuthorizedModelRequestWithCache(req, reg, model, body, "auto", nil, defaultHubLLMPromptCacheConfig())
	if err != nil {
		t.Fatalf("third request error = %v", err)
	}
	if statusCode != http.StatusOK || providerID != "provider-a" || !bytes.Contains(respBody, []byte("from-a")) {
		t.Fatalf("third request routed to %q status=%d body=%s", providerID, statusCode, string(respBody))
	}
	snapshot := globalProviderResilience.snapshot(reg.FindProvider("provider-a"))
	if snapshot.ConsecutiveFailures != 0 || snapshot.CircuitOpen {
		t.Fatalf("provider-a resilience did not recover: %+v", snapshot)
	}
}

func TestLLMV1ChatCompletionsHandlerWrapsFinalUpstream500AsBadGateway(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "upstream500@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	globalProviderResilience.reset()
	defer globalProviderResilience.reset()
	invalidateLLMRuntimeCaches(system)

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           "coding-basic",
			Name:         "Coding Basic",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{"provider-a"}}},
		}},
		UserBindings: []llmservice.UserBinding{{Email: "upstream500@example.com", ServiceGroupIDs: []string{"coding-basic"}}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"message": "boom"}})
	}))
	defer server.Close()

	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{
		Providers: []im.LLMProvider{{ID: "provider-a", APIURL: server.URL, Model: "test-model"}},
	}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}
	invalidateLLMRuntimeCaches(system)

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502, body = %s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("LLM_UPSTREAM_FAILED")) {
		t.Fatalf("expected wrapped upstream failure, body = %s", rec.Body.String())
	}
}
