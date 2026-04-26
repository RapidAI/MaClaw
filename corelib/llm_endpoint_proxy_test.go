package corelib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	var payload map[string]any
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["model"] != "auto" {
		t.Fatalf("response model = %v, want auto", payload["model"])
	}
}

func TestNewLLMEndpointHTTPClientUsesConfiguredTimeout(t *testing.T) {
	client := NewLLMEndpointHTTPClient(MaclawLLMConfig{TimeoutSec: 7})
	if client.Timeout != 7*time.Second {
		t.Fatalf("client.Timeout = %s, want 7s", client.Timeout)
	}
}
