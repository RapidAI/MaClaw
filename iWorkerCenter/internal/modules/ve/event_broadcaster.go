package ve

import (
	"log"
	"sync"
	"time"
)

// ClientSender is the interface for sending messages to connected clients.
// Typically implemented by the device service (SendToMachine).
type ClientSender interface {
	SendToMachine(machineID string, msg any) error
}

// ConnectedClientsProvider lists all currently connected machine IDs.
type ConnectedClientsProvider interface {
	ListOnlineMachineIDs() []string
}

// EventBroadcaster pushes VE-related events to connected Maclaw clients.
// It wires into Registry.SetOnChange, PresenceManager.SetOnStatusChange,
// and AuthHandler.SetOnPush to broadcast events when state changes occur.
type EventBroadcaster struct {
	mu       sync.RWMutex
	sender   ClientSender
	clients  ConnectedClientsProvider
	registry *Registry

	// throttle: prevent high-frequency pushes from overwhelming clients
	lastBroadcast map[string]time.Time // event_type → last broadcast time
	throttleMs    int64                // minimum interval between same-type broadcasts
}

// NewEventBroadcaster creates a new event broadcaster.
func NewEventBroadcaster(sender ClientSender, clients ConnectedClientsProvider, registry *Registry) *EventBroadcaster {
	return &EventBroadcaster{
		sender:        sender,
		clients:       clients,
		registry:      registry,
		lastBroadcast: make(map[string]time.Time),
		throttleMs:    500, // 500ms throttle between same-type broadcasts
	}
}

// Wire connects the broadcaster to Registry, PresenceManager, and AuthHandler callbacks.
func (eb *EventBroadcaster) Wire(registry *Registry, presence *PresenceManager, authHandler *AuthHandler) {
	// Registry state changes → broadcast ve:list_update to all clients
	registry.SetOnChange(func() {
		eb.BroadcastListUpdate()
	})

	// Presence status changes → broadcast ve:status_change to all clients
	presence.SetOnStatusChange(func(veID, status string) {
		eb.BroadcastStatusChange(veID, status)
	})

	// Auth requests → push ve:auth_request to VE owner only
	authHandler.SetOnPush(func(ownerMachineID string, req AuthorizationRequest) {
		eb.PushAuthRequest(ownerMachineID, req)
	})
}

// BroadcastListUpdate sends a ve:list_update event to all connected clients.
// Triggered when VE list changes (register/approve/reject/disable/settings update).
func (eb *EventBroadcaster) BroadcastListUpdate() {
	if !eb.shouldBroadcast(VEEventListUpdate) {
		return
	}

	msg := map[string]any{
		"type": VEEventListUpdate,
		"ts":   time.Now().Unix(),
		"payload": map[string]any{
			"reason": "list_changed",
		},
	}

	eb.broadcastToAll(msg)
}

// BroadcastStatusChange sends a ve:status_change event to all connected clients.
// Triggered when a VE's online/offline status changes.
func (eb *EventBroadcaster) BroadcastStatusChange(veID, status string) {
	if !eb.shouldBroadcast(VEEventStatusChange) {
		return
	}

	// Also update the registry's online status
	if eb.registry != nil {
		eb.registry.SetOnlineStatus(veID, status)
	}

	msg := map[string]any{
		"type": VEEventStatusChange,
		"ts":   time.Now().Unix(),
		"payload": map[string]any{
			"ve_id":         veID,
			"online_status": status,
		},
	}

	eb.broadcastToAll(msg)
}

// BroadcastApproved sends a ve:approved event to the VE owner.
// Called after admin approves a registration.
func (eb *EventBroadcaster) BroadcastApproved(ownerMachineID, veID string) {
	msg := map[string]any{
		"type": VEEventApproved,
		"ts":   time.Now().Unix(),
		"payload": map[string]any{
			"ve_id": veID,
		},
	}
	eb.sendToClient(ownerMachineID, msg)
}

// BroadcastRejected sends a ve:rejected event to the VE owner.
// Called after admin rejects a registration.
func (eb *EventBroadcaster) BroadcastRejected(ownerMachineID, veID, reason string) {
	msg := map[string]any{
		"type": VEEventRejected,
		"ts":   time.Now().Unix(),
		"payload": map[string]any{
			"ve_id":  veID,
			"reason": reason,
		},
	}
	eb.sendToClient(ownerMachineID, msg)
}

// BroadcastDisabled sends a ve:disabled event to the VE owner.
// Called after admin disables a VE.
func (eb *EventBroadcaster) BroadcastDisabled(ownerMachineID, veID string) {
	msg := map[string]any{
		"type": VEEventDisabled,
		"ts":   time.Now().Unix(),
		"payload": map[string]any{
			"ve_id": veID,
		},
	}
	eb.sendToClient(ownerMachineID, msg)
}

// BroadcastGroupConfig sends a ve:group_config event to all connected clients.
// Called when admin changes group chat configuration.
func (eb *EventBroadcaster) BroadcastGroupConfig(maxParticipants int) {
	msg := map[string]any{
		"type": VEEventGroupConfig,
		"ts":   time.Now().Unix(),
		"payload": map[string]any{
			"max_group_participants": maxParticipants,
		},
	}
	eb.broadcastToAll(msg)
}

// PushAuthRequest sends a ve:auth_request event to the VE owner only.
// Called when another user requests access to a per_request VE.
func (eb *EventBroadcaster) PushAuthRequest(ownerMachineID string, req AuthorizationRequest) {
	payloadMap := map[string]any{
		"id":                   req.ID,
		"requester_name":       req.RequesterName,
		"requester_machine_id": req.RequesterMachineID,
		"target_ve_id":         req.TargetVEID,
		"target_ve_name":       req.TargetVEName,
		"created_at":           req.CreatedAt,
		"expires_at":           req.ExpiresAt,
	}

	msg := map[string]any{
		"type":    VEEventAuthRequest,
		"ts":      time.Now().Unix(),
		"payload": payloadMap,
	}
	eb.sendToClient(ownerMachineID, msg)
}

// --- Internal helpers ---

func (eb *EventBroadcaster) broadcastToAll(msg any) {
	if eb.clients == nil || eb.sender == nil {
		return
	}

	machineIDs := eb.clients.ListOnlineMachineIDs()
	for _, mid := range machineIDs {
		if err := eb.sender.SendToMachine(mid, msg); err != nil {
			log.Printf("[ve-events] broadcast to %s failed: %v", mid, err)
		}
	}
}

func (eb *EventBroadcaster) sendToClient(machineID string, msg any) {
	if eb.sender == nil || machineID == "" {
		return
	}
	if err := eb.sender.SendToMachine(machineID, msg); err != nil {
		log.Printf("[ve-events] send to %s failed: %v", machineID, err)
	}
}

func (eb *EventBroadcaster) shouldBroadcast(eventType string) bool {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	now := time.Now()
	last, ok := eb.lastBroadcast[eventType]
	if ok && now.Sub(last).Milliseconds() < eb.throttleMs {
		return false
	}
	eb.lastBroadcast[eventType] = now
	return true
}

// SetThrottleMs sets the minimum interval between same-type broadcasts.
// Used for testing.
func (eb *EventBroadcaster) SetThrottleMs(ms int64) {
	eb.mu.Lock()
	eb.throttleMs = ms
	eb.mu.Unlock()
}
