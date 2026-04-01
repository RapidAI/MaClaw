package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// MaclawLLMProvider, MaclawLLMConfig — see corelib_aliases.go

const codegenProviderName = "CodeGen"

// defaultMaclawLLMProviders returns the built-in provider list.
func defaultMaclawLLMProviders() []MaclawLLMProvider {
	return []MaclawLLMProvider{
		{Name: "免费", URL: "http://localhost:18099/v1", Model: "free-proxy", ContextLength: 10000, AuthType: "none"},
		{Name: "OpenAI", URL: "https://api.openai.com/v1", Model: "gpt-5.4", AuthType: "oauth", ContextLength: 128000},
		{Name: "智谱", URL: "https://open.bigmodel.cn/api/paas/v4", Model: "glm-5-turbo", ContextLength: 180000},
		{Name: "MiniMax", URL: "https://api.minimaxi.com/v1", Model: "MiniMax-M2.7", ContextLength: 128000},
		{Name: "Kimi", URL: "https://api.kimi.com/coding/v1", Model: "kimi-for-coding", ContextLength: 128000, AgentType: "claude-code/2.0.0"},
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
			if cfg.MaclawLLMContextLength > 0 {
				providers[0].ContextLength = cfg.MaclawLLMContextLength
			}
		}
	}
	// Backfill context_length for known providers that predate this field.
	// Also sync URL for non-custom preset providers (e.g. port change).
	defaults := defaultMaclawLLMProviders()
	defaultCtx := make(map[string]int, len(defaults))
	defaultURL := make(map[string]string, len(defaults))
	for _, d := range defaults {
		if d.ContextLength > 0 {
			defaultCtx[d.Name] = d.ContextLength
		}
		if !d.IsCustom {
			defaultURL[d.Name] = d.URL
		}
	}
	for i := range providers {
		if providers[i].ContextLength == 0 {
			if cl, ok := defaultCtx[providers[i].Name]; ok {
				providers[i].ContextLength = cl
			}
		}
		if providers[i].Name == codegenProviderName && providers[i].AuthType == "sso" {
			providers[i].Protocol = "openai"
			providers[i].URL = strings.TrimRight(strings.TrimSpace(providers[i].URL), "/")
			providers[i].URL = strings.TrimSuffix(providers[i].URL, "/anthropic")
			continue
		}
		// Keep preset provider URLs in sync (handles port changes etc.)
		if !providers[i].IsCustom {
			if u, ok := defaultURL[providers[i].Name]; ok {
				providers[i].URL = u
			}
		}
	}

	// Ensure all default providers are present (e.g. OpenAI or Custom1 added
	// after the user already saved their config). Insert missing ones at the
	// correct position so the tab order matches defaultMaclawLLMProviders().
	existingNames := make(map[string]bool, len(providers))
	for _, p := range providers {
		existingNames[p.Name] = true
	}
	// Build a lookup: provider name → default order index.
	defaultOrder := make(map[string]int, len(defaults))
	for i, d := range defaults {
		defaultOrder[d.Name] = i
	}
	for dIdx, d := range defaults {
		if existingNames[d.Name] {
			continue
		}
		// Find insertion point: right before the first provider whose
		// default-order index is greater than dIdx.
		insertAt := len(providers)
		for i, p := range providers {
			if pIdx, ok := defaultOrder[p.Name]; ok && pIdx > dIdx {
				insertAt = i
				break
			}
		}
		// Safe mid-slice insert (avoid shared-backing-array mutation).
		updated := make([]MaclawLLMProvider, 0, len(providers)+1)
		updated = append(updated, providers[:insertAt]...)
		updated = append(updated, d)
		updated = append(updated, providers[insertAt:]...)
		providers = updated
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
	cfg.MaclawLLMContextLength = 0
	for _, p := range providers {
		if p.Name == current {
			cfg.MaclawLLMUrl = strings.TrimRight(strings.TrimSpace(p.URL), "/")
			cfg.MaclawLLMKey = strings.TrimSpace(p.Key)
			cfg.MaclawLLMModel = strings.TrimSpace(p.Model)
			cfg.MaclawLLMProtocol = p.Protocol
			cfg.MaclawLLMContextLength = p.ContextLength
			break
		}
	}
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	// Auto-start or stop the free proxy based on the selected provider.
	if current == freeProviderName {
		if !a.IsFreeProxyRunning() {
			go a.ensureFreeProxyIfNeeded()
		}
	} else {
		if a.IsFreeProxyRunning() {
			go a.StopFreeProxy()
		}
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
	// Use GetMaclawLLMProviders which applies URL sync for preset providers
	// (e.g. port changes), instead of reading legacy fields directly.
	data := a.GetMaclawLLMProviders()
	for _, p := range data.Providers {
		if p.Name == data.Current {
			return MaclawLLMConfig{
				URL:            p.URL,
				Key:            p.Key,
				Model:          p.Model,
				Protocol:       p.Protocol,
				ContextLength:  p.ContextLength,
				SupportsVision: p.SupportsVision,
				AgentType:      p.AgentType,
			}
		}
	}
	return MaclawLLMConfig{}
}

// isMaclawLLMConfigured returns true if the MaClaw LLM URL and model are set.
func (a *App) isMaclawLLMConfigured() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return strings.TrimSpace(cfg.MaclawLLMUrl) != "" && strings.TrimSpace(cfg.MaclawLLMModel) != ""
}

// isProMode returns true if the UI is in "pro" mode (full coding tools).
// In lite/simple mode, coding session tools are not available because the
// user has not configured coding LLM providers.
func (a *App) isProMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.UIMode == "pro"
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
	cfg.MaclawLLMContextLength = llm.ContextLength
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	a.notifyHubLLMConfigChanged()
	return nil
}

