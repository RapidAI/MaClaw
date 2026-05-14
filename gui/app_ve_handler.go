package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/a2a"
)

// VEMessageHandler processes incoming A2A messages when this maclaw instance
// is acting as a virtual employee. It receives GroupEnvelope messages (Type=discussion_message)
// from the Hub, extracts content, processes them through the local AI agent (reusing
// the IMMessageHandler agent loop pattern), and sends streaming responses back.
type VEMessageHandler struct {
	app            *App
	mu             sync.Mutex
	activeSessions map[string]*veSession // key: consultation/session ID
}

// veSession tracks an active VE conversation session.
type veSession struct {
	SessionID    string
	RequesterID  string
	LastActivity time.Time
	Cancel       context.CancelFunc
}

// NewVEMessageHandler creates a new VE message handler.
func NewVEMessageHandler(app *App) *VEMessageHandler {
	return &VEMessageHandler{
		app:            app,
		activeSessions: make(map[string]*veSession),
	}
}

// HandleGroupEnvelope processes an incoming GroupEnvelope when this maclaw instance
// is acting as a virtual employee. It validates the envelope type is discussion_message,
// extracts the content from the embedded GroupDiscussionMessage, and invokes the local
// AI agent (reusing the IMMessageHandler agent loop pattern).
func (h *VEMessageHandler) HandleGroupEnvelope(envelope a2a.GroupEnvelope) {
	if envelope.Type != a2a.GroupMessageDiscussionMessage {
		return
	}
	if envelope.Message == nil || strings.TrimSpace(envelope.Message.Content) == "" {
		return
	}

	sessionID := envelope.SessionID
	if sessionID == "" {
		sessionID = envelope.Message.SessionID
	}
	if sessionID == "" {
		log.Printf("[ve-handler] received envelope without session ID, ignoring")
		return
	}

	h.HandleIncomingMessage(sessionID, *envelope.Message)
}

// HandleIncomingMessage processes an incoming A2A discussion message
// when this maclaw instance is acting as a virtual employee.
// It runs the local AI agent and sends streaming responses back via Hub.
func (h *VEMessageHandler) HandleIncomingMessage(sessionID string, msg a2a.GroupDiscussionMessage) {
	if strings.TrimSpace(msg.Content) == "" {
		return
	}

	h.mu.Lock()
	session, ok := h.activeSessions[sessionID]
	if !ok {
		ctx, cancel := context.WithCancel(context.Background())
		session = &veSession{
			SessionID:    sessionID,
			RequesterID:  msg.FromID,
			LastActivity: time.Now(),
			Cancel:       cancel,
		}
		h.activeSessions[sessionID] = session
		_ = ctx // used by cancel
	}
	session.LastActivity = time.Now()
	h.mu.Unlock()

	// Process in background goroutine to not block the WebSocket reader
	go h.processAndRespond(sessionID, msg)
}

// processAndRespond runs the AI agent on the incoming message and streams the response back.
// It implements the 60s first-response timeout: if no chunk is produced within 60s,
// a timeout error message is sent back.
func (h *VEMessageHandler) processAndRespond(sessionID string, msg a2a.GroupDiscussionMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	userMessage := msg.Content

	// Channel to signal that the first chunk has been sent
	firstChunkSent := make(chan struct{}, 1)
	// Channel to collect the final result or error
	type result struct {
		err error
	}
	resultCh := make(chan result, 1)

	// Start the AI agent processing with streaming
	go func() {
		err := h.runAgentWithStreaming(ctx, sessionID, userMessage, firstChunkSent)
		resultCh <- result{err: err}
	}()

	// 60s first-response timeout
	firstResponseTimer := time.NewTimer(60 * time.Second)
	defer firstResponseTimer.Stop()

	select {
	case <-firstChunkSent:
		// First chunk was sent within 60s, wait for completion
		r := <-resultCh
		if r.err != nil {
			log.Printf("[ve-handler] error generating response for session %s: %v", sessionID, r.err)
		}
	case r := <-resultCh:
		// Agent finished (possibly with error) before timeout
		if r.err != nil {
			log.Printf("[ve-handler] error generating response for session %s: %v", sessionID, r.err)
			h.sendMessage(sessionID, a2a.GroupDiscussionMessage{
				Kind:    a2a.MessageStatement,
				Content: fmt.Sprintf("[閿欒] 澶勭悊娑堟伅鏃跺嚭閿? %v", r.err),
			})
		}
	case <-firstResponseTimer.C:
		// 60s timeout: no chunk produced
		cancel() // cancel the agent processing
		h.sendMessage(sessionID, a2a.GroupDiscussionMessage{
			Kind:    a2a.MessageStatement,
			Content: "[瓒呮椂] VE 澶勭悊娑堟伅瓒呮椂锛?0绉掑唴鏃犲搷搴旓級锛岃绋嶅悗閲嶈瘯",
		})
		// Drain the result channel
		<-resultCh
	case <-ctx.Done():
		// Overall context cancelled
		<-resultCh
	}
}

