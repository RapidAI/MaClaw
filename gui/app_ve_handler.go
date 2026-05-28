package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
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
	switch envelope.Type {
	case a2a.GroupMessageDiscussionMessage:
		// Existing discussion message handling
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

	case a2a.GroupMessageApprovalRequest:
		h.handleApprovalRequest(envelope)

	default:
		return
	}
}

// handleApprovalRequest processes an incoming approval_request envelope by
// deserializing the payload and routing it to the VE approval handler.
func (h *VEMessageHandler) handleApprovalRequest(envelope a2a.GroupEnvelope) {
	if len(envelope.Payload) == 0 {
		log.Printf("[ve-handler] received approval_request with empty payload, ignoring")
		return
	}

	var req veApprovalRequestPayload
	if err := json.Unmarshal(envelope.Payload, &req); err != nil {
		log.Printf("[ve-handler] failed to parse approval request payload: %v", err)
		return
	}

	// Load VE approval config and create handler if needed
	cfg := h.loadVEApprovalConfig()
	if cfg == nil || !cfg.Enabled {
		log.Printf("[ve-handler] approval capability disabled, rejecting request %s", req.ID)
		h.sendApprovalResponse(envelope, req.ID, "reject", "approval capability is disabled on this VE")
		return
	}

	details, err := decodeVEApprovalDetails(req.Details)
	if err != nil {
		log.Printf("[ve-handler] failed to parse approval request details: %v", err)
		h.sendApprovalResponse(envelope, req.ID, "reject", "invalid approval request details: "+err.Error())
		return
	}

	handler := NewVEApprovalHandler(cfg)
	veReq := &VEApprovalRequest{
		ID:            req.ID,
		RequesterID:   req.RequesterID,
		RequesterName: req.RequesterName,
		Payload:       details,
	}

	decision, err := handler.HandleApprovalRequest(context.Background(), veReq)
	if err != nil {
		log.Printf("[ve-handler] approval request %s rejected: %v", req.ID, err)
		h.sendApprovalResponse(envelope, req.ID, "reject", err.Error())
		return
	}

	h.sendApprovalResponse(envelope, req.ID, string(decision.Decision), decision.Rationale)
}

// veApprovalRequestPayload is the JSON structure within an approval_request envelope payload.
type veApprovalRequestPayload struct {
	ID            string          `json:"id"`
	InstanceID    string          `json:"instance_id"`
	NodeID        string          `json:"node_id"`
	RequesterID   string          `json:"requester_id"`
	RequesterName string          `json:"requester_name"`
	WorkflowName  string          `json:"workflow_name"`
	Title         string          `json:"title"`
	Summary       string          `json:"summary"`
	Details       json.RawMessage `json:"details"`
}

func decodeVEApprovalDetails(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]interface{}{}, nil
	}
	var details map[string]interface{}
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil, err
	}
	if details == nil {
		details = map[string]interface{}{}
	}
	return details, nil
}

// sendApprovalResponse sends an approval_response back through the Hub
// as a discussion message with the decision embedded in the content.
func (h *VEMessageHandler) sendApprovalResponse(originalEnvelope a2a.GroupEnvelope, requestID, decision, rationale string) {
	if h.app == nil {
		return
	}

	sessionID := originalEnvelope.SessionID
	if sessionID == "" {
		log.Printf("[ve-handler] cannot send approval response: no session ID in original envelope")
		return
	}

	response := map[string]interface{}{
		"type":       "approval_response",
		"request_id": requestID,
		"decision":   decision,
		"rationale":  rationale,
		"decided_at": time.Now().UTC().Format(time.RFC3339),
	}

	payload, err := json.Marshal(response)
	if err != nil {
		log.Printf("[ve-handler] failed to marshal approval response: %v", err)
		return
	}

	// Send as a discussion message with the approval response payload in content.
	msg := a2a.GroupDiscussionMessage{
		Kind:    a2a.MessageStatement,
		Content: string(payload),
	}
	if err := h.app.sendVEA2AMessage(sessionID, msg); err != nil {
		log.Printf("[ve-handler] failed to send approval response for request %s: %v", requestID, err)
	}
}