// StartOpenAIOAuth starts the OpenAI OAuth PKCE flow. On success, it updates
// the OpenAI provider config with the obtained tokens and persists the change.
func (a *App) StartOpenAIOAuth() (string, error) {
	cfg := oauth.DefaultConfig()
	result, err := oauth.RunOAuthFlow(cfg)
	if err != nil {
		return "", fmt.Errorf("OAuth 登录失败: %w", err)
	}

	// Update the OpenAI provider with the obtained tokens
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if p.Name == "OpenAI" && p.AuthType == "oauth" {
			data.Providers[i] = oauth.ApplyTokenResult(p, result)
			if err := a.SaveMaclawLLMProviders(data.Providers, "OpenAI"); err != nil {
				return "", fmt.Errorf("保存 OAuth 配置失败: %w", err)
			}
			return "OpenAI OAuth 登录成功", nil
		}
	}
	return "", fmt.Errorf("未找到 OpenAI provider")
}

// GetOpenAIUsage queries the OpenAI billing API for the current OAuth provider's
// usage info. It refreshes the token first if needed.
func (a *App) GetOpenAIUsage() (*oauth.UsageInfo, error) {
	if err := a.ensureOAuthToken(); err != nil {
		return nil, fmt.Errorf("OAuth token 刷新失败: %w", err)
	}
	data := a.GetMaclawLLMProviders()
	for _, p := range data.Providers {
		if p.Name == data.Current && p.AuthType == "oauth" {
			if p.Key == "" {
				return nil, fmt.Errorf("未登录 OpenAI，请先完成 OAuth 授权")
			}
			return oauth.QueryUsage(p.Key)
		}
	}
	return nil, fmt.Errorf("当前 provider 不支持用量查询")
}

// ensureOAuthToken checks if the current provider uses OAuth and refreshes
// the token if needed. Returns the (possibly updated) LLM config.
func (a *App) ensureOAuthToken() error {
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if p.Name == data.Current && p.AuthType == "oauth" {
			cfg := oauth.DefaultConfig()
			updated, err := oauth.EnsureValidToken(p, cfg, func(up MaclawLLMProvider) error {
				data.Providers[i] = up
				return a.SaveMaclawLLMProviders(data.Providers, data.Current)
			})
			if err != nil {
				return err
			}
			data.Providers[i] = updated
			break
		}
	}
	return nil
}

// TestMaclawLLM sends a "hello" message to the configured LLM endpoint
// using the OpenAI-compatible or Anthropic Messages API and returns the response.
// After a successful text test, it also probes vision support and persists the
// result into the provider's SupportsVision field.
func (a *App) TestMaclawLLM(llm MaclawLLMConfig) (string, error) {
	log.Printf("[LLM] TestMaclawLLM: agent_type=%q user_agent=%q", llm.AgentType, llm.UserAgent())
	if err := a.ensureOAuthToken(); err != nil {
		return "", fmt.Errorf("OAuth token refresh failed: %w", err)
	}

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
	var textResult string
	var err error
	if protocol == "anthropic" {
		textResult, err = a.testAnthropicLLM(url, key, model, llm.UserAgent())
	} else {
		textResult, err = a.testOpenAILLM(url, key, model, llm.UserAgent())
	}
	if err != nil {
		return "", err
	}

	// Probe vision support and persist the result.
	vision := probeVisionSupport(url, key, model, protocol, llm.UserAgent())
	a.saveVisionProbeResult(vision)
	log.Printf("[LLM] vision probe for %s: supports_vision=%v", model, vision)

	suffix := "（不支持图片）"
	if vision {
		suffix = "（支持图片）"
	}
	return textResult + "\n" + suffix, nil
}

// testOpenAILLM tests an OpenAI-compatible endpoint.
func (a *App) testOpenAILLM(url, key, model, userAgent string) (string, error) {
	// Build OpenAI-compatible chat completion request.
	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
		"max_tokens": 100,
		"stream":     false,
	}
	data, _ := json.Marshal(reqBody)

	endpoint := url + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
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

	// Some gateways (e.g. newapi) return SSE even when stream=false.
	// Detect and handle both formats.
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("empty response body")
	}

	var jsonBody []byte
	if trimmed[0] == '{' {
		// Normal JSON response.
		jsonBody = body
	} else if bytes.HasPrefix(trimmed, []byte("data: ")) {
		// SSE format — collect content from chunks.
		var content strings.Builder
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				break
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content != "" {
					content.WriteString(c.Delta.Content)
				}
				if c.Message.Content != "" {
					content.WriteString(c.Message.Content)
				}
			}
		}
		if content.Len() > 0 {
			return stripFunctionCalls(stripThinkTags(content.String())), nil
		}
		return "", fmt.Errorf("SSE response contained no content (model may not exist on this gateway)")
	} else {
		preview := string(trimmed)
		if len(preview) > 256 {
			preview = preview[:256] + "..."
		}
		return "", fmt.Errorf("unexpected response format: %s", preview)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(jsonBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from model")
	}
	text := result.Choices[0].Message.Content
	if text == "" {
		text = result.Choices[0].Message.ReasoningContent
	}
	return stripFunctionCalls(stripThinkTags(text)), nil
}

