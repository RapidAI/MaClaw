package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
)

// VEMessageHandler processes incoming A2A messages when this maclaw instance
// is acting as a digital employee. It receives GroupEnvelope messages (Type=discussion_message)
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
	ctx          context.Context
	History      []agent.ConversationEntry
}

// NewVEMessageHandler creates a new VE message handler.
func NewVEMessageHandler(app *App) *VEMessageHandler {
	return &VEMessageHandler{
		app:            app,
		activeSessions: make(map[string]*veSession),
	}
}

// HandleGroupEnvelope processes an incoming GroupEnvelope when this maclaw instance
// is acting as a digital employee. It validates the envelope type is discussion_message,
// extracts the content from the embedded GroupDiscussionMessage, and invokes the local
// AI agent (reusing the IMMessageHandler agent loop pattern).
func (h *VEMessageHandler) HandleGroupEnvelope(envelope a2a.GroupEnvelope) {
	if envelope.Type != a2a.GroupMessageDiscussionMessage {
		return
	}
	if envelope.Message == nil {
		return
	}
	// Allow messages with attachments even if content is empty
	if strings.TrimSpace(envelope.Message.Content) == "" && !HasAttachments(*envelope.Message) {
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
// when this maclaw instance is acting as a digital employee.
// It runs the local AI agent and sends streaming responses back via Hub.
// If the message contains attachments (TextAttachment/ImageAttachment/FileAttachment),
// they are decoded/downloaded and appended to the AI Agent input as context.
func (h *VEMessageHandler) HandleIncomingMessage(sessionID string, msg a2a.GroupDiscussionMessage) {
	if h.shouldIgnoreIncomingVEMessage(msg) {
		return
	}

	// Process attachments and append context to message content
	if HasAttachments(msg) {
		attachmentContext := h.ProcessMessageAttachments(msg)
		if attachmentContext != "" {
			msg.Content = msg.Content + attachmentContext
		}
	}

	if strings.TrimSpace(msg.Content) == "" {
		return
	}

	h.mu.Lock()
	session, ok := h.activeSessions[sessionID]
	h.mu.Unlock()
	if !ok {
		restoredHistory := h.restoreSessionHistory(sessionID, msg)
		h.mu.Lock()
		session, ok = h.activeSessions[sessionID]
		if !ok {
			ctx, cancel := context.WithCancel(context.Background())
			session = &veSession{
				SessionID:    sessionID,
				RequesterID:  msg.FromID,
				LastActivity: time.Now(),
				Cancel:       cancel,
				ctx:          ctx,
				History:      restoredHistory,
			}
			h.activeSessions[sessionID] = session
		}
		h.mu.Unlock()
	}

	h.mu.Lock()
	session.LastActivity = time.Now()
	sessionCtx := session.ctx
	h.mu.Unlock()

	// Process in background goroutine to not block the WebSocket reader
	go h.processAndRespond(sessionCtx, sessionID, msg)
}

// processAndRespond runs the AI agent on the incoming message and streams the response back.
// It implements the 60s first-response timeout: if no chunk is produced within 60s,
// a timeout error message is sent back.
func (h *VEMessageHandler) shouldIgnoreIncomingVEMessage(msg a2a.GroupDiscussionMessage) bool {
	switch msg.Kind {
	case a2a.MessageStreamChunk, a2a.MessageStreamEnd:
		return true
	}
	fromID := strings.TrimSpace(msg.FromID)
	if fromID == "" || h == nil || h.app == nil {
		return false
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return false
	}
	localID := firstNonEmptyGroupString(cfg.RemoteMachineID, cfg.RemoteClientID)
	return localID != "" && strings.EqualFold(fromID, localID)
}

func (h *VEMessageHandler) processAndRespond(sessionCtx context.Context, sessionID string, msg a2a.GroupDiscussionMessage) {
	// Derive a per-message context from the session context so that
	// CloseSession() cancellation propagates to in-flight processing.
	ctx, cancel := context.WithTimeout(sessionCtx, 5*time.Minute)
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
				Content: fmt.Sprintf("[error] Failed to process message: %v", r.err),
			})
		}
	case <-firstResponseTimer.C:
		// 60s timeout: no chunk produced
		cancel() // cancel the agent processing
		h.sendMessage(sessionID, a2a.GroupDiscussionMessage{
			Kind:    a2a.MessageStatement,
			Content: "[timeout] Digital employee response timed out after 60 seconds. Please try again later",
		})
		// Let the buffered result channel receive later; do not block after timeout.
	case <-ctx.Done():
		// Let the buffered result channel receive later; do not block after cancellation.
	}
}

