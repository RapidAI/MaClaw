package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	groupHubSyncChunkFlushInterval = 80 * time.Millisecond
	groupHubSyncChunkMaxBytes      = 2048
)

// GroupChatDispatcher routes group chat messages to the local AI agent
// when it is participating as a speaker in a VE group discussion.
// It maintains the active sessions where the local AI dispatcher is enabled.
type GroupChatDispatcher struct {
	app *App
	mu  sync.RWMutex
	// sessions tracks which discussion sessions have local AI enabled
	sessions map[string]*groupExecutorSession
	// cachedMachineID is lazily resolved and cached (immutable during runtime)
	cachedMachineID     string
	cachedMachineIDOnce sync.Once
}

func (d *GroupChatDispatcher) getLocalMachineID() string {
	d.cachedMachineIDOnce.Do(func() {
		if d.app == nil {
			return
		}
		cfg, err := d.app.LoadConfig()
		if err != nil {
			return
		}
		d.cachedMachineID = strings.TrimSpace(groupDiscussionAgentID(cfg))
	})
	return d.cachedMachineID
}

type groupExecutorSession struct {
	SessionID string
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewGroupChatDispatcher creates a new dispatcher.
func NewGroupChatDispatcher(app *App) *GroupChatDispatcher {
	return &GroupChatDispatcher{
		app:      app,
		sessions: make(map[string]*groupExecutorSession),
	}
}

// RegisterSession marks a session as having local AI enabled.
// After this call, incoming messages for this session will be routed to the main agent.
func (d *GroupChatDispatcher) RegisterSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.sessions[sessionID]; exists {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.sessions[sessionID] = &groupExecutorSession{
		SessionID: sessionID,
		ctx:       ctx,
		cancel:    cancel,
	}
	log.Printf("[group-dispatcher] registered session %s for local executor", sessionID)
}

// UnregisterSession removes a session from the dispatcher.
func (d *GroupChatDispatcher) UnregisterSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if sess, exists := d.sessions[sessionID]; exists {
		sess.cancel()
		delete(d.sessions, sessionID)
		log.Printf("[group-dispatcher] unregistered session %s", sessionID)
	}
}

// IsRegistered returns true if the session has local AI enabled.
func (d *GroupChatDispatcher) IsRegistered(sessionID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, exists := d.sessions[sessionID]
	return exists
}

// HandleGroupMessage processes an incoming group discussion message for a session
// where local AI is enabled. It routes the message through the main
// IMMessageHandler with full tool access.
// localDispatch indicates the message was dispatched directly from tryLocalExecutorDispatch
// (bypassing Hub), so responses should be emitted directly to the frontend.
func (d *GroupChatDispatcher) HandleGroupMessage(sessionID string, msg a2a.GroupDiscussionMessage, localDispatch bool) {
	if d.app == nil {
		return
	}

	// Skip own messages (only relevant for Hub-routed messages)
	machineID := d.getLocalMachineID()
	if machineID != "" && veGroupParticipantIdentityMatches(msg.FromID, machineID) {
		return
	}

	// Only respond to substantive messages (questions, statements)
	if !shouldExecutorRespond(msg) {
		return
	}

	d.mu.RLock()
	sess, exists := d.sessions[sessionID]
	d.mu.RUnlock()
	if !exists {
		return
	}

	// Check if context is cancelled
	if sess.ctx.Err() != nil {
		return
	}

	// Route to main agent in a goroutine (non-blocking)
	go d.routeToMainAgent(sess, msg, localDispatch)
}

