package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
)

func TestHubLLMStatusIncludesPromptCacheRate(t *testing.T) {
	settings := &testSystemSettingsRepo{}
	reg := &im.LLMProviderRegistry{
		TokenUsage: map[string]*corelib.TokenUsageStat{
			"provider-a": {
				InputTokens:       100,
				CachedInputTokens: 40,
				CacheWriteTokens:  10,
				Requests:          4,
				CachedRequests:    1,
			},
			"provider-b": {
				InputTokens:       300,
				CachedInputTokens: 160,
				CacheWriteTokens:  20,
				Requests:          6,
				CachedRequests:    3,
			},
		},
	}
	if err := im.SaveLLMProviderRegistry(nil, settings, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/hub_llm_status", nil)
	rr := httptest.NewRecorder()
	HubLLMStatusHandler(func() string { return "healthy" }, settings).ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		Status      string `json:"status"`
		PromptCache struct {
			InputTokens       int64   `json:"input_tokens"`
			CachedInputTokens int64   `json:"cached_input_tokens"`
			CacheWriteTokens  int64   `json:"cache_write_tokens"`
			Requests          int64   `json:"requests"`
			CachedRequests    int64   `json:"cached_requests"`
			CacheRate         float64 `json:"cache_rate"`
			CacheReuseRate    float64 `json:"cache_reuse_rate"`
		} `json:"prompt_cache"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "healthy" {
		t.Fatalf("status = %q", body.Status)
	}
	if body.PromptCache.CachedRequests != 4 || body.PromptCache.Requests != 10 {
		t.Fatalf("cache requests = %d/%d", body.PromptCache.CachedRequests, body.PromptCache.Requests)
	}
	if body.PromptCache.CacheRate != 40 {
		t.Fatalf("cache rate = %v, want 40", body.PromptCache.CacheRate)
	}
	if body.PromptCache.CachedInputTokens != 200 || body.PromptCache.InputTokens != 400 {
		t.Fatalf("cache tokens = %d/%d", body.PromptCache.CachedInputTokens, body.PromptCache.InputTokens)
	}
	if body.PromptCache.CacheReuseRate != 50 {
		t.Fatalf("cache reuse rate = %v, want 50", body.PromptCache.CacheReuseRate)
	}
	if body.PromptCache.CacheWriteTokens != 30 {
		t.Fatalf("cache write tokens = %d, want 30", body.PromptCache.CacheWriteTokens)
	}
}
