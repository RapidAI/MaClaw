package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// MaclawLLMProvider represents a single LLM provider entry for MaClaw.
type MaclawLLMProvider struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
	Protocol string `json:"protocol,omitempty"` // "openai" (default) or "anthropic"
	IsCustom bool   `json:"is_custom,omitempty"`
}

// MaclawLLMConfig is the LLM configuration for the MaClaw desktop agent.
type MaclawLLMConfig struct {
	URL      string `json:"url"`
	Key      string `json:"key"`
	Model    string `json:"model"`
	Protocol string `json:"protocol,omitempty"` // "openai" (default) or "anthropic"
}

// defaultMaclawLLMProviders returns the built-in provider list.
func defaultMaclawLLMProviders() []MaclawLLMProvider {
	return []MaclawLLMProvider{
		{Name: "智谱", URL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5-turbo"},
		{Name: "Custom1", URL: "", Model: "", IsCustom: true},
		{Name: "Custom2", URL: "", Model: "", IsCustom: true},
	}
}

// GetMaclawLLMProviders returns the provider list and current selection.
func (a *App) GetMaclawLLMProviders() struct {
	Providers []MaclawLLMProvider `json:"providers"`
	Current   string              `json:"current"`
} {
	cfg, err := a.LoadConfig()
	if err != nil {
		defaults := defaultMaclawLLMProviders()
		return struct {
			Providers []MaclawLLMProvider `json:"providers"`
			Current   string              `json:"current"`
		}{Providers: defaults, Current: defaults[0].Name}
	}
	providers := cfg.MaclawLLMProviders
	if len(providers) == 0 {
		providers = defaultMaclawLLMProviders()
		// Migrate legacy single-config if present
		if strings.TrimSpace(cfg.MaclawLLMUrl) != "" {
			providers[0].URL = cfg.MaclawLLMUrl
			providers[0].Key = cfg.MaclawLLMKey
			providers[0].Model = cfg.MaclawLLMModel
		}
	}
	current := cfg.MaclawLLMCurrentProvider
	if current == "" {
		current = providers[0].Name
	}
	return struct {
		Providers []MaclawLLMProvider `json:"providers"`
		Current   string              `json:"current"`
	}{Providers: providers, Current: current}
}

// SaveMaclawLLMProviders persists the provider list and current selection.
func (a *App) SaveMaclawLLMProviders(providers []MaclawLLMProvider, current string) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.MaclawLLMProviders = providers
	cfg.MaclawLLMCurrentProvider = current
	// Sync legacy fields from the current provider for backward compatibility
	cfg.MaclawLLMUrl = ""
	cfg.MaclawLLMKey = ""
	cfg.MaclawLLMModel = ""
	cfg.MaclawLLMProtocol = ""
	for _, p := range providers {
		if p.Name == current {
			cfg.MaclawLLMUrl = strings.TrimRight(strings.TrimSpace(p.URL), "/")
			cfg.MaclawLLMKey = strings.TrimSpace(p.Key)
			cfg.MaclawLLMModel = strings.TrimSpace(p.Model)
			cfg.MaclawLLMProtocol = p.Protocol
			break
		}
	}
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	// Immediately notify Hub of the LLM configuration change via heartbeat
	// so the Hub-side llm_configured flag is updated without waiting for the
	// next periodic heartbeat cycle.
	a.notifyHubLLMConfigChanged()
	return nil
}

// notifyHubLLMConfigChanged sends an immediate heartbeat to the Hub so that
// the llm_configured status is refreshed right after the user saves LLM config.
func (a *App) notifyHubLLMConfigChanged() {
	if a.remoteSessions == nil {
		return
	}
	hc := a.remoteSessions.hubClient
	if hc == nil || !hc.IsConnected() {
		return
	}
	if err := hc.SendHeartbeat(); err != nil {
		log.Printf("[LLM] failed to send immediate heartbeat after LLM config change: %v", err)
	}
}

