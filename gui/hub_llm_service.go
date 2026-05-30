package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	hubServiceProviderName  = "MaClaw\u5b98\u65b9"
	hubServiceAutoModel     = "auto"
	hubServiceStatusTimeout = 2 * time.Second
)

// ensureViewerTokenMu serializes re-enroll attempts when multiple callers
// discover a missing viewer token concurrently.
var ensureViewerTokenMu sync.Mutex

// ensureViewerToken checks whether the config has a RemoteViewerToken. If it
// is missing but the registration credentials (RemoteEmail + RemoteHubURL) are
// present, it performs a re-enroll to obtain a fresh viewer token.
//
// This is a **startup recovery** mechanism, not a workaround. It covers:
//   - Users who registered with an older Hub/client that did not issue viewer
//     tokens at enrollment time.
//   - WebSocket connection failures after registration (the auth.ok path that
//     normally delivers the viewer token never executed).
//   - Any historical config corruption that lost the token.
//
// The re-enroll itself uses PatchConfig for atomic persistence, so the token
// cannot be lost to a concurrent SaveConfig race.
func (a *App) ensureViewerToken(cfg corelib.AppConfig) (corelib.AppConfig, error) {
	if strings.TrimSpace(cfg.RemoteViewerToken) != "" {
		return cfg, nil
	}
	if strings.TrimSpace(cfg.RemoteEmail) == "" || strings.TrimSpace(cfg.RemoteHubURL) == "" {
		return cfg, fmt.Errorf("hub access token is missing")
	}

	ensureViewerTokenMu.Lock()
	defer ensureViewerTokenMu.Unlock()

	// Double-check after lock; another goroutine may have completed recovery.
	freshCfg, err := a.LoadConfig()
	if err != nil {
		return cfg, fmt.Errorf("hub access token is missing")
	}
	if strings.TrimSpace(freshCfg.RemoteViewerToken) != "" {
		return freshCfg, nil
	}

	log.Printf("[hub-llm-service] viewer token missing, re-enrolling email=%s", freshCfg.RemoteEmail)
	result, err := a.ActivateRemote(freshCfg.RemoteEmail, "", "")
	if err != nil {
		log.Printf("[hub-llm-service] re-enroll failed: %v", err)
		return cfg, fmt.Errorf("hub access token is missing (recovery failed: %v)", err)
	}
	if result.ViewerToken == "" {
		log.Printf("[hub-llm-service] re-enroll succeeded but hub did not issue viewer token")
		return cfg, fmt.Errorf("hub access token is missing (hub did not issue token)")
	}

	log.Printf("[hub-llm-service] viewer token recovered via re-enroll")
	// ActivateRemote persisted via PatchConfig; reload to get the fresh copy.
	updated, err := a.LoadConfig()
	if err != nil {
		return cfg, err
	}
	return updated, nil
}

type HubLLMAuthorizedModel struct {
	Name            string   `json:"name"`
	ProviderIDs     []string `json:"provider_ids,omitempty"`
	ServiceGroupIDs []string `json:"service_group_ids,omitempty"`
}

type HubLLMActiveGrant struct {
	ServiceGroupID    string  `json:"service_group_id"`
	Source            string  `json:"source"`
	StartsAt          string  `json:"starts_at"`
	ExpiresAt         string  `json:"expires_at"`
	Active            bool    `json:"active"`
	Status            string  `json:"status,omitempty"`
	StatusReason      string  `json:"status_reason,omitempty"`
	CreditsTotal      float64 `json:"credits_total,omitempty"`
	CreditsUsed       float64 `json:"credits_used,omitempty"`
	CreditsAvailable  float64 `json:"credits_available,omitempty"`
	RetryAfterSeconds int64   `json:"retry_after_seconds,omitempty"`
	RetryAfterAt      string  `json:"retry_after_at,omitempty"`
	CreditsRemaining  float64 `json:"credits_remaining,omitempty"`
}

type HubLLMServiceStatus struct {
	Active             bool                    `json:"active"`
	SkipLLMConfig      bool                    `json:"skip_llm_config"`
	AuthMode           string                  `json:"auth_mode"`
	ServiceGroupIDs    []string                `json:"service_group_ids,omitempty"`
	ServiceGroupNames  []string                `json:"service_group_names,omitempty"`
	AvailableModels    []string                `json:"available_models,omitempty"`
	AuthorizedModels   []HubLLMAuthorizedModel `json:"authorized_models,omitempty"`
	ActiveGrants       []HubLLMActiveGrant     `json:"active_grants,omitempty"`
	CreditGrants       []HubLLMActiveGrant     `json:"credit_grants,omitempty"`
	InactiveReasons    []string                `json:"inactive_reasons,omitempty"`
	NearestExpiresAt   string                  `json:"nearest_expires_at,omitempty"`
	EffectiveExpiresAt string                  `json:"effective_expires_at,omitempty"`
	DefaultModel       string                  `json:"default_model,omitempty"`
	HubLLMBaseURL      string                  `json:"hub_llm_base_url,omitempty"`
	CreditsTotal       float64                 `json:"credits_total,omitempty"`
	CreditsUsed        float64                 `json:"credits_used,omitempty"`
	CreditsRemaining   float64                 `json:"credits_remaining,omitempty"`
	CreditsAvailable   float64                 `json:"credits_available,omitempty"`
	TokensPerCredit    int                     `json:"tokens_per_credit,omitempty"`
}

