package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
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
