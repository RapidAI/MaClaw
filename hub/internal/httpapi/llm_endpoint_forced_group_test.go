package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestLLMEndpointForcedServiceGroupStaysOnTheViewer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/llm/v1/chat/completions", nil)
	req.Header.Set(llmpool.ServiceGroupIDHeader, "System-Free")
	group, forced := llmEndpointForcedServiceGroup(context.Background(), req)
	if !forced || group != llmservice.SystemFreeServiceGroupID {
		t.Fatalf("system-free header group=%q forced=%v", group, forced)
	}

	req.Header.Set(llmpool.ServiceGroupIDHeader, "paid-group")
	if group, forced = llmEndpointForcedServiceGroup(context.Background(), req); forced || group != "" {
		t.Fatalf("paid header group=%q forced=%v", group, forced)
	}
	if group, forced = llmEndpointForcedServiceGroup(context.Background(), nil); forced || group != "" {
		t.Fatalf("nil request group=%q forced=%v", group, forced)
	}

	ctx := withLLMEndpointAPIKeyAuth(context.Background(), llmEndpointAPIKeyAuth{
		KeyID:          "key-1",
		ServiceGroupID: "paid-group",
	})
	req.Header.Set(llmpool.ServiceGroupIDHeader, llmservice.SystemFreeServiceGroupID)
	group, forced = llmEndpointForcedServiceGroup(ctx, req)
	if !forced || group != "paid-group" {
		t.Fatalf("api key group=%q forced=%v", group, forced)
	}
}

func TestHubPublicLLMEndpoint(t *testing.T) {
	if got := hubPublicLLMEndpoint("  https://hub.example/ "); got != "https://hub.example/api/llm/v1" {
		t.Fatalf("endpoint = %q", got)
	}
	if got := hubPublicLLMEndpoint("https://hub.example/api/llm/v1/"); got != "https://hub.example/api/llm/v1" {
		t.Fatalf("already llm endpoint = %q", got)
	}
	if got := hubPublicLLMEndpoint("  "); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := (botOwnerLLM{}).PublicLLMBaseURL(context.Background()); got != "" {
		t.Fatalf("nil center = %q", got)
	}
}
