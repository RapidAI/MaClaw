package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	hubServiceProviderName            = "MaClaw\u5b98\u65b9"
	hubServiceAutoModel               = "auto"
	hubServiceStatusTimeout           = 2 * time.Second
	hubServiceAccountStatusMaxTimeout = 5 * time.Second
	hubViewerTokenRecoveryRetryDelay  = 5 * time.Minute
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
	if !a.shouldAttemptHubViewerTokenRecovery() {
		return cfg, fmt.Errorf("hub access token is missing (recovery throttled)")
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
	if !a.shouldAttemptHubViewerTokenRecovery() {
		return cfg, fmt.Errorf("hub access token is missing (recovery throttled)")
	}

	log.Printf("[hub-llm-service] viewer token missing, re-enrolling email=%s", freshCfg.RemoteEmail)
	result, err := a.ActivateRemote(freshCfg.RemoteEmail, "", "")
	if err != nil {
		a.deferHubViewerTokenRecovery(hubViewerTokenRecoveryRetryDelay)
		log.Printf("[hub-llm-service] re-enroll failed: %v", err)
		return cfg, fmt.Errorf("hub access token is missing (recovery failed: %v)", err)
	}
	if result.ViewerToken == "" {
		a.deferHubViewerTokenRecovery(hubViewerTokenRecoveryRetryDelay)
		log.Printf("[hub-llm-service] re-enroll succeeded but hub did not issue viewer token")
		return cfg, fmt.Errorf("hub access token is missing (hub did not issue token)")
	}

	a.hubViewerTokenRecoveryNextAttempt.Store(time.Time{})
	log.Printf("[hub-llm-service] viewer token recovered via re-enroll")
	// ActivateRemote persisted via PatchConfig; reload to get the fresh copy.
	updated, err := a.LoadConfig()
	if err != nil {
		return cfg, err
	}
	return updated, nil
}

func (a *App) shouldAttemptHubViewerTokenRecovery() bool {
	if a == nil {
		return false
	}
	if next, ok := a.hubViewerTokenRecoveryNextAttempt.Load().(time.Time); ok && !next.IsZero() && time.Now().Before(next) {
		return false
	}
	return true
}

func (a *App) deferHubViewerTokenRecovery(delay time.Duration) {
	if a == nil {
		return
	}
	if delay <= 0 {
		delay = hubViewerTokenRecoveryRetryDelay
	}
	a.hubViewerTokenRecoveryNextAttempt.Store(time.Now().Add(delay))
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

type hubLLMServiceAccountResponse struct {
	Status        HubLLMServiceStatus `json:"status"`
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
	if _, err := a.syncHubLLMServiceStatusToConfig(status, false); err != nil {
		return status, err
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
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hubLLMServiceURL(cfg.RemoteHubURL, "/api/llm/service/redeem"), bytes.NewReader(payload))
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.RemoteViewerToken))
	req.Header.Set("Cache-Control", "no-cache")
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
	serviceStatus := result.ServiceStatus
	if refreshed, err := a.fetchHubLLMServiceStatus(cfg); err == nil || hubLLMServiceStatusEmpty(serviceStatus) {
		if err != nil {
			return HubLLMServiceStatus{}, err
		}
		serviceStatus = refreshed
	}
	if _, err := a.syncHubLLMServiceStatusToConfig(serviceStatus, false); err != nil {
		return serviceStatus, err
	}
	return serviceStatus, nil
}

