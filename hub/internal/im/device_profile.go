package im

import (
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// DeviceProfile holds project context reported by a connected MaClaw client.
// Stored in memory only; clients re-report on reconnect.
type DeviceProfile struct {
	MachineID      string   `json:"machine_id"`
	Name           string   `json:"name"`
	LLMConfigured  bool     `json:"llm_configured"`
	ProjectPath    string   `json:"project_path,omitempty"`
	Language       string   `json:"language,omitempty"`
	Framework      string   `json:"framework,omitempty"`
	ActiveSessions []string `json:"active_sessions,omitempty"`
}

// DeviceProfileCache is a thread-safe in-memory cache of device profiles.
type DeviceProfileCache struct {
	mu       sync.RWMutex
	profiles map[string]map[string]DeviceProfile
}

// NewDeviceProfileCache creates an empty cache.
func NewDeviceProfileCache() *DeviceProfileCache {
	return &DeviceProfileCache{profiles: make(map[string]map[string]DeviceProfile)}
}

// Update adds or replaces a device profile for the default tenant.
func (c *DeviceProfileCache) Update(userID string, profile DeviceProfile) {
	c.UpdateForTenant(store.DefaultTenantID, userID, profile)
}

// UpdateForTenant adds or replaces a device profile for the given tenant/user.
func (c *DeviceProfileCache) UpdateForTenant(tenantID, userID string, profile DeviceProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := deviceProfileCacheKey(tenantID, userID)
	m, ok := c.profiles[key]
	if !ok {
		m = make(map[string]DeviceProfile)
		c.profiles[key] = m
	}
	m[profile.MachineID] = profile
}

// Remove deletes a device profile from the default tenant.
func (c *DeviceProfileCache) Remove(userID, machineID string) {
	c.RemoveForTenant(store.DefaultTenantID, userID, machineID)
}

// RemoveForTenant deletes a device profile, e.g. on disconnect.
func (c *DeviceProfileCache) RemoveForTenant(tenantID, userID, machineID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := deviceProfileCacheKey(tenantID, userID)
	m, ok := c.profiles[key]
	if !ok {
		return
	}
	delete(m, machineID)
	if len(m) == 0 {
		delete(c.profiles, key)
	}
}

// GetAll returns all default-tenant device profiles for a user. Returns nil if none.
func (c *DeviceProfileCache) GetAll(userID string) []DeviceProfile {
	return c.GetAllForTenant(store.DefaultTenantID, userID)
}

// GetAllForTenant returns all device profiles for a tenant/user. Returns nil if none.
func (c *DeviceProfileCache) GetAllForTenant(tenantID, userID string) []DeviceProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.profiles[deviceProfileCacheKey(tenantID, userID)]
	if !ok || len(m) == 0 {
		return nil
	}
	out := make([]DeviceProfile, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	return out
}

func deviceProfileCacheKey(tenantID, userID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = store.DefaultTenantID
	}
	return tenantID + "\x00" + strings.TrimSpace(userID)
}
