package main

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// VE WebSocket event type constants (must match iWorkerCenter/internal/modules/ve/events.go).
const (
	veEventListUpdate    = "ve:list_update"
	veEventStatusChange  = "ve:status_change"
	veEventAuthRequest   = "ve:auth_request"
	veEventApproved      = "ve:approved"
	veEventRejected      = "ve:rejected"
	veEventDisabled      = "ve:disabled"
	veEventGroupConfig   = "ve:group_config"
)

// isVEEvent returns true if the message type is a VE-related event.
func isVEEvent(msgType string) bool {
	return strings.HasPrefix(msgType, "ve:")
}

// handleVEEvent processes VE-related WebSocket events from the Hub
// and forwards them to the frontend via Wails EventsEmit.
// This is called from the readLoop when a VE event is received.
func (c *RemoteHubClient) handleVEEvent(msg inboundHubEnvelope) {
	if c == nil || c.app == nil || c.app.ctx == nil {
		return
	}

	// Parse the payload for forwarding to frontend
	var payload map[string]any
	if len(msg.Payload) > 0 {
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			log.Printf("[hub-client] handleVEEvent: failed to parse payload for %s: %v", msg.Type, err)
			payload = map[string]any{}
		}
	} else {
		payload = map[string]any{}
	}

	// Construct the event data to emit to frontend
	eventData := map[string]any{
		"type":    msg.Type,
		"ts":     msg.TS,
		"payload": payload,
	}

	// Emit to frontend using the event type as the Wails event name.
	// Frontend subscribes to these events via runtime.EventsOn("ve:list_update", ...) etc.
	runtime.EventsEmit(c.app.ctx, msg.Type, eventData)

	// Also emit a generic "ve-event" for components that want to listen to all VE events
	runtime.EventsEmit(c.app.ctx, "ve-event", eventData)

	log.Printf("[hub-client] handleVEEvent: forwarded %s to frontend", msg.Type)
}
