package corelib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestLLMProviderConcurrencyControllerAcquireAndRelease(t *testing.T) {
	controller := NewLLMProviderConcurrencyController()
	release, err := controller.Acquire(context.Background(), "provider-a", 1, 2, 0)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	snap := controller.Snapshot("provider-a", 1, 2, 0)
	if snap.InFlight != 1 || snap.QueueWaiters != 0 || snap.MaxConcurrency != 1 || snap.MaxQueueWaiters != 2 {
		t.Fatalf("unexpected snapshot after acquire: %+v", snap)
	}
	release()
	snap = controller.Snapshot("provider-a", 1, 2, 0)
	if snap.InFlight != 0 || snap.QueueWaiters != 0 {
		t.Fatalf("unexpected snapshot after release: %+v", snap)
	}
}

func TestLLMProviderConcurrencyControllerQueueTimeout(t *testing.T) {
	controller := NewLLMProviderConcurrencyController()
	release, err := controller.Acquire(context.Background(), "provider-a", 1, 2, 30)
	if err != nil {
		t.Fatalf("first Acquire() error = %v", err)
	}
	defer release()

	if _, err := controller.Acquire(context.Background(), "provider-a", 1, 2, 30); err == nil {
		t.Fatal("expected queue timeout error")
	} else if queueErr, ok := err.(*LLMProviderConcurrencyError); !ok || queueErr.Kind != LLMProviderConcurrencyQueueTimeout {
		t.Fatalf("unexpected error kind: %v", err)
	}
}

func TestLLMProviderResilienceControllerCircuitBreakerAndReset(t *testing.T) {
	controller := NewLLMProviderResilienceController()
	provider := LLMEndpointProvider{ID: "provider-a", CircuitBreakerThreshold: 1, CircuitBreakerCooldownMS: 60000, FailureBackoffBaseMS: 1000, FailureBackoffMaxMS: 1000}
	controller.RecordFailure(provider)
	if err := controller.BeforeAttempt(provider); err == nil {
		t.Fatal("expected circuit-open error")
	} else if resilienceErr, ok := err.(*LLMProviderResilienceError); !ok || resilienceErr.Kind != LLMProviderResilienceCircuitOpen {
		t.Fatalf("unexpected resilience error: %v", err)
	}
	snap := controller.Snapshot(provider)
	if !snap.CircuitOpen || snap.ConsecutiveFailures != 1 {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	controller.Reset()
	if err := controller.BeforeAttempt(provider); err != nil {
		t.Fatalf("BeforeAttempt after reset error = %v", err)
	}
}

func TestLLMEndpointProxyForwardProviderRequest(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			writeErrorForTest(w, http.StatusBadGateway, "temporary upstream failure")
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode upstream request: %v", err)
		}
		if body["model"] != "upstream-model" {
			t.Fatalf("upstream model = %v", body["model"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "chatcmpl-test",
			"object": "chat.completion",
			"model":  "upstream-model",
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "ok"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer server.Close()

	proxy := NewLLMEndpointProxy()
	provider := LLMEndpointProvider{ID: "provider-a", APIURL: server.URL, Model: "upstream-model", MaxConcurrency: 1}
	result, err := proxy.ForwardProviderRequest(context.Background(), provider, map[string]any{"model": "auto", "messages": []any{}}, "auto")
	if err != nil {
		t.Fatalf("ForwardProviderRequest() error = %v", err)
	}
	if result.StatusCode != http.StatusOK || result.ProviderID != "provider-a" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Attempts != 2 || hits.Load() != 2 {
		t.Fatalf("expected one retry, attempts=%d hits=%d", result.Attempts, hits.Load())
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["model"] != "auto" {
		t.Fatalf("response model = %v, want auto", payload["model"])
	}
}

func writeErrorForTest(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": message}})
}

func TestNewLLMEndpointHTTPClientUsesConfiguredTimeout(t *testing.T) {
	client := NewLLMEndpointHTTPClient(MaclawLLMConfig{TimeoutSec: 300})
	if client.Timeout != 300*time.Second {
		t.Fatalf("client.Timeout = %s, want 300s", client.Timeout)
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
	}
	if transport.ResponseHeaderTimeout != 300*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %s, want 300s", transport.ResponseHeaderTimeout)
	}
	if transport.MaxIdleConnsPerHost < 100 || transport.MaxConnsPerHost < 100 {
		t.Fatalf("transport pool too small: MaxIdleConnsPerHost=%d MaxConnsPerHost=%d", transport.MaxIdleConnsPerHost, transport.MaxConnsPerHost)
	}
}

func TestNewLLMEndpointHTTPClientClampsTimeout(t *testing.T) {
	client := NewLLMEndpointHTTPClient(MaclawLLMConfig{TimeoutSec: 120})
	if client.Timeout != time.Duration(MinAgentTimeoutSec)*time.Second {
		t.Fatalf("client.Timeout = %s, want %ds", client.Timeout, MinAgentTimeoutSec)
	}
	client = NewLLMEndpointHTTPClient(MaclawLLMConfig{TimeoutSec: 900})
	if client.Timeout != time.Duration(MaxAgentTimeoutSec)*time.Second {
		t.Fatalf("client.Timeout = %s, want %ds", client.Timeout, MaxAgentTimeoutSec)
	}
	client = NewLLMEndpointHTTPClient(MaclawLLMConfig{})
	if client.Timeout != time.Duration(DefaultLLMTimeoutSec)*time.Second {
		t.Fatalf("client.Timeout = %s, want %ds", client.Timeout, DefaultLLMTimeoutSec)
	}
}

func TestLLMEndpointProxyReusesHTTPClientByTimeout(t *testing.T) {
	proxy := NewLLMEndpointProxy()
	cfg := MaclawLLMConfig{TimeoutSec: 360}
	first := proxy.cachedHTTPClient(cfg)
	second := proxy.cachedHTTPClient(cfg)
	if first == nil || first != second {
		t.Fatalf("expected cachedHTTPClient to reuse client: %p vs %p", first, second)
	}
}

func TestBoundedSubTimeoutNeverExceedsTotal(t *testing.T) {
	if got := boundedSubTimeout(2*time.Second, 5*time.Second, 30*time.Second); got != 2*time.Second {
		t.Fatalf("boundedSubTimeout short total = %s, want 2s", got)
	}
	if got := boundedSubTimeout(10*time.Minute, 5*time.Second, 30*time.Second); got != 30*time.Second {
		t.Fatalf("boundedSubTimeout long total = %s, want 30s", got)
	}
}

func TestForwardOpenAICompatRequestWithRetryRespectsCallerDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writeErrorForTest(w, http.StatusBadGateway, "slow failure")
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, attempts, err := ForwardOpenAICompatRequestWithRetry(ctx, MaclawLLMConfig{URL: server.URL, Model: "upstream-model", TimeoutSec: 10}, map[string]interface{}{"messages": []interface{}{}}, server.Client(), "auto")
	if err == nil {
		t.Fatal("expected deadline error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 because caller deadline expired", attempts)
	}
}
