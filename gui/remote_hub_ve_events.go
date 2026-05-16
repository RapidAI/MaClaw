package main

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// VE WebSocket event type constants emitted by Maclaw Hub.
const (
	veEventListUpdate        = "ve:list_update"
	veEventStatusChange      = "ve:status_change"
	veEventAuthRequest       = "ve:auth_request"
	veEventApproved          = "ve:approved"
	veEventRejected          = "ve:rejected"
	veEventDisabled          = "ve:disabled"
	veEventGroupConfig       = "ve:group_config"
	veEventDiscussionMessage = "ve:discussion_message"
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
	if strings.TrimSpace(msg.Type) == veEventDiscussionMessage {
		c.handleVEDiscussionMessage(msg)
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
		"ts":      msg.TS,
		"payload": payload,
	}

	// Emit to frontend using the event type as the Wails event name.
	// Frontend subscribes to these events via runtime.EventsOn("ve:list_update", ...) etc.
	runtime.EventsEmit(c.app.ctx, msg.Type, eventData)

	// Also emit a generic "ve-event" for components that want to listen to all VE events
	runtime.EventsEmit(c.app.ctx, "ve-event", eventData)

	log.Printf("[hub-client] handleVEEvent: forwarded %s to frontend", msg.Type)
}

func (c *RemoteHubClient) digitalEmployeeMessageHandler() *VEMessageHandler {
	if c == nil || c.app == nil {
		return nil
	}
	c.veHandlerMu.Lock()
	defer c.veHandlerMu.Unlock()
	if c.veHandler == nil {
		c.veHandler = NewVEMessageHandler(c.app)
	}
	return c.veHandler
}

func (c *RemoteHubClient) groupChatDispatcher() *GroupChatDispatcher {
	if c == nil || c.app == nil {
		return nil
	}
	c.veHandlerMu.Lock()
	defer c.veHandlerMu.Unlock()
	if c.groupDispatcher == nil {
		c.groupDispatcher = NewGroupChatDispatcher(c.app)
	}
	return c.groupDispatcher
}

func (c *RemoteHubClient) handleVEDiscussionMessage(msg inboundHubEnvelope) {
	envelope, targetRole, err := decodeVEDiscussionPayload(msg.Payload)
	if err != nil {
		log.Printf("[hub-client] handleVEDiscussionMessage: failed to parse payload: %v", err)
		return
	}
	if envelope.Message == nil {
		return
	}
	targetRole = strings.TrimSpace(targetRole)
	sessionID := firstNonEmptyGroupString(envelope.SessionID, envelope.Message.SessionID, msg.SessionID)
	content := envelope.Message.Content
	eventPayload := map[string]any{"session_id": sessionID, "content": content, "chunk": content}
	switch envelope.Message.Kind {
	case a2a.MessageStreamChunk:
		runtime.EventsEmit(c.app.ctx, "ve:stream_chunk", eventPayload)
	case a2a.MessageStreamEnd:
		runtime.EventsEmit(c.app.ctx, "ve:stream_end", eventPayload)
		c.cachePushedVEDiscussionMessage(envelope)
	default:
		c.cachePushedVEDiscussionMessage(envelope)
		if strings.EqualFold(targetRole, "initiator") {
			runtime.EventsEmit(c.app.ctx, "ve:stream_chunk", eventPayload)
			runtime.EventsEmit(c.app.ctx, "ve:stream_end", eventPayload)
		} else if strings.EqualFold(targetRole, "executor") {
			// Route to local maclaw executor via GroupChatDispatcher
			if dispatcher := c.groupChatDispatcher(); dispatcher != nil && dispatcher.IsRegistered(sessionID) {
				dispatcher.HandleGroupMessage(sessionID, *envelope.Message)
			}
		} else if shouldDigitalEmployeeRespondToDiscussion(targetRole, envelope.Message.Kind) {
			if handler := c.digitalEmployeeMessageHandler(); handler != nil {
				handler.HandleGroupEnvelope(envelope)
			}
		}
	}
	runtime.EventsEmit(c.app.ctx, "ve-event", map[string]any{"type": msg.Type, "ts": msg.TS, "payload": envelope})
}

func (c *RemoteHubClient) cachePushedVEDiscussionMessage(envelope a2a.GroupEnvelope) {
	if c == nil || c.app == nil || envelope.Message == nil {
		return
	}
	sessionID := firstNonEmptyGroupString(envelope.SessionID, envelope.Message.SessionID)
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	go c.cachePushedVEDiscussionSnapshot(envelope)
}

func (c *RemoteHubClient) cachePushedVEDiscussionSnapshot(envelope a2a.GroupEnvelope) {
	if c == nil || c.app == nil || envelope.Message == nil {
		return
	}
	sessionID := firstNonEmptyGroupString(envelope.SessionID, envelope.Message.SessionID)
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	client, cfg, err := c.app.groupDiscussionClient()
	if err != nil {
		log.Printf("[hub-client] cachePushedVEDiscussionSnapshot: group discussion client unavailable: %v", err)
		return
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	detail, err := client.GetConsultationDetailForAgent(ctx, sessionID, groupDiscussionAgentID(cfg))
	if err != nil {
		log.Printf("[hub-client] cachePushedVEDiscussionSnapshot: refresh %s failed: %v", sessionID, err)
		return
	}
	if store, storeErr := c.app.openGroupDiscussionHistoryStore(); storeErr == nil {
		_ = store.CacheDetail(ctx, detail, c.app.groupDiscussionAttachmentRoot)
		_ = store.Close()
	}
}

func shouldDigitalEmployeeRespondToDiscussion(targetRole string, kind a2a.MessageKind) bool {
	role := strings.ToLower(strings.TrimSpace(targetRole))
	switch role {
	case "", "speak", "speaker", "review", "participant":
		return shouldHandleIncomingDigitalEmployeeMessage(kind)
	default:
		return false
	}
}

func shouldHandleIncomingDigitalEmployeeMessage(kind a2a.MessageKind) bool {
	switch kind {
	case "", a2a.MessageQuestion, a2a.MessageStatement:
		return true
	default:
		return false
	}
}

func decodeVEDiscussionPayload(payload json.RawMessage) (a2a.GroupEnvelope, string, error) {
	var wrapped struct {
		Envelope   *a2a.GroupEnvelope `json:"envelope"`
		TargetRole string             `json:"target_role"`
	}
	if len(payload) == 0 {
		return a2a.GroupEnvelope{}, "", nil
	}
	if err := json.Unmarshal(payload, &wrapped); err == nil && wrapped.Envelope != nil {
		return *wrapped.Envelope, strings.TrimSpace(wrapped.TargetRole), nil
	}
	var envelope a2a.GroupEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return a2a.GroupEnvelope{}, "", err
	}
	return envelope, "", nil
}
