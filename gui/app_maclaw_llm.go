package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/oauth"
)

// MaclawLLMProvider and MaclawLLMConfig are defined in corelib.

const codegenProviderName = "CodeGen"

const legacyHubServiceProviderName = "MaClaw\u6a21\u578b\u670d\u52a1"

const zhipuCodingProviderName = "智谱编程"
const volcengineAgentPlanProviderName = "\u706b\u5c71\u5f15\u64ce Agent Plan"
const legacyVolcengineTokenPlanProviderName = "\u706b\u5c71\u5f15\u64ceTokenPlan"
const legacyVolcengineTokenPlanAnthropicProviderName = "\u706b\u5c71\u5f15\u64ceTokenPlan (Anthropic)"
const legacyVolcengineTokenPlanAnthropicProviderNameAlt = "\u706b\u5c71\u5f15\u64ceTokenPlan Anthropic"

// obsoleteProviderNames lists provider names that have been permanently removed.
// They are stripped from the persisted provider list on load.
var obsoleteProviderNames = map[string]bool{
	"免费":   true,
	"智谱龙虾": true,
	"智谱":   true,
}

var hubServiceProviderNameAliases = map[string]bool{
	hubServiceProviderName:       true,
	"MaClaw Official":            true,
	"MaClaw \u5b98\u65b9":        true,
	"MaClaw\u7039\u6a3b\u67df":   true,
	legacyHubServiceProviderName: true,
}

func isHubServiceProviderName(name string) bool {
	return hubServiceProviderNameAliases[strings.TrimSpace(name)]
}

func canonicalHubServiceProviderName(name string) string {
	if isHubServiceProviderName(name) {
		return hubServiceProviderName
	}
	return name
}

func normalizeMaclawLLMProviders(providers []corelib.MaclawLLMProvider) []corelib.MaclawLLMProvider {
	normalized := make([]corelib.MaclawLLMProvider, 0, len(providers))
	seenHubService := false
	seenVolcengineAgentPlan := false
	hubIndex := -1
	for _, provider := range providers {
		originalName := strings.TrimSpace(provider.Name)
		provider.Name = canonicalVolcengineTokenPlanProviderName(canonicalHubServiceProviderName(provider.Name))
		if provider.Name == hubServiceProviderName {
			provider.IsHubService = true
			if seenHubService {
				if originalName == hubServiceProviderName && hubIndex >= 0 {
					normalized[hubIndex] = provider
				}
				continue
			}
			seenHubService = true
		}
		if provider.Name == volcengineAgentPlanProviderName {
			if seenVolcengineAgentPlan {
				continue
			}
			seenVolcengineAgentPlan = true
		}
		normalized = append(normalized, provider)
		if provider.Name == hubServiceProviderName {
			hubIndex = len(normalized) - 1
		}
	}
	return normalized
}

func maclawLLMProvidersEqual(a, b []corelib.MaclawLLMProvider) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func normalizeLLMTimeoutSec(timeoutSec int) int {
	return corelib.NormalizeAgentTimeoutSec(timeoutSec)
}

func normalizeMaclawLLMProvider(provider corelib.MaclawLLMProvider) corelib.MaclawLLMProvider {
	provider.URL = strings.TrimRight(strings.TrimSpace(provider.URL), "/")
	provider.Key = strings.TrimSpace(provider.Key)
	provider.Model = strings.TrimSpace(provider.Model)
	provider.TimeoutSec = normalizeLLMTimeoutSec(provider.TimeoutSec)
	provider.InputPricePerMTokensRMB = corelib.NormalizeLLMTokenPricePerMTokensRMB(provider.InputPricePerMTokensRMB, corelib.DefaultLLMInputPricePerMTokensRMB)
	provider.OutputPricePerMTokensRMB = corelib.NormalizeLLMTokenPricePerMTokensRMB(provider.OutputPricePerMTokensRMB, corelib.DefaultLLMOutputPricePerMTokensRMB)
	return provider
}

func markHubServiceProvider(provider corelib.MaclawLLMProvider) corelib.MaclawLLMProvider {
	provider.Name = canonicalHubServiceProviderName(provider.Name)
	provider.IsHubService = provider.Name == hubServiceProviderName
	return provider
}

func canonicalVolcengineTokenPlanProviderName(name string) string {
	switch strings.TrimSpace(name) {
	case volcengineAgentPlanProviderName, legacyVolcengineTokenPlanProviderName, legacyVolcengineTokenPlanAnthropicProviderName, legacyVolcengineTokenPlanAnthropicProviderNameAlt:
		return volcengineAgentPlanProviderName
	}
	return name
}

// defaultMaclawLLMProviders returns the built-in provider list.
func defaultMaclawLLMProviders() []corelib.MaclawLLMProvider {
	return []corelib.MaclawLLMProvider{
		{Name: "OpenAI", URL: "https://chatgpt.com/backend-api/codex", Model: oauth.CodexSubscriptionDefaultModel, AuthType: "oauth", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, WireAPI: "responses-ws"},
		{Name: "Anthropic", URL: "https://api.anthropic.com", Model: "claude-sonnet-4-5-20250514", AuthType: "oauth", Protocol: "anthropic", ContextLength: 200000, TimeoutSec: corelib.DefaultLLMTimeoutSec},
		{Name: "GitHub Copilot", URL: "https://api.githubcopilot.com", Model: "claude-sonnet-4", AuthType: "oauth", ContextLength: 200000, TimeoutSec: corelib.DefaultLLMTimeoutSec},
		{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash", ContextLength: 400000, TimeoutSec: corelib.DefaultLLMTimeoutSec},
		// xAI's Grok Build source uses grok-build with a 256K context window.
		// The public xAI endpoint is OpenAI-compatible, so it uses the standard
		// OpenAI request and model-discovery path.
		{Name: "xAI-Grok", URL: "https://api.x.ai/v1", Model: "grok-build", Protocol: "openai", ContextLength: 256000, TimeoutSec: corelib.DefaultLLMTimeoutSec},
		{Name: zhipuCodingProviderName, URL: "https://open.bigmodel.cn/api/anthropic", Model: "GLM-5.2", Protocol: "anthropic", AgentType: "claude code 2.0", ContextLength: 400000, TimeoutSec: corelib.DefaultLLMTimeoutSec},
		{Name: "MiniMax", URL: "https://api.minimaxi.com/v1", Model: "MiniMax-M2.7", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec},
		{Name: "Kimi", URL: "https://api.kimi.com/coding/v1", Model: "kimi-for-coding", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AgentType: "claude code 2.0"},
		{Name: volcengineAgentPlanProviderName, URL: "https://ark.cn-beijing.volces.com/api/plan/v3", Model: "glm-5.2", Protocol: "openai", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, WireAPI: "responses"},
		{Name: "讯飞星辰", URL: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", Model: "astron-code-latest", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec},
		{Name: "Custom1", URL: "", Model: "", IsCustom: true, TimeoutSec: corelib.DefaultLLMTimeoutSec},
		{Name: "Custom2", URL: "", Model: "", IsCustom: true, TimeoutSec: corelib.DefaultLLMTimeoutSec},
	}
}

// GetMaclawLLMProviders returns the provider list and current selection.
func (a *App) GetMaclawLLMProviders() struct {
	Providers []corelib.MaclawLLMProvider `json:"providers"`
	Current   string                      `json:"current"`
} {
	cfg, err := a.LoadConfig()
	if err != nil {
		defaults := defaultMaclawLLMProviders()
		return struct {
			Providers []corelib.MaclawLLMProvider `json:"providers"`
			Current   string                      `json:"current"`
		}{Providers: defaults, Current: defaults[0].Name}
	}
	rawProviders := a.syncedMaclawLLMProviders(cfg)
	missingTimeout := make(map[string]bool, len(rawProviders))
	for _, provider := range rawProviders {
		if provider.TimeoutSec <= 0 {
			missingTimeout[canonicalHubServiceProviderName(strings.TrimSpace(provider.Name))] = true
		}
	}
	providers := normalizeMaclawLLMProviders(rawProviders)
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
			if cfg.MaclawLLMTimeoutSec > 0 {
				providers[0].TimeoutSec = cfg.MaclawLLMTimeoutSec
			}
		}
	}
	// Backfill context_length for known providers that predate this field.
	// Also sync URL for non-custom preset providers (e.g. port change).
	defaults := defaultMaclawLLMProviders()
	defaultCtx := make(map[string]int, len(defaults))
	defaultTimeout := make(map[string]int, len(defaults))
	defaultURL := make(map[string]string, len(defaults))
	defaultProvider := make(map[string]corelib.MaclawLLMProvider, len(defaults))
	for _, d := range defaults {
		defaultProvider[d.Name] = d
		if d.ContextLength > 0 {
			defaultCtx[d.Name] = d.ContextLength
		}
		if d.TimeoutSec > 0 {
			defaultTimeout[d.Name] = d.TimeoutSec
		}
		if !d.IsCustom {
			defaultURL[d.Name] = d.URL
		}
	}
	// Remove obsolete providers that have been permanently retired.
	{
		n := 0
		for _, p := range providers {
			if !obsoleteProviderNames[p.Name] {
				providers[n] = p
				n++
			}
		}
		providers = providers[:n]
	}
	current := canonicalVolcengineTokenPlanProviderName(canonicalHubServiceProviderName(cfg.MaclawLLMCurrentProvider))
	for i := range providers {
		providers[i].Name = canonicalVolcengineTokenPlanProviderName(providers[i].Name)
		if providers[i].ContextLength == 0 {
			if cl, ok := defaultCtx[providers[i].Name]; ok {
				providers[i].ContextLength = cl
			}
		}
		if providers[i].TimeoutSec <= 0 || (missingTimeout[canonicalHubServiceProviderName(providers[i].Name)] && canonicalHubServiceProviderName(providers[i].Name) == current && cfg.MaclawLLMTimeoutSec > 0) {
			if canonicalHubServiceProviderName(providers[i].Name) == current && cfg.MaclawLLMTimeoutSec > 0 {
				providers[i].TimeoutSec = cfg.MaclawLLMTimeoutSec
			} else if ts, ok := defaultTimeout[providers[i].Name]; ok {
				providers[i].TimeoutSec = ts
			} else {
				providers[i].TimeoutSec = corelib.DefaultLLMTimeoutSec
			}
		}
		providers[i] = markHubServiceProvider(normalizeMaclawLLMProvider(providers[i]))
		if providers[i].Name == codegenProviderName && providers[i].AuthType == "sso" {
			providers[i] = corelib.NormalizeCodeGenSSOProvider(providers[i])
			continue
		}
		// Keep preset provider URLs in sync (handles port changes etc.)
		if !providers[i].IsCustom {
			if u, ok := defaultURL[providers[i].Name]; ok {
				providers[i].URL = u
			}
			if providers[i].Name == volcengineAgentPlanProviderName {
				if d, ok := defaultProvider[providers[i].Name]; ok {
					providers[i].Model = d.Model
					providers[i].Protocol = d.Protocol
					providers[i].AgentType = d.AgentType
					providers[i].WireAPI = d.WireAPI
				}
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
		updated := make([]corelib.MaclawLLMProvider, 0, len(providers)+1)
		updated = append(updated, providers[:insertAt]...)
		updated = append(updated, d)
		updated = append(updated, providers[insertAt:]...)
		providers = updated
	}
	// Migrate renamed Hub service provider: "MaClaw模型服务" → "MaClaw官方"
	current = canonicalHubServiceProviderName(current)
	// Migrate: if current provider no longer exists in the list (e.g. "免费"
	// was removed), fall back to the first available provider.
	if current != "" {
		found := false
		for _, p := range providers {
			if p.Name == current {
				found = true
				break
			}
		}
		if !found {
			current = ""
		}
	}
	if current == "" {
		current = providers[0].Name
	}
	// OAuth/SSO credentials live in the independent credential store. Hydrate
	// Key only for managed-auth providers so the UI can show "OAuth authenticated"
	// and model-list fetch can pass a token without a visible API Key field.
	for i := range providers {
		kind := normalizeMaclawLLMAuthTypeKind(providers[i].AuthType)
		if !kind.IsOAuth() && providers[i].AuthType != "sso" {
			continue
		}
		if storeKey := a.resolveProviderKeyFromStore(providers[i]); storeKey != "" {
			providers[i].Key = storeKey
		}
	}
	return struct {
		Providers []corelib.MaclawLLMProvider `json:"providers"`
		Current   string                      `json:"current"`
	}{Providers: providers, Current: current}
}

// GetMaclawLLMPanelState returns the settings needed by the desktop LLM panel
// in a single config read to avoid timeout/race issues from multiple parallel
// Wails calls contending on configMu.
func (a *App) GetMaclawLLMPanelState() struct {
	Providers           []corelib.MaclawLLMProvider `json:"providers"`
	Current             string                      `json:"current"`
	MaxIterations       int                         `json:"max_iterations"`
	TrajectoryLogging   bool                        `json:"trajectory_logging"`
	TrialReflectEnabled bool                        `json:"trial_reflect_enabled"`
} {
	cfg, err := a.LoadConfig()
	if err != nil {
		defaults := defaultMaclawLLMProviders()
		return struct {
			Providers           []corelib.MaclawLLMProvider `json:"providers"`
			Current             string                      `json:"current"`
			MaxIterations       int                         `json:"max_iterations"`
			TrajectoryLogging   bool                        `json:"trajectory_logging"`
			TrialReflectEnabled bool                        `json:"trial_reflect_enabled"`
		}{
			Providers:           defaults,
			Current:             defaults[0].Name,
			MaxIterations:       config.MaxAgentIterationsCap,
			TrajectoryLogging:   false,
			TrialReflectEnabled: false,
		}
	}
	providerState := a.GetMaclawLLMProviders()
	// Use the single source of truth for configured → effective conversion.
	maxIter := config.EffectiveMaxIterations(cfg.MaclawAgentMaxIterations)
	return struct {
		Providers           []corelib.MaclawLLMProvider `json:"providers"`
		Current             string                      `json:"current"`
		MaxIterations       int                         `json:"max_iterations"`
		TrajectoryLogging   bool                        `json:"trajectory_logging"`
		TrialReflectEnabled bool                        `json:"trial_reflect_enabled"`
	}{
		Providers:           providerState.Providers,
		Current:             providerState.Current,
		MaxIterations:       maxIter,
		TrajectoryLogging:   cfg.LLMTrajectoryLogging,
		TrialReflectEnabled: cfg.TrialReflectEnabled,
	}
}

// SaveMaclawLLMProviders persists the provider list and current selection.
func (a *App) SaveMaclawLLMProviders(providers []corelib.MaclawLLMProvider, current string) error {
	current = canonicalVolcengineTokenPlanProviderName(canonicalHubServiceProviderName(current))
	providers = normalizeMaclawLLMProviders(providers)
	start := time.Now()
	log.Printf("[LLM] SaveMaclawLLMProviders:start current=%s providers=%d", current, len(providers))
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[LLM] SaveMaclawLLMProviders:load_config_failed after=%s err=%v", time.Since(start), err)
		return fmt.Errorf("load config: %w", err)
	}
	log.Printf("[LLM] SaveMaclawLLMProviders:load_config=%s", time.Since(start))
	cfg.MaclawLLMUrl = ""
	cfg.MaclawLLMKey = ""
	cfg.MaclawLLMModel = ""
	cfg.MaclawLLMProtocol = ""
	cfg.MaclawLLMContextLength = 0
	cfg.MaclawLLMTimeoutSec = 0
	for i := range providers {
		providers[i] = markHubServiceProvider(normalizeMaclawLLMProvider(providers[i]))
		providers[i] = corelib.NormalizeCodeGenSSOProvider(providers[i])
	}
	// OAuth/SSO tokens are managed internally (credential store + config). If the
	// frontend saves with an empty Key (UI never shows a key field for OAuth),
	// preserve the existing secrets so a model/settings save cannot wipe login.
	providers = preserveManagedAuthSecrets(providers, cfg.MaclawLLMProviders)
	if current == hubServiceProviderName {
		hasHubProvider := false
		var hubStatus HubLLMServiceStatus
		haveHubStatus := false
		for _, p := range providers {
			if p.Name == hubServiceProviderName {
				hasHubProvider = true
				break
			}
		}
		if !hasHubProvider && strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != "" {
			syncCfg := cfg
			syncCfg.MaclawLLMProviders = providers
			syncCfg.MaclawLLMCurrentProvider = current
			if status, err := a.fetchHubLLMServiceStatusWithTimeout(syncCfg, hubServiceStatusTimeout); err == nil {
				hubStatus = status
				haveHubStatus = true
				// Update local status cache so sidebar sees fresh state.
				storeHubServiceStatusCache(syncCfg.RemoteHubURL, syncCfg.RemoteViewerToken, status)
				a.applyHubLLMServiceStatusToConfig(&syncCfg, status)
				providers = syncCfg.MaclawLLMProviders
			} else {
				log.Printf("[LLM] SaveMaclawLLMProviders:hub_provider_sync_failed err=%v", err)
			}
		}
		if !hasHubProvider {
			for _, p := range providers {
				if p.Name == hubServiceProviderName {
					hasHubProvider = true
					break
				}
			}
		}
		if !hasHubProvider {
			if haveHubStatus {
				return fmt.Errorf("%s", hubLLMServiceMissingProviderMessage(hubStatus))
			}
			return fmt.Errorf("MaClaw 官方服务商暂不可用：请刷新 Hub 服务状态后重试")
		}
	}
	cfg.MaclawLLMProviders = providers
	cfg.MaclawLLMCurrentProvider = current
	for _, p := range providers {
		if p.Name == current {
			cfg.MaclawLLMUrl = strings.TrimRight(strings.TrimSpace(p.URL), "/")
			cfg.MaclawLLMKey = strings.TrimSpace(p.Key)
			cfg.MaclawLLMModel = strings.TrimSpace(p.Model)
			cfg.MaclawLLMProtocol = p.Protocol
			cfg.MaclawLLMContextLength = p.ContextLength
			cfg.MaclawLLMTimeoutSec = p.TimeoutSec
			break
		}
	}
	persistStart := time.Now()
	if err := a.PatchConfig(func(currentCfg *corelib.AppConfig) {
		currentCfg.MaclawLLMUrl = cfg.MaclawLLMUrl
		currentCfg.MaclawLLMKey = cfg.MaclawLLMKey
		currentCfg.MaclawLLMModel = cfg.MaclawLLMModel
		currentCfg.MaclawLLMProtocol = cfg.MaclawLLMProtocol
		currentCfg.MaclawLLMContextLength = cfg.MaclawLLMContextLength
		currentCfg.MaclawLLMTimeoutSec = cfg.MaclawLLMTimeoutSec
		currentCfg.MaclawLLMProviders = cfg.MaclawLLMProviders
		currentCfg.MaclawLLMCurrentProvider = cfg.MaclawLLMCurrentProvider
	}); err != nil {
		log.Printf("[LLM] SaveMaclawLLMProviders:save_config_failed after=%s err=%v", time.Since(persistStart), err)
		return err
	}
	log.Printf("[LLM] SaveMaclawLLMProviders:save_config=%s", time.Since(persistStart))
	// Provider switch ends sticky multi-model council for this session.
	a.clearMoAStickyForAllUsers()
	if a.ctx != nil {
		a.emitEvent("llm-token-usage-changed", current)
		a.emitEvent("moa-session-changed", nil)
	}
	// Invalidate LLM-dependent tool outcome records when the provider changes.
	// PatchConfig (used above) does not go through PatchConfigFields, so the
	// invalidation hook there is not triggered. We must fire it explicitly.
	if tracker := a.usageTracker; tracker != nil {
		for _, toolName := range []string{"craft_tool", "delegate_task", "ask_user"} {
			tracker.InvalidateOutcomes(toolName, "llm_provider_changed")
		}
	}
	// Immediately notify Hub of the LLM configuration change via heartbeat
	// so the Hub-side llm_configured flag is updated without waiting for the
	// next periodic heartbeat cycle.
	a.refreshMemoryEvolutionLLM()
	go a.notifyHubLLMConfigChanged()
	log.Printf("[LLM] SaveMaclawLLMProviders:done total=%s", time.Since(start))
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
// MaterializeProviderByName returns a full MaclawLLMConfig for a named provider
// using the same OAuth/CredentialStore path as GetMaclawLLMConfig (K19).
func (a *App) MaterializeProviderByName(providerName string) (corelib.MaclawLLMConfig, error) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("provider name required")
	}
	data := a.GetMaclawLLMProviders()
	for _, p := range data.Providers {
		if !strings.EqualFold(strings.TrimSpace(p.Name), providerName) {
			continue
		}
		authKind := normalizeMaclawLLMAuthTypeKind(p.AuthType)
		wireAPI := p.WireAPI
		if wireAPI == "" && authKind.IsOAuth() {
			wireAPI = "responses-ws"
		}
		key := p.Key
		if authKind.IsOAuth() {
			if storeKey := a.resolveProviderKeyFromStore(p); storeKey != "" {
				key = storeKey
			} else if p.OAuthAccessToken != "" && !strings.HasPrefix(p.Key, "sk-") {
				key = p.OAuthAccessToken
			}
		}
		return a.withGlobalThinkingMode(corelib.MaclawLLMConfig{
			URL:             p.URL,
			Key:             key,
			Model:           p.Model,
			Protocol:        p.Protocol,
			ContextLength:   p.ContextLength,
			TimeoutSec:      normalizeLLMTimeoutSec(p.TimeoutSec),
			MaxOutputTokens: p.MaxOutputTokens,
			SupportsVision:  p.SupportsVision,
			AgentType:       p.AgentType,
			WireAPI:         wireAPI,
			ProviderName:    p.Name,
			AuthType:        p.AuthType,
		}), nil
	}
	return corelib.MaclawLLMConfig{}, fmt.Errorf("provider %q not found", providerName)
}