func (a *App) syncHubLLMServiceStatusIntoConfig(cfg *corelib.AppConfig) {
	if cfg == nil {
		return
	}
	if cfg.MaclawLLMProviders == nil {
		cfg.MaclawLLMProviders = []corelib.MaclawLLMProvider{}
	}
	if strings.TrimSpace(cfg.RemoteHubURL) == "" {
		return
	}
	if strings.TrimSpace(cfg.RemoteViewerToken) == "" {
		// Attempt auto-recovery of viewer token before giving up.
		recovered, err := a.ensureViewerToken(*cfg)
		if err == nil && strings.TrimSpace(recovered.RemoteViewerToken) != "" {
			*cfg = recovered
			// Fall through to the normal status-fetch path below.
		} else {
			_, _ = a.syncHubLLMServiceStatusToConfig(HubLLMServiceStatus{}, false)
			if freshCfg, err := a.LoadConfig(); err == nil {
				*cfg = freshCfg
			}
			return
		}
	}
	status, err := a.fetchHubLLMServiceStatusWithTimeout(*cfg, hubServiceStatusTimeout)
	if err != nil {
		return
	}
	_, _ = a.syncHubLLMServiceStatusToConfig(status, false)
	if freshCfg, err := a.LoadConfig(); err == nil {
		// Update the caller's copy so syncedMaclawLLMProviders returns fresh data.
		*cfg = freshCfg
	}
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
	if status, err := a.fetchHubLLMServiceAccountStatus(cfg, hubLLMServiceAccountStatusTimeout(timeout)); err == nil {
		if hubLLMServiceStatusNeedsRouteDetails(status) {
			if routeStatus, routeErr := a.fetchHubLLMServiceLegacyStatus(cfg, timeout); routeErr == nil {
				status = mergeHubLLMServiceRouteDetails(status, routeStatus)
			}
		}
		return status, nil
	} else if !isHubLLMServiceAccountFallbackError(err) {
		return HubLLMServiceStatus{}, err
	}
	return a.fetchHubLLMServiceLegacyStatus(cfg, timeout)
}

func (a *App) fetchHubLLMServiceLegacyStatus(cfg corelib.AppConfig, timeout time.Duration) (HubLLMServiceStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hubLLMServiceURL(cfg.RemoteHubURL, "/api/llm/service/status"), nil)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.RemoteViewerToken))
	req.Header.Set("Cache-Control", "no-cache")
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

func hubLLMServiceStatusNeedsRouteDetails(status HubLLMServiceStatus) bool {
	return strings.TrimSpace(status.HubLLMBaseURL) == "" || len(status.AvailableModels) == 0 || strings.TrimSpace(status.DefaultModel) == ""
}

func mergeHubLLMServiceRouteDetails(accountStatus, routeStatus HubLLMServiceStatus) HubLLMServiceStatus {
	merged := accountStatus
	if strings.TrimSpace(merged.HubLLMBaseURL) == "" {
		merged.HubLLMBaseURL = routeStatus.HubLLMBaseURL
	}
	if strings.TrimSpace(merged.DefaultModel) == "" {
		merged.DefaultModel = routeStatus.DefaultModel
	}
	if len(merged.AvailableModels) == 0 {
		merged.AvailableModels = append([]string(nil), routeStatus.AvailableModels...)
	}
	if len(merged.AuthorizedModels) == 0 {
		merged.AuthorizedModels = append([]HubLLMAuthorizedModel(nil), routeStatus.AuthorizedModels...)
	}
	if len(merged.InactiveReasons) == 0 {
		merged.InactiveReasons = append([]string(nil), routeStatus.InactiveReasons...)
	}
	return merged
}

func (a *App) fetchHubLLMServiceAccountStatus(cfg corelib.AppConfig, timeout time.Duration) (HubLLMServiceStatus, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hubLLMServiceURL(cfg.RemoteHubURL, "/api/llm/service/account"), nil)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.RemoteViewerToken))
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := hubHTTPClient.Do(req)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var failure map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&failure); err == nil {
			if msg, _ := failure["message"].(string); strings.TrimSpace(msg) != "" {
				return HubLLMServiceStatus{}, fmt.Errorf("account status query failed: %s: %s", resp.Status, msg)
			}
		}
		return HubLLMServiceStatus{}, fmt.Errorf("account status query failed: %s", resp.Status)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return HubLLMServiceStatus{}, fmt.Errorf("account status query failed: decode response: %w", err)
	}
	status, err := decodeHubLLMServiceAccountStatus(raw)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	if hubLLMServiceStatusEmpty(status) {
		return HubLLMServiceStatus{}, fmt.Errorf("account status query failed: empty status")
	}
	return status, nil
}

