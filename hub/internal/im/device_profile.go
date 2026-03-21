package im

import "sync"

// DeviceProfile holds project context reported by a connected MaClaw client.
// Stored in memory only — not persisted. Clients re-report on reconnect.
type DeviceProfile struct {
	MachineID      string   `json:"machine_id"`
	Name           string   `json:"name"`
	LLMConfigured  bool     `json:"llm_configured"`
	ProjectPath    string   `json:"project_path,omitempty"`
	Language       string   `json:"language,omitempty"`
	Framework      string   `json:"framework,omitempty"`
	ActiveSessions []string `json:"active_sessions,omitempty"`
}

// DeviceProfileCache is a thread-safe in-memory cache of device profiles
// keyed by userID → machineID.
type DeviceProfileCache struct {
	mu       sync.RWMutex
	profiles map[string]map[string]DeviceProfile // userID → machineID → profile
}

// NewDeviceProfileCache creates an empty cache.
func NewDeviceProfileCache() *DeviceProfileCache {
	return &DeviceProfileCache{
		profiles: make(map[string]map[string]DeviceProfile),
	}
}

// Update adds or replaces a device profile for the given user.
func (c *DeviceProfileCache) Update(userID string, profile DeviceProfile) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.profiles[userID]
	if !ok {
		m = make(map[string]DeviceProfile)
		c.profiles[userID] = m
	}
	m[profile.MachineID] = profile
}

// Remove deletes a device profile (e.g. on disconnect).
func (c *DeviceProfileCache) Remove(userID, machineID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.profiles[userID]
	if !ok {
		return
	}
	delete(m, machineID)
	if len(m) == 0 {
		delete(c.profiles, userID)
	}
}

// GetAll returns all device profiles for a user. Returns nil if none.
func (c *DeviceProfileCache) GetAll(userID string) []DeviceProfile {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.profiles[userID]
	if !ok || len(m) == 0 {
		return nil
	}
	out := make([]DeviceProfile, 0, len(m))
	for _, p := range m {
		out = append(out, p)
	}
	return out
}
