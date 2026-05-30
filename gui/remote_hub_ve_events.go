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
	if HasAttachments(*envelope.Message) {
		c.localizeVEDiscussionAttachmentPaths(sessionID, envelope.Message)
	}
	eventPayload := buildVEDiscussionStreamEventPayload(envelope, sessionID, content)

	// Dedup: if this session has a local executor that emits stream events directly
	// to the frontend, skip re-emitting stream chunks/ends that originated from
	// our own local agent (already displayed via emitStreamToFrontend).
	// Messages from OTHER participants must still be emitted to the frontend.
	isOwnMessage := false
	if dispatcher := c.groupChatDispatcher(); dispatcher != nil && dispatcher.IsRegistered(sessionID) {
		// Check if this message originated from our own local agent
		localMachineID := dispatcher.getLocalMachineID()
		if localMachineID != "" && strings.EqualFold(strings.TrimSpace(envelope.Message.FromID), localMachineID) {
			isOwnMessage = true
		}
	}

	switch envelope.Message.Kind {
	case a2a.MessageStreamChunk:
		if !isOwnMessage {
			runtime.EventsEmit(c.app.ctx, "ve:stream_chunk", eventPayload)
		}
	case a2a.MessageStreamEnd:
		if !isOwnMessage {
			runtime.EventsEmit(c.app.ctx, "ve:stream_end", eventPayload)
		}
		c.cachePushedVEDiscussionMessage(envelope)
	default:
		c.cachePushedVEDiscussionMessage(envelope)
		if shouldEmitVEDiscussionMessageToFrontend(targetRole, *envelope.Message) {
			// For initiator-targeted messages: skip if it's our own message echoed back
			// (user already sees it via optimistic update in frontend)
			if !isOwnMessage {
				runtime.EventsEmit(c.app.ctx, "ve:stream_chunk", eventPayload)
				runtime.EventsEmit(c.app.ctx, "ve:stream_end", eventPayload)
			}
		}
		dispatcher := c.groupChatDispatcher()
		localDispatcherRegistered := dispatcher != nil && dispatcher.IsRegistered(sessionID)
		if localDispatcherRegistered && shouldRouteVEDiscussionToLocalDispatcher(targetRole, *envelope.Message) {
			dispatcher.HandleGroupMessage(sessionID, *envelope.Message, false)
		} else if shouldRouteVEDiscussionToDigitalEmployee(targetRole, *envelope.Message, localDispatcherRegistered) {
			if handler := c.digitalEmployeeMessageHandler(); handler != nil {
				handler.HandleGroupEnvelope(envelope)
			}
		}
	}
	runtime.EventsEmit(c.app.ctx, "ve-event", map[string]any{"type": msg.Type, "ts": msg.TS, "payload": envelope})
}

func shouldEmitVEDiscussionMessageToFrontend(targetRole string, msg a2a.GroupDiscussionMessage) bool {
	role := strings.ToLower(strings.TrimSpace(targetRole))
	if role == "initiator" {
		return true
	}
	return HasAttachments(msg) && role != "executor"
}

func (c *RemoteHubClient) localizeVEDiscussionAttachmentPaths(sessionID string, msg *a2a.GroupDiscussionMessage) {
	if c == nil || c.app == nil || msg == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	attempted := 0
	for i := range msg.TextAttachments {
		msg.TextAttachments[i].LocalPath = ""
		if attempted < veAttachmentContextMaxCount {
			attempted++
		}
	}
	for i := range msg.ImageAttachments {
		att := &msg.ImageAttachments[i]
		att.LocalPath = ""
		if attempted >= veAttachmentContextMaxCount {
			continue
		}
		attempted++
		if strings.TrimSpace(att.FileURL) == "" {
			continue
		}
		result, err := c.app.GroupDiscussionDownloadAttachment(sessionID, att.FileURL, att.Filename)
		if err != nil {
			log.Printf("[hub-client] download VE image attachment %s failed: %v", att.Filename, err)
			continue
		}
		att.LocalPath = result.LocalPath
	}
	for i := range msg.FileAttachments {
		att := &msg.FileAttachments[i]
		att.LocalPath = ""
		if attempted >= veAttachmentContextMaxCount {
			continue
		}
		attempted++
		if strings.TrimSpace(att.FileURL) == "" {
			continue
		}
		result, err := c.app.GroupDiscussionDownloadAttachment(sessionID, att.FileURL, att.Filename)
		if err != nil {
			log.Printf("[hub-client] download VE file attachment %s failed: %v", att.Filename, err)
			continue
		}
		att.LocalPath = result.LocalPath
		if att.SizeBytes <= 0 {
			att.SizeBytes = result.SizeBytes
		}
	}
}