// testAnthropicLLM tests an Anthropic Messages API endpoint.
func (a *App) testAnthropicLLM(url, key, model, userAgent string) (string, error) {
	reqBody := map[string]interface{}{
		"model": model,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hello"},
		},
		"max_tokens": 100,
	}
	data, _ := json.Marshal(reqBody)

	// If the URL already ends with /v1, append /messages directly;
	// otherwise append /v1/messages (standard Anthropic base URL).
	endpoint := corelib.AnthropicMessagesEndpoint(url)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, key)

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
			return stripFunctionCalls(stripThinkTags(block.Text)), nil
		}
	}
	return "", fmt.Errorf("no text response from model")
}

// probeVisionSupport sends a tiny 1x1 red PNG as an image_url message to the
// LLM and returns true if the model responds successfully (i.e. supports vision).
// This is a best-effort probe — network errors or timeouts return false.
func probeVisionSupport(baseURL, key, model, protocol, userAgent string) bool {
	// 1x1 red PNG, 68 bytes
	const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg=="

	if protocol == "anthropic" {
		return probeVisionAnthropic(baseURL, key, model, tinyPNG, userAgent)
	}
	return probeVisionOpenAI(baseURL, key, model, tinyPNG, userAgent)
}

func probeVisionOpenAI(baseURL, key, model, imgB64, userAgent string) bool {
	reqBody := map[string]interface{}{
		"model": model,
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "What color is this image? Reply in one word."},
					map[string]interface{}{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": "data:image/png;base64," + imgB64,
						},
					},
				},
			},
		},
		"max_tokens": 20,
		"stream":     false,
	}
	data, _ := json.Marshal(reqBody)
	endpoint := baseURL + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 8*1024))
	return resp.StatusCode == http.StatusOK
}

func probeVisionAnthropic(baseURL, key, model, imgB64, userAgent string) bool {
	reqBody := map[string]interface{}{
		"model": model,
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "What color is this image? Reply in one word."},
					map[string]interface{}{
						"type": "image",
						"source": map[string]interface{}{
							"type":       "base64",
							"media_type": "image/png",
							"data":       imgB64,
						},
					},
				},
			},
		},
		"max_tokens": 20,
	}
	data, _ := json.Marshal(reqBody)
	endpoint := corelib.AnthropicMessagesEndpoint(baseURL)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, key)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 8*1024))
	return resp.StatusCode == http.StatusOK
}

// saveVisionProbeResult persists the vision probe result into the matching
// provider entry in the config.
func (a *App) saveVisionProbeResult(supportsVision bool) {
	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if p.Name == data.Current {
			data.Providers[i].SupportsVision = supportsVision
			if err := a.SaveMaclawLLMProviders(data.Providers, data.Current); err != nil {
				log.Printf("[LLM] failed to save vision probe result: %v", err)
			}
			return
		}
	}
}

// maxAgentIterationsCap — see corelib_aliases.go

// GetMaclawAgentMaxIterations returns the configured max agent iterations.
//   - positive value: use that as the limit
//   - -1 or 0 (not configured): unlimited → return 0
func (a *App) GetMaclawAgentMaxIterations() int {
	cfg, err := a.LoadConfig()
	if err != nil || cfg.MaclawAgentMaxIterations <= 0 {
		return maxAgentIterationsCap // not configured → default 300
	}
	return cfg.MaclawAgentMaxIterations
}

// SetMaclawAgentMaxIterations persists the max agent iterations setting.
//   - n > 0: fixed limit
//   - n == 0: unlimited (stored as -1 internally)
//   - n < 0: also unlimited (stored as 0 internally, treated same as not configured)
func (a *App) SetMaclawAgentMaxIterations(n int) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if n <= 0 {
		n = maxAgentIterationsCap // default 300
	}
	if n < minAgentIterations {
		n = minAgentIterations
	}
	if n > maxAgentIterationsCap {
		n = maxAgentIterationsCap
	}
	cfg.MaclawAgentMaxIterations = n // 30-300
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
// All requests carry a User-Agent of "claude-code/2.0.0" so LLM providers can
// recognise the client for coding-plan eligibility.
func (a *App) PingMaclawLLM() MaclawLLMStatus {
	if err := a.ensureOAuthToken(); err != nil {
		return MaclawLLMStatus{Online: false, Configured: true, Error: err.Error()}
	}
	if err := a.ensureCodeGenToken(); err != nil {
		return MaclawLLMStatus{Online: false, Configured: true, Error: err.Error()}
	}

	llmCfg := a.GetMaclawLLMConfig()
	baseURL := strings.TrimRight(strings.TrimSpace(llmCfg.URL), "/")
	model := strings.TrimSpace(llmCfg.Model)
	if baseURL == "" || model == "" {
		return MaclawLLMStatus{Online: false, Configured: false}
	}

	key := strings.TrimSpace(llmCfg.Key)
	protocol := strings.TrimSpace(llmCfg.Protocol)
	ua := llmCfg.UserAgent()
	log.Printf("[LLM] PingMaclawLLM: agent_type=%q user_agent=%q", llmCfg.AgentType, ua)

	if protocol == "anthropic" {
		probeEndpoint := corelib.AnthropicMessagesEndpoint(baseURL)
		online, err := maclawAnthropicProbe(probeEndpoint, key, ua)
		if err == nil {
			return MaclawLLMStatus{Online: online, Configured: true}
		}
		return MaclawLLMStatus{Online: false, Configured: true, Error: err.Error()}
	}

	online, err2 := maclawLLMProbe(baseURL+"/models", key, ua)
	if err2 == nil {
		return MaclawLLMStatus{Online: online, Configured: true}
	}

	online, err2 = maclawLLMProbe(baseURL+"/chat/completions", key, ua)
	if err2 == nil {
		return MaclawLLMStatus{Online: online, Configured: true}
	}

	return MaclawLLMStatus{Online: false, Configured: true, Error: err2.Error()}
}

