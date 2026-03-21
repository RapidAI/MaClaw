package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// ---------------------------------------------------------------------------
// IMMessageHandler — handles IM messages forwarded from Hub via WebSocket
// ---------------------------------------------------------------------------

// IMUserMessage is the payload of an "im.user_message" from Hub.
type IMUserMessage struct {
	UserID            string   `json:"user_id"`
	Platform          string   `json:"platform"`
	Text              string   `json:"text"`
	MinIterations     int      `json:"min_iterations,omitempty"`      // floor for agent loop iterations (used by scheduled tasks)
	IsBackground      bool     `json:"is_background,omitempty"`       // true for scheduled tasks / auto-picked tasks (uses separate HTTP client)
	BackgroundSlotKind string  `json:"background_slot_kind,omitempty"` // "coding", "scheduled", "auto" — determines concurrency slot (default: "scheduled")
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
	maxConversationTurns   = 40
	maxMemoryTokenEstimate = 60_000 // lowered: tools+system prompt consume ~15-20K
	memoryTTL              = 2 * time.Hour  // 对话记忆过期时间
	memoryCleanupInterval  = 10 * time.Minute
)

type conversationEntry struct {
	Role             string      `json:"role"`
	Content          interface{} `json:"content"`
	ReasoningContent string      `json:"reasoning_content,omitempty"`
	ToolCalls        interface{} `json:"tool_calls,omitempty"`
	ToolCallID       string      `json:"tool_call_id,omitempty"`
}

// toMessage converts a conversationEntry to a map suitable for the LLM API.
func (e conversationEntry) toMessage() interface{} {
	m := map[string]interface{}{"role": e.Role, "content": e.Content}
	if e.ReasoningContent != "" {
		m["reasoning_content"] = e.ReasoningContent
	}
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

// memoryShardCount is the number of shards for conversation memory.
// Must be a power of 2 for fast modulo via bitwise AND.
const memoryShardCount = 16

// memoryShard holds a subset of conversation sessions, protected by its
// own lock to reduce contention when multiple users chat concurrently.
type memoryShard struct {
	mu       sync.RWMutex
	sessions map[string]*conversationSession
}

type conversationMemory struct {
	shards   [memoryShardCount]*memoryShard
	stopCh   chan struct{}
	archiver *ConversationArchiver
}

func newConversationMemory() *conversationMemory {
	cm := &conversationMemory{
		stopCh: make(chan struct{}),
	}
	for i := range cm.shards {
		cm.shards[i] = &memoryShard{
			sessions: make(map[string]*conversationSession),
		}
	}
	go cm.evictionLoop()
	return cm
}

// shard returns the shard for a given userID using FNV-1a hash.
func (cm *conversationMemory) shard(userID string) *memoryShard {
	h := uint32(2166136261) // FNV offset basis
	for i := 0; i < len(userID); i++ {
		h ^= uint32(userID[i])
		h *= 16777619 // FNV prime
	}
	return cm.shards[h&(memoryShardCount-1)]
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
	// Collect expired sessions outside the lock to avoid holding it during
	// archival (which may perform network I/O).
	type expiredEntry struct {
		userID  string
		entries []conversationEntry
	}
	var toArchive []expiredEntry

	for _, sh := range cm.shards {
		sh.mu.Lock()
		for uid, s := range sh.sessions {
			if now.Sub(s.lastAccess) > memoryTTL {
				if cm.archiver != nil {
					toArchive = append(toArchive, expiredEntry{uid, s.entries})
				}
				delete(sh.sessions, uid)
			}
		}
		sh.mu.Unlock()
	}

	// Archive outside any lock so slow I/O doesn't block other users.
	for _, e := range toArchive {
		if err := cm.archiver.Archive(e.userID, e.entries); err != nil {
			fmt.Fprintf(os.Stderr, "conversation_archiver: failed to archive user %s: %v\n", e.userID, err)
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
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	s := sh.sessions[userID]
	if s == nil {
		return nil
	}
	out := make([]conversationEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

func (cm *conversationMemory) save(userID string, entries []conversationEntry) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.sessions[userID] = &conversationSession{
		entries:    entries,
		lastAccess: time.Now(),
	}
}

func (cm *conversationMemory) clear(userID string) {
	sh := cm.shard(userID)
	sh.mu.Lock()
	defer sh.mu.Unlock()
	delete(sh.sessions, userID)
}

func estimateTokens(entries []conversationEntry) int {
	total := 0
	for _, e := range entries {
		data, _ := json.Marshal(e)
		total += estimateBytesToTokens(data)
	}
	return total
}

// estimateConversationTokens estimates the token count for a raw conversation
// slice ([]interface{}) used inside the agent loop.
// For Chinese-heavy content the JSON byte length underestimates token count
// because CJK characters are 3 bytes in UTF-8 but typically 1-2 tokens.
// We use len/3 instead of len/4 to be more conservative.
func estimateConversationTokens(msgs []interface{}) int {
	total := 0
	for _, m := range msgs {
		data, _ := json.Marshal(m)
		total += estimateBytesToTokens(data)
	}
	return total
}

// estimateToolsTokens estimates the token count consumed by tool definitions.
func estimateToolsTokens(tools []map[string]interface{}) int {
	if len(tools) == 0 {
		return 0
	}
	data, _ := json.Marshal(tools)
	return estimateBytesToTokens(data)
}

// estimateBytesToTokens converts JSON bytes to an approximate token count.
// CJK characters are 3 bytes in UTF-8 but typically 1-2 tokens; ASCII is
// roughly 4 bytes per token. We use a blended ratio of ~2.5 bytes/token
// which is more accurate for mixed CJK/ASCII content than the old /3.
func estimateBytesToTokens(data []byte) int {
	return (len(data)*10 + 24) / 25 // equivalent to len/2.5, rounded up
}

// defaultContextTokens is re-exported from corelib for local use.
const defaultContextTokens = corelib.DefaultContextTokens

// msgRole extracts the "role" field from a conversation message regardless
// of whether it's map[string]string or map[string]interface{}.
func msgRole(m interface{}) string {
	switch v := m.(type) {
	case map[string]interface{}:
		r, _ := v["role"].(string)
		return r
	case map[string]string:
		return v["role"]
	}
	return ""
}

// msgHasToolCalls checks if a conversation message has a non-nil tool_calls field.
func msgHasToolCalls(m interface{}) bool {
	if v, ok := m.(map[string]interface{}); ok {
		return v["tool_calls"] != nil
	}
	return false
}

// trimConversation keeps the first message (system prompt) and trims older
// middle messages so the total estimated tokens stay under the limit.
// It preserves tool-call integrity: assistant messages with tool_calls and
// their corresponding tool-result messages are always kept or dropped together.
// trimConversation trims conversation messages to fit within tokenLimit.
// toolsTokens is the estimated token count consumed by tool definitions,
// which must be subtracted from the available budget for messages.
// summarizer is an optional callback that summarizes dropped messages into a
// short text so the LLM retains key context. When nil, dropped messages are
// replaced with a generic placeholder.
func trimConversation(msgs []interface{}, tokenLimit int, toolsTokens int, summarizer func(string) string) []interface{} {
	if tokenLimit <= 0 {
		tokenLimit = defaultContextTokens * 80 / 100
	}
	// Reserve space for tool definitions.
	msgBudget := tokenLimit - toolsTokens
	if msgBudget < 4000 {
		msgBudget = 4000 // absolute minimum to avoid degenerate cases
	}
	if estimateConversationTokens(msgs) <= msgBudget {
		return msgs
	}
	if len(msgs) <= 3 {
		return msgs
	}

	// Strategy: keep msgs[0] (system prompt), drop oldest middle messages
	// until we fit. We scan from index 1 forward, skipping the tail we want
	// to keep, and grow the tail until it fits.
	//
	// To avoid breaking tool-call pairs we identify "logical groups":
	// an assistant message with tool_calls + all immediately following tool
	// messages form one indivisible group.

	type msgGroup struct {
		start, end int // half-open range [start, end) in msgs
	}

	// Build groups from msgs[1:]
	var groups []msgGroup
	i := 1
	for i < len(msgs) {
		gStart := i
		role := msgRole(msgs[i])
		if role == "assistant" && msgHasToolCalls(msgs[i]) {
			// This assistant message + all following tool messages = one group
			i++
			for i < len(msgs) {
				if msgRole(msgs[i]) != "tool" {
					break
				}
				i++
			}
		} else {
			i++
		}
		groups = append(groups, msgGroup{start: gStart, end: i})
	}

	// Try dropping the fewest groups from the front first (dropCount=1),
	// increasing until the remaining tail fits within the budget.
	// This preserves as much recent context as possible.
	systemMsg := msgs[:1]
	fallbackPlaceholder := []interface{}{map[string]string{
		"role":    "user",
		"content": "[注意：中间的对话历史因上下文长度限制已被省略，请基于最近的上下文继续工作]",
	}}

	// Start from keeping all groups, then drop from the front.
	// First pass: find the minimum dropCount without summarization.
	bestDropCount := -1
	for dropCount := 1; dropCount < len(groups); dropCount++ {
		kept := groups[dropCount:]
		var result []interface{}
		result = append(result, systemMsg...)
		result = append(result, fallbackPlaceholder...)
		for _, g := range kept {
			result = append(result, msgs[g.start:g.end]...)
		}
		if estimateConversationTokens(result) <= msgBudget {
			bestDropCount = dropCount
			break
		}
	}

	if bestDropCount > 0 {
		dropped := groups[:bestDropCount]
		kept := groups[bestDropCount:]

		// Try to summarize the dropped messages (one LLM call only).
		placeholder := fallbackPlaceholder
		if summarizer != nil && len(dropped) > 0 {
			var sb strings.Builder
			for _, g := range dropped {
				for idx := g.start; idx < g.end; idx++ {
					data, _ := json.Marshal(msgs[idx])
					sb.Write(data)
					sb.WriteByte('\n')
				}
			}
			raw := sb.String()
			if len(raw) > 32000 {
				raw = raw[:32000] + "\n...(truncated)"
			}
			if summary := summarizer(raw); summary != "" {
				// Cap summary to ~2000 tokens (~5000 chars) to avoid blowing the budget.
				if len(summary) > 5000 {
					runes := []rune(summary)
					if len(runes) > 5000 {
						summary = string(runes[:5000]) + "…"
					}
				}
				placeholder = []interface{}{
					map[string]string{"role": "user", "content": "[对话历史摘要]\n" + summary},
					map[string]string{"role": "assistant", "content": "好的，我已了解之前的对话上下文。"},
				}
			}
		}

		var result []interface{}
		result = append(result, systemMsg...)
		result = append(result, placeholder...)
		for _, g := range kept {
			result = append(result, msgs[g.start:g.end]...)
		}
		// If summary made it larger than fallback, just use fallback.
		if estimateConversationTokens(result) > msgBudget {
			result = result[:0]
			result = append(result, systemMsg...)
			result = append(result, fallbackPlaceholder...)
			for _, g := range kept {
				result = append(result, msgs[g.start:g.end]...)
			}
		}
		return result
	}

	// Even keeping only the last group doesn't fit — try secondary truncation
	// of tool results within the last group to squeeze it in.
	lastG := groups[len(groups)-1]
	result := truncateLastGroup(msgs, lastG.start, lastG.end, systemMsg, fallbackPlaceholder)
	if estimateConversationTokens(result) <= msgBudget {
		return result
	}

	// Still over budget — aggressively truncate assistant content in the result
	// while keeping tool-call pairs intact.
	result = truncateAssistantContent(result, msgBudget)
	if estimateConversationTokens(result) <= msgBudget {
		return result
	}

	// Last resort: drop the entire tool-call group, keep only system +
	// placeholder + a minimal user message so the LLM can still respond.
	// This avoids orphaned tool messages that would cause API errors.
	return append(systemMsg, fallbackPlaceholder...)
}

// truncateLastGroup builds a result from system + placeholder + the last
// message group, truncating tool-result content to fit.
func truncateLastGroup(msgs []interface{}, start, end int, systemMsg, placeholder []interface{}) []interface{} {
	var result []interface{}
	result = append(result, systemMsg...)
	result = append(result, placeholder...)
	for idx := start; idx < end; idx++ {
		m := msgs[idx]
		if mm, ok := m.(map[string]interface{}); ok {
			if role, _ := mm["role"].(string); role == "tool" {
				if content, _ := mm["content"].(string); len(content) > 1024 {
					runes := []rune(content)
					headRunes := 400
					tailRunes := 200
					if len(runes) > headRunes+tailRunes {
						truncated := string(runes[:headRunes]) + "\n…(截断)…\n" + string(runes[len(runes)-tailRunes:])
						cp := make(map[string]interface{}, len(mm))
						for k, v := range mm {
							cp[k] = v
						}
						cp["content"] = truncated
						result = append(result, cp)
						continue
					}
				}
			}
		}
		result = append(result, m)
	}
	return result
}

// truncateAssistantContent shrinks assistant message text content in the
// conversation to help fit within the token budget. It never touches
// tool_calls or tool messages to avoid breaking call/result pairing.
func truncateAssistantContent(msgs []interface{}, budget int) []interface{} {
	result := make([]interface{}, len(msgs))
	copy(result, msgs)
	for i, m := range result {
		if estimateConversationTokens(result) <= budget {
			break
		}
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := mm["role"].(string)
		if role != "assistant" {
			continue
		}
		cp := make(map[string]interface{}, len(mm))
		for k, v := range mm {
			cp[k] = v
		}
		// Truncate reasoning_content first (it can be very long with
		// thinking-mode models like Kimi) since it's less critical than
		// the actual content for subsequent reasoning.
		if rc, _ := cp["reasoning_content"].(string); len(rc) > 200 {
			runes := []rune(rc)
			if len(runes) > 200 {
				cp["reasoning_content"] = string(runes[:100]) + "\n…(reasoning truncated)…\n" + string(runes[len(runes)-50:])
			}
		}
		content, _ := cp["content"].(string)
		if len(content) <= 200 {
			result[i] = cp
			continue
		}
		runes := []rune(content)
		if len(runes) <= 200 {
			result[i] = cp
			continue
		}
		cp["content"] = string(runes[:100]) + "\n…(截断)…\n" + string(runes[len(runes)-50:])
		result[i] = cp
	}
	return result
}

// makeSummarizer returns a summarizer callback that uses doSimpleLLMRequest
// to condense dropped conversation history into a short summary.
func makeSummarizer(cfg MaclawLLMConfig, httpClient *http.Client) func(string) string {
	return func(text string) string {
		msgs := []interface{}{
			map[string]string{"role": "user", "content": "请简洁总结以下对话历史，保留关键事实、决策和待办事项：\n\n" + text},
		}
		result, err := doSimpleLLMRequest(cfg, msgs, httpClient, 30*time.Second)
		if err != nil || result.Content == "" {
			return ""
		}
		return result.Content
	}
}

func trimHistory(entries []conversationEntry) []conversationEntry {
	if len(entries) <= maxConversationTurns {
		return entries
	}
	trimmed := entries[len(entries)-maxConversationTurns:]
	// Ensure we don't start with orphaned "tool" messages that lack a
	// preceding assistant message with tool_calls — the LLM API rejects
	// such sequences with "Messages with role 'tool' must be a response
	// to a preceding message with 'tool_calls'".
	for len(trimmed) > 0 && trimmed[0].Role == "tool" {
		trimmed = trimmed[1:]
	}
	return trimmed
}

// maxToolResultLen caps individual tool results to ~4KB before they enter
// the conversation. This prevents a single verbose tool output (e.g. bash
// stdout, large file read) from dominating the context window.
const maxToolResultLen = 4096

// truncateToolResult caps a tool result string to maxToolResultLen bytes.
// If truncated, it keeps the first and last portions so the LLM sees both
// the beginning (often headers/status) and the end (often the conclusion).
func truncateToolResult(s string) string {
	if len(s) <= maxToolResultLen {
		return s
	}
	headLen := maxToolResultLen * 2 / 3
	tailLen := maxToolResultLen - headLen - 40 // 40 bytes for the separator
	return s[:headLen] + "\n\n... (已截断，共 " + fmt.Sprintf("%d", len(s)) + " 字节) ...\n\n" + s[len(s)-tailLen:]
}

// truncateToolResultForTool applies tool-specific truncation strategies.
// Terminal output (get_session_output, bash) keeps more tail (recent output
// is more relevant). Structured data keeps more head (headers/schema).
func truncateToolResultForTool(toolName, s string) string {
	if len(s) <= maxToolResultLen {
		return s
	}
	sep := "\n\n... (已截断，共 " + fmt.Sprintf("%d", len(s)) + " 字节) ...\n\n"
	sepLen := len(sep)
	budget := maxToolResultLen - sepLen

	switch toolName {
	case "get_session_output", "send_and_observe", "bash":
		// Terminal output: tail is more important (recent lines)
		headLen := budget / 4
		tailLen := budget - headLen
		return s[:headLen] + sep + s[len(s)-tailLen:]
	default:
		// Default: head-heavy (status/headers at top)
		headLen := budget * 2 / 3
		tailLen := budget - headLen
		return s[:headLen] + sep + s[len(s)-tailLen:]
	}
}

// thinkTagPattern matches <think>...</think> blocks (including multiline)
// produced by reasoning models (DeepSeek, Kimi, QwQ, etc.) that should not
// be shown to end users. Also handles unclosed <think> tags (e.g. when
// output is truncated by max_tokens).
var thinkTagPattern = regexp.MustCompile(`(?si)<think>.*?</think>|<think>.*$`)

// stripThinkingTags removes <think>...</think> blocks from LLM output and
// trims any leading whitespace left behind.
func stripThinkingTags(s string) string {
	if !strings.Contains(s, "<think>") {
		return s
	}
	cleaned := thinkTagPattern.ReplaceAllString(s, "")
	return strings.TrimSpace(cleaned)
}

// ---------------------------------------------------------------------------
// IMMessageHandler
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
	app     *App
	manager *RemoteSessionManager
	memory  *conversationMemory
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
}

// NewIMMessageHandler creates a new handler.
func NewIMMessageHandler(app *App, manager *RemoteSessionManager) *IMMessageHandler {
	// Optimised transport for interactive chat — larger connection pool
	// so concurrent requests don't queue behind each other.
	chatTransport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		MaxConnsPerHost:     20,
		IdleConnTimeout:     90 * time.Second,
	}
	// Separate transport for background tasks (scheduled tasks, auto-picked
	// ClawNet tasks) so they never starve the chat connection pool.
	taskTransport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 10,
		MaxConnsPerHost:     10,
		IdleConnTimeout:     90 * time.Second,
	}

	chatClient := &http.Client{Timeout: 120 * time.Second, Transport: chatTransport}
	taskClient := &http.Client{Timeout: 180 * time.Second, Transport: taskTransport}

	h := &IMMessageHandler{
		app:        app,
		manager:    manager,
		memory:     newConversationMemory(),
		client:     chatClient,
		taskClient: taskClient,
	}
	// Initialize ToolRegistry and register builtin tools.
	h.registry = NewToolRegistry()
	registerBuiltinTools(h.registry, h)
	// Register non-code tools (Git, file search, health check).
	registerNonCodeTools(h.registry, app)
	h.toolBuilder = NewDynamicToolBuilder(h.registry)
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

// getTools returns the current tool definitions, using the generator with
// a 5-second cache when configured, falling back to buildToolDefinitions().
func (h *IMMessageHandler) getTools() []map[string]interface{} {
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
			return cached
		}

		// Sync dynamic tools (ClawNet) only on cache rebuild, not every call.
		h.syncClawNetTools()

		tools := h.toolBuilder.BuildAll()

		h.toolsMu.Lock()
		h.cachedTools = tools
		h.toolsCacheTime = time.Now()
		h.toolsMu.Unlock()
		return tools
	}

	// --- Legacy path: ToolDefinitionGenerator or hardcoded ---
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
	return h.HandleIMMessageWithProgressAndStream(msg, onProgress, nil, nil)
}

