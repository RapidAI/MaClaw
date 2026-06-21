package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if strings.TrimSpace(fmt.Sprint(payload["request_id"])) == "" {
		t.Fatalf("missing request_id in body = %s", rec.Body.String())
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["failure_stage"])); got != "upstream_provider" {
		t.Fatalf("failure_stage = %q, want upstream_provider; body=%s", got, rec.Body.String())
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["provider_id"])); got != "provider-a" {
		t.Fatalf("provider_id = %q, want provider-a; body=%s", got, rec.Body.String())
	}
	if got := int(payload["upstream_status"].(float64)); got != http.StatusInternalServerError {
		t.Fatalf("upstream_status = %d, want 500; body=%s", got, rec.Body.String())
	}
	if strings.TrimSpace(fmt.Sprint(payload["upstream_host"])) == "" {
		t.Fatalf("missing upstream_host in body = %s", rec.Body.String())
	}
	if rec.Header().Get("X-MaClaw-Request-ID") != payload["request_id"] {
		t.Fatalf("request id header/body mismatch: header=%q body=%v", rec.Header().Get("X-MaClaw-Request-ID"), payload["request_id"])
	}
}

func TestLLMV1ChatCompletionsHandlerReportsMaClawOfficialHubCenterHost(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "official-upstream@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	globalProviderResilience.reset()
	defer globalProviderResilience.reset()
	invalidateLLMRuntimeCaches(system)

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           llmservice.MaClawOfficialServiceGroupID,
			Name:         "MaClaw Official",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}},
		}},
		UserBindings: []llmservice.UserBinding{{Email: "official-upstream@example.com", ServiceGroupIDs: []string{llmservice.MaClawOfficialServiceGroupID}}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	hubCenter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": map[string]any{"message": "deepseek timeout"}})
	}))
	defer hubCenter.Close()
	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: hubCenter.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "messages": []any{map[string]any{"role": "user", "content": "hello"}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504, body = %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	parsed, err := url.Parse(hubCenter.URL)
	if err != nil {
		t.Fatalf("parse hubcenter URL: %v", err)
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["provider_id"])); got != llmservice.MaClawOfficialProviderID {
		t.Fatalf("provider_id = %q, want %q; body=%s", got, llmservice.MaClawOfficialProviderID, rec.Body.String())
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["failure_stage"])); got != "upstream_provider" {
		t.Fatalf("failure_stage = %q, want upstream_provider; body=%s", got, rec.Body.String())
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["upstream_host"])); got != parsed.Host {
		t.Fatalf("upstream_host = %q, want HubCenter host %q; body=%s", got, parsed.Host, rec.Body.String())
	}
	if got := int(payload["upstream_status"].(float64)); got != http.StatusGatewayTimeout {
		t.Fatalf("upstream_status = %d, want 504; body=%s", got, rec.Body.String())
	}
	if rec.Header().Get("X-MaClaw-Request-ID") != payload["request_id"] {
		t.Fatalf("request id header/body mismatch: header=%q body=%v", rec.Header().Get("X-MaClaw-Request-ID"), payload["request_id"])
	}
}

func TestLLMV1ResponsesHandlerReportsMaClawOfficialHubCenterHost(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, _ := issueViewerToken(t, identity, "official-responses-upstream@example.com")
	ctx := context.Background()
	system := newTestLLMServiceSystemSettings()
	globalProviderResilience.reset()
	defer globalProviderResilience.reset()
	invalidateLLMRuntimeCaches(system)

	if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{
			ID:           llmservice.MaClawOfficialServiceGroupID,
			Name:         "MaClaw Official",
			AccessPolicy: llmservice.AccessPolicyFree,
			Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}},
		}},
		UserBindings: []llmservice.UserBinding{{Email: "official-responses-upstream@example.com", ServiceGroupIDs: []string{llmservice.MaClawOfficialServiceGroupID}}},
	}); err != nil {
		t.Fatalf("save service registry: %v", err)
	}
	if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{}); err != nil {
		t.Fatalf("save provider registry: %v", err)
	}

	hubCenter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/llm/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"message": "deepseek unavailable"}})
	}))
	defer hubCenter.Close()
	SetMaClawModule(&llmservice.MaClawModule{
		Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
			HubCenterURL: hubCenter.URL,
			HubID:        "hub-1",
			MachineToken: "machine-token",
		}),
	})

	bodyBytes, err := json.Marshal(map[string]any{"model": "auto", "input": "hello"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/responses", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	LLMV1ResponsesHandler(identity, system, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body = %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	parsed, err := url.Parse(hubCenter.URL)
	if err != nil {
		t.Fatalf("parse hubcenter URL: %v", err)
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["provider_id"])); got != llmservice.MaClawOfficialProviderID {
		t.Fatalf("provider_id = %q, want %q; body=%s", got, llmservice.MaClawOfficialProviderID, rec.Body.String())
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["failure_stage"])); got != "upstream_provider" {
		t.Fatalf("failure_stage = %q, want upstream_provider; body=%s", got, rec.Body.String())
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["upstream_host"])); got != parsed.Host {
		t.Fatalf("upstream_host = %q, want HubCenter host %q; body=%s", got, parsed.Host, rec.Body.String())
	}
	if got := int(payload["upstream_status"].(float64)); got != http.StatusServiceUnavailable {
		t.Fatalf("upstream_status = %d, want 503; body=%s", got, rec.Body.String())
	}
	if got := strings.TrimSpace(fmt.Sprint(payload["wire_api"])); got != "responses" {
		t.Fatalf("wire_api = %q, want responses; body=%s", got, rec.Body.String())
	}
	if rec.Header().Get("X-MaClaw-Request-ID") != payload["request_id"] {
		t.Fatalf("request id header/body mismatch: header=%q body=%v", rec.Header().Get("X-MaClaw-Request-ID"), payload["request_id"])
	}
}

