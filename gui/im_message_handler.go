package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// imHeartbeatMsg is the sentinel value sent as a progress update to keep the
// Hub-side response timer alive. It must never be delivered to the end user.
const imHeartbeatMsg = "__heartbeat__"

// ---------------------------------------------------------------------------
// IMMessageHandler — handles IM messages forwarded from Hub via WebSocket
// ---------------------------------------------------------------------------

// MessageAttachment represents a file/image/audio attachment from IM.
type MessageAttachment struct {
	Type     string `json:"type"`      // "image", "file", "audio", "video"
	FileName string `json:"file_name"` // Display name
	MimeType string `json:"mime_type"` // MIME type
	Data     string `json:"data"`      // Base64-encoded content
	Size     int64  `json:"size"`      // Original size in bytes
}

// IMUserMessage is the payload of an "im.user_message" from Hub.
type IMUserMessage struct {
	UserID             string              `json:"user_id"`
	Platform           string              `json:"platform"`
	Text               string              `json:"text"`
	Attachments        []MessageAttachment `json:"attachments,omitempty"`          // File/image attachments from user
	MinIterations      int                 `json:"min_iterations,omitempty"`       // floor for agent loop iterations (used by scheduled tasks)
	IsBackground       bool                `json:"is_background,omitempty"`        // true for scheduled tasks / auto-picked tasks (uses separate HTTP client)
	BackgroundSlotKind string              `json:"background_slot_kind,omitempty"` // "coding", "scheduled", "auto" — determines concurrency slot (default: "scheduled")
}

// IMAgentResponse is the structured reply sent back to Hub.
type IMAgentResponse struct {
	Text            string             `json:"text"`
	Fields          []IMResponseField  `json:"fields,omitempty"`
	Actions         []IMResponseAction `json:"actions,omitempty"`
	ImageKey        string             `json:"image_key,omitempty"`
	FileData        string             `json:"file_data,omitempty"`
	FileName        string             `json:"file_name,omitempty"`
	FileMimeType    string             `json:"file_mime_type,omitempty"`
	LocalFilePath   string             `json:"local_file_path,omitempty"`
	LocalFilePaths  []string           `json:"local_file_paths,omitempty"`
	ThumbnailBase64 string             `json:"thumbnail_base64,omitempty"`
	Error           string             `json:"error,omitempty"`
	Deferred        bool               `json:"deferred,omitempty"`
}

// IMResponseField is a key-value field in the agent response.
type IMResponseField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// IMResponseAction is a suggested action in the agent response.
type IMResponseAction struct {
	Label   string `json:"label"`
	Command string `json:"command"`
	Style   string `json:"style"`
}

// ---------------------------------------------------------------------------
// Conversation Memory
// ---------------------------------------------------------------------------



// toolsCacheTTL is the maximum age of the cached tool definitions.
// When MCP_Registry changes, tools are regenerated within this window.
const toolsCacheTTL = 5 * time.Second

// ProgressCallback is called by the agent loop to send intermediate progress
// ProgressCallback — see corelib_aliases.go

// IMMessageHandler processes IM messages using the local LLM Agent.
// It accesses mcpRegistry and skillExecutor via h.app at call time
// (not captured at construction) to handle late initialization.
type IMMessageHandler struct {
	app        *App
	manager    *RemoteSessionManager
	memory     *conversationMemory
	client     *http.Client // chat-priority HTTP client (optimised transport)
	taskClient *http.Client // background-task HTTP client (separate pool)

	// Unified tool registry and dynamic builder (Phase 1 upgrade).
	registry    *ToolRegistry
	toolBuilder *DynamicToolBuilder

	// Security firewall (Phase 2 upgrade).
	firewall *SecurityFirewall

	// Dynamic tool generation and routing (lazily initialized via setters).
	toolDefGen     *ToolDefinitionGenerator
	toolRouter     *ToolRouter
	cachedTools    []map[string]interface{}
	toolsCacheTime time.Time
	toolsMu        sync.RWMutex

	// Capability gap detection (lazily initialized via setter).
	capabilityGapDetector *CapabilityGapDetector

	// Long-term memory store (lazily initialized via setter).
	memoryStore *MemoryStore

	// Session template manager (lazily initialized via setter).
	templateManager *SessionTemplateManager

	// Scheduled task manager (lazily initialized via setter).
	scheduledTaskManager *ScheduledTaskManager

	// Smart session startup components (lazily initialized via setters).
	contextResolver *SessionContextResolver
	sessionPrecheck *SessionPrecheck
	startupFeedback *SessionStartupFeedback

	// Configuration manager (lazily initialized via setter).
	configManager *ConfigManager

	// Dynamic loop limit — set by the "set_max_iterations" tool during an
	// active agent loop. Reset to 0 at the start of each runAgentLoop call.
	// A positive value overrides the configured maxIter for the current loop.
	// NOTE: This field is kept as a legacy bridge alongside currentLoopCtx.
	// Both are kept in sync by toolSetMaxIterations. Will be fully replaced
	// by per-loop LoopContext.MaxIterations once Task 5 routes background
	// loops through bgManager (eliminating shared handler state).
	loopMaxOverride int

	// currentLoopCtx points to the LoopContext of the currently executing
	// runAgentLoop. Used by tools (e.g. set_max_iterations) to interact
	// with the active loop. Set at the start of runAgentLoop, cleared at end.
	currentLoopCtx *LoopContext

	// Background loop manager and session monitor (lazily initialized via setters).
	bgManager      *BackgroundLoopManager
	sessionMonitor *SessionMonitor

	// SSH session manager (lazily initialized on first SSH tool call).
	sshMgr *remote.SSHSessionManager

	// lastUserText stores the most recent user message text for the current
	// agent loop. Used by toolCreateSession to detect non-coding tasks and
	// prevent unnecessary session creation.
	lastUserText string

	// imFileSender is an optional callback that forwards a file to the user's
	// IM channels (Feishu/WeChat/etc.) via the Hub WebSocket. Set by the
	// desktop GUI after connecting to the Hub. When nil, IM forwarding is
	// silently skipped.
	imFileSender func(b64Data, fileName, mimeType, message string) error

	// agentActivity is a process-local shared store that lets the GUI AI
	// assistant and IM channels see each other's active tasks.
	agentActivity *AgentActivityStore

	// lastScreenshotAt records the time of the last successful screenshot
	// to enforce a cooldown period and prevent accidental rapid-fire captures.
	lastScreenshotAt time.Time

	// topicDetector automatically detects topic switches and clears stale
	// conversation context so users don't need to manually /new.
	topicDetector *topicSwitchDetector


	// --- First-layer Harness modules (lazily initialized via setters) ---

	// goalAnchor periodically re-injects the original user goal into the
	// LLM context to prevent drift during long-running agent loops.
	goalAnchor *GoalAnchor

	// driftDetector analyzes recent tool_call sequences to detect loop
	// patterns and trigger re-planning when the agent is stuck.
	driftDetector *DriftDetector

	// harnessProgressTracker maintains a structured task checklist that is
	// injected into the LLM context before each iteration.
	harnessProgressTracker *HarnessProgressTracker

	// adaptiveRetry classifies tool_call failures and decides retry
	// strategy, supplementing the existing isRetryableLLMError logic.
	adaptiveRetry *AdaptiveRetry
}

