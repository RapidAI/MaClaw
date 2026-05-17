package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
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

	// Cached knowledge base availability — computed once per agent loop invocation
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
	// Build a richer system prompt using the VE's registered identity
	var veName, veSkill string
	if c.app != nil {
		if status, err := c.app.GetVEStatus(); err == nil && status != nil && status.Employee != nil {
			veName = status.Employee.Name
			veSkill = status.Employee.SkillDescription
		}
	}

	var sb strings.Builder
	if veName != "" {
		sb.WriteString(fmt.Sprintf("你是「%s」，一个数字员工AI助手。", veName))
	} else {
		sb.WriteString("你是一个数字员工AI助手。")
	}
	if veSkill != "" {
		sb.WriteString(fmt.Sprintf("你的专长领域：%s。", veSkill))
	}
	sb.WriteString("\n\n核心原则：\n")
	sb.WriteString("- 用中文回复（除非用户使用其他语言）\n")
	sb.WriteString("- 简洁、专业、准确\n")
	sb.WriteString("- 不确定时明确说明\n")
	sb.WriteString("- 提供完整可运行的代码示例（如果需要）\n")
	sb.WriteString("\n能力边界（重要）：\n")
	sb.WriteString("- 你运行在数字员工所有者的机器上，为远程用户提供服务\n")
	sb.WriteString("- 你可以读取本地文件和浏览目录（使用 read_file 和 list_directory 工具）\n")

	// Knowledge base capability declaration
	hasKnowledge := c.veKnowledgeAvailable()
	if hasKnowledge {
		sb.WriteString("- 你可以搜索本地知识库（使用 knowledge_search 和 knowledge_context_pack 工具）\n")
		sb.WriteString("- 知识库包含所有者保存的网页、文档、笔记等结构化知识，是你回答问题的首要信息来源\n")
	}

	sb.WriteString("- 你不能修改文件、执行命令、访问网络或操作浏览器\n")
	sb.WriteString("- 敏感文件（.env、私钥、credentials 等）会被自动拦截，无法读取\n")
	sb.WriteString("- 当用户请求你无法完成的操作时，直接说明「当前数字员工模式不支持此操作」，不要编造理由（如「我是云端程序」「我没有本地系统」等）\n")
	sb.WriteString("- 你可以回答知识性问题、提供建议、生成文本内容、分析问题、读取文件内容\n")
	sb.WriteString("- 你可以检索所有者的记忆库（使用 memory 工具的 recall 操作），获取所有者积累的事实、偏好和项目知识\n")
	sb.WriteString("\n安全规则：\n")
	sb.WriteString("- 不要泄露密码、token、API key、私钥等敏感凭据\n")
	sb.WriteString("- 如果敏感信息不可用或未获批准，说明无法提供即可\n")

	// Knowledge base rules — instruct the LLM to use knowledge tools
	if hasKnowledge {
		sb.WriteString("\n## 知识库使用规则（重要）\n")
		sb.WriteString("- 回答优先级：当用户提问时，**必须优先使用知识库内容回答**。先查看下方「知识库参考」中是否有答案，有则直接引用并标注来源。\n")
		sb.WriteString("- 主动检索：如果自动检索的内容不够详细，主动调用 knowledge_search 或 knowledge_context_pack 工具深入检索。\n")
		sb.WriteString("- 来源透明：回答中明确区分哪些信息来自知识库、哪些来自你的训练数据。\n")
		sb.WriteString("- 绝不要在有知识库可查的情况下直接用训练数据回答——用户信任的是所有者积累的知识。\n")
		sb.WriteString("- 如果知识库中确实没有相关信息，明确告知用户「知识库中未找到相关信息」，然后用训练数据补充回答并标注。\n")

		// Auto-recall: inject relevant knowledge snippets into the system prompt
		c.appendVEKnowledgeAutoRecall(&sb, userText)
	}

	// File-sending capability declaration (Requirements 4.5):
	// When VEAllowedDirectories is non-empty, declare the file-sending capability
	// so the LLM knows it can browse, inspect, and send files from those directories.
	allowedDirs := c.getVEAllowedDirectories()
	if len(allowedDirs) > 0 {
		sb.WriteString("\n## 文件发送能力\n")
		sb.WriteString("- 你可以发送文件给用户（使用 send_file 工具）\n")
		sb.WriteString("- 允许访问的目录：\n")
		for _, dir := range allowedDirs {
			sb.WriteString(fmt.Sprintf("  - %s\n", dir))
		}
		sb.WriteString("- 发送文件前，先用 list_directory 浏览目录内容，用 read_file 确认文件内容\n")
		sb.WriteString("- 文件大小限制：50 MB\n")
		sb.WriteString("- 敏感文件（.env、私钥等）即使在允许目录中也无法发送\n")
	}

	// Memory recall: inject relevant memories from the machine owner's memory store.
	// This gives the VE access to facts, project knowledge, and other information
	// that the owner's maclaw has accumulated (e.g. "安妮18岁" stored as user_fact).
	c.appendVEMemoryRecall(&sb, userText)

	return sb.String()
}