type hubLLMServiceRedeemResponse struct {
	Success       bool                `json:"success"`
	ServiceStatus HubLLMServiceStatus `json:"service_status"`
}

func (a *App) GetHubLLMServiceStatus() (HubLLMServiceStatus, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	// Auto-recover missing viewer token before querying status.
	cfg, err = a.ensureViewerToken(cfg)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	status, err := a.fetchHubLLMServiceStatus(cfg)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	// Reload config before applying status to avoid overwriting concurrent
	// changes made while the HTTP call was in flight.
	freshCfg, err := a.LoadConfig()
	if err != nil {
		return status, err
	}
	if a.applyHubLLMServiceStatusToConfig(&freshCfg, status) {
		if saveErr := a.SaveConfig(freshCfg); saveErr != nil {
			return status, saveErr
		}
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "hub-llm-service-changed")
		}
	}
	return status, nil
}

func (a *App) syncedMaclawLLMProviders(cfg corelib.AppConfig) []corelib.MaclawLLMProvider {
	a.syncHubLLMServiceStatusIntoConfig(&cfg)
	return append([]corelib.MaclawLLMProvider(nil), cfg.MaclawLLMProviders...)
}

func (a *App) RedeemHubLLMService(code string) (HubLLMServiceStatus, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	if strings.TrimSpace(cfg.RemoteHubURL) == "" {
		return HubLLMServiceStatus{}, fmt.Errorf("hub URL is not configured")
	}
	// Auto-recover missing viewer token via re-enroll before giving up.
	cfg, err = a.ensureViewerToken(cfg)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	payload, err := json.Marshal(map[string]string{"code": strings.TrimSpace(code)})
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.RemoteHubURL, "/")+"/api/llm/service/redeem", bytes.NewReader(payload))
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.RemoteViewerToken))
	resp, err := hubHTTPClient.Do(req)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var failure map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&failure); err == nil {
			if msg, _ := failure["message"].(string); strings.TrimSpace(msg) != "" {
				return HubLLMServiceStatus{}, fmt.Errorf("%s", msg)
			}
		}
		return HubLLMServiceStatus{}, fmt.Errorf("redeem failed: %s", resp.Status)
	}
	var result hubLLMServiceRedeemResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return HubLLMServiceStatus{}, err
	}
	// Reload config before applying status to avoid overwriting concurrent
	// changes (e.g. user editing LLM providers while redeem HTTP call was
	// in flight). Same pattern as #11 SSO config race fix.
	freshCfg, err := a.LoadConfig()
	if err != nil {
		return result.ServiceStatus, err
	}
	if a.applyHubLLMServiceStatusToConfig(&freshCfg, result.ServiceStatus) {
		if err := a.SaveConfig(freshCfg); err != nil {
			return result.ServiceStatus, err
		}
	}
	// Notify frontend that the Hub LLM service status has changed so that
	// LLMConfigPanel (and any other listener) can reload its provider list
	// and hub service status. Without this event, the LLM config dialog
	// shows stale data when the user redeems from the service redemption tab and
	// then switches to the LLM configuration tab.
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "hub-llm-service-changed")
	}
	return result.ServiceStatus, nil
}

func (a *App) syncHubLLMServiceStatusIntoConfig(cfg *corelib.AppConfig) {
	if cfg == nil {
		return
	}
	if cfg.MaclawLLMProviders == nil {
		cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{}
	}
	if strings.TrimSpace(cfg.RemoteViewerToken) == "" || strings.TrimSpace(cfg.RemoteHubURL) == "" {
		// Attempt auto-recovery of viewer token before giving up.
		if strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) == "" {
			recovered, err := a.ensureViewerToken(*cfg)
			if err == nil && strings.TrimSpace(recovered.RemoteViewerToken) != "" {
				*cfg = recovered
				// Fall through to the normal status-fetch path below.
			} else {
				if a.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{}) {
					_ = a.SaveConfig(*cfg)
				}
				return
			}
		} else {
			if a.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{}) {
				_ = a.SaveConfig(*cfg)
			}
			return
		}
	}
	status, err := a.fetchHubLLMServiceStatusWithTimeout(*cfg, hubServiceStatusTimeout)
	if err != nil {
		return
	}
	// Reload config before applying to avoid overwriting concurrent changes.
	freshCfg, loadErr := a.LoadConfig()
	if loadErr != nil {
		// Fall back to the passed-in config if reload fails.
		freshCfg = *cfg
	}
	if a.applyHubLLMServiceStatusToConfig(&freshCfg, status) {
		_ = a.SaveConfig(freshCfg)
	}
	// Update the caller's copy so syncedMaclawLLMProviders returns fresh data.
	*cfg = freshCfg
}