func shouldRouteVEDiscussionToLocalDispatcher(targetRole string, msg a2a.GroupDiscussionMessage) bool {
	if !shouldExecutorRespond(msg) {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(targetRole), "executor") {
		return true
	}
	return shouldDigitalEmployeeRespondToDiscussion(targetRole, msg.Kind)
}

func shouldRouteVEDiscussionToDigitalEmployee(targetRole string, msg a2a.GroupDiscussionMessage, localDispatcherRegistered bool) bool {
	if localDispatcherRegistered && shouldRouteVEDiscussionToLocalDispatcher(targetRole, msg) {
		return false
	}
	return shouldDigitalEmployeeRespondToDiscussion(targetRole, msg.Kind)
}

func buildVEDiscussionStreamEventPayload(envelope a2a.GroupEnvelope, sessionID string, content string) map[string]any {
	payload := map[string]any{"session_id": sessionID, "content": content, "chunk": content}
	if envelope.Message == nil {
		return payload
	}
	if attachments := groupDiscussionMessageAttachmentsPayload(*envelope.Message); len(attachments) > 0 {
		payload["attachments"] = attachments
	}
	fromID := strings.TrimSpace(firstNonEmptyGroupString(envelope.Message.FromID, envelope.FromID))
	if fromID != "" {
		payload["from_id"] = fromID
		payload["sender_id"] = fromID
	}
	return payload
}

func groupDiscussionMessageAttachmentsPayload(msg a2a.GroupDiscussionMessage) []map[string]any {
	attachments := make([]map[string]any, 0, len(msg.TextAttachments)+len(msg.ImageAttachments)+len(msg.FileAttachments))
	for _, att := range msg.TextAttachments {
		item := map[string]any{"type": "text", "filename": att.Filename, "mime_type": att.MimeType, "local_path": att.LocalPath}
		attachments = append(attachments, item)
	}
	for _, att := range msg.ImageAttachments {
		item := map[string]any{"type": "image", "filename": att.Filename, "mime_type": att.MimeType, "file_url": att.FileURL, "local_path": att.LocalPath}
		attachments = append(attachments, item)
	}
	for _, att := range msg.FileAttachments {
		item := map[string]any{"type": "file", "filename": att.Filename, "mime_type": att.MimeType, "file_url": att.FileURL, "local_path": att.LocalPath, "size_bytes": att.SizeBytes}
		attachments = append(attachments, item)
	}
	return attachments
}

func (c *RemoteHubClient) cachePushedVEDiscussionMessage(envelope a2a.GroupEnvelope) {
	if c == nil || c.app == nil || envelope.Message == nil {
		return
	}
	sessionID := firstNonEmptyGroupString(envelope.SessionID, envelope.Message.SessionID)
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	state := &veDetailRefreshState{}
	if existing, loaded := c.veDetailRefresh.LoadOrStore(sessionID, state); loaded {
		if refreshState, ok := existing.(*veDetailRefreshState); ok && refreshState != nil {
			refreshState.mu.Lock()
			refreshState.dirty = true
			refreshState.mu.Unlock()
		}
		return
	}
	go func() {
		defer c.veDetailRefresh.Delete(sessionID)
		for {
			c.cachePushedVEDiscussionSnapshot(envelope)
			state.mu.Lock()
			if !state.dirty {
				state.mu.Unlock()
				return
			}
			state.dirty = false
			state.mu.Unlock()
		}
	}()
}

func (c *RemoteHubClient) cachePushedVEDiscussionSnapshot(envelope a2a.GroupEnvelope) {
	if c == nil || c.app == nil || envelope.Message == nil {
		return
	}
	sessionID := firstNonEmptyGroupString(envelope.SessionID, envelope.Message.SessionID)
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	client, cfg, err := c.app.veA2AHubClient()
	if err != nil {
		log.Printf("[hub-client] cachePushedVEDiscussionSnapshot: VE A2A client unavailable: %v", err)
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