// NewIMMessageHandler creates a new handler.
func NewIMMessageHandler(app *App, manager *RemoteSessionManager) *IMMessageHandler {
	// Optimised transport for interactive chat — larger connection pool
	// so concurrent requests don't queue behind each other.
	chatTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 180 * time.Second,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		MaxConnsPerHost:     20,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true, // 禁止自动 gzip，避免 SSE 流式被压缩缓冲
	}
	// Separate transport for background tasks (scheduled tasks, auto-picked
	// ClawNet tasks) so they never starve the chat connection pool.
	taskTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 240 * time.Second,
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     10,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
	}

	chatClient := &http.Client{Transport: chatTransport}
	taskClient := &http.Client{Transport: taskTransport}

	h := &IMMessageHandler{
		app:        app,
		manager:    manager,
		memory:     newConversationMemory(),
		client:        chatClient,
		taskClient:    taskClient,
		agentActivity: NewAgentActivityStore(),
	}
	// Initialize ToolRegistry and register builtin tools.
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	// Register non-code tools (Git, file search, health check).
	registerNonCodeTools(h.registry, app)
	// Register browser automation tools (CDP-based).
	registerBrowserTools(h.registry)
	h.toolBuilder = NewDynamicToolBuilder(h.registry)

	// Initialize automatic topic switch detector.
	h.topicDetector = newTopicSwitchDetector(func() (*http.Client, MaclawLLMConfig) {
		return h.client, h.app.GetMaclawLLMConfig()
	})

	return h
}

// SetToolRegistry replaces the tool registry (for testing or late reconfiguration).
func (h *IMMessageHandler) SetToolRegistry(r *ToolRegistry) {
	h.registry = r
	h.toolBuilder = NewDynamicToolBuilder(r)
}

// SetSecurityFirewall configures the security firewall for tool execution checks.
func (h *IMMessageHandler) SetSecurityFirewall(fw *SecurityFirewall) {
	h.firewall = fw
}

// SetToolDefGenerator configures the dynamic tool definition generator.
// When set, it replaces the hardcoded buildToolDefinitions() output.
func (h *IMMessageHandler) SetToolDefGenerator(gen *ToolDefinitionGenerator) {
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.toolDefGen = gen
	// Invalidate cache so next call regenerates.
	h.cachedTools = nil
	h.toolsCacheTime = time.Time{}
}

// SetCapabilityGapDetector configures the capability gap detector.
func (h *IMMessageHandler) SetCapabilityGapDetector(detector *CapabilityGapDetector) {
	h.capabilityGapDetector = detector
}

// SetToolRouter configures the tool router for context-aware tool filtering.
func (h *IMMessageHandler) SetToolRouter(router *ToolRouter) {
	h.toolsMu.Lock()
	defer h.toolsMu.Unlock()
	h.toolRouter = router
	// Wire the registry into the router so it can dynamically resolve
	// builtin tool names and use tags for TF-IDF scoring.
	if router != nil && h.registry != nil {
		router.SetRegistry(h.registry)
	}
}

// SetContextResolver configures the session context resolver for auto-detecting
// project paths and recommending tools.
func (h *IMMessageHandler) SetContextResolver(resolver *SessionContextResolver) {
	h.contextResolver = resolver
}

// SetSessionPrecheck configures the session precheck for environment validation.
func (h *IMMessageHandler) SetSessionPrecheck(precheck *SessionPrecheck) {
	h.sessionPrecheck = precheck
}

// SetStartupFeedback configures the startup feedback monitor.
func (h *IMMessageHandler) SetStartupFeedback(feedback *SessionStartupFeedback) {
	h.startupFeedback = feedback
}

// SetConfigManager configures the configuration manager for config tools.
func (h *IMMessageHandler) SetConfigManager(cm *ConfigManager) {
	h.configManager = cm
}

// SetMemoryStore configures the long-term memory store.
func (h *IMMessageHandler) SetMemoryStore(ms *MemoryStore) {
	h.memoryStore = ms
}

// SetTemplateManager configures the session template manager.
func (h *IMMessageHandler) SetTemplateManager(tm *SessionTemplateManager) {
	h.templateManager = tm
}

// SetScheduledTaskManager configures the scheduled task manager.
func (h *IMMessageHandler) SetScheduledTaskManager(stm *ScheduledTaskManager) {
	h.scheduledTaskManager = stm
}

// SetBackgroundLoopManager configures the background loop manager.
func (h *IMMessageHandler) SetBackgroundLoopManager(blm *BackgroundLoopManager) {
	h.bgManager = blm
}

// SetSessionMonitor configures the session monitor.
func (h *IMMessageHandler) SetSessionMonitor(sm *SessionMonitor) {
	h.sessionMonitor = sm
}

// SetIMFileSender configures the callback used to forward files to the user's
// IM channels (Feishu/WeChat/etc.) when the agent is running on the desktop.
func (h *IMMessageHandler) SetIMFileSender(fn func(b64Data, fileName, mimeType, message string) error) {
	h.imFileSender = fn
}

// SetGoalAnchor configures the goal anchoring module for the agent loop.
func (h *IMMessageHandler) SetGoalAnchor(ga *GoalAnchor) {
	h.goalAnchor = ga
}

// SetDriftDetector configures the drift detection module for the agent loop.
func (h *IMMessageHandler) SetDriftDetector(dd *DriftDetector) {
	h.driftDetector = dd
}

// SetHarnessProgressTracker configures the progress tracking module for the agent loop.
func (h *IMMessageHandler) SetHarnessProgressTracker(pt *HarnessProgressTracker) {
	h.harnessProgressTracker = pt
}

// SetAdaptiveRetry configures the adaptive retry module for the agent loop.
func (h *IMMessageHandler) SetAdaptiveRetry(ar *AdaptiveRetry) {
	h.adaptiveRetry = ar
}

