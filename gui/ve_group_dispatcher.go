package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// GroupChatDispatcher routes group chat messages to the local maclaw agent
// when it's participating as an executor in a VE group discussion.
// It maintains a set of active sessions where local maclaw is an executor.
type GroupChatDispatcher struct {
	app *App
	mu  sync.RWMutex
	// sessions tracks which discussion sessions have local maclaw as executor
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
		d.cachedMachineID = strings.TrimSpace(cfg.RemoteMachineID)
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

// RegisterSession marks a session as having local maclaw as executor.
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

// IsRegistered returns true if the session has local maclaw as executor.
func (d *GroupChatDispatcher) IsRegistered(sessionID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, exists := d.sessions[sessionID]
	return exists
}

// HandleGroupMessage processes an incoming group discussion message for a session
// where local maclaw is the executor. It routes the message through the main
// IMMessageHandler with full tool access.
// localDispatch indicates the message was dispatched directly from tryLocalExecutorDispatch
// (bypassing Hub), so responses should be emitted directly to the frontend.
func (d *GroupChatDispatcher) HandleGroupMessage(sessionID string, msg a2a.GroupDiscussionMessage, localDispatch bool) {
	if d.app == nil {
		return
	}

	// Skip own messages (only relevant for Hub-routed messages)
	machineID := d.getLocalMachineID()
	if machineID != "" && msg.FromID == machineID {
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[group-dispatcher] panic in routeToMainAgent for session %s: %v", sess.SessionID, r)
		}
	}()

	hubClient := d.app.hubClient()
	if hubClient == nil {
		log.Printf("[group-dispatcher] hub client unavailable for session %s", sess.SessionID)
		return
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		log.Printf("[group-dispatcher] IM handler unavailable for session %s", sess.SessionID)
		return
	}

	// Build context with sender info for the system prompt
	senderContext := ""
	if msg.FromID != "" {
		senderContext = fmt.Sprintf("[来自群聊参与者 %s 的消息]\n", msg.FromID)
	}

	imMsg := IMUserMessage{
		UserID:   fmt.Sprintf("ve-group-executor:%s", sess.SessionID),
		Platform: "ve_group_executor",
		Text:     senderContext + msg.Content,
		Lang:     "zh",
	}

	// For locally-dispatched messages (from tryLocalExecutorDispatch), emit stream
	// events directly to frontend for zero-latency display, and async sync response
	// chunks to Hub for other participants via a single ordered goroutine.

	// hubSyncCh serializes Hub sync messages to avoid goroutine explosion and
	// preserve chunk ordering. Buffered to avoid blocking the onToken callback.
	var hubSyncCh chan a2a.GroupDiscussionMessage
	var hubSyncDone chan struct{}
	if localDispatch {
		hubSyncCh = make(chan a2a.GroupDiscussionMessage, 64)
		hubSyncDone = make(chan struct{})
		go func() {
			defer close(hubSyncDone)
			for m := range hubSyncCh {
				d.sendToGroup(sess.SessionID, m)
			}
		}()
	}

	// Stream response
	resp := handler.HandleIMMessageWithProgressAndStream(imMsg, nil, func(chunk string) {
		if strings.TrimSpace(chunk) == "" {
			return
		}
		if localDispatch {
			// Emit directly to frontend — zero network latency
			d.emitStreamToFrontend(sess.SessionID, chunk)
			// Queue for ordered Hub sync (single goroutine, preserves order)
			select {
			case hubSyncCh <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamChunk, Content: chunk}:
			default:
				// Channel full — drop chunk for Hub sync (frontend already has it)
				log.Printf("[group-dispatcher] hub sync channel full, dropping chunk for session %s", sess.SessionID)
			}
		} else {
			// Hub-routed message: send response through Hub (original behavior)
			d.sendToGroup(sess.SessionID, a2a.GroupDiscussionMessage{
				Kind:    a2a.MessageStreamChunk,
				Content: chunk,
			})
		}
	}, nil, nil)

	// Send stream end
	if resp != nil {
		if localDispatch {
			d.emitStreamToFrontend(sess.SessionID, "")
			// Queue stream_end and close channel to signal completion
			select {
			case hubSyncCh <- a2a.GroupDiscussionMessage{Kind: a2a.MessageStreamEnd, Content: ""}:
			default:
			}
			close(hubSyncCh)
			<-hubSyncDone // Wait for all Hub syncs to complete
		} else {
			d.sendToGroup(sess.SessionID, a2a.GroupDiscussionMessage{
				Kind:    a2a.MessageStreamEnd,
				Content: "",
			})
		}
	} else if localDispatch {
		// No response — still need to close the sync channel
		close(hubSyncCh)
		<-hubSyncDone
	}
}

// emitStreamToFrontend sends stream events directly to the frontend via Wails runtime,
// bypassing Hub network round-trip. Used for local executor dispatch.
func (d *GroupChatDispatcher) emitStreamToFrontend(sessionID, chunk string) {
	if d.app == nil || d.app.ctx == nil {
		return
	}
	if chunk == "" {
		// Stream end
		runtime.EventsEmit(d.app.ctx, "ve:stream_end", map[string]any{
			"session_id": sessionID,
			"content":    "",
			"chunk":      "",
		})
	} else {
		// Stream chunk
		runtime.EventsEmit(d.app.ctx, "ve:stream_chunk", map[string]any{
			"session_id": sessionID,
			"content":    chunk,
			"chunk":      chunk,
		})
	}
}

func (d *GroupChatDispatcher) sendToGroup(sessionID string, msg a2a.GroupDiscussionMessage) {
	if d.app == nil {
		return
	}
	if err := d.app.GroupDiscussionSendMessage(sessionID, msg); err != nil {
		log.Printf("[group-dispatcher] failed to send message to session %s: %v", sessionID, err)
	}
}

// shouldExecutorRespond determines if the executor should respond to this message.
// Only responds to questions and statements (not stream chunks, invitations, etc.)
func shouldExecutorRespond(msg a2a.GroupDiscussionMessage) bool {
	switch msg.Kind {
	case a2a.MessageQuestion, a2a.MessageStatement, "":
		return strings.TrimSpace(msg.Content) != ""
	default:
		return false
	}
}