// appendVEKnowledgeAutoRecall searches the knowledge base using the user message
// and injects top results into the system prompt for VE sessions.
// Reuses the global knowledgeAutoRecallStore singleton (same as main AI assistant)
// to avoid repeated SQLite open/close overhead.
func (c *veAgentCallbacks) appendVEKnowledgeAutoRecall(b *strings.Builder, msg string) {
	if msg == "" || c.app == nil {
		return
	}

	// Truncate long messages to first 200 chars for FTS query
	query := msg
	runes := []rune(query)
	if len(runes) > 200 {
		query = string(runes[:200])
	}

	// Reuse the global auto-recall store singleton — same instance used by the main
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
			// Another goroutine initialized it — close our duplicate and use theirs
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

	b.WriteString("\n## 知识库参考（自动检索）\n")
	b.WriteString("以下内容来自知识库，与当前问题可能相关。请优先引用相关内容回答；不相关则忽略。\n")
	b.WriteString("如需更多信息，可调用 knowledge_search 或 knowledge_context_pack 深入检索。\n\n")

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
// like "安妮18岁" or SSH server credentials that the owner's maclaw has learned.
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
	// user_fact entries (e.g. "安妮18岁", "马勇的妹妹叫安妮") are excluded from
	// RecallDynamic (they're injected separately in the main AI assistant via
	// UserFactSummary). VE must also inject them to answer personal questions.
	if summary := memStore.UserFactSummary(400); summary != "" {
		b.WriteString("\n## 所有者信息\n")
		b.WriteString(summary)
		b.WriteString("\n")
	}

	// --- Memory Index: tell the LLM what categories of knowledge exist ---
	stats := memStore.CategoryStats()
	if len(stats) > 0 {
		var parts []string
		for _, st := range stats {
			part := fmt.Sprintf("%s: %d条", st.Category.DisplayName(), st.Count)
			if len(st.Tags) > 0 {
				part += "(" + strings.Join(st.Tags, ", ") + ")"
			}
			parts = append(parts, part)
		}
		b.WriteString("\n[记忆索引] ")
		b.WriteString(strings.Join(parts, " | "))
		b.WriteString("\n")
	}

	// --- Proactive Recall: find memories relevant to the user's question ---
	if msg == "" {
		return
	}
	recalled := memStore.RecallDynamic(msg, "", "")

	// Supplementary entity-based recall for short/noisy messages
	if len(recalled) < 8 {
		expanded := corememory.ExpandQuery(msg)
		if len(expanded.Entities) > 0 {
			seen := make(map[string]bool, len(recalled))
			for _, e := range recalled {
				seen[e.ID] = true
			}
			entities := expanded.Entities
			if len(entities) > 1 {
				entities = entities[:1]
			}
			for _, entity := range entities {
				extra := memStore.RecallDynamic(entity, "", "")
				for _, e := range extra {
					if !seen[e.ID] {
						seen[e.ID] = true
						recalled = append(recalled, e)
						if len(recalled) >= 10 {
							break
						}
					}
				}
				if len(recalled) >= 10 {
					break
				}
			}
		}
	}

	// Cap at 10 entries to control prompt size (VE has simpler prompts, less budget)
	const maxVERecall = 10
	if len(recalled) > maxVERecall {
		recalled = recalled[:maxVERecall]
	}

	if len(recalled) > 0 {
		b.WriteString("\n## 所有者记忆（自动召回）\n")
		b.WriteString("以下信息来自所有者的记忆库，与当前问题可能相关。请结合知识库和记忆内容回答。\n")
		for _, e := range recalled {
			text := e.CompactForm
			if text == "" {
				text = e.Content
			}
			runes := []rune(text)
			if len(runes) > 200 {
				text = string(runes[:200]) + "…"
			}
			b.WriteString(fmt.Sprintf("- [%s] %s\n", e.Category, text))
		}
		b.WriteString("如需更多记忆，可调用 memory(action: recall, query: \"关键词\") 深入检索。\n")
	}
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
	// Load allowedDirs once per tool invocation — used by both the blocked-tool
	// conditional unblock and the file-operation path validation below.
	allowedDirs := c.getVEAllowedDirectories()

	// Defense in depth: even if a blocked tool's definition leaked into the tool list,
	// the execution layer rejects it.
	//
	// Exception: tools in veConfigUnblockedTools are conditionally allowed when
	// VEAllowedDirectories is configured. The definition layer (filterToolsForVEWithConfig)
	// and execution layer must agree — both check allowedDirs.
	if isVEToolBlocked(name) {
		// Conditional unblock: when allowedDirs is configured, tools in
		// veConfigUnblockedTools are allowed through (execution-layer path
		// validation enforces directory scoping below).
		if !(len(allowedDirs) > 0 && veConfigUnblockedTools[name]) {
			return fmt.Sprintf("[error] 工具 %s 在数字员工模式下不可用（安全策略限制）", name)
		}
	}

	// Parse args once — reused for action check and handler invocation.
	var args map[string]interface{}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("[error] 参数解析失败: %v", err)
		}
	}

	// Check per-tool action restrictions (e.g., memory save is blocked)
	if action, _ := args["action"].(string); action != "" {
		if isVEToolActionBlocked(name, action) {
			return fmt.Sprintf("[error] 工具 %s 的 %s 操作在数字员工模式下不可用", name, action)
		}
	}

	// VE MCP-only skill execution guard: run_skill is allowed only for
	// skills whose steps are all call_mcp_tool.
	if name == "run_skill" {
		skillName, _ := args["name"].(string)
		if skillName == "" {
			return "[error] 缺少 name 参数"
		}
		allowed, reason := isVERunSkillAllowed(skillName, c.app)
		if !allowed {
			return fmt.Sprintf("[error] 数字员工无法执行此 Skill: %s", reason)
		}
		// Allowed — fall through to registry execution below
	}

	// ---------------------------------------------------------------------------
	// Defense-in-depth path validation for VE file operations.
	// Validation chain (first failure stops execution):
	//   1. Tool blocked check (isVEToolBlocked) — handled above
	//   2. Path parameter check (empty/missing) → "[error] path 参数不能为空"
	//   3. Directory containment check (ValidateVEFilePath / IsWithinAllowedDirs)
	//   4. Sensitive file check (CheckVEPathSensitive) → "[error] 该文件包含敏感信息，无法发送"
	//   5. File size check (> 50 MB, send_file only) → "[error] 文件过大"
	//   6. Actual file read + send → success or OS error
	//
	// Requirements: 4.3, 4.6, 6.3, 6.4, 6.5
	// ---------------------------------------------------------------------------

	switch name {
	case "send_file":
		// Step 2: Path parameter check
		path, _ := args["path"].(string)
		if strings.TrimSpace(path) == "" {
			return "[error] path 参数不能为空"
		}
		// Step 3: Directory containment check
		canonicalPath, err := ValidateVEFilePath(path, allowedDirs)
		if err != nil {
			return err.Error()
		}
		// Step 4: Sensitive file check
		if sensitiveErr := CheckVEPathSensitive(canonicalPath); sensitiveErr != nil {
			return sensitiveErr.Error()
		}
		// Step 5: File size check (50 MB limit for VE mode)
		const veMaxFileSize = 50 * 1024 * 1024 // 50 MB
		info, statErr := os.Stat(canonicalPath)
		if statErr != nil {
			return fmt.Sprintf("[error] 无法读取文件: %v", statErr)
		}
		if info.Size() > veMaxFileSize {
			return fmt.Sprintf("[error] 文件过大（%d bytes），VE 模式最大支持 50 MB", info.Size())
		}
		// Step 6: Delegate to registry handler for actual send
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
		return "[error] send_file 处理器不可用"

	case "read_file":
		// When allowedDirs is configured, enforce directory containment.
		// When allowedDirs is empty, fall through to the original VE read_file
		// handler (veToolReadFile) which only does sensitive file check + size limit.
		// This preserves backward compatibility: VE can always read non-sensitive files.
		if len(allowedDirs) > 0 {
			// Step 2: Path parameter check
			path, _ := args["path"].(string)
			if strings.TrimSpace(path) == "" {
				return "[error] path 参数不能为空"
			}
			// Step 3: Directory containment check
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
			// Step 2: Path parameter check
			path, _ := args["path"].(string)
			if strings.TrimSpace(path) == "" {
				return "[error] path 参数不能为空"
			}
			// Step 3: Directory containment check
			if _, err := IsWithinAllowedDirs(path, allowedDirs); err != nil {
				return err.Error()
			}
		}
		// Delegate to handler (applies its own sensitive dir check when no allowedDirs)
		return executeVERemoteTool(c.app, name, argsJSON)
	}

	// Execute via the main ToolRegistry — uses same handlers as main agent.
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
