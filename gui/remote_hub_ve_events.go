package main

import (
	"encoding/json"
	"log"
	"strings"
	"time"

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
	veEventDiscussionInvite  = "ve:discussion_invite"
	veEventDiscussionMessage = "ve:discussion_message"
	veEventDiscussionRename  = "ve:discussion_rename"
)

// isVEEvent returns true if the message type is a VE-related event.
func isVEEvent(msgType string) bool {
	return strings.HasPrefix(msgType, "ve:")
}

// handleVEEvent processes VE-related WebSocket events from the Hub
// and forwards them to the frontend via Wails EventsEmit.
// This is called from the readLoop when a VE event is received.
func (c *RemoteHubClient) handleVEEvent(msg inboundHubEnvelope) {
	if c == nil || c.app == nil {
		return
	}
	if strings.TrimSpace(msg.Type) == veEventDiscussionInvite {
		c.handleVEDiscussionInvite(msg)
	}
	if strings.TrimSpace(msg.Type) == veEventDiscussionRename {
		c.cachePushedVEDiscussionRename(decodeVEEventPayloadMap(msg))
	}
	if shouldClearDiscoverableVECacheForEvent(msg.Type) {
		c.app.clearDiscoverableVECache()
	}
	if c.app.ctx == nil {
		return
	}
	if strings.TrimSpace(msg.Type) == veEventDiscussionMessage {
		c.handleVEDiscussionMessage(msg)
		return
	}

	payload := decodeVEEventPayloadMap(msg)

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

func shouldClearDiscoverableVECacheForEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case veEventListUpdate, veEventStatusChange, veEventApproved, veEventRejected, veEventDisabled:
		return true
	default:
		return false
	}
}

func decodeVEEventPayloadMap(msg inboundHubEnvelope) map[string]any {
	if len(msg.Payload) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("[hub-client] handleVEEvent: failed to parse payload for %s: %v", msg.Type, err)
		return map[string]any{}
	}
	return payload
}

func (c *RemoteHubClient) cachePushedVEDiscussionRename(payload map[string]any) {
	if c == nil || c.app == nil || len(payload) == 0 {
		return
	}
	payload = discussionRenamePayloadMap(payload)
	field := func(keys ...string) string {
		for _, key := range keys {
			if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
		return ""
	}
	discussionID := field("discussion_id", "discussionId", "session_id", "sessionId")
	topic := field("topic", "title")
	if discussionID == "" || topic == "" {
		return
	}
	if store, err := c.app.openGroupDiscussionHistoryStore(); err == nil {
		ctx, cancel := groupDiscussionContext()
		_ = store.RenameCachedDiscussion(ctx, discussionID, topic)
		cancel()
		_ = store.Close()
	}
}

func discussionRenamePayloadMap(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return payload
	}
	for _, key := range []string{"payload", "Payload"} {
		if nested, ok := payload[key].(map[string]any); ok && len(nested) > 0 {
			return discussionRenamePayloadMap(nested)
		}
	}
	return payload
}

