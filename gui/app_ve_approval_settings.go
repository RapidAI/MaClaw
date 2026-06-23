package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// GetVEApprovalConfig returns the current VE approval capability configuration.
// If no config is stored, returns the default configuration.
func (a *App) GetVEApprovalConfig() (*VEApprovalConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if cfg.VEApprovalConfigJSON == "" {
		def := DefaultVEApprovalConfig()
		return &def, nil
	}
	var approvalCfg VEApprovalConfig
	if err := json.Unmarshal([]byte(cfg.VEApprovalConfigJSON), &approvalCfg); err != nil {
		def := DefaultVEApprovalConfig()
		return &def, nil
	}
	return &approvalCfg, nil
}

// SaveVEApprovalConfig validates and persists the VE approval capability configuration.
func (a *App) SaveVEApprovalConfig(approvalCfg VEApprovalConfig) error {
	if err := approvalCfg.Validate(); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	data, err := json.Marshal(approvalCfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.VEApprovalConfigJSON = string(data)
	}); err != nil {
		return err
	}
	// Sync approval capability status to Hub. Best-effort: failure is logged
	// but does not block the local save. The UpdateVESettings path provides
	// a secondary sync on next VE profile update (startup, name change, etc.).
	a.syncApprovalCapabilityToHub(approvalCfg.Enabled)
	return nil
}

// syncApprovalCapabilityToHub notifies the Hub of this VE's approval capability
// status. Best-effort: logs failure but does not propagate error to caller.
// Uses a shorter timeout (5s) since this is a tiny payload and should not block
// the user's save experience for long when Hub is temporarily unreachable.
func (a *App) syncApprovalCapabilityToHub(enabled bool) {
	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return
	}
	body := map[string]any{
		"approval_capability_enabled": enabled,
	}
	if _, err := a.doHubJSONWithTimeout(hubURL, token, "PUT", "/api/ve/settings/approval_capability", body, 5*time.Second); err != nil {
		a.log("[ve-approval] sync to Hub failed: " + err.Error())
	}
}

// isApprovalCapabilityEnabled returns whether the local VE has approval capability enabled.
// Used by RegisterVirtualEmployee and UpdateVESettings to include the status in Hub requests.
func (a *App) isApprovalCapabilityEnabled() bool {
	cfg, err := a.GetVEApprovalConfig()
	if err != nil || cfg == nil {
		return false
	}
	return cfg.Enabled
}