func (a *App) GetMaclawLLMConfig() corelib.MaclawLLMConfig {
	// Use GetMaclawLLMProviders which applies URL sync for preset providers
	// (e.g. port changes), instead of reading legacy fields directly.
	data := a.GetMaclawLLMProviders()
	for _, p := range data.Providers {
		if p.Name == data.Current {
			authKind := normalizeMaclawLLMAuthTypeKind(p.AuthType)
			wireAPI := p.WireAPI
			if wireAPI == "" && authKind.IsOAuth() {
				wireAPI = "responses-ws"
			}
			// ChatGPT Codex subscription endpoints authenticate with the raw OAuth JWT.
			// Primary: read from independent CredentialStore (Pi-aligned).
			// Fallback: config.json fields.
			key := p.Key
			if authKind.IsOAuth() {
				if storeKey := a.resolveProviderKeyFromStore(p); storeKey != "" {
					key = storeKey
				} else if p.OAuthAccessToken != "" && !strings.HasPrefix(p.Key, "sk-") {
					key = p.OAuthAccessToken
				}
			}
			// Diagnostic log for OAuth token debugging
			if authKind.IsOAuth() {
				oatLen := len(p.OAuthAccessToken)
				keyLen := len(p.Key)
				keyPfx := p.Key
				if len(keyPfx) > 10 {
					keyPfx = keyPfx[:10]
				}
				oatPfx := p.OAuthAccessToken
				if len(oatPfx) > 10 {
					oatPfx = oatPfx[:10]
				}
				log.Printf("[LLM] GetMaclawLLMConfig oauth: wire_api=%s key_pfx=%s(%d) oat_pfx=%s(%d) auth=%s",
					wireAPI, keyPfx, keyLen, oatPfx, oatLen, p.AuthType)
			}
			return a.withGlobalThinkingMode(corelib.MaclawLLMConfig{
				URL:             p.URL,
				Key:             key,
				Model:           p.Model,
				Protocol:        p.Protocol,
				ContextLength:   p.ContextLength,
				TimeoutSec:      normalizeLLMTimeoutSec(p.TimeoutSec),
				MaxOutputTokens: p.MaxOutputTokens,
				SupportsVision:  p.SupportsVision,
				AgentType:       p.AgentType,
				WireAPI:         wireAPI,
				ProviderName:    p.Name,
				AuthType:        p.AuthType,
			})
		}
	}
	return corelib.MaclawLLMConfig{}
}

// isMaclawLLMConfigured returns true if the current MaClaw LLM selection
// resolves to a usable URL and model.
func (a *App) isMaclawLLMConfigured() bool {
	cfg := a.GetMaclawLLMConfig()
	return strings.TrimSpace(cfg.URL) != "" && strings.TrimSpace(cfg.Model) != ""
}

// SetMaclawLLMCurrentModel updates only the model id/name for the currently
// selected MaClaw LLM provider. Unlike SaveMaclawLLMProviders, this does not
// clear session multi-model council sticky state.
func (a *App) SetMaclawLLMCurrentModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return fmt.Errorf("model is required")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		currentRaw := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
		currentCanon := canonicalVolcengineTokenPlanProviderName(canonicalHubServiceProviderName(currentRaw))
		for i := range cfg.MaclawLLMProviders {
			pName := strings.TrimSpace(cfg.MaclawLLMProviders[i].Name)
			pCanon := canonicalVolcengineTokenPlanProviderName(canonicalHubServiceProviderName(pName))
			if pName != currentRaw && (currentCanon == "" || pCanon != currentCanon) {
				continue
			}
			cfg.MaclawLLMProviders[i].Model = model
			has := false
			for _, m := range cfg.MaclawLLMProviders[i].Models {
				if strings.TrimSpace(m) == model {
					has = true
					break
				}
			}
			if !has {
				cfg.MaclawLLMProviders[i].Models = append(cfg.MaclawLLMProviders[i].Models, model)
			}
			break
		}
		// Always keep the legacy flat field aligned (used by some status paths).
		cfg.MaclawLLMModel = model
	}); err != nil {
		return err
	}
	if a.ctx != nil {
		a.emitEvent("llm-token-usage-changed", model)
	}
	a.refreshMemoryEvolutionLLM()
	return nil
}

// isProMode returns true if the UI is in "pro" mode (full coding tools).
// In lite/simple mode, coding session tools are not available because the
// user has not configured coding LLM providers.
func (a *App) isProMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return normalizeUIModeKind(cfg.UIMode).IsProDefault()
}

// SaveMaclawLLMConfig persists the MaClaw LLM configuration.
func (a *App) SaveMaclawLLMConfig(llm corelib.MaclawLLMConfig) error {
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.MaclawLLMUrl = strings.TrimRight(strings.TrimSpace(llm.URL), "/")
		cfg.MaclawLLMKey = strings.TrimSpace(llm.Key)
		cfg.MaclawLLMModel = strings.TrimSpace(llm.Model)
		cfg.MaclawLLMProtocol = llm.Protocol
		cfg.MaclawLLMContextLength = llm.ContextLength
		cfg.MaclawLLMTimeoutSec = normalizeLLMTimeoutSec(llm.TimeoutSec)
	}); err != nil {
		return err
	}
	a.refreshMemoryEvolutionLLM()
	a.notifyHubLLMConfigChanged()
	return nil
}

