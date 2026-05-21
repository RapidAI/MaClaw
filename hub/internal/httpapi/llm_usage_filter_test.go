package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestRemoteCodingToolProviderIDMatchingIsCaseAndSpaceInsensitive(t *testing.T) {
	for _, providerID := range []string{
		" Codex:gpt-5.4 ",
		"CLAUDE:sonnet",
		"Gemini:2.5-pro",
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
	}, nil)

	if len(resp.Providers) != 2 {
		t.Fatalf("providers = %#v", resp.Providers)
	}
	remoteProvider, ok := resp.Providers[0].(map[string]any)
	if !ok {
		t.Fatalf("provider payload = %#v", resp.Providers[0])
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
	UpdateLLMProvidersHandler(system).ServeHTTP(rr, req)
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
