package llmservice

import (
	"context"
	"log"
)

// MaClawModule holds all MaClaw Official integration components for the Hub.
type MaClawModule struct {
	Client      *MaClawProviderClient
	AccessCtrl  *TenantLLMAccessControl
	AuthSync    *AuthSyncClient
}

// InitMaClawModule initializes the MaClaw Official provider integration.
// hubCenterURL is the base URL of the pinned HubCenter node.
// hubID is this Hub instance's ID.
// machineToken is the Hub's machine auth token (hub_secret).
// tenantIDsFunc returns the list of active tenant IDs to sync authorization for.
func InitMaClawModule(hubCenterURL, hubID, machineToken string, tenantIDsFunc func() []string) *MaClawModule {
	if hubCenterURL == "" {
		log.Printf("[maclaw-init] no HubCenter URL configured, MaClaw Official provider disabled")
		return nil
	}

	client := NewMaClawProviderClient(MaClawProviderConfig{
		HubCenterURL: hubCenterURL,
		HubID:        hubID,
		MachineToken: machineToken,
	})

	accessCtrl := NewTenantLLMAccessControl(client)
	authSync := NewAuthSyncClient(client, accessCtrl, tenantIDsFunc)

	// Start background authorization sync
	authSync.Start()

	log.Printf("[maclaw-init] MaClaw Official provider initialized (hubcenter=%s, hub=%s)", hubCenterURL, hubID)

	return &MaClawModule{
		Client:     client,
		AccessCtrl: accessCtrl,
		AuthSync:   authSync,
	}
}

// EnsureRegistryBuiltins injects the MaClaw Official provider and service group
// into the registry if not already present. Should be called on Hub startup
// and after registry loads.
func EnsureRegistryBuiltins(ctx context.Context, system SystemSettingsRepository) {
	reg, err := LoadRegistry(ctx, system)
	if err != nil {
		log.Printf("[maclaw-init] failed to load registry: %v", err)
		return
	}
	if EnsureBuiltinProvider(reg) {
		if err := SaveRegistry(ctx, system, reg); err != nil {
			log.Printf("[maclaw-init] failed to save registry with builtins: %v", err)
		} else {
			log.Printf("[maclaw-init] injected MaClaw Official builtin provider + service group")
		}
	}
}

// Shutdown stops background processes.
func (m *MaClawModule) Shutdown() {
	if m == nil {
		return
	}
	if m.AuthSync != nil {
		m.AuthSync.Stop()
	}
}