// StartOpenAIOAuth starts the OpenAI OAuth PKCE flow. On success, it updates
// the OpenAI provider config with the obtained tokens and persists the change.
// The flow can be cancelled via CancelOpenAIOAuth.
func (a *App) StartOpenAIOAuth() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	a.oauthMu.Lock()
	a.oauthCancel = cancel
	a.oauthMu.Unlock()
	defer func() {
		cancel()
		a.oauthMu.Lock()
		a.oauthCancel = nil
		a.oauthMu.Unlock()
	}()

	cfg := oauth.DefaultConfig()
	result, err := oauth.RunOAuthFlowCtx(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("OAuth 登录失败: %w", err)
	}

	// Update the OpenAI provider with the obtained tokens and sync config
	data := a.GetMaclawLLMProviders()
	defaults := defaultMaclawLLMProviders()
	var defaultOpenAI *corelib.MaclawLLMProvider
	for i := range defaults {
		if defaults[i].Name == "OpenAI" && normalizeMaclawLLMAuthTypeKind(defaults[i].AuthType).IsOAuth() {
			defaultOpenAI = &defaults[i]
			break
		}
	}
	for i, p := range data.Providers {
		if p.Name == "OpenAI" && normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			data.Providers[i] = oauth.ApplyTokenResult(p, result)
			// Sync URL, Model, WireAPI to latest defaults so stale config doesn't linger
			if defaultOpenAI != nil {
				data.Providers[i].URL = defaultOpenAI.URL
				data.Providers[i].Model = defaultOpenAI.Model
				data.Providers[i].WireAPI = defaultOpenAI.WireAPI
			}
			if err := a.SaveMaclawLLMProviders(data.Providers, "OpenAI"); err != nil {
				return "", fmt.Errorf("保存 OAuth 配置失败: %w", err)
			}
			// Also save to independent credential store (Pi-aligned)
			a.saveOAuthResultToStore("OpenAI", result)
			return "OpenAI OAuth 登录成功", nil
		}
	}
	return "", fmt.Errorf("未找到 OpenAI provider")
}

// CancelOpenAIOAuth cancels an in-progress OAuth flow, unblocking StartOpenAIOAuth.
func (a *App) CancelOpenAIOAuth() {
	a.oauthMu.Lock()
	defer a.oauthMu.Unlock()
	if a.oauthCancel != nil {
		a.oauthCancel()
	}
}

// ImportCodexAuth imports credentials from Codex CLI's ~/.codex/auth.json.
// It supports two modes:
//  1. Direct API key: reads OPENAI_API_KEY field
//  2. ChatGPT OAuth tokens: reads tokens.id_token and exchanges it for an API key
//     via OpenAI's token exchange endpoint (same as Codex CLI's obtain_api_key)
//
// This is a fallback for users who can login via Codex but not via maclaw's OAuth
// (e.g. due to network/proxy issues with auth.openai.com).
func (a *App) ImportCodexAuth() (string, error) {
	auth, err := configfile.ReadCodexAuth()
	if err != nil {
		return "", fmt.Errorf("读取 Codex 认证信息失败: %w", err)
	}
	if auth == nil {
		return "", fmt.Errorf("未找到 Codex 认证信息，请先在 Codex CLI 中执行 codex login")
	}

	var apiKey string
	var source string

	// Strategy 1: Direct API key
	if key, _ := auth["OPENAI_API_KEY"].(string); key != "" {
		apiKey = key
		source = "API Key"
	}

	// Strategy 2: ChatGPT OAuth tokens → 直接使用 access_token（Responses API 接受 OAuth token）
	if apiKey == "" {
		if tokens, ok := auth["tokens"].(map[string]interface{}); ok {
			if accessToken, _ := tokens["access_token"].(string); accessToken != "" {
				apiKey = accessToken
				source = "ChatGPT access_token"
			}
		}
	}

	if apiKey == "" {
		return "", fmt.Errorf("Codex auth.json 中未找到可用的认证信息（无 API Key 也无 OAuth tokens）")
	}

	data := a.GetMaclawLLMProviders()
	for i, p := range data.Providers {
		if p.Name == "OpenAI" && normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			data.Providers[i].Key = apiKey
			if err := a.SaveMaclawLLMProviders(data.Providers, "OpenAI"); err != nil {
				return "", fmt.Errorf("保存配置失败: %w", err)
			}
			return fmt.Sprintf("已从 Codex CLI 导入（%s）", source), nil
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
		if p.Name == data.Current && normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
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
		if p.Name == data.Current && normalizeMaclawLLMAuthTypeKind(p.AuthType).IsOAuth() {
			// Primary path: use CredentialStore (serialized, independent of configMu)
			if a.credentialStore != nil && credentialStoreProviderID(p) != "" {
				return a.ensureOAuthTokenViaStore(p, i)
			}
			// Fallback path: legacy config.json-based refresh
			cfg := oauth.DefaultConfig()
			updated, err := oauth.EnsureValidToken(p, cfg, func(up corelib.MaclawLLMProvider) error {
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
// After a successful text test, it also probes vision support synchronously.
func (a *App) TestMaclawLLM(llm corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
	log.Printf("[LLM] TestMaclawLLM: agent_type=%q user_agent=%q", llm.AgentType, llm.UserAgent())
	if err := a.ensureOAuthToken(); err != nil {
		return corelib.MaclawLLMTestResult{}, fmt.Errorf("OAuth token refresh failed: %w", err)
	}

	url := strings.TrimRight(strings.TrimSpace(llm.URL), "/")
	if url == "" {
		return corelib.MaclawLLMTestResult{}, fmt.Errorf("LLM URL is not configured")
	}
	key := strings.TrimSpace(llm.Key)
	model := strings.TrimSpace(llm.Model)
	if model == "" {
		return corelib.MaclawLLMTestResult{}, fmt.Errorf("model name is not configured")
	}

	protocol := strings.TrimSpace(llm.Protocol)
	var textResult string
	var err error
	if llm.IsResponsesAPI() {
		textResult, err = a.testResponsesAPILLM(url, key, model, llm.UserAgent(), llm.ProviderName)
	} else if protocol == "anthropic" {
		textResult, err = a.testAnthropicLLM(url, key, model, llm.UserAgent())
	} else {
		textResult, err = a.testOpenAILLM(url, key, model, llm.UserAgent())
	}
	if err != nil {
		return corelib.MaclawLLMTestResult{}, err
	}

	log.Printf("[LLM] TestMaclawLLM text_test_ok model=%s protocol=%s", model, protocol)
	vision := false
	if llm.IsResponsesAPI() {
		vision = probeVisionResponsesAPI(url, key, model, llm.UserAgent())
	} else {
		vision = probeVisionSupport(url, key, model, protocol, llm.UserAgent())
	}
	log.Printf("[LLM] vision probe for %s: supports_vision=%v", model, vision)

	return corelib.MaclawLLMTestResult{
		Message:        textResult,
		SupportsVision: vision,
	}, nil
}

// testOpenAILLM tests an OpenAI-compatible endpoint.
func (a *App) testOpenAILLM(url, key, model, userAgent string) (string, error) {
	cfg := corelib.MaclawLLMConfig{URL: url, Key: key, Model: model, AgentType: userAgent}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	client := &http.Client{Timeout: 30 * time.Second}

	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "provider-test"})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, 30*time.Second)
	if err != nil {
		msg := err.Error()
		msg = strings.TrimPrefix(msg, "HTTP 500: ")
		msg = strings.TrimPrefix(msg, "llm error: ")
		msg = strings.TrimPrefix(msg, "parse response: ")
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return "", fmt.Errorf("%s", msg)
	}
	if resp == nil || resp.Content == "" {
		return "", fmt.Errorf("no response from model")
	}
	return stripFunctionCalls(stripThinkTags(resp.Content)), nil
}

// testAnthropicLLM tests an Anthropic Messages API endpoint.
func (a *App) testAnthropicLLM(url, key, model, userAgent string) (string, error) {
	cfg := corelib.MaclawLLMConfig{URL: url, Key: key, Model: model, Protocol: "anthropic", AgentType: userAgent}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	client := &http.Client{Timeout: 30 * time.Second}

	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "provider-test"})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, 30*time.Second)
	if err != nil {
		msg := err.Error()
		msg = strings.TrimPrefix(msg, "HTTP 500: ")
		msg = strings.TrimPrefix(msg, "parse response: ")
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return "", fmt.Errorf("%s", msg)
	}
	if resp == nil || resp.Content == "" {
		return "", fmt.Errorf("no response from model")
	}
	return stripFunctionCalls(stripThinkTags(resp.Content)), nil
}

// testResponsesAPILLM tests an OpenAI Responses API endpoint.
func (a *App) testResponsesAPILLM(url, key, model, userAgent, providerName string) (string, error) {
	cfg := corelib.MaclawLLMConfig{URL: url, Key: key, Model: model, AgentType: userAgent, WireAPI: "responses", ProviderName: providerName}
	messages := []interface{}{map[string]interface{}{"role": "user", "content": "hello"}}
	client := &http.Client{Timeout: 30 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "provider-test"})
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		return "", acquireErr
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()

	req, _, endpoint, err := llm.NewResponsesAPIRequest(scheduledCtx, cfg, messages, llm.ResponsesAPIRequestOptions{
		Stream: false,
	})
	if err != nil {
		return "", err
	}
	log.Printf("[LLM] TestResponsesAPI POST %s model=%s", endpoint, model)

	resp, err := client.Do(req)
	globalLLMScheduler.ObserveResult(trace, err)
	if err != nil {
		return "", fmt.Errorf("[%s] %w", endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := classifyResponsesAPIHTTPError(resp.StatusCode, body, endpoint, model, cfg.ProviderName)
		err := fmt.Errorf("%s", msg)
		globalLLMScheduler.ObserveResult(trace, err)
		return "", err
	}

	parsed, err := llm.ParseNonStreamResponsesAPIResponse(resp)
	globalLLMScheduler.ObserveResult(trace, err)
	if err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("no response from model")
	}
	return stripFunctionCalls(stripThinkTags(parsed.Choices[0].Message.Content)), nil
}

// probeVisionSupport sends a tiny 4x4 red PNG as an image_url message to the
// LLM and returns true if the model responds successfully (i.e. supports vision).
// This is a best-effort probe — network errors or timeouts return false.
func probeVisionSupport(baseURL, key, model, protocol, userAgent string) bool {
	if protocol == "anthropic" {
		return probeVisionAnthropic(baseURL, key, model, visionProbeRedPNG, userAgent)
	}
	return probeVisionOpenAI(baseURL, key, model, visionProbeRedPNG, userAgent)
}

// visionProbeRedPNG is a 4x4 solid red (#FF0000) PNG used by all vision probes.
// We use 4x4 instead of 1x1 because some models (e.g. Xiaomi MiMo-V2.5)
// misidentify a single-pixel image's colour while correctly recognising a
// slightly larger one.
const visionProbeRedPNG = "iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAIAAAAmkwkpAAAAFUlEQVR4nGL5z4AATAxEcQABAAD//zRtAQqfxpGAAAAAAElFTkSuQmCC"

func probeVisionOpenAI(baseURL, key, model, imgB64, userAgent string) bool {
	cfg := corelib.MaclawLLMConfig{URL: baseURL, Key: key, Model: model, AgentType: userAgent}
	messages := []interface{}{
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
	}
	client := &http.Client{Timeout: 35 * time.Second}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "vision-probe"})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, 30*time.Second)
	if err != nil {
		log.Printf("[LLM] vision probe OpenAI error: %v", err)
		return false
	}
	if resp == nil {
		return false
	}
	ok := resp.Content != "" && looksLikeVisionResponse(resp.Content)
	if !ok && resp.Content != "" {
		log.Printf("[LLM] vision probe OpenAI: model replied %q, looksLikeVision=false", truncateForLog(resp.Content, 120))
	}
	return ok
}

// looksLikeVisionResponse checks whether the model's reply indicates it actually
// "saw" the test image.  The probe sends a solid-red image and asks for the colour.
//
// Two-layer detection:
//  1. Positive signal: the reply mentions ANY colour name → the model perceived
//     the image and attempted to describe it.  Some models misidentify the exact
//     colour of a tiny image (e.g. "yellow" instead of "red"), but the fact that
//     they name a colour at all proves vision capability.
//  2. Negative signal: the reply says "no image" / "can't see" / "没有图片" etc.
//     → the model did NOT receive or process the image data.
//
// Result: positive && !negative → true (supports vision).
func looksLikeVisionResponse(content string) bool {
	lower := strings.ToLower(content)

	// Negative signals: model explicitly says it cannot see an image.
	negatives := []string{
		"no image", "don't see", "can't see", "cannot see",
		"not see", "no picture", "not provided", "not attached",
		"没有图片", "看不到", "无法看到", "未提供", "没有提供",
		"no visual", "not visible",
	}
	for _, neg := range negatives {
		if strings.Contains(lower, neg) {
			return false
		}
	}

	// Positive signals: model names a colour → it perceived the image.
	colours := []string{
		"red", "blue", "green", "yellow", "orange", "pink", "purple",
		"white", "black", "gray", "grey", "brown", "cyan", "magenta",
		"红", "蓝", "绿", "黄", "橙", "粉", "紫", "白", "黑", "灰", "棕",
	}
	for _, c := range colours {
		if strings.Contains(lower, c) {
			return true
		}
	}

	return false
}

