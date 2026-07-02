package notification

import (
	"encoding/json"
	"fmt"
	"time"
)

// ----- Broadcasting interfaces (used by Service) -----

// WSBroadcaster defines the interface for pushing WebSocket envelopes to clients.
// It decouples notification delivery from the concrete ws.Hub / device.Service
// implementation (dependency inversion).
type WSBroadcaster interface {
	// BroadcastToMachines sends an envelope (pre-serialized JSON bytes) to
	// the specified machine connections. Machines that are offline are silently
	// skipped (push + pull dual guarantee — offline machines pull on reconnect).
	BroadcastToMachines(machineIDs []string, envelope []byte) error

	// BroadcastToAll sends an envelope to all currently connected clients.
	BroadcastToAll(envelope []byte) error
}

// IMPusher defines the optional interface for pushing urgent notifications to
// IM channels (e.g., Feishu/WeChat/QQ). When configured, urgent notifications
// can be additionally pushed through IM channels for immediate user attention.
type IMPusher interface {
	// PushToIM sends a notification to the specified machines' associated users
	// via their IM channels.
	PushToIM(machineIDs []string, title, content string) error
}

// ----- Minimal interfaces for dependency inversion -----
// These allow the Pusher to adapt the Hub's device.Service without importing
// the device package directly.

// MachineSender is the minimal interface that the Hub's device.Service satisfies
// for sending messages to specific machines.
type MachineSender interface {
	SendToMachine(machineID string, msg any) error
}

// OnlineMachinesLister is the minimal interface for listing all currently
// connected machine IDs. The Hub's device.Service satisfies this.
type OnlineMachinesLister interface {
	ListOnlineMachineIDs() []string
}

// ----- Pusher — implements WSBroadcaster -----

// Pusher adapts the Hub's WebSocket device service to the WSBroadcaster interface.
// It sends pre-built notification envelopes to target machines via the existing
// Hub WebSocket infrastructure.
type Pusher struct {
	sender MachineSender
	lister OnlineMachinesLister
}

// NewPusher creates a Pusher that implements WSBroadcaster by adapting the given
// sender and lister. The sender is typically *device.Service (which implements
// SendToMachine). The lister provides all connected machine IDs for BroadcastToAll;
// if nil, BroadcastToAll returns an error.
func NewPusher(sender MachineSender, lister OnlineMachinesLister) *Pusher {
	return &Pusher{
		sender: sender,
		lister: lister,
	}
}

// BroadcastToMachines sends the envelope to each specified machine. Errors from
// individual sends are silently skipped — delivery is best-effort with pull-on-
// reconnect as fallback (NFR-2 push+pull dual guarantee, NFR-5 graceful degradation).
func (p *Pusher) BroadcastToMachines(machineIDs []string, envelope []byte) error {
	if p.sender == nil {
		return fmt.Errorf("notification pusher: sender is nil")
	}
	if len(machineIDs) == 0 {
		return nil
	}

	// Deserialize envelope into a generic map for SendToMachine (which
	// expects any JSON-serializable value, not raw bytes).
	var msg map[string]any
	if err := json.Unmarshal(envelope, &msg); err != nil {
		return fmt.Errorf("notification pusher: invalid envelope JSON: %w", err)
	}

	for _, machineID := range machineIDs {
		// Errors are silently ignored per design: offline machines will pull
		// unread notifications on reconnect (FR-3 push+pull dual guarantee).
		_ = p.sender.SendToMachine(machineID, msg)
	}
	return nil
}

// BroadcastToAll sends the envelope to all currently connected machines.
func (p *Pusher) BroadcastToAll(envelope []byte) error {
	if p.lister == nil {
		return fmt.Errorf("notification pusher: lister is nil, cannot broadcast to all")
	}

	machineIDs := p.lister.ListOnlineMachineIDs()
	return p.BroadcastToMachines(machineIDs, envelope)
}

// ----- Envelope construction helper -----

// NotificationEnvelopeType is the WebSocket envelope type for notification push messages.
const NotificationEnvelopeType = "notification.push"

// notificationEnvelope is a local construction helper that produces the same JSON
// shape as ws.Envelope without importing the ws package. This avoids a circular
// dependency when the ws package imports from this package in the future.
type notificationEnvelope struct {
	Type    string          `json:"type"`
	Ts      int64           `json:"ts"`
	Payload json.RawMessage `json:"payload"`
}

// BuildNotificationEnvelope constructs a JSON-serialized WebSocket envelope with
// type="notification.push" and the current Unix timestamp. The payload is the
// NotificationPushPayload (action="new"|"revoke" + notification data).
//
// This is a standalone helper that avoids importing the ws package. It produces
// the exact same JSON format as ws.Envelope{Type, TS, Payload}.
func BuildNotificationEnvelope(payload NotificationPushPayload) ([]byte, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("notification envelope: marshal payload: %w", err)
	}

	env := notificationEnvelope{
		Type:    NotificationEnvelopeType,
		Ts:      time.Now().Unix(),
		Payload: payloadBytes,
	}

	data, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("notification envelope: marshal envelope: %w", err)
	}
	return data, nil
}