// maclawLLMProbe sends a GET request to endpoint and returns true when the
// server responds with any 2xx/4xx status (proving it is reachable and the
// credentials are accepted or at least the server is alive).  Only network
// errors and 5xx are treated as "offline".
func maclawLLMProbe(endpoint, key, userAgent string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", userAgent)
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
// the x-api-key and Authorization Bearer headers to verify connectivity.
func maclawAnthropicProbe(endpoint, key, userAgent string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("anthropic-version", "2023-06-01")
	corelib.SetAnthropicAuthHeaders(req, key)

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

// GetLLMTrajectoryLogging returns the current trajectory logging toggle state.
func (a *App) GetLLMTrajectoryLogging() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.LLMTrajectoryLogging
}

// SetLLMTrajectoryLogging enables or disables LLM trajectory logging.
func (a *App) SetLLMTrajectoryLogging(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.LLMTrajectoryLogging = enabled
	return a.SaveConfig(cfg)
}

// ---------------------------------------------------------------------------
// LLM Token Usage Statistics
// ---------------------------------------------------------------------------

// AccumulateLLMTokenUsage adds token counts for the given provider.
// Called internally after each LLM API call. Thread-safe via tokenUsageMu.
func (a *App) AccumulateLLMTokenUsage(providerName string, inputTokens, outputTokens int) {
	if inputTokens == 0 && outputTokens == 0 {
		return
	}
	a.tokenUsageMu.Lock()
	defer a.tokenUsageMu.Unlock()
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[LLM] AccumulateLLMTokenUsage: load config: %v", err)
		return
	}
	if cfg.LLMTokenUsage == nil {
		cfg.LLMTokenUsage = make(map[string]*TokenUsageStat)
	}
	stat, ok := cfg.LLMTokenUsage[providerName]
	if !ok {
		stat = &TokenUsageStat{}
		cfg.LLMTokenUsage[providerName] = stat
	}
	stat.InputTokens += int64(inputTokens)
	stat.OutputTokens += int64(outputTokens)
	stat.TotalTokens = stat.InputTokens + stat.OutputTokens
	if err := a.SaveConfig(cfg); err != nil {
		log.Printf("[LLM] AccumulateLLMTokenUsage: save config: %v", err)
	}
}

// GetLLMTokenUsage returns the token usage stats for a specific provider.
// If provider is empty, returns stats for the current provider.
func (a *App) GetLLMTokenUsage(provider string) *TokenUsageStat {
	cfg, err := a.LoadConfig()
	if err != nil {
		return &TokenUsageStat{}
	}
	if provider == "" {
		provider = cfg.MaclawLLMCurrentProvider
	}
	if cfg.LLMTokenUsage == nil {
		return &TokenUsageStat{}
	}
	if stat, ok := cfg.LLMTokenUsage[provider]; ok {
		return stat
	}
	return &TokenUsageStat{}
}

// GetAllLLMTokenUsage returns token usage stats for all providers.
func (a *App) GetAllLLMTokenUsage() map[string]*TokenUsageStat {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]*TokenUsageStat{}
	}
	if cfg.LLMTokenUsage == nil {
		return map[string]*TokenUsageStat{}
	}
	return cfg.LLMTokenUsage
}

// ResetLLMTokenUsage resets the token usage stats for a specific provider.
// If provider is empty, resets all providers.
func (a *App) ResetLLMTokenUsage(provider string) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if cfg.LLMTokenUsage == nil {
		return nil
	}
	if provider == "" {
		cfg.LLMTokenUsage = make(map[string]*TokenUsageStat)
	} else {
		delete(cfg.LLMTokenUsage, provider)
	}
	return a.SaveConfig(cfg)
}

// ---------------------------------------------------------------------------
// CodeGen SSO — TigerClaw 品牌企业 SSO 集成
// ---------------------------------------------------------------------------

// shouldSkipCodeGenSSO returns true when the given brand ID is not "qianxin",
// meaning all CodeGen SSO logic should be skipped.
func shouldSkipCodeGenSSO(brandID string) bool {
	return brandID != "qianxin"
}