// GetMaclawLLMConfig returns the current MaClaw LLM configuration.
func (a *App) GetMaclawLLMConfig() MaclawLLMConfig {
	cfg, err := a.LoadConfig()
	if err != nil {
		return MaclawLLMConfig{}
	}
	return MaclawLLMConfig{
		URL:      cfg.MaclawLLMUrl,
		Key:      cfg.MaclawLLMKey,
		Model:    cfg.MaclawLLMModel,
		Protocol: cfg.MaclawLLMProtocol,
	}
}

// isMaclawLLMConfigured returns true if the MaClaw LLM URL and model are set.
func (a *App) isMaclawLLMConfigured() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return strings.TrimSpace(cfg.MaclawLLMUrl) != "" && strings.TrimSpace(cfg.MaclawLLMModel) != ""
}

// SaveMaclawLLMConfig persists the MaClaw LLM configuration.
func (a *App) SaveMaclawLLMConfig(llm MaclawLLMConfig) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.MaclawLLMUrl = strings.TrimRight(strings.TrimSpace(llm.URL), "/")
	cfg.MaclawLLMKey = strings.TrimSpace(llm.Key)
	cfg.MaclawLLMModel = strings.TrimSpace(llm.Model)
	cfg.MaclawLLMProtocol = llm.Protocol
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	a.notifyHubLLMConfigChanged()
	return nil
}

// TestMaclawLLM sends a "hello" message to the configured LLM endpoint
// using the OpenAI-compatible or Anthropic Messages API and returns the response.
func (a *App) TestMaclawLLM(llm MaclawLLMConfig) (string, error) {
	url := strings.TrimRight(strings.TrimSpace(llm.URL), "/")
	if url == "" {
		return "", fmt.Errorf("LLM URL is not configured")
	}
	key := strings.TrimSpace(llm.Key)
	model := strings.TrimSpace(llm.Model)
	if model == "" {
		return "", fmt.Errorf("model name is not configured")
	}

	protocol := strings.TrimSpace(llm.Protocol)
	if protocol == "anthropic" {
		return a.testAnthropicLLM(url, key, model)
	}
	return a.testOpenAILLM(url, key, model)
}

// testOpenAILLM tests an OpenAI-compatible endpoint.
func (a *App) testOpenAILLM(url, key, model string) (string, error) {
	// Build OpenAI-compatible chat completion request.
	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
		"max_tokens": 100,
	}
	data, _ := json.Marshal(reqBody)

	endpoint := url + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenClaw/1.0")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // cap at 64KB
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from model")
	}
	return result.Choices[0].Message.Content, nil
}

// testAnthropicLLM tests an Anthropic Messages API endpoint.
func (a *App) testAnthropicLLM(url, key, model string) (string, error) {
	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hello"},
		},
		"max_tokens": 100,
	}
	data, _ := json.Marshal(reqBody)

	endpoint := url + "/v1/messages"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenClaw/1.0")
	req.Header.Set("anthropic-version", "2023-06-01")
	if key != "" {
		req.Header.Set("x-api-key", key)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	// Anthropic Messages API response format
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	for _, block := range result.Content {
		if block.Type == "text" && block.Text != "" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("no text response from model")
}

// defaultAgentMaxIterations is the fallback when the user has not configured a custom value.
const defaultAgentMaxIterations = 12

// GetMaclawAgentMaxIterations returns the configured max agent iterations.
//   - positive value: use that as the limit
//   - -1: unlimited (agent self-manages via set_max_iterations tool)
//   - 0 or missing: not configured → return default (12)
func (a *App) GetMaclawAgentMaxIterations() int {
	cfg, err := a.LoadConfig()
	if err != nil || cfg.MaclawAgentMaxIterations == 0 {
		return defaultAgentMaxIterations
	}
	if cfg.MaclawAgentMaxIterations < 0 {
		return 0 // -1 stored → 0 returned → means "unlimited"
	}
	return cfg.MaclawAgentMaxIterations
}

// SetMaclawAgentMaxIterations persists the max agent iterations setting.
//   - n > 0: fixed limit
//   - n == 0: unlimited (stored as -1 internally)
//   - n < 0: reset to default (stored as 0 internally, i.e. field absent)
func (a *App) SetMaclawAgentMaxIterations(n int) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	switch {
	case n > 0:
		cfg.MaclawAgentMaxIterations = n
	case n == 0:
		cfg.MaclawAgentMaxIterations = -1 // sentinel for "unlimited"
	default:
		cfg.MaclawAgentMaxIterations = 0 // zero-value = not configured
	}
	return a.SaveConfig(cfg)
}

