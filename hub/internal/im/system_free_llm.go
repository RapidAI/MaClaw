package im

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// SystemLLMSettingsScope maps a tenant id onto a settings repository.
// app.bootstrap wires this to tenant-scoped system_settings keys.
type SystemLLMSettingsScope func(tenantID string, base store.SystemSettingsRepository) store.SystemSettingsRepository

// SystemLLMResolver resolves server-side LLM credentials for Hub IM/agents
// from the reserved system-free service group.
type SystemLLMResolver struct {
	System       store.SystemSettingsRepository
	Scope        SystemLLMSettingsScope
	MaClawClient *llmservice.MaClawProviderClient
}

// ResolveHubLLMConfig returns a direct-call config for system-free when a local
// provider is configured. Returns nil when only maclaw_official is available
// (use Call instead) or when system-free is not ready.
func (r *SystemLLMResolver) ResolveHubLLMConfig(ctx context.Context) *HubLLMConfig {
	if r == nil || r.System == nil {
		return nil
	}
	system := r.scoped(ctx)
	serviceReg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil || serviceReg == nil {
		return nil
	}
	_ = llmservice.EnsureSystemFreeServiceGroup(serviceReg)

	providerReg, err := LoadLLMProviderRegistry(ctx, system)
	if err != nil || providerReg == nil {
		return nil
	}
	group := serviceReg.FindModelServiceGroup(llmservice.SystemFreeServiceGroupID)
	if group == nil {
		return nil
	}
	for _, model := range group.Models {
		for _, pid := range modelProviderIDs(model) {
			if llmservice.IsBuiltinProvider(pid) {
				continue
			}
			p := providerReg.FindProvider(pid)
			if p == nil {
				continue
			}
			if strings.TrimSpace(p.APIURL) == "" || strings.TrimSpace(p.APIKey) == "" {
				continue
			}
			modelName := strings.TrimSpace(p.Model)
			if modelName == "" {
				modelName = strings.TrimSpace(model.Name)
			}
			if modelName == "" {
				modelName = "auto"
			}
			return &HubLLMConfig{
				Enabled:  true,
				APIURL:   strings.TrimSpace(p.APIURL),
				APIKey:   strings.TrimSpace(p.APIKey),
				Model:    modelName,
				Protocol: normalizeStoredProviderProtocol(p.Protocol),
				WireAPI:  normalizeStoredProviderWireAPI(p.WireAPI),
				AgentType: normalizeStoredProviderAgentType(p.AgentType),
			}
		}
	}
	return nil
}

// HasSystemFreeRoute reports whether system-free can be used (local provider
// and/or maclaw official client).
func (r *SystemLLMResolver) HasSystemFreeRoute(ctx context.Context) bool {
	if r == nil {
		return false
	}
	if r.ResolveHubLLMConfig(ctx) != nil {
		return true
	}
	return r.systemFreeUsesMaClaw(ctx) && r.MaClawClient != nil
}

func (r *SystemLLMResolver) systemFreeUsesMaClaw(ctx context.Context) bool {
	system := r.scoped(ctx)
	serviceReg, err := llmservice.LoadRegistry(ctx, system)
	if err != nil || serviceReg == nil {
		return false
	}
	_ = llmservice.EnsureSystemFreeServiceGroup(serviceReg)
	group := serviceReg.FindModelServiceGroup(llmservice.SystemFreeServiceGroupID)
	if group == nil {
		return false
	}
	for _, model := range group.Models {
		for _, pid := range modelProviderIDs(model) {
			if llmservice.IsBuiltinProvider(pid) {
				return true
			}
		}
	}
	return false
}

// Call performs a server-side chat completion via system-free.
// Prefer local provider direct call; otherwise forward through MaClaw Official
// with service_group_id=system-free.
func (r *SystemLLMResolver) Call(ctx context.Context, messages []interface{}, timeout time.Duration) (*agent.LLMSimpleResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("system LLM resolver not configured")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if cfg := r.ResolveHubLLMConfig(ctx); cfg != nil {
		client := &http.Client{Timeout: timeout}
		return agent.DoSimpleLLMRequest(cfg.ToMaclawLLMConfig(), messages, client, timeout)
	}
	if r.MaClawClient == nil || !r.systemFreeUsesMaClaw(ctx) {
		return nil, fmt.Errorf("system-free LLM is not configured")
	}

	// Build OpenAI-compatible body for HubCenter proxy.
	model := "auto"
	bodyObj := map[string]any{
		"model":    model,
		"messages": messages,
	}
	body, err := json.Marshal(bodyObj)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tenantID := TenantIDFromContext(ctx)
	respBody, status, err := r.MaClawClient.Forward(callCtx, body, tenantID, llmservice.SystemFreeServiceGroupID)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		snippet := strings.TrimSpace(string(respBody))
		if len(snippet) > 240 {
			snippet = snippet[:240]
		}
		return nil, fmt.Errorf("system-free maclaw official HTTP %d: %s", status, snippet)
	}
	content, err := extractOpenAIChatContent(respBody)
	if err != nil {
		return nil, err
	}
	return &agent.LLMSimpleResponse{Content: agent.StripThinkingTags(content)}, nil
}