func probeVisionAnthropic(baseURL, key, model, imgB64, userAgent string) bool {
	cfg := corelib.MaclawLLMConfig{URL: baseURL, Key: key, Model: model, Protocol: "anthropic", AgentType: userAgent}
	messages := []interface{}{
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
	}
	client := &http.Client{Timeout: 35 * time.Second}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "vision-probe"})
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, 30*time.Second)
	if err != nil {
		log.Printf("[LLM] vision probe Anthropic error: %v", err)
		return false
	}
	if resp == nil {
		return false
	}
	ok := resp.Content != "" && looksLikeVisionResponse(resp.Content)
	if !ok && resp.Content != "" {
		log.Printf("[LLM] vision probe Anthropic: model replied %q, looksLikeVision=false", truncateForLog(resp.Content, 120))
	}
	return ok
}

// probeVisionResponsesAPI sends a tiny 4x4 PNG via the Responses API format
// and returns true if the model responds with a vision-aware answer.
func probeVisionResponsesAPI(baseURL, key, model, userAgent string) bool {
	client := &http.Client{Timeout: 35 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "vision-probe"})
	lease, trace, acquireErr := acquireLLMSchedulerLease(ctx)
	if acquireErr != nil {
		log.Printf("[LLM] vision probe ResponsesAPI scheduler error: %v", acquireErr)
		return false
	}
	defer lease.Release()
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	defer scheduledCancel()

	endpoint := strings.TrimRight(baseURL, "/")
	endpoint = llm.BuildResponsesEndpoint(endpoint)
	probeCfg := corelib.MaclawLLMConfig{URL: baseURL, Key: key, Model: model, Protocol: "openai"}
	reqBody := map[string]interface{}{
		"model": model,
		"store": false,
		"input": []interface{}{
			map[string]interface{}{
				"type": "message",
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{"type": "input_text", "text": "What color is this image? Reply in one word."},
					map[string]interface{}{"type": "input_image", "image_url": "data:image/png;base64," + visionProbeRedPNG},
				},
			},
		},
		"stream": false,
	}
	if probeCfg.NeedsConservativeOpenAICompatSanitization() {
		delete(reqBody, "store")
	}
	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(scheduledCtx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("originator", "codex_cli_rs")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, userAgent)
	if llm.IsCodexSubscriptionEndpoint(baseURL) {
		req.Header.Set("OpenAI-Beta", "responses=experimental")
		if accountID, _ := oauth.ExtractAccountIDFromJWT(key); accountID != "" {
			req.Header.Set("chatgpt-account-id", accountID)
		}
	}

	resp, err := client.Do(req)
	globalLLMScheduler.ObserveResult(trace, err)
	if err != nil {
		log.Printf("[LLM] vision probe ResponsesAPI error: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[LLM] vision probe ResponsesAPI: HTTP %d request=%s", resp.StatusCode, llm.SummarizeOpenAIChatRequestBody(data))
		globalLLMScheduler.ObserveResult(trace, fmt.Errorf("HTTP %d", resp.StatusCode))
		return false
	}

	parsed, err := llm.ParseNonStreamResponsesAPIResponse(resp)
	globalLLMScheduler.ObserveResult(trace, err)
	if err != nil || len(parsed.Choices) == 0 {
		return false
	}
	content := parsed.Choices[0].Message.Content
	ok := looksLikeVisionResponse(content)
	if !ok && content != "" {
		log.Printf("[LLM] vision probe ResponsesAPI: model replied %q, looksLikeVision=false", truncateForLog(content, 120))
	}
	return ok
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

// normalizeThinkingModeSetting normalizes a user-supplied thinking mode to
// "" (auto), "enabled", or "disabled". Unknown values fall back to auto.
func normalizeThinkingModeSetting(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "enabled", "on", "1", "true":
		return "enabled"
	case "disabled", "off", "0", "false", "none":
		return "disabled"
	default:
		return ""
	}
}

// withGlobalThinkingMode applies the global maclaw_llm_thinking_mode setting
// to a materialized LLM config. "" leaves provider/model auto behavior.
func (a *App) withGlobalThinkingMode(cfg corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	appCfg, err := a.LoadConfig()
	if err != nil {
		return cfg
	}
	cfg.ThinkingMode = normalizeThinkingModeSetting(appCfg.MaclawLLMThinkingMode)
	return cfg
}

// GetMaclawLLMThinkingMode returns the global thinking mode:
// "" (auto), "enabled", or "disabled".
func (a *App) GetMaclawLLMThinkingMode() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	return normalizeThinkingModeSetting(cfg.MaclawLLMThinkingMode)
}

// SetMaclawLLMThinkingMode persists the global thinking mode
// ("" / "auto" = provider default, "enabled", "disabled").
func (a *App) SetMaclawLLMThinkingMode(mode string) error {
	_, err := a.PatchConfigFields(map[string]interface{}{"maclaw_llm_thinking_mode": mode})
	return err
}

// GetMaclawAgentMaxIterations returns the configured max agent iterations.
//   - positive value: use that as the limit
//   - -1 or 0 (not configured): unlimited → return 0
func (a *App) GetMaclawAgentMaxIterations() int {	cfg, err := a.LoadConfig()
	if err != nil || cfg.MaclawAgentMaxIterations <= 0 {
		return config.MaxAgentIterationsCap // not configured → default 300
	}
	return cfg.MaclawAgentMaxIterations
}

// SetMaclawAgentMaxIterations persists the max agent iterations setting.
//   - n > 0: fixed limit
//   - n == 0: unlimited (stored as -1 internally)
//   - n < 0: also unlimited (stored as 0 internally, treated same as not configured)
func (a *App) SetMaclawAgentMaxIterations(n int) error {
	_, err := a.PatchConfigFields(map[string]interface{}{"maclaw_agent_max_iterations": n})
	return err
}

func (a *App) GetSubAgentConcurrency() int {
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.DefaultSubAgentConcurrency
	}
	return corelib.NormalizeSubAgentConcurrency(cfg.SubAgentConcurrency)
}

func (a *App) SetSubAgentConcurrency(n int) error {
	_, err := a.PatchConfigFields(map[string]interface{}{"subagent_concurrency": n})
	return err
}

// MaclawLLMStatus represents the online/offline status of the MaClaw LLM agent.
type MaclawLLMStatus struct {
	Online     bool   `json:"online"`
	Configured bool   `json:"configured"`
	Error      string `json:"error,omitempty"`
}

type MobileLLMQRCodeSession struct {
	Status    string `json:"status"`
	SessionID string `json:"session_id"`
	ExpiresAt string `json:"expires_at"`
	QRPayload string `json:"qr_payload"`
}

// maclawLLMPingClient is a shared HTTP client for lightweight LLM pings.
// Reusing the client enables TCP connection pooling across periodic pings.
var maclawLLMPingClient = &http.Client{Timeout: 10 * time.Second}

// PingMaclawLLM performs a lightweight connectivity check against the
// configured LLM endpoint.  It first tries GET /models (free, no tokens
// consumed).  If that returns 404 it falls back to a HEAD request on the
// chat completions path.
//
// All requests carry the configured User-Agent so LLM providers can recognise
// the client for coding-plan eligibility.
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
		probeCfg := llmCfg
		probeCfg.URL = baseURL
		probeCfg.Key = key
		probeCfg.Model = model
		probeCfg.Protocol = "anthropic"
		probeCfg.AgentType = ua
		online, err := maclawAnthropicProbe(probeCfg)
		if err == nil {
			return MaclawLLMStatus{Online: online, Configured: true}
		}
		return MaclawLLMStatus{Online: false, Configured: true, Error: err.Error()}
	}

	probeBaseURL := normalizeOpenAIProbeBaseURL(baseURL, ua)
	var err2 error
	for _, endpoint := range openAIModelsEndpointCandidates(probeBaseURL, protocol) {
		online, probeErr := maclawLLMProbe(endpoint, key, ua)
		if probeErr == nil {
			return MaclawLLMStatus{Online: online, Configured: true}
		}
		err2 = probeErr
	}

	online, err2 := maclawLLMProbe(llm.BuildOpenAIChatCompletionsEndpoint(probeBaseURL), key, ua)
	if err2 == nil {
		return MaclawLLMStatus{Online: online, Configured: true}
	}

	return MaclawLLMStatus{Online: false, Configured: true, Error: err2.Error()}
}

func normalizeOpenAIProbeBaseURL(baseURL, userAgent string) string {
	return corelib.NormalizeGLMCodingPlanOpenAIBaseURL(baseURL, userAgent)
}