func (d *GroupChatDispatcher) routeToMainAgent(sess *groupExecutorSession, msg a2a.GroupDiscussionMessage, localDispatch bool) {
	var hubSyncCh chan a2a.GroupDiscussionMessage
	var hubSyncDone chan struct{}
	finishHubSync := func() {}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[group-dispatcher] panic in routeToMainAgent for session %s: %v", sess.SessionID, r)
			finishHubSync()
		}
	}()
	if localDispatch {
		hubSyncCh = make(chan a2a.GroupDiscussionMessage, 64)
		hubSyncDone = make(chan struct{})
		go func() {
			defer close(hubSyncDone)
			d.sendQueuedGroupMessages(sess.SessionID, hubSyncCh)
		}()
		var closeOnce sync.Once
		finishHubSync = func() {
			closeOnce.Do(func() {
				close(hubSyncCh)
				<-hubSyncDone
			})
		}
		if targetedInput, ok := d.app.localTargetedGroupMessage(msg); ok {
			hubSyncCh <- targetedInput
		}
	}

	hubClient := d.app.hubClient()
	if hubClient == nil {
		log.Printf("[group-dispatcher] hub client unavailable for session %s", sess.SessionID)
		finishHubSync()
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		log.Printf("[group-dispatcher] IM handler unavailable for session %s", sess.SessionID)
		finishHubSync()
		return
	}

	// Build context with sender info for the system prompt.
	senderContext := groupExecutorSenderContext(msg.FromID)

	content := msg.Content
	if HasAttachments(msg) {
		content += NewVEMessageHandler(d.app).ProcessMessageAttachmentsForSession(sess.SessionID, msg)
	}

	imMsg := IMUserMessage{
		UserID:   fmt.Sprintf("ve-group-executor:%s", sess.SessionID),
		Platform: "ve_group_executor",
		Text:     senderContext + content,
		Lang:     "zh",
	}

	// Group-executor LLM rounds may produce assistant content before choosing
	// tool calls. That content is part of the agent loop, not a user-visible VE
	// answer. Publish only the final IM response after the handler has resolved
	// all tool calls.

	// hubSyncCh serializes Hub sync messages to avoid goroutine explosion and
	// preserve final response ordering.

	resp := handler.HandleIMMessageWithProgressAndStream(imMsg, nil, nil, nil, nil)
	if resp != nil {
		chunk := strings.TrimSpace(resp.Text)
		if chunk != "" {
			if localDispatch {
				// Emit directly to frontend after tool resolution.
				d.emitStreamToFrontend(sess.SessionID, chunk)
				// Queue for ordered Hub sync (single goroutine, preserves order)
				select {
				case hubSyncCh <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamChunk, Content: chunk}:
				default:
					// Channel full - drop chunk for Hub sync (frontend already has it).
					log.Printf("[group-dispatcher] hub sync channel full, dropping chunk for session %s", sess.SessionID)
				}
			} else {
				// Hub-routed message: send final response through Hub.
				d.sendToGroup(sess.SessionID, a2a.GroupDiscussionMessage{
					Kind:    a2a.MessageStreamChunk,
					Content: chunk,
				})
			}
		}
	}

	// Send stream end
	if resp != nil {
		d.forwardAgentResponseFiles(sess.SessionID, resp, localDispatch)
		if localDispatch {
			d.emitStreamToFrontend(sess.SessionID, "")
			// Always preserve stream_end so Hub history can collapse streamed chunks reliably.
			hubSyncCh <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamEnd, Content: ""}
			finishHubSync() // Wait for all Hub syncs to complete
		} else {
			d.sendToGroup(sess.SessionID, a2a.GroupDiscussionMessage{
				Kind:    a2a.MessageStreamEnd,
				Content: "",
			})
		}
	} else if localDispatch {
		// No response - still need to close the sync channel.
		finishHubSync()
	}
}

func (d *GroupChatDispatcher) forwardAgentResponseFiles(sessionID string, resp *IMAgentResponse, localDispatch bool) {
	if d == nil || d.app == nil || resp == nil {
		return
	}
	paths := append([]string{}, resp.LocalFilePaths...)
	if strings.TrimSpace(resp.LocalFilePath) != "" {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for _, path := range uniqueVEFilePaths(paths) {
		if localDispatch {
			localMsg, _, localErr := buildLocalVEFileAttachmentMessage(path, "", "")
			if localErr == nil {
				d.emitAttachmentToFrontend(sessionID, localMsg)
			} else {
				log.Printf("[group-dispatcher] failed to prepare local response file %s for session %s: %v", path, sessionID, localErr)
			}
			go d.syncAgentResponseFileToHub(sessionID, path)
			continue
		}
		d.syncAgentResponseFileToHub(sessionID, path)
	}
}

func uniqueVEFilePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	unique := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, path)
	}
	return unique
}

func (d *GroupChatDispatcher) syncAgentResponseFileToHub(sessionID, path string) {
	if d == nil || d.app == nil {
		return
	}
	msg, err := d.app.buildVEFileAttachmentMessage(sessionID, path, "", "")
	if err != nil {
		log.Printf("[group-dispatcher] failed to prepare response file %s for session %s: %v", path, sessionID, err)
		return
	}
	d.sendToGroup(sessionID, msg)
}

func (d *GroupChatDispatcher) emitAttachmentToFrontend(sessionID string, msg a2a.GroupDiscussionMessage) {
	if d == nil || d.app == nil || d.app.ctx == nil {
		return
	}
	senderID := d.getLocalMachineID()
	if senderID == "" {
		senderID = "local-maclaw"
	}
	payload := map[string]any{
		"session_id":  sessionID,
		"content":     msg.Content,
		"chunk":       msg.Content,
		"sender_name": "本机AI",
		"sender_id":   senderID,
		"from_id":     senderID,
		"attachments": groupDiscussionMessageAttachmentsPayload(msg),
	}
	runtime.EventsEmit(d.app.ctx, "ve:stream_chunk", payload)
}

