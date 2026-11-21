package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestGossipLowValueModerationFlagsAndHidesTestContent(t *testing.T) {
	env := newGossipTestEnv(t)

	resp := doJSONRequest(t, env.handler, http.MethodPost, "/api/admin/moderation/config", map[string]any{
		"enabled": true,
		"url":     "",
		"api_key": "",
		"model":   "",
	}, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("save moderation config: %d %s", resp.Code, resp.Body.String())
	}
	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/admin/moderation/config", nil, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("load moderation config: %d %s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "api_key") {
		t.Fatalf("moderation config must not return an api key: %s", resp.Body.String())
	}
	loaded := decodeJSON(t, resp.Body.Bytes())
	if _, ok := loaded["service_group_id"]; !ok {
		t.Fatalf("moderation config missing service_group_id: %v", loaded)
	}

	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/admin/moderation/test", map[string]any{
		"content": "test",
	}, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("moderation test: %d %s", resp.Code, resp.Body.String())
	}
	testData := decodeJSON(t, resp.Body.Bytes())
	if flagged, _ := testData["flagged"].(bool); !flagged {
		t.Fatalf("expected test content to be flagged, got %v", testData)
	}

	resp = doJSONRequest(t, env.handler, http.MethodPost, "/api/gossip/publish", map[string]any{
		"machine_id": "low-value-machine",
		"content":    "test",
		"category":   "owner",
	}, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("publish: %d %s", resp.Code, resp.Body.String())
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/gossip/browse?page=1", nil, "")
	if resp.Code != http.StatusOK {
		t.Fatalf("browse: %d %s", resp.Code, resp.Body.String())
	}
	browseData := decodeJSON(t, resp.Body.Bytes())
	if total, _ := browseData["total"].(float64); total != 0 {
		t.Fatalf("expected flagged test content hidden from public browse, got total=%v", total)
	}

	resp = doJSONRequest(t, env.handler, http.MethodGet, "/api/admin/gossip?filter=flagged", nil, env.token)
	if resp.Code != http.StatusOK {
		t.Fatalf("admin flagged list: %d %s", resp.Code, resp.Body.String())
	}
	adminData := decodeJSON(t, resp.Body.Bytes())
	posts, _ := adminData["posts"].([]any)
	if len(posts) != 1 {
		t.Fatalf("expected 1 flagged admin post, got %d", len(posts))
	}
	post, _ := posts[0].(map[string]any)
	if flagged, _ := post["flagged"].(bool); !flagged {
		t.Fatalf("expected admin post flagged=true, got %v", post)
	}
}

func TestLowValueContentRule(t *testing.T) {
	tests := []struct {
		content string
		flagged bool
	}{
		{"test", true},
		{"  TEST!!!  ", true},
		{"123456", true},
		{"aaaaaa", true},
		{"MaClaw finished the overtime coding for me.", false},
	}
	for _, tt := range tests {
		if got := shouldFlagLowValueContent(tt.content); got != tt.flagged {
			t.Fatalf("shouldFlagLowValueContent(%q)=%v, want %v", tt.content, got, tt.flagged)
		}
	}
}

func TestModerationUsesLLMBeforeLowValueFallback(t *testing.T) {
	var systemPrompt, userPrompt string
	called := false
	flagged := moderateContent(t.Context(), &LLMModerationConfig{Enabled: true}, "test", func(_ context.Context, system, user string) (string, error) {
		called = true
		systemPrompt = system
		userPrompt = user
		return "PASS", nil
	})
	if !called {
		t.Fatal("expected moderation to call LLM before applying low-value fallback")
	}
	if systemPrompt == "" || !strings.Contains(userPrompt, "test") {
		t.Fatalf("expected prompt to include content under review, got system=%q user=%q", systemPrompt, userPrompt)
	}
	if !flagged {
		t.Fatal("expected low-value fallback to flag test after LLM PASS")
	}
}

func TestModerationLimitsLLMPromptContent(t *testing.T) {
	longContent := strings.Repeat("x", llmModerationPromptContentRuneLimit+512)
	var prompt string
	_ = moderateContent(t.Context(), &LLMModerationConfig{Enabled: true}, longContent, func(_ context.Context, _, user string) (string, error) {
		prompt = user
		return "PASS", nil
	})
	if !strings.Contains(prompt, "...[truncated]") {
		t.Fatalf("expected truncated marker in prompt")
	}
	if strings.Count(prompt, "x") > llmModerationPromptContentRuneLimit {
		t.Fatalf("prompt included too much content: x count=%d", strings.Count(prompt, "x"))
	}
}

func TestParseModerationAnswer(t *testing.T) {
	tests := []struct {
		answer  string
		flagged bool
	}{
		{"REJECT", true},
		{"REJECT: unsafe", true},
		{"PASS", false},
		{"PASS - not reject", false},
		{"This should reject", true},
	}
	for _, tt := range tests {
		if got := parseModerationAnswer(tt.answer); got != tt.flagged {
			t.Fatalf("parseModerationAnswer(%q)=%v, want %v", tt.answer, got, tt.flagged)
		}
	}
}