func (a *App) CreateMobileLLMDesktopQRSession(name, providerURL, key, model string, models []string, protocol string) (MobileLLMQRCodeSession, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return MobileLLMQRCodeSession{}, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return MobileLLMQRCodeSession{}, fmt.Errorf("MaClaw Hub login is required before creating a mobile LLM QR code")
	}
	body, err := json.Marshal(map[string]any{
		"name":     strings.TrimSpace(name),
		"url":      strings.TrimSpace(providerURL),
		"key":      strings.TrimSpace(key),
		"model":    strings.TrimSpace(model),
		"models":   models,
		"protocol": strings.TrimSpace(protocol),
	})
	if err != nil {
		return MobileLLMQRCodeSession{}, err
	}
	req, err := http.NewRequest(http.MethodPost, hubURL+"/api/mobile/llm/desktop-qr-sessions", bytes.NewReader(body))
	if err != nil {
		return MobileLLMQRCodeSession{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := maclawLLMPingClient.Do(req)
	if err != nil {
		return MobileLLMQRCodeSession{}, fmt.Errorf("create mobile LLM QR session failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MobileLLMQRCodeSession{}, fmt.Errorf("create mobile LLM QR session failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var session MobileLLMQRCodeSession
	if err := json.Unmarshal(data, &session); err != nil {
		return MobileLLMQRCodeSession{}, fmt.Errorf("decode mobile LLM QR session: %w", err)
	}
	if strings.TrimSpace(session.QRPayload) == "" {
		return MobileLLMQRCodeSession{}, fmt.Errorf("Hub did not return a mobile LLM QR payload")
	}
	return session, nil
}

func (a *App) CreateMobileAuthDesktopQRSession() (MobileLLMQRCodeSession, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return MobileLLMQRCodeSession{}, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	mobile := mobileAuthBoundPhoneDigits(cfg.RemoteMobile, cfg.RemoteEmail)
	if hubURL == "" || viewerToken == "" || strings.TrimSpace(cfg.RemoteMachineID) == "" || strings.TrimSpace(cfg.RemoteMachineToken) == "" {
		return MobileLLMQRCodeSession{}, fmt.Errorf("MaClaw Hub registration is required before creating a mobile authentication QR code")
	}
	if mobile == "" {
		return MobileLLMQRCodeSession{}, fmt.Errorf("mobile authentication QR code requires a bound phone number")
	}

	req, err := http.NewRequest(http.MethodPost, hubURL+"/api/mobile/auth/desktop-qr-sessions", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return MobileLLMQRCodeSession{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := maclawLLMPingClient.Do(req)
	if err != nil {
		return MobileLLMQRCodeSession{}, fmt.Errorf("create mobile authentication QR session failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return MobileLLMQRCodeSession{}, mobileAuthDesktopQRSessionHTTPError(resp.StatusCode, data)
	}
	var session MobileLLMQRCodeSession
	if err := json.Unmarshal(data, &session); err != nil {
		return MobileLLMQRCodeSession{}, fmt.Errorf("decode mobile authentication QR session: %w", err)
	}
	if strings.TrimSpace(session.QRPayload) == "" {
		return MobileLLMQRCodeSession{}, fmt.Errorf("Hub did not return a mobile authentication QR payload")
	}
	return session, nil
}

// mobileAuthBoundPhoneDigits returns digits from a stored mobile field or a
// phone-primary Hub email (phone:<digits>). Empty means no usable bound phone.
func mobileAuthBoundPhoneDigits(remoteMobile, remoteEmail string) string {
	if digits := mobileAuthPhoneDigits(remoteMobile); digits != "" {
		return digits
	}
	email := strings.TrimSpace(remoteEmail)
	if strings.HasPrefix(strings.ToLower(email), "phone:") {
		return mobileAuthPhoneDigits(email[len("phone:"):])
	}
	return ""
}

func mobileAuthPhoneDigits(value string) string {
	// Reuse enrollment phone normalization so GUI registration and auth QR agree.
	digits := normalizeRemoteRegistrationPhoneNumber(value)
	if len(digits) < 8 || len(digits) > 15 {
		return ""
	}
	return digits
}

func mobileAuthDesktopQRSessionHTTPError(status int, body []byte) error {
	raw := strings.TrimSpace(string(body))
	switch {
	case status == http.StatusUnauthorized:
		return fmt.Errorf("MaClaw Hub login is required before creating a mobile authentication QR code")
	case status == http.StatusForbidden && strings.Contains(raw, "PHONE_IDENTITY_REQUIRED"):
		return fmt.Errorf("mobile authentication QR code requires a bound phone number")
	case status == http.StatusNotFound:
		return fmt.Errorf("create mobile authentication QR session failed: Hub does not support mobile authentication QR (HTTP 404). Update/redeploy Hub and try again")
	default:
		if raw == "" {
			return fmt.Errorf("create mobile authentication QR session failed: HTTP %d", status)
		}
		return fmt.Errorf("create mobile authentication QR session failed: HTTP %d %s", status, raw)
	}
}

func openAIModelsEndpointCandidates(baseURL, protocol string) []string {
	return llm.BuildOpenAIModelsEndpointCandidates(baseURL, protocol)
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
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
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, userAgent)

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

// maclawAnthropicProbe sends a tiny Messages API request via anthropic-sdk-go
// to verify the configured Anthropic-compatible endpoint.
func maclawAnthropicProbe(cfg corelib.MaclawLLMConfig) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := llm.DoAnthropicRequestWithOptions(ctx, cfg, []interface{}{
		map[string]interface{}{"role": "user", "content": "ping"},
	}, nil, maclawLLMPingClient, llm.AnthropicMessagesRequestOptions{MaxTokens: 8})
	if err != nil {
		return false, fmt.Errorf("request failed: %w", err)
	}
	return resp != nil, nil
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
	_, err := a.PatchConfigFields(map[string]interface{}{"llm_trajectory_logging": enabled})
	return err
}

// GetTrialReflectEnabled returns whether the assistant should use trial-and-reflect mode.
func (a *App) GetTrialReflectEnabled() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false
	}
	return cfg.TrialReflectEnabled
}

// SetTrialReflectEnabled enables or disables the assistant's trial-and-reflect mode.
func (a *App) SetTrialReflectEnabled(enabled bool) error {
	_, err := a.PatchConfigFields(map[string]interface{}{"trial_reflect_enabled": enabled})
	return err
}

// ---------------------------------------------------------------------------
// LLM Token Usage Statistics
// ---------------------------------------------------------------------------

// AccumulateLLMTokenUsage adds token counts for the given provider.
// Called internally after each LLM API call. Thread-safe via tokenUsageMu.
func (a *App) AccumulateLLMTokenUsage(providerName string, inputTokens, outputTokens int) {
	a.AccumulateLLMTokenUsageWithCache(providerName, inputTokens, outputTokens, 0, 0)
}

func maclawLLMUsageProviderName(app *App, llmCfg corelib.MaclawLLMConfig) string {
	if providerName := strings.TrimSpace(llmCfg.ProviderName); providerName != "" {
		return providerName
	}
	if app == nil {
		return ""
	}
	providers := app.GetMaclawLLMProviders()
	return strings.TrimSpace(providers.Current)
}

func maclawLLMUsagePricesFromConfig(cfg *corelib.AppConfig, providerName string) (float64, float64) {
	inputPrice := corelib.DefaultLLMInputPricePerMTokensRMB
	outputPrice := corelib.DefaultLLMOutputPricePerMTokensRMB
	if cfg == nil {
		return inputPrice, outputPrice
	}
	providerName = strings.TrimSpace(providerName)
	for _, provider := range normalizeMaclawLLMProviders(cfg.MaclawLLMProviders) {
		if strings.EqualFold(strings.TrimSpace(provider.Name), providerName) {
			return provider.InputPricePerMTokensRMB, provider.OutputPricePerMTokensRMB
		}
	}
	return inputPrice, outputPrice
}

// AccumulateLLMTokenUsageWithCache adds token counts plus provider-reported
// prompt-cache read/write tokens for the given provider.
func (a *App) AccumulateLLMTokenUsageWithCache(providerName string, inputTokens, outputTokens, cachedInputTokens, cacheWriteTokens int) {
	if inputTokens == 0 && outputTokens == 0 && cachedInputTokens == 0 && cacheWriteTokens == 0 {
		return
	}
	providerName = strings.TrimSpace(providerName)
	if providerName == "" {
		return
	}
	if isRemoteToolTokenUsageProvider(providerName) {
		log.Printf("[LLM] ignoring remote-tool token usage for %q; remote coding tools are session diagnostics, not Maclaw usage", providerName)
		return
	}
	a.tokenUsageMu.Lock()
	defer a.tokenUsageMu.Unlock()
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		if cfg.LLMTokenUsage == nil {
			cfg.LLMTokenUsage = make(map[string]*corelib.TokenUsageStat)
		}
		stat, ok := cfg.LLMTokenUsage[providerName]
		if !ok || stat == nil {
			stat = &corelib.TokenUsageStat{}
			cfg.LLMTokenUsage[providerName] = stat
		}
		stat.InputTokens += int64(inputTokens)
		stat.OutputTokens += int64(outputTokens)
		stat.TotalTokens = stat.InputTokens + stat.OutputTokens
		stat.CachedInputTokens += int64(cachedInputTokens)
		stat.CacheWriteTokens += int64(cacheWriteTokens)
		inputPrice, outputPrice := maclawLLMUsagePricesFromConfig(cfg, providerName)
		stat.InputPricePerMTokensRMB = inputPrice
		stat.OutputPricePerMTokensRMB = outputPrice
		stat.InputCostRMB, stat.OutputCostRMB, stat.TotalCostRMB = corelib.CalculateLLMCostRMB(stat.InputTokens, stat.OutputTokens, inputPrice, outputPrice)
		stat.Requests++
		if cachedInputTokens > 0 {
			stat.CachedRequests++
		}
	}); err != nil {
		log.Printf("[LLM] AccumulateLLMTokenUsage: save config: %v", err)
		return
	}
	if a.ctx != nil {
		a.emitEvent("llm-token-usage-changed", providerName)
	}
}

// AccumulateLLMLocalCacheRequest records local LLM response-cache request counts
// for the sidebar cache hit-rate display. Token counts remain provider-reported.
func (a *App) AccumulateLLMLocalCacheRequest(providerName string, hit bool) {
	providerName = strings.TrimSpace(providerName)
	if providerName == "" || isRemoteToolTokenUsageProvider(providerName) {
		return
	}
	a.tokenUsageMu.Lock()
	defer a.tokenUsageMu.Unlock()
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		if cfg.LLMTokenUsage == nil {
			cfg.LLMTokenUsage = make(map[string]*corelib.TokenUsageStat)
		}
		stat, ok := cfg.LLMTokenUsage[providerName]
		if !ok || stat == nil {
			stat = &corelib.TokenUsageStat{}
			cfg.LLMTokenUsage[providerName] = stat
		}
		stat.LocalCacheRequests++
		if hit {
			stat.LocalCacheHits++
		}
	}); err != nil {
		log.Printf("[LLM] AccumulateLLMLocalCacheRequest: save config: %v", err)
		return
	}
	if a.ctx != nil {
		a.emitEvent("llm-token-usage-changed", providerName)
	}
}

// GetLLMTokenUsage returns the token usage stats for a specific provider.
// If provider is empty, returns stats for the current provider.
func (a *App) GetLLMTokenUsage(provider string) *corelib.TokenUsageStat {
	cfg, err := a.LoadConfig()
	if err != nil {
		return &corelib.TokenUsageStat{}
	}
	if provider == "" {
		provider = cfg.MaclawLLMCurrentProvider
	}
	if isRemoteToolTokenUsageProvider(provider) {
		return &corelib.TokenUsageStat{}
	}
	if cfg.LLMTokenUsage == nil {
		return &corelib.TokenUsageStat{}
	}
	if stat, ok := cfg.LLMTokenUsage[provider]; ok {
		if stat == nil {
			return &corelib.TokenUsageStat{}
		}
		copy := *stat
		return &copy
	}
	return &corelib.TokenUsageStat{}
}

// GetAllLLMTokenUsage returns token usage stats for all providers.
func (a *App) GetAllLLMTokenUsage() map[string]*corelib.TokenUsageStat {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]*corelib.TokenUsageStat{}
	}
	if cfg.LLMTokenUsage == nil {
		return map[string]*corelib.TokenUsageStat{}
	}
	return corelib.FilterRemoteCodingToolTokenUsage(cfg.LLMTokenUsage)
}

func isRemoteToolTokenUsageProvider(provider string) bool {
	return corelib.IsRemoteCodingToolTokenUsageProvider(provider)
}

// ResetLLMTokenUsage resets the token usage stats for a specific provider.
// If provider is empty, resets all providers.
func (a *App) ResetLLMTokenUsage(provider string) error {
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		if cfg.LLMTokenUsage == nil {
			return
		}
		if provider == "" {
			cfg.LLMTokenUsage = make(map[string]*corelib.TokenUsageStat)
			return
		}
		delete(cfg.LLMTokenUsage, provider)
	})
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
//  1. 非 qianxin 品牌直接返回 nil
//  2. 查找 AuthType=="sso" 的 CodeGen provider
//  3. TokenExpiresAt > 0 且未过期 → 返回 nil
//  4. TokenExpiresAt > 0 且即将过期 → 尝试 RefreshCodeGenToken
//     a. 刷新成功 → 更新 provider + 持久化到 MaClaw config（不写编程工具原生配置）
//     b. 刷新失败 → 返回 "认证已过期" 错误
//  5. TokenExpiresAt == 0 → ValidateCodeGenToken(API 调用验证)
//     a. 有效 → 返回 nil
//     b. 无效 → 返回 "认证已失效" 错误
//
// 原生工具配置（~/.codex、~/.claude 等）仅在用户从编程工具界面点击「启动」时写入。
func (a *App) ensureCodeGenToken() error {
	// 1. 品牌检查：非 qianxin 直接返回
	if shouldSkipCodeGenSSO(brand.Current().ID) {
		return nil
	}

	// 2. 查找 AuthType=="sso" 的 CodeGen provider
	data := a.GetMaclawLLMProviders()
	providerIdx := -1
	var provider corelib.MaclawLLMProvider
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
		result, err := oauth.RefreshCodeGenTokenWithClientName(provider.RefreshToken, provider.UserAgent())
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
		// Only update MaClaw's in-app tool model lists (config.json). Do not
		// rewrite native CLI configs (~/.codex, ~/.claude, …) until the user
		// explicitly launches that tool from the programming-tools UI.
		a.syncCodeGenAPIKeysToToolConfigs(updated)
		return nil
	}

	// 5. TokenExpiresAt == 0 → 用 API 调用验证 token 有效性
	if provider.TokenExpiresAt == 0 && provider.Key != "" {
		if !oauth.ValidateCodeGenTokenWithClientName(provider.Key, provider.UserAgent()) {
			return fmt.Errorf("CodeGen 认证已失效，请重新进行企业 SSO 登录")
		}
	}
	return nil
}

func (a *App) ensureCodeGenConfiguredModelAvailable() error {
	if shouldSkipCodeGenSSO(brand.Current().ID) {
		return nil
	}
	models, err := a.FetchCodeGenModels()
	if err != nil {
		return err
	}
	if len(models) == 0 {
		return nil
	}
	firstModel := strings.TrimSpace(models[0].ID)
	if firstModel == "" {
		return nil
	}
	available := make(map[string]bool, len(models))
	for _, m := range models {
		if id := strings.TrimSpace(m.ID); id != "" {
			available[id] = true
		}
	}

	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}

	changed := false
	var codegenProvider *corelib.MaclawLLMProvider
	for i := range cfg.MaclawLLMProviders {
		if cfg.MaclawLLMProviders[i].Name != codegenProviderName {
			continue
		}
		codegenProvider = &cfg.MaclawLLMProviders[i]
		if model := strings.TrimSpace(cfg.MaclawLLMProviders[i].Model); model == "" || !available[model] {
			cfg.MaclawLLMProviders[i].Model = firstModel
			changed = true
		}
		break
	}
	if codegenProvider == nil {
		return nil
	}

	openaiTarget := codeGenToolTarget(*codegenProvider, "responses")
	openaiTarget.ModelName = codegenProviderName
	openaiTarget.ModelId = firstModel
	anthropicTarget := codeGenToolTarget(*codegenProvider, "anthropic")
	anthropicTarget.ModelName = codegenProviderName
	anthropicTarget.ModelId = firstModel

	if ensureCodeGenToolModelAvailable(&cfg.Claude, anthropicTarget, available) {
		changed = true
	}
	toolConfigs := []*corelib.ToolConfig{
		&cfg.Codex, &cfg.Opencode, &cfg.CodeBuddy, &cfg.IFlow, &cfg.Kilo,
	}
	for _, tc := range toolConfigs {
		if ensureCodeGenToolModelAvailable(tc, openaiTarget, available) {
			changed = true
		}
	}

	if !changed {
		return nil
	}
	if err := a.PatchConfig(func(currentCfg *corelib.AppConfig) {
		currentCfg.MaclawLLMProviders = cfg.MaclawLLMProviders
		currentCfg.Claude = cfg.Claude
		currentCfg.Codex = cfg.Codex
		currentCfg.Opencode = cfg.Opencode
		currentCfg.CodeBuddy = cfg.CodeBuddy
		currentCfg.IFlow = cfg.IFlow
		currentCfg.Kilo = cfg.Kilo
	}); err != nil {
		return err
	}
	// Native tool configs are deferred until LaunchTool; only MaClaw config
	// (providers + per-tool model lists) is updated above.
	return nil
}

// CodeGenSSOInfo 是 StartCodeGenSSO 的返回结果，包含 SSO 认证成功后的关键信息。
type CodeGenSSOInfo struct {
	// Message 是面向用户的成功/警告消息。
	Message string `json:"message"`
	// Email 是从 id_token 解析出的用户邮件地址，用于自动注册 Hub。
	Email string `json:"email"`
	// ModelID is the first usable model selected during SSO login.
	ModelID string `json:"model_id"`
}