// emitStreamToFrontend sends stream events directly to the frontend via Wails runtime,
// bypassing Hub network round-trip. Used for local executor dispatch.
func (d *GroupChatDispatcher) emitStreamToFrontend(sessionID, chunk string) {
	if d.app == nil || d.app.ctx == nil {
		return
	}
	senderID := d.getLocalMachineID()
	if senderID == "" {
		senderID = "local-maclaw"
	}
	if chunk == "" {
		// Stream end
		runtime.EventsEmit(d.app.ctx, "ve:stream_end", map[string]any{
			"session_id":  sessionID,
			"content":     "",
			"chunk":       "",
			"sender_name": "本机AI",
			"sender_id":   senderID,
		})
	} else {
		// Stream chunk
		runtime.EventsEmit(d.app.ctx, "ve:stream_chunk", map[string]any{
			"session_id":  sessionID,
			"content":     chunk,
			"chunk":       chunk,
			"sender_name": "本机AI",
			"sender_id":   senderID,
		})
	}
}

func (d *GroupChatDispatcher) sendQueuedGroupMessages(sessionID string, messages <-chan a2a.GroupDiscussionMessage) {
	if d.app == nil {
		for range messages {
		}
		return
	}
	client, cfg, err := d.app.veA2AHubClient()
	if err != nil {
		log.Printf("[group-dispatcher] group discussion client unavailable for session %s: %v", sessionID, err)
		for range messages {
		}
		return
	}
	fromID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	send := func(msg a2a.GroupDiscussionMessage) {
		if strings.TrimSpace(msg.FromID) == "" {
			msg.FromID = fromID
		}
		ctx, cancel := groupDiscussionContext()
		err := client.SendDiscussionMessage(ctx, sessionID, msg)
		cancel()
		if err != nil {
			log.Printf("[group-dispatcher] failed to sync queued message to session %s: %v", sessionID, err)
		}
	}

	var pendingChunk a2a.GroupDiscussionMessage
	var pendingContent strings.Builder
	flushPendingChunk := func() {
		if pendingContent.Len() == 0 {
			return
		}
		msg := pendingChunk
		msg.Kind = a2a.MessageStreamChunk
		msg.Content = pendingContent.String()
		send(msg)
		pendingChunk = a2a.GroupDiscussionMessage{}
		pendingContent.Reset()
	}
	timer := time.NewTimer(groupHubSyncChunkFlushInterval)
	if !timer.Stop() {
		<-timer.C
	}
	timerActive := false
	stopTimer := func() {
		if !timerActive {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timerActive = false
	}
	resetTimer := func() {
		if !timer.Stop() && timerActive {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(groupHubSyncChunkFlushInterval)
		timerActive = true
	}
	defer stopTimer()

	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				stopTimer()
				flushPendingChunk()
				return
			}
			if msg.Kind == a2a.MessageStreamChunk {
				if pendingContent.Len() == 0 {
					pendingChunk = msg
					resetTimer()
				}
				pendingContent.WriteString(msg.Content)
				if pendingContent.Len() >= groupHubSyncChunkMaxBytes {
					stopTimer()
					flushPendingChunk()
				}
				continue
			}
			stopTimer()
			flushPendingChunk()
			send(msg)
		case <-timer.C:
			timerActive = false
			flushPendingChunk()
		}
	}
}

func (d *GroupChatDispatcher) sendToGroup(sessionID string, msg a2a.GroupDiscussionMessage) {
	if d.app == nil {
		return
	}
	if err := d.app.sendVEA2AMessage(sessionID, msg); err != nil {
		log.Printf("[group-dispatcher] failed to send message to session %s: %v", sessionID, err)
	}
}

func groupExecutorSenderContext(fromID string) string {
	fromID = strings.TrimSpace(fromID)
	if fromID == "" {
		return ""
	}
	return fmt.Sprintf("[from group participant %s; reply only with group-visible content, no hidden reasoning or meta notes]\n", fromID)
}

// shouldExecutorRespond determines if the executor should respond to this message.
// Only responds to questions and statements (not stream chunks, invitations, etc.)
func shouldExecutorRespond(msg a2a.GroupDiscussionMessage) bool {
	switch msg.Kind {
	case a2a.MessageQuestion, a2a.MessageStatement, "":
		return strings.TrimSpace(msg.Content) != "" || HasAttachments(msg)
	default:
		return false
	}
}
