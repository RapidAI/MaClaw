package compute

import (
	"errors"
	"sync"
)

// ErrNoPermission is returned when switching to local mode without compute_permission.
var ErrNoPermission = errors.New("compute_permission not granted: cannot switch to local mode")

// ErrInvalidSource is returned for unrecognised source values.
var ErrInvalidSource = errors.New("invalid source: must be \"cloud\" or \"local\"")

// ErrNotLocalMode is returned when trying to set local providers while in cloud mode.
var ErrNotLocalMode = errors.New("cannot set local providers while in cloud mode")

// SourceManager controls which provider list (cloud-synced vs local) is active.
// It is safe for concurrent use.
type SourceManager struct {
	mu                        sync.RWMutex
	source                    string // "cloud" or "local"
	syncMgr                   *SyncManager
	localProviders            []ComputeProvider
	fallbackProvidersResolver func() []ComputeProvider
}

// NewSourceManager creates a SourceManager that defaults to cloud mode.
func NewSourceManager(syncMgr *SyncManager) *SourceManager {
	sm := &SourceManager{
		source:  "cloud",
		syncMgr: syncMgr,
	}
	if syncMgr != nil {
		syncMgr.SetForceSyncObserver(sm.CheckForceSync)
	}
	return sm
}

// SetFallbackProvidersResolver configures the local provider fallback used when
// Cloud sync is unavailable and no cached Cloud provider list exists.
func (sm *SourceManager) SetFallbackProvidersResolver(resolve func() []ComputeProvider) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.fallbackProvidersResolver = resolve
}

// GetSource returns the current compute source ("cloud" or "local").
func (sm *SourceManager) GetSource() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.source
}

// SetSource switches the compute source. Switching to "local" requires
// compute_permission from the SyncManager; otherwise ErrNoPermission is returned.
func (sm *SourceManager) SetSource(source string) error {
	if source != "cloud" && source != "local" {
		return ErrInvalidSource
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if source == "local" && (sm.syncMgr == nil || !sm.syncMgr.GetComputePermission()) {
		return ErrNoPermission
	}

	sm.source = source
	return nil
}

// GetActiveProviders returns the provider list for the current mode.
// In cloud mode it delegates to SyncManager, unless Cloud is unavailable and
// no cached Cloud providers exist, in which case it uses the local fallback.
func (sm *SourceManager) GetActiveProviders() []ComputeProvider {
	providers, _, _ := sm.GetActiveProviderSnapshot()
	return providers
}

// GetActiveProviderSnapshot returns providers plus the effective source actually
// serving runtime requests. effectiveSource is cloud, local, or local_fallback.
func (sm *SourceManager) GetActiveProviderSnapshot() ([]ComputeProvider, string, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.source == "local" {
		out := make([]ComputeProvider, len(sm.localProviders))
		copy(out, sm.localProviders)
		return out, "local", false
	}
	if sm.syncMgr == nil {
		if sm.shouldUseFallbackLocked() {
			return sm.localFallbackProvidersLocked(), "local_fallback", true
		}
		return []ComputeProvider{}, "cloud", false
	}
	providers := sm.syncMgr.GetProviders()
	if len(providers) > 0 || !sm.shouldUseFallbackLocked() {
		return providers, "cloud", false
	}
	return sm.localFallbackProvidersLocked(), "local_fallback", true
}

// IsLocalEditAllowed returns true only when the source is "local" and
// compute_permission is granted.
func (sm *SourceManager) IsLocalEditAllowed() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.source == "local" && sm.syncMgr != nil && sm.syncMgr.GetComputePermission()
}

// HandleForceSync switches back to cloud mode and discards local providers.
func (sm *SourceManager) HandleForceSync() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.source = "cloud"
	sm.localProviders = nil
}

// CheckForceSync checks the SyncManager for a pending force_sync flag and,
// if set, calls HandleForceSync to revert to cloud mode.
func (sm *SourceManager) CheckForceSync() {
	if sm.syncMgr != nil && sm.syncMgr.HasForceSync() {
		sm.HandleForceSync()
	}
}

// SetLocalProviders replaces the local provider list. It only works when
// the source is "local"; otherwise ErrNotLocalMode is returned.
func (sm *SourceManager) SetLocalProviders(providers []ComputeProvider) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.source != "local" {
		return ErrNotLocalMode
	}

	cp := make([]ComputeProvider, len(providers))
	copy(cp, providers)
	sm.localProviders = cp
	return nil
}

// GetLocalProviders returns a copy of the local provider list.
func (sm *SourceManager) GetLocalProviders() []ComputeProvider {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	out := make([]ComputeProvider, len(sm.localProviders))
	copy(out, sm.localProviders)
	return out
}

func (sm *SourceManager) shouldUseFallbackLocked() bool {
	if sm.fallbackProvidersResolver == nil {
		return false
	}
	if sm.syncMgr == nil {
		return true
	}
	status := sm.syncMgr.GetSyncStatus()
	return status.Status == "pending" || status.Status == "failure" || status.Status == "waiting_for_credentials"
}

func (sm *SourceManager) localFallbackProvidersLocked() []ComputeProvider {
	if sm.fallbackProvidersResolver == nil {
		return nil
	}
	providers := sm.fallbackProvidersResolver()
	out := make([]ComputeProvider, len(providers))
	copy(out, providers)
	return out
}