// runAgentWithStreaming runs the AI agent and streams chunks back via Hub.
// Each generated chunk is sent as a GroupDiscussionMessage with kind=stream_chunk.
// When generation is complete, a kind=stream_end message is sent.
// The firstChunkSent channel is closed after the first chunk is sent.
func (h *VEMessageHandler) runAgentWithStreaming(ctx context.Context, sessionID, userMessage string, firstChunkSent chan<- struct{}) error {
	firstSent := false

	// onToken callback: called for each generated token/chunk
	onToken := func(chunk string) {
		if ctx.Err() != nil {
			return
		}
		if strings.TrimSpace(chunk) == "" {
			return
		}
		h.SendStreamChunk(sessionID, chunk)
		if !firstSent {
			firstSent = true
			select {
			case firstChunkSent <- struct{}{}:
			default:
			}
		}
	}

	// Run the agent loop (reusing IMMessageHandler pattern)
	fullResponse, err := h.runAgentForVE(ctx, sessionID, userMessage, onToken)
	if err != nil {
		return err
	}

	// If no streaming was done (agent returned full response without streaming),
	// send the complete response as a single stream_chunk + stream_end
	if !firstSent && strings.TrimSpace(fullResponse) != "" {
		h.SendStreamChunk(sessionID, fullResponse)
		select {
		case firstChunkSent <- struct{}{}:
		default:
		}
	}

	// Signal end of streaming
	h.SendStreamEnd(sessionID)
	return nil
}

// runAgentForVE runs the AI agent for a VE session.
// The onToken callback is invoked for each generated token during streaming.
// This reuses the IMMessageHandler's agent loop pattern.
func (h *VEMessageHandler) runAgentForVE(ctx context.Context, sessionID, userMessage string, onToken func(string)) (string, error) {
	if h.app == nil {
		return "", fmt.Errorf("app is nil")
	}

	// Build a simple LLM request using the app's configured LLM
	if _, err := h.app.LoadConfig(); err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	// Use the agent loop pattern: construct a prompt and call the LLM
	// In the full implementation, this delegates to corelib/agent.RunLoop
	// with VE-specific system prompt and tool set.
	// For now, we use a simplified single-turn LLM call with streaming.
	_ = ctx
	_ = sessionID
	_ = onToken

	// Placeholder: in production this connects to the full agent loop
	// via corelib/agent.RunLoop with onToken callback for streaming
	return fmt.Sprintf("[VE 宸插鐞哴 %s", userMessage), nil
}

// sendMessage sends a discussion message back through the Hub.
func (h *VEMessageHandler) sendMessage(sessionID string, msg a2a.GroupDiscussionMessage) {
	if h.app == nil {
		return
	}

	// Get the agent ID for this maclaw instance
	cfg, _ := h.app.LoadConfig()
	agentID := cfg.RemoteMachineID
	if agentID == "" {
		agentID = cfg.RemoteClientID
	}

	msg.FromID = agentID
	msg.SessionID = sessionID
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	// Send via the existing GroupDiscussionSendMessage method
	if err := h.app.GroupDiscussionSendMessage(sessionID, msg); err != nil {
		log.Printf("[ve-handler] failed to send message for session %s: %v", sessionID, err)
	}
}

// SendStreamChunk sends a streaming response chunk back to the requester.
// Each chunk is constructed as a GroupDiscussionMessage with kind=stream_chunk.
func (h *VEMessageHandler) SendStreamChunk(sessionID, chunk string) {
	h.sendMessage(sessionID, a2a.GroupDiscussionMessage{
		Kind:    a2a.MessageStreamChunk,
		Content: chunk,
	})
}

// SendStreamEnd signals the end of a streaming response.
// Constructed as a GroupDiscussionMessage with kind=stream_end.
func (h *VEMessageHandler) SendStreamEnd(sessionID string) {
	h.sendMessage(sessionID, a2a.GroupDiscussionMessage{
		Kind:    a2a.MessageStreamEnd,
		Content: "",
	})
}

// CloseSession closes a VE session and cleans up resources.
func (h *VEMessageHandler) CloseSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if session, ok := h.activeSessions[sessionID]; ok {
		if session.Cancel != nil {
			session.Cancel()
		}
		delete(h.activeSessions, sessionID)
	}
}

// ActiveSessionCount returns the number of active VE sessions.
func (h *VEMessageHandler) ActiveSessionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.activeSessions)
}