// ConfigProvider returns a HubLLMConfig provider compatible with existing IM
// components. When only maclaw_official is available, returns a non-nil enabled
// marker config so call sites that only check cfg != nil still proceed when
// Call() can succeed. Prefer wiring Call() directly when possible.
func (r *SystemLLMResolver) ConfigProvider(legacy func(context.Context) *HubLLMConfig) func(context.Context) *HubLLMConfig {
	return func(ctx context.Context) *HubLLMConfig {
		if cfg := r.ResolveHubLLMConfig(ctx); cfg != nil {
			return cfg
		}
		// Synthetic marker: enabled but empty URL. Call sites that use
		// ToMaclawLLMConfig + DoSimpleLLMRequest will fail; prefer DoSystemLLM.
		if r.HasSystemFreeRoute(ctx) {
			return &HubLLMConfig{Enabled: true, Model: "auto"}
		}
		if legacy != nil {
			return legacy(ctx)
		}
		return nil
	}
}

func (r *SystemLLMResolver) scoped(ctx context.Context) store.SystemSettingsRepository {
	if r == nil || r.System == nil {
		return nil
	}
	if r.Scope == nil {
		return r.System
	}
	return r.Scope(TenantIDFromContext(ctx), r.System)
}

func modelProviderIDs(model llmservice.ModelServiceModel) []string {
	out := make([]string, 0, len(model.ProviderIDs)+len(model.ProviderConfigs))
	seen := map[string]struct{}{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	for _, id := range model.ProviderIDs {
		add(id)
	}
	for _, cfg := range model.ProviderConfigs {
		add(cfg.ProviderID)
	}
	return out
}

func extractOpenAIChatContent(body []byte) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse system-free response: %w", err)
	}
	if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return "", fmt.Errorf("%s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("no choices in system-free response")
	}
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if text == "" {
		text = strings.TrimSpace(parsed.Choices[0].Message.ReasoningContent)
	}
	if text == "" {
		return "", fmt.Errorf("empty content in system-free response")
	}
	return text, nil
}

// DoSystemLLM is a package-level helper used by IM components. When resolver is
// nil it falls back to cfg + DoSimpleLLMRequest.
func DoSystemLLM(ctx context.Context, resolver *SystemLLMResolver, cfg *HubLLMConfig, messages []interface{}, client *http.Client, timeout time.Duration) (*agent.LLMSimpleResponse, error) {
	if resolver != nil {
		// Prefer resolver when it has a real system-free route (local or maclaw).
		if resolver.HasSystemFreeRoute(ctx) {
			resp, err := resolver.Call(ctx, messages, timeout)
			if err == nil {
				return resp, nil
			}
			log.Printf("[system-free-llm] Call failed: %v; trying direct cfg fallback", err)
		}
	}
	if cfg == nil || !cfg.Enabled {
		return nil, fmt.Errorf("LLM not configured")
	}
	if strings.TrimSpace(cfg.APIURL) == "" || strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("LLM endpoint not configured")
	}
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	return agent.DoSimpleLLMRequest(cfg.ToMaclawLLMConfig(), messages, client, timeout)
}

// Package-level resolver wired by app bootstrap for IM server-side LLM.
var defaultSystemLLMResolver *SystemLLMResolver

// SetSystemLLMResolver installs the process-wide system-free LLM resolver.
func SetSystemLLMResolver(r *SystemLLMResolver) {
	defaultSystemLLMResolver = r
}

// DefaultSystemLLMResolver returns the process-wide resolver (may be nil).
func DefaultSystemLLMResolver() *SystemLLMResolver {
	return defaultSystemLLMResolver
}
