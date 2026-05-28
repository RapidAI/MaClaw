package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	var called bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected system and user messages, got %#v", req.Messages)
		}
		if req.Messages[0].Content == "" || !strings.Contains(req.Messages[1].Content, "test") {
			t.Fatalf("expected prompt to include content under review, got %#v", req.Messages)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"content": "PASS"},
			}},
		})
	}))
	defer server.Close()

	flagged := moderateContent(t.Context(), &LLMModerationConfig{
		Enabled:   true,
		URL:       server.URL,
		APIKey:    "test-key",
		ModelName: "test-model",
	}, "test")
	if !called {
		t.Fatal("expected moderation to call LLM before applying low-value fallback")
	}
	if !flagged {
		t.Fatal("expected low-value fallback to flag test after LLM PASS")
	}
}

func TestModerationLimitsLLMPromptContent(t *testing.T) {
	longContent := strings.Repeat("x", llmModerationPromptContentRuneLimit+512)
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected system and user messages, got %#v", req.Messages)
		}
		prompt = req.Messages[1].Content
		writeJSON(w, http.StatusOK, map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "PASS"}}},
		})
	}))
	defer server.Close()

	_ = moderateContent(t.Context(), &LLMModerationConfig{Enabled: true, URL: server.URL, APIKey: "test-key", ModelName: "test-model"}, longContent)
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
