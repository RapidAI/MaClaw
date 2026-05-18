package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VEApprovalCapabilityStatus represents the result of checking a VE's approval capability.
type VEApprovalCapabilityStatus struct {
	VEID          string `json:"ve_id"`
	HasCapability bool   `json:"has_capability"`
	Enabled       bool   `json:"enabled"`
	Error         string `json:"error,omitempty"`
}

// CheckVEApprovalCapability checks whether the specified VE has approval capability enabled.
// For the local VE (empty veID or matching the local machine ID), it reads from the local config.
// For remote VEs, it queries the hub for the VE's capability status.
//
// Returns (true, nil) if the VE has approval capability enabled.
// Returns (false, nil) if the VE exists but does not have approval capability enabled.
// Returns (false, error) if the VE cannot be found or an error occurs.
func (a *App) CheckVEApprovalCapability(veID string) (bool, error) {
	if veID == "" {
		return false, fmt.Errorf("ve_id is required")
	}

	// Check if this is the local VE by comparing with the configured machine ID.
	localMachineID := a.getLocalVEMachineID()
	if veID == localMachineID {
		return a.checkLocalVEApprovalCapability()
	}

	// For remote VEs, query the hub for capability status.
	return a.checkRemoteVEApprovalCapability(veID)
}

// CheckVEApprovalCapabilityStatus returns a detailed status object for the VE's approval capability.
// This is the Wails binding exposed to the frontend for use in the workflow designer's
// approval node configuration panel.
func (a *App) CheckVEApprovalCapabilityStatus(veID string) VEApprovalCapabilityStatus {
	if veID == "" {
		return VEApprovalCapabilityStatus{
			VEID:          veID,
			HasCapability: false,
			Enabled:       false,
			Error:         "ve_id is required",
		}
	}

	enabled, err := a.CheckVEApprovalCapability(veID)
	status := VEApprovalCapabilityStatus{
		VEID:          veID,
		HasCapability: enabled,
		Enabled:       enabled,
	}
	if err != nil {
		status.Error = err.Error()
	}
	return status
}

// ValidateVEApproverAssignment validates that a VE can be assigned as an approver
// in the workflow designer. It checks that the VE has approval capability enabled.
// Returns nil if the assignment is valid, or an error describing why it's invalid.
func (a *App) ValidateVEApproverAssignment(veID string) error {
	enabled, err := a.CheckVEApprovalCapability(veID)
	if err != nil {
		return fmt.Errorf("cannot verify VE approval capability: %w", err)
	}
	if !enabled {
		return fmt.Errorf("VE %q does not have approval capability enabled; please enable it in the VE's settings before assigning as approver", veID)
	}
	return nil
}

// checkLocalVEApprovalCapability checks the local VE's approval capability from config.
func (a *App) checkLocalVEApprovalCapability() (bool, error) {
	cfg, err := a.GetVEApprovalConfig()
	if err != nil {
		return false, fmt.Errorf("failed to load VE approval config: %w", err)
	}
	return cfg.Enabled, nil
}

// checkRemoteVEApprovalCapability queries the hub for a remote VE's approval capability.
// The hub maintains knowledge of connected VEs and their reported capabilities.
func (a *App) checkRemoteVEApprovalCapability(veID string) (bool, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return false, fmt.Errorf("failed to load config: %w", err)
	}
	hubURL := strings.TrimSpace(cfg.RemoteHubURL)
	if hubURL == "" {
		return false, fmt.Errorf("hub URL not configured; cannot check remote VE capability")
	}

	endpoint := strings.TrimRight(hubURL, "/") + "/api/v1/ve/" + veID + "/approval_capability"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication token if available.
	if cfg.RemoteMachineToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.RemoteMachineToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to query hub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, fmt.Errorf("VE %q not found on hub", veID)
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("hub returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("failed to read hub response: %w", err)
	}

	var result struct {
		Enabled bool   `json:"enabled"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return false, fmt.Errorf("failed to parse hub response: %w", err)
	}
	if result.Error != "" {
		return false, fmt.Errorf("hub error: %s", result.Error)
	}
	return result.Enabled, nil
}

// getLocalVEMachineID returns the local machine's ID from config.
func (a *App) getLocalVEMachineID() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	return cfg.RemoteMachineID
}
