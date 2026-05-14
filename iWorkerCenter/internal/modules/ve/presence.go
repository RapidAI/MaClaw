package ve

import (
	"context"
	"log"
	"sync"
	"time"
)

const (
	DefaultHeartbeatInterval = 15 * time.Second
	DefaultMissThreshold     = 2 // consecutive missed heartbeats before offline
)

// PresenceManager tracks online/offline status of virtual employees
// based on WebSocket heartbeat signals.
type PresenceManager struct {
	mu                sync.RWMutex
	veStatus          map[string]*presenceState // key: ve_id
	heartbeatInterval time.Duration
	missThreshold     int
	onStatusChange    func(veID, status string) // callback when status changes
}

// presenceState tracks all connected instances for a single VE.
type presenceState struct {
	machineIDs   map[string]time.Time // machine_id → last heartbeat time
	onlineStatus string               // "online" or "offline"
}

// NewPresenceManager creates a new presence manager.
func NewPresenceManager() *PresenceManager {
	return &PresenceManager{
		veStatus:          make(map[string]*presenceState),
		heartbeatInterval: DefaultHeartbeatInterval,
		missThreshold:     DefaultMissThreshold,
	}
}

// SetOnStatusChange sets the callback invoked when a VE's online status changes.
func (pm *PresenceManager) SetOnStatusChange(fn func(veID, status string)) {
	pm.mu.Lock()
	pm.onStatusChange = fn
	pm.mu.Unlock()
}

// RecordHeartbeat records a heartbeat from a specific machine instance of a VE.
func (pm *PresenceManager) RecordHeartbeat(veID, machineID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	state, ok := pm.veStatus[veID]
	if !ok {
		state = &presenceState{
			machineIDs:   make(map[string]time.Time),
			onlineStatus: "offline",
		}
		pm.veStatus[veID] = state
	}

	state.machineIDs[machineID] = time.Now()

	// Transition to online if was offline
	if state.onlineStatus == "offline" {
		state.onlineStatus = "online"
		pm.notifyStatusChange(veID, "online")
	}
}

// OnWebSocketConnect marks a VE instance as connected.
func (pm *PresenceManager) OnWebSocketConnect(veID, machineID string) {
	pm.RecordHeartbeat(veID, machineID)
}

// OnWebSocketDisconnect removes a specific machine instance.
// If no instances remain, the VE goes offline.
func (pm *PresenceManager) OnWebSocketDisconnect(veID, machineID string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	state, ok := pm.veStatus[veID]
	if !ok {
		return
	}

	delete(state.machineIDs, machineID)

	// If no instances remain, mark offline
	if len(state.machineIDs) == 0 && state.onlineStatus == "online" {
		state.onlineStatus = "offline"
		pm.notifyStatusChange(veID, "offline")
	}
}

// IsOnline returns the current online status of a VE.
func (pm *PresenceManager) IsOnline(veID string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	state, ok := pm.veStatus[veID]
	if !ok {
		return false
	}
	return state.onlineStatus == "online"
}

// GetStatus returns "online" or "offline" for a VE.
func (pm *PresenceManager) GetStatus(veID string) string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	state, ok := pm.veStatus[veID]
	if !ok {
		return "offline"
	}
	return state.onlineStatus
}

// StartMonitor starts a background goroutine that periodically checks
// for missed heartbeats and marks VEs as offline.
func (pm *PresenceManager) StartMonitor(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(pm.heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pm.checkHeartbeats()
			}
		}
	}()
}

// checkHeartbeats scans all VE presence states and removes stale instances.
func (pm *PresenceManager) checkHeartbeats() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	deadline := time.Now().Add(-pm.heartbeatInterval * time.Duration(pm.missThreshold))

	for veID, state := range pm.veStatus {
		// Remove stale machine instances
		for machineID, lastHB := range state.machineIDs {
			if lastHB.Before(deadline) {
				delete(state.machineIDs, machineID)
				log.Printf("[ve-presence] machine %s of VE %s missed heartbeat (last: %s), removed",
					machineID, veID, lastHB.Format(time.RFC3339))
			}
		}

		// If all instances gone, mark offline
		if len(state.machineIDs) == 0 && state.onlineStatus == "online" {
			state.onlineStatus = "offline"
			pm.notifyStatusChange(veID, "offline")
		}
	}
}

// InstanceCount returns the number of active instances for a VE.
func (pm *PresenceManager) InstanceCount(veID string) int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	state, ok := pm.veStatus[veID]
	if !ok {
		return 0
	}
	return len(state.machineIDs)
}

func (pm *PresenceManager) notifyStatusChange(veID, status string) {
	if pm.onStatusChange != nil {
		go pm.onStatusChange(veID, status)
	}
}