// ensureCodeGenToken 检查 CodeGen SSO token 是否有效。
// 1. 非 qianxin 品牌直接返回 nil
// 2. 查找 AuthType=="sso" 的 CodeGen provider
// 3. TokenExpiresAt > 0 且未过期 → 返回 nil
// 4. TokenExpiresAt > 0 且即将过期 → 尝试 RefreshCodeGenToken
//    a. 刷新成功 → 更新 provider + WriteAllToolConfigs + 持久化
//    b. 刷新失败 → 返回 "认证已过期" 错误
// 5. TokenExpiresAt == 0 → ValidateCodeGenToken(API 调用验证)
//    a. 有效 → 返回 nil
//    b. 无效 → 返回 "认证已失效" 错误
func (a *App) ensureCodeGenToken() error {
	// 1. 品牌检查：非 qianxin 直接返回
	if shouldSkipCodeGenSSO(brand.Current().ID) {
		return nil
	}

	// 2. 查找 AuthType=="sso" 的 CodeGen provider
	data := a.GetMaclawLLMProviders()
	providerIdx := -1
	var provider MaclawLLMProvider
	for i, p := range data.Providers {
		if p.Name == codegenProviderName && p.AuthType == "sso" {
			providerIdx = i
			provider = p
			break
		}
	}
	if providerIdx < 0 {
		// 没有 SSO provider，跳过校验
		return nil
	}

	// 3 & 4. TokenExpiresAt > 0: 检查是否需要刷新
	if provider.TokenExpiresAt > 0 {
		if !oauth.NeedsRefreshCodeGen(provider) {
			return nil // token 仍然有效
		}
		// 即将过期 → 尝试静默刷新
		if provider.RefreshToken == "" {
			return fmt.Errorf("CodeGen 认证已过期，请重新进行企业 SSO 登录")
		}
		result, err := oauth.RefreshCodeGenToken(provider.RefreshToken)
		if err != nil {
			log.Printf("[CodeGen] token refresh failed: %v", err)
			return fmt.Errorf("CodeGen 认证已过期，请重新进行企业 SSO 登录")
		}
		// 刷新成功 → 更新 provider 字段
		updated := oauth.ApplyTokenResult(provider, result)
		data.Providers[providerIdx] = updated
		// 持久化到 config.json
		if err := a.SaveMaclawLLMProviders(data.Providers, data.Current); err != nil {
			log.Printf("[CodeGen] save refreshed token failed: %v", err)
			return fmt.Errorf("CodeGen 认证刷新成功但保存失败: %w", err)
		}
		// 同步更新所有工具配置
		tcResult := configfile.WriteAllToolConfigs(configfile.ToolConfigParams{
			Token:            updated.Key,
			BaseURL:          updated.URL,
			AnthropicBaseURL: codegenAnthropicBaseURL(updated.URL),
			ModelID:          updated.Model,
			ProviderName:     codegenProviderName,
		})
		for _, f := range tcResult.Failed {
			log.Printf("[CodeGen] WriteAllToolConfigs: %s failed: %v", f.Tool, f.Error)
		}
		// 同步更新编程工具模型列表中 CodeGen 条目的 api_key
		if cfg, loadErr := a.LoadConfig(); loadErr == nil {
			toolConfigs := []*ToolConfig{
				&cfg.Claude, &cfg.Codex, &cfg.Opencode,
				&cfg.CodeBuddy, &cfg.IFlow, &cfg.Kilo,
			}
			changed := false
			for _, tc := range toolConfigs {
				for i, m := range tc.Models {
					if m.ModelName == codegenProviderName && m.ApiKey != updated.Key {
						tc.Models[i].ApiKey = updated.Key
						changed = true
					}
				}
			}
			if changed {
				_ = a.SaveConfig(cfg)
			}
		}
		// 同步更新本地 Anthropic→OpenAI 代理的上游凭证
		go a.ensureCodeGenProxyIfNeeded()
		return nil
	}

	// 5. TokenExpiresAt == 0 → 用 API 调用验证 token 有效性
	if provider.TokenExpiresAt == 0 && provider.Key != "" {
		if !oauth.ValidateCodeGenToken(provider.Key) {
			return fmt.Errorf("CodeGen 认证已失效，请重新进行企业 SSO 登录")
		}
	}
	return nil
}

// CodeGenSSOInfo 是 StartCodeGenSSO 的返回结果，包含 SSO 认证成功后的关键信息。
type CodeGenSSOInfo struct {
	// Message 是面向用户的成功/警告消息。
	Message string `json:"message"`
	// Email 是从 id_token 解析出的用户邮件地址，用于自动注册 Hub。
	Email string `json:"email"`
}

// StartCodeGenSSO 执行企业 SSO 扫码登录流程，成功后：
//  1. 将 "CodeGen" 服务商 upsert 到 MaClaw LLM providers 列表并设为当前服务商
//  2. 将认证信息写入 ~/.claude/settings.json 供 TigerClaw Code 使用
//  3. 返回用户 email（从 SSO 解析），供前端自动注册 Hub
//
// 仅在 TigerClaw 品牌（oem_qianxin）下的 Onboarding 第 1 步调用。
func (a *App) StartCodeGenSSO() (CodeGenSSOInfo, error) {
	// 1. 启动扫码登录流程，弹出浏览器完成企业 SSO 登录
	result, err := oauth.RunCodeGenSSOFlow()
	if err != nil {
		return CodeGenSSOInfo{}, fmt.Errorf("SSO 认证失败: %w", err)
	}

	// 2. 构造 CodeGen provider 条目并 upsert 到列表
	data := a.GetMaclawLLMProviders()
	updatedProviders := upsertCodeGenProvider(data.Providers, result)

	// 3. 保存到 MaClaw 配置（~/.maclaw/config.json）
	if err := a.SaveMaclawLLMProviders(updatedProviders, codegenProviderName); err != nil {
		return CodeGenSSOInfo{}, fmt.Errorf("保存 MaClaw 配置失败: %w", err)
	}

	// 4. 如果拿到了 email，顺手存入配置，供后续自动注册 Hub 使用
	if result.Email != "" {
		if appCfg, err := a.LoadConfig(); err == nil {
			if appCfg.RemoteEmail == "" {
				appCfg.RemoteEmail = result.Email
				_ = a.SaveConfig(appCfg)
			}
		}
	}

	// 5. 写入所有编程工具配置文件
	// 非致命：MaClaw 已配置成功，部分工具写入失败仅记录警告
	toolResult := configfile.WriteAllToolConfigs(configfile.ToolConfigParams{
		Token:            result.AccessToken,
		BaseURL:          result.BaseURL,
		AnthropicBaseURL: codegenAnthropicBaseURL(result.BaseURL),
		ModelID:          result.ModelID,
		ProviderName:     codegenProviderName,
	})

	// 6. 将 CodeGen 注入到各编程工具的服务商列表中
	a.injectCodeGenModelIntoToolConfigs(result)

	// 7. 启动本地 Anthropic→OpenAI 协议转换代理，供 Claude Code 使用
	go a.ensureCodeGenProxyIfNeeded()

	var msg string
	if len(toolResult.Failed) == 0 {
		msg = "SSO 认证成功，所有工具配置已写入完毕"
	} else {
		failedNames := make([]string, 0, len(toolResult.Failed))
		for _, f := range toolResult.Failed {
			log.Printf("[CodeGen SSO] WriteAllToolConfigs: %s failed: %v", f.Tool, f.Error)
			failedNames = append(failedNames, f.Tool)
		}
		msg = fmt.Sprintf("SSO 认证成功（注意：%s 配置写入失败，请手动检查）", strings.Join(failedNames, "、"))
	}

	return CodeGenSSOInfo{
		Message: msg,
		Email:   result.Email,
	}, nil
}

