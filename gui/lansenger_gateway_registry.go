package main

import (
	"reflect"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
)

// lansengerGatewayRegistry is the process-level transport registry. It shares
// only the App and the global LLM scheduler underneath it; every entry owns a
// separate gateway manager, Agent runtime, durable history and FIFO worker.
type lansengerGatewayRegistry struct {
	app  *App
	mu   sync.Mutex
	bots map[string]*lansengerGatewayManager
}

func newLansengerGatewayRegistry(app *App) *lansengerGatewayRegistry {
	return &lansengerGatewayRegistry{app: app, bots: make(map[string]*lansengerGatewayManager)}
}

func lansengerBotProfileFingerprint(profile corelib.LansengerBotProfile) corelib.LansengerBotProfile {
	// A profile is a complete runtime contract. In particular, changing a
	// document directory, group permission, knowledge source or @ preference
	// must not leave a running manager on its stale snapshot.
	return profile
}

// syncFromConfig applies a profile diff. A changed profile is replaced as one
// unit so an old handler cannot retain prior prompt, expert or directory state.
func (r *lansengerGatewayRegistry) syncFromConfig(cfg corelib.AppConfig, forceRestart bool) {
	if r == nil {
		return
	}
	desired := make(map[string]corelib.LansengerBotProfile)
	for _, profile := range cfg.LansengerBots {
		profile.ID = strings.TrimSpace(profile.ID)
		if profile.ID != "" {
			desired[profile.ID] = profile
		}
	}

	var stop []*lansengerGatewayManager
	r.mu.Lock()
	for id, existing := range r.bots {
		profile, ok := desired[id]
		if !ok || !profile.Enabled || existing == nil || existing.profile == nil || !reflect.DeepEqual(lansengerBotProfileFingerprint(*existing.profile), lansengerBotProfileFingerprint(profile)) {
			delete(r.bots, id)
			stop = append(stop, existing)
		}
	}
	for id, profile := range desired {
		if profile.IsConfigured() && r.bots[id] == nil {
			r.bots[id] = newLansengerGatewayManagerForProfile(r.app, profile)
		}
	}
	start := make([]*lansengerGatewayManager, 0, len(r.bots))
	for _, manager := range r.bots {
		start = append(start, manager)
	}
	r.mu.Unlock()

	for _, manager := range stop {
		if manager != nil {
			manager.Stop()
		}
	}
	for _, manager := range start {
		manager.syncFromConfig(forceRestart)
	}
}

func (r *lansengerGatewayRegistry) stopAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	managers := make([]*lansengerGatewayManager, 0, len(r.bots))
	for _, manager := range r.bots {
		managers = append(managers, manager)
	}
	r.bots = make(map[string]*lansengerGatewayManager)
	r.mu.Unlock()
	for _, manager := range managers {
		if manager != nil {
			manager.Stop()
		}
	}
}

func (r *lansengerGatewayRegistry) isEmpty() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.bots) == 0
}

func (r *lansengerGatewayRegistry) defaultManager() *lansengerGatewayManager {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Legacy APIs without a profile selector must consistently resolve to the
	// migrated default profile; choosing the lexicographically first custom bot
	// would make scheduled delivery and historical status actions drift when a
	// new bot is added.
	return r.bots[corelib.DefaultLansengerBotProfileID]
}

func (r *lansengerGatewayRegistry) manager(profileID string) *lansengerGatewayManager {
	if r == nil {
		return nil
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bots[profileID]
}

// managers returns a stable snapshot for aggregate/read-only operations. It
// never exposes the registry map itself after releasing the lock.
func (r *lansengerGatewayRegistry) managers() []*lansengerGatewayManager {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	managers := make([]*lansengerGatewayManager, 0, len(r.bots))
	for _, manager := range r.bots {
		managers = append(managers, manager)
	}
	return managers
}

func (r *lansengerGatewayRegistry) status(profileID string) string {
	if manager := r.manager(profileID); manager != nil {
		return manager.Status()
	}
	return "disconnected"
}

// aggregateStatus summarizes every configured profile for legacy UI surfaces
// that have no current-bot selector. Connected wins: the channel is usable
// whenever at least one bot is online. Otherwise retain the most actionable
// transient/error state instead of presenting a misleading disconnected state.
func (r *lansengerGatewayRegistry) aggregateStatus() string {
	if r == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	managers := r.managers()

	status := gatewayConnectionStatusDisconnected.String()
	for _, manager := range managers {
		if manager == nil {
			continue
		}
		switch manager.Status() {
		case gatewayConnectionStatusConnected.String():
			return gatewayConnectionStatusConnected.String()
		case gatewayConnectionStatusReconnecting.String():
			if status != gatewayConnectionStatusConnecting.String() {
				status = gatewayConnectionStatusReconnecting.String()
			}
		case gatewayConnectionStatusConnecting.String():
			status = gatewayConnectionStatusConnecting.String()
		case gatewayConnectionStatusError.String():
			if status == gatewayConnectionStatusDisconnected.String() {
				status = gatewayConnectionStatusError.String()
			}
		}
	}
	return status
}

func (r *lansengerGatewayRegistry) restart(profileID string) string {
	if manager := r.manager(profileID); manager != nil {
		manager.Restart()
		return manager.Status()
	}
	return "disconnected"
}

func (r *lansengerGatewayRegistry) handlers() []*IMMessageHandler {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*IMMessageHandler, 0, len(r.bots))
	for _, manager := range r.bots {
		if handler := manager.currentLocalHandler(); handler != nil {
			result = append(result, handler)
		}
	}
	return result
}

func (r *lansengerGatewayRegistry) restartAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	managers := make([]*lansengerGatewayManager, 0, len(r.bots))
	for _, manager := range r.bots {
		managers = append(managers, manager)
	}
	r.mu.Unlock()
	for _, manager := range managers {
		if manager != nil {
			manager.Restart()
		}
	}
}
