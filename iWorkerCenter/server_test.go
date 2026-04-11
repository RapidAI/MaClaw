package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setCenterTestHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestNewCenterServerLoadsDefaultProviders(t *testing.T) {
	setCenterTestHome(t)

	server := newCenterServer(":0")
	if len(server.providers) != 2 {
		t.Fatalf("providers len = %d, want 2", len(server.providers))
	}
	if server.providers[0].ID != "office-openai" {
		t.Fatalf("first provider = %q, want office-openai", server.providers[0].ID)
	}
}

func TestLoadCenterSettingsReturnsDefaultsWhenFileMissing(t *testing.T) {
	setCenterTestHome(t)

	settings, err := readCenterSettings()
	if err != nil {
		t.Fatalf("readCenterSettings returned error: %v", err)
	}
	if len(settings.Providers) != 2 {
		t.Fatalf("providers len = %d, want 2", len(settings.Providers))
	}
}

func TestWriteCenterSettingsRoundTrip(t *testing.T) {
	home := setCenterTestHome(t)
	settings := centerSettingsFile{
		Providers: []centerProviderFile{{
			ID:          "custom-openai",
			Name:        "自定义服务",
			Protocol:    "",
			BaseURL:     "http://127.0.0.1:9000/",
			APIKey:      "token-a",
			Model:       "gpt-test",
			Priority:    77,
			Features:    nil,
			Description: "自定义 provider",
			Enabled:     true,
			TimeoutSec:  0,
		}},
	}

	if err := writeCenterSettings(settings); err != nil {
		t.Fatalf("writeCenterSettings returned error: %v", err)
	}

	path := filepath.Join(home, ".iworkercenter", "settings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file not written: %v", err)
	}

	got, err := readCenterSettings()
	if err != nil {
		t.Fatalf("readCenterSettings returned error: %v", err)
	}
	if len(got.Providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(got.Providers))
	}
	if got.Providers[0].Protocol != "openai" || got.Providers[0].BaseURL != "http://127.0.0.1:9000" {
		t.Fatalf("provider = %+v, want normalized values", got.Providers[0])
	}
	if got.Providers[0].TimeoutSec != 60 {
		t.Fatalf("TimeoutSec = %d, want 60", got.Providers[0].TimeoutSec)
	}
	if got.Providers[0].Features == nil {
		t.Fatalf("Features should be normalized to empty slice")
	}
}

func TestLoadCenterProvidersReadsSettingsFile(t *testing.T) {
	home := setCenterTestHome(t)

	path := filepath.Join(home, ".iworkercenter", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	data := []byte(`{"providers":[{"id":"custom-openai","name":"自定义服务","protocol":"openai","base_url":"http://127.0.0.1:9000/","api_key":"token-a","model":"gpt-test","priority":77,"features":["表格"],"description":"自定义 provider","enabled":true,"timeout_sec":25}]}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	providers := loadCenterProviders()
	if len(providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(providers))
	}
	if providers[0].ID != "custom-openai" || providers[0].BaseURL != "http://127.0.0.1:9000" {
		t.Fatalf("provider = %+v, want normalized custom provider", providers[0])
	}
	if providers[0].TimeoutSec != 25 {
		t.Fatalf("TimeoutSec = %d, want 25", providers[0].TimeoutSec)
	}
}

func TestCenterStatusSnapshot(t *testing.T) {
	home := setCenterTestHome(t)
	status, err := centerStatusSnapshot()
	if err != nil {
		t.Fatalf("centerStatusSnapshot returned error: %v", err)
	}
	if status.Status != "ok" {
		t.Fatalf("Status = %q, want ok", status.Status)
	}
	if status.ProviderCount != 2 {
		t.Fatalf("ProviderCount = %d, want 2", status.ProviderCount)
	}
	wantPath := filepath.Join(home, ".iworkercenter", "settings.json")
	if status.ConfigPath != wantPath {
		t.Fatalf("ConfigPath = %q, want %q", status.ConfigPath, wantPath)
	}
}

func TestHandleHealthReturnsStatusSnapshot(t *testing.T) {
	home := setCenterTestHome(t)
	server := newCenterServer(":0")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Status        string `json:"status"`
		ProviderCount int    `json:"provider_count"`
		ConfigPath    string `json:"config_path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("Status = %q, want ok", body.Status)
	}
	if body.ProviderCount != 2 {
		t.Fatalf("ProviderCount = %d, want 2", body.ProviderCount)
	}
	wantPath := filepath.Join(home, ".iworkercenter", "settings.json")
	if body.ConfigPath != wantPath {
		t.Fatalf("ConfigPath = %q, want %q", body.ConfigPath, wantPath)
	}
}

func TestPickProviderByModelHint(t *testing.T) {
	setCenterTestHome(t)
	server := newCenterServer(":0")
	provider := server.pickProvider(openAIChatRequest{Model: "analysis-anthropic"})
	if provider == nil {
		t.Fatalf("pickProvider returned nil")
	}
	if provider.ID != "analysis-anthropic" {
		t.Fatalf("provider.ID = %q, want analysis-anthropic", provider.ID)
	}
}

func TestPickProviderByFeatureMatch(t *testing.T) {
	setCenterTestHome(t)
	server := newCenterServer(":0")
	provider := server.pickProvider(openAIChatRequest{
		Messages: []openAIChatMessage{{Role: "user", Content: "请帮我整理会议纪要和正式通知"}},
	})
	if provider == nil {
		t.Fatalf("pickProvider returned nil")
	}
	if provider.ID != "office-openai" {
		t.Fatalf("provider.ID = %q, want office-openai", provider.ID)
	}
}

func TestHandleChatCompletionsFallsBackToNextProvider(t *testing.T) {
	setCenterTestHome(t)
	server := newCenterServer(":0")
	attempted := make([]string, 0, 2)
	server.forward = func(_ context.Context, provider CenterProvider, _ openAIChatRequest) ([]byte, error) {
		attempted = append(attempted, provider.ID)
		if provider.ID == "office-openai" {
			return nil, errors.New("primary provider failed")
		}
		return []byte(`{"id":"ok","object":"chat.completion","created":1,"model":"analysis-anthropic","choices":[{"index":0,"message":{"role":"assistant","content":"分析结果"},"finish_reason":"stop"}]}`), nil
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"请帮我整理会议纪要和正式通知"}]}`))
	rec := httptest.NewRecorder()

	server.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(attempted) != 2 {
		t.Fatalf("attempted = %v, want 2 providers", attempted)
	}
	if attempted[0] != "office-openai" || attempted[1] != "analysis-anthropic" {
		t.Fatalf("attempted = %v, want [office-openai analysis-anthropic]", attempted)
	}
	if !strings.Contains(rec.Body.String(), "分析结果") {
		t.Fatalf("response body = %s, want fallback response", rec.Body.String())
	}
}

func TestBuildAnthropicFallbackBody(t *testing.T) {
	body := []byte(`{"content":[{"type":"text","text":"分析结果"}]}`)
	converted, err := convertAnthropicBodyDirect(body, "analysis-anthropic")
	if err != nil {
		t.Fatalf("convertAnthropicBodyDirect returned error: %v", err)
	}
	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(converted, &parsed); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if parsed.Model != "analysis-anthropic" {
		t.Fatalf("Model = %q, want analysis-anthropic", parsed.Model)
	}
	if len(parsed.Choices) != 1 || parsed.Choices[0].Message.Content != "分析结果" {
		t.Fatalf("unexpected choices: %+v", parsed.Choices)
	}
}