// MaclawLLMStatus represents the online/offline status of the MaClaw LLM agent.
type MaclawLLMStatus struct {
	Online     bool   `json:"online"`
	Configured bool   `json:"configured"`
	Error      string `json:"error,omitempty"`
}

// maclawLLMPingClient is a shared HTTP client for lightweight LLM pings.
// Reusing the client enables TCP connection pooling across periodic pings.
var maclawLLMPingClient = &http.Client{Timeout: 10 * time.Second}

// PingMaclawLLM performs a lightweight connectivity check against the
// configured LLM endpoint.  It first tries GET /models (free, no tokens
// consumed).  If that returns 404 it falls back to a HEAD request on the
// chat completions path.
//
// All requests carry User-Agent "OpenClaw/1.0" so LLM providers can
// recognise the client for coding-plan eligibility.
func (a *App) PingMaclawLLM() MaclawLLMStatus {
	cfg, err := a.LoadConfig()
	if err != nil {
		return MaclawLLMStatus{Online: false, Configured: false, Error: err.Error()}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.MaclawLLMUrl), "/")
	model := strings.TrimSpace(cfg.MaclawLLMModel)
	if baseURL == "" || model == "" {
		return MaclawLLMStatus{Online: false, Configured: false}
	}

	key := strings.TrimSpace(cfg.MaclawLLMKey)
	protocol := strings.TrimSpace(cfg.MaclawLLMProtocol)

	if protocol == "anthropic" {
		// Anthropic: probe the messages endpoint
		online, err := maclawAnthropicProbe(baseURL+"/v1/messages", key)
		if err == nil {
			return MaclawLLMStatus{Online: online, Configured: true}
		}
		return MaclawLLMStatus{Online: false, Configured: true, Error: err.Error()}
	}

	// OpenAI-compatible: Try GET /models first — most OpenAI-compatible APIs support this and
	// it costs zero tokens.
	online, err2 := maclawLLMProbe(baseURL+"/models", key)
	if err2 == nil {
		return MaclawLLMStatus{Online: online, Configured: true}
	}

	// Fallback: HEAD on the chat completions endpoint.  A 405 (Method Not
	// Allowed) still proves the server is reachable and authenticated.
	online, err2 = maclawLLMProbe(baseURL+"/chat/completions", key)
	if err2 == nil {
		return MaclawLLMStatus{Online: online, Configured: true}
	}

	return MaclawLLMStatus{Online: false, Configured: true, Error: err2.Error()}
}

// maclawLLMProbe sends a GET request to endpoint and returns true when the
// server responds with any 2xx/4xx status (proving it is reachable and the
// credentials are accepted or at least the server is alive).  Only network
// errors and 5xx are treated as "offline".
func maclawLLMProbe(endpoint, key string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "OpenClaw/1.0")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := maclawLLMPingClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 1024)) // drain for conn reuse

	// 2xx or 4xx → server is alive (4xx = auth issue but reachable).
	// 5xx → server error, treat as offline.
	if resp.StatusCode < 500 {
		return true, nil
	}
	return false, fmt.Errorf("HTTP %d", resp.StatusCode)
}

// maclawAnthropicProbe sends a GET request to the Anthropic endpoint with
// the x-api-key header to verify connectivity.
func maclawAnthropicProbe(endpoint, key string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "OpenClaw/1.0")
	req.Header.Set("anthropic-version", "2023-06-01")
	if key != "" {
		req.Header.Set("x-api-key", key)
	}

	resp, err := maclawLLMPingClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 1024))

	if resp.StatusCode < 500 {
		return true, nil
	}
	return false, fmt.Errorf("HTTP %d", resp.StatusCode)
}
