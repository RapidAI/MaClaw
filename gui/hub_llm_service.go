package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	hubServiceProviderName  = "MaClaw\u6a21\u578b\u670d\u52a1"
	hubServiceAutoModel     = "auto"
	hubServiceStatusTimeout = 2 * time.Second
)

type HubLLMAuthorizedModel struct {
	Name            string   `json:"name"`
	ProviderIDs     []string `json:"provider_ids,omitempty"`
	ServiceGroupIDs []string `json:"service_group_ids,omitempty"`
}

type HubLLMActiveGrant struct {
	ServiceGroupID string `json:"service_group_id"`
	Source         string `json:"source"`
	ExpiresAt      string `json:"expires_at"`
}

type HubLLMServiceStatus struct {
	Active            bool                    `json:"active"`
	SkipLLMConfig     bool                    `json:"skip_llm_config"`
	AuthMode          string                  `json:"auth_mode"`
	ServiceGroupIDs   []string                `json:"service_group_ids,omitempty"`
	ServiceGroupNames []string                `json:"service_group_names,omitempty"`
	AvailableModels   []string                `json:"available_models,omitempty"`
	AuthorizedModels  []HubLLMAuthorizedModel `json:"authorized_models,omitempty"`
	ActiveGrants      []HubLLMActiveGrant     `json:"active_grants,omitempty"`
	NearestExpiresAt  string                  `json:"nearest_expires_at,omitempty"`
	DefaultModel      string                  `json:"default_model,omitempty"`
	HubLLMBaseURL     string                  `json:"hub_llm_base_url,omitempty"`
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
	status, err := a.fetchHubLLMServiceStatus(cfg)
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	if a.applyHubLLMServiceStatusToConfig(&cfg, status) {
		if saveErr := a.SaveConfig(cfg); saveErr != nil {
			return status, saveErr
		}
	}
	return status, nil
}

func (a *App) RedeemHubLLMService(code string) (HubLLMServiceStatus, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return HubLLMServiceStatus{}, err
	}
	if strings.TrimSpace(cfg.RemoteHubURL) == "" {
		return HubLLMServiceStatus{}, fmt.Errorf("hub URL is not configured")
	}
	if strings.TrimSpace(cfg.RemoteViewerToken) == "" {
		return HubLLMServiceStatus{}, fmt.Errorf("hub access token is missing")
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
	if a.applyHubLLMServiceStatusToConfig(&cfg, result.ServiceStatus) {
		if err := a.SaveConfig(cfg); err != nil {
			return result.ServiceStatus, err
		}
	}
	return result.ServiceStatus, nil
}

func (a *App) syncHubLLMServiceStatusIntoConfig(cfg *AppConfig) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.RemoteViewerToken) == "" || strings.TrimSpace(cfg.RemoteHubURL) == "" {
		if a.applyHubLLMServiceStatusToConfig(cfg, HubLLMServiceStatus{}) {
			_ = a.SaveConfig(*cfg)
		}
		return
	}
	status, err := a.fetchHubLLMServiceStatusWithTimeout(*cfg, hubServiceStatusTimeout)
	if err != nil {
		return
	}
	if a.applyHubLLMServiceStatusToConfig(cfg, status) {
		_ = a.SaveConfig(*cfg)
	}
}

func (a *App) fetchHubLLMServiceStatus(cfg AppConfig) (HubLLMServiceStatus, error) {
	return a.fetchHubLLMServiceStatusWithTimeout(cfg, 30*time.Second)
}

func (a *App) fetchHubLLMServiceStatusWithTimeout(cfg AppConfig, timeout time.Duration) (HubLLMServiceStatus, error) {
	if strings.TrimSpace(cfg.RemoteHubURL) == "" {
		return HubLLMServiceStatus{}, fmt.Errorf("hub URL is not configured")
	}
	if strings.TrimSpace(cfg.RemoteViewerToken) == "" {
		return HubLLMServiceStatus{}, fmt.Errorf("hub access token is missing")
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

func (a *App) applyHubLLMServiceStatusToConfig(cfg *AppConfig, status HubLLMServiceStatus) bool {
	if cfg == nil {
		return false
	}
	changed := false
	providers := append([]MaclawLLMProvider(nil), cfg.MaclawLLMProviders...)
	providerIndex := -1
	for i := range providers {
		if providers[i].Name == hubServiceProviderName {
			providerIndex = i
			break
		}
	}
	if !status.Active || strings.TrimSpace(cfg.RemoteViewerToken) == "" || strings.TrimSpace(status.HubLLMBaseURL) == "" {
		if providerIndex >= 0 {
			providers = append(providers[:providerIndex], providers[providerIndex+1:]...)
			changed = true
			if cfg.MaclawLLMCurrentProvider == hubServiceProviderName {
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
	provider := MaclawLLMProvider{
		Name:          hubServiceProviderName,
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
		providers = append([]MaclawLLMProvider{provider}, providers...)
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

func (a *App) isMaclawLLMConfiguredWithConfig(cfg AppConfig) bool {
	for _, p := range cfg.MaclawLLMProviders {
		if p.Name != cfg.MaclawLLMCurrentProvider {
			continue
		}
		return strings.TrimSpace(p.URL) != "" && strings.TrimSpace(p.Model) != ""
	}
	return strings.TrimSpace(cfg.MaclawLLMUrl) != "" && strings.TrimSpace(cfg.MaclawLLMModel) != ""
}
