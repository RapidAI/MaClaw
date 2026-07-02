package ws

import "encoding/json"

// WebSocket message type constants.
// Each constant represents the "type" field in an Envelope, following the
// "domain.action" naming convention (e.g., "auth.ok", "session.summary").
const (
	// MessageTypeNotificationPush is sent Server → Client to push a new
	// notification or revoke an existing one.
	MessageTypeNotificationPush = "notification.push"

	// MessageTypeNotificationAck is sent Client → Server to acknowledge
	// (mark as read) a notification.
	MessageTypeNotificationAck = "notification.ack"
)

type Envelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	TS        int64           `json:"ts,omitempty"`
	MachineID string          `json:"machine_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}