func shouldPatchRemoteEmailFromLogin(currentEmail, loginEmail string) bool {
	loginEmail = strings.TrimSpace(loginEmail)
	if loginEmail == "" {
		return false
	}
	if strings.HasPrefix(strings.ToLower(loginEmail), "phone:") {
		return false
	}
	currentEmail = strings.TrimSpace(currentEmail)
	if currentEmail == "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(currentEmail), "phone:")
}

// StartCodeGenSSO 执行企业 SSO 扫码登录流程，成功后：
//  1. 将 "CodeGen" 服务商 upsert 到 MaClaw LLM providers 列表并设为当前服务商
//  2. 将 CodeGen 注入各编程工具在 MaClaw config 中的服务商列表（不写原生 CLI 配置）
//  3. 返回用户 email（从 SSO 解析），供前端自动注册 Hub
//
// 原生工具配置（~/.codex、~/.claude 等）仅在用户从编程工具界面点击「启动」时写入。
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
			if shouldPatchRemoteEmailFromLogin(appCfg.RemoteEmail, result.Email) {
				_, _ = a.PatchConfigFields(map[string]interface{}{"remote_email": result.Email})
			}
		}
	}

	// 5. 将 CodeGen 注入到各编程工具的服务商列表中（仅 MaClaw config.json）
	a.injectCodeGenModelIntoToolConfigs(result)

	return CodeGenSSOInfo{
		Message: "SSO 认证成功",
		Email:   result.Email,
		ModelID: result.ModelID,
	}, nil
}

// upsertCodeGenProvider 在 providers 列表中插入或更新 "CodeGen" 服务商条目。
// 如果列表中已存在同名条目则覆盖，否则追加到列表末尾。
// 返回新的 providers 切片（不修改原切片）。
func upsertCodeGenProvider(providers []corelib.MaclawLLMProvider, result oauth.CodeGenSSOResult) []corelib.MaclawLLMProvider {
	models := make([]string, 0, len(result.Models))
	for _, model := range result.Models {
		models = appendUniqueCodeGenModel(models, model.ID)
	}
	entry := corelib.MaclawLLMProvider{
		Name:          codegenProviderName,
		URL:           result.BaseURL,
		Key:           result.AccessToken,
		Model:         corelib.NormalizeCodeGenSSOModel(result.ModelID),
		Protocol:      "openai",                  // AI 助手通过 OpenAI 协议接入 CodeGen
		AgentType:     corelib.CodeGenClientName, // TigerClaw CodeGen client identity
		AuthType:      "sso",                     // 标识认证来源，区别于手动 API Key
		ContextLength: result.ContextLength,
		Models:        models,
	}
	// 遍历查找并覆盖已有 CodeGen 条目
	for i, p := range providers {
		if p.Name == codegenProviderName {
			if strings.TrimSpace(p.AgentType) != "" && !strings.EqualFold(strings.TrimSpace(p.AgentType), "openclaw") {
				entry.AgentType = p.AgentType
			}
			updated := make([]corelib.MaclawLLMProvider, len(providers))
			copy(updated, providers)
			updated[i] = entry
			return updated
		}
	}
	// 未找到则追加
	return append(providers, entry)
}

func appendUniqueCodeGenModel(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// codegenClaudeRemoteBaseURL is the remote Anthropic-compatible base URL used
// by Claude/TigerClaw Code. It intentionally does not include /v1 because the
// Claude Code client appends its Anthropic API paths itself.
const codegenClaudeRemoteBaseURL = "https://codegen.qianxin-inc.cn/api"

// codegenAnthropicBaseURL 返回 CodeGen 给 Claude/TigerClaw Code 使用的 Anthropic 兼容端点。
func codegenAnthropicBaseURL(openaiBaseURL string) string {
	return codegenClaudeRemoteBaseURL
}

// injectCodeGenModelIntoToolConfigs 将 CodeGen 服务商作为模型条目注入到各编程工具的
// 模型列表中（Claude、Codex、OpenCode 等），使其出现在前端的服务商选择网格中。
// 如果已存在同名条目则更新，否则插入到 Custom 条目之前。
//
// 注意：Claude Code 使用 anthropic 协议，直连 CodeGen 的远程兼容端点。
// 其他工具继续直接使用 openai URL。
func (a *App) injectCodeGenModelIntoToolConfigs(result oauth.CodeGenSSOResult) {
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[CodeGen SSO] injectCodeGenModelIntoToolConfigs: LoadConfig failed: %v", err)
		return
	}

	openaiURL := result.BaseURL
	anthropicURL := codegenAnthropicBaseURL(openaiURL)
	modelName := codeGenToolModelName()

	// Claude Code 使用 anthropic 协议端点
	claudeModel := corelib.ModelConfig{
		ModelName: modelName,
		ModelId:   result.ModelID,
		ModelUrl:  anthropicURL,
		ApiKey:    result.AccessToken,
		WireApi:   "anthropic",
		AgentType: corelib.CodeGenClientName,
	}
	upsertModelInToolConfig(&cfg.Claude, claudeModel)

	// 其他工具使用 openai 协议端点
	openaiModel := corelib.ModelConfig{
		ModelName: modelName,
		ModelId:   result.ModelID,
		ModelUrl:  openaiURL,
		ApiKey:    result.AccessToken,
		WireApi:   "responses",
		AgentType: corelib.CodeGenClientName,
	}
	openaiToolConfigs := []*corelib.ToolConfig{
		&cfg.Codex,
		&cfg.Opencode,
		&cfg.CodeBuddy,
		&cfg.IFlow,
		&cfg.Kilo,
	}
	for _, tc := range openaiToolConfigs {
		upsertModelInToolConfig(tc, openaiModel)
	}

	if err := a.PatchConfig(func(currentCfg *corelib.AppConfig) {
		currentCfg.Claude = cfg.Claude
		currentCfg.Codex = cfg.Codex
		currentCfg.Opencode = cfg.Opencode
		currentCfg.CodeBuddy = cfg.CodeBuddy
		currentCfg.IFlow = cfg.IFlow
		currentCfg.Kilo = cfg.Kilo
	}); err != nil {
		log.Printf("[CodeGen SSO] injectCodeGenModelIntoToolConfigs: PatchConfig failed: %v", err)
	}
}

func codeGenToolModelName() string {
	// ModelName 在前端按钮网格中作为显示名称使用，始终显示服务商名称 "CodeGen"。
	// 实际模型 ID（如 "qax-codegen/Auto"、"auto"）存储在 ModelId 字段中。
	return codegenProviderName
}

func codeGenToolTarget(provider corelib.MaclawLLMProvider, wireAPI string) corelib.ModelConfig {
	modelName := codeGenToolModelName()
	modelID := strings.TrimSpace(provider.Model)
	if modelID == "" {
		modelID = codegenProviderName
	}
	modelURL := provider.URL
	if wireAPI == "anthropic" {
		modelURL = codegenAnthropicBaseURL(provider.URL)
	}
	return corelib.ModelConfig{
		ModelName: modelName,
		ModelId:   modelID,
		ModelUrl:  modelURL,
		ApiKey:    provider.Key,
		WireApi:   wireAPI,
		AgentType: provider.UserAgent(),
	}
}

func updateCodeGenToolAPIKey(tc *corelib.ToolConfig, target corelib.ModelConfig) bool {
	changed := false
	for i, m := range tc.Models {
		if isCodeGenToolModel(m, target) {
			if tc.Models[i].ApiKey != target.ApiKey {
				tc.Models[i].ApiKey = target.ApiKey
				changed = true
			}
			if strings.TrimSpace(target.AgentType) != "" && tc.Models[i].AgentType != target.AgentType {
				tc.Models[i].AgentType = target.AgentType
				changed = true
			}
		}
	}
	return changed
}

// syncCodeGenAPIKeysToToolConfigs updates CodeGen entries in MaClaw's per-tool
// model lists after a token refresh. It never touches native CLI config files.
func (a *App) syncCodeGenAPIKeysToToolConfigs(provider corelib.MaclawLLMProvider) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	changed := false
	if updateCodeGenToolAPIKey(&cfg.Claude, codeGenToolTarget(provider, "anthropic")) {
		changed = true
	}
	openaiTarget := codeGenToolTarget(provider, "responses")
	for _, tc := range []*corelib.ToolConfig{
		&cfg.Codex, &cfg.Opencode, &cfg.CodeBuddy, &cfg.IFlow, &cfg.Kilo,
	} {
		if updateCodeGenToolAPIKey(tc, openaiTarget) {
			changed = true
		}
	}
	if !changed {
		return
	}
	if err := a.PatchConfig(func(currentCfg *corelib.AppConfig) {
		currentCfg.Claude = cfg.Claude
		currentCfg.Codex = cfg.Codex
		currentCfg.Opencode = cfg.Opencode
		currentCfg.CodeBuddy = cfg.CodeBuddy
		currentCfg.IFlow = cfg.IFlow
		currentCfg.Kilo = cfg.Kilo
	}); err != nil {
		log.Printf("[CodeGen] syncCodeGenAPIKeysToToolConfigs failed: %v", err)
	}
}

func ensureCodeGenToolModelAvailable(tc *corelib.ToolConfig, target corelib.ModelConfig, available map[string]bool) bool {
	changed := false
	for i, m := range tc.Models {
		if !isCodeGenToolModel(m, target) {
			continue
		}
		currentWasThisEntry := tc.CurrentModel == m.ModelName
		modelID := strings.TrimSpace(m.ModelId)
		if modelID == "" {
			modelID = strings.TrimSpace(m.ModelName)
		}
		if available[modelID] {
			continue
		}
		tc.Models[i].ModelName = target.ModelName
		tc.Models[i].ModelId = target.ModelId
		tc.Models[i].ModelUrl = target.ModelUrl
		tc.Models[i].ApiKey = target.ApiKey
		tc.Models[i].WireApi = target.WireApi
		tc.Models[i].AgentType = target.AgentType
		if currentWasThisEntry {
			tc.CurrentModel = target.ModelName
		}
		changed = true
	}
	return changed
}

// upsertModelInToolConfig 在 ToolConfig 的 Models 列表中插入或更新指定名称的模型。
// 如果已存在同名条目则更新其字段；否则插入到第一个 IsCustom 条目之前。
func upsertModelInToolConfig(tc *corelib.ToolConfig, model corelib.ModelConfig) bool {
	changed := false
	found := false
	if tc.CurrentModel != model.ModelName {
		tc.CurrentModel = model.ModelName
		changed = true
	}
	updatedModels := make([]corelib.ModelConfig, 0, len(tc.Models)+1)
	for _, m := range tc.Models {
		if isCodeGenToolModel(m, model) {
			if found {
				changed = true
				continue
			}
			if m.ModelName != model.ModelName || m.ModelId != model.ModelId || m.ModelUrl != model.ModelUrl || m.ApiKey != model.ApiKey || m.WireApi != model.WireApi || m.AgentType != model.AgentType {
				changed = true
			}
			m.ModelName = model.ModelName
			m.ModelId = model.ModelId
			m.ModelUrl = model.ModelUrl
			m.ApiKey = model.ApiKey
			m.WireApi = model.WireApi
			m.AgentType = model.AgentType
			updatedModels = append(updatedModels, m)
			found = true
			continue
		}
		updatedModels = append(updatedModels, m)
	}
	if found {
		tc.Models = updatedModels
		return changed
	}
	// 插入到第一个 Custom 条目之前
	insertIdx := len(updatedModels)
	for i, m := range updatedModels {
		if m.IsCustom {
			insertIdx = i
			break
		}
	}
	newModels := make([]corelib.ModelConfig, 0, len(updatedModels)+1)
	newModels = append(newModels, updatedModels[:insertIdx]...)
	newModels = append(newModels, model)
	newModels = append(newModels, updatedModels[insertIdx:]...)
	tc.Models = newModels
	return true
}

