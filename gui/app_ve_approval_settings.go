package main

import (
	"encoding/json"
	"fmt"

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
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.VEApprovalConfigJSON = string(data)
	})
}