// getTools returns the current tool definitions, using the generator with
// a 5-second cache when configured, falling back to buildToolDefinitions().
func (h *IMMessageHandler) getTools() []map[string]interface{} {
	var tools []map[string]interface{}

	// --- Phase 1 upgrade: prefer DynamicToolBuilder from ToolRegistry ---
	// Note: We use BuildAll() here intentionally — context-aware filtering
	// is handled downstream by routeTools() / ToolRouter which uses TF-IDF.
	// DynamicToolBuilder.Build(msg) is an alternative path for simpler setups
	// without ToolRouter.
	if h.toolBuilder != nil && h.registry != nil {
		h.toolsMu.RLock()
		cached := h.cachedTools
		cacheTime := h.toolsCacheTime
		h.toolsMu.RUnlock()

		if cached != nil && time.Since(cacheTime) < toolsCacheTTL {
			tools = cached
		} else {
			// Sync dynamic tools (ClawNet) only on cache rebuild, not every call.
			h.syncClawNetTools()

			tools = h.toolBuilder.BuildAll()

			h.toolsMu.Lock()
			h.cachedTools = tools
			h.toolsCacheTime = time.Now()
			h.toolsMu.Unlock()
		}
	} else {
		// --- Legacy path: ToolDefinitionGenerator or hardcoded ---
		h.toolsMu.RLock()
		gen := h.toolDefGen
		cached := h.cachedTools
		cacheTime := h.toolsCacheTime
		h.toolsMu.RUnlock()

		// Fallback: no generator configured — use hardcoded definitions.
		if gen == nil {
			tools = h.buildToolDefinitions()
		} else if cached != nil && time.Since(cacheTime) < toolsCacheTTL {
			// Return cached tools if still fresh (within 5 seconds).
			tools = cached
		} else {
			// Regenerate from the generator.
			tools = gen.Generate()

			h.toolsMu.Lock()
			h.cachedTools = tools
			h.toolsCacheTime = time.Now()
			h.toolsMu.Unlock()
		}
	}

	// In lite/simple mode (UIMode != "pro"), filter out coding session tools
	// since the user has not configured coding LLM providers. This removes
	// the tool definitions entirely so they are never sent to the LLM,
	// saving tokens and preventing the agent from attempting coding sessions.
	if !h.app.isProMode() {
		tools = filterCodingTools(tools)
	}

	return tools
}

// routeTools applies the ToolRouter to filter tools based on user message.
// If no router is configured, returns allTools unchanged.
func (h *IMMessageHandler) routeTools(userMessage string, allTools []map[string]interface{}) []map[string]interface{} {
	h.toolsMu.RLock()
	router := h.toolRouter
	h.toolsMu.RUnlock()

	if router == nil {
		return allTools
	}
	return router.Route(userMessage, allTools)
}

// syncClawNetTools dynamically registers or unregisters ClawNet tools
// based on whether the ClawNet daemon is currently running.
func (h *IMMessageHandler) syncClawNetTools() {
	if h.registry == nil {
		return
	}
	running := h.app.clawNetClient != nil && h.app.clawNetClient.IsRunning()
	_, hasSearch := h.registry.Get("clawnet_search")

	if running && !hasSearch {
		h.registry.Register(RegisteredTool{
			Name:        "clawnet_search",
			Description: "在虾网（ClawNet P2P 知识网络）中搜索知识条目。返回匹配的知识列表，包含标题、内容、作者等。",
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"clawnet", "search", "knowledge", "p2p"},
			Status:      RegToolAvailable,
			InputSchema: map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "搜索关键词"},
			},
			Required: []string{"query"},
			Source:   "clawnet",
			Handler:  func(args map[string]interface{}) string { return h.toolClawNetSearch(args) },
		})
		h.registry.Register(RegisteredTool{
			Name:        "clawnet_publish",
			Description: "向虾网（ClawNet P2P 知识网络）发布一条知识条目。发布后其他节点可以搜索到。",
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"clawnet", "publish", "knowledge", "p2p"},
			Status:      RegToolAvailable,
			InputSchema: map[string]interface{}{
				"title": map[string]string{"type": "string", "description": "知识标题"},
				"body":  map[string]string{"type": "string", "description": "知识内容（Markdown 格式）"},
			},
			Required: []string{"title", "body"},
			Source:   "clawnet",
			Handler:  func(args map[string]interface{}) string { return h.toolClawNetPublish(args) },
		})
	} else if !running && hasSearch {
		h.registry.Unregister("clawnet_search")
		h.registry.Unregister("clawnet_publish")
	}
}

// HandleIMMessage processes an IM user message and returns the Agent's response.
func (h *IMMessageHandler) HandleIMMessage(msg IMUserMessage) *IMAgentResponse {
	return h.HandleIMMessageWithProgress(msg, nil)
}

// HandleIMMessageWithProgress processes an IM message with an optional progress
// callback. When onProgress is non-nil, the agent loop sends intermediate status
// updates (e.g. "正在执行 bash 命令…") so the Hub can relay them to the user
// and reset the response timeout — preventing 504 on long-running tasks.
func (h *IMMessageHandler) HandleIMMessageWithProgress(msg IMUserMessage, onProgress ProgressCallback) *IMAgentResponse {
	return h.HandleIMMessageWithProgressAndStream(msg, onProgress, nil, nil, nil)
}

// HandleIMMessageWithProgressAndStream extends HandleIMMessageWithProgress with
// streaming support for the desktop AI assistant. When onToken is non-nil, each
// LLM text delta is pushed in real-time. When onNewRound is non-nil, it is called
// at the start of each new agent loop iteration (after the first) so the frontend
// can create a new message bubble. IM platforms pass nil for both.
func (h *IMMessageHandler) HandleIMMessageWithProgressAndStream(msg IMUserMessage, onProgress ProgressCallback, onToken TokenCallback, onNewRound NewRoundCallback, onStreamDone StreamDoneCallback) *IMAgentResponse {
	trimmed := strings.TrimSpace(msg.Text)

	// Slash commands are processed before the LLM config check — they don't
	// need LLM and must always work so users can manage state even when LLM
	// is misconfigured.
	if trimmed == "/new" || trimmed == "/reset" || trimmed == "/clear" {
		h.memory.clear(msg.UserID)
		return &IMAgentResponse{Text: "对话已重置。"}
	}
	if trimmed == "/exit" || trimmed == "/quit" {
		return h.handleExitCommand(msg.UserID)
	}
	if trimmed == "/sessions" || trimmed == "/status" {
		return h.handleSessionsCommand()
	}
	if trimmed == "/help" {
		return &IMAgentResponse{Text: "📖 可用命令:\n" +
			"/new /reset — 重置对话\n" +
			"/exit /quit — 终止所有会话，退出编程模式\n" +
			"/sessions — 查看当前会话状态\n" +
			"/help — 显示此帮助"}
	}

	if !h.app.isMaclawLLMConfigured() {
		return &IMAgentResponse{
			Error: "MaClaw LLM 未配置，无法处理请求。请在 MaClaw 客户端的设置中配置 LLM。",
		}
	}

	// Select HTTP client: background tasks use a separate connection pool
	// so they never block interactive chat requests.
	httpClient := h.client
	if msg.IsBackground {
		httpClient = h.taskClient
	}

	// --- Automatic topic switch detection ---
	// For interactive (non-background) messages, detect if the user has
	// switched topics and auto-clear stale conversation context.
	if !msg.IsBackground && h.topicDetector != nil {
		if h.topicDetector.detect(trimmed, msg.UserID, h.memory) == TopicNew {
			// Archive a one-line summary before clearing.
			if h.memoryStore != nil {
				entries := h.memory.load(msg.UserID)
				if summary := buildQuickSummary(entries); summary != "" {
					_ = h.memoryStore.Save(MemoryEntry{
						Content:  summary,
						Category: "conversation_summary",
					})
				}
			}
			h.memory.clear(msg.UserID)
			log.Printf("[TopicDetector] auto-cleared context for user %s", msg.UserID)
		}
	}

	// --- Background routing: delegate to BackgroundLoopManager ---
	if msg.IsBackground && h.bgManager != nil {
		slotKind := parseSlotKind(msg.BackgroundSlotKind)
		maxIter := h.app.GetMaclawAgentMaxIterations()
		if msg.MinIterations > maxIter {
			maxIter = msg.MinIterations
		}

		loopCtx, waitC := h.bgManager.SpawnOrQueue(slotKind, msg.UserID, msg.Text, maxIter)
		if loopCtx == nil && waitC != nil {
			// Slot full — block until a slot opens.
			loopCtx = <-waitC
		}
		if loopCtx == nil {
			return &IMAgentResponse{Error: "后台任务启动失败：无法获取执行槽位"}
		}
		loopCtx.HTTPClient = httpClient

		var systemPrompt string
		history := h.memory.load(msg.UserID)
		history = h.compactHistory(history, httpClient)
		if h.memoryStore != nil {
			systemPrompt = h.buildSystemPromptWithMemory(msg.Text, len(history) == 0)
		} else {
			systemPrompt = h.buildSystemPrompt()
		}

		result := h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, msg.Text, msg.Attachments, onProgress, nil, nil, msg.MinIterations, msg.Platform)

		// Mark loop as completed/failed and dequeue next.
		if result != nil && result.Error != "" {
			loopCtx.SetState("failed")
		} else {
			loopCtx.SetState("completed")
		}
		h.bgManager.Complete(loopCtx.ID)
		return result
	}

	history := h.memory.load(msg.UserID)
	history = h.compactHistory(history, httpClient)
	var systemPrompt string
	if h.memoryStore != nil {
		systemPrompt = h.buildSystemPromptWithMemory(msg.Text, len(history) == 0)
	} else {
		systemPrompt = h.buildSystemPrompt()
	}

	// Create a LoopContext for this chat loop.
	loopCtx := NewLoopContext("chat", h.app.GetMaclawAgentMaxIterations(), httpClient)
	// Wire the bgManager's statusC so the chat loop can drain background events.
	if h.bgManager != nil {
		loopCtx.StatusC = h.bgManager.statusC
	}
	return h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, msg.Text, msg.Attachments, onProgress, onToken, onNewRound, msg.MinIterations, msg.Platform)
}