// upsertCodeGenProvider 在 providers 列表中插入或更新 "CodeGen" 服务商条目。
// 如果列表中已存在同名条目则覆盖，否则追加到列表末尾。
// 返回新的 providers 切片（不修改原切片）。
func upsertCodeGenProvider(providers []MaclawLLMProvider, result oauth.CodeGenSSOResult) []MaclawLLMProvider {
	entry := MaclawLLMProvider{
		Name:          codegenProviderName,
		URL:           result.BaseURL,
		Key:           result.AccessToken,
		Model:         result.ModelID,
		Protocol:      "openai",          // AI 助手通过 OpenAI 协议接入 CodeGen
		AgentType:     "openclaw",        // MaClaw Agent 默认协议
		AuthType:      "sso",             // 标识认证来源，区别于手动 API Key
		ContextLength: result.ContextLength,
	}
	// 遍历查找并覆盖已有 CodeGen 条目
	for i, p := range providers {
		if p.Name == codegenProviderName {
			updated := make([]MaclawLLMProvider, len(providers))
			copy(updated, providers)
			updated[i] = entry
			return updated
		}
	}
	// 未找到则追加
	return append(providers, entry)
}

// codegenClaudeProxyBaseURL 是本地 OpenAI→Anthropic 协议转换代理地址。
// CodeGen 原始服务只提供 OpenAI 协议；Claude Code 需要 Anthropic 协议，
// 因此必须固定走本地兼容代理，而不是把上游 CodeGen URL 直接追加 /anthropic。
const codegenClaudeProxyBaseURL = "http://127.0.0.1:5001/anthropic"

// codegenAnthropicBaseURL 返回 CodeGen 给 Claude/TigerClaw Code 使用的 Anthropic 兼容端点。
func codegenAnthropicBaseURL(openaiBaseURL string) string {
	return codegenClaudeProxyBaseURL
}

// injectCodeGenModelIntoToolConfigs 将 CodeGen 服务商作为模型条目注入到各编程工具的
// 模型列表中（Claude、Codex、OpenCode 等），使其出现在前端的服务商选择网格中。
// 如果已存在同名条目则更新，否则插入到 Custom 条目之前。
//
// 注意：Claude Code 使用 anthropic 协议，需要将 CodeGen 的 openai base URL
// 转换为 anthropic 兼容端点（追加 /anthropic）。其他工具直接使用 openai URL。
func (a *App) injectCodeGenModelIntoToolConfigs(result oauth.CodeGenSSOResult) {
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[CodeGen SSO] injectCodeGenModelIntoToolConfigs: LoadConfig failed: %v", err)
		return
	}

	openaiURL := result.BaseURL
	anthropicURL := codegenAnthropicBaseURL(openaiURL)

	// Claude Code 使用 anthropic 协议端点
	claudeModel := ModelConfig{
		ModelName: codegenProviderName,
		ModelId:   result.ModelID,
		ModelUrl:  anthropicURL,
		ApiKey:    result.AccessToken,
		WireApi:   "anthropic",
	}
	upsertModelInToolConfig(&cfg.Claude, claudeModel)

	// 其他工具使用 openai 协议端点
	openaiModel := ModelConfig{
		ModelName: codegenProviderName,
		ModelId:   result.ModelID,
		ModelUrl:  openaiURL,
		ApiKey:    result.AccessToken,
		WireApi:   "responses",
	}
	openaiToolConfigs := []*ToolConfig{
		&cfg.Codex,
		&cfg.Opencode,
		&cfg.CodeBuddy,
		&cfg.IFlow,
		&cfg.Kilo,
	}
	for _, tc := range openaiToolConfigs {
		upsertModelInToolConfig(tc, openaiModel)
	}

	if err := a.SaveConfig(cfg); err != nil {
		log.Printf("[CodeGen SSO] injectCodeGenModelIntoToolConfigs: SaveConfig failed: %v", err)
	}
}

// upsertModelInToolConfig 在 ToolConfig 的 Models 列表中插入或更新指定名称的模型。
// 如果已存在同名条目则更新其字段；否则插入到第一个 IsCustom 条目之前。
func upsertModelInToolConfig(tc *ToolConfig, model ModelConfig) {
	for i, m := range tc.Models {
		if m.ModelName == model.ModelName {
			tc.Models[i].ModelId = model.ModelId
			tc.Models[i].ModelUrl = model.ModelUrl
			tc.Models[i].ApiKey = model.ApiKey
			tc.Models[i].WireApi = model.WireApi
			return
		}
	}
	// 插入到第一个 Custom 条目之前
	insertIdx := len(tc.Models)
	for i, m := range tc.Models {
		if m.IsCustom {
			insertIdx = i
			break
		}
	}
	newModels := make([]ModelConfig, 0, len(tc.Models)+1)
	newModels = append(newModels, tc.Models[:insertIdx]...)
	newModels = append(newModels, model)
	newModels = append(newModels, tc.Models[insertIdx:]...)
	tc.Models = newModels
}

