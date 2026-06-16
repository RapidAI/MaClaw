package llmservice

import (
	"context"
	"sync"
	"time"
)

// TenantLLMAccessControl manages the "algorithm access" authorization status
// for each tenant, determining whether they can add third-party LLM providers
// or are restricted to the built-in MaClaw Official only.
type TenantLLMAccessControl struct {
	mu     sync.RWMutex
	cache  map[string]*cachedAuthStatus // key: tenantID
	client *MaClawProviderClient
}

type cachedAuthStatus struct {
	status    *TenantAuthorizationStatus
	fetchedAt time.Time
}

const authCacheTTL = 5 * time.Minute

// NewTenantLLMAccessControl creates a new access controller.
func NewTenantLLMAccessControl(client *MaClawProviderClient) *TenantLLMAccessControl {
	return &TenantLLMAccessControl{
		cache:  map[string]*cachedAuthStatus{},
		client: client,
	}
}

// CanAddExternalProvider returns true if the tenant is authorized to add
// third-party LLM providers. If false, returns a user-facing message.
func (ac *TenantLLMAccessControl) CanAddExternalProvider(ctx context.Context, tenantID string) (bool, string) {
	status := ac.getStatus(ctx, tenantID)
	if status != nil && status.AllowExternalProviders {
		return true, ""
	}
	return false, "需要获得 MaClaw 官方算力模块授权才能添加自定义 LLM 服务。请联系 MaClaw 官方获取授权。"
}

// GetAuthorizationStatus returns the full authorization status for a tenant.
func (ac *TenantLLMAccessControl) GetAuthorizationStatus(ctx context.Context, tenantID string) *TenantAuthorizationStatus {
	return ac.getStatus(ctx, tenantID)
}

// RefreshAuthorizationStatus bypasses the short-lived cache and asks HubCenter
// for the latest tenant authorization status. If the refresh fails, callers can
// still use GetAuthorizationStatus to fall back to the cached value.
func (ac *TenantLLMAccessControl) RefreshAuthorizationStatus(ctx context.Context, tenantID string) (*TenantAuthorizationStatus, error) {
	if ac == nil || ac.client == nil {
		return nil, nil
	}
	status, err := ac.client.QueryAuthorization(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	ac.UpdateFromHeartbeat(tenantID, status)
	return status, nil
}

// InvalidateCache clears the cached status for a tenant (e.g., after heartbeat sync).
func (ac *TenantLLMAccessControl) InvalidateCache(tenantID string) {
	ac.mu.Lock()
	delete(ac.cache, tenantID)
	ac.mu.Unlock()
}

// InvalidateAll clears all cached statuses.
func (ac *TenantLLMAccessControl) InvalidateAll() {
	ac.mu.Lock()
	ac.cache = map[string]*cachedAuthStatus{}
	ac.mu.Unlock()
}

// CleanupStale removes cached entries that haven't been refreshed in over 1 hour.
// Should be called periodically (e.g., every 10 minutes).
func (ac *TenantLLMAccessControl) CleanupStale() {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	cutoff := time.Now().Add(-time.Hour)
	for tid, entry := range ac.cache {
		if entry.fetchedAt.Before(cutoff) {
			delete(ac.cache, tid)
		}
	}
}

// UpdateFromHeartbeat updates cached authorization from heartbeat response data.
func (ac *TenantLLMAccessControl) UpdateFromHeartbeat(tenantID string, status *TenantAuthorizationStatus) {
	if status == nil {
		return
	}
	ac.mu.Lock()
	ac.cache[tenantID] = &cachedAuthStatus{
		status:    status,
		fetchedAt: time.Now(),
	}
	ac.mu.Unlock()
}

func (ac *TenantLLMAccessControl) getStatus(ctx context.Context, tenantID string) *TenantAuthorizationStatus {
	// Check cache first
	ac.mu.RLock()
	cached := ac.cache[tenantID]
	ac.mu.RUnlock()

	if cached != nil && time.Since(cached.fetchedAt) < authCacheTTL {
		return cached.status
	}

	// Fetch from HubCenter
	if ac.client == nil {
		return nil
	}
	status, err := ac.client.QueryAuthorization(ctx, tenantID)
	if err != nil {
		// On error, return cached (possibly stale) or nil
		if cached != nil {
			return cached.status
		}
		return nil
	}

	// Update cache
	ac.mu.Lock()
	ac.cache[tenantID] = &cachedAuthStatus{
		status:    status,
		fetchedAt: time.Now(),
	}
	ac.mu.Unlock()
	return status
}

// ---------------------------------------------------------------------------
// Service Group filtering
// ---------------------------------------------------------------------------

// FilterServiceGroupsForTenant returns only the service groups visible to a tenant.
// Unauthorized tenants see only the MaClaw Official group.
func FilterServiceGroupsForTenant(ctx context.Context, ac *TenantLLMAccessControl, tenantID string, allGroups []ModelServiceGroup) []ModelServiceGroup {
	if ac == nil {
		return allGroups
	}

	canExternal, _ := ac.CanAddExternalProvider(ctx, tenantID)
	if canExternal {
		return allGroups
	}

	// Only return the builtin MaClaw Official group
	var filtered []ModelServiceGroup
	for _, g := range allGroups {
		if IsBuiltinServiceGroup(g.ID) {
			filtered = append(filtered, g)
		}
	}
	return filtered
}

// FilterProvidersForTenant returns only the providers visible to a tenant.
func FilterProvidersForTenant(ctx context.Context, ac *TenantLLMAccessControl, tenantID string, allProviders []ModelServiceModel) []ModelServiceModel {
	if ac == nil {
		return allProviders
	}

	canExternal, _ := ac.CanAddExternalProvider(ctx, tenantID)
	if canExternal {
		return allProviders
	}

	// Only return models from MaClaw Official provider
	var filtered []ModelServiceModel
	for _, m := range allProviders {
		hasBuiltin := false
		for _, pid := range m.ProviderIDs {
			if pid == MaClawOfficialProviderID {
				hasBuiltin = true
				break
			}
		}
		if hasBuiltin {
			filtered = append(filtered, m)
		}
	}
	return filtered
}