// HandleIMMessageWithProgressAndStream extends HandleIMMessageWithProgress with
// streaming support for the desktop AI assistant. When onToken is non-nil, each
// LLM text delta is pushed in real-time. When onNewRound is non-nil, it is called
// at the start of each new agent loop iteration (after the first) so the frontend
// can create a new message bubble. IM platforms pass nil for both.
func (h *IMMessageHandler) HandleIMMessageWithProgressAndStream(msg IMUserMessage, onProgress ProgressCallback, onToken TokenCallback, onNewRound NewRoundCallback) *IMAgentResponse {
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

		result := h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, msg.Text, onProgress, nil, nil, msg.MinIterations, msg.Platform)

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
	return h.runAgentLoop(loopCtx, msg.UserID, systemPrompt, history, msg.Text, onProgress, onToken, onNewRound, msg.MinIterations, msg.Platform)
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
	if estimateTokens(entries) < maxMemoryTokenEstimate {
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

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
}

type llmChoice struct {
	Message      llmMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type llmMessage struct {
	Role             string        `json:"role"`
	Content          string        `json:"content"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []llmToolCall `json:"tool_calls,omitempty"`
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
// Supports both OpenAI-compatible and Anthropic Messages API protocols.
// The httpClient parameter selects which connection pool to use (chat vs background).
func (h *IMMessageHandler) doLLMRequest(cfg MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llmResponse, error) {
	if cfg.Protocol == "anthropic" {
		return h.doAnthropicLLMRequest(cfg, messages, tools, httpClient)
	}
	return h.doOpenAILLMRequest(cfg, messages, tools, httpClient)
}

// doOpenAILLMRequest sends a request using the OpenAI-compatible protocol.
func (h *IMMessageHandler) doOpenAILLMRequest(cfg MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llmResponse, error) {
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

	resp, err := httpClient.Do(req)
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
		return nil, dumpLLMContext(resp.StatusCode, msg, data)
	}

	var result llmResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &result, nil
}

// doAnthropicLLMRequest sends a request using the Anthropic Messages API protocol
// and converts the response to the internal llmResponse format for compatibility.
func (h *IMMessageHandler) doAnthropicLLMRequest(cfg MaclawLLMConfig, messages []interface{}, tools []map[string]interface{}, httpClient *http.Client) (*llmResponse, error) {
	endpoint := strings.TrimRight(cfg.URL, "/") + "/v1/messages"

	converted := convertToAnthropicMessages(messages)

	reqBody := map[string]interface{}{
		"model":      cfg.Model,
		"messages":   converted.Messages,
		"max_tokens": 4096,
	}
	if converted.SystemText != "" {
		reqBody["system"] = converted.SystemText
	}

	// Convert OpenAI-style tools to Anthropic tool format
	if len(tools) > 0 {
		if at := convertToAnthropicTools(tools); len(at) > 0 {
			reqBody["tools"] = at
		}
	}

	data, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenClaw/1.0")
	req.Header.Set("anthropic-version", "2023-06-01")
	if cfg.Key != "" {
		req.Header.Set("x-api-key", cfg.Key)
	}

	resp, err := httpClient.Do(req)
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
		return nil, dumpLLMContext(resp.StatusCode, msg, data)
	}

	// Parse Anthropic response and convert to internal llmResponse format
	var anthropicResp struct {
		Content    []anthropicContentBlock `json:"content"`
		StopReason string                 `json:"stop_reason"`
	}
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	// Convert to llmResponse
	msg := llmMessage{Role: "assistant"}
	var textParts []string
	for _, block := range anthropicResp.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			argsJSON, _ := json.Marshal(block.Input)
			msg.ToolCalls = append(msg.ToolCalls, llmToolCall{
				ID:   block.ID,
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}
	msg.Content = strings.Join(textParts, "\n")

	finishReason := "stop"
	if anthropicResp.StopReason == "tool_use" {
		finishReason = "tool_calls"
	} else if anthropicResp.StopReason == "max_tokens" {
		finishReason = "length"
	}

	return &llmResponse{
		Choices: []llmChoice{{Message: msg, FinishReason: finishReason}},
	}, nil
}

// anthropicContentBlock represents a content block in the Anthropic Messages API response.
type anthropicContentBlock struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

// ---------------------------------------------------------------------------
// Agentic Loop — multi-round tool calling
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

func (h *IMMessageHandler) runAgentLoop(ctx *LoopContext, userID, systemPrompt string, history []conversationEntry, userText string, onProgress ProgressCallback, onToken TokenCallback, onNewRound NewRoundCallback, minIterations int, platform string) (result *IMAgentResponse) {
	// panic recovery — 防止工具执行异常导致 goroutine 崩溃
	defer func() {
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Agent 内部错误: %v", r)}
		}
	}()

	// Wire the loop context so tools can access it.
	h.currentLoopCtx = ctx
	defer func() { h.currentLoopCtx = nil }()

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

	// Delayed acknowledgment: when debug is off, schedule a brief receipt
	// after a short grace period. If the agent loop finishes quickly (e.g.
	// simple greetings), the receipt is suppressed — the user sees only the
	// final card, avoiding the redundant "收到，正在处理中" message.
	const ackDelay = 3 * time.Second
	ackDone := make(chan struct{})
	if !isDebug() {
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
	const heartbeatMsg = "__heartbeat__"
	heartbeatDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendProgress(heartbeatMsg)
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

	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": systemPrompt})
	for _, entry := range history {
		conversation = append(conversation, entry.toMessage())
	}
	conversation = append(conversation, map[string]string{"role": "user", "content": userText})

	history = append(history, conversationEntry{Role: "user", Content: userText})

	// maxIter == 0 means "unlimited" — agent decides when to stop.
	// We still enforce a hard safety cap to prevent runaway loops.
	effectiveMax := maxIter
	if effectiveMax <= 0 {
		effectiveMax = maxAgentIterationsCap
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
			} else if iteration == 3 || (iteration > 3 && iteration%5 == 0) {
				// Non-debug mode: send a patience hint at iteration 4, then
				// every 5 rounds so the user knows a long task is still alive.
				sendProgress("⏳ 任务较复杂，正在耐心处理中，稍后发你结果…")
			}
		}
		conversation = trimConversation(conversation, cfg.EffectiveContextTokens(), toolsTokenBudget, makeSummarizer(cfg, httpClient))
		// Notify frontend of new round (for streaming UI) — skip first iteration
		// since the frontend already created a placeholder message.
		if onNewRound != nil && iteration > 0 {
			onNewRound()
		}
		resp, err := h.doLLMRequestStream(cfg, conversation, tools, httpClient, onToken)
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
		if choice.Message.ReasoningContent != "" {
			assistantMsg["reasoning_content"] = choice.Message.ReasoningContent
		}
		if len(choice.Message.ToolCalls) > 0 {
			assistantMsg["tool_calls"] = choice.Message.ToolCalls
		}
		conversation = append(conversation, assistantMsg)

		historyEntry := conversationEntry{Role: "assistant", Content: choice.Message.Content, ReasoningContent: choice.Message.ReasoningContent}
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
					return &IMAgentResponse{Text: stripThinkingTags(finalText)}
				}
			}
			h.memory.save(userID, trimHistory(history))
			return &IMAgentResponse{Text: stripThinkingTags(choice.Message.Content)}
		}

		// Execute tool calls and feed results back.
		var pendingImageKey string
		type pendingFile struct {
			name, mimeType, data string
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
			if strings.HasPrefix(result, "[file_base64|") {
				rest := strings.TrimPrefix(result, "[file_base64|")
				if closeBracket := strings.Index(rest, "]"); closeBracket > 0 {
					meta := rest[:closeBracket]
					parts := strings.SplitN(meta, "|", 2)
					if len(parts) == 2 {
						pendingFiles = append(pendingFiles, pendingFile{
							name:     parts[0],
							mimeType: parts[1],
							data:     rest[closeBracket+1:],
						})
						toolContent = fmt.Sprintf("文件 %s 已准备好，将发送给用户。", parts[0])
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
				for _, pf := range pendingFiles {
					filePath, err := h.saveFileDataToLocal(pf.name, pf.data)
					if err != nil {
						failLines = append(failLines, fmt.Sprintf("📄 %s 保存失败: %s", pf.name, err.Error()))
						continue
					}
					savedPaths = append(savedPaths, filePath)
				}
				// Text only contains failure messages (if any); paths are in LocalFilePaths
				// so the frontend can render clickable links without duplication.
				resp := &IMAgentResponse{
					Text:           strings.Join(failLines, "\n"),
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
		if err == nil && len(bonusResp.Choices) > 0 {
			bc := bonusResp.Choices[0]
			assistantMsg := map[string]interface{}{
				"role":    "assistant",
				"content": bc.Message.Content,
			}
			if bc.Message.ReasoningContent != "" {
				assistantMsg["reasoning_content"] = bc.Message.ReasoningContent
			}
			if len(bc.Message.ToolCalls) > 0 {
				assistantMsg["tool_calls"] = bc.Message.ToolCalls
			}
			conversation = append(conversation, assistantMsg)
			history = append(history, conversationEntry{
				Role: "assistant", Content: bc.Message.Content, ReasoningContent: bc.Message.ReasoningContent, ToolCalls: bc.Message.ToolCalls,
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
// ~/.cceasy/screenshots/ and returns the absolute file path.
func (h *IMMessageHandler) saveScreenshotToFile(base64Data string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".cceasy", "screenshots")
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

// saveFileDataToLocal saves base64-encoded file data to ~/.cceasy/files/
// and returns the absolute file path.
func (h *IMMessageHandler) saveFileDataToLocal(name, base64Data string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".cceasy", "files")
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
// System Prompt
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) buildSystemPrompt() string {
	var b strings.Builder

	// Use configurable role name and description from settings.
	// Priority: memory self_identity > config > hardcoded defaults.
	roleName := "MaClaw"
	roleDesc := "一个尽心尽责无所不能的软件开发管家"
	roleTitle := "AI个人助手"
	if cfg, err := h.app.LoadConfig(); err == nil {
		if cfg.MaclawRoleName != "" {
			roleName = cfg.MaclawRoleName
		}
		if cfg.MaclawRoleDescription != "" {
			roleDesc = cfg.MaclawRoleDescription
		}
		if cfg.UIMode == "pro" {
			roleTitle = "AI编程助手"
		}
	}

	// Override identity from memory self_identity if present.
	var selfIdentityOverride string
	if h.memoryStore != nil {
		selfIdentityOverride = h.memoryStore.SelfIdentitySummary(600)
	}

	if selfIdentityOverride != "" {
		b.WriteString(fmt.Sprintf(`你的自我认知（来自记忆）：%s
你的底层系统名为 %s。你基于以上自我认知与用户交互。用户通过 IM（飞书/QBot）向你发送消息，你可以自主使用工具完成任务。
注意：如果用户在对话中要求你扮演其他角色或重新定义你的身份，请按照用户的要求调整，并用 memory(action: save, category: "self_identity") 更新你的自我认知记忆。`, selfIdentityOverride, roleName))
	} else {
		b.WriteString(fmt.Sprintf(`你是 %s %s，%s。
用户通过 IM（飞书/QBot）向你发送消息，你可以自主使用工具完成任务。
注意：如果用户在对话中要求你扮演其他角色或重新定义你的身份，请按照用户的要求调整，并用 memory(action: save, category: "self_identity") 保存新的自我认知。`, roleName, roleTitle, roleDesc))
	}

	b.WriteString(`
## 核心原则
- 主动使用工具：不要只是描述步骤，直接执行。收到请求后立即调用对应工具。
- 永远不要说"我没有某某工具"或"我无法执行"——先检查你的工具列表，大部分操作都有对应工具。
- 多步推理：复杂任务可以连续调用多个工具，逐步完成。
- 记忆上下文：你拥有对话记忆，可以引用之前的对话内容。
- 智能推断参数：如果用户没有指定 session_id 等参数，查看当前会话列表自动选择。

## ⚠️ 编程任务工作流（极其重要）

### 第一步：识别任务类型
- 编程任务（Coding_Task）：需要调用 create_session 启动远程编程工具的需求（写代码、重构、修 bug、添加功能等）
- 非编程任务：简单问答、文件操作（bash/read_file/write_file）、配置管理、截屏等 → 直接执行，不需要确认

### 第二步：检查跳过信号（Skip_Signal）
如果用户消息中包含以下表达，跳过确认直接执行：
- 中文：直接做、不用问了、按你的想法来、直接开始、不用确认、马上做、赶紧做
- English：just do it、skip confirmation、go ahead、do it now

### 第三步：需求确认（Confirmation Phase）
对于编程任务且无跳过信号时，必须先输出确认消息再执行：

**确认消息格式：**
📋 **需求确认**
1. 需求理解：[用简洁语言复述你对需求的理解]
2. 实现方案：[涉及的文件和大致实现思路]
3. 边界情况：[需要用户决定的设计决策点，如无则省略]

请确认是否按此方案执行？

**确认阶段规则：**
- 等待用户明确同意（如"好的"、"可以"、"确认"、"没问题"）后才调用 create_session
- 如果用户提出修正或追加新需求（如"加音效"、"还要支持XX"、"改成YY"），将新需求整合到当前方案中，重新输出完整的确认消息，不要直接开始编程
- ⚠️ 关键：在确认阶段，任何不是明确同意或跳过信号的用户回复，都应视为需求变更/补充，必须重新确认。不要把追加需求当成新的独立指令直接执行
- 如果用户在确认阶段发出跳过信号，立即执行

### 第四步：执行编程任务
用户确认后（或跳过确认后），按以下步骤执行：
1. 创建会话：调用 create_session 启动编程工具
2. 发送指令：调用 get_session_output 确认状态为 running 后（最多检查 2 次），立即用 send_and_observe 将确认阶段达成的需求理解和实现方案整合到编程指令中发送给编程工具
⚠️ 严禁在 create_session 之后、send_and_observe 之前插入 bash、read_file、write_file 等其他工具调用。创建会话后的第一个动作必须是 get_session_output 检查状态，第二个动作必须是 send_and_observe 发送编程指令。
3. 跟踪进度：
   - 简单操作（ls、cat 等）：send_and_observe 会直接返回结果
   - 复杂编程任务（写代码、重构等）：编程工具可能需要数分钟完成。如果 send_and_observe 返回时会话状态为 busy，每 15-30 秒调用一次 get_session_output 检查进度是正常的
   - ⚠️ 绝对不要终止状态为 busy 的编程会话——编程工具正在工作中
⚠️ 编程工具启动后会等待输入，不发送指令它不会开始工作。对已退出或出错的会话不要反复轮询 get_session_output。

### 第五步：任务完成后 Review/Fix/Optimize（RFO Phase）
当编程任务成功完成（会话状态为 waiting_input 或 exited 且 exit_code=0）时：

**RFO 询问格式：**
✅ 任务已完成。是否需要进一步优化代码质量？（会消耗额外 tokens）
- Review：审查代码质量、命名、结构
- Fix：修复潜在问题、边界情况
- Optimize：性能优化、代码简化
- 跳过：直接结束

**RFO 规则：**
- 用户可选择一个或多个选项（如"review 和 optimize"、"全部"）
- 多选时按 Review → Fix → Optimize 顺序执行
- 每个选项通过 send_and_observe 发送对应指令给编程工具
- 每个选项完成后报告结果再执行下一个
- 用户说"不需要"、"跳过"、"skip"时直接结束
- 如果任务失败（exit_code≠0 或 error 状态），跳过 RFO 直接报告失败

## ⚠️ 执行验证原则
每次执行操作后，必须验证是否真正成功，绝不能仅凭工具返回"已发送"就告诉用户执行成功。
- 优先使用 send_and_observe（发送并等待输出），它会自动等待结果返回
- 验证失败如实告知用户并尝试修复

## 🛑 会话失败止损原则（极其重要）
当会话状态为 exited 且退出码非 0 时，说明编程工具启动失败或异常退出：
- 不要反复重试创建新会话——同样的环境问题会导致同样的失败
- 不要反复调用 get_session_output 轮询已退出的会话——状态不会改变
- 立即停止工具调用，将错误信息和修复建议直接告知用户
- 常见原因：工具未安装、API Key 未配置、项目路径不存在、网络问题
- 如果输出中有具体错误信息，提取关键信息告诉用户如何修复
- 最多重试 1 次（换工具或换服务商），仍然失败则直接告知用户

## 工具使用要点
- 向会话发送指令优先用 send_and_observe（自动等待输出），避免分别调用 send_input + get_session_output
- 中断或终止会话用 control_session（action: interrupt/kill）
- 配置管理用 manage_config（action: get/update/batch_update/list_schema/export/import）
- 简单文件/命令操作直接用 bash/read_file/write_file/list_directory，不要绕道创建会话
- 截屏直接调用 screenshot，无需活跃会话也能截取本机桌面
- 用 send_file 通过 IM 通道直接发送文件给用户（支持图片、文档等任意文件类型）
- ⚠️ 发送本地磁盘上的文件/图片给用户时，必须用 send_file 工具——会话内的工具无法直接投递文件到 IM。SDK 会话中产生的截图会自动推送给用户，无需额外操作。
- 用 open 打开文件或网址（PDF、Excel、URL 等）
- 创建会话时可用 project_id 参数指定预设项目，或用 list_projects 查看可用项目列表

`)
	b.WriteString("## 当前设备状态\n")
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "MaClaw Desktop"
	}
	b.WriteString(fmt.Sprintf("- 设备名: %s\n", hostname))
	b.WriteString(fmt.Sprintf("- 平台: %s\n", normalizedRemotePlatform()))
	b.WriteString(fmt.Sprintf("- App 版本: %s\n", remoteAppVersion()))
	now := time.Now()
	b.WriteString(fmt.Sprintf("- 当前时间: %s（%s）\n", now.Format("2006-01-02 15:04"), now.Weekday()))

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

	// Inject background loop status when bgManager is active.
	if h.bgManager != nil {
		bgLoops := h.bgManager.List()
		if len(bgLoops) > 0 {
			b.WriteString("\n## 后台任务\n")
			for _, lctx := range bgLoops {
				b.WriteString(fmt.Sprintf("- [%s] 类型=%s 状态=%s 轮次=%d/%d",
					lctx.ID, lctx.SlotKind.String(), lctx.State(),
					lctx.Iteration(), lctx.MaxIterations()))
				if lctx.Description != "" {
					b.WriteString(fmt.Sprintf(" 描述=%s", lctx.Description))
				}
				b.WriteString("\n")
			}
			b.WriteString("⚠️ 有后台任务正在运行时，如果用户提出新的编程需求，先记录需求，等后台任务完成后再处理。\n")
		}
	}

	if h.app.skillExecutor != nil {
		skills := h.app.skillExecutor.List()
		if len(skills) > 0 {
			b.WriteString("\n## 已注册 Skill\n")
			for _, s := range skills {
				if s.Status == "active" {
					b.WriteString(fmt.Sprintf("- %s: %s", s.Name, s.Description))
					if s.UsageCount > 0 {
						b.WriteString(fmt.Sprintf(" (用过%d次, 成功率%.0f%%)", s.UsageCount, s.SuccessRate*100))
					}
					b.WriteString("\n")
				}
			}
		}
	}

	// Dynamic tool discovery info
	if h.registry != nil {
		allTools := h.registry.ListAvailable()
		mcpTools := h.registry.ListByCategory(ToolCategoryMCP)
		nonCodeTools := h.registry.ListByCategory(ToolCategoryNonCode)
		if len(mcpTools) > 0 || len(nonCodeTools) > 0 {
			b.WriteString(fmt.Sprintf("\n## 动态工具（共 %d 个可用）\n", len(allTools)))
			if len(mcpTools) > 0 {
				b.WriteString(fmt.Sprintf("- MCP 工具: %d 个（来自已注册的 MCP Server）\n", len(mcpTools)))
			}
			if len(nonCodeTools) > 0 {
				b.WriteString(fmt.Sprintf("- 非编程工具: %d 个（git_status, git_diff, git_commit, search_files 等）\n", len(nonCodeTools)))
			}
			b.WriteString("- 工具列表根据消息内容动态筛选，可用「使用XX工具」激活特定分组\n")
		}
	}

	// Security firewall info
	if h.firewall != nil {
		b.WriteString("\n## 安全防火墙\n")
		b.WriteString("- 所有工具调用经过安全风险评估和策略检查\n")
		b.WriteString("- 高风险操作（删除文件、修改权限、数据库 DROP 等）会被拦截或要求确认\n")
		b.WriteString("- 可用 query_audit_log 工具查看安全审计日志\n")
	}

	// Task orchestration info
	b.WriteString("\n## 高级能力\n")
	b.WriteString("- tool=auto: 创建会话时自动选择最适合的编程工具\n")
	b.WriteString("- orchestrate_task: 将复杂任务拆分为多个子任务并行执行\n")
	b.WriteString("- add_context_note: 记录项目上下文备注，跨会话共享\n")

	b.WriteString("\n## 对话管理\n")
	b.WriteString("- /new 或 /reset 重置对话 | /exit 或 /quit 终止所有会话 | /sessions 查看状态 | /help 帮助\n")
	b.WriteString("- 用户表达退出意图时，提醒发送 /exit\n")
	b.WriteString("\n请用中文回复，关键技术术语保留英文。回复要简洁实用。")

	// Inject lightweight memory section: user_fact summary + tool hint.
	h.appendMemorySection(&b, false)

	return b.String()
}

// buildSystemPromptWithMemory builds the system prompt with the lightweight
// memory section (user_fact summary + dynamic recall hint). The isFirstTurn
// flag controls whether the full memory management guide is included.
func (h *IMMessageHandler) buildSystemPromptWithMemory(userMessage string, isFirstTurn bool) string {
	// Build the base prompt without memory (strip the default non-first-turn section).
	base := h.buildSystemPrompt()
	if !isFirstTurn {
		return base
	}
	// First turn: strip the default memory section and re-append with guide.
	if idx := strings.Index(base, "\n## 用户记忆\n"); idx >= 0 {
		base = base[:idx]
	}
	var b strings.Builder
	b.WriteString(base)
	h.appendMemorySection(&b, true)
	return b.String()
}

// appendMemorySection appends a lightweight "## 用户记忆" section containing:
//   - A compressed one-line summary of user_fact entries (always present)
//   - A hint that other memories can be recalled via memory(action: recall)
//   - Full memory management guide only on first turn (isFirstTurn=true)
//
// Non-user_fact memories are NO LONGER injected here. The LLM retrieves
// them on demand via the memory(action: recall) tool.
func (h *IMMessageHandler) appendMemorySection(b *strings.Builder, isFirstTurn bool) {
	if h.memoryStore == nil {
		return
	}

	summary := h.memoryStore.UserFactSummary(400)

	b.WriteString("\n## 用户记忆\n")
	if summary != "" {
		b.WriteString(fmt.Sprintf("用户信息: %s\n", summary))
	}
	b.WriteString("其他记忆（偏好、项目知识、指令等）可通过 memory(action: recall, query: \"检索关键词\") 按需召回。\n")

	if isFirstTurn {
		b.WriteString("\n## 记忆管理指引\n")
		b.WriteString("识别到有价值的信息时，主动调用 memory(action: save) 保存：\n")
		b.WriteString("- 用户信息 → user_fact | 偏好 → preference | 项目知识 → project_knowledge | 指令 → instruction\n")
	}
}

// ---------------------------------------------------------------------------
// Tool Definitions
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) buildToolDefinitions() []map[string]interface{} {
	defs := []map[string]interface{}{
		toolDef("list_sessions", "列出当前所有远程会话及其状态", nil, nil),
		toolDef("create_session", "创建新的远程会话。可指定 provider 选择服务商。创建后建议用 get_session_output 观察启动状态。",
			map[string]interface{}{
				"tool":         map[string]string{"type": "string", "description": "工具名称，如 claude, codex, cursor, gemini, opencode"},
				"project_path": map[string]string{"type": "string", "description": "项目路径（可选）"},
				"project_id":   map[string]string{"type": "string", "description": "预设项目 ID（可选，与 project_path 二选一）"},
				"provider":     map[string]string{"type": "string", "description": "服务商名称（可选，如 Original, DeepSeek, 百度千帆）。不指定则使用桌面端当前选中的服务商"},
			}, []string{"tool"}),
		toolDef("list_providers", "列出指定编程工具的所有可用服务商（已过滤未配置的空服务商）",
			map[string]interface{}{
				"tool": map[string]string{"type": "string", "description": "工具名称，如 claude, codex, gemini"},
			}, []string{"tool"}),
		toolDef("list_projects", "列出已配置的项目列表，包含项目 ID、名称和路径", nil, nil),
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
		toolDef("screenshot", "截取屏幕截图并发送给用户。如果有活跃会话可指定 session_id，没有活跃会话时会直接截取本机桌面屏幕（不需要创建会话）。",
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
		toolDef("list_skills", "列出已注册的本地 Skill。如果本地没有 Skill，会同时展示 SkillHub 上的推荐 Skill 供安装。", nil, nil),
		toolDef("search_skill_hub", "在已配置的 SkillHub（如 openclaw、tencent 等）上搜索可用的 Skill",
			map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "搜索关键词（如 'git commit'、'代码审查'、'部署'）"},
			}, []string{"query"}),
		toolDef("install_skill_hub", "从 SkillHub 安装指定的 Skill 到本地。设置 auto_run=true 可安装后立即执行。",
			map[string]interface{}{
				"skill_id": map[string]string{"type": "string", "description": "Skill ID（从 search_skill_hub 结果中获取）"},
				"hub_url":  map[string]string{"type": "string", "description": "来源 Hub URL（从 search_skill_hub 结果中获取）"},
				"auto_run": map[string]string{"type": "boolean", "description": "安装成功后是否立即执行（默认 true）"},
			}, []string{"skill_id", "hub_url"}),
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
		toolDef("craft_tool", "当现有工具都无法完成任务时，自动研究问题并生成脚本来解决。会用 LLM 生成代码、执行、并注册为可复用的 Skill。适用于数据处理、API 调用、文件转换、系统管理等需要编程才能完成的任务。",
			map[string]interface{}{
				"task":          map[string]string{"type": "string", "description": "需要完成的任务描述（越详细越好）"},
				"language":      map[string]string{"type": "string", "description": "脚本语言: python/bash/powershell/node（可选，自动检测）"},
				"save_as_skill": map[string]string{"type": "boolean", "description": "执行成功后是否注册为 Skill 供下次复用（默认 true）"},
				"skill_name":    map[string]string{"type": "string", "description": "Skill 名称（可选，自动生成）"},
				"timeout":       map[string]string{"type": "integer", "description": "执行超时秒数（默认 60，最大 300）"},
			}, []string{"task"}),
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
		toolDef("send_file", "读取本机文件并发送给用户（通过 IM 通道直接发送文件）",
			map[string]interface{}{
				"path":      map[string]string{"type": "string", "description": "文件的绝对路径或相对于主目录的路径"},
				"file_name": map[string]string{"type": "string", "description": "发送时显示的文件名（可选，默认使用原文件名）"},
			}, []string{"path"}),
		toolDef("open", "用操作系统默认程序打开文件或网址。例如：打开 PDF 用默认阅读器、打开 .xlsx 用 Excel、打开 URL 用默认浏览器、打开文件夹用资源管理器。也支持 mailto: 链接。",
			map[string]interface{}{
				"target": map[string]string{"type": "string", "description": "要打开的文件路径、目录路径或 URL（如 C:\\Users\\test\\doc.pdf、https://example.com、mailto:test@example.com）"},
			}, []string{"target"}),
		// --- 长期记忆工具（合并） ---
		toolDef("memory", "管理长期记忆（action: recall/save/list/delete）。recall 按需检索相关记忆，save 保存新记忆。",
			map[string]interface{}{
				"action":   map[string]string{"type": "string", "description": "操作: recall(按需召回)/save(保存)/list(列出或搜索)/delete(删除)"},
				"query":    map[string]string{"type": "string", "description": "检索关键词（recall 时必填，由你提炼的精准检索词，非用户原始消息）"},
				"content":  map[string]string{"type": "string", "description": "记忆内容（save 时必填）"},
				"category": map[string]string{"type": "string", "description": "类别: user_fact/preference/project_knowledge/instruction（save 时必填，recall/list 时可选过滤）"},
				"tags": map[string]interface{}{
					"type":        "array",
					"description": "关联标签（save 时可选）",
					"items":       map[string]string{"type": "string"},
				},
				"keyword": map[string]string{"type": "string", "description": "按关键词搜索（list 时可选）"},
				"id":      map[string]string{"type": "string", "description": "记忆条目 ID（delete 时必填）"},
			}, []string{"action"}),
		// --- 会话模板工具 ---
		toolDef("create_template", "创建会话模板（快捷启动配置）",
			map[string]interface{}{
				"name":         map[string]string{"type": "string", "description": "模板名称"},
				"tool":         map[string]string{"type": "string", "description": "工具名称"},
				"project_path": map[string]string{"type": "string", "description": "项目路径"},
				"model_config": map[string]string{"type": "string", "description": "模型配置"},
				"yolo_mode":    map[string]string{"type": "boolean", "description": "是否开启 Yolo 模式"},
			}, []string{"name", "tool"}),
		toolDef("list_templates", "列出所有会话模板", nil, nil),
		toolDef("launch_template", "使用模板启动会话",
			map[string]interface{}{
				"template_name": map[string]string{"type": "string", "description": "模板名称"},
			}, []string{"template_name"}),
		// --- 配置管理工具 ---
		toolDef("get_config", "获取指定配置区域的当前值",
			map[string]interface{}{
				"section": map[string]string{"type": "string", "description": "配置区域名称（如 claude/gemini/remote/projects/maclaw_llm/proxy/general），为空或 all 返回概览"},
			}, []string{"section"}),
		toolDef("update_config", "修改单个配置项",
			map[string]interface{}{
				"section": map[string]string{"type": "string", "description": "配置区域名称"},
				"key":     map[string]string{"type": "string", "description": "配置项名称"},
				"value":   map[string]string{"type": "string", "description": "新值"},
			}, []string{"section", "key", "value"}),
		toolDef("batch_update_config", "批量修改配置（原子性，任一项失败则全部回滚）",
			map[string]interface{}{
				"changes": map[string]string{"type": "string", "description": "JSON 数组，每项包含 section/key/value，例如 [{\"section\":\"general\",\"key\":\"language\",\"value\":\"en\"}]"},
			}, []string{"changes"}),
		toolDef("list_config_schema", "列出所有可配置项的 schema 信息", nil, nil),
		toolDef("export_config", "导出当前配置（敏感字段已脱敏）", nil, nil),
		toolDef("import_config", "导入配置（JSON 格式，保留本机特有字段）",
			map[string]interface{}{
				"json_data": map[string]string{"type": "string", "description": "要导入的配置 JSON 字符串"},
			}, []string{"json_data"}),
		// --- Agent 自管理工具 ---
		toolDef("set_max_iterations", fmt.Sprintf("调整最大推理轮数。设置后会持久化保存，后续对话也会生效。当你判断任务复杂需要更多轮次时调用此工具扩展上限，任务简单时可缩减。上限不超过 %d。", maxAgentIterationsCap),
			map[string]interface{}{
				"max_iterations": map[string]string{"type": "integer", "description": fmt.Sprintf("新的最大轮数（1-%d）", maxAgentIterationsCap)},
				"reason":         map[string]string{"type": "string", "description": "调整原因（用于日志记录）"},
			}, []string{"max_iterations"}),
		// --- 定时任务工具 ---
		toolDef("create_scheduled_task", "创建定时任务。用户说 每天9点做XX、每周一下午3点做YY、从3月1号到15号每天上午10点做ZZ 时，解析出时间参数并调用此工具。day_of_week: -1=每天, 0=周日, 1=周一...6=周六。day_of_month: -1=不限, 1-31=每月几号。重要：如果用户说的是一次性任务（如'今天中午提醒我'、'明天下午3点做XX'），必须将 start_date 和 end_date 都设为目标日期，确保只执行一次。",
			map[string]interface{}{
				"name":         map[string]string{"type": "string", "description": "任务名称（简短描述）"},
				"action":       map[string]string{"type": "string", "description": "到时要执行的操作（自然语言描述，会发送给 agent 执行）"},
				"hour":         map[string]string{"type": "integer", "description": "执行时间-小时（0-23）"},
				"minute":       map[string]string{"type": "integer", "description": "执行时间-分钟（0-59，默认0）"},
				"day_of_week":  map[string]string{"type": "integer", "description": "星期几（-1=每天, 0=周日, 1=周一...6=周六，默认-1）"},
				"day_of_month": map[string]string{"type": "integer", "description": "每月几号（-1=不限, 1-31，默认-1）"},
				"start_date":   map[string]string{"type": "string", "description": "生效开始日期（格式 2006-01-02，可选）"},
				"end_date":     map[string]string{"type": "string", "description": "生效结束日期（格式 2006-01-02，可选）"},
			}, []string{"name", "action", "hour"}),
		toolDef("list_scheduled_tasks", "列出所有定时任务及其状态、下次执行时间", nil, nil),
		toolDef("delete_scheduled_task", "删除定时任务（按 ID 或名称）",
			map[string]interface{}{
				"id":   map[string]string{"type": "string", "description": "任务 ID（优先）"},
				"name": map[string]string{"type": "string", "description": "任务名称（ID 为空时按名称匹配）"},
			}, nil),
		toolDef("update_scheduled_task", "修改定时任务的时间或内容",
			map[string]interface{}{
				"id":           map[string]string{"type": "string", "description": "任务 ID（必填）"},
				"name":         map[string]string{"type": "string", "description": "新名称（可选）"},
				"action":       map[string]string{"type": "string", "description": "新的执行内容（可选）"},
				"hour":         map[string]string{"type": "integer", "description": "新的小时（可选）"},
				"minute":       map[string]string{"type": "integer", "description": "新的分钟（可选）"},
				"day_of_week":  map[string]string{"type": "integer", "description": "新的星期几（可选）"},
				"day_of_month": map[string]string{"type": "integer", "description": "新的每月几号（可选）"},
				"start_date":   map[string]string{"type": "string", "description": "新的开始日期（可选）"},
				"end_date":     map[string]string{"type": "string", "description": "新的结束日期（可选）"},
			}, []string{"id"}),
	}

	// ---------- ClawNet tools (dynamic — only when daemon is running) ----------
	if h.app != nil && h.app.clawNetClient != nil && h.app.clawNetClient.IsRunning() {
		defs = append(defs,
			toolDef("clawnet_search", "在虾网（ClawNet P2P 知识网络）中搜索知识条目。返回匹配的知识列表，包含标题、内容、作者等。",
				map[string]interface{}{
					"query": map[string]string{"type": "string", "description": "搜索关键词"},
				}, []string{"query"}),
			toolDef("clawnet_publish", "向虾网（ClawNet P2P 知识网络）发布一条知识条目。发布后其他节点可以搜索到。",
				map[string]interface{}{
					"title": map[string]string{"type": "string", "description": "知识标题"},
					"body":  map[string]string{"type": "string", "description": "知识内容（Markdown 格式）"},
				}, []string{"title", "body"}),
		)
	}

	return defs
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

func (h *IMMessageHandler) executeTool(name, argsJSON string, onProgress ProgressCallback) (result string) {
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

	// --- SecurityFirewall check (Phase 2 upgrade) ---
	if h.firewall != nil {
		ctx := &SecurityCallContext{SessionID: "local"}
		allowed, reason := h.firewall.Check(name, args, ctx)
		if !allowed {
			return reason
		}
	}

	// --- Registry-based dispatch (unified path) ---
	if h.registry != nil {
		if tool, ok := h.registry.Get(name); ok {
			if tool.HandlerProg != nil {
				return tool.HandlerProg(args, onProgress)
			}
			if tool.Handler != nil {
				return tool.Handler(args)
			}
		}
	}

	return fmt.Sprintf("未知工具: %s", name)
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
	projectPath, _ := args["project_path"].(string)
	projectID, _ := args["project_id"].(string)
	provider, _ := args["provider"].(string)

	var hints []string

	// Smart tool recommendation when tool is empty.
	if tool == "" && h.contextResolver != nil {
		recommended, reason := h.contextResolver.ResolveTool(projectPath, "")
		if recommended != "" {
			tool = recommended
			hints = append(hints, fmt.Sprintf("🔧 自动推荐工具: %s（%s）", tool, reason))
		}
	}
	if tool == "" {
		return "缺少 tool 参数，且无法自动推荐工具"
	}

	// Resolve project_id to project path (takes priority over project_path).
	cfg, cfgErr := h.app.LoadConfig()
	if cfgErr != nil {
		return fmt.Sprintf("加载配置失败: %s", cfgErr.Error())
	}
	if projectID != "" {
		var found bool
		for _, p := range cfg.Projects {
			if p.Id == projectID {
				projectPath = p.Path
				found = true
				hints = append(hints, fmt.Sprintf("📁 通过项目 ID 解析: %s → %s", projectID, p.Path))
				break
			}
		}
		if !found {
			var available []string
			for _, p := range cfg.Projects {
				available = append(available, fmt.Sprintf("%s(%s)", p.Id, p.Name))
			}
			if len(available) == 0 {
				return fmt.Sprintf("项目 ID %q 未找到，当前没有已配置的项目", projectID)
			}
			return fmt.Sprintf("项目 ID %q 未找到，可用项目: %s", projectID, strings.Join(available, ", "))
		}
	}

	// Smart project detection when project_path is empty.
	if projectPath == "" && h.contextResolver != nil {
		detected, reason := h.contextResolver.ResolveProject()
		if detected != "" {
			projectPath = detected
			hints = append(hints, fmt.Sprintf("📁 自动检测项目: %s（%s）", projectPath, reason))
		}
	}

	// Pre-launch environment check.
	if h.sessionPrecheck != nil {
		result := h.sessionPrecheck.Check(tool, projectPath)
		if !result.ToolReady {
			hints = append(hints, fmt.Sprintf("⚠️ 工具预检未通过: %s", result.ToolHint))
		}
		if !result.ProjectReady {
			hints = append(hints, "⚠️ 项目路径不存在或无法访问")
		}
		if !result.ModelReady {
			hints = append(hints, fmt.Sprintf("⚠️ 模型预检未通过: %s", result.ModelHint))
		}
		if result.AllPassed {
			hints = append(hints, "✅ 环境预检全部通过")
		}
		// Block session creation when the tool binary is missing — launching
		// a process that doesn't exist always exits immediately with code 1,
		// wasting a session slot and confusing the user with a cryptic error.
		if !result.ToolReady {
			return strings.Join(hints, "\n") + "\n❌ 工具未安装，无法创建会话。请先在桌面端安装 " + tool + " 后重试。"
		}
	}

	// ProviderResolver integration: resolve provider before starting session.
	toolCfg, tcErr := remoteToolConfig(cfg, tool)
	if tcErr != nil {
		return fmt.Sprintf("获取工具配置失败: %s", tcErr.Error())
	}

	resolver := &ProviderResolver{}
	resolveResult, resolveErr := resolver.Resolve(toolCfg, provider)
	if resolveErr != nil {
		errMsg := fmt.Sprintf("❌ 无法创建会话：%s\n请在桌面端为 %s 配置至少一个有效的服务商。", resolveErr.Error(), tool)
		return errMsg
	}
	if resolveResult.Fallback {
		hints = append(hints, fmt.Sprintf("⚡ 服务商已降级: %s → %s", resolveResult.OriginalName, resolveResult.Provider.ModelName))
	}
	resolvedProvider := resolveResult.Provider.ModelName

	view, err := h.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool: tool, ProjectPath: projectPath, Provider: resolvedProvider,
		LaunchSource: RemoteLaunchSourceAI,
	})
	if err != nil {
		errMsg := fmt.Sprintf("❌ 创建会话失败: %s", err.Error())
		errMsg += fmt.Sprintf("\n💡 修复建议:\n- 检查 %s 是否已安装并可正常运行\n- 确认项目路径 %s 存在且可访问\n- 使用 list_providers 查看可用服务商配置", tool, projectPath)
		return errMsg
	}

	// Start monitoring session startup progress in background.
	if h.startupFeedback != nil {
		h.startupFeedback.WatchStartup(view.ID, func(msg string) {
			// Progress messages are logged; in a real IM context the
			// onProgress callback from the agent loop would relay these.
			fmt.Fprintf(os.Stderr, "startup_feedback[%s]: %s\n", view.ID, msg)
		})
	}

	var b strings.Builder
	for _, hint := range hints {
		b.WriteString(hint)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("✅ 会话已创建 [%s]\n", view.ID))
	b.WriteString(fmt.Sprintf("🔧 工具: %s | 📦 服务商: %s | 📁 项目: %s\n", view.Tool, resolvedProvider, projectPath))
	b.WriteString(fmt.Sprintf("\n📋 下一步操作："))
	b.WriteString(fmt.Sprintf("\n1. 调用 get_session_output(session_id=%q) 确认会话已启动（状态为 running）", view.ID))
	b.WriteString(fmt.Sprintf("\n2. 立即调用 send_and_observe(session_id=%q, text=\"编程指令\") 将需求发送给编程工具", view.ID))
	b.WriteString("\n⚠️ 编程工具启动后等待输入，不发送指令不会开始工作。最多检查 2 次 get_session_output，确认 running 后立即发送。")
	b.WriteString("\n🛑 如果会话已退出（exited）且退出码非 0，不要重试，直接告知用户错误信息。")
	return b.String()
}

func (h *IMMessageHandler) toolListProviders(args map[string]interface{}) string {
	toolName, _ := args["tool"].(string)
	if toolName == "" {
		return "缺少 tool 参数"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %s", err.Error())
	}
	toolCfg, err := remoteToolConfig(cfg, toolName)
	if err != nil {
		return fmt.Sprintf("不支持的工具: %s", toolName)
	}
	valid := validProviders(toolCfg)
	if len(valid) == 0 {
		return fmt.Sprintf("工具 %s 没有可用的服务商，请在桌面端配置", toolName)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("工具 %s 的可用服务商:\n", toolName))
	for _, m := range valid {
		isDefault := ""
		if strings.EqualFold(m.ModelName, toolCfg.CurrentModel) {
			isDefault = " [当前默认]"
		}
		modelId := m.ModelId
		if len(modelId) > 20 {
			modelId = modelId[:20] + "..."
		}
		b.WriteString(fmt.Sprintf("  - %s (model_id=%s)%s\n", m.ModelName, modelId, isDefault))
	}
	return b.String()
}

func (h *IMMessageHandler) toolListProjects() string {
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return fmt.Sprintf("加载配置失败: %s", err.Error())
	}
	if len(cfg.Projects) == 0 {
		return "当前没有已配置的项目。请在桌面端添加项目。"
	}
	var b strings.Builder
	b.WriteString("📋 已配置项目列表:\n")
	for i, p := range cfg.Projects {
		current := ""
		if p.Id == cfg.CurrentProject {
			current = " ⭐ 当前项目"
		}
		b.WriteString(fmt.Sprintf("%d. [%s] %s - %s%s\n", i+1, p.Id, p.Name, p.Path, current))
	}
	return b.String()
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

	// When the session is still in "starting" state with no output, wait
	// briefly (up to ~5s) for the process to either produce output or exit.
	// This avoids returning an empty "(暂无输出)" result that causes the
	// LLM agent to poll repeatedly, wasting many iterations.
	session.mu.RLock()
	isStarting := session.Status == SessionStarting
	hasOutput := len(session.RawOutputLines) > 0
	session.mu.RUnlock()

	if isStarting && !hasOutput {
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			session.mu.RLock()
			changed := session.Status != SessionStarting || len(session.RawOutputLines) > 0
			session.mu.RUnlock()
			if changed {
				break
			}
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
		// When the session is running but has no output, it's likely waiting
		// for the first user message (SDK mode tools like Claude Code wait
		// for input after init). Hint the AI to send the task.
		if status == string(SessionRunning) {
			b.WriteString(fmt.Sprintf("\n📌 会话已就绪但暂无输出——编程工具在等待输入。请立即调用 send_and_observe(session_id=%q, text=\"编程指令\") 发送任务。", sessionID))
		} else if status == string(SessionStarting) {
			b.WriteString("\n⏳ 会话正在启动中，请稍后再次调用 get_session_output 检查状态（最多再检查 1 次）。")
		}
	}

	// When the session is busy (coding tool actively executing), hint the
	// Agent based on the current stall detection state.
	if status == string(SessionBusy) {
		session.mu.RLock()
		stallState := session.StallState
		session.mu.RUnlock()

		switch stallState {
		case StallStateSuspected:
			b.WriteString("\n⏳ 编程工具输出暂停，系统正在尝试恢复，请稍后再检查")
		case StallStateStuck:
			b.WriteString("\n⚠️ 编程工具可能已卡住，建议发送具体指令或终止会话")
		default: // StallStateNormal
			b.WriteString("\n⏳ 编程工具正在工作中，请等待后再检查进度")
		}
	}

	// When the session is waiting for input, hint the Agent based on the
	// semantic completion analysis result.
	if status == string(SessionWaitingInput) {
		session.mu.RLock()
		completionLevel := session.CompletionLevel
		session.mu.RUnlock()

		switch completionLevel {
		case CompletionCompleted:
			b.WriteString("\n✅ 任务似乎已完成，可以查看结果")
		case CompletionIncomplete:
			b.WriteString("\n⚠️ 任务似乎未完成，建议发送「继续」让编程工具继续工作")
		// CompletionUncertain: 保持现有默认提示（"⚠️ 会话正在等待用户输入"）
		}
	}

	// When the session has exited with a non-zero code, append a strong
	// stop-loss hint so the LLM agent stops retrying and reports the
	// failure to the user immediately.
	session.mu.RLock()
	var exitCodeVal *int
	if session.ExitCode != nil {
		cp := *session.ExitCode
		exitCodeVal = &cp
	}
	sessionStatus := session.Status
	sessionTool := session.Tool
	session.mu.RUnlock()

	if (sessionStatus == SessionExited || sessionStatus == SessionError) && exitCodeVal != nil && *exitCodeVal != 0 {
		b.WriteString(fmt.Sprintf("\n🛑 会话已失败退出（退出码 %d）。不要再对此会话调用任何工具。", *exitCodeVal))
		b.WriteString(fmt.Sprintf("\n请立即将错误信息告知用户，并建议检查 %s 的安装和配置。", sessionTool))
		b.WriteString("\n不要重复创建新会话重试——同样的环境问题会导致同样的失败。")
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

// toolSendAndObserve combines send_input + get_session_output into a single
// tool call. It sends text to a session, waits briefly for output to
// accumulate, then returns the session output — saving one LLM round-trip.
func (h *IMMessageHandler) toolSendAndObserve(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	text, _ := args["text"].(string)
	if sessionID == "" || text == "" {
		return "缺少 session_id 或 text 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}

	// Snapshot line count and image count BEFORE sending so we can detect new output/images.
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("会话 %s 不存在", sessionID)
	}
	session.mu.RLock()
	baseLineCount := len(session.RawOutputLines)
	baseImageCount := len(session.OutputImages)
	session.mu.RUnlock()

	if err := h.manager.WriteInput(sessionID, text); err != nil {
		return fmt.Sprintf("发送失败: %s", err.Error())
	}

	// Poll up to ~30s with increasing intervals, waiting for meaningful output.
	// We use newLines > 1 (not > 0) because the PTY echo of the sent text
	// typically produces 1 line immediately; real output starts after that.
	//
	// If the caller provides a positive timeout_seconds (capped at 120s),
	// we build a custom polling array that totals approximately that many
	// seconds, starting with the same ramp-up pattern and then filling
	// with 3 000 ms intervals.
	waitMs := []int{500, 500, 1000, 1000, 1500, 1500, 2000, 2000, 3000, 3000, 3000, 3000, 3000, 3000, 3000}
	if ts, ok := args["timeout_seconds"].(float64); ok && ts > 0 {
		if ts > 120.0 {
			ts = 120.0
		}
		targetMs := int(ts * 1000)
		base := []int{500, 500, 1000, 1000, 1500, 1500, 2000}
		sum := 0
		for _, v := range base {
			sum += v
		}
		custom := make([]int, len(base))
		copy(custom, base)
		for sum < targetMs {
			custom = append(custom, 3000)
			sum += 3000
		}
		waitMs = custom
	}
	for _, ms := range waitMs {
		time.Sleep(time.Duration(ms) * time.Millisecond)
		session.mu.RLock()
		newLines := len(session.RawOutputLines) - baseLineCount
		waiting := session.Summary.WaitingForUser
		status := session.Status
		session.mu.RUnlock()
		// Stop early if: meaningful new output, session waiting for input, or session ended.
		if newLines > 1 || waiting || status == SessionExited || status == SessionError {
			break
		}
	}

	// Check if session is still busy after polling — read stall state for precise hint.
	session.mu.RLock()
	stillBusy := session.Status == SessionBusy
	stallState := session.StallState
	session.mu.RUnlock()

	// Check if new images were produced during the command execution.
	// Images from SDK sessions are already delivered to the user via the
	// session.image WebSocket channel (Hub → Feishu notifier), so we do NOT
	// return [screenshot_base64] here (that would cause duplicate delivery).
	// Instead, append a note to the text output so the AI knows an image
	// was sent and can reference it in its response.
	session.mu.RLock()
	newImageCount := len(session.OutputImages) - baseImageCount
	session.mu.RUnlock()

	output := h.toolGetSessionOutput(map[string]interface{}{
		"session_id": sessionID,
		"lines":      float64(40),
	})

	// NOTE: busy/stall hints are already appended by toolGetSessionOutput above,
	// so we do NOT duplicate them here. The stillBusy/stallState variables are
	// retained for potential future use (e.g. deciding whether to retry).
	_ = stillBusy
	_ = stallState

	if newImageCount > 0 {
		output += fmt.Sprintf("\n\n📷 会话产生了 %d 张图片，已自动通过 IM 发送给用户。", newImageCount)
	}

	return output
}

// toolControlSession merges interrupt_session and kill_session into one tool.
func (h *IMMessageHandler) toolControlSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	action, _ := args["action"].(string)
	if sessionID == "" {
		return "缺少 session_id 参数"
	}
	if h.manager == nil {
		return "会话管理器未初始化"
	}
	switch action {
	case "interrupt":
		if err := h.manager.Interrupt(sessionID); err != nil {
			return fmt.Sprintf("中断失败: %s", err.Error())
		}
		return fmt.Sprintf("已向会话 %s 发送中断信号", sessionID)
	case "kill":
		if err := h.manager.Kill(sessionID); err != nil {
			return fmt.Sprintf("终止失败: %s", err.Error())
		}
		return fmt.Sprintf("已终止会话 %s", sessionID)
	default:
		return "action 参数无效，可选值: interrupt, kill"
	}
}

// toolManageConfig merges all config operations into a single tool.
func (h *IMMessageHandler) toolManageConfig(args map[string]interface{}) string {
	action, _ := args["action"].(string)
	switch action {
	case "get":
		return h.toolGetConfig(args)
	case "update":
		return h.toolUpdateConfig(args)
	case "batch_update":
		return h.toolBatchUpdateConfig(args)
	case "list_schema":
		return h.toolListConfigSchema()
	case "export":
		return h.toolExportConfig()
	case "import":
		return h.toolImportConfig(args)
	default:
		return "action 参数无效，可选值: get, update, batch_update, list_schema, export, import"
	}
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
			// 没有活跃会话时，直接截屏本机屏幕（不依赖 session）
			base64Data, err := h.manager.CaptureScreenshotDirect()
			if err != nil {
				return fmt.Sprintf("截图失败: %s", err.Error())
			}
			return fmt.Sprintf("[screenshot_base64]%s", base64Data)
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
	// 截图已通过 session.image 通道直接发送给用户，
	// 返回特殊标记让 runAgentLoop 立即终止，避免 Agent 继续推理导致重复发图。
	return "[screenshot_sent]"
}

func (h *IMMessageHandler) toolListMCPTools() string {
	var b strings.Builder
	hasAny := false

	// List local (stdio) MCP servers
	if mgr := h.app.localMCPManager; mgr != nil {
		for _, ts := range mgr.GetAllTools() {
			hasAny = true
			b.WriteString(fmt.Sprintf("## %s (%s) [本地/stdio]\n", ts.ServerName, ts.ServerID))
			for _, t := range ts.Tools {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
			}
		}
	}

	// List remote (HTTP) MCP servers
	registry := h.app.mcpRegistry
	if registry != nil {
		servers := registry.ListServers()
		for _, s := range servers {
			hasAny = true
			b.WriteString(fmt.Sprintf("## %s (%s) [远程/HTTP] 状态=%s\n", s.Name, s.ID, s.HealthStatus))
			tools := registry.GetServerTools(s.ID)
			if len(tools) == 0 {
				b.WriteString("  (无工具或无法获取)\n")
				continue
			}
			for _, t := range tools {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
			}
		}
	}

	if !hasAny {
		return "没有已注册的 MCP Server"
	}
	return b.String()
}

func (h *IMMessageHandler) toolCallMCPTool(args map[string]interface{}) string {
	serverID, _ := args["server_id"].(string)
	toolName, _ := args["tool_name"].(string)
	if serverID == "" || toolName == "" {
		return "缺少 server_id 或 tool_name 参数"
	}
	toolArgs, _ := args["arguments"].(map[string]interface{})

	// Try local MCP manager first (stdio-based servers)
	if mgr := h.app.localMCPManager; mgr != nil && mgr.IsRunning(serverID) {
		result, err := mgr.CallTool(serverID, toolName, toolArgs)
		if err != nil {
			return fmt.Sprintf("本地 MCP 调用失败: %s", err.Error())
		}
		return result
	}

	// Fall back to remote MCP registry (HTTP-based servers)
	registry := h.app.mcpRegistry
	if registry == nil {
		return "MCP Registry 未初始化"
	}
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

	var b strings.Builder

	// Show local skills
	if len(skills) > 0 {
		b.WriteString("=== 本地已注册 Skill ===\n")
		for _, s := range skills {
			line := fmt.Sprintf("- %s [%s]: %s", s.Name, s.Status, s.Description)
			if s.Source == "hub" {
				line += fmt.Sprintf(" (来源: Hub, trust: %s)", s.TrustLevel)
			} else if s.Source == "file" {
				line += " (来源: 本地文件)"
			}
			if s.UsageCount > 0 {
				line += fmt.Sprintf(" (用过%d次, 成功率%.0f%%)", s.UsageCount, s.SuccessRate*100)
			}
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString("本地没有已注册的 Skill。\n")
	}

	// If local skills are empty or few, also show Hub recommendations
	if len(skills) < 3 && h.app.skillHubClient != nil {
		recs := h.app.skillHubClient.GetRecommendations()
		if len(recs) > 0 {
			b.WriteString("\n=== SkillHub 推荐 Skill（可用 install_skill_hub 安装）===\n")
			for _, r := range recs {
				b.WriteString(fmt.Sprintf("- [%s] %s: %s (trust: %s, downloads: %d, hub: %s)\n",
					r.ID, r.Name, r.Description, r.TrustLevel, r.Downloads, r.HubURL))
			}
		} else {
			b.WriteString("\n提示：可以使用 search_skill_hub 工具在 SkillHub 上搜索更多 Skill。\n")
		}
	}

	return b.String()
}

func (h *IMMessageHandler) toolSearchSkillHub(args map[string]interface{}) string {
	query, _ := args["query"].(string)
	if query == "" {
		return "缺少 query 参数"
	}

	if h.app.skillHubClient == nil {
		h.app.ensureRemoteInfra()
	}
	if h.app.skillHubClient == nil {
		return "SkillHub 客户端未初始化，请检查配置中的 skill_hub_urls"
	}

	results, err := h.app.skillHubClient.Search(context.Background(), query)
	if err != nil {
		return fmt.Sprintf("搜索失败: %s", err.Error())
	}
	if len(results) == 0 {
		return fmt.Sprintf("在 SkillHub 上未找到与 %q 相关的 Skill", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 个 Skill：\n", len(results)))
	for _, r := range results {
		tags := ""
		if len(r.Tags) > 0 {
			tags = " [" + strings.Join(r.Tags, ", ") + "]"
		}
		b.WriteString(fmt.Sprintf("- ID: %s | %s: %s%s (trust: %s, downloads: %d, hub: %s)\n",
			r.ID, r.Name, r.Description, tags, r.TrustLevel, r.Downloads, r.HubURL))
	}
	b.WriteString("\n使用 install_skill_hub 工具安装，需提供 skill_id 和 hub_url 参数。")
	return b.String()
}

func (h *IMMessageHandler) toolInstallSkillHub(args map[string]interface{}) string {
	skillID, _ := args["skill_id"].(string)
	hubURL, _ := args["hub_url"].(string)
	if skillID == "" {
		return "缺少 skill_id 参数"
	}
	if hubURL == "" {
		return "缺少 hub_url 参数"
	}

	if h.app.skillHubClient == nil {
		h.app.ensureRemoteInfra()
	}
	if h.app.skillHubClient == nil {
		return "SkillHub 客户端未初始化"
	}
	if h.app.skillExecutor == nil {
		return "Skill Executor 未初始化"
	}

	// Download from Hub
	entry, err := h.app.skillHubClient.Install(context.Background(), skillID, hubURL)
	if err != nil {
		return fmt.Sprintf("安装失败: %s", err.Error())
	}

	// Security review if risk assessor is available
	if h.app.riskAssessor != nil {
		assessment := h.app.riskAssessor.AssessSkill(entry, entry.TrustLevel)
		if assessment.Level == RiskCritical {
			if h.app.auditLog != nil {
				_ = h.app.auditLog.Log(AuditEntry{
					Timestamp:    time.Now(),
					Action:       AuditActionHubSkillReject,
					ToolName:     "hub_skill_install",
					RiskLevel:    RiskCritical,
					PolicyAction: PolicyDeny,
					Result:       fmt.Sprintf("rejected skill %s from %s: critical risk", skillID, hubURL),
				})
			}
			return fmt.Sprintf("⚠️ Skill %q 包含高风险操作，已拒绝自动安装。风险因素: %s",
				entry.Name, strings.Join(assessment.Factors, ", "))
		}
	}

	// Register locally
	if err := h.app.skillExecutor.Register(*entry); err != nil {
		return fmt.Sprintf("注册失败: %s", err.Error())
	}

	// Audit log
	if h.app.auditLog != nil {
		_ = h.app.auditLog.Log(AuditEntry{
			Timestamp:    time.Now(),
			Action:       AuditActionHubSkillInstall,
			ToolName:     "hub_skill_install",
			RiskLevel:    RiskLow,
			PolicyAction: PolicyAllow,
			Result:       fmt.Sprintf("installed skill %s (%s) from %s, trust: %s", entry.Name, skillID, hubURL, entry.TrustLevel),
		})
	}

	// Auto-run: default to true unless explicitly set to false.
	autoRun := true
	if v, ok := args["auto_run"]; ok {
		switch val := v.(type) {
		case bool:
			autoRun = val
		case string:
			autoRun = strings.EqualFold(val, "true")
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("✅ 已成功安装 Skill「%s」\n描述: %s\n来源: %s\n信任等级: %s\n",
		entry.Name, entry.Description, hubURL, entry.TrustLevel))

	if autoRun {
		b.WriteString(fmt.Sprintf("\n正在立即执行 Skill「%s」...\n", entry.Name))
		result, err := h.app.skillExecutor.Execute(entry.Name)
		if err != nil {
			b.WriteString(fmt.Sprintf("执行失败: %s\n%s", err.Error(), result))
		} else {
			b.WriteString(fmt.Sprintf("执行结果:\n%s", result))
		}
	} else {
		b.WriteString(fmt.Sprintf("\n可以使用 run_skill 工具执行，名称为: %s", entry.Name))
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

func (h *IMMessageHandler) toolBash(args map[string]interface{}, onProgress ProgressCallback) string {
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
	hideCommandWindow(cmd)

	// Start the command and send periodic heartbeats for long-running ops.
	err := cmd.Start()
	if err != nil {
		return fmt.Sprintf("[错误] 命令启动失败: %v", err)
	}

	// Heartbeat goroutine: send progress every 30s while the command runs.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		elapsed := 0
		for {
			select {
			case <-ticker.C:
				elapsed += 30
				// Truncate command for display.
				displayCmd := command
				if len(displayCmd) > 60 {
					displayCmd = displayCmd[:60] + "…"
				}
				if onProgress != nil {
					onProgress(fmt.Sprintf("⏳ 命令仍在执行中（已 %ds）: %s", elapsed, displayCmd))
				}
			case <-done:
				return
			}
		}
	}()

	err = cmd.Wait()
	close(done)

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

const sendFileMaxSize = 200 << 20 // 200 MB — large files are handled by plugin-level fallback (temp URL)

func (h *IMMessageHandler) toolSendFile(args map[string]interface{}) string {
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
		return fmt.Sprintf("%s 是目录，不能作为文件发送", absPath)
	}
	if info.Size() > sendFileMaxSize {
		return fmt.Sprintf("文件过大（%d 字节），最大允许 %d 字节", info.Size(), sendFileMaxSize)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("读取文件失败: %s", err.Error())
	}

	fileName, _ := args["file_name"].(string)
	if fileName == "" {
		fileName = filepath.Base(absPath)
	}

	mimeType := mime.TypeByExtension(filepath.Ext(absPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	// Use | as delimiter to avoid conflicts with : in filenames or MIME types.
	return fmt.Sprintf("[file_base64|%s|%s]%s", fileName, mimeType, b64)
}

func (h *IMMessageHandler) toolOpen(args map[string]interface{}) string {
	target, _ := args["target"].(string)
	if target == "" {
		return "缺少 target 参数"
	}

	// Detect URLs (http, https, file, mailto, etc.)
	isURL := strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
	if !isURL {
		target = resolvePath(target)
		// Verify the path exists before attempting to open.
		if _, err := os.Stat(target); err != nil {
			return fmt.Sprintf("路径不存在或无法访问: %s", err.Error())
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Use rundll32 url.dll,FileProtocolHandler — opens files/URLs with
		// the default handler without spawning a visible console window.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("打开失败: %s", err.Error())
	}

	// Don't wait for the process — it's a GUI application.
	go cmd.Wait()

	if isURL {
		return fmt.Sprintf("已用默认浏览器打开: %s", target)
	}
	return fmt.Sprintf("已用默认程序打开: %s", target)
}

// ---------------------------------------------------------------------------
// Memory Tools
// ---------------------------------------------------------------------------

// toolMemory merges save/list/delete/recall memory operations into a single tool.
func (h *IMMessageHandler) toolMemory(args map[string]interface{}) string {
	if h.memoryStore == nil {
		return "长期记忆未初始化"
	}

	action := stringVal(args, "action")
	switch action {
	case "recall":
		query := stringVal(args, "query")
		if query == "" {
			return "缺少 query 参数"
		}
		category := MemoryCategory(stringVal(args, "category"))
		// Resolve current project path for affinity boosting.
		var projectPath string
		if h.contextResolver != nil {
			projectPath, _ = h.contextResolver.ResolveProject()
		}
		entries := h.memoryStore.RecallDynamic(query, category, projectPath)
		if len(entries) == 0 {
			return "没有找到相关记忆。"
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("召回 %d 条相关记忆:\n", len(entries)))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("- [%s] %s\n", string(e.Category), e.Content))
		}
		// Touch access counts.
		ids := make([]string, len(entries))
		for i, e := range entries {
			ids[i] = e.ID
		}
		h.memoryStore.TouchAccess(ids)
		return b.String()

	case "save":
		content := stringVal(args, "content")
		if content == "" {
			return "缺少 content 参数"
		}
		category := stringVal(args, "category")
		if category == "" {
			category = "user_fact"
		}
		var tags []string
		if rawTags, ok := args["tags"]; ok {
			if tagSlice, ok := rawTags.([]interface{}); ok {
				for _, t := range tagSlice {
					if s, ok := t.(string); ok && s != "" {
						tags = append(tags, s)
					}
				}
			}
		}
		entry := MemoryEntry{
			Content:  content,
			Category: MemoryCategory(category),
			Tags:     tags,
		}
		if err := h.memoryStore.Save(entry); err != nil {
			return fmt.Sprintf("保存记忆失败: %s", err.Error())
		}
		summary := content
		if len(summary) > 50 {
			summary = summary[:50] + "..."
		}
		return fmt.Sprintf("已保存记忆: %s", summary)

	case "list":
		category := MemoryCategory(stringVal(args, "category"))
		keyword := stringVal(args, "keyword")
		entries := h.memoryStore.List(category, keyword)
		if len(entries) == 0 {
			return "没有找到匹配的记忆条目。"
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("找到 %d 条记忆:\n", len(entries)))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("- [%s] (%s) %s", e.ID, e.Category, e.Content))
			if len(e.Tags) > 0 {
				b.WriteString(fmt.Sprintf(" 标签=%v", e.Tags))
			}
			b.WriteString("\n")
		}
		return b.String()

	case "delete":
		id := stringVal(args, "id")
		if id == "" {
			return "缺少 id 参数"
		}
		if err := h.memoryStore.Delete(id); err != nil {
			return fmt.Sprintf("删除记忆失败: %s", err.Error())
		}
		return fmt.Sprintf("已删除记忆: %s", id)

	default:
		return "action 参数无效，可选值: recall, save, list, delete"
	}
}

// ---------------------------------------------------------------------------
// Template Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateTemplate(args map[string]interface{}) string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	name := stringVal(args, "name")
	tool := stringVal(args, "tool")
	if name == "" || tool == "" {
		return "缺少 name 或 tool 参数"
	}

	tpl := SessionTemplate{
		Name:        name,
		Tool:        tool,
		ProjectPath: stringVal(args, "project_path"),
		ModelConfig: stringVal(args, "model_config"),
	}

	// Parse yolo_mode (can arrive as bool or string).
	if yolo, ok := args["yolo_mode"].(bool); ok {
		tpl.YoloMode = yolo
	} else if yoloStr, ok := args["yolo_mode"].(string); ok {
		tpl.YoloMode = yoloStr == "true"
	}

	if err := h.templateManager.Create(tpl); err != nil {
		return fmt.Sprintf("创建模板失败: %s", err.Error())
	}
	return fmt.Sprintf("模板已创建: %s（工具=%s, 项目=%s）", name, tool, tpl.ProjectPath)
}

func (h *IMMessageHandler) toolListTemplates() string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	templates := h.templateManager.List()
	if len(templates) == 0 {
		return "当前没有会话模板。"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("共 %d 个模板:\n", len(templates)))
	for _, t := range templates {
		b.WriteString(fmt.Sprintf("- %s: 工具=%s 项目=%s", t.Name, t.Tool, t.ProjectPath))
		if t.ModelConfig != "" {
			b.WriteString(fmt.Sprintf(" 模型=%s", t.ModelConfig))
		}
		if t.YoloMode {
			b.WriteString(" [Yolo]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolLaunchTemplate(args map[string]interface{}) string {
	if h.templateManager == nil {
		return "模板管理器未初始化"
	}

	name := stringVal(args, "template_name")
	if name == "" {
		return "缺少 template_name 参数"
	}

	tpl, err := h.templateManager.Get(name)
	if err != nil {
		return fmt.Sprintf("获取模板失败: %s", err.Error())
	}

	// Build args from template config and delegate to toolCreateSession.
	sessionArgs := map[string]interface{}{
		"tool":         tpl.Tool,
		"project_path": tpl.ProjectPath,
	}
	return h.toolCreateSession(sessionArgs)
}

// ---------------------------------------------------------------------------
// Config Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolGetConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	section := stringVal(args, "section")
	result, err := h.configManager.GetConfig(section, true)
	if err != nil {
		return fmt.Sprintf("读取配置失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	section := stringVal(args, "section")
	key := stringVal(args, "key")
	value := stringVal(args, "value")
	if section == "" || key == "" {
		return "缺少 section 或 key 参数"
	}

	oldValue, err := h.configManager.UpdateConfig(section, key, value)
	if err != nil {
		return fmt.Sprintf("修改配置失败: %s", err.Error())
	}
	return fmt.Sprintf("配置已更新: %s.%s\n旧值: %s\n新值: %s", section, key, oldValue, value)
}

func (h *IMMessageHandler) toolBatchUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	changesStr := stringVal(args, "changes")
	if changesStr == "" {
		return "缺少 changes 参数"
	}

	var changes []ConfigChange
	if err := json.Unmarshal([]byte(changesStr), &changes); err != nil {
		return fmt.Sprintf("解析 changes 参数失败: %s", err.Error())
	}
	if len(changes) == 0 {
		return "changes 列表为空"
	}

	if err := h.configManager.BatchUpdate(changes); err != nil {
		return fmt.Sprintf("批量更新配置失败: %s", err.Error())
	}
	return fmt.Sprintf("批量更新成功，共应用 %d 项变更", len(changes))
}

func (h *IMMessageHandler) toolListConfigSchema() string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	result, err := h.configManager.SchemaJSON()
	if err != nil {
		return fmt.Sprintf("获取配置 Schema 失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolExportConfig() string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	result, err := h.configManager.ExportConfig()
	if err != nil {
		return fmt.Sprintf("导出配置失败: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolImportConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "配置管理器未初始化"
	}

	jsonData := stringVal(args, "json_data")
	if jsonData == "" {
		return "缺少 json_data 参数"
	}

	report, err := h.configManager.ImportConfig(jsonData)
	if err != nil {
		return fmt.Sprintf("导入配置失败: %s", err.Error())
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("配置导入完成: 应用 %d 项, 跳过 %d 项", report.Applied, report.Skipped))
	if len(report.Warnings) > 0 {
		b.WriteString("\n警告:")
		for _, w := range report.Warnings {
			b.WriteString(fmt.Sprintf("\n  - %s", w))
		}
	}
	return b.String()
}

// toolSetMaxIterations allows the agent to dynamically adjust the max
// iterations for the current conversation loop. This does NOT change the
// persisted config — it only affects the in-flight loop.
func (h *IMMessageHandler) toolSetMaxIterations(args map[string]interface{}) string {
	n, ok := args["max_iterations"].(float64)
	if !ok || n < 1 {
		return fmt.Sprintf("缺少或无效的 max_iterations 参数（需要 1-%d 的整数）", maxAgentIterationsCap)
	}
	limit := int(n)
	if limit > maxAgentIterationsCap {
		limit = maxAgentIterationsCap
	}
	reason := stringVal(args, "reason")
	h.loopMaxOverride = limit
	// Also update the active LoopContext so background loops see the change.
	if h.currentLoopCtx != nil {
		h.currentLoopCtx.SetMaxIterations(limit)
	}

	// Persist to config so the setting survives across conversations.
	if err := h.app.SetMaclawAgentMaxIterations(limit); err != nil {
		// Non-fatal: the override still applies to the current loop.
		_ = err
	}

	if reason != "" {
		return fmt.Sprintf("✅ 已将最大轮数调整为 %d（已持久化，原因: %s）", limit, reason)
	}
	return fmt.Sprintf("✅ 已将最大轮数调整为 %d（已持久化）", limit)
}

// ---------------------------------------------------------------------------
// LLM provider switch tool
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolSwitchLLMProvider(args map[string]interface{}) string {
	providerName := stringVal(args, "provider")
	if providerName == "" {
		// No provider specified — list available providers and current selection.
		info := h.app.GetMaclawLLMProviders()
		var b strings.Builder
		b.WriteString(fmt.Sprintf("当前 LLM 服务商: %s\n可用服务商:\n", info.Current))
		for _, p := range info.Providers {
			if p.URL == "" && p.Key == "" && p.Model == "" {
				continue // skip unconfigured custom slots
			}
			marker := ""
			if p.Name == info.Current {
				marker = " [当前]"
			}
			b.WriteString(fmt.Sprintf("  - %s (model=%s)%s\n", p.Name, p.Model, marker))
		}
		return b.String()
	}

	info := h.app.GetMaclawLLMProviders()

	// Collect only configured providers for matching.
	var configured []MaclawLLMProvider
	for _, p := range info.Providers {
		if p.URL != "" || p.Key != "" || p.Model != "" {
			configured = append(configured, p)
		}
	}

	// Match: exact (case-insensitive) first, then substring fallback.
	needle := strings.ToLower(strings.TrimSpace(providerName))
	var match *MaclawLLMProvider
	for i := range configured {
		if strings.ToLower(configured[i].Name) == needle {
			match = &configured[i]
			break
		}
	}
	if match == nil {
		// Fuzzy: check if provider name contains the needle (not the reverse,
		// to avoid short provider names matching arbitrary long input).
		for i := range configured {
			lower := strings.ToLower(configured[i].Name)
			if strings.Contains(lower, needle) {
				match = &configured[i]
				break
			}
		}
	}

	if match == nil {
		var names []string
		for _, p := range configured {
			names = append(names, p.Name)
		}
		return fmt.Sprintf("未找到服务商 %q，可用: %s", providerName, strings.Join(names, ", "))
	}

	if match.Name == info.Current {
		return fmt.Sprintf("当前已经是 %s，无需切换", match.Name)
	}

	if err := h.app.SaveMaclawLLMProviders(info.Providers, match.Name); err != nil {
		return fmt.Sprintf("切换失败: %s", err.Error())
	}
	return fmt.Sprintf("✅ 已将 LLM 服务商切换为 %s (model=%s)", match.Name, match.Model)
}

// ---------------------------------------------------------------------------
// Scheduled task tool implementations
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	name := stringVal(args, "name")
	action := stringVal(args, "action")
	if name == "" || action == "" {
		return "缺少 name 或 action 参数"
	}
	hour := -1
	if v, ok := args["hour"].(float64); ok {
		hour = int(v)
	}
	if hour < 0 || hour > 23 {
		return "hour 必须在 0-23 之间"
	}
	minute := 0
	if v, ok := args["minute"].(float64); ok {
		minute = int(v)
	}
	dow := -1
	if v, ok := args["day_of_week"].(float64); ok {
		dow = int(v)
	}
	dom := -1
	if v, ok := args["day_of_month"].(float64); ok {
		dom = int(v)
	}

	t := ScheduledTask{
		Name:       name,
		Action:     action,
		Hour:       hour,
		Minute:     minute,
		DayOfWeek:  dow,
		DayOfMonth: dom,
		StartDate:  stringVal(args, "start_date"),
		EndDate:    stringVal(args, "end_date"),
		TaskType:   stringVal(args, "task_type"),
	}

	id, err := h.scheduledTaskManager.Add(t)
	if err != nil {
		return fmt.Sprintf("创建定时任务失败: %s", err.Error())
	}

	// Notify frontend to refresh the scheduled tasks panel.
	h.app.emitEvent("scheduled-tasks-changed")

	// 非一次性任务同步到系统日历
	if created := h.scheduledTaskManager.Get(id); created != nil && isRecurringTask(created) {
		go func() {
			if err := SyncTaskToSystemCalendar(created); err != nil {
				h.app.log(fmt.Sprintf("[scheduled-task] calendar sync failed: %v", err))
			}
		}()
	}

	// Format next run time for display.
	if task := h.scheduledTaskManager.Get(id); task != nil && task.NextRunAt != nil {
		return fmt.Sprintf("✅ 定时任务已创建\nID: %s\n名称: %s\n操作: %s\n下次执行: %s", id, name, action, task.NextRunAt.Format("2006-01-02 15:04"))
	}
	return fmt.Sprintf("✅ 定时任务已创建（ID: %s）", id)
}

func (h *IMMessageHandler) toolListScheduledTasks() string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	tasks := h.scheduledTaskManager.List()
	if len(tasks) == 0 {
		return "当前没有定时任务。"
	}

	weekdays := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("共 %d 个定时任务：\n\n", len(tasks)))
	for _, t := range tasks {
		b.WriteString(fmt.Sprintf("📋 [%s] %s\n", t.ID, t.Name))
		b.WriteString(fmt.Sprintf("   操作: %s\n", t.Action))

		// Schedule description
		sched := fmt.Sprintf("每天 %02d:%02d", t.Hour, t.Minute)
		if t.DayOfWeek >= 0 && t.DayOfWeek <= 6 {
			sched = fmt.Sprintf("每%s %02d:%02d", weekdays[t.DayOfWeek], t.Hour, t.Minute)
		}
		if t.DayOfMonth > 0 {
			sched = fmt.Sprintf("每月%d号 %02d:%02d", t.DayOfMonth, t.Hour, t.Minute)
		}
		if t.StartDate != "" || t.EndDate != "" {
			sched += fmt.Sprintf("（%s ~ %s）", t.StartDate, t.EndDate)
		}
		b.WriteString(fmt.Sprintf("   时间: %s\n", sched))
		b.WriteString(fmt.Sprintf("   状态: %s", t.Status))
		if t.NextRunAt != nil {
			b.WriteString(fmt.Sprintf(" | 下次执行: %s", t.NextRunAt.Format("2006-01-02 15:04")))
		}
		if t.RunCount > 0 {
			b.WriteString(fmt.Sprintf(" | 已执行 %d 次", t.RunCount))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolDeleteScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	name := stringVal(args, "name")
	if id == "" && name == "" {
		return "请提供 id 或 name 参数"
	}
	var err error
	if id != "" {
		err = h.scheduledTaskManager.Delete(id)
	} else {
		err = h.scheduledTaskManager.DeleteByName(name)
	}
	if err != nil {
		return fmt.Sprintf("删除失败: %s", err.Error())
	}
	h.app.emitEvent("scheduled-tasks-changed")
	return "✅ 定时任务已删除"
}

func (h *IMMessageHandler) toolUpdateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "定时任务管理器未初始化"
	}
	id := stringVal(args, "id")
	if id == "" {
		return "缺少 id 参数"
	}
	err := h.scheduledTaskManager.Update(id, args)
	if err != nil {
		return fmt.Sprintf("更新失败: %s", err.Error())
	}
	h.app.emitEvent("scheduled-tasks-changed")
	// Show updated task info.
	if t := h.scheduledTaskManager.Get(id); t != nil {
		next := "-"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		return fmt.Sprintf("✅ 定时任务已更新\nID: %s\n名称: %s\n操作: %s\n时间: %02d:%02d\n下次执行: %s", t.ID, t.Name, t.Action, t.Hour, t.Minute, next)
	}
	return "✅ 定时任务已更新"
}

// ---------- ClawNet Knowledge Tools ----------

func (h *IMMessageHandler) toolClawNetSearch(args map[string]interface{}) string {
	if h.app.clawNetClient == nil || !h.app.clawNetClient.IsRunning() {
		return "虾网未连接，请先在设置中启用 ClawNet"
	}
	query := stringVal(args, "query")
	if query == "" {
		return "缺少 query 参数"
	}
	entries, err := h.app.clawNetClient.SearchKnowledge(query)
	if err != nil {
		return fmt.Sprintf("搜索失败: %s", err.Error())
	}
	if len(entries) == 0 {
		return fmt.Sprintf("未找到与「%s」相关的知识条目", query)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🔍 虾网知识搜索「%s」— 找到 %d 条:\n\n", query, len(entries)))
	for i, e := range entries {
		if i >= 10 {
			b.WriteString(fmt.Sprintf("... 还有 %d 条结果\n", len(entries)-10))
			break
		}
		b.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, e.Title))
		if e.Body != "" {
			body := e.Body
			if len(body) > 200 {
				body = body[:200] + "…"
			}
			b.WriteString(fmt.Sprintf("   %s\n", body))
		}
		if e.Author != "" {
			b.WriteString(fmt.Sprintf("   — %s", e.Author))
		}
		if e.Domain != "" {
			b.WriteString(fmt.Sprintf(" [%s]", e.Domain))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolClawNetPublish(args map[string]interface{}) string {
	if h.app.clawNetClient == nil || !h.app.clawNetClient.IsRunning() {
		return "虾网未连接，请先在设置中启用 ClawNet"
	}
	title := stringVal(args, "title")
	body := stringVal(args, "body")
	if title == "" {
		return "缺少 title 参数"
	}
	if body == "" {
		return "缺少 body 参数"
	}
	entry, err := h.app.clawNetClient.PublishKnowledge(title, body)
	if err != nil {
		return fmt.Sprintf("发布失败: %s", err.Error())
	}
	return fmt.Sprintf("✅ 知识已发布到虾网\nID: %s\n标题: %s", entry.ID, entry.Title)
}

func (h *IMMessageHandler) toolQueryAuditLog(args map[string]interface{}) string {
	if h.app == nil || h.app.auditLog == nil {
		return "审计日志未初始化"
	}

	filter := AuditFilter{}
	if since := stringVal(args, "since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.StartTime = &t
		}
	}
	if until := stringVal(args, "until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.EndTime = &t
		}
	}
	if tn := stringVal(args, "tool_name"); tn != "" {
		filter.ToolName = tn
	}
	if rl := stringVal(args, "risk_level"); rl != "" {
		filter.RiskLevels = []RiskLevel{RiskLevel(rl)}
	}

	entries, err := h.app.auditLog.Query(filter)
	if err != nil {
		return fmt.Sprintf("查询失败: %s", err.Error())
	}

	limit := 20
	if l, ok := args["limit"]; ok {
		if lf, ok := l.(float64); ok && lf > 0 {
			limit = int(lf)
		}
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	if len(entries) == 0 {
		return "没有找到匹配的审计记录"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("找到 %d 条审计记录:\n\n", len(entries)))
	for i, e := range entries {
		b.WriteString(fmt.Sprintf("%d. [%s] %s | 风险: %s | 决策: %s | 结果: %s\n",
			i+1, e.Timestamp.Format("01-02 15:04"), e.ToolName, e.RiskLevel, e.PolicyAction, e.Result))
	}
	return b.String()
}