// ---------------------------------------------------------------------------
// CodeGen 模型列表 + 模型选择保存
// ---------------------------------------------------------------------------

// CodeGenModelItem 描述一个从 CodeGen 服务获取的可用模型。
type CodeGenModelItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FetchCodeGenModels 用当前 CodeGen provider 的 access_token 调用
// {baseURL}/models 端点，返回该账号可用的模型列表。
// 前端 SSO 成功后调用此函数填充模型选择器。
func (a *App) FetchCodeGenModels() ([]CodeGenModelItem, error) {
	// 从已保存的 CodeGen provider 中读取认证信息
	data := a.GetMaclawLLMProviders()
	var codeGenProvider *MaclawLLMProvider
	for i := range data.Providers {
		if data.Providers[i].Name == codegenProviderName {
			codeGenProvider = &data.Providers[i]
			break
		}
	}
	if codeGenProvider == nil || codeGenProvider.Key == "" {
		return nil, fmt.Errorf("CodeGen SSO 未完成，请先完成企业认证")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(codeGenProvider.URL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("CodeGen base_url 未配置")
	}

	endpoint := baseURL + "/models"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+codeGenProvider.Key)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取模型列表失败: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("服务器返回 HTTP %d: %s", resp.StatusCode, truncateCodeGenStr(string(body), 256))
	}

	// 解析 OpenAI 兼容格式：{"data": [{"id": "...", ...}, ...]}
	// 同时兼容 Anthropic 格式：{"models": [{"id": "...", "display_name": "..."}]}
	var result struct {
		Data   []struct{ ID string `json:"id"` }             `json:"data"`
		Models []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}

	var items []CodeGenModelItem
	// 优先 Anthropic 格式
	if len(result.Models) > 0 {
		for _, m := range result.Models {
			name := m.DisplayName
			if name == "" {
				name = m.ID
			}
			items = append(items, CodeGenModelItem{ID: m.ID, Name: name})
		}
	} else {
		for _, m := range result.Data {
			items = append(items, CodeGenModelItem{ID: m.ID, Name: m.ID})
		}
	}

	// 若服务端返回空列表，至少用 SSO 返回的默认模型填充
	if len(items) == 0 && codeGenProvider.Model != "" {
		items = append(items, CodeGenModelItem{ID: codeGenProvider.Model, Name: codeGenProvider.Model})
	}

	return items, nil
}