func isCodeGenToolModel(existing, target corelib.ModelConfig) bool {
	if strings.EqualFold(strings.TrimSpace(existing.ModelName), codegenProviderName) {
		return true
	}
	if existing.IsCustom {
		return false
	}
	existingURL := strings.TrimRight(strings.TrimSpace(existing.ModelUrl), "/")
	targetURL := strings.TrimRight(strings.TrimSpace(target.ModelUrl), "/")
	return existingURL != "" && existingURL == targetURL && strings.TrimSpace(existing.WireApi) == strings.TrimSpace(target.WireApi)
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
//
// 内部委托给 FetchProviderModels，避免重复的 HTTP + JSON 解析逻辑。
func (a *App) FetchCodeGenModels() ([]CodeGenModelItem, error) {
	// 从已保存的 CodeGen provider 中读取认证信息
	data := a.GetMaclawLLMProviders()
	codeGenProviderIndex := -1
	for i := range data.Providers {
		if data.Providers[i].Name == codegenProviderName {
			codeGenProviderIndex = i
			break
		}
	}
	if codeGenProviderIndex < 0 {
		return nil, fmt.Errorf("CodeGen SSO 未完成，请先完成企业认证")
	}
	codeGenProvider := data.Providers[codeGenProviderIndex]
	if codeGenProvider.Key == "" {
		if items := codeGenModelItemsFromSavedIDs(codeGenProvider.Models); len(items) > 0 {
			return items, nil
		}
		return nil, fmt.Errorf("CodeGen SSO 未完成，请先完成企业认证")
	}

	models, err := a.fetchProviderModels(codeGenProvider.URL, codeGenProvider.Key, "openai", codeGenProvider.UserAgent(), false)
	if err != nil {
		if items := codeGenModelItemsFromSavedIDs(codeGenProvider.Models); len(items) > 0 {
			log.Printf("[CodeGen] FetchCodeGenModels: live fetch failed, using cached provider models: %v", err)
			return items, nil
		}
		return nil, fmt.Errorf("获取 CodeGen 模型列表失败: %w", err)
	}

	items := make([]CodeGenModelItem, 0, len(models))
	modelIDs := make([]string, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		items = append(items, CodeGenModelItem{
			ID:   id,
			Name: model.Name,
		})
		modelIDs = appendUniqueCodeGenModel(modelIDs, id)
	}
	if len(items) == 0 {
		if cached := codeGenModelItemsFromSavedIDs(codeGenProvider.Models); len(cached) > 0 {
			return cached, nil
		}
		return nil, fmt.Errorf("CodeGen 服务商返回了空的模型列表")
	}
	if !reflect.DeepEqual(codeGenProvider.Models, modelIDs) {
		data.Providers[codeGenProviderIndex].Models = modelIDs
		if err := a.SaveMaclawLLMProviders(data.Providers, data.Current); err != nil {
			log.Printf("[CodeGen] FetchCodeGenModels: save provider models failed: %v", err)
		}
	}
	return items, nil
}

func codeGenModelItemsFromSavedIDs(ids []string) []CodeGenModelItem {
	items := make([]CodeGenModelItem, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		items = append(items, CodeGenModelItem{ID: id, Name: id})
	}
	return items
}

// SaveCodeGenModelChoice 保存用户在 SSO 后选择的模型：
//   - maclawModel：用于驱动 MaClaw Agent（写入 config.json 的 CodeGen provider）
//   - claudeCodeModel：用于 TigerClaw Code / Claude 工具在 MaClaw 内的模型列表条目
//
// 两个模型可以相同也可以不同，独立配置。
// 不写入 ~/.claude/settings.json 等原生 CLI 配置；那些仅在编程工具「启动」时写入。
func (a *App) SaveCodeGenModelChoice(maclawModel, claudeCodeModel string) error {
	maclawModel = strings.TrimSpace(maclawModel)
	claudeCodeModel = strings.TrimSpace(claudeCodeModel)
	codegenAgent := corelib.CodeGenClientName
	var codegenKey, codegenURL string

	// 1. 更新 MaClaw CodeGen provider 的 model 字段
	if maclawModel != "" {
		data := a.GetMaclawLLMProviders()
		updated := false
		for i := range data.Providers {
			if data.Providers[i].Name == codegenProviderName {
				codegenKey = data.Providers[i].Key
				codegenURL = data.Providers[i].URL
				codegenAgent = data.Providers[i].UserAgent()
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

	// 2. 同步更新各编程工具模型列表中的 CodeGen 条目（仅 MaClaw config.json）
	if cfg, err := a.LoadConfig(); err == nil {
		changed := false
		if codegenKey == "" || codegenURL == "" {
			for _, p := range cfg.MaclawLLMProviders {
				if p.Name == codegenProviderName {
					codegenKey = p.Key
					codegenURL = p.URL
					codegenAgent = p.UserAgent()
					break
				}
			}
		}

		if claudeTargetModel != "" {
			if cfg.Claude.CurrentModel != codegenProviderName {
				cfg.Claude.CurrentModel = codegenProviderName
				changed = true
			}
			claudeTargetEntry := corelib.ModelConfig{
				ModelName: codegenProviderName,
				ModelId:   claudeTargetModel,
				ModelUrl:  codegenAnthropicBaseURL(codegenURL),
				ApiKey:    codegenKey,
				WireApi:   "anthropic",
				AgentType: codegenAgent,
			}
			if upsertModelInToolConfig(&cfg.Claude, claudeTargetEntry) {
				changed = true
			}
		}

		if maclawModel != "" {
			openaiTargetEntry := corelib.ModelConfig{
				ModelName: codegenProviderName,
				ModelId:   maclawModel,
				ModelUrl:  codegenURL,
				ApiKey:    codegenKey,
				WireApi:   "responses",
				AgentType: codegenAgent,
			}
			toolConfigs := []*corelib.ToolConfig{
				&cfg.Codex, &cfg.Opencode,
				&cfg.CodeBuddy, &cfg.IFlow, &cfg.Kilo,
			}
			for _, tc := range toolConfigs {
				if upsertModelInToolConfig(tc, openaiTargetEntry) {
					changed = true
				}
			}
		}

		if changed {
			if err := a.PatchConfig(func(currentCfg *corelib.AppConfig) {
				currentCfg.Claude = cfg.Claude
				currentCfg.Codex = cfg.Codex
				currentCfg.Opencode = cfg.Opencode
				currentCfg.CodeBuddy = cfg.CodeBuddy
				currentCfg.IFlow = cfg.IFlow
				currentCfg.Kilo = cfg.Kilo
			}); err != nil {
				log.Printf("[CodeGen] SaveCodeGenModelChoice: sync tool config model failed: %v", err)
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
				if shouldPatchRemoteEmailFromLogin(appCfg.RemoteEmail, result.Email) {
					_, _ = a.PatchConfigFields(map[string]interface{}{"remote_email": result.Email})
				}
			}
		}

		// Inject into MaClaw tool model lists only; native CLI configs are
		// written later when the user launches a programming tool.
		a.injectCodeGenModelIntoToolConfigs(result)

		resultCh <- ssoPollingResult{
			info: CodeGenSSOInfo{Message: "SSO 认证成功", Email: result.Email, ModelID: result.ModelID},
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

// ---------------------------------------------------------------------------
// 通用模型列表获取（适用于所有 OpenAI / Anthropic 兼容服务商）
// ---------------------------------------------------------------------------

// ProviderModelItem 描述一个从服务商 /models 端点获取的可用模型。
// 复用 CodeGenModelItem 的结构，但语义上是通用的。
type ProviderModelItem = CodeGenModelItem

type providerModelEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	OwnedBy     string `json:"owned_by"`
	Available   *bool  `json:"available,omitempty"`
	Enabled     *bool  `json:"enabled,omitempty"`
	Active      *bool  `json:"active,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Status      string `json:"status,omitempty"`
}

func providerModelEntryID(m providerModelEntry) string {
	return strings.TrimSpace(m.ID)
}

func providerModelEntryName(m providerModelEntry) string {
	for _, name := range []string{m.DisplayName, m.Name, m.ID} {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isUsableProviderModel(m providerModelEntry) bool {
	if providerModelEntryID(m) == "" {
		return false
	}
	if m.Disabled {
		return false
	}
	if m.Available != nil && !*m.Available {
		return false
	}
	if m.Enabled != nil && !*m.Enabled {
		return false
	}
	if m.Active != nil && !*m.Active {
		return false
	}
	return normalizeMaclawLLMProviderStatusKind(m.Status).IsAvailable()
}

func providerModelItemFromEntry(m providerModelEntry) (ProviderModelItem, bool) {
	if !isUsableProviderModel(m) {
		return ProviderModelItem{}, false
	}
	id := providerModelEntryID(m)
	name := providerModelEntryName(m)
	if name == "" {
		name = id
	}
	return ProviderModelItem{ID: id, Name: name}, true
}

// preserveManagedAuthSecrets keeps OAuth/SSO credentials when the UI saves
// provider settings without an API Key field (tokens are internal).
func preserveManagedAuthSecrets(incoming, existing []corelib.MaclawLLMProvider) []corelib.MaclawLLMProvider {
	if len(incoming) == 0 || len(existing) == 0 {
		return incoming
	}
	byName := make(map[string]corelib.MaclawLLMProvider, len(existing))
	for _, p := range existing {
		byName[p.Name] = p
	}
	for i, p := range incoming {
		kind := normalizeMaclawLLMAuthTypeKind(p.AuthType)
		if !kind.IsOAuth() && p.AuthType != "sso" {
			continue
		}
		old, ok := byName[p.Name]
		if !ok {
			continue
		}
		if strings.TrimSpace(p.Key) == "" && strings.TrimSpace(old.Key) != "" {
			incoming[i].Key = old.Key
		}
		if strings.TrimSpace(p.RefreshToken) == "" && strings.TrimSpace(old.RefreshToken) != "" {
			incoming[i].RefreshToken = old.RefreshToken
		}
		if strings.TrimSpace(p.OAuthAccessToken) == "" && strings.TrimSpace(old.OAuthAccessToken) != "" {
			incoming[i].OAuthAccessToken = old.OAuthAccessToken
		}
		if p.TokenExpiresAt == 0 && old.TokenExpiresAt != 0 {
			incoming[i].TokenExpiresAt = old.TokenExpiresAt
		}
	}
	return incoming
}

// urlsMatchForModelFetch reports whether two provider base URLs refer to the
// same API host for model discovery (exact or parent/child path).
func urlsMatchForModelFetch(a, b string) bool {
	a = strings.TrimRight(strings.ToLower(strings.TrimSpace(a)), "/")
	b = strings.TrimRight(strings.ToLower(strings.TrimSpace(b)), "/")
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// resolveAPIKeyForModelFetch looks up a managed OAuth/SSO (or saved API) key for
// the given provider base URL when the frontend did not supply one.
//
// Match order:
//  1. exact URL match (preferred — avoids prefix collisions)
//  2. parent/child path match
//
// Within each tier, prefer OAuth/SSO providers over plain API-key entries.
func (a *App) resolveAPIKeyForModelFetch(baseURL string) string {
	if a == nil {
		return ""
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}

	cfg, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	providers := normalizeMaclawLLMProviders(a.syncedMaclawLLMProviders(cfg))

	type candidate struct {
		idx      int
		provider corelib.MaclawLLMProvider
		score    int // exact+managed=6, exact=4, managed=2, other=0
	}
	var candidates []candidate
	for i, p := range providers {
		p = normalizeMaclawLLMProvider(p)
		if p.URL == "" {
			continue
		}
		exact := strings.EqualFold(strings.TrimRight(p.URL, "/"), baseURL)
		loose := !exact && urlsMatchForModelFetch(p.URL, baseURL)
		if !exact && !loose {
			continue
		}
		kind := normalizeMaclawLLMAuthTypeKind(p.AuthType)
		managed := kind.IsOAuth() || p.AuthType == "sso"
		score := 0
		if exact {
			score += 4
		}
		if managed {
			score += 2
		}
		candidates = append(candidates, candidate{
			idx:      i,
			provider: p,
			score:    score,
		})
	}
	// exact+managed > exact > managed > other
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	for _, c := range candidates {
		p := c.provider
		storeID := credentialStoreProviderID(p)
		if storeID != "" {
			// Best-effort refresh so model listing works after token expiry.
			if a.credentialStore != nil {
				_ = a.ensureOAuthTokenViaStore(p, c.idx)
			}
			if key := a.resolveProviderKeyFromStore(p); key != "" {
				return key
			}
		}
		if key := strings.TrimSpace(p.Key); key != "" {
			return key
		}
		// OAuthAccessToken is not always an API key. For GitHub Copilot it holds
		// the long-lived GitHub token; the short-lived Copilot token is in Key /
		// credential store RawAccessToken. Never fall back to it for copilot.
		if storeID != "github-copilot" {
			if key := strings.TrimSpace(p.OAuthAccessToken); key != "" {
				return key
			}
		}
	}
	return ""
}

// FetchProviderModels 通过 {baseURL}/models 端点获取服务商可用的模型列表。
// 适用于所有 OpenAI / Anthropic 兼容的 API 服务商。
//
// 参数：
//   - baseURL: 服务商 API 基础地址（如 https://api.deepseek.com/v1）
//   - apiKey: API Key（OAuth/SSO 可为空，后端从内部凭证解析）
//   - protocol: "openai"（默认）或 "anthropic"
//
// 前端在 MaClaw LLM 配置面板和编程工具配置面板中调用此函数，
// 让用户从服务商实际支持的模型中选择，而非手动输入模型名。
func (a *App) FetchProviderModels(baseURL, apiKey, protocol, userAgent string) ([]ProviderModelItem, error) {
	return a.fetchProviderModels(baseURL, apiKey, protocol, userAgent, true)
}

func (a *App) fetchProviderModels(baseURL, apiKey, protocol, userAgent string, sortModels bool) ([]ProviderModelItem, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	apiKey = strings.TrimSpace(apiKey)
	protocol = strings.TrimSpace(protocol)

	// ChatGPT / Codex subscription (chatgpt.com/backend-api) does not expose a
	// standard OpenAI-compatible /models endpoint. Using GET /models returns
	// HTTP 403 even with a valid OAuth token. Handle it on a dedicated path.
	if llm.IsCodexSubscriptionEndpoint(baseURL) {
		return a.fetchCodexSubscriptionModels(baseURL, apiKey, userAgent, sortModels)
	}

	// OAuth/SSO providers manage tokens internally. When the frontend does not
	// (or cannot) pass a key, resolve it from credential store / saved config.
	if apiKey == "" {
		apiKey = a.resolveAPIKeyForModelFetch(baseURL)
	}

	maskedKey := "***"
	if len(apiKey) > 8 {
		maskedKey = apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
	}
	log.Printf("[FetchProviderModels] start: url=%q protocol=%q userAgent=%q keyLen=%d key=%s", baseURL, protocol, userAgent, len(apiKey), maskedKey)

	if baseURL == "" {
		return nil, fmt.Errorf("API 地址为空，请先填写 API Endpoint")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("API Key 为空：请先填写 API Key，或完成 OAuth/SSO 登录")
	}

	// Model discovery protocol override for hybrid gateways. CodeGen speaks
	// Anthropic wire protocol for chat (/v1/messages) but its /v1/models
	// endpoint only accepts OpenAI-style Bearer token auth. The Anthropic SDK
	// sends x-api-key + anthropic-version headers which CodeGen rejects (400).
	// Override to OpenAI so doFetchModelsRequest uses Authorization: Bearer.
	if strings.EqualFold(protocol, "anthropic") && corelib.IsCodeGenURL(baseURL) {
		log.Printf("[FetchProviderModels] protocol override: %q -> \"openai\" (CodeGen hybrid gateway)", protocol)
		protocol = "openai"
	}

	client := &http.Client{Timeout: 15 * time.Second}
	if strings.EqualFold(protocol, "anthropic") {
		log.Printf("[FetchProviderModels] using Anthropic SDK for model listing")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		models, err := llm.ListAnthropicModelsWithSDK(ctx, corelib.MaclawLLMConfig{
			URL:       baseURL,
			Key:       apiKey,
			Protocol:  "anthropic",
			AgentType: userAgent,
		}, client)
		if err != nil {
			log.Printf("[FetchProviderModels] Anthropic SDK error: %v", err)
			return nil, fmt.Errorf("fetch anthropic models with sdk: %w", err)
		}
		items := make([]ProviderModelItem, 0, len(models))
		for _, model := range models {
			name := model.DisplayName
			if name == "" {
				name = model.ID
			}
			items = append(items, ProviderModelItem{ID: model.ID, Name: name})
		}
		if len(items) == 0 {
			log.Printf("[FetchProviderModels] Anthropic SDK returned empty model list")
			return nil, fmt.Errorf("anthropic sdk returned empty model list")
		}
		if sortModels {
			sort.Slice(items, func(i, j int) bool {
				return items[i].ID < items[j].ID
			})
		}
		log.Printf("[FetchProviderModels] success via Anthropic SDK: %d models", len(items))
		return items, nil
	}

	candidates := openAIModelsEndpointCandidates(normalizeOpenAIProbeBaseURL(baseURL, userAgent), protocol)
	log.Printf("[FetchProviderModels] using OpenAI protocol, endpoint candidates: %v", candidates)

	var resp *http.Response
	var err error
	for _, endpoint := range candidates {
		log.Printf("[FetchProviderModels] trying endpoint: %s", endpoint)
		r, e := a.doFetchModelsRequest(client, endpoint, apiKey, protocol, userAgent)
		if e != nil {
			log.Printf("[FetchProviderModels] endpoint %s failed: %v", endpoint, e)
			err = e
			continue
		}
		if r.StatusCode == http.StatusNotFound {
			log.Printf("[FetchProviderModels] endpoint %s returned 404, trying next", endpoint)
			r.Body.Close()
			continue
		}
		log.Printf("[FetchProviderModels] endpoint %s returned HTTP %d", endpoint, r.StatusCode)
		resp = r
		break
	}
	if resp == nil {
		if err != nil {
			log.Printf("[FetchProviderModels] all endpoints failed, last error: %v", err)
			return nil, fmt.Errorf("获取模型列表失败: %w", err)
		}
		log.Printf("[FetchProviderModels] all endpoints returned 404")
		return nil, fmt.Errorf("服务器返回 HTTP 404: 模型列表端点不存在，请检查 API 地址是否正确")
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 128*1024))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		log.Printf("[FetchProviderModels] auth failed: HTTP %d, body=%s", resp.StatusCode, truncateCodeGenStr(string(body), 200))
		return nil, fmt.Errorf("认证失败 (HTTP %d)，请检查 API Key 是否正确", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("[FetchProviderModels] unexpected status: HTTP %d, body=%s", resp.StatusCode, truncateCodeGenStr(string(body), 200))
		return nil, fmt.Errorf("服务器返回 HTTP %d: %s", resp.StatusCode, truncateCodeGenStr(string(body), 256))
	}

	// 解析响应——兼容多种格式：
	// 1. OpenAI 格式：{"data": [{"id": "...", "owned_by": "..."}]}
	// 2. Anthropic 格式：{"models": [{"id": "...", "display_name": "..."}]}
	// 3. 简单数组格式：[{"id": "..."}, ...]

	var items []ProviderModelItem

	// 先尝试解析为 JSON 对象（OpenAI / Anthropic 格式）
	var objResult struct {
		Data   []providerModelEntry `json:"data"`
		Models []providerModelEntry `json:"models"`
	}

	parsed := false
	if err := json.Unmarshal(body, &objResult); err == nil {
		// 优先 Anthropic 格式
		if len(objResult.Models) > 0 {
			for _, m := range objResult.Models {
				if item, ok := providerModelItemFromEntry(m); ok {
					items = append(items, item)
				}
			}
			parsed = true
		} else if len(objResult.Data) > 0 {
			for _, m := range objResult.Data {
				if item, ok := providerModelItemFromEntry(m); ok {
					items = append(items, item)
				}
			}
			parsed = true
		}
	}

	// 对象格式未解析出结果时，尝试解析为简单数组格式：[{"id": "..."}, ...]
	if !parsed {
		var arr []providerModelEntry
		if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
			for _, m := range arr {
				if item, ok := providerModelItemFromEntry(m); ok {
					items = append(items, item)
				}
			}
		}
	}

	if len(items) == 0 {
		log.Printf("[FetchProviderModels] parsed response but got 0 models, bodyLen=%d", len(body))
		return nil, fmt.Errorf("服务商返回了空的模型列表")
	}

	// CodeGen returns both "auto" (unusable shorthand) and "qax-codegen/Auto"
	// (canonical name). Filter out the bare "auto" to avoid user confusion —
	// selecting it would cause 400 errors on both OpenAI and Anthropic endpoints.
	if corelib.IsCodeGenURL(baseURL) {
		filtered := items[:0]
		for _, item := range items {
			if !strings.EqualFold(item.ID, corelib.CodeGenAutoModelAlias) {
				filtered = append(filtered, item)
			}
		}
		if len(filtered) > 0 {
			items = filtered
		}
	}

	if sortModels {
		// 按 ID 排序，方便普通服务商模型浏览；CodeGen 调用保留服务端顺序。
		sort.Slice(items, func(i, j int) bool {
			return items[i].ID < items[j].ID
		})
	}

	modelIDs := make([]string, 0, min(len(items), 5))
	for i, item := range items {
		if i >= 5 {
			break
		}
		modelIDs = append(modelIDs, item.ID)
	}
	log.Printf("[FetchProviderModels] success: %d models, first5=%v", len(items), modelIDs)
	return items, nil
}

// codexSubscriptionModelCatalog is the built-in fallback when ChatGPT's
// codex/models endpoint is unavailable. Keep ids aligned with Codex CLI.
func codexSubscriptionModelCatalog() []ProviderModelItem {
	ids := []string{
		"gpt-5.6-luna",
		"gpt-5.6-terra",
		"gpt-5.6-sol",
		"gpt-5.5",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
		"gpt-5.2",
		"gpt-5.2-codex",
		"gpt-5.1",
		"gpt-5.1-codex-max",
		"gpt-5.1-codex-mini",
	}
	items := make([]ProviderModelItem, 0, len(ids))
	for _, id := range ids {
		items = append(items, ProviderModelItem{ID: id, Name: id})
	}
	return items
}

// resolveCodexAuthToken picks the JWT access token for ChatGPT backend-api.
// sk- API keys from token-exchange cannot call chatgpt.com/backend-api.
func (a *App) resolveCodexAuthToken(baseURL, frontendKey string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	pickJWT := func(key, raw string) string {
		raw = strings.TrimSpace(raw)
		key = strings.TrimSpace(key)
		// Prefer raw OAuth JWT (eyJ...) over exchanged sk- API keys.
		if raw != "" && !strings.HasPrefix(raw, "sk-") {
			return raw
		}
		if key != "" && !strings.HasPrefix(key, "sk-") {
			return key
		}
		return ""
	}

	if a != nil {
		if cfg, err := a.LoadConfig(); err == nil {
			for _, p := range normalizeMaclawLLMProviders(a.syncedMaclawLLMProviders(cfg)) {
				p = normalizeMaclawLLMProvider(p)
				if !urlsMatchForModelFetch(p.URL, baseURL) {
					continue
				}
				if storeID := credentialStoreProviderID(p); storeID != "" && a.credentialStore != nil {
					if cred, err := a.credentialStore.Read(storeID); err == nil && cred != nil {
						if tok := pickJWT(cred.AccessToken, cred.RawAccessToken); tok != "" {
							return tok
						}
					}
				}
				if tok := pickJWT(p.Key, p.OAuthAccessToken); tok != "" {
					return tok
				}
			}
		}
	}

	frontendKey = strings.TrimSpace(frontendKey)
	if frontendKey != "" && !strings.HasPrefix(frontendKey, "sk-") {
		return frontendKey
	}
	// A platform API key is never valid for the ChatGPT subscription backend.
	// Returning an empty token makes the caller use its built-in model catalog.
	return ""
}

// fetchCodexSubscriptionModels lists models for OpenAI ChatGPT OAuth (Codex).
// Tries GET {base}/codex/models with Codex headers; on failure returns a built-in catalog.
func (a *App) fetchCodexSubscriptionModels(baseURL, frontendKey, userAgent string, sortModels bool) ([]ProviderModelItem, error) {
	token := a.resolveCodexAuthToken(baseURL, frontendKey)
	if token == "" {
		// Still return catalog so the user can pick a model after login state is partial.
		log.Printf("[FetchProviderModels] codex: no auth token, returning built-in catalog")
		return codexSubscriptionModelCatalog(), nil
	}

	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		ua = "openclaw"
	}

	// Normalize to .../backend-api then append /codex/models
	endpoint := strings.TrimRight(baseURL, "/")
	endpoint = strings.TrimSuffix(endpoint, "/codex/models")
	endpoint = strings.TrimSuffix(endpoint, "/codex")
	endpoint = strings.TrimSuffix(endpoint, "/models")
	endpoint = strings.TrimRight(endpoint, "/") + "/codex/models"

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		log.Printf("[FetchProviderModels] codex: build request failed: %v", err)
		return codexSubscriptionModelCatalog(), nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("Accept", "application/json")
	if accountID, _ := oauth.ExtractAccountIDFromJWT(token); accountID != "" {
		req.Header.Set("chatgpt-account-id", accountID)
	}

	log.Printf("[FetchProviderModels] codex: GET %s (tokenLen=%d)", endpoint, len(token))
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[FetchProviderModels] codex: request failed: %v, using built-in catalog", err)
		return codexSubscriptionModelCatalog(), nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		log.Printf("[FetchProviderModels] codex: HTTP %d body=%s, using built-in catalog",
			resp.StatusCode, truncateCodeGenStr(string(body), 200))
		return codexSubscriptionModelCatalog(), nil
	}

	items := parseProviderModelsJSON(body)
	if len(items) == 0 {
		log.Printf("[FetchProviderModels] codex: empty remote list, using built-in catalog")
		return codexSubscriptionModelCatalog(), nil
	}
	if sortModels {
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	}
	log.Printf("[FetchProviderModels] codex: success %d models", len(items))
	return items, nil
}

// parseProviderModelsJSON extracts model items from OpenAI / Anthropic / array shapes.
func parseProviderModelsJSON(body []byte) []ProviderModelItem {
	var items []ProviderModelItem
	var objResult struct {
		Data   []providerModelEntry `json:"data"`
		Models []providerModelEntry `json:"models"`
	}
	if err := json.Unmarshal(body, &objResult); err == nil {
		src := objResult.Models
		if len(src) == 0 {
			src = objResult.Data
		}
		for _, m := range src {
			if item, ok := providerModelItemFromEntry(m); ok {
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			return items
		}
	}
	var arr []providerModelEntry
	if err := json.Unmarshal(body, &arr); err == nil {
		for _, m := range arr {
			if item, ok := providerModelItemFromEntry(m); ok {
				items = append(items, item)
			}
		}
	}
	return items
}

// doFetchModelsRequest 发送 GET 请求到指定的 models 端点。
// 提取为独立方法以支持 FetchProviderModels 中的 fallback 重试。
func (a *App) doFetchModelsRequest(client *http.Client, endpoint, apiKey, protocol, userAgent string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("构建请求失败: %w", err)
	}
	ua := strings.TrimSpace(userAgent)
	if ua == "" {
		ua = "openclaw"
	}
	if corelib.IsCodeGenURL(endpoint) && strings.EqualFold(ua, "openclaw") {
		ua = corelib.CodeGenClientName
	}
	req.Header.Set("User-Agent", ua)
	if protocol == "anthropic" {
		corelib.SetAnthropicAuthHeaders(req, apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	corelib.SetCodeGenClientNameHeaderIfNeededWithName(req, ua)
	if strings.EqualFold(ua, corelib.CodeGenClientName) {
		req.Header.Set(corelib.CodeGenClientNameHeader, corelib.CodeGenClientName)
	}
	return client.Do(req)
}

// truncateCodeGenStr 截断字符串到 maxLen，超出时追加 "..."。
// 注意：避免与 scheduled_task.go 中的 truncateStr 重名，故加 CodeGen 前缀。
func truncateCodeGenStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