func decodeHubLLMServiceAccountStatus(raw json.RawMessage) (HubLLMServiceStatus, error) {
	var result hubLLMServiceAccountResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return HubLLMServiceStatus{}, fmt.Errorf("account status query failed: decode response: %w", err)
	}
	if !hubLLMServiceStatusEmpty(result.Status) {
		return result.Status, nil
	}
	if !hubLLMServiceStatusEmpty(result.ServiceStatus) {
		return result.ServiceStatus, nil
	}
	var status HubLLMServiceStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return HubLLMServiceStatus{}, fmt.Errorf("account status query failed: decode response: %w", err)
	}
	return status, nil
}

func hubLLMServiceURL(baseURL, path string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + path + "?t=" + strconv.FormatInt(time.Now().UnixNano(), 10)
}

func hubLLMServiceStatusEmpty(status HubLLMServiceStatus) bool {
	return strings.TrimSpace(status.HubLLMBaseURL) == "" && len(status.ServiceGroupIDs) == 0 && len(status.ServiceGroupNames) == 0 && len(status.CreditGrants) == 0 && len(status.ActiveGrants) == 0 && len(status.AvailableModels) == 0 && len(status.AuthorizedModels) == 0 && status.CreditsTotal <= 0 && status.CreditsUsed <= 0 && status.CreditsRemaining <= 0 && status.CreditsAvailable <= 0
}

func hubLLMServiceAccountStatusTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 || timeout > hubServiceAccountStatusMaxTimeout {
		return hubServiceAccountStatusMaxTimeout
	}
	return timeout
}

func isHubLLMServiceAccountFallbackError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "401") || strings.Contains(msg, "403") {
		return false
	}
	if strings.Contains(msg, "404") || strings.Contains(msg, "405") || strings.Contains(msg, "decode response") || strings.Contains(msg, "empty status") {
		return true
	}
	for code := http.StatusInternalServerError; code <= 599; code++ {
		if strings.Contains(msg, strconv.Itoa(code)) {
			return true
		}
	}
	return false
}

func isHubLLMServiceAuthorizationError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") || strings.Contains(msg, "403")
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
	hasEntitlement := hubLLMServiceStatusHasEntitlement(status)
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

func hubLLMServiceStatusHasEntitlement(status HubLLMServiceStatus) bool {
	return status.Active || len(status.CreditGrants) > 0 || len(status.ActiveGrants) > 0
}

func (a *App) syncHubLLMServiceStatusToConfig(status HubLLMServiceStatus, forceCurrentProvider bool) (bool, error) {
	if cfg, err := a.LoadConfig(); err == nil {
		preview := cfg
		if !a.applyHubLLMServiceStatusPatchToConfig(&preview, status, forceCurrentProvider) {
			return false, nil
		}
	}
	changed, err := a.PatchConfigIfChanged(func(cfg *corelib.AppConfig) bool {
		return a.applyHubLLMServiceStatusPatchToConfig(cfg, status, forceCurrentProvider)
	})
	if err != nil {
		return false, err
	}
	if !changed {
		return false, nil
	}
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "hub-llm-service-changed")
	}
	return true, nil
}

func (a *App) applyHubLLMServiceStatusPatchToConfig(cfg *corelib.AppConfig, status HubLLMServiceStatus, forceCurrentProvider bool) bool {
	changed := a.applyHubLLMServiceStatusToConfig(cfg, status)
	if forceCurrentProvider && hubLLMServiceStatusHasEntitlement(status) && strings.TrimSpace(cfg.RemoteViewerToken) != "" && strings.TrimSpace(status.HubLLMBaseURL) != "" && cfg.MaclawLLMCurrentProvider != hubServiceProviderName {
		cfg.MaclawLLMCurrentProvider = hubServiceProviderName
		changed = true
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