// runAgentWithStreaming runs the AI agent and streams chunks back via Hub.
// Each generated chunk is sent as a GroupDiscussionMessage with kind=stream_chunk.
// When generation is complete, a kind=stream_end message is sent.
// The firstChunkSent channel is closed after the first chunk is sent.
func (h *VEMessageHandler) runAgentWithStreaming(ctx context.Context, sessionID, userMessage string, firstChunkSent chan<- struct{}) error {
	firstSent := false

	if query, ok := detectDigitalEmployeeSensitiveQuery(userMessage); ok {
		if h.shouldAnnounceSensitivePermissionRequest() {
			h.SendStreamChunk(sessionID, "\u6b63\u5728\u5bfb\u6c42\u4eba\u7c7b\u5458\u5de5\u8bb8\u53ef...")
			firstSent = true
			select {
			case firstChunkSent <- struct{}{}:
			default:
			}
		}
		if !h.authorizeSensitiveQuery(ctx, sessionID, query) {
			h.SendStreamChunk(sessionID, "\u4eba\u7c7b\u5458\u5de5\u672a\u6388\u6743\u63d0\u4f9b\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\uff0c\u5df2\u62d2\u7edd\u3002")
			h.SendStreamEnd(sessionID)
			return nil
		}
	}

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

	if ctx.Err() != nil {
		return ctx.Err()
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
// This reuses the IMMessageHandler's agent loop pattern via corelib/agent.RunLoop.
func (h *VEMessageHandler) runAgentForVE(ctx context.Context, sessionID, userMessage string, onToken func(string)) (string, error) {
	if h.app == nil {
		return "", fmt.Errorf("app is nil")
	}

	llmCfg := h.app.GetMaclawLLMConfig()
	if llmCfg.URL == "" && llmCfg.Key == "" {
		return "", fmt.Errorf("LLM not configured")
	}

	// Build VE-specific callbacks for the agent loop
	callbacks := &veAgentCallbacks{
		app:       h.app,
		ctx:       ctx,
		sessionID: sessionID,
		llmCfg:    llmCfg,
		onToken:   onToken,
	}

	// Load conversation history for this VE session
	h.mu.Lock()
	history := h.getSessionHistory(sessionID)
	h.mu.Unlock()

	// Run the shared agent loop
	result := agent.RunLoop(callbacks, userMessage, history, nil)

	// Save updated history
	h.mu.Lock()
	h.updateSessionHistory(sessionID, userMessage, result.Text)
	h.mu.Unlock()

	if result.Error != "" {
		return "", fmt.Errorf("%s", result.Error)
	}

	return result.Text, nil
}

// veAgentCallbacks implements agent.LoopCallbacks for VE sessions.
// It provides a simplified agent loop with VE-specific system prompt and tools.
type veAgentCallbacks struct {
	app       *App
	ctx       context.Context
	sessionID string
	llmCfg    corelib.MaclawLLMConfig
	onToken   func(string)
}

func (c *veAgentCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.llmCfg
}

func (c *veAgentCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(0) // use default
}

func (c *veAgentCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	return "You are a digital employee AI assistant. Answer the user professionally and accurately." +
		"\n\nSecurity guidelines:\n- Do not reveal passwords, tokens, API keys, private keys, or other sensitive credentials unless the local human employee approval gate has already allowed this request.\n- If sensitive information is unavailable or approval was not granted, say that you cannot provide it." +
		"\n\nResponse guidelines:\n- Be concise and direct\n- Provide complete runnable examples when code is needed\n- Say clearly when you are unsure"
}

func (c *veAgentCallbacks) BuildTools(userText string) []map[string]interface{} {
	// VE sessions use a minimal tool set: no tools for now
	return nil
}

func (c *veAgentCallbacks) ExecuteTool(name, argsJSON string) string {
	return fmt.Sprintf("[tool %s is unavailable in digital employee mode]", name)
}

func (c *veAgentCallbacks) OnToken(delta string) {
	if c.onToken != nil {
		c.onToken(delta)
	}
}

func (c *veAgentCallbacks) OnProgress(text string) {}

func (c *veAgentCallbacks) OnToolCall(name string) {}

func (c *veAgentCallbacks) OnToolResult(name string) {}

func (c *veAgentCallbacks) ShouldStop() bool {
	return c.ctx.Err() != nil
}

const sensitivePermissionWaitingText = "\u6b63\u5728\u5bfb\u6c42\u4eba\u7c7b\u5458\u5de5\u8bb8\u53ef..."

func (h *VEMessageHandler) restoreSessionHistory(sessionID string, current a2a.GroupDiscussionMessage) []agent.ConversationEntry {
	if h == nil || h.app == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	detail, err := h.app.GroupDiscussionGetConsultationDetail(sessionID)
	if err != nil {
		return nil
	}
	cfg, _ := h.app.LoadConfig()
	localID := firstNonEmptyGroupString(cfg.RemoteMachineID, cfg.RemoteClientID)
	messages := detail.Messages
	if len(messages) == 0 && detail.Session != nil {
		messages = detail.Session.Messages
	}
	return buildVEConversationHistoryFromMessages(messages, localID, current)
}

func buildVEConversationHistoryFromMessages(messages []a2a.Message, localID string, current a2a.GroupDiscussionMessage) []agent.ConversationEntry {
	localID = strings.TrimSpace(localID)
	entries := make([]agent.ConversationEntry, 0, len(messages))
	var stream strings.Builder
	streamFrom := ""
	flushStream := func() {
		content := cleanVESessionHistoryContent(stream.String())
		fromID := strings.TrimSpace(streamFrom)
		stream.Reset()
		streamFrom = ""
		if content == "" {
			return
		}
		entries = append(entries, agent.ConversationEntry{Role: veHistoryRoleForSender(fromID, localID), Content: veHistoryContentForSender(fromID, localID, content)})
	}
	for _, msg := range messages {
		if isCurrentVEHistoryMessage(msg, current) {
			break
		}
		fromID := strings.TrimSpace(msg.FromID)
		switch msg.Kind {
		case a2a.MessageStreamChunk:
			if streamFrom != "" && !strings.EqualFold(streamFrom, fromID) {
				flushStream()
			}
			streamFrom = fromID
			stream.WriteString(msg.Content)
			continue
		case a2a.MessageStreamEnd:
			flushStream()
			continue
		default:
			flushStream()
		}
		content := cleanVESessionHistoryContent(msg.Content)
		if content == "" {
			continue
		}
		entries = append(entries, agent.ConversationEntry{Role: veHistoryRoleForSender(fromID, localID), Content: veHistoryContentForSender(fromID, localID, content)})
	}
	flushStream()
	if len(entries) > 40 {
		entries = entries[len(entries)-40:]
	}
	return entries
}

func isCurrentVEHistoryMessage(msg a2a.Message, current a2a.GroupDiscussionMessage) bool {
	currentID := strings.TrimSpace(current.ID)
	if currentID != "" {
		return strings.EqualFold(strings.TrimSpace(msg.ID), currentID)
	}
	if current.CreatedAt.IsZero() || !msg.CreatedAt.Equal(current.CreatedAt) {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(msg.FromID), strings.TrimSpace(current.FromID)) &&
		msg.Kind == current.Kind &&
		strings.TrimSpace(msg.Content) == strings.TrimSpace(current.Content)
}

func veHistoryRoleForSender(fromID, localID string) string {
	if localID != "" && strings.EqualFold(strings.TrimSpace(fromID), localID) {
		return "assistant"
	}
	return "user"
}

func veHistoryContentForSender(fromID, localID, content string) string {
	content = strings.TrimSpace(content)
	if content == "" || veHistoryRoleForSender(fromID, localID) == "assistant" {
		return content
	}
	fromID = strings.TrimSpace(fromID)
	if fromID == "" {
		return content
	}
	return "[" + fromID + "] " + content
}

func cleanVESessionHistoryContent(content string) string {
	content = strings.ReplaceAll(content, sensitivePermissionWaitingText, "")
	return strings.TrimSpace(content)
}

// getSessionHistory returns the conversation history for a VE session.
// Must be called with h.mu held.
func (h *VEMessageHandler) getSessionHistory(sessionID string) []agent.ConversationEntry {
	session, ok := h.activeSessions[sessionID]
	if !ok || session == nil {
		return nil
	}
	return session.History
}

// updateSessionHistory appends the user message and assistant response to the session history.
// Must be called with h.mu held.
func (h *VEMessageHandler) updateSessionHistory(sessionID, userMessage, assistantResponse string) {
	session, ok := h.activeSessions[sessionID]
	if !ok || session == nil {
		return
	}
	session.History = append(session.History,
		agent.ConversationEntry{Role: "user", Content: userMessage},
	)
	if strings.TrimSpace(assistantResponse) != "" {
		session.History = append(session.History,
			agent.ConversationEntry{Role: "assistant", Content: assistantResponse},
		)
	}
	// Keep history bounded
	if len(session.History) > 40 {
		session.History = session.History[len(session.History)-40:]
	}
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