func (c *RemoteHubClient) handleVEDiscussionInvite(msg inboundHubEnvelope) {
	var payload struct {
		Envelope *a2a.GroupEnvelope `json:"envelope"`
		InviteID string             `json:"invite_id"`
	}
	if len(msg.Payload) == 0 {
		return
	}
	if err := json.Unmarshal(msg.Payload, &payload); err != nil || payload.Envelope == nil || payload.Envelope.Invitation == nil {
		return
	}
	inviteID := strings.TrimSpace(payload.InviteID)
	if inviteID == "" {
		return
	}
	client, cfg, err := c.app.veA2AHubClient()
	if err != nil {
		log.Printf("[hub-client] handleVEDiscussionInvite: VE A2A client unavailable: %v", err)
		return
	}
	localID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	if localID == "" || !isVEGroupDefaultResponderMatch(payload.Envelope.Invitation.ToID, localID) {
		return
	}
	if !payload.Envelope.Invitation.Trusted {
		log.Printf("[hub-client] handleVEDiscussionInvite: ignored untrusted invite %s", inviteID)
		return
	}
	if !groupDiscussionRoleAllowed(cfg, payload.Envelope.Invitation.Role) {
		log.Printf("[hub-client] handleVEDiscussionInvite: role %q not allowed", payload.Envelope.Invitation.Role)
		return
	}
	if cfg.GroupDiscussion.RejectWhenDND && strings.EqualFold(strings.TrimSpace(cfg.GroupDiscussion.Availability), "dnd") {
		log.Printf("[hub-client] handleVEDiscussionInvite: ignored invite %s while DND", inviteID)
		return
	}
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	if err := client.AcceptInvite(ctx, inviteID, a2a.GroupInvitationResponse{FromID: localID, Reason: "digital employee auto accepted"}); err != nil {
		log.Printf("[hub-client] handleVEDiscussionInvite: accept %s failed: %v", inviteID, err)
		return
	}
	log.Printf("[hub-client] handleVEDiscussionInvite: accepted %s for %s", inviteID, localID)
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
		if localMachineID != "" && veGroupParticipantIdentityMatches(envelope.Message.FromID, localMachineID) {
			isOwnMessage = true
		}
	}
	// Also check VE 1:1 handler sessions — responses are streamed locally and
	// then synced to Hub; the Hub echo must not duplicate frontend display.
	if !isOwnMessage {
		if veHandler := c.digitalEmployeeMessageHandler(); veHandler != nil && veHandler.IsActiveSession(sessionID) {
			localMachineID := veHandler.getLocalAgentID()
			if localMachineID != "" && veGroupParticipantIdentityMatches(envelope.Message.FromID, localMachineID) {
				isOwnMessage = true
			}
		}
	}

	switch envelope.Message.Kind {
	case a2a.MessageStreamChunk:
		if !isOwnMessage && (content != "" || HasAttachments(*envelope.Message)) {
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
		localMachineID := ""
		if dispatcher != nil {
			localMachineID = dispatcher.getLocalMachineID()
		}
		if localDispatcherRegistered && shouldRouteVEDiscussionToLocalDispatcher(targetRole, *envelope.Message, localMachineID) {
			dispatcher.HandleGroupMessage(sessionID, *envelope.Message, false)
		} else if shouldRouteVEDiscussionToDigitalEmployee(targetRole, *envelope.Message, localDispatcherRegistered, localMachineID) {
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

func shouldRouteVEDiscussionToLocalDispatcher(targetRole string, msg a2a.GroupDiscussionMessage, localMachineID string) bool {
	if !shouldExecutorRespond(msg) {
		return false
	}
	if len(msg.ToIDs) > 0 {
		return groupDiscussionMessageTargetsLocal(msg, localMachineID)
	}
	if strings.EqualFold(strings.TrimSpace(targetRole), "executor") {
		return true
	}
	return false
}

func shouldRouteVEDiscussionToDigitalEmployee(targetRole string, msg a2a.GroupDiscussionMessage, localDispatcherRegistered bool, localMachineID string) bool {
	if len(msg.ToIDs) > 0 && !groupDiscussionMessageTargetsLocal(msg, localMachineID) {
		return false
	}
	if localDispatcherRegistered {
		if len(msg.ToIDs) == 0 {
			return false
		}
		if shouldRouteVEDiscussionToLocalDispatcher(targetRole, msg, localMachineID) {
			return false
		}
	}
	return shouldDigitalEmployeeRespondToDiscussion(targetRole, msg.Kind)
}

func groupDiscussionMessageTargetsLocal(msg a2a.GroupDiscussionMessage, localMachineID string) bool {
	localMachineID = strings.TrimSpace(localMachineID)
	if localMachineID == "" {
		return false
	}
	for _, toID := range msg.ToIDs {
		if isVEGroupDefaultResponderMatch(strings.TrimSpace(toID), localMachineID) {
			return true
		}
	}
	return false
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
		sleepVEDetailRefreshDebounce()
		for {
			state.mu.Lock()
			state.dirty = false
			state.mu.Unlock()
			refreshed := c.cachePushedVEDiscussionSnapshot(envelope)
			state.mu.Lock()
			if !refreshed {
				state.saturated++
				if state.saturated <= veDetailRefreshMaxSaturated {
					state.dirty = true
				}
			} else {
				state.saturated = 0
			}
			if !state.dirty {
				state.mu.Unlock()
				return
			}
			state.dirty = false
			saturated := state.saturated
			state.mu.Unlock()
			sleepVEDetailRefreshRetryDelay(saturated)
		}
	}()
}

func (c *RemoteHubClient) cachePushedVEDiscussionSnapshot(envelope a2a.GroupEnvelope) bool {
	if c == nil || c.app == nil || envelope.Message == nil {
		return true
	}
	sessionID := firstNonEmptyGroupString(envelope.SessionID, envelope.Message.SessionID)
	if strings.TrimSpace(sessionID) == "" {
		return true
	}
	client, cfg, err := c.app.veA2AHubClient()
	if err != nil {
		log.Printf("[hub-client] cachePushedVEDiscussionSnapshot: VE A2A client unavailable: %v", err)
		return true
	}
	release, ok := acquireVEDetailRefreshSlot(2 * time.Second)
	if !ok {
		log.Printf("[hub-client] cachePushedVEDiscussionSnapshot: refresh %s skipped because refresh queue is saturated", sessionID)
		return false
	}
	defer release()
	ctx, cancel := groupDiscussionContext()
	defer cancel()
	detail, err := client.GetConsultationDetailForAgent(ctx, sessionID, groupDiscussionAgentID(cfg))
	if err != nil {
		log.Printf("[hub-client] cachePushedVEDiscussionSnapshot: refresh %s failed: %v", sessionID, err)
		return true
	}
	if store, storeErr := c.app.openGroupDiscussionHistoryStore(); storeErr == nil {
		_ = store.CacheDetail(ctx, detail, c.app.groupDiscussionAttachmentRoot)
		_ = store.Close()
	}
	return true
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
