package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// IMMessageHandler — handles IM messages forwarded from Hub via WebSocket
// ---------------------------------------------------------------------------

// IMUserMessage is the payload of an "im.user_message" from Hub.
type IMUserMessage struct {
	UserID   string `json:"user_id"`
	Platform string `json:"platform"`
	Text     string `json:"text"`
}

// IMAgentResponse is the structured reply sent back to Hub.
type IMAgentResponse struct {
	Text     string             `json:"text"`
	Fields   []IMResponseField  `json:"fields,omitempty"`
	Actions  []IMResponseAction `json:"actions,omitempty"`
	ImageKey string             `json:"image_key,omitempty"`
	Error    string             `json:"error,omitempty"`
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

const (
	maxAgentIterations     = 12
	maxConversationTurns   = 40
	maxMemoryTokenEstimate = 80_000
	memoryTTL              = 2 * time.Hour  // 对话记忆过期时间
	memoryCleanupInterval  = 10 * time.Minute
)

type conversationEntry struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCalls  interface{} `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// toMessage converts a conversationEntry to a map suitable for the LLM API.
func (e conversationEntry) toMessage() interface{} {
	m := map[string]interface{}{"role": e.Role, "content": e.Content}
	if e.ToolCalls != nil {
		m["tool_calls"] = e.ToolCalls
	}
	if e.ToolCallID != "" {
		m["tool_call_id"] = e.ToolCallID
	}
	return m
}

type conversationSession struct {
	entries    []conversationEntry
	lastAccess time.Time
}

type conversationMemory struct {
	mu       sync.RWMutex
	sessions map[string]*conversationSession
	stopCh   chan struct{}
}

func newConversationMemory() *conversationMemory {
	cm := &conversationMemory{
		sessions: make(map[string]*conversationSession),
		stopCh:   make(chan struct{}),
	}
	go cm.evictionLoop()
	return cm
}

// evictionLoop 定期清理过期的对话记忆，防止内存无限增长
func (cm *conversationMemory) evictionLoop() {
	ticker := time.NewTicker(memoryCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cm.evictExpired()
		case <-cm.stopCh:
			return
		}
	}
}

func (cm *conversationMemory) evictExpired() {
	now := time.Now()
	cm.mu.Lock()
	defer cm.mu.Unlock()
	for uid, s := range cm.sessions {
		if now.Sub(s.lastAccess) > memoryTTL {
			delete(cm.sessions, uid)
		}
	}
}

func (cm *conversationMemory) stop() {
	select {
	case cm.stopCh <- struct{}{}:
	default:
	}
}

func (cm *conversationMemory) load(userID string) []conversationEntry {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	s := cm.sessions[userID]
	if s == nil {
		return nil
	}
	out := make([]conversationEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

func (cm *conversationMemory) save(userID string, entries []conversationEntry) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.sessions[userID] = &conversationSession{
		entries:    entries,
		lastAccess: time.Now(),
	}
}

func (cm *conversationMemory) clear(userID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.sessions, userID)
}

func estimateTokens(entries []conversationEntry) int {
	total := 0
	for _, e := range entries {
		data, _ := json.Marshal(e)
		total += len(data)
	}
	return total / 4
}

func trimHistory(entries []conversationEntry) []conversationEntry {
	if len(entries) <= maxConversationTurns {
		return entries
	}
	return entries[len(entries)-maxConversationTurns:]
}

// ---------------------------------------------------------------------------
// IMMessageHandler
// ---------------------------------------------------------------------------

// toolsCacheTTL is the maximum age of the cached tool definitions.
// When MCP_Registry changes, tools are regenerated within this window.
const toolsCacheTTL = 5 * time.Second

// IMMessageHandler processes IM messages using the local LLM Agent.
// It accesses mcpRegistry and skillExecutor via h.app at call time
// (not captured at construction) to handle late initialization.
type IMMessageHandler struct {
	app     *App
	manager *RemoteSessionManager
	memory  *conversationMemory
	client  *http.Client

	// Dynamic tool generation and routing (lazily initialized via setters).
	toolDefGen     *ToolDefinitionGenerator
	toolRouter     *ToolRouter
	cachedTools    []map[string]interface{}
	toolsCacheTime time.Time
	toolsMu        sync.RWMutex

	// Capability gap detection (lazily initialized via setter).
	capabilityGapDetector *CapabilityGapDetector
}

// NewIMMessageHandler creates a new handler.
func NewIMMessageHandler(app *App, manager *RemoteSessionManager) *IMMessageHandler {
	return &IMMessageHandler{
		app:     app,
		manager: manager,
		memory:  newConversationMemory(),
		client:  &http.Client{Timeout: 120 * time.Second},
	}
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
}

// getTools returns the current tool definitions, using the generator with
// a 5-second cache when configured, falling back to buildToolDefinitions().
func (h *IMMessageHandler) getTools() []map[string]interface{} {
	h.toolsMu.RLock()
	gen := h.toolDefGen
	cached := h.cachedTools
	cacheTime := h.toolsCacheTime
	h.toolsMu.RUnlock()

	// Fallback: no generator configured — use hardcoded definitions.
	if gen == nil {
		return h.buildToolDefinitions()
	}

	// Return cached tools if still fresh (within 5 seconds).
	if cached != nil && time.Since(cacheTime) < toolsCacheTTL {
		return cached
	}

	// Regenerate from the generator.
	tools := gen.Generate()

	h.toolsMu.Lock()
	h.cachedTools = tools
	h.toolsCacheTime = time.Now()
	h.toolsMu.Unlock()

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

// HandleIMMessage processes an IM user message and returns the Agent's response.
func (h *IMMessageHandler) HandleIMMessage(msg IMUserMessage) *IMAgentResponse {
	if !h.app.isMaclawLLMConfigured() {
		return &IMAgentResponse{
			Error: "MaClaw LLM 未配置，无法处理请求。请在 MaClaw 客户端的设置中配置 LLM。",
		}
	}

	trimmed := strings.TrimSpace(msg.Text)
	if trimmed == "/new" || trimmed == "/reset" || trimmed == "/clear" {
		h.memory.clear(msg.UserID)
		return &IMAgentResponse{Text: "对话已重置。"}
	}

	history := h.memory.load(msg.UserID)
	history = h.compactHistory(history)
	systemPrompt := h.buildSystemPrompt()
	return h.runAgentLoop(msg.UserID, systemPrompt, history, msg.Text)
}

// compactHistory summarizes old conversation turns to stay within token limits.
func (h *IMMessageHandler) compactHistory(entries []conversationEntry) []conversationEntry {
	if estimateTokens(entries) < maxMemoryTokenEstimate {
		return entries
	}
	split := len(entries) / 2
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
	resp, err := h.doLLMRequest(cfg, conv, nil)
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

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
}

type llmChoice struct {
	Message      llmMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type llmMessage struct {
	Role      string        `json:"role"`
	Content   string        `json:"content"`
	ToolCalls []llmToolCall `json:"tool_calls,omitempty"`
}

type llmToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// doLLMRequest sends a chat completion request to the configured LLM.
func (h *IMMessageHandler) doLLMRequest(cfg MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}) (*llmResponse, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/chat/completions"

	reqBody := map[string]interface{}{
		"model":    cfg.Model,
		"messages": messages,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenClaw/1.0")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if resp.StatusCode != http.StatusOK {
		msg := string(body)
		if len(msg) > 512 {
			msg = msg[:512] + "..."
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}

	var result llmResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}

// ---------------------------------------------------------------------------
// Agentic Loop — multi-round tool calling
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) runAgentLoop(userID, systemPrompt string, history []conversationEntry, userText string) (result *IMAgentResponse) {
	// panic recovery — 防止工具执行异常导致 goroutine 崩溃
	defer func() {
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Agent 内部错误: %v", r)}
		}
	}()

	cfg := h.app.GetMaclawLLMConfig()
	allTools := h.getTools()
	tools := h.routeTools(userText, allTools)

	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, entry := range history {
		conversation = append(conversation, entry.toMessage())
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": userText})

	history = append(history, conversationEntry{Role: "user", Content: userText})

	for iteration := 0; iteration < maxAgentIterations; iteration++ {
		resp, err := h.doLLMRequest(cfg, conversation, tools)
		if err != nil {
			return &IMAgentResponse{Error: fmt.Sprintf("LLM 调用失败: %s", err.Error())}
		}
		if len(resp.Choices) == 0 {
			return &IMAgentResponse{Error: "LLM 未返回有效回复"}
		}

		choice := resp.Choices[0]

		assistantMsg := map[string]interface{}{
			"role":    "assistant",
			"content": choice.Message.Content,
		}
		if len(choice.Message.ToolCalls) > 0 {
			assistantMsg["tool_calls"] = choice.Message.ToolCalls
		}
		conversation = append(conversation, assistantMsg)

		historyEntry := conversationEntry{Role: "assistant", Content: choice.Message.Content}
		if len(choice.Message.ToolCalls) > 0 {
			historyEntry.ToolCalls = choice.Message.ToolCalls
		}
		history = append(history, historyEntry)

		// No tool calls → final response.
		if len(choice.Message.ToolCalls) == 0 || choice.FinishReason == "stop" {
			// Check for capability gap before returning.
			if h.capabilityGapDetector != nil && h.capabilityGapDetector.Detect(choice.Message.Content) {
				skillName, result, err := h.capabilityGapDetector.Resolve(
					context.Background(), userText, nil,
					func(status string) {
						// Status updates are logged but not sent to user in this context.
					},
				)
				if skillName != "" && err == nil {
					finalText := fmt.Sprintf("✅ 已自动安装并执行 Skill「%s」\n%s", skillName, result)
					h.memory.save(userID, trimHistory(history))
					return &IMAgentResponse{Text: finalText}
				}
			}
			h.memory.save(userID, trimHistory(history))
			return &IMAgentResponse{Text: choice.Message.Content}
		}

		// Execute tool calls and feed results back.
		for _, tc := range choice.Message.ToolCalls {
			result := h.executeTool(tc.Function.Name, tc.Function.Arguments)
			conversation = append(conversation, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      result,
			})
			history = append(history, conversationEntry{
				Role: "tool", Content: result, ToolCallID: tc.ID,
			})
		}
	}

	h.memory.save(userID, trimHistory(history))
	return &IMAgentResponse{Text: "(已达到最大推理轮次，请继续发送消息以完成任务)"}
}

// ---------------------------------------------------------------------------
// System Prompt
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) buildSystemPrompt() string {
	var b strings.Builder
	b.WriteString(`你是 MaClaw 远程开发助手，一个运行在用户设备上的 AI Agent。
用户通过 IM（飞书/QBot）向你发送消息，你可以自主使用工具完成任务。

## 核心原则
- 主动使用工具：不要只是描述步骤，直接执行。收到请求后立即调用对应工具。
- 永远不要说"我没有某某工具"或"我无法执行"——先检查你的工具列表，大部分操作都有对应工具。
- 多步推理：复杂任务可以连续调用多个工具，逐步完成。
- 记忆上下文：你拥有对话记忆，可以引用之前的对话内容。
- 智能推断参数：如果用户没有指定 session_id 等参数，查看当前会话列表自动选择。

## ⚠️ 执行验证原则（极其重要，必须遵守）
每次执行操作后，你必须验证操作是否真正成功，绝不能仅凭工具返回"已发送"就告诉用户执行成功。
1. send_input 发送命令后 → 必须立即调用 get_session_output 查看实际输出，确认命令是否执行成功、有无报错。
2. create_session 创建会话后 → 必须调用 get_session_output 确认会话正常启动。
3. screenshot 截屏后 → 必须调用 get_session_events 确认截图是否成功发送。
4. call_mcp_tool / run_skill 执行后 → 检查返回结果是否包含错误。
绝对禁止在没有验证的情况下告诉用户"已完成"或"执行成功"。
如果验证发现失败，如实告诉用户失败原因，并尝试修复。

## 工具使用指南
- 执行命令：用 bash 直接在本机执行 shell 命令（创建目录、安装软件、运行脚本等），不需要创建会话。
- 文件操作：用 read_file 读文件、write_file 写文件、list_directory 列目录，这些都直接在本机执行。
- 截屏：直接调用 screenshot 工具，如果只有一个会话会自动选择。
- 创建会话：用 create_session，创建后必须用 get_session_output 确认启动。
- 发送命令：用 send_input，发送后必须用 get_session_output 确认结果。
- 查看输出：用 get_session_output 获取会话最近输出。
- 并行任务：用 parallel_execute 同时执行多个任务。
- MCP 工具：用 list_mcp_tools 查看可用工具，用 call_mcp_tool 调用。
- Skill：用 list_skills 查看，用 run_skill 执行。

注意：简单的文件操作和命令执行请直接用 bash/read_file/write_file/list_directory，不要绕道创建会话。

`)
	b.WriteString("## 当前设备状态\n")
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "MaClaw Desktop"
	}
	b.WriteString(fmt.Sprintf("- 设备名: %s\n", hostname))
	b.WriteString(fmt.Sprintf("- 平台: %s\n", normalizedRemotePlatform()))
	b.WriteString(fmt.Sprintf("- App 版本: %s\n", remoteAppVersion()))

	if h.manager != nil {
		sessions := h.manager.List()
		b.WriteString(fmt.Sprintf("- 活跃会话: %d 个\n", len(sessions)))
		if len(sessions) > 0 {
			b.WriteString("\n## 当前会话列表\n")
			for _, s := range sessions {
				s.mu.RLock()
				status := string(s.Status)
				task := s.Summary.CurrentTask
				lastResult := s.Summary.LastResult
				s.mu.RUnlock()
				b.WriteString(fmt.Sprintf("- [%s] 工具=%s 标题=%s 状态=%s", s.ID, s.Tool, s.Title, status))
				if task != "" {
					b.WriteString(fmt.Sprintf(" 当前任务=%s", task))
				}
				if lastResult != "" {
					b.WriteString(fmt.Sprintf(" 最近结果=%s", lastResult))
				}
				b.WriteString("\n")
			}
		}
	}

	if h.app.mcpRegistry != nil {
		servers := h.app.mcpRegistry.ListServers()
		if len(servers) > 0 {
			b.WriteString("\n## 已注册 MCP Server\n")
			for _, s := range servers {
				b.WriteString(fmt.Sprintf("- [%s] %s 状态=%s\n", s.ID, s.Name, s.HealthStatus))
			}
		}
	}

	if h.app.skillExecutor != nil {
		skills := h.app.skillExecutor.List()
		if len(skills) > 0 {
			b.WriteString("\n## 已注册 Skill\n")
			for _, s := range skills {
				if s.Status == "active" {
					b.WriteString(fmt.Sprintf("- %s: %s\n", s.Name, s.Description))
				}
			}
		}
	}

	b.WriteString("\n## 对话管理\n")
	b.WriteString("- 用户发送 /new 或 /reset 可重置对话\n")
	b.WriteString("- 你拥有多轮对话记忆，可以引用之前的上下文\n")
	b.WriteString("\n请用中文回复，关键技术术语保留英文。回复要简洁实用。")
	return b.String()
}

// ---------------------------------------------------------------------------
// Tool Definitions
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) buildToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		toolDef("list_sessions", "列出当前所有远程会话及其状态", nil, nil),
		toolDef("create_session", "创建新的远程会话。创建后建议用 get_session_output 观察启动状态。",
			map[string]interface{}{
				"tool":         map[string]string{"type": "string", "description": "工具名称，如 claude, codex, cursor, gemini, opencode"},
				"project_path": map[string]string{"type": "string", "description": "项目路径（可选）"},
			}, []string{"tool"}),
		toolDef("send_input", "向指定会话发送文本输入。发送后可用 get_session_output 观察结果。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
				"text":       map[string]string{"type": "string", "description": "要发送的文本"},
			}, []string{"session_id", "text"}),
		toolDef("get_session_output", "获取指定会话的最近输出内容和状态摘要。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
				"lines":      map[string]string{"type": "integer", "description": "返回最近 N 行输出（默认 30，最大 100）"},
			}, []string{"session_id"}),
		toolDef("get_session_events", "获取指定会话的重要事件列表（文件修改、命令执行、错误等）",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			}, []string{"session_id"}),
		toolDef("interrupt_session", "中断指定会话（发送 Ctrl+C 信号）",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			}, []string{"session_id"}),
		toolDef("kill_session", "终止指定会话",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID"},
			}, []string{"session_id"}),
		toolDef("screenshot", "截取会话的屏幕截图并发送给用户。如果只有一个活跃会话，可以省略 session_id 自动选择。",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "会话 ID（可选，只有一个会话时自动选择）"},
			}, nil),
		toolDef("list_mcp_tools", "列出已注册的 MCP Server 及其工具", nil, nil),
		toolDef("call_mcp_tool", "调用指定 MCP Server 上的工具",
			map[string]interface{}{
				"server_id": map[string]string{"type": "string", "description": "MCP Server ID"},
				"tool_name": map[string]string{"type": "string", "description": "工具名称"},
				"arguments": map[string]string{"type": "object", "description": "工具参数（JSON 对象）"},
			}, []string{"server_id", "tool_name"}),
		toolDef("list_skills", "列出已注册的 Skill", nil, nil),
		toolDef("run_skill", "执行指定的 Skill",
			map[string]interface{}{
				"name": map[string]string{"type": "string", "description": "Skill 名称"},
			}, []string{"name"}),
		toolDef("parallel_execute", "并行执行多个编程任务，每个任务在独立会话中运行（最多5个）",
			map[string]interface{}{
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "任务列表，每个任务包含 tool（工具名）、description（任务描述）、project_path（项目路径）",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"tool":         map[string]string{"type": "string", "description": "工具名称"},
							"description":  map[string]string{"type": "string", "description": "任务描述"},
							"project_path": map[string]string{"type": "string", "description": "项目路径"},
						},
					},
				},
			}, []string{"tasks"}),
		toolDef("recommend_tool", "根据任务描述推荐最合适的编程工具",
			map[string]interface{}{
				"task_description": map[string]string{"type": "string", "description": "任务描述"},
			}, []string{"task_description"}),
		// --- 本机直接操作工具 ---
		toolDef("bash", "在本机直接执行 shell 命令（如创建目录、移动文件、运行脚本等）。命令在 MaClaw 所在设备上执行，不需要会话。",
			map[string]interface{}{
				"command":     map[string]string{"type": "string", "description": "要执行的 shell 命令"},
				"working_dir": map[string]string{"type": "string", "description": "工作目录（可选，默认为用户主目录）"},
				"timeout":     map[string]string{"type": "integer", "description": "超时秒数（可选，默认 30，最大 120）"},
			}, []string{"command"}),
		toolDef("read_file", "读取本机文件内容",
			map[string]interface{}{
				"path":  map[string]string{"type": "string", "description": "文件路径（绝对路径或相对于主目录的路径）"},
				"lines": map[string]string{"type": "integer", "description": "最多读取行数（可选，默认 200）"},
			}, []string{"path"}),
		toolDef("write_file", "写入内容到本机文件（会创建不存在的目录）",
			map[string]interface{}{
				"path":    map[string]string{"type": "string", "description": "文件路径"},
				"content": map[string]string{"type": "string", "description": "文件内容"},
			}, []string{"path", "content"}),
		toolDef("list_directory", "列出本机目录内容",
			map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "目录路径（可选，默认为用户主目录）"},
			}, nil),
	}
}

func toolDef(name, desc string, props map[string]interface{}, required []string) map[string]interface{} {
	params := map[string]interface{}{"type": "object"}
	if props != nil {
		params["properties"] = props
	} else {
		params["properties"] = map[string]interface{}{}
	}
	if len(required) > 0 {
		params["required"] = required
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        name,
			"description": desc,
			"parameters":  params,
		},
	}
}

// ---------------------------------------------------------------------------
// Tool Execution
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) executeTool(name, argsJSON string) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = fmt.Sprintf("工具执行异常: %v", r)
		}
	}()

	var args map[string]interface{}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("参数解析失败: %s", err.Error())
		}
	}
	if args == nil {
		args = map[string]interface{}{}
	}
	switch name {
	case "list_sessions":
		return h.toolListSessions()
	case "create_session":
		return h.toolCreateSession(args)
	case "send_input":
		return h.toolSendInput(args)
	case "get_session_output":
		return h.toolGetSessionOutput(args)
	case "get_session_events":
		return h.toolGetSessionEvents(args)
	case "interrupt_session":
		return h.toolInterruptSession(args)
	case "kill_session":
		return h.toolKillSession(args)
	case "screenshot":
		return h.toolScreenshot(args)
	case "list_mcp_tools":
		return h.toolListMCPTools()
	case "call_mcp_tool":
		return h.toolCallMCPTool(args)
	case "list_skills":
		return h.toolListSkills()
	case "run_skill":
		return h.toolRunSkill(args)
	case "parallel_execute":
		return h.toolParallelExecute(args)
	case "recommend_tool":
		return h.toolRecommendTool(args)
	case "bash":
		return h.toolBash(args)
	case "read_file":
		return h.toolReadFile(args)
	case "write_file":
		return h.toolWriteFile(args)
	case "list_directory":
		return h.toolListDirectory(args)
	default:
		return fmt.Sprintf("未知工具: %s", name)
	}
}

func (h *IMMessageHandler) toolListSessions() string {
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return "当前没有活跃会话。"
	}
	var b strings.Builder
	for _, s := range sessions {
		s.mu.RLock()
		status := string(s.Status)
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("- [%s] 工具=%s 标题=%s 状态=%s", s.ID, s.Tool, s.Title, status))
		if task != "" {
			b.WriteString(fmt.Sprintf(" 任务=%s", task))
		}
		if waiting {
			b.WriteString(" [等待用户输入]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolCreateSession(args map[string]interface{}) string {
	tool, _ := args["tool"].(string)
	if tool == "" {
		return "缺少 tool 参数"
	}
	projectPath, _ := args["project_path"].(string)
	view, err := h.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool: tool, ProjectPath: projectPath,
	})
	if err != nil {
		return fmt.Sprintf("创建会话失败: %s", err.Error())
	}
	return fmt.Sprintf("会话已创建: ID=%s 工具=%s 标题=%s\n⚠️ 你必须立即调用 get_session_output(session_id=%q) 确认会话是否正常启动，不要直接告诉用户已完成。", view.ID, view.Tool, view.Title, view.ID)
}

func (h *IMMessageHandler) toolSendInput(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	text, _ := args["text"].(string)
	if sessionID == "" || text == "" {
		return "缺少 session_id 或 text 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.WriteInput(sessionID, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}
	return fmt.Sprintf("已发送到会话 %s。⚠️ 你必须立即调用 get_session_output(session_id=%q) 验证命令是否执行成功，不要直接告诉用户已完成。", sessionID, sessionID)
}

func (h *IMMessageHandler) toolGetSessionOutput(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}

	maxLines := 30
	if n, ok := args["lines"].(float64); ok && n > 0 {
		maxLines = int(n)
		if maxLines > 100 {
			maxLines = 100
		}
	}

	session.mu.RLock()
	status := string(session.Status)
	summary := session.Summary
	rawLines := make([]string, len(session.RawOutputLines))
	copy(rawLines, session.RawOutputLines)
	session.mu.RUnlock()

	var b strings.Builder
	b.WriteString(fmt.Sprintf("会话 %s 状态: %s\n", sessionID, status))
	if summary.CurrentTask != "" {
		b.WriteString(fmt.Sprintf("当前任务: %s\n", summary.CurrentTask))
	}
	if summary.ProgressSummary != "" {
		b.WriteString(fmt.Sprintf("进度: %s\n", summary.ProgressSummary))
	}
	if summary.LastResult != "" {
		b.WriteString(fmt.Sprintf("最近结果: %s\n", summary.LastResult))
	}
	if summary.LastCommand != "" {
		b.WriteString(fmt.Sprintf("最近命令: %s\n", summary.LastCommand))
	}
	if summary.WaitingForUser {
		b.WriteString("⚠️ 会话正在等待用户输入\n")
	}
	if summary.SuggestedAction != "" {
		b.WriteString(fmt.Sprintf("建议操作: %s\n", summary.SuggestedAction))
	}
	if len(rawLines) > 0 {
		start := 0
		if len(rawLines) > maxLines {
			start = len(rawLines) - maxLines
		}
		b.WriteString(fmt.Sprintf("\n--- 最近 %d 行输出 ---\n", len(rawLines)-start))
		for _, line := range rawLines[start:] {
			b.WriteString(line)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("\n(暂无输出)\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolGetSessionEvents(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}
	session.mu.RLock()
	events := make([]ImportantEvent, len(session.Events))
	copy(events, session.Events)
	session.mu.RUnlock()
	if len(events) == 0 {
		return fmt.Sprintf("会话 %s 暂无重要事件。", sessionID)
	}
	var b strings.Builder
	for _, ev := range events {
		b.WriteString(fmt.Sprintf("- [%s] %s: %s", ev.Severity, ev.Type, ev.Title))
		if ev.Summary != "" {
			b.WriteString(fmt.Sprintf(" — %s", ev.Summary))
		}
		if ev.RelatedFile != "" {
			b.WriteString(fmt.Sprintf(" (文件: %s)", ev.RelatedFile))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolInterruptSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.Interrupt(sessionID); err != nil {
		return fmt.Sprintf("中断失败: %s", err.Error())
	}
	return fmt.Sprintf("已向会话 %s 发送中断信号", sessionID)
}

func (h *IMMessageHandler) toolKillSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.Kill(sessionID); err != nil {
		return fmt.Sprintf("终止失败: %s", err.Error())
	}
	return fmt.Sprintf("已终止会话 %s", sessionID)
}

func (h *IMMessageHandler) toolScreenshot(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)

	// 如果未指定 session_id，自动选择唯一活跃会话
	if sessionID == "" && h.manager != nil {
		sessions := h.manager.List()
		if len(sessions) == 1 {
			sessionID = sessions[0].ID
		} else if len(sessions) > 1 {
			var lines []string
			lines = append(lines, "有多个活跃会话，请指定 session_id：")
			for _, s := range sessions {
				s.mu.RLock()
				status := string(s.Status)
				s.mu.RUnlock()
				lines = append(lines, fmt.Sprintf("- %s (工具=%s, 状态=%s)", s.ID, s.Tool, status))
			}
			return strings.Join(lines, "\n")
		} else {
			return "当前没有活跃会话，请先用 create_session 创建一个会话，然后再截屏"
		}
	}

	if sessionID == "" {
		return "缺少 session_id 参数，且无法自动选择会话"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	if err := h.manager.CaptureScreenshot(sessionID); err != nil {
		return fmt.Sprintf("截图失败: %s", err.Error())
	}
	return fmt.Sprintf("已请求截图。⚠️ 你必须立即调用 get_session_events(session_id=%q) 确认截图是否成功发送，不要直接告诉用户已完成。", sessionID)
}

func (h *IMMessageHandler) toolListMCPTools() string {
	registry := h.app.mcpRegistry
	if registry == nil {
		return "MCP Registry 未初始化"
	}
	servers := registry.ListServers()
	if len(servers) == 0 {
		return "没有已注册的 MCP Server"
	}
	var b strings.Builder
	for _, s := range servers {
		b.WriteString(fmt.Sprintf("## %s (%s) 状态=%s\n", s.Name, s.ID, s.HealthStatus))
		tools := registry.GetServerTools(s.ID)
		if len(tools) == 0 {
			b.WriteString("  (无工具或无法获取)\n")
			continue
		}
		for _, t := range tools {
			b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
		}
	}
	return b.String()
}

func (h *IMMessageHandler) toolCallMCPTool(args map[string]interface{}) string {
	registry := h.app.mcpRegistry
	if registry == nil {
		return "MCP Registry 未初始化"
	}
	serverID, _ := args["server_id"].(string)
	toolName, _ := args["tool_name"].(string)
	if serverID == "" || toolName == "" {
		return "缺少 server_id 或 tool_name 参数"
	}
	toolArgs, _ := args["arguments"].(map[string]interface{})
	result, err := registry.CallTool(serverID, toolName, toolArgs)
	if err != nil {
		return fmt.Sprintf("MCP 调用失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolListSkills() string {
	exec := h.app.skillExecutor
	if exec == nil {
		return "Skill Executor 未初始化"
	}
	skills := exec.List()
	if len(skills) == 0 {
		return "没有已注册的 Skill"
	}
	var b strings.Builder
	for _, s := range skills {
		b.WriteString(fmt.Sprintf("- %s [%s]: %s\n", s.Name, s.Status, s.Description))
	}
	return b.String()
}

func (h *IMMessageHandler) toolRunSkill(args map[string]interface{}) string {
	exec := h.app.skillExecutor
	if exec == nil {
		return "Skill Executor 未初始化"
	}
	name, _ := args["name"].(string)
	if name == "" {
		return "缺少 name 参数"
	}
	result, err := exec.Execute(name)
	if err != nil {
		return fmt.Sprintf("Skill 执行失败: %s\n%s", err.Error(), result)
	}
	return result
}

// stringVal extracts a string value from a map, returning "" if the key is
// missing or not a string.
func stringVal(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func (h *IMMessageHandler) toolParallelExecute(args map[string]interface{}) string {
	orch := h.app.orchestrator
	if orch == nil {
		return "Orchestrator 未初始化"
	}
	tasksRaw, ok := args["tasks"].([]interface{})
	if !ok || len(tasksRaw) == 0 {
		return "缺少 tasks 参数"
	}
	var tasks []TaskRequest
	for _, t := range tasksRaw {
		tm, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		tr := TaskRequest{
			Tool:        stringVal(tm, "tool"),
			Description: stringVal(tm, "description"),
			ProjectPath: stringVal(tm, "project_path"),
		}
		if tr.Tool == "" {
			continue
		}
		tasks = append(tasks, tr)
	}
	if len(tasks) == 0 {
		return "没有有效的任务"
	}
	result, err := orch.ExecuteParallel(tasks)
	if err != nil {
		return fmt.Sprintf("并行执行失败: %s", err.Error())
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("任务 %s: %s\n", result.TaskID, result.Summary))
	for key, sr := range result.Results {
		b.WriteString(fmt.Sprintf("- %s: tool=%s status=%s", key, sr.Tool, sr.Status))
		if sr.SessionID != "" {
			b.WriteString(fmt.Sprintf(" session=%s", sr.SessionID))
		}
		if sr.Error != "" {
			b.WriteString(fmt.Sprintf(" error=%s", sr.Error))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolRecommendTool(args map[string]interface{}) string {
	selector := h.app.toolSelector
	if selector == nil {
		return "ToolSelector 未初始化"
	}
	desc, _ := args["task_description"].(string)
	if desc == "" {
		return "缺少 task_description 参数"
	}
	// Build list of installed tools by checking if their binaries are on PATH.
	var installed []string
	for _, tool := range []string{"claude", "codex", "gemini", "cursor", "opencode", "iflow", "kilo"} {
		meta, ok := remoteToolCatalog[tool]
		if !ok {
			continue
		}
		if _, err := exec.LookPath(meta.BinaryName); err == nil {
			installed = append(installed, tool)
		}
	}
	name, reason := selector.Recommend(desc, installed)
	return fmt.Sprintf("推荐工具: %s\n理由: %s", name, reason)
}

// ---------------------------------------------------------------------------
// 本机直接操作工具 (bash, read_file, write_file, list_directory)
// ---------------------------------------------------------------------------

const (
	bashDefaultTimeout = 30
	bashMaxTimeout     = 120
	readFileMaxLines   = 200
	writeFileMaxSize   = 1 << 20 // 1 MB
)

// resolvePath resolves a path, expanding ~ and making relative paths relative
// to the user's home directory.
func resolvePath(p string) string {
	if p == "" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(p, "~") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p)
	}
	return filepath.Clean(p)
}

func (h *IMMessageHandler) toolBash(args map[string]interface{}) string {
	command, _ := args["command"].(string)
	if command == "" {
		return "缺少 command 参数"
	}

	timeout := bashDefaultTimeout
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = int(t)
		if timeout > bashMaxTimeout {
			timeout = bashMaxTimeout
		}
	}

	workDir := resolvePath(stringVal(args, "working_dir"))

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command", command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var b strings.Builder
	if stdout.Len() > 0 {
		out := stdout.String()
		if len(out) > 8192 {
			out = out[:8192] + "\n... (输出已截断)"
		}
		b.WriteString(out)
	}
	if stderr.Len() > 0 {
		errOut := stderr.String()
		if len(errOut) > 4096 {
			errOut = errOut[:4096] + "\n... (错误输出已截断)"
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr] ")
		b.WriteString(errOut)
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			b.WriteString(fmt.Sprintf("\n[错误] 命令超时（%d 秒）", timeout))
		} else {
			b.WriteString(fmt.Sprintf("\n[错误] 退出码: %v", err))
		}
	}

	if b.Len() == 0 {
		return "(命令执行完成，无输出)"
	}
	return b.String()
}

func (h *IMMessageHandler) toolReadFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	if p == "" {
		return "缺少 path 参数"
	}
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 是目录，请使用 list_directory 工具", absPath)
	}

	maxLines := readFileMaxLines
	if n, ok := args["lines"].(float64); ok && n > 0 {
		maxLines = int(n)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("读取失败: %s", err.Error())
	}

	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		return strings.Join(lines, "") + fmt.Sprintf("\n... (已截断，共 %d 行，显示前 %d 行)", len(strings.SplitAfter(string(data), "\n")), maxLines)
	}
	return string(data)
}

func (h *IMMessageHandler) toolWriteFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if p == "" || content == "" {
		return "缺少 path 或 content 参数"
	}
	if len(content) > writeFileMaxSize {
		return fmt.Sprintf("内容过大（%d 字节），最大允许 %d 字节", len(content), writeFileMaxSize)
	}

	absPath := resolvePath(p)

	// 自动创建父目录
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Sprintf("创建目录失败: %s", err.Error())
	}

	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return fmt.Sprintf("写入失败: %s", err.Error())
	}

	// 验证写入
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("写入后验证失败: %s", err.Error())
	}
	return fmt.Sprintf("已写入 %s（%d 字节）", absPath, info.Size())
}

func (h *IMMessageHandler) toolListDirectory(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("路径不存在或无法访问: %s", err.Error())
	}
	if !info.IsDir() {
		return fmt.Sprintf("%s 不是目录", absPath)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return fmt.Sprintf("读取目录失败: %s", err.Error())
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("目录: %s（共 %d 项）\n", absPath, len(entries)))
	shown := 0
	for _, entry := range entries {
		if shown >= 100 {
			b.WriteString(fmt.Sprintf("... 还有 %d 项未显示\n", len(entries)-shown))
			break
		}
		info, _ := entry.Info()
		if entry.IsDir() {
			b.WriteString(fmt.Sprintf("  📁 %s/\n", entry.Name()))
		} else if info != nil {
			b.WriteString(fmt.Sprintf("  📄 %s (%d bytes)\n", entry.Name(), info.Size()))
		} else {
			b.WriteString(fmt.Sprintf("  📄 %s\n", entry.Name()))
		}
		shown++
	}
	return b.String()
}
