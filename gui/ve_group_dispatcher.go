package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
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
func (d *GroupChatDispatcher) HandleGroupMessage(sessionID string, msg a2a.GroupDiscussionMessage) {
	if d.app == nil {
		return
	}

	// Skip own messages
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
	go d.routeToMainAgent(sess, msg)
}

func (d *GroupChatDispatcher) routeToMainAgent(sess *groupExecutorSession, msg a2a.GroupDiscussionMessage) {
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

	// Stream response back to group
	resp := handler.HandleIMMessageWithProgressAndStream(imMsg, nil, func(chunk string) {
		if strings.TrimSpace(chunk) == "" {
			return
		}
		d.sendToGroup(sess.SessionID, a2a.GroupDiscussionMessage{
			Kind:    a2a.MessageStreamChunk,
			Content: chunk,
		})
	}, nil, nil)

	// Send stream end
	if resp != nil {
		d.sendToGroup(sess.SessionID, a2a.GroupDiscussionMessage{
			Kind:    a2a.MessageStreamEnd,
			Content: "",
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