func (a *App) fetchHubLLMServiceStatus(cfg corelib.AppConfig) (HubLLMServiceStatus, error) {
	return a.fetchHubLLMServiceStatusWithTimeout(cfg, 30*time.Second)
}

func (a *App) fetchHubLLMServiceStatusWithTimeout(cfg corelib.AppConfig, timeout time.Duration) (HubLLMServiceStatus, error) {
	if strings.TrimSpace(cfg.RemoteHubURL) == "" {
		return HubLLMServiceStatus{}, fmt.Errorf("hub URL is not configured")
	}
	// Auto-recover missing viewer token via re-enroll.
	if strings.TrimSpace(cfg.RemoteViewerToken) == "" {
		recovered, err := a.ensureViewerToken(cfg)
		if err != nil {
			return HubLLMServiceStatus{}, err
		}
		cfg = recovered
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.RemoteHubURL, "/")+"/api/llm/service/status", nil)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.RemoteViewerToken))
	resp, err := hubHTTPClient.Do(req)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var failure map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&failure); err == nil {
			if msg, _ := failure["message"].(string); strings.TrimSpace(msg) != "" {
				return HubLLMServiceStatus{}, fmt.Errorf("%s", msg)
			}
		}
		return HubLLMServiceStatus{}, fmt.Errorf("status query failed: %s", resp.Status)
	}
	var status HubLLMServiceStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return HubLLMServiceStatus{}, err
	}
	return status, nil
}

func (a *App) applyHubLLMServiceStatusToConfig(cfg *corelib.AppConfig, status HubLLMServiceStatus) bool {
	if cfg == nil {
		return false
	}
	changed := false
	originalProviders := append([]corelib.MaclawLLMProvider(nil), cfg.MaclawLLMProviders...)
	providers := normalizeMaclawLLMProviders(originalProviders)
	if !maclawLLMProvidersEqual(originalProviders, providers) {
		changed = true
	}
	providerIndex := -1
	for i := range providers {
		if isHubServiceProviderName(providers[i].Name) {
			providerIndex = i
			break
		}
	}
	hasEntitlement := status.Active || len(status.CreditGrants) > 0 || len(status.ActiveGrants) > 0
	if !hasEntitlement || strings.TrimSpace(cfg.RemoteViewerToken) == "" || strings.TrimSpace(status.HubLLMBaseURL) == "" {
		if providerIndex >= 0 {
			providers = append(providers[:providerIndex], providers[providerIndex+1:]...)
			changed = true
			if isHubServiceProviderName(cfg.MaclawLLMCurrentProvider) {
				cfg.MaclawLLMCurrentProvider = ""
				changed = true
			}
		}
		if changed {
			cfg.MaclawLLMProviders = providers
		}
		return changed
	}
	model := hubServiceAutoModel
	if providerIndex >= 0 {
		existingModel := strings.TrimSpace(providers[providerIndex].Model)
		if existingModel != "" && !strings.EqualFold(existingModel, hubServiceAutoModel) {
			model = existingModel
		}
	}
	provider := corelib.MaclawLLMProvider{
		Name:          hubServiceProviderName,
		IsHubService:  true,
		URL:           strings.TrimRight(strings.TrimSpace(status.HubLLMBaseURL), "/"),
		Key:           strings.TrimSpace(cfg.RemoteViewerToken),
		Model:         model,
		Protocol:      "openai",
		ContextLength: corelib.DefaultContextTokens,
		TimeoutSec:    corelib.DefaultLLMTimeoutSec,
		AgentType:     "openclaw",
	}
	if providerIndex >= 0 {
		if providers[providerIndex] != provider {
			providers[providerIndex] = provider
			changed = true
		}
	} else {
		providers = append([]corelib.MaclawLLMProvider{provider}, providers...)
		changed = true
	}
	if canonicalCurrent := canonicalHubServiceProviderName(cfg.MaclawLLMCurrentProvider); canonicalCurrent != cfg.MaclawLLMCurrentProvider {
		cfg.MaclawLLMCurrentProvider = canonicalCurrent
		changed = true
	}
	if cfg.MaclawLLMCurrentProvider == "" || cfg.MaclawLLMCurrentProvider == hubServiceProviderName || !a.isMaclawLLMConfiguredWithConfig(*cfg) {
		if cfg.MaclawLLMCurrentProvider != hubServiceProviderName {
			cfg.MaclawLLMCurrentProvider = hubServiceProviderName
			changed = true
		}
	}
	if cfg.MaclawLLMUrl != provider.URL || cfg.MaclawLLMKey != provider.Key || cfg.MaclawLLMModel != provider.Model || cfg.MaclawLLMProtocol != provider.Protocol || cfg.MaclawLLMTimeoutSec != provider.TimeoutSec || cfg.MaclawLLMContextLength != provider.ContextLength {
		cfg.MaclawLLMUrl = provider.URL
		cfg.MaclawLLMKey = provider.Key
		cfg.MaclawLLMModel = provider.Model
		cfg.MaclawLLMProtocol = provider.Protocol
		cfg.MaclawLLMTimeoutSec = provider.TimeoutSec
		cfg.MaclawLLMContextLength = provider.ContextLength
		changed = true
	}
	if changed {
		cfg.MaclawLLMProviders = providers
	}
	return changed
}