// SaveCodeGenModelChoice 保存用户在 SSO 后选择的模型：
//   - maclawModel：用于驱动 MaClaw Agent（写入 config.json 的 CodeGen provider）
//   - claudeCodeModel：用于驱动 TigerClaw Code（写入 ~/.claude/settings.json）
//
// 两个模型可以相同也可以不同，独立配置。
func (a *App) SaveCodeGenModelChoice(maclawModel, claudeCodeModel string) error {
	maclawModel = strings.TrimSpace(maclawModel)
	claudeCodeModel = strings.TrimSpace(claudeCodeModel)

	// 1. 更新 MaClaw CodeGen provider 的 model 字段
	if maclawModel != "" {
		data := a.GetMaclawLLMProviders()
		updated := false
		for i := range data.Providers {
			if data.Providers[i].Name == codegenProviderName {
				data.Providers[i].Model = maclawModel
				updated = true
				break
			}
		}
		if updated {
			if err := a.SaveMaclawLLMProviders(data.Providers, codegenProviderName); err != nil {
				return fmt.Errorf("保存 MaClaw 模型选择失败: %w", err)
			}
		}
	}

	claudeTargetModel := claudeCodeModel
	if claudeTargetModel == "" {
		claudeTargetModel = maclawModel
	}

	// 2. 同步更新各编程工具模型列表中的 CodeGen 条目
	if cfg, err := a.LoadConfig(); err == nil {
		changed := false
		var claudeEntry *ModelConfig

		if claudeTargetModel != "" {
			if cfg.Claude.CurrentModel != codegenProviderName {
				cfg.Claude.CurrentModel = codegenProviderName
				changed = true
			}
			for i := range cfg.Claude.Models {
				if cfg.Claude.Models[i].ModelName == codegenProviderName {
					if cfg.Claude.Models[i].ModelId != claudeTargetModel {
						cfg.Claude.Models[i].ModelId = claudeTargetModel
						changed = true
					}
					claudeEntry = &cfg.Claude.Models[i]
					break
				}
			}
		}

		if maclawModel != "" {
			toolConfigs := []*ToolConfig{
				&cfg.Codex, &cfg.Opencode,
				&cfg.CodeBuddy, &cfg.IFlow, &cfg.Kilo,
			}
			for _, tc := range toolConfigs {
				for i := range tc.Models {
					if tc.Models[i].ModelName == codegenProviderName && tc.Models[i].ModelId != maclawModel {
						tc.Models[i].ModelId = maclawModel
						changed = true
					}
				}
			}
		}

		if changed {
			if err := a.SaveConfig(cfg); err != nil {
				log.Printf("[CodeGen] SaveCodeGenModelChoice: sync tool config model failed: %v", err)
			} else if claudeEntry != nil && claudeEntry.ApiKey != "" {
				if err := configfile.WriteClaudeSettings(claudeEntry.ApiKey, claudeEntry.ModelUrl, claudeEntry.ModelId); err != nil {
					log.Printf("[CodeGen] SaveCodeGenModelChoice: update claude model failed: %v", err)
				}
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// 内嵌二维码扫码 SSO — Embedded QR Code SSO
// ---------------------------------------------------------------------------

// ssoPollingResult 是后台轮询 goroutine 的结果。
type ssoPollingResult struct {
	info CodeGenSSOInfo
	err  error
}

// ssoPollingSession 保存一次内嵌 SSO 轮询会话的状态。
type ssoPollingSession struct {
	cancel   context.CancelFunc
	resultCh chan ssoPollingResult
}

// CodeGenSSOEmbeddedResult 是 StartCodeGenSSOEmbedded 的返回值。
type CodeGenSSOEmbeddedResult struct {
	QRCodeURL string `json:"qr_code_url"` // 二维码内容 URL，供前端 QRCodeSVG 渲染
}

// StartCodeGenSSOEmbedded 启动内嵌 SSO 扫码流程（本地回调模式）。
//
// 流程：
//  1. 启动本地 HTTP 服务器接收 SSO 回调
//  2. 打开浏览器访问 SSO 登录页（ref 指向本地服务器）
//  3. 用户在浏览器中扫码，SSO 完成后浏览器自动重定向到本地服务器
//  4. 本地服务器从 URL 参数中提取 token，自动完成配置
//
// 无需轮询，token 通过 HTTP 回调直接获取。
func (a *App) StartCodeGenSSOEmbedded() (CodeGenSSOEmbeddedResult, error) {
	if brand.Current().ID != "qianxin" {
		return CodeGenSSOEmbeddedResult{}, fmt.Errorf("内嵌扫码仅支持奇安信品牌")
	}

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan ssoPollingResult, 1)

	session := &ssoPollingSession{
		cancel:   cancel,
		resultCh: resultCh,
	}

	a.ssoPollingMu.Lock()
	if a.ssoPolling != nil {
		a.ssoPolling.cancel()
	}
	a.ssoPolling = session
	a.ssoPollingMu.Unlock()

	// 后台 goroutine：本地回调模式 SSO 流程
	go func() {
		result, err := oauth.RunCodeGenSSOFlowWithCallback(ctx)
		if err != nil {
			if ctx.Err() != nil {
				resultCh <- ssoPollingResult{err: context.Canceled}
				return
			}
			resultCh <- ssoPollingResult{err: err}
			return
		}

		// 后处理：与 StartCodeGenSSO 完全一致
		data := a.GetMaclawLLMProviders()
		updatedProviders := upsertCodeGenProvider(data.Providers, result)

		if err := a.SaveMaclawLLMProviders(updatedProviders, codegenProviderName); err != nil {
			resultCh <- ssoPollingResult{err: fmt.Errorf("保存 MaClaw 配置失败: %w", err)}
			return
		}

		if result.Email != "" {
			if appCfg, err := a.LoadConfig(); err == nil {
				if appCfg.RemoteEmail == "" {
					appCfg.RemoteEmail = result.Email
					_ = a.SaveConfig(appCfg)
				}
			}
		}

		toolResult := configfile.WriteAllToolConfigs(configfile.ToolConfigParams{
			Token:            result.AccessToken,
			BaseURL:          result.BaseURL,
			AnthropicBaseURL: codegenAnthropicBaseURL(result.BaseURL),
			ModelID:          result.ModelID,
			ProviderName:     codegenProviderName,
		})

		// 将 CodeGen 注入到各编程工具的服务商列表中
		a.injectCodeGenModelIntoToolConfigs(result)

		// 启动本地 Anthropic→OpenAI 协议转换代理，供 Claude Code 使用
		go a.ensureCodeGenProxyIfNeeded()

		var msg string
		if len(toolResult.Failed) == 0 {
			msg = "SSO 认证成功，所有工具配置已写入完毕"
		} else {
			failedNames := make([]string, 0, len(toolResult.Failed))
			for _, f := range toolResult.Failed {
				log.Printf("[CodeGen SSO Embedded] WriteAllToolConfigs: %s failed: %v", f.Tool, f.Error)
				failedNames = append(failedNames, f.Tool)
			}
			msg = fmt.Sprintf("SSO 认证成功（注意：%s 配置写入失败，请手动检查）", strings.Join(failedNames, "、"))
		}

		resultCh <- ssoPollingResult{
			info: CodeGenSSOInfo{Message: msg, Email: result.Email},
		}
	}()

	return CodeGenSSOEmbeddedResult{}, nil
}

// WaitCodeGenSSOResult 阻塞等待内嵌 SSO 轮询结果。
// 前端通过 Wails 异步调用此方法。
func (a *App) WaitCodeGenSSOResult() (CodeGenSSOInfo, error) {
	a.ssoPollingMu.Lock()
	session := a.ssoPolling
	a.ssoPollingMu.Unlock()

	if session == nil {
		return CodeGenSSOInfo{}, fmt.Errorf("没有正在进行的 SSO 轮询会话")
	}

	result, ok := <-session.resultCh
	if !ok {
		return CodeGenSSOInfo{}, fmt.Errorf("SSO 轮询会话已关闭")
	}

	if result.err != nil {
		return CodeGenSSOInfo{}, result.err
	}

	return result.info, nil
}

// CancelCodeGenSSOPolling 取消正在进行的内嵌 SSO 轮询。
// 前端在用户关闭/离开 OnboardingWizard 时调用。
func (a *App) CancelCodeGenSSOPolling() {
	a.ssoPollingMu.Lock()
	defer a.ssoPollingMu.Unlock()

	if a.ssoPolling != nil {
		a.ssoPolling.cancel()
		a.ssoPolling = nil
	}
}

// truncateCodeGenStr 截断字符串到 maxLen，超出时追加 "..."。
// 注意：避免与 scheduled_task.go 中的 truncateStr 重名，故加 CodeGen 前缀。
func truncateCodeGenStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