// handleExitCommand terminates all active sessions, resets conversation
// memory, and returns the user to normal chat mode.
func (h *IMMessageHandler) handleExitCommand(userID string) *IMAgentResponse {
	var killed []string
	var failCount int
	if h.manager != nil {
		for _, s := range h.manager.List() {
			s.mu.RLock()
			active := isActiveRemoteSessionStatus(s.Status)
			sid := s.ID
			tool := s.Tool
			s.mu.RUnlock()
			if active {
				if err := h.manager.Kill(sid); err == nil {
					killed = append(killed, fmt.Sprintf("%s(%s)", sid, tool))
				} else {
					failCount++
				}
			}
		}
	}
	h.memory.clear(userID)

	var b strings.Builder
	if len(killed) > 0 {
		b.WriteString(fmt.Sprintf("已退出编程模式。终止了 %d 个会话: %s", len(killed), strings.Join(killed, ", ")))
	} else {
		b.WriteString("已退出编程模式。")
	}
	if failCount > 0 {
		b.WriteString(fmt.Sprintf("\n⚠️ %d 个会话终止失败，可能需要手动处理。", failCount))
	}
	b.WriteString("\n对话已重置，后续消息将正常对话。")
	return &IMAgentResponse{Text: b.String()}
}

// handleSessionsCommand returns a quick status summary of active sessions.
func (h *IMMessageHandler) handleSessionsCommand() *IMAgentResponse {
	if h.manager == nil {
		return &IMAgentResponse{Text: "会话管理器未初始化。"}
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return &IMAgentResponse{
			Text: "当前没有活跃会话。\n\n💡 提示: 发送 /exit 可退出编程模式回到普通对话。",
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("📋 当前 %d 个会话:\n", len(sessions)))
	for _, s := range sessions {
		s.mu.RLock()
		status := string(s.Status)
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("• [%s] %s — %s", s.ID, s.Tool, status))
		if task != "" {
			b.WriteString(fmt.Sprintf(" | %s", task))
		}
		if waiting {
			b.WriteString(" ⏳等待输入")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n💡 发送 /exit 可终止所有会话并退出编程模式。")
	return &IMAgentResponse{Text: b.String()}
}

// compactHistory summarizes old conversation turns to stay within token limits.
func (h *IMMessageHandler) compactHistory(entries []conversationEntry, httpClient *http.Client) []conversationEntry {
	if estimateConversationEntryTokens(entries) < maxMemoryTokenEstimate {
		return entries
	}
	split := len(entries) / 2
	// Adjust split point forward so we don't cut in the middle of a
	// tool-call group (assistant+tool_calls followed by tool results).
	// The "recent" slice must not start with orphaned role:"tool" entries.
	for split < len(entries) && entries[split].Role == "tool" {
		split++
	}
	if split >= len(entries) {
		// Degenerate case: everything is tool messages — just return as-is.
		return entries
	}
	recent := entries[split:]

	var sb strings.Builder
	for _, e := range entries[:split] {
		data, _ := json.Marshal(e)
		sb.Write(data)
		sb.WriteByte('\n')
	}
	summaryText := sb.String()
	if len(summaryText) > 32000 {
		summaryText = summaryText[:32000] + "\n...(truncated)"
	}

	cfg := h.app.GetMaclawLLMConfig()
	msgs := []map[string]string{
		{"role": "user", "content": "请简洁总结以下对话历史，保留关键事实、决策和待办事项：\n\n" + summaryText},
	}
	conv := make([]interface{}, len(msgs))
	for i, m := range msgs {
		conv[i] = m
	}
	resp, err := h.doLLMRequest(cfg, conv, nil, httpClient)
	if err != nil || len(resp.Choices) == 0 {
		return recent
	}

	compacted := []conversationEntry{
		{Role: "user", Content: "[对话历史摘要]\n" + resp.Choices[0].Message.Content},
		{Role: "assistant", Content: "好的，我已了解之前的对话上下文。"},
	}
	return append(compacted, recent...)
}

// ---------------------------------------------------------------------------
// LLM types and HTTP client
// ---------------------------------------------------------------------------

// parseSlotKind converts a string slot kind to the SlotKind enum.
// Defaults to SlotKindScheduled for unknown values.
func parseSlotKind(s string) SlotKind {
	switch s {
	case "coding":
		return SlotKindCoding
	case "scheduled", "":
		return SlotKindScheduled
	case "auto":
		return SlotKindAuto
	default:
		return SlotKindScheduled
	}
}

// drainStatusEvents non-blockingly drains all pending StatusEvents from the
// LoopContext's StatusC channel, injecting each as a system message into the
// conversation and forwarding to the user via sendProgress.
func drainStatusEvents(ctx *LoopContext, conversation *[]interface{}, sendProgress func(string)) {
	for {
		select {
		case evt := <-ctx.StatusC:
			statusMsg := fmt.Sprintf("[后台事件] %s", evt.Message)
			*conversation = append(*conversation, map[string]string{
				"role": "system", "content": statusMsg,
			})
			sendProgress(fmt.Sprintf("📡 %s", evt.Message))
		default:
			return
		}
	}
}

func (h *IMMessageHandler) runAgentLoop(ctx *LoopContext, userID, systemPrompt string, history []conversationEntry, userText string, attachments []MessageAttachment, onProgress ProgressCallback, onToken TokenCallback, onNewRound NewRoundCallback, minIterations int, platform string) (result *IMAgentResponse) {
	// panic recovery — 防止工具执行异常导致 goroutine 崩溃
	defer func() {
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Agent 内部错误: %v", r)}
		}
	}()

	// Wire the loop context so tools can access it.
	h.currentLoopCtx = ctx
	h.lastUserText = userText
	ctx.Platform = platform
	defer func() { h.currentLoopCtx = nil; h.lastUserText = "" }()

	// --- Initialize first-layer Harness modules (optional, nil-safe) ---
	var loopGoalAnchor *GoalAnchor
	if h.goalAnchor != nil {
		loopGoalAnchor = h.goalAnchor
	} else {
		loopGoalAnchor = NewGoalAnchor(userText, 5)
	}

	var loopDriftDetector *DriftDetector
	if h.driftDetector != nil {
		loopDriftDetector = h.driftDetector
	}

	var loopProgressTracker *HarnessProgressTracker
	if h.harnessProgressTracker != nil {
		loopProgressTracker = h.harnessProgressTracker
	}

	var loopAdaptiveRetry *AdaptiveRetry
	if h.adaptiveRetry != nil {
		loopAdaptiveRetry = h.adaptiveRetry
	}

	// Helper to send progress if callback is set.
	sendProgress := func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	}

	// isDebug reads the debug toggle live from config so changes take effect
	// immediately — even mid-loop when the user flips the switch.
	// Cached for up to 2 seconds to avoid excessive disk reads in the hot loop.
	var cachedDebug bool
	var cachedDebugTime time.Time
	isDebug := func() bool {
		if now := time.Now(); now.Sub(cachedDebugTime) > 2*time.Second {
			c, err := h.app.LoadConfig()
			if err != nil {
				cachedDebug = false
			} else {
				cachedDebug = c.MaclawDebugToolCalls
			}
			cachedDebugTime = now
		}
		return cachedDebug
	}

	// sendToolProgress sends tool-execution progress only when debug is on.
	sendToolProgress := func(text string) {
		if isDebug() {
			sendProgress(text)
		}
	}

	// Delayed acknowledgment: when debug is off and streaming is not active,
	// schedule a brief receipt after a short grace period. If the agent loop
	// finishes quickly (e.g. simple greetings), the receipt is suppressed —
	// the user sees only the final card, avoiding the redundant "收到，正在处理中" message.
	// When streaming (onToken != nil), the user already sees real-time output,
	// so the acknowledgment is unnecessary.
	const ackDelay = 3 * time.Second
	ackDone := make(chan struct{})
	if !isDebug() && onToken == nil {
		ackTimer := time.NewTimer(ackDelay)
		go func() {
			select {
			case <-ackTimer.C:
				sendProgress("📨 收到，正在处理中，稍后发你结果…")
			case <-ackDone:
				ackTimer.Stop()
			}
		}()
	}
	// Ensure the delayed ack goroutine is cancelled when the loop returns.
	defer close(ackDone)

	// Heartbeat: send a silent keepalive every 60s so the Hub-side response
	// timer (180s) is continuously reset during long LLM calls or tool
	// executions. The Hub recognises the exact string and resets the timer
	// without forwarding the message to the user.
	const heartbeatInterval = 60 * time.Second
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendProgress(imHeartbeatMsg)
			case <-heartbeatDone:
				return
			}
		}
	}()
	defer close(heartbeatDone)

	cfg := h.app.GetMaclawLLMConfig()
	maxIter := h.app.GetMaclawAgentMaxIterations()
	h.loopMaxOverride = 0 // reset dynamic override for this loop
	// Sync initial maxIter into the LoopContext so ctx is the source of truth.
	if ctx.MaxIterations() <= 0 {
		ctx.SetMaxIterations(maxIter)
	}
	allTools := h.getTools()
	tools := h.routeTools(userText, allTools)
	toolsTokenBudget := estimateToolsTokens(tools)
	httpClient := ctx.HTTPClient

	// Cross-channel activity: report this loop so the other channel can see it.
	activitySource := "im"
	if platform == "desktop" {
		activitySource = "gui"
	}
	reportActivity := func(iter, maxI int, summary string) {
		task := userText
		if len(task) > 100 {
			task = task[:100]
		}
		if len(summary) > 120 {
			summary = summary[:120]
		}
		h.agentActivity.Update(&AgentActivity{
			Source:      activitySource,
			Task:        task,
			Iteration:   iter,
			MaxIter:     maxI,
			LastSummary: summary,
		})
	}
	reportActivity(0, maxIter, "")
	defer h.agentActivity.Clear(activitySource)

	// Inject cross-channel activity awareness into the system prompt.
	if extra := h.agentActivity.FormatForPrompt(activitySource); extra != "" {
		systemPrompt += extra
	}

	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, entry := range history {
		conversation = append(conversation, entry.toMessage())
	}

	// Build user message — multimodal if attachments contain images.
	userContent := buildUserContent(userText, attachments, cfg.Protocol, cfg.SupportsVision)
	conversation = append(conversation, map[string]interface{}{"role": "user", "content": userContent})

	history = append(history, conversationEntry{Role: "user", Content: userContent})

	// maxIter defaults to 300 (MaxAgentIterationsCap).
	// We still enforce a hard safety cap to prevent runaway loops.
	effectiveMax := maxIter
	if effectiveMax <= 0 {
		effectiveMax = maxAgentIterationsCap
	}
	if effectiveMax < minAgentIterations {
		effectiveMax = minAgentIterations
	}
	// Apply minimum iterations floor (e.g. scheduled tasks need more rounds).
	if minIterations > 0 && effectiveMax < minIterations {
		effectiveMax = minIterations
		if effectiveMax > maxAgentIterationsCap {
			effectiveMax = maxAgentIterationsCap
		}
	}

	for iteration := 0; ; iteration++ {
		ctx.SetIteration(iteration)

		// --- Check dynamic override from set_max_iterations tool ---
		// Both loopMaxOverride (legacy) and ctx.MaxIterations() are kept in
		// sync by toolSetMaxIterations. Read from ctx as source of truth.
		if h.loopMaxOverride > 0 {
			override := h.loopMaxOverride
			if minIterations > 0 && override < minIterations {
				override = minIterations
			}
			effectiveMax = override
			// Keep ctx in sync.
			ctx.SetMaxIterations(effectiveMax)
		} else {
			// ctx may have been updated externally (e.g. by ContinueC).
			if cm := ctx.MaxIterations(); cm > 0 && cm != effectiveMax {
				effectiveMax = cm
			}
		}

		// --- Background loop: pause near limit, wait for 续命 ---
		// Only pause if: (a) background loop, (b) effectiveMax > 4 to ensure
		// meaningful work before first pause, (c) iteration is at the pause
		// threshold. The threshold is effectiveMax-2 to give 2 remaining rounds
		// for graceful wrap-up after resume.
		if ctx.Kind == LoopKindBackground && effectiveMax > 4 && iteration == effectiveMax-2 {
			ctx.SetState("paused")
			// Notify via StatusC that we're approaching the limit.
			if ctx.StatusC != nil {
				select {
				case ctx.StatusC <- StatusEvent{
					Type:      StatusEventApproachingLimit,
					LoopID:    ctx.ID,
					SessionID: ctx.SessionID,
					Message:   fmt.Sprintf("后台任务 %s 即将达到最大轮数 (%d/%d)", ctx.ID, iteration, effectiveMax),
					Remaining: effectiveMax - iteration,
				}:
				default:
				}
			}
			// Wait for continue signal, cancel, or timeout (5 min).
			select {
			case extra := <-ctx.ContinueC:
				ctx.AddMaxIterations(extra)
				effectiveMax = ctx.MaxIterations()
				ctx.SetState("running")
			case <-ctx.CancelC:
				ctx.SetState("stopped")
				return &IMAgentResponse{Text: fmt.Sprintf("后台任务 %s 已被停止。", ctx.ID)}
			case <-time.After(5 * time.Minute):
				ctx.SetState("timeout")
				return &IMAgentResponse{Text: fmt.Sprintf("后台任务 %s 等待续命超时，已自动结束。", ctx.ID)}
			}
		}

		// --- Normal iteration limit check ---
		if iteration >= effectiveMax {
			break
		}

		// --- Chat loop: drain StatusC events before LLM call ---
		if ctx.Kind == LoopKindChat && ctx.StatusC != nil {
			drainStatusEvents(ctx, &conversation, sendProgress)
		}

		// --- Check cancellation ---
		if ctx.IsCancelled() {
			ctx.SetState("stopped")
			break
		}

		if iteration > 0 {
			if isDebug() {
				if maxIter > 0 || h.loopMaxOverride > 0 {
					sendProgress(fmt.Sprintf("🔄 Agent 推理中（第 %d/%d 轮）…", iteration+1, effectiveMax))
				} else {
					sendProgress(fmt.Sprintf("🔄 Agent 推理中（第 %d 轮）…", iteration+1))
				}
			} else if onToken == nil && (iteration == 3 || (iteration > 3 && iteration%5 == 0)) {
				// Non-debug, non-streaming mode: send a patience hint at iteration 4,
				// then every 5 rounds so the user knows a long task is still alive.
				// When streaming, the user already sees real-time output.
				sendProgress("⏳ 任务较复杂，正在耐心处理中，稍后发你结果…")
			}
		}
		conversation = trimConversation(conversation, cfg.EffectiveContextTokens(), toolsTokenBudget, makeSummarizer(cfg, httpClient))

		// --- Harness: inject GoalAnchor content ---
		if loopGoalAnchor != nil && loopGoalAnchor.ShouldAnchor(iteration) {
			var progressSummary string
			if loopProgressTracker != nil {
				progressSummary = loopProgressTracker.Summary()
			} else {
				progressSummary = fmt.Sprintf("迭代 %d/%d", iteration, effectiveMax)
			}
			anchorContent := loopGoalAnchor.BuildAnchorContent(progressSummary)
			conversation = append(conversation, map[string]string{
				"role": "system", "content": anchorContent,
			})
		}

		// --- Harness: inject ProgressTracker checklist ---
		if loopProgressTracker != nil {
			if checklist := loopProgressTracker.BuildChecklistContent(); checklist != "" {
				conversation = append(conversation, map[string]string{
					"role": "system", "content": "[📋 任务清单]\n" + checklist + "\n[/任务清单]",
				})
			}
		}

		// Notify frontend of new round (for streaming UI) — skip first iteration
		// since the frontend already created a placeholder message.
		if onNewRound != nil && iteration > 0 {
			onNewRound()
		}
		resp, err := h.doLLMRequestStream(cfg, conversation, tools, httpClient, onToken)
		// Retry on timeout / temporary network errors.
		// When AdaptiveRetry is available, use it for smarter classification;
		// otherwise fall back to the existing isRetryableLLMError logic.
		if err != nil {
			if loopAdaptiveRetry != nil {
				category := loopAdaptiveRetry.Classify("llm_request", err)
				decision := loopAdaptiveRetry.Decide("llm_request", category, 0)
				loopAdaptiveRetry.RecordFailure("llm_request", category, decision)
				if decision.Action == "retry" {
					log.Printf("[LLM] AdaptiveRetry: %s 错误，%v 后重试: %v", string(category), decision.Delay, err)
					time.Sleep(decision.Delay)
					resp, err = h.doLLMRequestStream(cfg, conversation, tools, httpClient, onToken)
				}
			} else if isRetryableLLMError(err) {
				log.Printf("[LLM] 首次请求超时/网络错误，2s 后重试: %v", err)
				time.Sleep(2 * time.Second)
				resp, err = h.doLLMRequestStream(cfg, conversation, tools, httpClient, onToken)
			}
		}
		// Accumulate token usage stats
		if resp != nil {
			var input, output int
			if resp.Usage != nil {
				u := resp.Usage
				input = u.PromptTokens
				output = u.CompletionTokens
				if input == 0 && u.InputTokens > 0 {
					input = u.InputTokens
				}
				if output == 0 && u.OutputTokens > 0 {
					output = u.OutputTokens
				}
			} else {
				// Fallback: estimate tokens when provider doesn't return usage in streaming mode.
				input = estimateConversationTokens(conversation)
				if len(resp.Choices) > 0 {
					output = estimateBytesToTokens([]byte(resp.Choices[0].Message.Content))
				}
			}
			h.app.AccumulateLLMTokenUsage(h.app.GetMaclawLLMProviders().Current, input, output)
		}
		if err != nil {
			return &IMAgentResponse{Error: fmt.Sprintf("LLM 调用失败: %s [url=%s model=%s protocol=%s]", err.Error(), cfg.URL, cfg.Model, cfg.Protocol)}
		}
		if len(resp.Choices) == 0 {
			return &IMAgentResponse{Error: "LLM 未返回有效回复"}
		}

		choice := resp.Choices[0]

		// Kimi's kimi-for-coding puts all output in reasoning_content with empty content.
		// Promote reasoning to content so the assistant message is never empty.
		msgContent := choice.Message.Content
		msgReasoning := choice.Message.ReasoningContent
		if msgContent == "" && msgReasoning != "" {
			msgContent = msgReasoning
		}

		assistantMsg := map[string]interface{}{
			"role":    "assistant",
			"content": msgContent,
		}
		if msgReasoning != "" {
			assistantMsg["reasoning_content"] = msgReasoning
		}
		if len(choice.Message.ToolCalls) > 0 {
			assistantMsg["tool_calls"] = choice.Message.ToolCalls
		}
		conversation = append(conversation, assistantMsg)

		// Update cross-channel activity every 5 iterations.
		if iteration%5 == 0 {
			reportActivity(iteration, effectiveMax, msgContent)
		}

		historyEntry := conversationEntry{Role: "assistant", Content: msgContent, ReasoningContent: msgReasoning}
		if len(choice.Message.ToolCalls) > 0 {
			historyEntry.ToolCalls = choice.Message.ToolCalls
		}
		history = append(history, historyEntry)

		// No tool calls → final response.
		// NOTE: Some LLM providers (e.g. DeepSeek, Qwen) return finish_reason="stop"
		// even when tool_calls are present. We must check tool_calls first and only
		// treat the response as final when there are genuinely no tool calls.
		if len(choice.Message.ToolCalls) == 0 {
			// Check for capability gap before returning.
			if h.capabilityGapDetector != nil && h.capabilityGapDetector.Detect(msgContent) {
				skillName, result, err := h.capabilityGapDetector.Resolve(
					context.Background(), userText, nil,
					func(status string) {
						// Status updates are logged but not sent to user in this context.
					},
				)
				if skillName != "" && err == nil {
					finalText := fmt.Sprintf("✅ 已自动安装并执行 Skill「%s」\n%s", skillName, result)
					h.memory.save(userID, trimHistory(history))
					return &IMAgentResponse{Text: stripThinkingTags(finalText)}
				}
			}
			h.memory.save(userID, trimHistory(history))
			return &IMAgentResponse{Text: stripThinkingTags(msgContent)}
		}

		// Execute tool calls and feed results back.
		var pendingImageKey string
		type pendingFile struct {
			name, mimeType, data string
			forwardIM            bool
			message              string // IM delivery prompt
		}
		var pendingFiles []pendingFile
		screenshotAlreadySent := false
		for _, tc := range choice.Message.ToolCalls {
			sendToolProgress(fmt.Sprintf("⚙️ 正在执行工具: %s", tc.Function.Name))
			// When debug is off, suppress intermediate progress from tool execution too.
			toolOnProgress := onProgress
			if !isDebug() {
				toolOnProgress = nil
			}
			result := h.executeTool(tc.Function.Name, tc.Function.Arguments, toolOnProgress)

			// Intercept direct screenshot results: extract base64 image data
			// so it can be delivered via IM image channel instead of text.
			toolContent := result
			if strings.HasPrefix(result, "[screenshot_base64]") {
				pendingImageKey = strings.TrimPrefix(result, "[screenshot_base64]")
				toolContent = "截图已成功捕获，将作为图片发送给用户。"
			}

			// Intercept session-based screenshot: image was already pushed
			// via session.image WebSocket channel, so we just need to stop
			// the agent loop — no image data to carry in the response.
			if result == "[screenshot_sent]" {
				screenshotAlreadySent = true
				toolContent = "截图已成功捕获并发送给用户。"
			}

			// Intercept file send results: collect ALL files (not just the last one).
			// Format: [file_base64|filename|mimetype]data
			//     or: [file_base64|filename|mimetype|im]data  (forward to IM)
			//     or: [file_base64|filename|mimetype|im|msg:提示信息]data
			if strings.HasPrefix(result, "[file_base64|") {
				rest := strings.TrimPrefix(result, "[file_base64|")
				if closeBracket := strings.Index(rest, "]"); closeBracket > 0 {
					meta := rest[:closeBracket]
					parts := strings.Split(meta, "|")
					if len(parts) >= 2 {
						fwd := false
						mType := parts[1]
						var fileMsg string
						// Scan remaining segments for flags.
						for i := 2; i < len(parts); i++ {
							seg := parts[i]
							if seg == "im" {
								fwd = true
							} else if strings.HasPrefix(seg, "msg:") {
								fileMsg = strings.TrimPrefix(seg, "msg:")
							} else {
								// Unknown segment — append to mimeType for safety.
								mType += "|" + seg
							}
						}
						// Fallback: auto-generate prompt based on filename if none provided.
						if fwd && fileMsg == "" {
							fileMsg = inferFileDeliveryMessage(parts[0])
						}
						pendingFiles = append(pendingFiles, pendingFile{
							name:      parts[0],
							mimeType:  mType,
							data:      rest[closeBracket+1:],
							forwardIM: fwd,
							message:   fileMsg,
						})
						if fwd {
							toolContent = fmt.Sprintf("文件 %s 已准备好，将通过 IM 通道发送给用户。", parts[0])
						} else {
							toolContent = fmt.Sprintf("文件 %s 已准备好，将发送给用户。", parts[0])
						}
					}
				}
			}

			truncated := truncateToolResultForTool(tc.Function.Name, toolContent)
			conversation = append(conversation, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      truncated,
			})
			history = append(history, conversationEntry{
				Role: "tool", Content: truncated, ToolCallID: tc.ID,
			})

			// --- Harness: DriftDetector — record tool_call and check for drift ---
			if loopDriftDetector != nil {
				argsHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tc.Function.Arguments)))
				loopDriftDetector.Record(ToolCallRecord{
					ToolName:  tc.Function.Name,
					ArgsHash:  argsHash,
					Timestamp: time.Now(),
				})
				driftResult := loopDriftDetector.DetectDrift()
				if driftResult.Drifted {
					log.Printf("[Harness] 漂移检测触发: pattern=%s needHuman=%v", driftResult.Pattern, driftResult.NeedHumanHelp)
					conversation = append(conversation, map[string]string{
						"role": "system", "content": driftResult.ReplanPrompt,
					})
					loopDriftDetector.ResetWindow()
					if driftResult.NeedHumanHelp {
						h.memory.save(userID, trimHistory(history))
						return &IMAgentResponse{
							Text: "⚠️ Agent 检测到重复漂移模式，需要人工介入。请检查当前任务状态并提供新的指示。",
						}
					}
				}
			}
		}

		// If a direct screenshot was captured, return it immediately as an image response.
		if pendingImageKey != "" {
			h.memory.save(userID, trimHistory(history))
			// Desktop platform: save to local file and return path + thumbnail
			if platform == "desktop" {
				filePath, err := h.saveScreenshotToFile(pendingImageKey)
				if err != nil {
					return &IMAgentResponse{Text: fmt.Sprintf("📷 截图已捕获，但保存文件失败: %s", err.Error())}
				}
				// Generate a small thumbnail (reuse the base64 data, frontend will size it)
				thumb := pendingImageKey
				// Cap thumbnail data to keep the JSON response lean
				if len(thumb) > 50000 {
					if downsized, err := downsizeScreenshotBase64(thumb, 10000); err == nil {
						thumb = downsized
					}
				}
				return &IMAgentResponse{
					Text:            "📷 截图已保存",
					LocalFilePath:   filePath,
					ThumbnailBase64: thumb,
				}
			}
			return &IMAgentResponse{
				Text:     "",
				ImageKey: pendingImageKey,
			}
		}

		// If screenshot was already delivered via session.image channel,
		// stop the loop immediately — no further agent reasoning needed.
		if screenshotAlreadySent {
			h.memory.save(userID, trimHistory(history))
			return &IMAgentResponse{Text: "📷 截图已发送"}
		}

		// If file(s) were prepared, return them for delivery.
		if len(pendingFiles) > 0 {
			h.memory.save(userID, trimHistory(history))
			// Desktop platform: save all files locally and return paths
			if platform == "desktop" {
				var savedPaths []string
				var failLines []string
				var imForwardedCount int
				for _, pf := range pendingFiles {
					filePath, err := h.saveFileDataToLocal(pf.name, pf.data)
					if err != nil {
						failLines = append(failLines, fmt.Sprintf("📄 %s 保存失败: %s", pf.name, err.Error()))
						continue
					}
					savedPaths = append(savedPaths, filePath)

					// Forward to IM channels if requested and sender is configured.
					if pf.forwardIM {
						if h.imFileSender == nil {
							failLines = append(failLines, fmt.Sprintf("📄 %s 已保存到本地，但未连接到 Hub，无法转发到 IM", pf.name))
						} else if err := h.imFileSender(pf.data, pf.name, pf.mimeType, pf.message); err != nil {
							log.Printf("[IMMessageHandler] IM forward failed for %s: %v", pf.name, err)
							failLines = append(failLines, fmt.Sprintf("📄 %s 已保存到本地，但发送到 IM 失败: %s", pf.name, err.Error()))
						} else {
							imForwardedCount++
						}
					}
				}
				// Text only contains failure messages (if any); paths are in LocalFilePaths
				// so the frontend can render clickable links without duplication.
				text := strings.Join(failLines, "\n")
				if imForwardedCount > 0 {
					imNote := fmt.Sprintf("📨 已将 %d 个文件发送到 IM 通道", imForwardedCount)
					if text != "" {
						text = imNote + "\n" + text
					} else {
						text = imNote
					}
				}
				resp := &IMAgentResponse{
					Text:           text,
					LocalFilePaths: savedPaths,
				}
				// Keep backward compat: also set singular field to first path
				if len(savedPaths) > 0 {
					resp.LocalFilePath = savedPaths[0]
				}
				return resp
			}
			// IM platforms: send the last file (IM channels support one attachment per message)
			last := pendingFiles[len(pendingFiles)-1]
			return &IMAgentResponse{
				Text:         "",
				FileData:     last.data,
				FileName:     last.name,
				FileMimeType: last.mimeType,
			}
		}
	}

	// When rounds are exhausted but coding sessions are still active,
	// auto-continue one extra round so the agent can check session status,
	// then ask the user whether to keep watching.
	if h.manager != nil && h.manager.HasActiveSessions() {
		sendProgress("⏳ 推理轮次已用完，但编程会话仍在运行，正在检查状态…")

		// Run one bonus iteration to let the agent observe current session state.
		conversation = trimConversation(conversation, cfg.EffectiveContextTokens(), toolsTokenBudget, makeSummarizer(cfg, httpClient))
		if onNewRound != nil {
			onNewRound()
		}
		bonusResp, err := h.doLLMRequestStream(cfg, conversation, tools, httpClient, onToken)
		// Accumulate token usage stats for bonus round
		if bonusResp != nil {
			var input, output int
			if bonusResp.Usage != nil {
				u := bonusResp.Usage
				input = u.PromptTokens
				output = u.CompletionTokens
				if input == 0 && u.InputTokens > 0 {
					input = u.InputTokens
				}
				if output == 0 && u.OutputTokens > 0 {
					output = u.OutputTokens
				}
			} else {
				// Fallback: estimate tokens when provider doesn't return usage in streaming mode.
				input = estimateConversationTokens(conversation)
				if len(bonusResp.Choices) > 0 {
					output = estimateBytesToTokens([]byte(bonusResp.Choices[0].Message.Content))
				}
			}
			h.app.AccumulateLLMTokenUsage(h.app.GetMaclawLLMProviders().Current, input, output)
		}
		if err == nil && len(bonusResp.Choices) > 0 {
			bc := bonusResp.Choices[0]
			bcContent := bc.Message.Content
			bcReasoning := bc.Message.ReasoningContent
			if bcContent == "" && bcReasoning != "" {
				bcContent = bcReasoning
			}
			assistantMsg := map[string]interface{}{
				"role":    "assistant",
				"content": bcContent,
			}
			if bcReasoning != "" {
				assistantMsg["reasoning_content"] = bcReasoning
			}
			if len(bc.Message.ToolCalls) > 0 {
				assistantMsg["tool_calls"] = bc.Message.ToolCalls
			}
			conversation = append(conversation, assistantMsg)
			history = append(history, conversationEntry{
				Role: "assistant", Content: bcContent, ReasoningContent: bcReasoning, ToolCalls: bc.Message.ToolCalls,
			})

			// Execute any tool calls from the bonus round.
			for _, tc := range bc.Message.ToolCalls {
				toolOnProgress := onProgress
				if !isDebug() {
					toolOnProgress = nil
				}
				toolResult := h.executeTool(tc.Function.Name, tc.Function.Arguments, toolOnProgress)
				truncated := truncateToolResultForTool(tc.Function.Name, toolResult)
				conversation = append(conversation, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": tc.ID,
					"content":      truncated,
				})
				history = append(history, conversationEntry{
					Role: "tool", Content: truncated, ToolCallID: tc.ID,
				})
			}
		}

		h.memory.save(userID, trimHistory(history))
		return &IMAgentResponse{Text: "🔔 编程会话还在运行中。回复「继续」可以继续看护，回复其它内容正常对话。"}
	}

	h.memory.save(userID, trimHistory(history))
	return &IMAgentResponse{Text: "(已达到最大推理轮次，请继续发送消息以完成任务)"}
}

// saveScreenshotToFile saves base64-encoded PNG data to a local file under
// ~/.maclaw/data/screenshots/ and returns the absolute file path.
func (h *IMMessageHandler) saveScreenshotToFile(base64Data string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".maclaw", "data", "screenshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create screenshots directory: %w", err)
	}
	fileName := fmt.Sprintf("screenshot_%s_%d.png", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	filePath := filepath.Join(dir, fileName)
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return filePath, nil
}

// saveFileDataToLocal saves base64-encoded file data to ~/.maclaw/data/files/
// and returns the absolute file path.
func (h *IMMessageHandler) saveFileDataToLocal(name, base64Data string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".maclaw", "data", "files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create files directory: %w", err)
	}
	if name == "" {
		name = fmt.Sprintf("file_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	}
	// Sanitize: use only the base name to prevent path traversal (e.g. "../../etc/passwd")
	name = filepath.Base(name)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		name = fmt.Sprintf("file_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	}
	filePath := filepath.Join(dir, name)
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file failed: %w", err)
	}
	return filePath, nil
}

// ---------------------------------------------------------------------------
// Attachment → LLM Content Builder
// ---------------------------------------------------------------------------