func hubLLMServiceMissingProviderMessage(status HubLLMServiceStatus) string {
	grant := primaryHubLLMServiceGrant(status)
	switch normalizeHubLLMServiceGrantStatusKind(grant.Status) {
	case hubLLMServiceGrantStatusPeriodLimited:
		if retry := formatHubLLMServiceRetry(grant); retry != "" {
			return "MaClaw 官方周期限流：当前周期额度已用尽，约 " + retry + " 后恢复；请刷新 Hub 服务状态。"
		}
		return "MaClaw 官方周期限流：当前周期额度已用尽；请刷新 Hub 服务状态。"
	case hubLLMServiceGrantStatusQueued:
		if retry := formatHubLLMServiceRetry(grant); retry != "" {
			return "MaClaw 官方授权尚未生效：约 " + retry + " 后生效；请刷新 Hub 服务状态。"
		}
		return "MaClaw 官方授权尚未生效；请刷新 Hub 服务状态。"
	case hubLLMServiceGrantStatusExhausted:
		return "MaClaw 官方额度已用尽：请兑换或等待新的授权额度。"
	case hubLLMServiceGrantStatusExpired:
		return "MaClaw 官方授权已过期：请兑换新的授权额度。"
	}
	return "MaClaw 官方服务商暂不可用：Hub 未返回可用服务入口，请刷新 Hub 服务状态后重试。"
}

func primaryHubLLMServiceGrant(status HubLLMServiceStatus) HubLLMActiveGrant {
	grants := status.CreditGrants
	if len(grants) == 0 {
		grants = status.ActiveGrants
	}
	var best HubLLMActiveGrant
	bestRank := 99
	for _, grant := range grants {
		rank := hubLLMServiceGrantStatusRank(grant.Status)
		if rank < bestRank {
			best = grant
			bestRank = rank
		}
	}
	return best
}

func hubLLMServiceGrantStatusRank(status string) int {
	return normalizeHubLLMServiceGrantStatusKind(status).Rank()
}

func formatHubLLMServiceRetry(grant HubLLMActiveGrant) string {
	seconds := grant.RetryAfterSeconds
	if seconds <= 0 && strings.TrimSpace(grant.RetryAfterAt) != "" {
		if retryAt, err := time.Parse(time.RFC3339, strings.TrimSpace(grant.RetryAfterAt)); err == nil {
			seconds = int64(time.Until(retryAt).Seconds())
		}
	}
	if seconds <= 0 {
		return ""
	}
	if seconds >= int64(24*time.Hour/time.Second) {
		return fmt.Sprintf("%d 天", (seconds+int64(24*time.Hour/time.Second)-1)/int64(24*time.Hour/time.Second))
	}
	if seconds >= int64(time.Hour/time.Second) {
		return fmt.Sprintf("%d 小时", (seconds+int64(time.Hour/time.Second)-1)/int64(time.Hour/time.Second))
	}
	return fmt.Sprintf("%d 分钟", maxInt64(1, (seconds+int64(time.Minute/time.Second)-1)/int64(time.Minute/time.Second)))
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (a *App) isMaclawLLMConfiguredWithConfig(cfg corelib.AppConfig) bool {
	current := canonicalHubServiceProviderName(cfg.MaclawLLMCurrentProvider)
	for _, p := range cfg.MaclawLLMProviders {
		if canonicalHubServiceProviderName(p.Name) != current {
			continue
		}
		return strings.TrimSpace(p.URL) != "" && strings.TrimSpace(p.Model) != ""
	}
	return strings.TrimSpace(cfg.MaclawLLMUrl) != "" && strings.TrimSpace(cfg.MaclawLLMModel) != ""
}