// loadVEApprovalConfig loads the VE approval configuration from the app config.
func (h *VEMessageHandler) loadVEApprovalConfig() *VEApprovalConfig {
	if h.app == nil {
		return nil
	}
	approvalCfg, err := h.app.GetVEApprovalConfig()
	if err != nil {
		return nil
	}
	return approvalCfg
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
		attachmentContext := h.ProcessMessageAttachmentsForSession(sessionID, msg)
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
		if chunk == "" {
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
// Uses a dedicated agent loop with VE-specific system prompt and a safe tool subset.
// This is intentionally separate from the main IMMessageHandler to maintain security
// isolation: VE sessions don't trigger workflow engines, coding gates, or other
// main-agent middleware that could interfere with remote user requests.
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

	// Run the shared agent loop with VE-specific tools and system prompt
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

	// Cached knowledge base availability is computed once per agent loop invocation
	// to avoid repeated SQLite open/close and ensure BuildSystemPrompt and BuildTools
	// see a consistent value.
	knowledgeChecked bool
	hasKnowledge     bool
}

func (c *veAgentCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.llmCfg
}

func (c *veAgentCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(0) // use default
}

// veKnowledgeAvailable returns whether the knowledge base has content.
// Result is cached for the lifetime of this callbacks instance (one agent loop invocation).
func (c *veAgentCallbacks) veKnowledgeAvailable() bool {
	if c.knowledgeChecked {
		return c.hasKnowledge
	}
	c.knowledgeChecked = true
	c.hasKnowledge = veHasKnowledgeSources(c.app)
	return c.hasKnowledge
}
func (c *veAgentCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	var veName, veSkill string
	if c.app != nil {
		if status, err := c.app.GetVEStatus(); err == nil && status != nil && status.Employee != nil {
			veName = status.Employee.Name
			veSkill = status.Employee.SkillDescription
		}
	}

	var sb strings.Builder
	if veName != "" {
		sb.WriteString(fmt.Sprintf("You are %s, a digital employee AI assistant.", veName))
	} else {
		sb.WriteString("You are a digital employee AI assistant.")
	}
	if veSkill != "" {
		sb.WriteString(fmt.Sprintf(" Your specialty: %s.", veSkill))
	}
	if isFirstTurn {
		sb.WriteString("\nThis is the first turn of the session. Establish context and handle the request directly.")
	}

	sb.WriteString("\n\nCore rules:\n")
	sb.WriteString("- Reply in the user's language by default.\n")
	sb.WriteString("- Be concise, professional, and accurate; state uncertainty clearly.\n")
	sb.WriteString("- Provide complete runnable code examples when code is needed.\n")

	sb.WriteString("\nCapability boundaries:\n")
	sb.WriteString("- You run on the digital employee owner's machine for a remote user.\n")
	sb.WriteString("- You may read local files and list directories with read_file and list_directory.\n")

	hasKnowledge := c.veKnowledgeAvailable()
	if hasKnowledge {
		sb.WriteString("- You may search the local knowledge base with knowledge_search and knowledge_context_pack.\n")
		sb.WriteString("- The knowledge base is the preferred source for saved pages, documents, notes, and structured knowledge.\n")
	}

	allowedDirs := c.getVEAllowedDirectories()
	if len(allowedDirs) > 0 {
		sb.WriteString("- You cannot modify files, execute commands, or operate a browser; you may send files from the configured allowed directories.\n")
	} else {
		sb.WriteString("- You cannot modify files, execute commands, access the network, operate a browser, or send files until the owner adds at least one allowed access directory in Settings > Digital Employee.\n")
	}
	sb.WriteString("- Sensitive files such as .env, private keys, and credentials are blocked and must not be read or sent.\n")
	sb.WriteString("- If an operation is unsupported in digital employee mode, say so directly and do not invent reasons.\n")
	sb.WriteString("- You may answer questions, provide advice, generate text, analyze problems, and read allowed file content.\n")
	sb.WriteString("- You may use memory recall to retrieve the owner's accumulated facts, preferences, and project knowledge.\n")

	sb.WriteString("\nSafety rules:\n")
	sb.WriteString("- Do not reveal passwords, tokens, API keys, private keys, or other sensitive credentials.\n")
	sb.WriteString("- If sensitive information is unavailable or not approved, say that you cannot provide it.\n")

	if hasKnowledge {
		sb.WriteString("\n## Knowledge Base Rules\n")
		sb.WriteString("- Prefer the auto-recalled knowledge base context below when relevant, and cite sources when possible.\n")
		sb.WriteString("- If auto recall is insufficient, call knowledge_search or knowledge_context_pack.\n")
		sb.WriteString("- Distinguish knowledge-base information from general model knowledge.\n")
		sb.WriteString("- If the knowledge base has no relevant information, say that and then supplement with general knowledge.\n")
		c.appendVEKnowledgeAutoRecall(&sb, userText)
	}

	if len(allowedDirs) > 0 {
		sb.WriteString("\n## 文件发送能力 / File Sending\n")
		sb.WriteString("- You may use send_file to send files from the configured allowed directories.\n")
		sb.WriteString("- When the user asks you to send, give, attach, or share a file, call send_file; do not paste the file contents as plain text unless the user explicitly asks to view/read the contents.\n")
		sb.WriteString("- If the requested file is outside the allowed directories, say that the directory is not authorized and ask the owner to add it in Settings > Digital Employee > Allowed Access Directories.\n")
		sb.WriteString("- Allowed directories:\n")
		for _, dir := range allowedDirs {
			sb.WriteString(fmt.Sprintf("  - %s\n", dir))
		}
		sb.WriteString("- Before sending, browse with list_directory when needed and confirm the exact requested file.\n")
		sb.WriteString("- File size limit: 50 MB.\n")
		sb.WriteString("- 敏感文件 / Sensitive files must not be sent even from allowed directories.\n")
	}

	c.appendVEMemoryRecall(&sb, userText)
	return sb.String()
}

// appendVEKnowledgeAutoRecall searches the knowledge base using the user message
// and injects top results into the system prompt for VE sessions.
func (c *veAgentCallbacks) appendVEKnowledgeAutoRecall(b *strings.Builder, msg string) {
	if msg == "" || c.app == nil {
		return
	}

	query := msg
	runes := []rune(query)
	if len(runes) > 200 {
		query = string(runes[:200])
	}

	// Reuse the global auto-recall store singleton used by the main
	// AI assistant. This avoids repeated open/close and benefits from FTS index caching.
	knowledgeAutoRecallStoreMu.Lock()
	store := knowledgeAutoRecallStore
	knowledgeAutoRecallStoreMu.Unlock()

	if store == nil {
		// Lazily initialize if not yet opened (VE message may arrive before main AI assistant)
		var err error
		store, err = c.app.openKnowledgeStore()
		if err != nil {
			return
		}
		knowledgeAutoRecallStoreMu.Lock()
		if knowledgeAutoRecallStore == nil {
			knowledgeAutoRecallStore = store
		} else {
			// Another goroutine initialized it; close our duplicate and use theirs.
			store.Close()
			store = knowledgeAutoRecallStore
		}
		knowledgeAutoRecallStoreMu.Unlock()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, err := store.Search(ctx, knowledge.SearchOptions{
		Query: query,
		Limit: 5,
	})
	if err != nil || len(results) == 0 {
		return
	}

	// Dynamic threshold + injection count based on top score (same logic as main AI assistant)
	topScore := results[0].Score
	var maxInject int
	switch {
	case topScore >= 3.0:
		maxInject = 3
	case topScore >= 1.0:
		maxInject = 2
	case topScore >= 0.3:
		maxInject = 1
	default:
		return
	}

	b.WriteString("\n## Knowledge Base References (auto recall)\n")
	b.WriteString("The following content may be relevant to the current question. Prefer it when applicable; ignore it if unrelated.\n")
	b.WriteString("Call knowledge_search or knowledge_context_pack if deeper retrieval is needed.\n\n")

	injected := 0
	for _, r := range results {
		if injected >= maxInject {
			break
		}
		if r.Score < 0.3 {
			break
		}
		source := r.Source.Title
		if source == "" {
			source = r.Source.RelativePath
		}
		if source == "" {
			source = r.Source.URI
		}
		text := knowledgeAutoRecallSnippet(r)
		if text == "" {
			continue
		}
		if len([]rune(text)) > 200 {
			text = string([]rune(text)[:200]) + "..."
		}
		b.WriteString(fmt.Sprintf("- [%s] %s\n", source, text))
		injected++
	}
}

// appendVEMemoryRecall performs proactive recall from the machine owner's memory
// store and injects relevant entries into the VE system prompt. This bridges the
// gap between the knowledge base (structured documents) and the memory system
// (accumulated facts, project knowledge, user preferences, task artifacts).
//
// Without this, the VE can only access knowledge base content but not memories
// like personal facts or SSH server credentials that the owner's maclaw has learned.
func (c *veAgentCallbacks) appendVEMemoryRecall(b *strings.Builder, msg string) {
	if c.app == nil {
		return
	}
	// Ensure memory store is initialized (it's lazily created; VE messages may
	// arrive before the main AI assistant triggers ensureMemoryStore).
	if c.app.memoryStore == nil {
		c.app.ensureMemoryStore()
	}
	memStore := c.app.memoryStore
	if memStore == nil {
		return
	}

	// --- User Facts: always inject the owner's user_fact summary ---
	// user_fact entries are excluded from
	// RecallDynamic (they're injected separately in the main AI assistant via
	// UserFactSummary). VE must also inject them to answer personal questions.
	b.WriteString(memStore.UserFactSummaryForPrompt(corememory.UserFactPromptOptions("\n## Owner Information")))

	// --- Dynamic memory context: index plus optional proactive recall. ---
	promptContext, _ := memStore.ProactiveContextForPrompt(msg, corememory.VEProactivePromptOptions())
	b.WriteString(promptContext)

}

func (c *veAgentCallbacks) BuildTools(userText string) []map[string]interface{} {
	// VE sessions use the same ToolRegistry as the main agent, filtered by VE policy.
	// This ensures VE automatically inherits new read-only tools (knowledge, search, etc.)
	// without manual maintenance. Blocked tools (write, execute, modify) are removed.
	//
	// When VEAllowedDirectories is configured, filterToolsForVEWithConfig conditionally
	// unblocks send_file, list_directory, and read_file (Requirements 4.1, 4.2, 6.1).
	if c.app != nil {
		handler := c.app.ensureLocalIMHandler()
		if handler != nil && handler.registry != nil {
			allTools := NewDynamicToolBuilder(handler.registry).BuildAll()
			allowedDirs := c.getVEAllowedDirectories()
			return filterToolsForVEWithConfig(allTools, allowedDirs)
		}
	}
	// Fallback: if registry is unavailable, return minimal safe tools
	return veRemoteToolDefinitions(c.veKnowledgeAvailable())
}

// getVEAllowedDirectories reads the VEAllowedDirectories list from AppConfig.
// Returns an empty slice if config is unavailable or the field is not set.
func (c *veAgentCallbacks) getVEAllowedDirectories() []string {
	if c.app == nil {
		return nil
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.VEAllowedDirectories
}

func (c *veAgentCallbacks) ExecuteTool(name, argsJSON string) string {
	// Load allowedDirs once per tool invocation; used by both the blocked-tool
	// conditional unblock and the file-operation path validation below.
	allowedDirs := c.getVEAllowedDirectories()

	// Defense in depth: even if a blocked tool's definition leaked into the tool list,
	// the execution layer rejects it.
	//
	// Exception: tools in veConfigUnblockedTools are conditionally allowed when
	// VEAllowedDirectories is configured. The definition layer (filterToolsForVEWithConfig)
	// and execution layer must agree; both check allowedDirs.
	if isVEToolBlocked(name) {
		// Conditional unblock: when allowedDirs is configured, tools in
		// veConfigUnblockedTools are allowed through (execution-layer path
		// validation enforces directory scoping below).
		if !(len(allowedDirs) > 0 && veConfigUnblockedTools[name]) {
			if name == "send_file" && len(allowedDirs) == 0 {
				return "[error] send_file is unavailable because no allowed access directories are configured. Add a directory in Settings > Digital Employee > Allowed Access Directories first."
			}
			return fmt.Sprintf("[error] tool %s is unavailable in digital employee mode (safety policy)", name)
		}
	}

	// Parse args once; reused for action check and handler invocation.
	var args map[string]interface{}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("[error] failed to parse arguments: %v", err)
		}
	}

	// Check per-tool action restrictions (e.g., memory save is blocked)
	if action, _ := args["action"].(string); action != "" {
		if isVEToolActionBlocked(name, action) {
			return fmt.Sprintf("[error] tool %s action %s is unavailable in digital employee mode", name, action)
		}
	}

	// VE MCP-only skill execution guard: run_skill is allowed only for
	// skills whose steps are all call_mcp_tool.
	if name == "run_skill" {
		skillName, _ := args["name"].(string)
		if skillName == "" {
			return "[error] missing name parameter"
		}
		allowed, reason := isVERunSkillAllowed(skillName, c.app)
		if !allowed {
			return fmt.Sprintf("[error] digital employee cannot run this Skill: %s", reason)
		}
		// Allowed; fall through to registry execution below.
	}

	// ---------------------------------------------------------------------------
	// Defense-in-depth path validation for VE file operations.
	// Validation chain (first failure stops execution):
	//   1. Tool blocked check (isVEToolBlocked) handled above
	//   2. Path parameter check (empty/missing)
	//   3. Directory containment check (ValidateVEFilePath / IsWithinAllowedDirs)
	//   4. Sensitive file check (CheckVEPathSensitive)
	//   5. File size check (> 50 MB, send_file only)
	//   6. Actual file read + send: success or OS error
	//
	// Requirements: 4.3, 4.6, 6.3, 6.4, 6.5
	// ---------------------------------------------------------------------------

	switch name {
	case "send_file":
		path, _ := args["path"].(string)
		// Steps 2-3: empty path + directory containment check (also returns FileInfo to avoid double stat)
		canonicalPath, info, err := ValidateVEFilePathWithInfo(path, allowedDirs)
		if err != nil {
			return err.Error()
		}
		// Step 4: Sensitive file check
		if sensitiveErr := CheckVEPathSensitive(canonicalPath); sensitiveErr != nil {
			return sensitiveErr.Error()
		}
		// Step 5: File size check (50 MB limit for VE mode)
		// Uses info from ValidateVEFilePathWithInfo; no redundant os.Stat call.
		if info.Size() > veFileAttachmentMaxSize {
			return fmt.Sprintf("[error] file is too large: %d bytes; VE mode limit is 50 MB", info.Size())
		}
		// Step 6: Upload to Hub relay and emit a real A2A attachment message.
		if c.app == nil || strings.TrimSpace(c.sessionID) == "" {
			return "[error] send_file handler unavailable"
		}
		displayName, _ := args["file_name"].(string)
		message, _ := args["message"].(string)
		if strings.TrimSpace(message) == "" {
			message = fmt.Sprintf("已发送文件：%s", firstNonEmptyGroupString(displayName, filepath.Base(canonicalPath)))
		}
		if err := c.app.sendVEFileAttachmentMessage(c.sessionID, canonicalPath, displayName, message); err != nil {
			return fmt.Sprintf("[error] send_file failed: %v", err)
		}
		return fmt.Sprintf("File %s has been sent to the user.", filepath.Base(canonicalPath))

	case "read_file":
		// When allowedDirs is configured, enforce directory containment.
		// When allowedDirs is empty, fall through to the original VE read_file
		// handler (veToolReadFile) which only does sensitive file check + size limit.
		// This preserves backward compatibility: VE can always read non-sensitive files.
		if len(allowedDirs) > 0 {
			path, _ := args["path"].(string)
			// Steps 2-3: empty path + directory containment check
			canonicalPath, err := ValidateVEFilePath(path, allowedDirs)
			if err != nil {
				return err.Error()
			}
			// Step 4: Sensitive file check
			if sensitiveErr := CheckVEPathSensitive(canonicalPath); sensitiveErr != nil {
				return sensitiveErr.Error()
			}
		}
		// Delegate to handler (applies its own sensitive check + size limit when no allowedDirs)
		return executeVERemoteTool(c.app, name, argsJSON)

	case "list_directory":
		// When allowedDirs is configured, enforce directory containment.
		// When allowedDirs is empty, fall through to the original VE list_directory
		// handler (veToolListDirectory) which only blocks sensitive directories.
		if len(allowedDirs) > 0 {
			path, _ := args["path"].(string)
			// Steps 2-3: empty path + directory containment check
			if _, err := IsWithinAllowedDirs(path, allowedDirs); err != nil {
				return err.Error()
			}
		}
		// Delegate to handler (applies its own sensitive dir check when no allowedDirs)
		return executeVERemoteTool(c.app, name, argsJSON)
	}

	// Execute via the main ToolRegistry; uses same handlers as main agent.
	// This gives VE access to knowledge_search, knowledge_context_pack, memory(recall),
	// web_search, web_fetch, discover_tool, and any future read-only tools automatically.
	if c.app != nil {
		handler := c.app.ensureLocalIMHandler()
		if handler != nil && handler.registry != nil {
			tool, ok := handler.registry.Get(name)
			if ok && tool.Handler != nil {
				return tool.Handler(args)
			}
			if ok && tool.HandlerProg != nil {
				return tool.HandlerProg(args, nil)
			}
		}
	}

	// Final fallback for tools not in registry (shouldn't happen in normal operation)
	return executeVERemoteTool(c.app, name, argsJSON)
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
	if !a2a.GroupDiscussionMessageHasPayload(msg) {
		log.Printf("[ve-handler] skipped empty outbound message for session %s kind=%s", sessionID, msg.Kind)
		return
	}
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

	// Send via the VE A2A path so direct employee chat works without enabling group discussion tools.
	if err := h.app.sendVEA2AMessage(sessionID, msg); err != nil {
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
