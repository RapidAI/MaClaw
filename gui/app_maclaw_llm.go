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

	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// MaclawLLMProvider, MaclawLLMConfig — see corelib_aliases.go

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

	endpoint := url + "/v1/messages"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
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
	endpoint := baseURL + "/v1/messages"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("anthropic-version", "2023-06-01")
	if key != "" {
		req.Header.Set("x-api-key", key)
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
		online, err := maclawAnthropicProbe(baseURL+"/v1/messages", key, ua)
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
// the x-api-key header to verify connectivity.
func maclawAnthropicProbe(endpoint, key, userAgent string) (bool, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", userAgent)
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