func TestLLMStreamHandlersReportMaClawOfficialHubCenterHostBeforeStreamStarts(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		body    map[string]any
		wireAPI string
	}{
		{
			name: "chat stream",
			path: "/api/llm/v1/chat/completions",
			body: map[string]any{
				"model":    "auto",
				"stream":   true,
				"messages": []any{map[string]any{"role": "user", "content": "hello"}},
			},
		},
		{
			name:    "responses stream",
			path:    "/api/llm/v1/responses",
			body:    map[string]any{"model": "auto", "input": "hello", "stream": true},
			wireAPI: "responses",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := GetMaClawModule()
			defer SetMaClawModule(previous)

			identity, _, _ := newHTTPAPITestServices(t)
			email := "official-" + strings.ReplaceAll(tt.name, " ", "-") + "@example.com"
			viewerToken, _ := issueViewerToken(t, identity, email)
			ctx := context.Background()
			system := newTestLLMServiceSystemSettings()
			globalProviderResilience.reset()
			defer globalProviderResilience.reset()
			invalidateLLMRuntimeCaches(system)

			if err := llmservice.SaveRegistry(ctx, system, &llmservice.Registry{
				ModelServiceGroups: []llmservice.ModelServiceGroup{{
					ID:           llmservice.MaClawOfficialServiceGroupID,
					Name:         "MaClaw Official",
					AccessPolicy: llmservice.AccessPolicyFree,
					Models:       []llmservice.ModelServiceModel{{Name: "auto", ProviderIDs: []string{llmservice.MaClawOfficialProviderID}}},
				}},
				UserBindings: []llmservice.UserBinding{{Email: email, ServiceGroupIDs: []string{llmservice.MaClawOfficialServiceGroupID}}},
			}); err != nil {
				t.Fatalf("save service registry: %v", err)
			}
			if err := im.SaveLLMProviderRegistry(ctx, system, &im.LLMProviderRegistry{}); err != nil {
				t.Fatalf("save provider registry: %v", err)
			}

			hubCenter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/llm/v1/chat/completions" {
					http.NotFound(w, r)
					return
				}
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"message": "stream upstream unavailable"}})
			}))
			defer hubCenter.Close()
			SetMaClawModule(&llmservice.MaClawModule{
				Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
					HubCenterURL: hubCenter.URL,
					HubID:        "hub-1",
					MachineToken: "machine-token",
				}),
			})

			bodyBytes, err := json.Marshal(tt.body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			req := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewReader(bodyBytes))
			req.Header.Set("Authorization", "Bearer "+viewerToken)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			switch tt.path {
			case "/api/llm/v1/chat/completions":
				LLMV1ChatCompletionsHandler(identity, system, nil).ServeHTTP(rec, req)
			case "/api/llm/v1/responses":
				LLMV1ResponsesHandler(identity, system, nil).ServeHTTP(rec, req)
			default:
				t.Fatalf("unsupported path %q", tt.path)
			}

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503, body = %s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
				t.Fatalf("stream should not start before upstream failure is wrapped; content-type=%q body=%s", rec.Header().Get("Content-Type"), rec.Body.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			parsed, err := url.Parse(hubCenter.URL)
			if err != nil {
				t.Fatalf("parse hubcenter URL: %v", err)
			}
			if got := strings.TrimSpace(fmt.Sprint(payload["provider_id"])); got != llmservice.MaClawOfficialProviderID {
				t.Fatalf("provider_id = %q, want %q; body=%s", got, llmservice.MaClawOfficialProviderID, rec.Body.String())
			}
			if got := strings.TrimSpace(fmt.Sprint(payload["failure_stage"])); got != "upstream_provider" {
				t.Fatalf("failure_stage = %q, want upstream_provider; body=%s", got, rec.Body.String())
			}
			if got := strings.TrimSpace(fmt.Sprint(payload["upstream_host"])); got != parsed.Host {
				t.Fatalf("upstream_host = %q, want HubCenter host %q; body=%s", got, parsed.Host, rec.Body.String())
			}
			if got := int(payload["upstream_status"].(float64)); got != http.StatusServiceUnavailable {
				t.Fatalf("upstream_status = %d, want 503; body=%s", got, rec.Body.String())
			}
			if tt.wireAPI != "" {
				if got := strings.TrimSpace(fmt.Sprint(payload["wire_api"])); got != tt.wireAPI {
					t.Fatalf("wire_api = %q, want %q; body=%s", got, tt.wireAPI, rec.Body.String())
				}
			}
			if rec.Header().Get("X-MaClaw-Request-ID") != payload["request_id"] {
				t.Fatalf("request id header/body mismatch: header=%q body=%v", rec.Header().Get("X-MaClaw-Request-ID"), payload["request_id"])
			}
		})
	}
}
