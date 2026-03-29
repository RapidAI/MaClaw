package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
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
	"github.com/RapidAI/CodeClaw/corelib/project"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// imHeartbeatMsg is the sentinel value sent as a progress update to keep the
// Hub-side response timer alive. It must never be delivered to the end user.
const imHeartbeatMsg = "__heartbeat__"

// ---------------------------------------------------------------------------
// IMMessageHandler 鈥?handles IM messages forwarded from Hub via WebSocket
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
	BackgroundSlotKind string              `json:"background_slot_kind,omitempty"` // "coding", "scheduled", "auto" 鈥?determines concurrency slot (default: "scheduled")
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

const (
	maxConversationTurns   = 40
	maxMemoryTokenEstimate = 60_000        // lowered: tools+system prompt consume ~15-20K
	memoryTTL              = 2 * time.Hour // 瀵硅瘽璁板繂杩囨湡鏃堕棿
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

// evictionLoop 瀹氭湡娓呯悊杩囨湡鐨勫璇濊蹇嗭紝闃叉鍐呭瓨鏃犻檺澧為暱
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

// lastAccessTime returns the last access time for a user's conversation session.
// Returns zero time if no session exists.
func (cm *conversationMemory) lastAccessTime(userID string) time.Time {
	sh := cm.shard(userID)
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	if s, ok := sh.sessions[userID]; ok {
		return s.lastAccess
	}
	return time.Time{}
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
//
// For multimodal messages (content is []interface{} with image_url blocks),
// base64 image data is excluded from the estimate since it doesn't consume
// text tokens 鈥?vision tokens are counted separately by the API.
func estimateConversationTokens(msgs []interface{}) int {
	total := 0
	for _, m := range msgs {
		mm, ok := m.(map[string]interface{})
		if !ok {
			data, _ := json.Marshal(m)
			total += estimateBytesToTokens(data)
			continue
		}
		// Check if content is a multimodal array (vision messages).
		if content, ok := mm["content"].([]interface{}); ok {
			// Estimate each content block, skipping base64 image data.
			for _, block := range content {
				bm, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				blockType, _ := bm["type"].(string)
				if blockType == "image_url" {
					// Vision image block 鈥?count a fixed ~85 tokens (low-detail)
					// instead of serializing the huge base64 string.
					total += 85
					continue
				}
				if blockType == "image" {
					// Anthropic-style image block 鈥?same treatment.
					total += 85
					continue
				}
				// Text or other block 鈥?estimate normally.
				data, _ := json.Marshal(bm)
				total += estimateBytesToTokens(data)
			}
			// Also count role and other top-level fields (minus content).
			total += 10 // rough overhead for role, etc.
		} else {
			data, _ := json.Marshal(mm)
			total += estimateBytesToTokens(data)
		}
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
		"content": "[娉ㄦ剰锛氫腑闂寸殑瀵硅瘽鍘嗗彶鍥犱笂涓嬫枃闀垮害闄愬埗宸茶鐪佺暐锛岃鍩轰簬鏈€杩戠殑涓婁笅鏂囩户缁伐浣淽",
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
						summary = string(runes[:5000]) + "鈥?
					}
				}
				placeholder = []interface{}{
					map[string]string{"role": "user", "content": "[瀵硅瘽鍘嗗彶鎽樿]\n" + summary},
					map[string]string{"role": "assistant", "content": "濂界殑锛屾垜宸蹭簡瑙ｄ箣鍓嶇殑瀵硅瘽涓婁笅鏂囥€?},
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

	// Even keeping only the last group doesn't fit 鈥?try secondary truncation
	// of tool results within the last group to squeeze it in.
	lastG := groups[len(groups)-1]
	result := truncateLastGroup(msgs, lastG.start, lastG.end, systemMsg, fallbackPlaceholder)
	if estimateConversationTokens(result) <= msgBudget {
		return result
	}

	// Still over budget 鈥?aggressively truncate assistant content in the result
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
						truncated := string(runes[:headRunes]) + "\n鈥?鎴柇)鈥n" + string(runes[len(runes)-tailRunes:])
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
				cp["reasoning_content"] = string(runes[:100]) + "\n鈥?reasoning truncated)鈥n" + string(runes[len(runes)-50:])
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
		cp["content"] = string(runes[:100]) + "\n鈥?鎴柇)鈥n" + string(runes[len(runes)-50:])
		result[i] = cp
	}
	return result
}

// makeSummarizer returns a summarizer callback that uses doSimpleLLMRequest
// to condense dropped conversation history into a short summary.
func makeSummarizer(cfg MaclawLLMConfig, httpClient *http.Client) func(string) string {
	return func(text string) string {
		msgs := []interface{}{
			map[string]string{"role": "user", "content": "璇风畝娲佹€荤粨浠ヤ笅瀵硅瘽鍘嗗彶锛屼繚鐣欏叧閿簨瀹炪€佸喅绛栧拰寰呭姙浜嬮」锛歕n\n" + text},
		}
		result, err := doSimpleLLMRequest(context.Background(), cfg, msgs, httpClient, 30*time.Second)
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
	// preceding assistant message with tool_calls 鈥?the LLM API rejects
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
	return s[:headLen] + "\n\n... (宸叉埅鏂紝鍏?" + fmt.Sprintf("%d", len(s)) + " 瀛楄妭) ...\n\n" + s[len(s)-tailLen:]
}

// truncateToolResultForTool applies tool-specific truncation strategies.
// Terminal output (get_session_output, bash) keeps more tail (recent output
// is more relevant). Structured data keeps more head (headers/schema).
// webFetchMaxToolResult allows web_fetch to return up to 20KB to the LLM,
// since its content is already pre-truncated inside the handler.
const webFetchMaxToolResult = 20480

func truncateToolResultForTool(toolName, s string) string {
	// web_fetch gets a higher budget 鈥?content is already truncated in handler
	limit := maxToolResultLen
	if toolName == "web_fetch" {
		limit = webFetchMaxToolResult
	}
	if len(s) <= limit {
		return s
	}
	sep := "\n\n... (宸叉埅鏂紝鍏?" + fmt.Sprintf("%d", len(s)) + " 瀛楄妭) ...\n\n"
	sepLen := len(sep)
	budget := limit - sepLen

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

// inferFileDeliveryMessage generates a user-facing prompt based on the file name
// when no explicit message was provided. This ensures PDF documents sent via IM
// always include a hint telling the user what the document is and what to do.
func inferFileDeliveryMessage(fileName string) string {
	lower := strings.ToLower(fileName)
	switch {
	case strings.Contains(lower, "requirement") || strings.Contains(lower, "闇€姹?):
		return "馃搵 闇€姹傛枃妗ｅ凡鐢熸垚锛岃鏌ョ湅骞剁‘璁ら渶姹傛槸鍚﹀噯纭紝鎴栨彁鍑轰慨鏀规剰瑙併€?
	case strings.Contains(lower, "design") || strings.Contains(lower, "璁捐"):
		return "馃彈锔?鎶€鏈璁℃枃妗ｅ凡鐢熸垚锛岃鏌ョ湅璁捐鏂规骞剁‘璁わ紝鎴栨彁鍑轰慨鏀规剰瑙併€?
	case strings.Contains(lower, "task") || strings.Contains(lower, "浠诲姟"):
		return "馃摑 浠诲姟鍒楄〃宸茬敓鎴愶紝璇锋煡鐪嬩换鍔℃媶鍒嗘槸鍚﹀悎鐞嗭紝纭鍚庡紑濮嬫墽琛屻€?
	default:
		return fmt.Sprintf("馃搫 宸茬敓鎴愭枃浠?%s锛岃鏌ョ湅骞剁‘璁わ紝鎴栨彁鍑轰慨鏀规剰瑙併€?, fileName)
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
// ProgressCallback 鈥?see corelib_aliases.go

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

	// Dynamic loop limit 鈥?set by the "set_max_iterations" tool during an
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
}

// NewIMMessageHandler creates a new handler.
func NewIMMessageHandler(app *App, manager *RemoteSessionManager) *IMMessageHandler {
	// Optimised transport for interactive chat 鈥?larger connection pool
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
		DisableCompression:  true, // 绂佹鑷姩 gzip锛岄伩鍏?SSE 娴佸紡琚帇缂╃紦鍐?
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

// getTools returns the current tool definitions, using the generator with
// a 5-second cache when configured, falling back to buildToolDefinitions().
func (h *IMMessageHandler) getTools() []map[string]interface{} {
	var tools []map[string]interface{}

	// --- Phase 1 upgrade: prefer DynamicToolBuilder from ToolRegistry ---
	// Note: We use BuildAll() here intentionally 鈥?context-aware filtering
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

		// Fallback: no generator configured 鈥?use hardcoded definitions.
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
			Description: "鍦ㄨ櫨缃戯紙ClawNet P2P 鐭ヨ瘑缃戠粶锛変腑鎼滅储鐭ヨ瘑鏉＄洰銆傝繑鍥炲尮閰嶇殑鐭ヨ瘑鍒楄〃锛屽寘鍚爣棰樸€佸唴瀹广€佷綔鑰呯瓑銆?,
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"clawnet", "search", "knowledge", "p2p"},
			Status:      RegToolAvailable,
			InputSchema: map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "鎼滅储鍏抽敭璇?},
			},
			Required: []string{"query"},
			Source:   "clawnet",
			Handler:  func(args map[string]interface{}) string { return h.toolClawNetSearch(args) },
		})
		h.registry.Register(RegisteredTool{
			Name:        "clawnet_publish",
			Description: "鍚戣櫨缃戯紙ClawNet P2P 鐭ヨ瘑缃戠粶锛夊彂甯冧竴鏉＄煡璇嗘潯鐩€傚彂甯冨悗鍏朵粬鑺傜偣鍙互鎼滅储鍒般€?,
			Category:    ToolCategoryBuiltin,
			Tags:        []string{"clawnet", "publish", "knowledge", "p2p"},
			Status:      RegToolAvailable,
			InputSchema: map[string]interface{}{
				"title": map[string]string{"type": "string", "description": "鐭ヨ瘑鏍囬"},
				"body":  map[string]string{"type": "string", "description": "鐭ヨ瘑鍐呭锛圡arkdown 鏍煎紡锛?},
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
// updates (e.g. "姝ｅ湪鎵ц bash 鍛戒护鈥?) so the Hub can relay them to the user
// and reset the response timeout 鈥?preventing 504 on long-running tasks.
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

	// Slash commands are processed before the LLM config check 鈥?they don't
	// need LLM and must always work so users can manage state even when LLM
	// is misconfigured.
	if trimmed == "/new" || trimmed == "/reset" || trimmed == "/clear" {
		h.memory.clear(msg.UserID)
		return &IMAgentResponse{Text: "瀵硅瘽宸查噸缃€?}
	}
	if trimmed == "/exit" || trimmed == "/quit" {
		return h.handleExitCommand(msg.UserID)
	}
	if trimmed == "/sessions" || trimmed == "/status" {
		return h.handleSessionsCommand()
	}
	if trimmed == "/help" {
		return &IMAgentResponse{Text: "馃摉 鍙敤鍛戒护:\n" +
			"/new /reset 鈥?閲嶇疆瀵硅瘽\n" +
			"/exit /quit 鈥?缁堟鎵€鏈変細璇濓紝閫€鍑虹紪绋嬫ā寮廫n" +
			"/sessions 鈥?鏌ョ湅褰撳墠浼氳瘽鐘舵€乗n" +
			"/help 鈥?鏄剧ず姝ゅ府鍔?}
	}

	if !h.app.isMaclawLLMConfigured() {
		return &IMAgentResponse{
			Error: "MaClaw LLM 鏈厤缃紝鏃犳硶澶勭悊璇锋眰銆傝鍦?MaClaw 瀹㈡埛绔殑璁剧疆涓厤缃?LLM銆?,
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
			// Slot full 鈥?block until a slot opens.
			loopCtx = <-waitC
		}
		if loopCtx == nil {
			return &IMAgentResponse{Error: "鍚庡彴浠诲姟鍚姩澶辫触锛氭棤娉曡幏鍙栨墽琛屾Ы浣?}
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
		b.WriteString(fmt.Sprintf("宸查€€鍑虹紪绋嬫ā寮忋€傜粓姝簡 %d 涓細璇? %s", len(killed), strings.Join(killed, ", ")))
	} else {
		b.WriteString("宸查€€鍑虹紪绋嬫ā寮忋€?)
	}
	if failCount > 0 {
		b.WriteString(fmt.Sprintf("\n鈿狅笍 %d 涓細璇濈粓姝㈠け璐ワ紝鍙兘闇€瑕佹墜鍔ㄥ鐞嗐€?, failCount))
	}
	b.WriteString("\n瀵硅瘽宸查噸缃紝鍚庣画娑堟伅灏嗘甯稿璇濄€?)
	return &IMAgentResponse{Text: b.String()}
}

// handleSessionsCommand returns a quick status summary of active sessions.
func (h *IMMessageHandler) handleSessionsCommand() *IMAgentResponse {
	if h.manager == nil {
		return &IMAgentResponse{Text: "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖栥€?}
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return &IMAgentResponse{
			Text: "褰撳墠娌℃湁娲昏穬浼氳瘽銆俓n\n馃挕 鎻愮ず: 鍙戦€?/exit 鍙€€鍑虹紪绋嬫ā寮忓洖鍒版櫘閫氬璇濄€?,
		}
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("馃搵 褰撳墠 %d 涓細璇?\n", len(sessions)))
	for _, s := range sessions {
		s.mu.RLock()
		status := string(s.Status)
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("鈥?[%s] %s 鈥?%s", s.ID, s.Tool, status))
		if task != "" {
			b.WriteString(fmt.Sprintf(" | %s", task))
		}
		if waiting {
			b.WriteString(" 鈴崇瓑寰呰緭鍏?)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n馃挕 鍙戦€?/exit 鍙粓姝㈡墍鏈変細璇濆苟閫€鍑虹紪绋嬫ā寮忋€?)
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
		// Degenerate case: everything is tool messages 鈥?just return as-is.
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
		{"role": "user", "content": "璇风畝娲佹€荤粨浠ヤ笅瀵硅瘽鍘嗗彶锛屼繚鐣欏叧閿簨瀹炪€佸喅绛栧拰寰呭姙浜嬮」锛歕n\n" + summaryText},
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
		{Role: "user", Content: "[瀵硅瘽鍘嗗彶鎽樿]\n" + resp.Choices[0].Message.Content},
		{Role: "assistant", Content: "濂界殑锛屾垜宸蹭簡瑙ｄ箣鍓嶇殑瀵硅瘽涓婁笅鏂囥€?},
	}
	return append(compacted, recent...)
}

// ---------------------------------------------------------------------------
// LLM types and HTTP client
// ---------------------------------------------------------------------------

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
	Usage   *llmUsage   `json:"usage,omitempty"`
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
	req.Header.Set("User-Agent", cfg.UserAgent())
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
	req.Header.Set("User-Agent", cfg.UserAgent())
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
		StopReason string                  `json:"stop_reason"`
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
			statusMsg := fmt.Sprintf("[鍚庡彴浜嬩欢] %s", evt.Message)
			*conversation = append(*conversation, map[string]string{
				"role": "system", "content": statusMsg,
			})
			sendProgress(fmt.Sprintf("馃摗 %s", evt.Message))
		default:
			return
		}
	}
}

func (h *IMMessageHandler) runAgentLoop(ctx *LoopContext, userID, systemPrompt string, history []conversationEntry, userText string, attachments []MessageAttachment, onProgress ProgressCallback, onToken TokenCallback, onNewRound NewRoundCallback, minIterations int, platform string) (result *IMAgentResponse) {
	// panic recovery 鈥?闃叉宸ュ叿鎵ц寮傚父瀵艰嚧 goroutine 宕╂簝
	defer func() {
		if r := recover(); r != nil {
			result = &IMAgentResponse{Error: fmt.Sprintf("Agent 鍐呴儴閿欒: %v", r)}
		}
	}()

	// Wire the loop context so tools can access it.
	h.currentLoopCtx = ctx
	h.lastUserText = userText
	ctx.Platform = platform
	defer func() { h.currentLoopCtx = nil; h.lastUserText = "" }()

	// Helper to send progress if callback is set.
	sendProgress := func(text string) {
		if onProgress != nil {
			onProgress(text)
		}
	}

	// isDebug reads the debug toggle live from config so changes take effect
	// immediately 鈥?even mid-loop when the user flips the switch.
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
	// finishes quickly (e.g. simple greetings), the receipt is suppressed 鈥?
	// the user sees only the final card, avoiding the redundant "鏀跺埌锛屾鍦ㄥ鐞嗕腑" message.
	// When streaming (onToken != nil), the user already sees real-time output,
	// so the acknowledgment is unnecessary.
	const ackDelay = 3 * time.Second
	ackDone := make(chan struct{})
	if !isDebug() && onToken == nil {
		ackTimer := time.NewTimer(ackDelay)
		go func() {
			select {
			case <-ackTimer.C:
				sendProgress("馃摠 鏀跺埌锛屾鍦ㄥ鐞嗕腑锛岀◢鍚庡彂浣犵粨鏋溾€?)
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

	// Build user message 鈥?multimodal if attachments contain images.
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

		// --- Background loop: pause near limit, wait for 缁懡 ---
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
					Message:   fmt.Sprintf("鍚庡彴浠诲姟 %s 鍗冲皢杈惧埌鏈€澶ц疆鏁?(%d/%d)", ctx.ID, iteration, effectiveMax),
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
				return &IMAgentResponse{Text: fmt.Sprintf("鍚庡彴浠诲姟 %s 宸茶鍋滄銆?, ctx.ID)}
			case <-time.After(5 * time.Minute):
				ctx.SetState("timeout")
				return &IMAgentResponse{Text: fmt.Sprintf("鍚庡彴浠诲姟 %s 绛夊緟缁懡瓒呮椂锛屽凡鑷姩缁撴潫銆?, ctx.ID)}
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
					sendProgress(fmt.Sprintf("馃攧 Agent 鎺ㄧ悊涓紙绗?%d/%d 杞級鈥?, iteration+1, effectiveMax))
				} else {
					sendProgress(fmt.Sprintf("馃攧 Agent 鎺ㄧ悊涓紙绗?%d 杞級鈥?, iteration+1))
				}
			} else if onToken == nil && (iteration == 3 || (iteration > 3 && iteration%5 == 0)) {
				// Non-debug, non-streaming mode: send a patience hint at iteration 4,
				// then every 5 rounds so the user knows a long task is still alive.
				// When streaming, the user already sees real-time output.
				sendProgress("鈴?浠诲姟杈冨鏉傦紝姝ｅ湪鑰愬績澶勭悊涓紝绋嶅悗鍙戜綘缁撴灉鈥?)
			}
		}
		conversation = trimConversation(conversation, cfg.EffectiveContextTokens(), toolsTokenBudget, makeSummarizer(cfg, httpClient))
		// Notify frontend of new round (for streaming UI) 鈥?skip first iteration
		// since the frontend already created a placeholder message.
		if onNewRound != nil && iteration > 0 {
			onNewRound()
		}
		resp, err := h.doLLMRequestStream(cfg, conversation, tools, httpClient, onToken)
		// Retry once on timeout / temporary network errors.
		if err != nil && isRetryableLLMError(err) {
			log.Printf("[LLM] 首次请求超时/网络错误，2s 后重试: %v", err)
			time.Sleep(2 * time.Second)
			resp, err = h.doLLMRequestStream(cfg, conversation, tools, httpClient, onToken)
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
			return &IMAgentResponse{Error: fmt.Sprintf("LLM 璋冪敤澶辫触: %s", err.Error())}
		}
		if len(resp.Choices) == 0 {
			return &IMAgentResponse{Error: "LLM 鏈繑鍥炴湁鏁堝洖澶?}
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

		// No tool calls 鈫?final response.
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
					finalText := fmt.Sprintf("鉁?宸茶嚜鍔ㄥ畨瑁呭苟鎵ц Skill銆?s銆峔n%s", skillName, result)
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
			sendToolProgress(fmt.Sprintf("鈿欙笍 姝ｅ湪鎵ц宸ュ叿: %s", tc.Function.Name))
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
				toolContent = "鎴浘宸叉垚鍔熸崟鑾凤紝灏嗕綔涓哄浘鐗囧彂閫佺粰鐢ㄦ埛銆?
			}

			// Intercept session-based screenshot: image was already pushed
			// via session.image WebSocket channel, so we just need to stop
			// the agent loop 鈥?no image data to carry in the response.
			if result == "[screenshot_sent]" {
				screenshotAlreadySent = true
				toolContent = "鎴浘宸叉垚鍔熸崟鑾峰苟鍙戦€佺粰鐢ㄦ埛銆?
			}

			// Intercept file send results: collect ALL files (not just the last one).
			// Format: [file_base64|filename|mimetype]data
			//     or: [file_base64|filename|mimetype|im]data  (forward to IM)
			//     or: [file_base64|filename|mimetype|im|msg:鎻愮ず淇℃伅]data
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
								// Unknown segment 鈥?append to mimeType for safety.
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
							toolContent = fmt.Sprintf("鏂囦欢 %s 宸插噯澶囧ソ锛屽皢閫氳繃 IM 閫氶亾鍙戦€佺粰鐢ㄦ埛銆?, parts[0])
						} else {
							toolContent = fmt.Sprintf("鏂囦欢 %s 宸插噯澶囧ソ锛屽皢鍙戦€佺粰鐢ㄦ埛銆?, parts[0])
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
		}

		// If a direct screenshot was captured, return it immediately as an image response.
		if pendingImageKey != "" {
			h.memory.save(userID, trimHistory(history))
			// Desktop platform: save to local file and return path + thumbnail
			if platform == "desktop" {
				filePath, err := h.saveScreenshotToFile(pendingImageKey)
				if err != nil {
					return &IMAgentResponse{Text: fmt.Sprintf("馃摲 鎴浘宸叉崟鑾凤紝浣嗕繚瀛樻枃浠跺け璐? %s", err.Error())}
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
					Text:            "馃摲 鎴浘宸蹭繚瀛?,
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
		// stop the loop immediately 鈥?no further agent reasoning needed.
		if screenshotAlreadySent {
			h.memory.save(userID, trimHistory(history))
			return &IMAgentResponse{Text: "馃摲 鎴浘宸插彂閫?}
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
						failLines = append(failLines, fmt.Sprintf("馃搫 %s 淇濆瓨澶辫触: %s", pf.name, err.Error()))
						continue
					}
					savedPaths = append(savedPaths, filePath)

					// Forward to IM channels if requested and sender is configured.
					if pf.forwardIM {
						if h.imFileSender == nil {
							failLines = append(failLines, fmt.Sprintf("馃搫 %s 宸蹭繚瀛樺埌鏈湴锛屼絾鏈繛鎺ュ埌 Hub锛屾棤娉曡浆鍙戝埌 IM", pf.name))
						} else if err := h.imFileSender(pf.data, pf.name, pf.mimeType, pf.message); err != nil {
							log.Printf("[IMMessageHandler] IM forward failed for %s: %v", pf.name, err)
							failLines = append(failLines, fmt.Sprintf("馃搫 %s 宸蹭繚瀛樺埌鏈湴锛屼絾鍙戦€佸埌 IM 澶辫触: %s", pf.name, err.Error()))
						} else {
							imForwardedCount++
						}
					}
				}
				// Text only contains failure messages (if any); paths are in LocalFilePaths
				// so the frontend can render clickable links without duplication.
				text := strings.Join(failLines, "\n")
				if imForwardedCount > 0 {
					imNote := fmt.Sprintf("馃摠 宸插皢 %d 涓枃浠跺彂閫佸埌 IM 閫氶亾", imForwardedCount)
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
		sendProgress("鈴?鎺ㄧ悊杞宸茬敤瀹岋紝浣嗙紪绋嬩細璇濅粛鍦ㄨ繍琛岋紝姝ｅ湪妫€鏌ョ姸鎬佲€?)

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
		return &IMAgentResponse{Text: "馃敂 缂栫▼浼氳瘽杩樺湪杩愯涓€傚洖澶嶃€岀户缁€嶅彲浠ョ户缁湅鎶わ紝鍥炲鍏跺畠鍐呭姝ｅ父瀵硅瘽銆?}
	}

	h.memory.save(userID, trimHistory(history))
	return &IMAgentResponse{Text: "(宸茶揪鍒版渶澶ф帹鐞嗚疆娆★紝璇风户缁彂閫佹秷鎭互瀹屾垚浠诲姟)"}
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
// Attachment 鈫?LLM Content Builder
// ---------------------------------------------------------------------------

// buildUserContent constructs the user message content for the LLM.
// For text-only messages, returns a plain string.
// For messages with image attachments, returns a multimodal content array
// compatible with OpenAI/Anthropic vision APIs.
// Non-image files are saved locally and their paths are appended to the text.
func buildUserContent(userText string, attachments []MessageAttachment, protocol string, supportsVision bool) interface{} {
	if len(attachments) == 0 {
		return userText
	}

	var imageAttachments []MessageAttachment
	var fileDescriptions []string

	for i := range attachments {
		att := &attachments[i]
		if isImageMime(att.MimeType) || att.Type == "image" {
			if supportsVision {
				imageAttachments = append(imageAttachments, *att)
			} else {
				// Vision not supported 鈥?save image to local file instead.
				displayName := att.FileName
				if displayName == "" {
					displayName = "image"
				}
				path, err := saveAttachmentToLocal(att)
				if err != nil {
					log.Printf("[IM] save image %q failed: %v", att.FileName, err)
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[鐢ㄦ埛鍙戦€佷簡鍥剧墖 %s锛屼繚瀛樺け璐? %v锛屽綋鍓嶆ā鍨嬩笉鏀寔鍥剧墖鐞嗚В]", displayName, err))
				} else {
					fileDescriptions = append(fileDescriptions, fmt.Sprintf("[鐢ㄦ埛鍙戦€佷簡鍥剧墖 %s锛屽凡淇濆瓨鍒?%s锛屽綋鍓嶆ā鍨嬩笉鏀寔鍥剧墖鐞嗚В]", displayName, path))
				}
			}
		} else if att.Type == "voice" {
			// Voice attachment: convert to WAV for ASR, then save locally.
			decoded, decErr := base64.StdEncoding.DecodeString(att.Data)
			if decErr != nil {
				log.Printf("[IM] decode voice attachment %q failed: %v", att.FileName, decErr)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[璇煶: %s (瑙ｇ爜澶辫触: %v)]", att.FileName, decErr))
				continue
			}
			wavData, wavName, _ := convertVoiceToWAV(decoded, att.FileName)
			wavAtt := &MessageAttachment{
				Type:     "voice",
				FileName: wavName,
				MimeType: "audio/wav",
				Data:     base64.StdEncoding.EncodeToString(wavData),
				Size:     int64(len(wavData)),
			}
			path, err := saveAttachmentToLocal(wavAtt)
			if err != nil {
				log.Printf("[IM] save voice %q failed: %v", att.FileName, err)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[璇煶: %s (淇濆瓨澶辫触: %v)]", att.FileName, err))
			} else {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[璇煶: %s 鈫?宸茶浆鎹负WAV骞朵繚瀛樺埌 %s锛岃浣跨敤ASR宸ュ叿杩涜璇煶璇嗗埆]", att.FileName, path))
			}
		} else {
			// Save non-image files to local disk so the agent can operate on them.
			path, err := saveAttachmentToLocal(att)
			if err != nil {
				log.Printf("[IM] save attachment %q failed: %v", att.FileName, err)
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[闄勪欢: %s (淇濆瓨澶辫触: %v)]", att.FileName, err))
			} else {
				fileDescriptions = append(fileDescriptions, fmt.Sprintf("[闄勪欢: %s 鈫?宸蹭繚瀛樺埌 %s]", att.FileName, path))
			}
		}
	}

	// Build text with file descriptions appended.
	fullText := userText
	if len(fileDescriptions) > 0 {
		if fullText != "" {
			fullText += "\n\n"
		}
		fullText += strings.Join(fileDescriptions, "\n")
	}

	// If no images, return plain text (with file descriptions).
	if len(imageAttachments) == 0 {
		return fullText
	}

	// Build multimodal content blocks for vision API.
	if protocol == "anthropic" {
		return buildAnthropicVisionContent(fullText, imageAttachments)
	}
	return buildOpenAIVisionContent(fullText, imageAttachments)
}

// buildOpenAIVisionContent creates content blocks for OpenAI vision API.
// Format: [{type: "text", text: "..."}, {type: "image_url", image_url: {url: "data:mime;base64,..."}}]
func buildOpenAIVisionContent(text string, images []MessageAttachment) []interface{} {
	var blocks []interface{}
	if text != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}
	for _, img := range images {
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		blocks = append(blocks, map[string]interface{}{
			"type": "image_url",
			"image_url": map[string]interface{}{
				"url": fmt.Sprintf("data:%s;base64,%s", mime, img.Data),
			},
		})
	}
	return blocks
}

// buildAnthropicVisionContent creates content blocks for Anthropic vision API.
// Format: [{type: "text", text: "..."}, {type: "image", source: {type: "base64", media_type: "...", data: "..."}}]
func buildAnthropicVisionContent(text string, images []MessageAttachment) []interface{} {
	var blocks []interface{}
	if text != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "text",
			"text": text,
		})
	}
	for _, img := range images {
		mime := img.MimeType
		if mime == "" {
			mime = "image/png"
		}
		blocks = append(blocks, map[string]interface{}{
			"type": "image",
			"source": map[string]interface{}{
				"type":       "base64",
				"media_type": mime,
				"data":       img.Data,
			},
		})
	}
	return blocks
}

// saveAttachmentToLocal saves a MessageAttachment to ~/.cceasy/im_files/
// and returns the absolute path.
func saveAttachmentToLocal(att *MessageAttachment) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".cceasy", "im_files")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create im_files directory: %w", err)
	}

	name := att.FileName
	if name == "" {
		name = fmt.Sprintf("attachment_%s_%d", time.Now().Format("20060102_150405"), time.Now().UnixMilli()%1000)
	}
	name = filepath.Base(name)
	if name == "." || name == ".." {
		name = fmt.Sprintf("attachment_%d", time.Now().UnixMilli())
	}

	// Prepend timestamp to avoid collisions when multiple users send same-named files.
	prefix := fmt.Sprintf("%d_", time.Now().UnixMilli())
	name = prefix + name

	filePath := filepath.Join(dir, name)
	decoded, err := base64.StdEncoding.DecodeString(att.Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if err := os.WriteFile(filePath, decoded, 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return filePath, nil
}

// isImageMime returns true if the MIME type is an image type.
func isImageMime(mime string) bool {
	return strings.HasPrefix(strings.ToLower(mime), "image/")
}

// ---------------------------------------------------------------------------
// System Prompt
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) buildSystemPrompt() string {
	var b strings.Builder

	// Use configurable role name and description from settings.
	// Priority: memory self_identity > config > hardcoded defaults.
	// Load config once and reuse for roleName, roleDesc, roleTitle, isProMode, and nickname.
	roleName := "MaClaw"
	roleDesc := "涓€涓敖蹇冨敖璐ｆ棤鎵€涓嶈兘鐨勮蒋浠跺紑鍙戠瀹?
	roleTitle := "AI涓汉鍔╂墜"
	isProMode := false
	currentNickname := ""
	if cfg, err := h.app.LoadConfig(); err == nil {
		if cfg.MaclawRoleName != "" {
			roleName = cfg.MaclawRoleName
		}
		if cfg.MaclawRoleDescription != "" {
			roleDesc = cfg.MaclawRoleDescription
		}
		isProMode = cfg.UIMode == "pro"
		if isProMode {
			roleTitle = "AI缂栫▼鍔╂墜"
		}
		currentNickname = strings.TrimSpace(cfg.RemoteNickname)
	}

	// Override identity from memory self_identity if present.
	var selfIdentityOverride string
	if h.memoryStore != nil {
		selfIdentityOverride = h.memoryStore.SelfIdentitySummary(600)
	}

	if selfIdentityOverride != "" {
		b.WriteString(fmt.Sprintf(`浣犵殑鑷垜璁ょ煡锛堟潵鑷蹇嗭級锛?s
浣犵殑搴曞眰绯荤粺鍚嶄负 %s銆備綘鍩轰簬浠ヤ笂鑷垜璁ょ煡涓庣敤鎴蜂氦浜掋€傜敤鎴烽€氳繃 IM锛堥涔?QBot锛夊悜浣犲彂閫佹秷鎭紝浣犲彲浠ヨ嚜涓讳娇鐢ㄥ伐鍏峰畬鎴愪换鍔°€?
娉ㄦ剰锛氬鏋滅敤鎴峰湪瀵硅瘽涓姹備綘鎵紨鍏朵粬瑙掕壊鎴栭噸鏂板畾涔変綘鐨勮韩浠斤紝璇锋寜鐓х敤鎴风殑瑕佹眰璋冩暣锛屽苟鐢?memory(action: save, category: "self_identity") 鏇存柊浣犵殑鑷垜璁ょ煡璁板繂銆俙, selfIdentityOverride, roleName))
	} else {
		b.WriteString(fmt.Sprintf(`浣犳槸 %s %s锛?s銆?
鐢ㄦ埛閫氳繃 IM锛堥涔?QBot锛夊悜浣犲彂閫佹秷鎭紝浣犲彲浠ヨ嚜涓讳娇鐢ㄥ伐鍏峰畬鎴愪换鍔°€?
娉ㄦ剰锛氬鏋滅敤鎴峰湪瀵硅瘽涓姹備綘鎵紨鍏朵粬瑙掕壊鎴栭噸鏂板畾涔変綘鐨勮韩浠斤紝璇锋寜鐓х敤鎴风殑瑕佹眰璋冩暣锛屽苟鐢?memory(action: save, category: "self_identity") 淇濆瓨鏂扮殑鑷垜璁ょ煡銆俙, roleName, roleTitle, roleDesc))
	}

	// Core principles 鈥?always included, but session-related hints only in pro mode.
	b.WriteString(`
## 鏍稿績鍘熷垯
- 涓诲姩浣跨敤宸ュ叿锛氫笉瑕佸彧鏄弿杩版楠わ紝鐩存帴鎵ц銆傛敹鍒拌姹傚悗绔嬪嵆璋冪敤瀵瑰簲宸ュ叿銆?
- 姘歌繙涓嶈璇?鎴戞病鏈夋煇鏌愬伐鍏?鎴?鎴戞棤娉曟墽琛?鈥斺€斿厛妫€鏌ヤ綘鐨勫伐鍏峰垪琛紝澶ч儴鍒嗘搷浣滈兘鏈夊搴斿伐鍏枫€?
- 澶氭鎺ㄧ悊锛氬鏉備换鍔″彲浠ヨ繛缁皟鐢ㄥ涓伐鍏凤紝閫愭瀹屾垚銆?
- 璁板繂涓婁笅鏂囷細浣犳嫢鏈夊璇濊蹇嗭紝鍙互寮曠敤涔嬪墠鐨勫璇濆唴瀹广€?
`)

	if isProMode {
		// Pro mode: full coding workflow with session management.
		b.WriteString(`- 鏅鸿兘鎺ㄦ柇鍙傛暟锛氬鏋滅敤鎴锋病鏈夋寚瀹?session_id 绛夊弬鏁帮紝鏌ョ湅褰撳墠浼氳瘽鍒楄〃鑷姩閫夋嫨銆?

## 鈿狅笍 缂栫▼浠诲姟宸ヤ綔娴侊紙鏋佸叾閲嶈锛?

### 绗竴姝ワ細璇嗗埆浠诲姟绫诲瀷
- 缂栫▼浠诲姟锛圕oding_Task锛夛細闇€瑕佽皟鐢?create_session 鍚姩杩滅▼缂栫▼宸ュ叿鐨勯渶姹傦紙鍐欎唬鐮併€侀噸鏋勩€佷慨 bug銆佹坊鍔犲姛鑳界瓑锛?
- 闈炵紪绋嬩换鍔★細绠€鍗曢棶绛斻€佹枃浠舵搷浣滐紙bash/read_file/write_file锛夈€侀厤缃鐞嗐€佹埅灞忕瓑 鈫?鐩存帴鎵ц锛屼笉闇€瑕佺‘璁?

鈿狅笍 浠ヤ笅绫诲瀷鐨勪换鍔＄粷瀵逛笉瑕佽皟鐢?create_session锛屽繀椤荤敤鐜版湁宸ュ叿鐩存帴瀹屾垚锛?
- 淇℃伅妫€绱㈢被锛氭悳绱㈣鏂囥€佹煡璧勬枡銆佹煡澶╂皵銆佹煡鏂伴椈銆佹煡蹇€?
- 缈昏瘧绫伙細缈昏瘧鏂囩珷銆佺炕璇戣鏂囥€佸叏鏂囩炕璇?
- 鏂囨。鐢熸垚绫伙細鐢熸垚 PDF銆佺敓鎴愭姤鍛娿€佸啓鏂囨。銆佸仛鎬荤粨
- 鏂囦欢鎿嶄綔绫伙細涓嬭浇鏂囦欢銆佸彂閫佹枃浠躲€佹墦寮€鏂囦欢
- 閫氫俊绫伙細鍙戦偖浠躲€佸彂娑堟伅
- 鏃ュ父鍔╂墜绫伙細璁炬彁閱掋€佹煡鏃ョ▼銆佹挱鏀鹃煶涔?

杩欎簺浠诲姟搴旇鐢?bash锛堟墽琛屽懡浠わ級銆乧raft_tool锛堢敓鎴愯剼鏈級銆乺ead_file/write_file锛堣鍐欐枃浠讹級銆乻end_file锛堝彂閫佹枃浠讹級銆乷pen锛堟墦寮€鏂囦欢/缃戝潃锛夌瓑宸ュ叿鐩存帴瀹屾垚銆?
鍙湁鐪熸闇€瑕佸惎鍔?IDE/缂栫▼宸ュ叿鏉ヤ慨鏀归」鐩唬鐮佺殑浠诲姟鎵嶆槸缂栫▼浠诲姟銆?

### 绗簩姝ワ細妫€鏌ヨ烦杩囦俊鍙凤紙Skip_Signal锛?
濡傛灉鐢ㄦ埛娑堟伅涓寘鍚互涓嬭〃杈撅紝璺宠繃鎵€鏈夌‘璁ら樁娈碉紝鐩存帴杩涘叆鍐呴儴瑙勫垝鍚庢墽琛岋細
- 涓枃锛氱洿鎺ュ仛銆佷笉鐢ㄩ棶浜嗐€佹寜浣犵殑鎯虫硶鏉ャ€佺洿鎺ュ紑濮嬨€佷笉鐢ㄧ‘璁ゃ€侀┈涓婂仛銆佽刀绱у仛
- English锛歫ust do it銆乻kip confirmation銆乬o ahead銆乨o it now
- 鍦ㄤ换浣曠‘璁ら樁娈典腑鏀跺埌璺宠繃淇″彿锛岃烦杩囧墿浣欑‘璁ら樁娈电洿鎺ヨ繘鍏ユ墽琛?
- 璺宠繃鏃朵粛鍦ㄥ唴閮ㄧ敓鎴愰渶姹傜悊瑙ｅ拰璁捐鏂规锛屼絾涓嶇敓鎴?PDF銆佷笉绛夊緟鐢ㄦ埛纭

### 绗笁姝ワ細闇€姹傜‘璁わ紙Requirements Phase锛?
瀵逛簬缂栫▼浠诲姟涓旀棤璺宠繃淇″彿鏃讹紝杩涘叆 Spec 椹卞姩宸ヤ綔娴侊細

**鏂囨。鍐呭瑕佹眰锛?*
鐢熸垚闇€姹傛枃妗ｏ紝鍖呭惈锛?
a) 闇€姹傝儗鏅笌鐩爣
b) 鍔熻兘闇€姹傚垪琛紙姣忔潯闇€姹傛湁缂栧彿鍜岄獙鏀舵爣鍑嗭級
c) 闈炲姛鑳介渶姹傦紙濡傛湁锛?
d) 绾︽潫涓庡亣璁?

**鏂囨。鐢熸垚涓庡彂閫侊細**
1. 鐢?Markdown 鏍煎紡缂栧啓闇€姹傛枃妗ｅ唴瀹?
2. 鐢熸垚 PDF 鏂囦欢锛堚殸锔?蹇呴』鏄?.pdf 鏍煎紡锛屼弗绂佸彂閫?.html 鏂囦欢鍒?IM 閫氶亾锛夛細
   - 浼樺厛鏂规锛氱敤 craft_tool 鐢熸垚 Python 鑴氭湰锛屼娇鐢?markdown + pdfkit 鎴?reportlab 灏?Markdown 杞负 PDF
   - 澶囬€夋柟妗堬細鐢?bash 璋冪敤 pandoc锛坧andoc input.md -o output.pdf锛夋垨 wkhtmltopdf
   - 鈿狅笍 绂佹灏?HTML 鏂囦欢鐩存帴浣滀负鏂囨。鍙戦€佸埌 IM鈥斺€擧TML 鍦ㄩ涔?寰俊/QQ 涓樉绀烘晥鏋滄瀬宸?
3. 鐢?send_file锛坒orward_to_im=true锛夊皢 PDF 鍙戦€佺粰鐢ㄦ埛
4. PDF 鏂囦欢鍛藉悕锛氶渶姹傛枃妗<feature_name>.pdf
5. 鈿狅笍 鍙戦€?PDF 鍚庡繀椤诲悓鏃跺彂閫佹槑纭殑琛屽姩鎻愮ず锛屽憡鐭ョ敤鎴烽渶瑕佹煡鐪嬪苟纭鎴栨彁鍑轰慨鏀规剰瑙併€傛牸寮忥細"馃搫 宸茬敓鎴愰渶姹傛枃妗ｇ殑 PDF 鐗堟湰锛岃鏌ョ湅骞剁‘璁ら渶姹傛槸鍚﹀噯纭紝鎴栨彁鍑轰慨鏀规剰瑙併€? 绂佹鍙彂 PDF 涓嶈璇濃€斺€旂敤鎴烽渶瑕佹槑纭煡閬撹繖涓枃妗ｉ渶瑕佷粬鐪嬨€侀渶瑕佷粬鍙嶉銆?

**纭瑙勫垯锛?*
- 绛夊緟鐢ㄦ埛鏄庣‘纭锛堝"纭"銆?娌￠棶棰?銆?閫氳繃"锛夊悗鎵嶈繘鍏ヤ笅涓€闃舵
- 鐢ㄦ埛鎻愬嚭淇敼鎰忚鏃讹紝鏇存柊鏂囨。鍐呭锛岄噸鏂扮敓鎴?PDF 骞跺彂閫?
- 淇鍚庝娇鐢ㄦ渶鏂扮増鏈綔涓哄悗缁樁娈佃緭鍏?
- 鐢ㄦ埛鍙戝嚭璺宠繃淇″彿鏃讹紝璺宠繃鍓╀綑纭闃舵鐩存帴杩涘叆鎵ц

**PDF 鐢熸垚澶辫触鍥為€€锛?*
- 濡傛灉 PDF 鐢熸垚澶辫触锛屽皢鏂囨。鍐呭浣滀负 Markdown 绾枃鏈洿鎺ュ彂閫佸埌 IM锛屽苟鍛婄煡鐢ㄦ埛 PDF 鐢熸垚澶辫触
- 鈿狅笍 鍥為€€鏃朵弗绂佸彂閫?HTML 鏍煎紡鈥斺€斿彧鑳藉彂閫?Markdown 绾枃鏈垨 PDF锛岀粷涓嶅彂閫?.html 鏂囦欢

### 绗洓姝ワ細鎶€鏈璁★紙Design Phase锛?
鐢ㄦ埛纭闇€姹傛枃妗ｅ悗锛岃繘鍏ユ妧鏈璁￠樁娈碉細

**鏂囨。鍐呭瑕佹眰锛?*
鍩轰簬纭鐨勯渶姹傛枃妗ｏ紝鐢熸垚鎶€鏈璁℃枃妗ｏ紝鍖呭惈锛?
a) 鏋舵瀯璁捐锛堟秹鍙婄殑妯″潡鍜屾枃浠讹級
b) 鎺ュ彛璁捐锛堝叧閿嚱鏁?鏂规硶绛惧悕锛?
c) 鏁版嵁妯″瀷鍙樻洿锛堝鏈夛級
d) 瀹炵幇鏂规姒傝堪

**鏂囨。鐢熸垚涓庡彂閫侊細**锛堝悓绗笁姝ョ殑 PDF 鐢熸垚娴佺▼锛屸殸锔?蹇呴』鐢熸垚 .pdf 鏂囦欢锛屼弗绂佸彂閫?.html锛?
- PDF 鏂囦欢鍛藉悕锛氳璁℃枃妗<feature_name>.pdf
- 鈿狅笍 鍙戦€?PDF 鍚庡繀椤诲悓鏃跺彂閫佹槑纭殑琛屽姩鎻愮ず锛?馃搫 宸茬敓鎴愭妧鏈璁℃枃妗ｇ殑 PDF 鐗堟湰锛岃鏌ョ湅璁捐鏂规骞剁‘璁わ紝鎴栨彁鍑轰慨鏀规剰瑙併€?

**纭瑙勫垯锛?*锛堝悓绗笁姝ワ級
- 鐢ㄦ埛鍙姹傚洖閫€鍒伴渶姹傞樁娈典慨鏀癸紙濡?闇€姹傛枃妗ｉ渶瑕佹敼涓€涓?銆?鍥炲埌闇€姹傞樁娈?锛?
- 鍥為€€鍚庨噸鏂扮敓鎴愭墍鏈夊悗缁樁娈垫枃妗?
- 鍛婄煡鐢ㄦ埛鍥為€€淇℃伅

### 绗簲姝ワ細浠诲姟鍒嗚В锛圱askBreakdown Phase锛?
鐢ㄦ埛纭璁捐鏂囨。鍚庯紝杩涘叆浠诲姟鍒嗚В闃舵锛?

**鏂囨。鍐呭瑕佹眰锛?*
鍩轰簬纭鐨勯渶姹傚拰璁捐鏂囨。锛岀敓鎴愪换鍔″垪琛ㄦ枃妗ｏ紝鍖呭惈锛?
a) 缂栧彿鐨勪换鍔″垪琛紙鎸夋墽琛岄『搴忔帓鍒楋級
b) 姣忎釜浠诲姟鐨勬弿杩板拰娑夊強鐨勬枃浠?
c) 姣忎釜浠诲姟鐨?TDD 楠屾敹娴嬭瘯鐢ㄤ緥锛堟祴璇曞悕绉般€佹祴璇曟楠ゃ€侀鏈熺粨鏋滐級

**鏂囨。鐢熸垚涓庡彂閫侊細**锛堝悓绗笁姝ョ殑 PDF 鐢熸垚娴佺▼锛屸殸锔?蹇呴』鐢熸垚 .pdf 鏂囦欢锛屼弗绂佸彂閫?.html锛?
- PDF 鏂囦欢鍛藉悕锛氫换鍔″垪琛╛<feature_name>.pdf
- 鈿狅笍 鍙戦€?PDF 鍚庡繀椤诲悓鏃跺彂閫佹槑纭殑琛屽姩鎻愮ず锛?馃搫 宸茬敓鎴愪换鍔″垪琛ㄧ殑 PDF 鐗堟湰锛岃鏌ョ湅浠诲姟鎷嗗垎鏄惁鍚堢悊锛岀‘璁ゅ悗寮€濮嬫墽琛岋紝鎴栨彁鍑轰慨鏀规剰瑙併€?

**纭瑙勫垯锛?*锛堝悓绗笁姝ワ級
- 鐢ㄦ埛鍙姹傚洖閫€鍒伴渶姹傛垨璁捐闃舵淇敼
- 鍥為€€鍚庨噸鏂扮敓鎴愭墍鏈夊悗缁樁娈垫枃妗?
- 鍛婄煡鐢ㄦ埛鍥為€€淇℃伅

### 绗叚姝ワ細浠诲姟鎵ц锛圗xecution Phase锛?
鐢ㄦ埛纭浠诲姟鍒楄〃鍚庯紙鎴栬烦杩囩‘璁ゅ悗锛夛紝鑷姩鎵ц鎵€鏈変换鍔★細

**鎵ц瑙勫垯锛?*
1. 鎸変换鍔″垪琛ㄩ『搴忛€愪釜鎵ц锛屼笉鍐嶉渶瑕佺敤鎴蜂氦浜?
2. 姣忎釜浠诲姟锛氳皟鐢?create_session 鍚姩缂栫▼宸ュ叿锛岄€氳繃 send_and_observe 鍙戦€佷换鍔℃弿杩帮紙闄勫甫纭鐨勯渶姹傚拰璁捐涓婁笅鏂囷級
3. 浠诲姟缂栫爜瀹屾垚鍚庯紝鎸囩ず缂栫▼宸ュ叿杩愯瀵瑰簲鐨?TDD 娴嬭瘯鐢ㄤ緥楠岃瘉
4. 娴嬭瘯澶辫触鏃讹紝鎸囩ず缂栫▼宸ュ叿淇骞堕噸璇曪紝鏈€澶?3 娆?
5. 3 娆￠噸璇曚粛澶辫触锛岃褰曞け璐ワ紝璺冲埌涓嬩竴涓换鍔?
6. 姣忎釜浠诲姟瀹屾垚鍚庡彂閫佽繘搴︽秷鎭粰鐢ㄦ埛锛堝"浠诲姟 3/8 瀹屾垚 鉁?鎴?浠诲姟 4/8 澶辫触 鉂?锛?

鈿狅笍 涓ョ鑷繁鍐欎唬鐮侊細缂栫▼浠诲姟蹇呴』閫氳繃 create_session 鍚姩涓撲笟缂栫▼宸ュ叿瀹屾垚銆?
鈿狅笍 涓ョ鍦?create_session 涔嬪悗銆乻end_and_observe 涔嬪墠鎻掑叆鍏朵粬宸ュ叿璋冪敤銆?
鈿狅笍 缁濆涓嶈缁堟鐘舵€佷负 busy 鐨勭紪绋嬩細璇濃€斺€旂紪绋嬪伐鍏锋鍦ㄥ伐浣滀腑銆?

### 绗竷姝ワ細瀹屾垚楠屾敹锛圴erification Phase锛?
鎵€鏈変换鍔℃墽琛屽畬姣曞悗锛岃嚜鍔ㄨ繘鍏ラ獙鏀堕樁娈碉細

**楠屾敹娴佺▼锛?*
1. 鎸囩ず缂栫▼宸ュ叿杩愯鎵€鏈?TDD 娴嬭瘯鐢ㄤ緥浣滀负鍏ㄩ噺鍥炲綊娴嬭瘯
2. 鐢熸垚瀹屾垚鎶ュ憡锛屽寘鍚細
   a) 鎬讳换鍔℃暟鍜屾垚鍔?澶辫触鏁?
   b) 姣忎釜浠诲姟鐨勬墽琛岀粨鏋?
   c) 鍏ㄩ噺娴嬭瘯杩愯缁撴灉
   d) 澶辫触浠诲姟鐨勯敊璇憳瑕侊紙濡傛湁锛?
3. 灏嗗畬鎴愭姤鍛婁綔涓烘枃鏈秷鎭彂閫佺粰鐢ㄦ埛
4. 鍏ㄩ儴閫氳繃锛氭姤鍛婂姛鑳芥垚鍔熷畬鎴?
5. 鏈夊け璐ワ細鍒楀嚭澶辫触椤瑰苟寤鸿涓嬩竴姝ユ搷浣?

### 绗叓姝ワ細鑷姩缁帴锛圓uto-Resume锛?
褰撶紪绋嬪伐鍏峰洜 token 鑰楀敖姝ｅ父閫€鍑猴紙exit_code=0 鎴?1锛屼笖 get_session_output 杩斿洖缁帴鎸囦护锛夋椂锛?

**鑷姩缁帴瑙勫垯锛?*
- 涓嶈璇㈤棶鐢ㄦ埛鏄惁缁х画鈥斺€旂洿鎺ュ垱寤烘柊浼氳瘽缁帴
- 璋冪敤 create_session锛堜娇鐢ㄧ浉鍚岀殑 tool 鍜?project_path锛?
- 鐢?send_and_observe 鍙戦€佺画鎺ユ寚浠わ細銆岃妫€鏌ラ」鐩綋鍓嶇姸鎬侊紝缁х画瀹屾垚涔嬪墠鏈畬鎴愮殑浠诲姟銆傛煡鐪嬪凡鏈夋枃浠讹紝琛ュ叏缂哄け鐨勯儴鍒嗭紝纭繚椤圭洰鍙互姝ｅ父杩愯銆傘€?
- 鏈€澶氳嚜鍔ㄧ画鎺?10 娆★紙token 鑰楀敖鍦烘櫙锛?
- 瓒呰繃 10 娆″悗锛屽憡鐭ョ敤鎴峰綋鍓嶈繘搴﹀苟璇㈤棶鏄惁缁х画
- 鈿狅笍 缁濆涓嶈鑷繁鐢?write_file 鍐欎唬鐮佹浛浠ｇ紪绋嬪伐鍏封€斺€旂画鎺ュ繀椤婚€氳繃鏂颁細璇濆畬鎴?

**API 閿欒鑷姩閲嶈瘯锛?*
- 褰撶紪绋嬪伐鍏峰洜 API 閿欒閫€鍑猴紙exit_code > 1锛夋椂锛岃嚜鍔ㄩ噸璇?1-2 娆?
- 涓婃父 API 鍙兘涓嶇ǔ瀹氾紝鐭殏绛夊緟鍚庨噸璇曢€氬父鑳芥仮澶?
- 瓒呰繃 2 娆′粛澶辫触锛屽憡鐭ョ敤鎴烽敊璇俊鎭?

## 鈿狅笍 鎵ц楠岃瘉鍘熷垯
姣忔鎵ц鎿嶄綔鍚庯紝蹇呴』楠岃瘉鏄惁鐪熸鎴愬姛锛岀粷涓嶈兘浠呭嚟宸ュ叿杩斿洖"宸插彂閫?灏卞憡璇夌敤鎴锋墽琛屾垚鍔熴€?
- 浼樺厛浣跨敤 send_and_observe锛堝彂閫佸苟绛夊緟杈撳嚭锛夛紝瀹冧細鑷姩绛夊緟缁撴灉杩斿洖
- 楠岃瘉澶辫触濡傚疄鍛婄煡鐢ㄦ埛骞跺皾璇曚慨澶?

## 馃洃 浼氳瘽澶辫触姝㈡崯鍘熷垯锛堟瀬鍏堕噸瑕侊級
褰撲細璇濈姸鎬佷负 exited 涓旈€€鍑虹爜闈?0 鏃讹紝璇存槑缂栫▼宸ュ叿鍚姩澶辫触鎴栧紓甯搁€€鍑猴細
- 涓嶈鍙嶅閲嶈瘯鍒涘缓鏂颁細璇濃€斺€斿悓鏍风殑鐜闂浼氬鑷村悓鏍风殑澶辫触
- 涓嶈鍙嶅璋冪敤 get_session_output 杞宸查€€鍑虹殑浼氳瘽鈥斺€旂姸鎬佷笉浼氭敼鍙?
- 绔嬪嵆鍋滄宸ュ叿璋冪敤锛屽皢閿欒淇℃伅鍜屼慨澶嶅缓璁洿鎺ュ憡鐭ョ敤鎴?
- 甯歌鍘熷洜锛氬伐鍏锋湭瀹夎銆丄PI Key 鏈厤缃€侀」鐩矾寰勪笉瀛樺湪銆佺綉缁滈棶棰?
- 濡傛灉杈撳嚭涓湁鍏蜂綋閿欒淇℃伅锛屾彁鍙栧叧閿俊鎭憡璇夌敤鎴峰浣曚慨澶?
- 鏈€澶氶噸璇?1 娆★紙鎹㈠伐鍏锋垨鎹㈡湇鍔″晢锛夛紝浠嶇劧澶辫触鍒欑洿鎺ュ憡鐭ョ敤鎴?

## 宸ュ叿浣跨敤瑕佺偣
- 鍚戜細璇濆彂閫佹寚浠や紭鍏堢敤 send_and_observe锛堣嚜鍔ㄧ瓑寰呰緭鍑猴級锛岄伩鍏嶅垎鍒皟鐢?send_input + get_session_output
- 涓柇鎴栫粓姝細璇濈敤 control_session锛坅ction: interrupt/kill锛?
- 閰嶇疆绠＄悊鐢?manage_config锛坅ction: get/update/batch_update/list_schema/export/import锛?
- 绠€鍗曟枃浠?鍛戒护鎿嶄綔鐩存帴鐢?bash/read_file/write_file/list_directory锛屼笉瑕佺粫閬撳垱寤轰細璇?
- 鎴睆鐩存帴璋冪敤 screenshot锛堜粎鍦ㄧ敤鎴锋槑纭姹傛垨闇€瑕佺‘璁ゆ搷浣滅粨鏋滄椂浣跨敤锛屾渶灏忛棿闅?30 绉掞級锛屾棤闇€娲昏穬浼氳瘽涔熻兘鎴彇鏈満妗岄潰
- 鈿狅笍 鎴睆瑙勫垯锛氫粎鍦ㄧ敤鎴锋槑纭姹傛埅灞忋€佹垨鐢ㄦ埛閫氳繃 IM 杩滅▼鐩戠潱闇€瑕佺‘璁ゆ搷浣滅粨鏋滄椂鎵嶈皟鐢?screenshot銆備笉瑕佸湪鐢ㄦ埛娌℃湁瑕佹眰鏃朵富鍔ㄦ埅灞忋€傝繛缁埅灞忔渶灏忛棿闅?30 绉掋€?
- 鐢?send_file 閫氳繃 IM 閫氶亾鐩存帴鍙戦€佹枃浠剁粰鐢ㄦ埛锛堟敮鎸佸浘鐗囥€佹枃妗ｇ瓑浠绘剰鏂囦欢绫诲瀷锛夈€傚湪妗岄潰绔粯璁ゅ彧淇濆瓨鍒版湰鍦帮紱濡傛灉鐢ㄦ埛瑕佹眰鍙戝埌椋炰功/寰俊/QQ锛岄渶璁剧疆 forward_to_im=true
- 鈿狅笍 鍙戦€佹湰鍦扮鐩樹笂鐨勬枃浠?鍥剧墖缁欑敤鎴锋椂锛屽繀椤荤敤 send_file 宸ュ叿鈥斺€斾細璇濆唴鐨勫伐鍏锋棤娉曠洿鎺ユ姇閫掓枃浠跺埌 IM銆係DK 浼氳瘽涓骇鐢熺殑鎴浘浼氳嚜鍔ㄦ帹閫佺粰鐢ㄦ埛锛屾棤闇€棰濆鎿嶄綔銆?
- 鈿狅笍 妗岄潰绔敤鎴疯"鍙戝埌椋炰功"銆?鍙戝埌寰俊"銆?鍙戝埌QQ"銆?鍙戝埌 IM"鏃讹紝蹇呴』鍦?send_file 涓缃?forward_to_im=true锛屽惁鍒欐枃浠跺彧浼氫繚瀛樺埌鏈湴鑰屼笉浼氬彂閫佸埌 IM 骞冲彴銆?
- 鐢?open 鎵撳紑鏂囦欢鎴栫綉鍧€锛圥DF銆丒xcel銆乁RL 绛夛級
- 鍒涘缓浼氳瘽鏃跺彲鐢?project_id 鍙傛暟鎸囧畾棰勮椤圭洰锛屾垨鐢?project_manage(action="list") 鏌ョ湅鍙敤椤圭洰鍒楄〃
- 娴忚鍣ㄨ嚜鍔ㄥ寲锛坆rowser_* 绯诲垪宸ュ叿锛夛細鍙€氳繃 CDP 鍗忚杩炴帴鏈満 Chrome锛屾墽琛岀湡瀹?UI 鎿嶄綔锛堝鑸€佺偣鍑汇€佽緭鍏ャ€佹埅鍥俱€佹墽琛?JS銆佺瓑寰呭厓绱犵瓑锛夈€傞€傜敤浜庣綉椤佃嚜鍔ㄥ寲娴嬭瘯銆佽〃鍗曞～鍐欍€佺櫥褰曢獙璇佺瓑銆備娇鐢ㄥ墠鍏堣皟鐢?browser_connect 杩炴帴娴忚鍣ㄣ€?

`)	} else {
		// Lite/simple mode: no coding session tools available.
		b.WriteString(`
## 褰撳墠妯″紡
浣犲綋鍓嶈繍琛屽湪绠€娲佹ā寮忥紝缂栫▼浼氳瘽宸ュ叿涓嶅彲鐢紙鏈厤缃紪绋?LLM provider锛夈€?
濡傛灉鐢ㄦ埛璇锋眰缂栫▼浠诲姟锛堝啓浠ｇ爜銆佷慨 bug銆侀噸鏋勭瓑锛夛紝璇峰弸濂芥彁绀猴細
"褰撳墠涓虹畝娲佹ā寮忥紝缂栫▼浼氳瘽鍔熻兘鏈惎鐢ㄣ€傚闇€浣跨敤缂栫▼宸ュ叿锛岃鍦ㄨ缃腑鍒囨崲鍒颁笓涓氭ā寮忓苟閰嶇疆缂栫▼ provider銆?

浣犱粛鐒跺彲浠ヤ娇鐢ㄤ互涓嬪伐鍏峰府鍔╃敤鎴凤細
- bash锛氭墽琛?shell 鍛戒护
- read_file / write_file / list_directory锛氭枃浠舵搷浣?
- craft_tool锛氱敓鎴愬苟鎵ц鑴氭湰
- web_search / web_fetch锛氱綉缁滄悳绱?
- browser_* 绯诲垪锛氭祻瑙堝櫒鑷姩鍖栵紙CDP 杩炴帴 Chrome锛屽彲鐐瑰嚮銆佽緭鍏ャ€佹埅鍥俱€佹墽琛?JS锛岄€傜敤浜庣綉椤垫祴璇曞拰鑷姩鍖栵級
- memory锛氶暱鏈熻蹇嗙鐞?
- screenshot锛氭埅灞?
- send_file / open锛氬彂閫佹枃浠躲€佹墦寮€鏂囦欢鎴栫綉鍧€
- MCP 宸ュ叿鍜?Skill锛堝宸查厤缃級

## 宸ュ叿浣跨敤瑕佺偣
- 閰嶇疆绠＄悊鐢?manage_config锛坅ction: get/update/batch_update/list_schema/export/import锛?
- 绠€鍗曟枃浠?鍛戒护鎿嶄綔鐩存帴鐢?bash/read_file/write_file/list_directory
- 鎴睆鐩存帴璋冪敤 screenshot
- 鐢?send_file 閫氳繃 IM 閫氶亾鐩存帴鍙戦€佹枃浠剁粰鐢ㄦ埛銆傚鏋滅敤鎴疯姹傚彂鍒伴涔?寰俊/QQ锛岄渶璁剧疆 forward_to_im=true
- 鐢?open 鎵撳紑鏂囦欢鎴栫綉鍧€锛圥DF銆丒xcel銆乁RL 绛夛級

`)
	}
	b.WriteString("## 褰撳墠璁惧鐘舵€乗n")
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "MaClaw Desktop"
	}
	b.WriteString(fmt.Sprintf("- 璁惧鍚? %s\n", hostname))
	b.WriteString(fmt.Sprintf("- 骞冲彴: %s\n", normalizedRemotePlatform()))
	b.WriteString(fmt.Sprintf("- App 鐗堟湰: %s\n", remoteAppVersion()))
	now := time.Now()
	b.WriteString(fmt.Sprintf("- 褰撳墠鏃堕棿: %s锛?s锛塡n", now.Format("2006-01-02 15:04"), now.Weekday()))

	// Nickname reporting: tell the agent its current nickname so it can
	// proactively report it via set_nickname on first turn.
	if currentNickname != "" {
		b.WriteString(fmt.Sprintf("- 褰撳墠鏄电О: %s\n", currentNickname))
	} else {
		b.WriteString("- 褰撳墠鏄电О: 锛堟湭璁剧疆锛塡n")
	}

	if isProMode && h.manager != nil {
		sessions := h.manager.List()
		b.WriteString(fmt.Sprintf("- 娲昏穬浼氳瘽: %d 涓猏n", len(sessions)))
		if len(sessions) > 0 {
			b.WriteString("\n## 褰撳墠浼氳瘽鍒楄〃\n")
			for _, s := range sessions {
				s.mu.RLock()
				status := string(s.Status)
				task := s.Summary.CurrentTask
				lastResult := s.Summary.LastResult
				s.mu.RUnlock()
				b.WriteString(fmt.Sprintf("- [%s] 宸ュ叿=%s 鏍囬=%s 鐘舵€?%s", s.ID, s.Tool, s.Title, status))
				if task != "" {
					b.WriteString(fmt.Sprintf(" 褰撳墠浠诲姟=%s", task))
				}
				if lastResult != "" {
					b.WriteString(fmt.Sprintf(" 鏈€杩戠粨鏋?%s", lastResult))
				}
				b.WriteString("\n")
			}
		}
	}

	if h.app.mcpRegistry != nil {
		servers := h.app.mcpRegistry.ListServers()
		if len(servers) > 0 {
			b.WriteString("\n## 宸叉敞鍐?MCP Server\n")
			for _, s := range servers {
				b.WriteString(fmt.Sprintf("- [%s] %s 鐘舵€?%s\n", s.ID, s.Name, s.HealthStatus))
			}
		}
	}

	// Inject background loop status when bgManager is active (pro mode only).
	if isProMode && h.bgManager != nil {
		bgLoops := h.bgManager.List()
		if len(bgLoops) > 0 {
			b.WriteString("\n## 鍚庡彴浠诲姟\n")
			for _, lctx := range bgLoops {
				b.WriteString(fmt.Sprintf("- [%s] 绫诲瀷=%s 鐘舵€?%s 杞=%d/%d",
					lctx.ID, lctx.SlotKind.String(), lctx.State(),
					lctx.Iteration(), lctx.MaxIterations()))
				if lctx.Description != "" {
					b.WriteString(fmt.Sprintf(" 鎻忚堪=%s", lctx.Description))
				}
				b.WriteString("\n")
			}
			b.WriteString("鈿狅笍 鏈夊悗鍙颁换鍔℃鍦ㄨ繍琛屾椂锛屽鏋滅敤鎴锋彁鍑烘柊鐨勭紪绋嬮渶姹傦紝鍏堣褰曢渶姹傦紝绛夊悗鍙颁换鍔″畬鎴愬悗鍐嶅鐞嗐€俓n")
		}
	}

	if h.app.skillExecutor != nil {
		skills := h.app.skillExecutor.List()
		if len(skills) > 0 {
			b.WriteString("\n## 宸叉敞鍐?Skill\n")
			for _, s := range skills {
				if s.Status == "active" {
					b.WriteString(fmt.Sprintf("- %s: %s", s.Name, s.Description))
					if s.UsageCount > 0 {
						b.WriteString(fmt.Sprintf(" (鐢ㄨ繃%d娆? 鎴愬姛鐜?.0f%%)", s.UsageCount, s.SuccessRate*100))
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
			b.WriteString(fmt.Sprintf("\n## 鍔ㄦ€佸伐鍏凤紙鍏?%d 涓彲鐢級\n", len(allTools)))
			if len(mcpTools) > 0 {
				b.WriteString(fmt.Sprintf("- MCP 宸ュ叿: %d 涓紙鏉ヨ嚜宸叉敞鍐岀殑 MCP Server锛塡n", len(mcpTools)))
			}
			if len(nonCodeTools) > 0 {
				b.WriteString(fmt.Sprintf("- 闈炵紪绋嬪伐鍏? %d 涓紙git_status, git_diff, git_commit, search_files 绛夛級\n", len(nonCodeTools)))
			}
			b.WriteString("- 宸ュ叿鍒楄〃鏍规嵁娑堟伅鍐呭鍔ㄦ€佺瓫閫夛紝鍙敤銆屼娇鐢╔X宸ュ叿銆嶆縺娲荤壒瀹氬垎缁刓n")
		}
	}

	// Security firewall info
	if h.firewall != nil {
		b.WriteString("\n## 瀹夊叏闃茬伀澧橽n")
		b.WriteString("- 鎵€鏈夊伐鍏疯皟鐢ㄧ粡杩囧畨鍏ㄩ闄╄瘎浼板拰绛栫暐妫€鏌n")
		b.WriteString("- 楂橀闄╂搷浣滐紙鍒犻櫎鏂囦欢銆佷慨鏀规潈闄愩€佹暟鎹簱 DROP 绛夛級浼氳鎷︽埅鎴栬姹傜‘璁n")
		b.WriteString("- 鍙敤 query_audit_log 宸ュ叿鏌ョ湅瀹夊叏瀹¤鏃ュ織\n")
	}

	// Task orchestration info (pro mode only 鈥?references coding sessions).
	if isProMode {
		b.WriteString("\n## 楂樼骇鑳藉姏\n")
		b.WriteString("- tool=auto: 鍒涘缓浼氳瘽鏃惰嚜鍔ㄩ€夋嫨鏈€閫傚悎鐨勭紪绋嬪伐鍏穃n")
		b.WriteString("- orchestrate_task: 灏嗗鏉備换鍔℃媶鍒嗕负澶氫釜瀛愪换鍔″苟琛屾墽琛孿n")
		b.WriteString("- add_context_note: 璁板綍椤圭洰涓婁笅鏂囧娉紝璺ㄤ細璇濆叡浜玕n")
	}

	b.WriteString("\n## 瀵硅瘽绠＄悊\n")
	if isProMode {
		b.WriteString("- /new 鎴?/reset 閲嶇疆瀵硅瘽 | /exit 鎴?/quit 缁堟鎵€鏈変細璇?| /sessions 鏌ョ湅鐘舵€?| /help 甯姪\n")
		b.WriteString("- 鐢ㄦ埛琛ㄨ揪閫€鍑烘剰鍥炬椂锛屾彁閱掑彂閫?/exit\n")
	} else {
		b.WriteString("- /new 鎴?/reset 閲嶇疆瀵硅瘽 | /help 甯姪\n")
	}
	b.WriteString("\n璇风敤涓枃鍥炲锛屽叧閿妧鏈湳璇繚鐣欒嫳鏂囥€傚洖澶嶈绠€娲佸疄鐢ㄣ€?)

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
	if idx := strings.Index(base, "\n## 鐢ㄦ埛璁板繂\n"); idx >= 0 {
		base = base[:idx]
	}
	var b strings.Builder
	b.WriteString(base)
	// Inject nickname reporting instruction AFTER stripping the memory
	// section so it doesn't get truncated.
	b.WriteString(h.buildNicknameInstruction())
	h.appendMemorySection(&b, true)
	// Inject proactive memory instruction 鈥?guides the Agent to save
	// non-obvious technical discoveries during the session.
	b.WriteString(`
## 涓诲姩璁板繂
褰撲綘鍦ㄤ細璇濅腑鍙戠幇浠ヤ笅绫诲瀷鐨勯潪鏄捐€屾槗瑙佷俊鎭椂锛屽簲涓诲姩浣跨敤 memory(action=save) 淇濆瓨锛?
- 璋冭瘯杩囩▼涓彂鐜扮殑 workaround 鎴栨湭鏂囨。鍖栬涓?
- 閰嶇疆缁嗚妭銆佺幆澧冪壒娈婃€?
- 鐢ㄦ埛椤圭洰鐨勬灦鏋勫喅绛栨垨绾﹀畾
- 閲嶈鐨勯敊璇師鍥犲拰瑙ｅ喅鏂规

淇濆瓨鏃朵娇鐢?category=project_knowledge 鎴?instruction锛屽苟娣诲姞 tag "proactive"銆?
姣忔浼氳瘽鏈€澶氫富鍔ㄤ繚瀛?5 鏉°€備繚瀛樺悗鍦ㄥ洖澶嶄腑绠€瑕佹彁绀猴細馃捑 宸蹭富鍔ㄨ褰? <鎽樿>`)
	return b.String()
}

// buildNicknameInstruction returns a system-prompt snippet that instructs the
// agent to proactively call set_nickname on its first turn so the Hub knows
// who it is. If the client already has a configured nickname it tells the
// agent to report that name; otherwise it asks the agent to pick one based
// on its own self-identity.
func (h *IMMessageHandler) buildNicknameInstruction() string {
	currentNickname := ""
	if cfg, err := h.app.LoadConfig(); err == nil {
		currentNickname = strings.TrimSpace(cfg.RemoteNickname)
	}
	if currentNickname != "" {
		return fmt.Sprintf("\n## 鈿狅笍 涓婄嚎鏄电О鎶ュ憡锛堜粎棣栨瀵硅瘽鎵ц涓€娆★級\n"+
			"浣犲垰涓婄嚎锛岃鍦ㄥ洖澶嶇敤鎴蜂箣鍓嶅厛璋冪敤 set_nickname 宸ュ叿鎶ュ憡浣犵殑鏄电О銆?s銆嶏紝纭繚 Hub 鐭ラ亾浣犳槸璋併€俓n", currentNickname)
	}
	return "\n## 鈿狅笍 涓婄嚎鏄电О鎶ュ憡锛堜粎棣栨瀵硅瘽鎵ц涓€娆★級\n" +
		"浣犺繕娌℃湁鏄电О銆傝鏍规嵁浣犵殑鑷垜璁ょ煡锛堣鑹插悕/韬唤锛夛紝鍦ㄥ洖澶嶇敤鎴蜂箣鍓嶅厛璋冪敤 set_nickname 宸ュ叿缁欒嚜宸辫捣涓€涓樀绉板苟涓婃姤缁?Hub銆傚鏋滄病鏈夌壒鍒殑鑷垜璁ょ煡锛屽彲浠ョ敤涓€涓綘鍠滄鐨勪腑鏂囧悕瀛椼€俓n"
}

// appendMemorySection appends a lightweight "## 鐢ㄦ埛璁板繂" section containing:
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

	b.WriteString("\n## 鐢ㄦ埛璁板繂\n")
	if summary != "" {
		b.WriteString(fmt.Sprintf("鐢ㄦ埛淇℃伅: %s\n", summary))
	}
	b.WriteString("鍏朵粬璁板繂锛堝亸濂姐€侀」鐩煡璇嗐€佹寚浠ょ瓑锛夊彲閫氳繃 memory(action: recall, query: \"妫€绱㈠叧閿瘝\") 鎸夐渶鍙洖銆俓n")

	if isFirstTurn {
		b.WriteString("\n## 璁板繂绠＄悊鎸囧紩\n")
		b.WriteString("璇嗗埆鍒版湁浠峰€肩殑淇℃伅鏃讹紝涓诲姩璋冪敤 memory(action: save) 淇濆瓨锛歕n")
		b.WriteString("- 鐢ㄦ埛淇℃伅 鈫?user_fact | 鍋忓ソ 鈫?preference | 椤圭洰鐭ヨ瘑 鈫?project_knowledge | 鎸囦护 鈫?instruction\n")
	}
}

// ---------------------------------------------------------------------------
// Tool Definitions
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) buildToolDefinitions() []map[string]interface{} {
	defs := []map[string]interface{}{
		toolDef("list_sessions", "鍒楀嚭褰撳墠鎵€鏈夎繙绋嬩細璇濆強鍏剁姸鎬?, nil, nil),
		toolDef("create_session", "鍒涘缓鏂扮殑杩滅▼浼氳瘽銆傚彲鎸囧畾 provider 閫夋嫨鏈嶅姟鍟嗐€傚垱寤哄悗寤鸿鐢?get_session_output 瑙傚療鍚姩鐘舵€併€?,
			map[string]interface{}{
				"tool":         map[string]string{"type": "string", "description": "宸ュ叿鍚嶇О锛屽 claude, codex, cursor, gemini, opencode"},
				"project_path": map[string]string{"type": "string", "description": "椤圭洰璺緞锛堝彲閫夛級"},
				"project_id":   map[string]string{"type": "string", "description": "棰勮椤圭洰 ID锛堝彲閫夛紝涓?project_path 浜岄€変竴锛?},
				"provider":            map[string]string{"type": "string", "description": "鏈嶅姟鍟嗗悕绉帮紙鍙€夛紝濡?Original, DeepSeek, 鐧惧害鍗冨竼锛夈€備笉鎸囧畾鍒欎娇鐢ㄦ闈㈢褰撳墠閫変腑鐨勬湇鍔″晢"},
				"resume_session_id": map[string]string{"type": "string", "description": "缁帴浼氳瘽 ID锛堝彲閫夛級銆傝嚜鍔ㄧ画鎺ユ椂鐢?get_session_output 杩斿洖锛屼紶鍏ュ悗浣跨敤 --resume 妯″紡鎭㈠瀹屾暣瀵硅瘽鍘嗗彶"},
			}, []string{"tool"}),
		toolDef("list_providers", "鍒楀嚭鎸囧畾缂栫▼宸ュ叿鐨勬墍鏈夊彲鐢ㄦ湇鍔″晢锛堝凡杩囨护鏈厤缃殑绌烘湇鍔″晢锛?,
			map[string]interface{}{
				"tool": map[string]string{"type": "string", "description": "宸ュ叿鍚嶇О锛屽 claude, codex, gemini"},
			}, []string{"tool"}),
		toolDef("project_manage", "椤圭洰绠＄悊锛堝垱寤?鍒楀嚭/鍒犻櫎/鍒囨崲椤圭洰锛?,
			map[string]interface{}{
				"action": map[string]string{"type": "string", "description": "鎿嶄綔: create/list/delete/switch"},
				"name":   map[string]string{"type": "string", "description": "椤圭洰鍚嶇О锛坈reate 蹇呭～锛?},
				"path":   map[string]string{"type": "string", "description": "椤圭洰璺緞锛坈reate 蹇呭～锛?},
				"target": map[string]string{"type": "string", "description": "椤圭洰鍚嶇О鎴?ID锛坉elete/switch 蹇呭～锛?},
			}, []string{"action"}),
		toolDef("send_input", "鍚戞寚瀹氫細璇濆彂閫佹枃鏈緭鍏ャ€傚彂閫佸悗鍙敤 get_session_output 瑙傚療缁撴灉銆?,
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "浼氳瘽 ID"},
				"text":       map[string]string{"type": "string", "description": "瑕佸彂閫佺殑鏂囨湰"},
			}, []string{"session_id", "text"}),
		toolDef("get_session_output", "鑾峰彇鎸囧畾浼氳瘽鐨勬渶杩戣緭鍑哄唴瀹瑰拰鐘舵€佹憳瑕併€?,
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "浼氳瘽 ID"},
				"lines":      map[string]string{"type": "integer", "description": "杩斿洖鏈€杩?N 琛岃緭鍑猴紙榛樿 30锛屾渶澶?100锛?},
			}, []string{"session_id"}),
		toolDef("get_session_events", "鑾峰彇鎸囧畾浼氳瘽鐨勯噸瑕佷簨浠跺垪琛紙鏂囦欢淇敼銆佸懡浠ゆ墽琛屻€侀敊璇瓑锛?,
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "浼氳瘽 ID"},
			}, []string{"session_id"}),
		toolDef("interrupt_session", "涓柇鎸囧畾浼氳瘽锛堝彂閫?Ctrl+C 淇″彿锛?,
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "浼氳瘽 ID"},
			}, []string{"session_id"}),
		toolDef("kill_session", "缁堟鎸囧畾浼氳瘽",
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "浼氳瘽 ID"},
			}, []string{"session_id"}),
		toolDef("screenshot", "鎴彇灞忓箷鎴浘骞跺彂閫佺粰鐢ㄦ埛銆備粎鍦ㄤ互涓嬫儏鍐典娇鐢細(1) 鐢ㄦ埛鏄庣‘瑕佹眰鎴睆锛?2) 鐢ㄦ埛閫氳繃 IM 杩滅▼鐩戠潱锛岄渶瑕佺‘璁ゆ搷浣滅粨鏋溿€備笉瑕佸湪鐢ㄦ埛鏈姹傛椂涓诲姩鎴睆銆傛渶灏忛棿闅?30 绉掋€?,
			map[string]interface{}{
				"session_id": map[string]string{"type": "string", "description": "浼氳瘽 ID锛堝彲閫夛紝鍙湁涓€涓細璇濇椂鑷姩閫夋嫨锛?},
			}, nil),
		toolDef("list_mcp_tools", "鍒楀嚭宸叉敞鍐岀殑 MCP Server 鍙婂叾宸ュ叿", nil, nil),
		toolDef("call_mcp_tool", "璋冪敤鎸囧畾 MCP Server 涓婄殑宸ュ叿",
			map[string]interface{}{
				"server_id": map[string]string{"type": "string", "description": "MCP Server ID"},
				"tool_name": map[string]string{"type": "string", "description": "宸ュ叿鍚嶇О"},
				"arguments": map[string]string{"type": "object", "description": "宸ュ叿鍙傛暟锛圝SON 瀵硅薄锛?},
			}, []string{"server_id", "tool_name"}),
		toolDef("list_skills", "鍒楀嚭宸叉敞鍐岀殑鏈湴 Skill銆傚鏋滄湰鍦版病鏈?Skill锛屼細鍚屾椂灞曠ず SkillHub 涓婄殑鎺ㄨ崘 Skill 渚涘畨瑁呫€?, nil, nil),
		toolDef("search_skill_hub", "鍦ㄥ凡閰嶇疆鐨?SkillHub锛堝 openclaw銆乼encent 绛夛級涓婃悳绱㈠彲鐢ㄧ殑 Skill",
			map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "鎼滅储鍏抽敭璇嶏紙濡?'git commit'銆?浠ｇ爜瀹℃煡'銆?閮ㄧ讲'锛?},
			}, []string{"query"}),
		toolDef("install_skill_hub", "浠?SkillHub 瀹夎鎸囧畾鐨?Skill 鍒版湰鍦般€傝缃?auto_run=true 鍙畨瑁呭悗绔嬪嵆鎵ц銆?,
			map[string]interface{}{
				"skill_id": map[string]string{"type": "string", "description": "Skill ID锛堜粠 search_skill_hub 缁撴灉涓幏鍙栵級"},
				"hub_url":  map[string]string{"type": "string", "description": "鏉ユ簮 Hub URL锛堜粠 search_skill_hub 缁撴灉涓幏鍙栵級"},
				"auto_run": map[string]string{"type": "boolean", "description": "瀹夎鎴愬姛鍚庢槸鍚︾珛鍗虫墽琛岋紙榛樿 true锛?},
			}, []string{"skill_id", "hub_url"}),
		toolDef("run_skill", "鎵ц鎸囧畾鐨?Skill",
			map[string]interface{}{
				"name": map[string]string{"type": "string", "description": "Skill 鍚嶇О"},
			}, []string{"name"}),
		toolDef("parallel_execute", "骞惰鎵ц澶氫釜缂栫▼浠诲姟锛屾瘡涓换鍔″湪鐙珛浼氳瘽涓繍琛岋紙鏈€澶?涓級",
			map[string]interface{}{
				"tasks": map[string]interface{}{
					"type":        "array",
					"description": "浠诲姟鍒楄〃锛屾瘡涓换鍔″寘鍚?tool锛堝伐鍏峰悕锛夈€乨escription锛堜换鍔℃弿杩帮級銆乸roject_path锛堥」鐩矾寰勶級",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"tool":         map[string]string{"type": "string", "description": "宸ュ叿鍚嶇О"},
							"description":  map[string]string{"type": "string", "description": "浠诲姟鎻忚堪"},
							"project_path": map[string]string{"type": "string", "description": "椤圭洰璺緞"},
						},
					},
				},
			}, []string{"tasks"}),
		toolDef("recommend_tool", "鏍规嵁浠诲姟鎻忚堪鎺ㄨ崘鏈€鍚堥€傜殑缂栫▼宸ュ叿",
			map[string]interface{}{
				"task_description": map[string]string{"type": "string", "description": "浠诲姟鎻忚堪"},
			}, []string{"task_description"}),
		toolDef("craft_tool", "褰撶幇鏈夊伐鍏烽兘鏃犳硶瀹屾垚浠诲姟鏃讹紝鑷姩鐮旂┒闂骞剁敓鎴愯剼鏈潵瑙ｅ喅銆備細鐢?LLM 鐢熸垚浠ｇ爜銆佹墽琛屻€佸苟娉ㄥ唽涓哄彲澶嶇敤鐨?Skill銆傞€傜敤浜庢暟鎹鐞嗐€丄PI 璋冪敤銆佹枃浠惰浆鎹€佺郴缁熺鐞嗙瓑闇€瑕佺紪绋嬫墠鑳藉畬鎴愮殑浠诲姟銆?,
			map[string]interface{}{
				"task":          map[string]string{"type": "string", "description": "闇€瑕佸畬鎴愮殑浠诲姟鎻忚堪锛堣秺璇︾粏瓒婂ソ锛?},
				"language":      map[string]string{"type": "string", "description": "鑴氭湰璇█: python/bash/powershell/node锛堝彲閫夛紝鑷姩妫€娴嬶級"},
				"save_as_skill": map[string]string{"type": "boolean", "description": "鎵ц鎴愬姛鍚庢槸鍚︽敞鍐屼负 Skill 渚涗笅娆″鐢紙榛樿 true锛?},
				"skill_name":    map[string]string{"type": "string", "description": "Skill 鍚嶇О锛堝彲閫夛紝鑷姩鐢熸垚锛?},
				"timeout":       map[string]string{"type": "integer", "description": "鎵ц瓒呮椂绉掓暟锛堥粯璁?60锛屾渶澶?300锛?},
			}, []string{"task"}),
		// --- 鏈満鐩存帴鎿嶄綔宸ュ叿 ---
		toolDef("bash", "鍦ㄦ湰鏈虹洿鎺ユ墽琛?shell 鍛戒护锛堝鍒涘缓鐩綍銆佺Щ鍔ㄦ枃浠躲€佽繍琛岃剼鏈瓑锛夈€傚懡浠ゅ湪 MaClaw 鎵€鍦ㄨ澶囦笂鎵ц锛屼笉闇€瑕佷細璇濄€?,
			map[string]interface{}{
				"command":     map[string]string{"type": "string", "description": "瑕佹墽琛岀殑 shell 鍛戒护"},
				"working_dir": map[string]string{"type": "string", "description": "宸ヤ綔鐩綍锛堝彲閫夛紝榛樿涓虹敤鎴蜂富鐩綍锛?},
				"timeout":     map[string]string{"type": "integer", "description": "瓒呮椂绉掓暟锛堝彲閫夛紝榛樿 30锛屾渶澶?120锛?},
			}, []string{"command"}),
		toolDef("read_file", "璇诲彇鏈満鏂囦欢鍐呭",
			map[string]interface{}{
				"path":  map[string]string{"type": "string", "description": "鏂囦欢璺緞锛堢粷瀵硅矾寰勬垨鐩稿浜庝富鐩綍鐨勮矾寰勶級"},
				"lines": map[string]string{"type": "integer", "description": "鏈€澶氳鍙栬鏁帮紙鍙€夛紝榛樿 200锛?},
			}, []string{"path"}),
		toolDef("write_file", "鍐欏叆鍐呭鍒版湰鏈烘枃浠讹紙浼氬垱寤轰笉瀛樺湪鐨勭洰褰曪級",
			map[string]interface{}{
				"path":    map[string]string{"type": "string", "description": "鏂囦欢璺緞"},
				"content": map[string]string{"type": "string", "description": "鏂囦欢鍐呭"},
			}, []string{"path", "content"}),
		toolDef("list_directory", "鍒楀嚭鏈満鐩綍鍐呭",
			map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "鐩綍璺緞锛堝彲閫夛紝榛樿涓虹敤鎴蜂富鐩綍锛?},
			}, nil),
		toolDef("send_file", "璇诲彇鏈満鏂囦欢骞跺彂閫佺粰鐢ㄦ埛锛堥€氳繃 IM 閫氶亾鐩存帴鍙戦€佹枃浠讹級銆傝缃?forward_to_im=true 鍙皢鏂囦欢鍚屾椂杞彂鍒扮敤鎴风殑椋炰功/寰俊/QQ绛?IM 骞冲彴銆?,
			map[string]interface{}{
				"path":          map[string]string{"type": "string", "description": "鏂囦欢鐨勭粷瀵硅矾寰勬垨鐩稿浜庝富鐩綍鐨勮矾寰?},
				"file_name":     map[string]string{"type": "string", "description": "鍙戦€佹椂鏄剧ず鐨勬枃浠跺悕锛堝彲閫夛紝榛樿浣跨敤鍘熸枃浠跺悕锛?},
				"forward_to_im": map[string]string{"type": "boolean", "description": "鏄惁鍚屾椂杞彂鍒扮敤鎴风殑 IM 骞冲彴锛堥涔?寰俊/QQ绛夛級銆備粎鍦ㄧ敤鎴锋槑纭姹傚彂閫佸埌椋炰功銆佸井淇°€丵Q绛?IM 鏃惰涓?true锛岄粯璁?false"},
			}, []string{"path"}),
		toolDef("open", "鐢ㄦ搷浣滅郴缁熼粯璁ょ▼搴忔墦寮€鏂囦欢鎴栫綉鍧€銆備緥濡傦細鎵撳紑 PDF 鐢ㄩ粯璁ら槄璇诲櫒銆佹墦寮€ .xlsx 鐢?Excel銆佹墦寮€ URL 鐢ㄩ粯璁ゆ祻瑙堝櫒銆佹墦寮€鏂囦欢澶圭敤璧勬簮绠＄悊鍣ㄣ€備篃鏀寔 mailto: 閾炬帴銆?,
			map[string]interface{}{
				"target": map[string]string{"type": "string", "description": "瑕佹墦寮€鐨勬枃浠惰矾寰勩€佺洰褰曡矾寰勬垨 URL锛堝 C:\\Users\\test\\doc.pdf銆乭ttps://example.com銆乵ailto:test@example.com锛?},
			}, []string{"target"}),
		// --- 闀挎湡璁板繂宸ュ叿锛堝悎骞讹級 ---
		toolDef("memory", "绠＄悊闀挎湡璁板繂锛坅ction: recall/save/list/delete锛夈€俽ecall 鎸夐渶妫€绱㈢浉鍏宠蹇嗭紝save 淇濆瓨鏂拌蹇嗐€?,
			map[string]interface{}{
				"action":   map[string]string{"type": "string", "description": "鎿嶄綔: recall(鎸夐渶鍙洖)/save(淇濆瓨)/list(鍒楀嚭鎴栨悳绱?/delete(鍒犻櫎)"},
				"query":    map[string]string{"type": "string", "description": "妫€绱㈠叧閿瘝锛坮ecall 鏃跺繀濉紝鐢变綘鎻愮偧鐨勭簿鍑嗘绱㈣瘝锛岄潪鐢ㄦ埛鍘熷娑堟伅锛?},
				"content":  map[string]string{"type": "string", "description": "璁板繂鍐呭锛坰ave 鏃跺繀濉級"},
				"category": map[string]string{"type": "string", "description": "绫诲埆: user_fact/preference/project_knowledge/instruction锛坰ave 鏃跺繀濉紝recall/list 鏃跺彲閫夎繃婊わ級"},
				"tags": map[string]interface{}{
					"type":        "array",
					"description": "鍏宠仈鏍囩锛坰ave 鏃跺彲閫夛級",
					"items":       map[string]string{"type": "string"},
				},
				"keyword": map[string]string{"type": "string", "description": "鎸夊叧閿瘝鎼滅储锛坙ist 鏃跺彲閫夛級"},
				"id":      map[string]string{"type": "string", "description": "璁板繂鏉＄洰 ID锛坉elete 鏃跺繀濉級"},
			}, []string{"action"}),
		// --- 浼氳瘽妯℃澘宸ュ叿 ---
		toolDef("create_template", "鍒涘缓浼氳瘽妯℃澘锛堝揩鎹峰惎鍔ㄩ厤缃級",
			map[string]interface{}{
				"name":         map[string]string{"type": "string", "description": "妯℃澘鍚嶇О"},
				"tool":         map[string]string{"type": "string", "description": "宸ュ叿鍚嶇О"},
				"project_path": map[string]string{"type": "string", "description": "椤圭洰璺緞"},
				"model_config": map[string]string{"type": "string", "description": "妯″瀷閰嶇疆"},
				"yolo_mode":    map[string]string{"type": "boolean", "description": "鏄惁寮€鍚?Yolo 妯″紡"},
			}, []string{"name", "tool"}),
		toolDef("list_templates", "鍒楀嚭鎵€鏈変細璇濇ā鏉?, nil, nil),
		toolDef("launch_template", "浣跨敤妯℃澘鍚姩浼氳瘽",
			map[string]interface{}{
				"template_name": map[string]string{"type": "string", "description": "妯℃澘鍚嶇О"},
			}, []string{"template_name"}),
		// --- 閰嶇疆绠＄悊宸ュ叿 ---
		toolDef("get_config", "鑾峰彇鎸囧畾閰嶇疆鍖哄煙鐨勫綋鍓嶅€?,
			map[string]interface{}{
				"section": map[string]string{"type": "string", "description": "閰嶇疆鍖哄煙鍚嶇О锛堝 claude/gemini/remote/projects/maclaw_llm/proxy/general锛夛紝涓虹┖鎴?all 杩斿洖姒傝"},
			}, []string{"section"}),
		toolDef("update_config", "淇敼鍗曚釜閰嶇疆椤?,
			map[string]interface{}{
				"section": map[string]string{"type": "string", "description": "閰嶇疆鍖哄煙鍚嶇О"},
				"key":     map[string]string{"type": "string", "description": "閰嶇疆椤瑰悕绉?},
				"value":   map[string]string{"type": "string", "description": "鏂板€?},
			}, []string{"section", "key", "value"}),
		toolDef("batch_update_config", "鎵归噺淇敼閰嶇疆锛堝師瀛愭€э紝浠讳竴椤瑰け璐ュ垯鍏ㄩ儴鍥炴粴锛?,
			map[string]interface{}{
				"changes": map[string]string{"type": "string", "description": "JSON 鏁扮粍锛屾瘡椤瑰寘鍚?section/key/value锛屼緥濡?[{\"section\":\"general\",\"key\":\"language\",\"value\":\"en\"}]"},
			}, []string{"changes"}),
		toolDef("list_config_schema", "鍒楀嚭鎵€鏈夊彲閰嶇疆椤圭殑 schema 淇℃伅", nil, nil),
		toolDef("export_config", "瀵煎嚭褰撳墠閰嶇疆锛堟晱鎰熷瓧娈靛凡鑴辨晱锛?, nil, nil),
		toolDef("import_config", "瀵煎叆閰嶇疆锛圝SON 鏍煎紡锛屼繚鐣欐湰鏈虹壒鏈夊瓧娈碉級",
			map[string]interface{}{
				"json_data": map[string]string{"type": "string", "description": "瑕佸鍏ョ殑閰嶇疆 JSON 瀛楃涓?},
			}, []string{"json_data"}),
		// --- Agent 鑷鐞嗗伐鍏?---
		toolDef("set_max_iterations", fmt.Sprintf("璋冩暣鏈€澶ф帹鐞嗚疆鏁般€傝缃悗浼氭寔涔呭寲淇濆瓨锛屽悗缁璇濅篃浼氱敓鏁堛€傚綋浣犲垽鏂换鍔″鏉傞渶瑕佹洿澶氳疆娆℃椂璋冪敤姝ゅ伐鍏锋墿灞曚笂闄愶紝浠诲姟绠€鍗曟椂鍙缉鍑忋€傝寖鍥?%d-%d銆?, minAgentIterations, maxAgentIterationsCap),
			map[string]interface{}{
				"max_iterations": map[string]string{"type": "integer", "description": fmt.Sprintf("鏂扮殑鏈€澶ц疆鏁帮紙%d-%d锛?, minAgentIterations, maxAgentIterationsCap)},
				"reason":         map[string]string{"type": "string", "description": "璋冩暣鍘熷洜锛堢敤浜庢棩蹇楄褰曪級"},
			}, []string{"max_iterations"}),
		// --- 瀹氭椂浠诲姟宸ュ叿 ---
		toolDef("create_scheduled_task", "鍒涘缓瀹氭椂浠诲姟銆傜敤鎴疯 姣忓ぉ9鐐瑰仛XX銆佹瘡鍛ㄤ竴涓嬪崍3鐐瑰仛YY銆佷粠3鏈?鍙峰埌15鍙锋瘡澶╀笂鍗?0鐐瑰仛ZZ 鏃讹紝瑙ｆ瀽鍑烘椂闂村弬鏁板苟璋冪敤姝ゅ伐鍏枫€俤ay_of_week: -1=姣忓ぉ, 0=鍛ㄦ棩, 1=鍛ㄤ竴...6=鍛ㄥ叚銆俤ay_of_month: -1=涓嶉檺, 1-31=姣忔湀鍑犲彿銆傞噸瑕侊細濡傛灉鐢ㄦ埛璇寸殑鏄竴娆℃€т换鍔★紙濡?浠婂ぉ涓崍鎻愰啋鎴?銆?鏄庡ぉ涓嬪崍3鐐瑰仛XX'锛夛紝蹇呴』灏?start_date 鍜?end_date 閮借涓虹洰鏍囨棩鏈燂紝纭繚鍙墽琛屼竴娆°€?,
			map[string]interface{}{
				"name":         map[string]string{"type": "string", "description": "浠诲姟鍚嶇О锛堢畝鐭弿杩帮級"},
				"action":       map[string]string{"type": "string", "description": "鍒版椂瑕佹墽琛岀殑鎿嶄綔锛堣嚜鐒惰瑷€鎻忚堪锛屼細鍙戦€佺粰 agent 鎵ц锛?},
				"hour":         map[string]string{"type": "integer", "description": "鎵ц鏃堕棿-灏忔椂锛?-23锛?},
				"minute":       map[string]string{"type": "integer", "description": "鎵ц鏃堕棿-鍒嗛挓锛?-59锛岄粯璁?锛?},
				"day_of_week":  map[string]string{"type": "integer", "description": "鏄熸湡鍑狅紙-1=姣忓ぉ, 0=鍛ㄦ棩, 1=鍛ㄤ竴...6=鍛ㄥ叚锛岄粯璁?1锛?},
				"day_of_month": map[string]string{"type": "integer", "description": "姣忔湀鍑犲彿锛?1=涓嶉檺, 1-31锛岄粯璁?1锛?},
				"start_date":   map[string]string{"type": "string", "description": "鐢熸晥寮€濮嬫棩鏈燂紙鏍煎紡 2006-01-02锛屽彲閫夛級"},
				"end_date":     map[string]string{"type": "string", "description": "鐢熸晥缁撴潫鏃ユ湡锛堟牸寮?2006-01-02锛屽彲閫夛級"},
			}, []string{"name", "action", "hour"}),
		toolDef("list_scheduled_tasks", "鍒楀嚭鎵€鏈夊畾鏃朵换鍔″強鍏剁姸鎬併€佷笅娆℃墽琛屾椂闂?, nil, nil),
		toolDef("delete_scheduled_task", "鍒犻櫎瀹氭椂浠诲姟锛堟寜 ID 鎴栧悕绉帮級",
			map[string]interface{}{
				"id":   map[string]string{"type": "string", "description": "浠诲姟 ID锛堜紭鍏堬級"},
				"name": map[string]string{"type": "string", "description": "浠诲姟鍚嶇О锛圛D 涓虹┖鏃舵寜鍚嶇О鍖归厤锛?},
			}, nil),
		toolDef("update_scheduled_task", "淇敼瀹氭椂浠诲姟鐨勬椂闂存垨鍐呭",
			map[string]interface{}{
				"id":           map[string]string{"type": "string", "description": "浠诲姟 ID锛堝繀濉級"},
				"name":         map[string]string{"type": "string", "description": "鏂板悕绉帮紙鍙€夛級"},
				"action":       map[string]string{"type": "string", "description": "鏂扮殑鎵ц鍐呭锛堝彲閫夛級"},
				"hour":         map[string]string{"type": "integer", "description": "鏂扮殑灏忔椂锛堝彲閫夛級"},
				"minute":       map[string]string{"type": "integer", "description": "鏂扮殑鍒嗛挓锛堝彲閫夛級"},
				"day_of_week":  map[string]string{"type": "integer", "description": "鏂扮殑鏄熸湡鍑狅紙鍙€夛級"},
				"day_of_month": map[string]string{"type": "integer", "description": "鏂扮殑姣忔湀鍑犲彿锛堝彲閫夛級"},
				"start_date":   map[string]string{"type": "string", "description": "鏂扮殑寮€濮嬫棩鏈燂紙鍙€夛級"},
				"end_date":     map[string]string{"type": "string", "description": "鏂扮殑缁撴潫鏃ユ湡锛堝彲閫夛級"},
			}, []string{"id"}),
	}

	// ---------- ClawNet tools (dynamic 鈥?only when daemon is running) ----------
	if h.app != nil && h.app.clawNetClient != nil && h.app.clawNetClient.IsRunning() {
		defs = append(defs,
			toolDef("clawnet_search", "鍦ㄨ櫨缃戯紙ClawNet P2P 鐭ヨ瘑缃戠粶锛変腑鎼滅储鐭ヨ瘑鏉＄洰銆傝繑鍥炲尮閰嶇殑鐭ヨ瘑鍒楄〃锛屽寘鍚爣棰樸€佸唴瀹广€佷綔鑰呯瓑銆?,
				map[string]interface{}{
					"query": map[string]string{"type": "string", "description": "鎼滅储鍏抽敭璇?},
				}, []string{"query"}),
			toolDef("clawnet_publish", "鍚戣櫨缃戯紙ClawNet P2P 鐭ヨ瘑缃戠粶锛夊彂甯冧竴鏉＄煡璇嗘潯鐩€傚彂甯冨悗鍏朵粬鑺傜偣鍙互鎼滅储鍒般€?,
				map[string]interface{}{
					"title": map[string]string{"type": "string", "description": "鐭ヨ瘑鏍囬"},
					"body":  map[string]string{"type": "string", "description": "鐭ヨ瘑鍐呭锛圡arkdown 鏍煎紡锛?},
				}, []string{"title", "body"}),
		)
	}

	// ---------- Web search & fetch tools ----------
	defs = append(defs,
		toolDef("web_search", "鎼滅储浜掕仈缃戝唴瀹广€傝繑鍥炴悳绱㈢粨鏋滃垪琛紙鏍囬銆乁RL銆佹憳瑕侊級銆傞€傜敤浜庢煡鎵捐祫鏂欍€佹妧鏈枃妗ｃ€佹渶鏂颁俊鎭瓑銆?,
			map[string]interface{}{
				"query":       map[string]string{"type": "string", "description": "鎼滅储鍏抽敭璇?},
				"max_results": map[string]string{"type": "integer", "description": "鏈€澶х粨鏋滄暟锛堥粯璁?8锛屾渶澶?20锛?},
			}, []string{"query"}),
		toolDef("web_fetch", "鎶撳彇鎸囧畾 URL 鐨勭綉椤靛唴瀹瑰苟鎻愬彇姝ｆ枃鏂囨湰銆傛敮鎸?HTTP/HTTPS/FTP 鍗忚锛岃嚜鍔ㄧ紪鐮佹娴嬶紙GBK/UTF-8 绛夛級銆丠TML 姝ｆ枃鎻愬彇銆傚彲閫?JS 娓叉煋锛堥渶鏈満瀹夎 Chrome锛夈€備篃鍙敤 save_path 涓嬭浇鏂囦欢鍒版湰鍦般€?,
			map[string]interface{}{
				"url":       map[string]string{"type": "string", "description": "瑕佹姄鍙栫殑 URL锛堟敮鎸?http/https/ftp 鍗忚锛?},
				"render_js": map[string]string{"type": "boolean", "description": "鏄惁浣跨敤 Chrome 娓叉煋 JS锛堝彲閫夛紝榛樿 false銆傞€傜敤浜?SPA 绛?JS 娓叉煋椤甸潰锛?},
				"save_path": map[string]string{"type": "string", "description": "淇濆瓨鏂囦欢璺緞锛堝彲閫夈€傛寚瀹氬悗灏嗗師濮嬪唴瀹逛繚瀛樺埌鏂囦欢鑰岄潪杩斿洖鏂囨湰锛岄€傜敤浜庝笅杞芥枃浠讹級"},
				"timeout":   map[string]string{"type": "integer", "description": "瓒呮椂绉掓暟锛堝彲閫夛紝榛樿 30锛屾渶澶?120锛?},
			}, []string{"url"}),
	)

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
			result = fmt.Sprintf("宸ュ叿鎵ц寮傚父: %v", r)
		}
	}()

	var args map[string]interface{}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("鍙傛暟瑙ｆ瀽澶辫触: %s", err.Error())
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

	return fmt.Sprintf("鏈煡宸ュ叿: %s", name)
}

func (h *IMMessageHandler) toolListSessions() string {
	if h.manager == nil {
		return "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	sessions := h.manager.List()
	if len(sessions) == 0 {
		return "褰撳墠娌℃湁娲昏穬浼氳瘽銆?
	}
	var b strings.Builder
	for _, s := range sessions {
		s.mu.RLock()
		status := string(s.Status)
		task := s.Summary.CurrentTask
		waiting := s.Summary.WaitingForUser
		s.mu.RUnlock()
		b.WriteString(fmt.Sprintf("- [%s] 宸ュ叿=%s 鏍囬=%s 鐘舵€?%s", s.ID, s.Tool, s.Title, status))
		if task != "" {
			b.WriteString(fmt.Sprintf(" 浠诲姟=%s", task))
		}
		if waiting {
			b.WriteString(" [绛夊緟鐢ㄦ埛杈撳叆]")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Non-coding task guard 鈥?prevents create_session for non-coding requests
// ---------------------------------------------------------------------------

// nonCodingKeywords are phrases that strongly indicate a non-coding task.
// When the user message contains these AND none of the coding keywords,
// we block session creation and guide the LLM to use direct tools instead.
// All entries MUST be lowercase (matched against lowercased user text).
var nonCodingKeywords = []string{
	"鎼滅储璁烘枃", "鎼滆鏂?, "鎵捐鏂?, "鏌ヨ鏂?, "涓嬭浇璁烘枃",
	"缈昏瘧", "鍏ㄦ枃缈昏瘧", "缈昏瘧鎴愪腑鏂?, "缈昏瘧鎴愯嫳鏂?,
	"鐢熸垚pdf", "鐢熸垚 pdf", "瀵煎嚭pdf", "瀵煎嚭 pdf",
	"鏌ュぉ姘?, "澶╂皵棰勬姤", "浠婂ぉ澶╂皵",
	"鏌ュ揩閫?, "蹇€掑崟鍙?, "鐗╂祦鏌ヨ",
	"鎼滅储鏂伴椈", "鏌ユ柊闂?, "鏈€鏂版柊闂?,
	"鎬荤粨鏂囩珷", "鎬荤粨璁烘枃", "鎽樿",
	"鍙戦偖浠?, "鍐欓偖浠?, "鍙戦€侀偖浠?,
	"鎻愰啋鎴?, "璁句釜闂归挓",
	"鎾斁闊充箰", "鏀鹃姝?,
	"arxiv",
}

// codingKeywords are phrases that indicate a genuine coding task.
// If any of these appear, the guard does NOT block session creation.
// All entries MUST be lowercase (matched against lowercased user text).
var codingKeywords = []string{
	"鍐欎唬鐮?, "缂栫▼", "寮€鍙?, "淇産ug", "淇?bug", "淇bug", "淇 bug",
	"閲嶆瀯", "refactor", "瀹炵幇", "娣诲姞鍔熻兘", "鏂板鍔熻兘",
	"鍐欒剼鏈?, "鍐欎竴涓剼鏈?, "鍐欎釜鑴氭湰",
	"鍐欏嚱鏁?, "鍐欐柟娉?, "鍐欐帴鍙?, "鍐檃pi", "鍐?api",
	"浠ｇ爜", "婧愮爜", "婧愪唬鐮?,
	"缂栬瘧", "鏋勫缓", "build", "compile",
	"娴嬭瘯", "鍗曞厓娴嬭瘯", "test",
	"閮ㄧ讲", "deploy",
	"pull request", "merge request",
	"git commit", "git push",
	"create_session", // explicit tool name = intentional
}

// checkNonCodingTaskGuard returns a non-empty hint string when the current
// user message looks like a non-coding task. Returns "" to allow session creation.
func (h *IMMessageHandler) checkNonCodingTaskGuard() string {
	msg := strings.ToLower(h.lastUserText)
	if msg == "" {
		return "" // no context available, allow
	}

	// If any coding keyword is present, always allow.
	for _, kw := range codingKeywords {
		if strings.Contains(msg, kw) {
			return ""
		}
	}

	// Check for non-coding keywords.
	matched := ""
	for _, kw := range nonCodingKeywords {
		if strings.Contains(msg, kw) {
			matched = kw
			break
		}
	}
	if matched == "" {
		return "" // no non-coding signal, allow
	}

	return fmt.Sprintf(`鈿狅笍 浠诲姟绫诲瀷妫€娴嬶細褰撳墠璇锋眰鐪嬭捣鏉ヤ笉鏄紪绋嬩换鍔★紙妫€娴嬪埌鍏抽敭璇嶏細%q锛夛紝涓嶉渶瑕佸垱寤虹紪绋嬩細璇濄€?

璇风洿鎺ヤ娇鐢ㄤ互涓嬪伐鍏峰畬鎴愪换鍔★細
- bash锛氭墽琛屽懡浠よ鎿嶄綔锛堝 curl 涓嬭浇銆佽剼鏈墽琛岋級
- craft_tool锛氳嚜鍔ㄧ敓鎴愬苟鎵ц鑴氭湰锛堥€傚悎鏁版嵁澶勭悊銆丄PI 璋冪敤銆佹枃浠惰浆鎹級
- read_file / write_file锛氳鍐欐湰鍦版枃浠?
- send_file锛氬皢鏂囦欢鍙戦€佺粰鐢ㄦ埛
- open锛氭墦寮€鏂囦欢鎴栫綉鍧€
- memory锛氫繚瀛?妫€绱俊鎭?

濡傛灉纭疄闇€瑕佺紪绋嬩細璇濓紝璇峰湪涓嬩竴杞噸鏂拌皟鐢?create_session銆俙, matched)
}

func (h *IMMessageHandler) toolCreateSession(args map[string]interface{}) string {
	// --- Non-coding task guard ---
	// Detect when the LLM incorrectly tries to create a coding session for
	// tasks that don't require one (e.g. search, translate, generate PDF).
	if hint := h.checkNonCodingTaskGuard(); hint != "" {
		return hint
	}

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
			hints = append(hints, fmt.Sprintf("馃敡 鑷姩鎺ㄨ崘宸ュ叿: %s锛?s锛?, tool, reason))
		}
	}
	if tool == "" {
		return "缂哄皯 tool 鍙傛暟锛屼笖鏃犳硶鑷姩鎺ㄨ崘宸ュ叿"
	}

	// Resolve project_id to project path (takes priority over project_path).
	cfg, cfgErr := h.app.LoadConfig()
	if cfgErr != nil {
		return fmt.Sprintf("鍔犺浇閰嶇疆澶辫触: %s", cfgErr.Error())
	}
	if projectID != "" {
		var found bool
		for _, p := range cfg.Projects {
			if p.Id == projectID {
				projectPath = p.Path
				found = true
				hints = append(hints, fmt.Sprintf("馃搧 閫氳繃椤圭洰 ID 瑙ｆ瀽: %s 鈫?%s", projectID, p.Path))
				break
			}
		}
		if !found {
			var available []string
			for _, p := range cfg.Projects {
				available = append(available, fmt.Sprintf("%s(%s)", p.Id, p.Name))
			}
			if len(available) == 0 {
				return fmt.Sprintf("椤圭洰 ID %q 鏈壘鍒帮紝褰撳墠娌℃湁宸查厤缃殑椤圭洰", projectID)
			}
			return fmt.Sprintf("椤圭洰 ID %q 鏈壘鍒帮紝鍙敤椤圭洰: %s", projectID, strings.Join(available, ", "))
		}
	}

	// Smart project detection when project_path is empty.
	if projectPath == "" && h.contextResolver != nil {
		detected, reason := h.contextResolver.ResolveProject()
		if detected != "" {
			projectPath = detected
			hints = append(hints, fmt.Sprintf("馃搧 鑷姩妫€娴嬮」鐩? %s锛?s锛?, projectPath, reason))
		}
	}

	// Pre-launch environment check.
	if h.sessionPrecheck != nil {
		result := h.sessionPrecheck.Check(tool, projectPath)
		if !result.ToolReady {
			hints = append(hints, fmt.Sprintf("鈿狅笍 宸ュ叿棰勬鏈€氳繃: %s", result.ToolHint))
		}
		if !result.ProjectReady {
			hints = append(hints, "鈿狅笍 椤圭洰璺緞涓嶅瓨鍦ㄦ垨鏃犳硶璁块棶")
		}
		if !result.ModelReady {
			hints = append(hints, fmt.Sprintf("鈿狅笍 妯″瀷棰勬鏈€氳繃: %s", result.ModelHint))
		}
		if result.AllPassed {
			hints = append(hints, "鉁?鐜棰勬鍏ㄩ儴閫氳繃")
		}
		// Block session creation when the tool binary is missing 鈥?launching
		// a process that doesn't exist always exits immediately with code 1,
		// wasting a session slot and confusing the user with a cryptic error.
		if !result.ToolReady {
			return strings.Join(hints, "\n") + "\n鉂?宸ュ叿鏈畨瑁咃紝鏃犳硶鍒涘缓浼氳瘽銆傝鍏堝湪妗岄潰绔畨瑁?" + tool + " 鍚庨噸璇曘€?
		}
	}

	// ProviderResolver integration: resolve provider before starting session.
	toolCfg, tcErr := remoteToolConfig(cfg, tool)
	if tcErr != nil {
		return fmt.Sprintf("鑾峰彇宸ュ叿閰嶇疆澶辫触: %s", tcErr.Error())
	}

	resolver := &ProviderResolver{}
	resolveResult, resolveErr := resolver.Resolve(toolCfg, provider)
	if resolveErr != nil {
		errMsg := fmt.Sprintf("鉂?鏃犳硶鍒涘缓浼氳瘽锛?s\n璇峰湪妗岄潰绔负 %s 閰嶇疆鑷冲皯涓€涓湁鏁堢殑鏈嶅姟鍟嗐€?, resolveErr.Error(), tool)
		return errMsg
	}
	if resolveResult.Fallback {
		hints = append(hints, fmt.Sprintf("鈿?鏈嶅姟鍟嗗凡闄嶇骇: %s 鈫?%s", resolveResult.OriginalName, resolveResult.Provider.ModelName))
	}
	resolvedProvider := resolveResult.Provider.ModelName

	resumeSessionID, _ := args["resume_session_id"].(string)

	view, err := h.app.StartRemoteSessionForProject(RemoteStartSessionRequest{
		Tool: tool, ProjectPath: projectPath, Provider: resolvedProvider,
		LaunchSource:    RemoteLaunchSourceAI,
		ResumeSessionID: resumeSessionID,
	})
	if err != nil {
		errMsg := fmt.Sprintf("鉂?鍒涘缓浼氳瘽澶辫触: %s", err.Error())
		errMsg += fmt.Sprintf("\n馃挕 淇寤鸿:\n- 妫€鏌?%s 鏄惁宸插畨瑁呭苟鍙甯歌繍琛孿n- 纭椤圭洰璺緞 %s 瀛樺湪涓斿彲璁块棶\n- 浣跨敤 list_providers 鏌ョ湅鍙敤鏈嶅姟鍟嗛厤缃?, tool, projectPath)
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
	b.WriteString(fmt.Sprintf("鉁?浼氳瘽宸插垱寤?[%s]\n", view.ID))
	b.WriteString(fmt.Sprintf("馃敡 宸ュ叿: %s | 馃摝 鏈嶅姟鍟? %s | 馃搧 椤圭洰: %s\n", view.Tool, resolvedProvider, projectPath))
	b.WriteString(fmt.Sprintf("\n馃搵 涓嬩竴姝ユ搷浣滐細"))
	b.WriteString(fmt.Sprintf("\n1. 璋冪敤 get_session_output(session_id=%q) 纭浼氳瘽宸插惎鍔紙鐘舵€佷负 running锛?, view.ID))
	b.WriteString(fmt.Sprintf("\n2. 绔嬪嵆璋冪敤 send_and_observe(session_id=%q, text=\"缂栫▼鎸囦护\") 灏嗛渶姹傚彂閫佺粰缂栫▼宸ュ叿", view.ID))
	b.WriteString("\n鈿狅笍 缂栫▼宸ュ叿鍚姩鍚庣瓑寰呰緭鍏ワ紝涓嶅彂閫佹寚浠や笉浼氬紑濮嬪伐浣溿€傛渶澶氭鏌?2 娆?get_session_output锛岀‘璁?running 鍚庣珛鍗冲彂閫併€?)
	b.WriteString("\n馃洃 濡傛灉浼氳瘽宸查€€鍑猴紙exited锛変笖閫€鍑虹爜闈?0锛屼笉瑕侀噸璇曪紝鐩存帴鍛婄煡鐢ㄦ埛閿欒淇℃伅銆?)
	return b.String()
}

func (h *IMMessageHandler) toolListProviders(args map[string]interface{}) string {
	toolName, _ := args["tool"].(string)
	if toolName == "" {
		return "缂哄皯 tool 鍙傛暟"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return fmt.Sprintf("鍔犺浇閰嶇疆澶辫触: %s", err.Error())
	}
	toolCfg, err := remoteToolConfig(cfg, toolName)
	if err != nil {
		return fmt.Sprintf("涓嶆敮鎸佺殑宸ュ叿: %s", toolName)
	}
	valid := validProviders(toolCfg)
	if len(valid) == 0 {
		return fmt.Sprintf("宸ュ叿 %s 娌℃湁鍙敤鐨勬湇鍔″晢锛岃鍦ㄦ闈㈢閰嶇疆", toolName)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("宸ュ叿 %s 鐨勫彲鐢ㄦ湇鍔″晢:\n", toolName))
	for _, m := range valid {
		isDefault := ""
		if strings.EqualFold(m.ModelName, toolCfg.CurrentModel) {
			isDefault = " [褰撳墠榛樿]"
		}
		modelId := m.ModelId
		if len(modelId) > 20 {
			modelId = modelId[:20] + "..."
		}
		b.WriteString(fmt.Sprintf("  - %s (model_id=%s)%s\n", m.ModelName, modelId, isDefault))
	}
	return b.String()
}

func (h *IMMessageHandler) toolProjectManage(args map[string]interface{}) string {
	action, _ := args["action"].(string)
	action = strings.TrimSpace(action)
	switch action {
	case "create":
		name, _ := args["name"].(string)
		path, _ := args["path"].(string)
		res, err := project.Create(h.app, name, path)
		if err != nil {
			return fmt.Sprintf("鍒涘缓椤圭洰澶辫触: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": "created"})
		return string(data)
	case "list":
		items, err := project.List(h.app)
		if err != nil {
			return fmt.Sprintf("鍔犺浇閰嶇疆澶辫触: %v", err)
		}
		if len(items) == 0 {
			return "褰撳墠娌℃湁宸查厤缃殑椤圭洰銆傝鍦ㄦ闈㈢娣诲姞椤圭洰銆?
		}
		data, _ := json.Marshal(items)
		return string(data)
	case "delete":
		target, _ := args["target"].(string)
		res, err := project.Delete(h.app, target)
		if err != nil {
			return fmt.Sprintf("鍒犻櫎椤圭洰澶辫触: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "status": "deleted"})
		return string(data)
	case "switch":
		target, _ := args["target"].(string)
		res, err := project.Switch(h.app, target)
		if err != nil {
			return fmt.Sprintf("鍒囨崲椤圭洰澶辫触: %v", err)
		}
		data, _ := json.Marshal(map[string]string{"id": res.Id, "name": res.Name, "path": res.Path, "status": "switched"})
		return string(data)
	default:
		return fmt.Sprintf("鏈煡 action: %s锛堟敮鎸?create/list/delete/switch锛?, action)
	}
}

func (h *IMMessageHandler) toolSendInput(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	text, _ := args["text"].(string)
	if sessionID == "" || text == "" {
		return "缂哄皯 session_id 鎴?text 鍙傛暟"
	}
	if h.manager == nil {
		return "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	if err := h.manager.WriteInput(sessionID, text); err != nil {
		return fmt.Sprintf("鍙戦€佸け璐? %s", err.Error())
	}
	return fmt.Sprintf("宸插彂閫佸埌浼氳瘽 %s銆傗殸锔?浣犲繀椤荤珛鍗宠皟鐢?get_session_output(session_id=%q) 楠岃瘉鍛戒护鏄惁鎵ц鎴愬姛锛屼笉瑕佺洿鎺ュ憡璇夌敤鎴峰凡瀹屾垚銆?, sessionID, sessionID)
}

// writeAutoResumeHint appends auto-resume instructions to the output buffer
// when a structured session exits with a ResumeContext. The reason parameter
// provides a context-specific description of why the session exited.
func writeAutoResumeHint(b *strings.Builder, rc *SessionResumeContext, reason string) {
	if rc == nil {
		b.WriteString("\n鉁?缂栫▼宸ュ叿宸叉甯搁€€鍑恒€傚闇€缁х画锛岃鍒涘缓鏂颁細璇濄€?)
		return
	}
	if rc.ResumeCount >= 10 {
		b.WriteString("\n鉁?缂栫▼宸ュ叿宸查€€鍑恒€傚凡鑷姩缁帴 10 娆★紝寤鸿鍛婄煡鐢ㄦ埛褰撳墠杩涘害骞惰闂槸鍚︾户缁€?)
		return
	}
	b.WriteString(fmt.Sprintf("\n馃攧 %s", reason))
	b.WriteString(fmt.Sprintf("\n馃搶 鑷姩缁帴鎸囦护锛堢 %d 娆＄画鎺ワ級锛?, rc.ResumeCount+1))
	b.WriteString("\n1. 绔嬪嵆璋冪敤 create_session 鍒涘缓鏂颁細璇濓紙浣跨敤鐩稿悓鐨?tool 鍜?project_path锛?)
	b.WriteString("\n2. 鐢?send_and_observe 鍙戦€佺画鎺ユ寚浠わ細")
	b.WriteString("\n   銆岃妫€鏌ラ」鐩綋鍓嶇姸鎬侊紝缁х画瀹屾垚涔嬪墠鏈畬鎴愮殑浠诲姟銆?)
	if rc.OriginalTask != "" {
		b.WriteString(fmt.Sprintf("鍘熷浠诲姟锛?s銆?, rc.OriginalTask))
	}
	if rc.LastProgress != "" {
		b.WriteString(fmt.Sprintf("涓婃杩涘害锛?s銆?, rc.LastProgress))
	}
	if len(rc.CompletedFiles) > 0 {
		b.WriteString(fmt.Sprintf("宸插畬鎴愮殑鏂囦欢锛?s銆?, strings.Join(rc.CompletedFiles, ", ")))
	}
	b.WriteString("鏌ョ湅宸叉湁鏂囦欢锛岃ˉ鍏ㄧ己澶辩殑閮ㄥ垎锛岀‘淇濋」鐩彲浠ユ甯歌繍琛屻€傘€?)
	b.WriteString("\n鈿狅笍 涓嶈璇㈤棶鐢ㄦ埛鏄惁缁х画鈥斺€旂洿鎺ュ垱寤烘柊浼氳瘽缁帴銆備笉瑕佽嚜宸辩敤 write_file 鍐欎唬鐮併€?)
}

func (h *IMMessageHandler) toolGetSessionOutput(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缂哄皯 session_id 鍙傛暟"
	}
	if h.manager == nil {
		return "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("浼氳瘽 %s 涓嶅瓨鍦?, sessionID)
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
	// This avoids returning an empty "(鏆傛棤杈撳嚭)" result that causes the
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
	b.WriteString(fmt.Sprintf("浼氳瘽 %s 鐘舵€? %s\n", sessionID, status))
	if summary.CurrentTask != "" {
		b.WriteString(fmt.Sprintf("褰撳墠浠诲姟: %s\n", summary.CurrentTask))
	}
	if summary.ProgressSummary != "" {
		b.WriteString(fmt.Sprintf("杩涘害: %s\n", summary.ProgressSummary))
	}
	if summary.LastResult != "" {
		b.WriteString(fmt.Sprintf("鏈€杩戠粨鏋? %s\n", summary.LastResult))
	}
	if summary.LastCommand != "" {
		b.WriteString(fmt.Sprintf("鏈€杩戝懡浠? %s\n", summary.LastCommand))
	}
	if summary.WaitingForUser {
		b.WriteString("鈿狅笍 浼氳瘽姝ｅ湪绛夊緟鐢ㄦ埛杈撳叆\n")
	}
	if summary.SuggestedAction != "" {
		b.WriteString(fmt.Sprintf("寤鸿鎿嶄綔: %s\n", summary.SuggestedAction))
	}
	if len(rawLines) > 0 {
		start := 0
		if len(rawLines) > maxLines {
			start = len(rawLines) - maxLines
		}
		b.WriteString(fmt.Sprintf("\n--- 鏈€杩?%d 琛岃緭鍑?---\n", len(rawLines)-start))
		for _, line := range rawLines[start:] {
			b.WriteString(line)
			b.WriteString("\n")
		}
	} else {
		b.WriteString("\n(鏆傛棤杈撳嚭)\n")
		// When the session is running but has no output, it's likely waiting
		// for the first user message (SDK mode tools like Claude Code wait
		// for input after init). Hint the AI to send the task.
		if status == string(SessionRunning) {
			b.WriteString(fmt.Sprintf("\n馃搶 浼氳瘽宸插氨缁絾鏆傛棤杈撳嚭鈥斺€旂紪绋嬪伐鍏峰湪绛夊緟杈撳叆銆傝绔嬪嵆璋冪敤 send_and_observe(session_id=%q, text=\"缂栫▼鎸囦护\") 鍙戦€佷换鍔°€?, sessionID))
		} else if status == string(SessionStarting) {
			b.WriteString("\n鈴?浼氳瘽姝ｅ湪鍚姩涓紝璇风◢鍚庡啀娆¤皟鐢?get_session_output 妫€鏌ョ姸鎬侊紙鏈€澶氬啀妫€鏌?1 娆★級銆?)
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
			b.WriteString("\n鈴?缂栫▼宸ュ叿杈撳嚭鏆傚仠锛岀郴缁熸鍦ㄥ皾璇曟仮澶嶏紝璇风◢鍚庡啀妫€鏌?)
		case StallStateStuck:
			b.WriteString("\n鈿狅笍 缂栫▼宸ュ叿鍙兘宸插崱浣忥紝寤鸿鍙戦€佸叿浣撴寚浠ゆ垨缁堟浼氳瘽")
		default: // StallStateNormal
			b.WriteString("\n鈴?缂栫▼宸ュ叿姝ｅ湪宸ヤ綔涓紝璇风瓑寰呭悗鍐嶆鏌ヨ繘搴?)
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
			b.WriteString("\n鉁?浠诲姟浼间箮宸插畬鎴愶紝鍙互鏌ョ湅缁撴灉")
		case CompletionIncomplete:
			b.WriteString("\n鈿狅笍 浠诲姟浼间箮鏈畬鎴愶紝寤鸿鍙戦€併€岀户缁€嶈缂栫▼宸ュ叿缁х画宸ヤ綔")
			// CompletionUncertain: 淇濇寔鐜版湁榛樿鎻愮ず锛?鈿狅笍 浼氳瘽姝ｅ湪绛夊緟鐢ㄦ埛杈撳叆"锛?
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
		// Structured sessions (Claude Code SDK, Gemini ACP, Codex, iFlow)
		// normally exit with code 1 鈥?this is NOT a failure.
		if session.isStructuredSession() && *exitCodeVal == 1 {
			// Check if there's a resume context indicating unfinished work.
			session.mu.RLock()
			rc := session.ResumeContext
			session.mu.RUnlock()

			writeAutoResumeHint(&b, rc, "缂栫▼宸ュ叿鍥?token 鑰楀敖姝ｅ父閫€鍑猴紝浣嗕换鍔″彲鑳芥湭瀹屾垚銆?)
		} else if session.isStructuredSession() && *exitCodeVal > 1 {
			// API error or unexpected crash 鈥?allow retry.
			session.mu.RLock()
			rc := session.ResumeContext
			session.mu.RUnlock()

			if rc != nil && rc.ExitReason == "api_error" && rc.ResumeCount < 2 {
				b.WriteString(fmt.Sprintf("\n鈿狅笍 缂栫▼宸ュ叿寮傚父閫€鍑猴紙閫€鍑虹爜 %d锛夛紝鍙兘鏄笂娓?API 涓嶇ǔ瀹氥€?, *exitCodeVal))
				b.WriteString(fmt.Sprintf("\n馃搶 鑷姩閲嶈瘯锛堢 %d 娆★級锛氱珛鍗冲垱寤烘柊浼氳瘽閲嶈瘯锛屼娇鐢ㄧ浉鍚屽弬鏁般€?, rc.ResumeCount+1))
				b.WriteString("\n涓嶈璇㈤棶鐢ㄦ埛鈥斺€旂洿鎺ラ噸璇曘€?)
			} else {
				b.WriteString(fmt.Sprintf("\n馃洃 浼氳瘽宸插け璐ラ€€鍑猴紙閫€鍑虹爜 %d锛夈€備笉瑕佸啀瀵规浼氳瘽璋冪敤浠讳綍宸ュ叿銆?, *exitCodeVal))
				b.WriteString(fmt.Sprintf("\n璇风珛鍗冲皢閿欒淇℃伅鍛婄煡鐢ㄦ埛锛屽苟寤鸿妫€鏌?%s 鐨勫畨瑁呭拰閰嶇疆銆?, sessionTool))
				b.WriteString("\n涓嶈閲嶅鍒涘缓鏂颁細璇濋噸璇曗€斺€斿悓鏍风殑鐜闂浼氬鑷村悓鏍风殑澶辫触銆?)
			}
		} else {
			b.WriteString(fmt.Sprintf("\n馃洃 浼氳瘽宸插け璐ラ€€鍑猴紙閫€鍑虹爜 %d锛夈€備笉瑕佸啀瀵规浼氳瘽璋冪敤浠讳綍宸ュ叿銆?, *exitCodeVal))
			b.WriteString(fmt.Sprintf("\n璇风珛鍗冲皢閿欒淇℃伅鍛婄煡鐢ㄦ埛锛屽苟寤鸿妫€鏌?%s 鐨勫畨瑁呭拰閰嶇疆銆?, sessionTool))
			b.WriteString("\n涓嶈閲嶅鍒涘缓鏂颁細璇濋噸璇曗€斺€斿悓鏍风殑鐜闂浼氬鑷村悓鏍风殑澶辫触銆?)
		}
	}

	// Structured sessions that exit with code 0 may also have unfinished
	// work (e.g. Claude Code completed its max-turns but the task isn't
	// done). Check ResumeContext for these sessions too.
	if (sessionStatus == SessionExited) && exitCodeVal != nil && *exitCodeVal == 0 && session.isStructuredSession() {
		session.mu.RLock()
		rc := session.ResumeContext
		session.mu.RUnlock()

		writeAutoResumeHint(&b, rc, "缂栫▼宸ュ叿宸叉甯搁€€鍑猴紙鍙兘杈惧埌 max-turns 闄愬埗锛夛紝浠诲姟鍙兘鏈畬鎴愩€?)
	}

	return b.String()
}

func (h *IMMessageHandler) toolGetSessionEvents(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缂哄皯 session_id 鍙傛暟"
	}
	if h.manager == nil {
		return "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("浼氳瘽 %s 涓嶅瓨鍦?, sessionID)
	}
	session.mu.RLock()
	events := make([]ImportantEvent, len(session.Events))
	copy(events, session.Events)
	session.mu.RUnlock()
	if len(events) == 0 {
		return fmt.Sprintf("浼氳瘽 %s 鏆傛棤閲嶈浜嬩欢銆?, sessionID)
	}
	var b strings.Builder
	for _, ev := range events {
		b.WriteString(fmt.Sprintf("- [%s] %s: %s", ev.Severity, ev.Type, ev.Title))
		if ev.Summary != "" {
			b.WriteString(fmt.Sprintf(" 鈥?%s", ev.Summary))
		}
		if ev.RelatedFile != "" {
			b.WriteString(fmt.Sprintf(" (鏂囦欢: %s)", ev.RelatedFile))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolInterruptSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缂哄皯 session_id 鍙傛暟"
	}
	if h.manager == nil {
		return "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	if err := h.manager.Interrupt(sessionID); err != nil {
		return fmt.Sprintf("涓柇澶辫触: %s", err.Error())
	}
	return fmt.Sprintf("宸插悜浼氳瘽 %s 鍙戦€佷腑鏂俊鍙?, sessionID)
}

func (h *IMMessageHandler) toolKillSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "缂哄皯 session_id 鍙傛暟"
	}
	if h.manager == nil {
		return "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	if err := h.manager.Kill(sessionID); err != nil {
		return fmt.Sprintf("缁堟澶辫触: %s", err.Error())
	}
	return fmt.Sprintf("宸茬粓姝細璇?%s", sessionID)
}

// toolSendAndObserve combines send_input + get_session_output into a single
// tool call. It sends text to a session, waits briefly for output to
// accumulate, then returns the session output 鈥?saving one LLM round-trip.
func (h *IMMessageHandler) toolSendAndObserve(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	text, _ := args["text"].(string)
	if sessionID == "" || text == "" {
		return "缂哄皯 session_id 鎴?text 鍙傛暟"
	}
	if h.manager == nil {
		return "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	// Snapshot line count and image count BEFORE sending so we can detect new output/images.
	session, ok := h.manager.Get(sessionID)
	if !ok {
		return fmt.Sprintf("浼氳瘽 %s 涓嶅瓨鍦?, sessionID)
	}
	session.mu.RLock()
	baseLineCount := len(session.RawOutputLines)
	baseImageCount := len(session.OutputImages)
	session.mu.RUnlock()

	if err := h.manager.WriteInput(sessionID, text); err != nil {
		return fmt.Sprintf("鍙戦€佸け璐? %s", err.Error())
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

	// Check if session is still busy after polling 鈥?read stall state for precise hint.
	session.mu.RLock()
	stillBusy := session.Status == SessionBusy
	stallState := session.StallState
	session.mu.RUnlock()

	// Check if new images were produced during the command execution.
	// Images from SDK sessions are already delivered to the user via the
	// session.image WebSocket channel (Hub 鈫?Feishu notifier), so we do NOT
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
		output += fmt.Sprintf("\n\n馃摲 浼氳瘽浜х敓浜?%d 寮犲浘鐗囷紝宸茶嚜鍔ㄩ€氳繃 IM 鍙戦€佺粰鐢ㄦ埛銆?, newImageCount)
	}

	return output
}

// toolControlSession merges interrupt_session and kill_session into one tool.
func (h *IMMessageHandler) toolControlSession(args map[string]interface{}) string {
	sessionID, _ := args["session_id"].(string)
	action, _ := args["action"].(string)
	if sessionID == "" {
		return "缂哄皯 session_id 鍙傛暟"
	}
	if h.manager == nil {
		return "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	switch action {
	case "interrupt":
		if err := h.manager.Interrupt(sessionID); err != nil {
			return fmt.Sprintf("涓柇澶辫触: %s", err.Error())
		}
		return fmt.Sprintf("宸插悜浼氳瘽 %s 鍙戦€佷腑鏂俊鍙?, sessionID)
	case "kill":
		if err := h.manager.Kill(sessionID); err != nil {
			return fmt.Sprintf("缁堟澶辫触: %s", err.Error())
		}
		return fmt.Sprintf("宸茬粓姝細璇?%s", sessionID)
	default:
		return "action 鍙傛暟鏃犳晥锛屽彲閫夊€? interrupt, kill"
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
		return "action 鍙傛暟鏃犳晥锛屽彲閫夊€? get, update, batch_update, list_schema, export, import"
	}
}

// screenshotCooldown is the minimum interval between consecutive screenshots
// to prevent accidental rapid-fire captures by the LLM.
const screenshotCooldown = 30 * time.Second

func (h *IMMessageHandler) toolScreenshot(args map[string]interface{}) string {
	// Enforce cooldown to prevent accidental repeated screenshots.
	if !h.lastScreenshotAt.IsZero() && time.Since(h.lastScreenshotAt) < screenshotCooldown {
		remaining := screenshotCooldown - time.Since(h.lastScreenshotAt)
		return fmt.Sprintf("鎴睆鍐峰嵈涓紝璇风瓑寰?%d 绉掑悗鍐嶈瘯", int(remaining.Seconds())+1)
	}

	sessionID, _ := args["session_id"].(string)

	// 濡傛灉鏈寚瀹?session_id锛岃嚜鍔ㄩ€夋嫨鍞竴娲昏穬浼氳瘽
	if sessionID == "" && h.manager != nil {
		sessions := h.manager.List()
		if len(sessions) == 1 {
			sessionID = sessions[0].ID
		} else if len(sessions) > 1 {
			var lines []string
			lines = append(lines, "鏈夊涓椿璺冧細璇濓紝璇锋寚瀹?session_id锛?)
			for _, s := range sessions {
				s.mu.RLock()
				status := string(s.Status)
				s.mu.RUnlock()
				lines = append(lines, fmt.Sprintf("- %s (宸ュ叿=%s, 鐘舵€?%s)", s.ID, s.Tool, status))
			}
			return strings.Join(lines, "\n")
		} else {
			// 娌℃湁娲昏穬浼氳瘽鏃讹紝鐩存帴鎴睆鏈満灞忓箷锛堜笉渚濊禆 session锛?
			base64Data, err := h.manager.CaptureScreenshotDirect()
			if err != nil {
				return fmt.Sprintf("鎴浘澶辫触: %s", err.Error())
			}
			h.lastScreenshotAt = time.Now()
			return fmt.Sprintf("[screenshot_base64]%s", base64Data)
		}
	}

	if sessionID == "" {
		return "缂哄皯 session_id 鍙傛暟锛屼笖鏃犳硶鑷姩閫夋嫨浼氳瘽"
	}
	if h.manager == nil {
		return "浼氳瘽绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	// Non-desktop platforms (WeChat, QQ, etc.) cannot receive session.image
	// WebSocket pushes, so capture and return base64 data directly.
	platform := ""
	if h.currentLoopCtx != nil {
		platform = h.currentLoopCtx.Platform
	}
	if platform != "" && platform != "desktop" {
		base64Data, err := h.manager.CaptureScreenshotToBase64(sessionID)
		if err != nil {
			return fmt.Sprintf("鎴浘澶辫触: %s", err.Error())
		}
		h.lastScreenshotAt = time.Now()
		return fmt.Sprintf("[screenshot_base64]%s", base64Data)
	}

	if err := h.manager.CaptureScreenshot(sessionID); err != nil {
		return fmt.Sprintf("鎴浘澶辫触: %s", err.Error())
	}
	// 鎴浘宸查€氳繃 session.image 閫氶亾鐩存帴鍙戦€佺粰鐢ㄦ埛锛?
	// 杩斿洖鐗规畩鏍囪璁?runAgentLoop 绔嬪嵆缁堟锛岄伩鍏?Agent 缁х画鎺ㄧ悊瀵艰嚧閲嶅鍙戝浘銆?
	h.lastScreenshotAt = time.Now()
	return "[screenshot_sent]"
}

func (h *IMMessageHandler) toolListMCPTools() string {
	var b strings.Builder
	hasAny := false

	// List local (stdio) MCP servers
	if mgr := h.app.localMCPManager; mgr != nil {
		for _, ts := range mgr.GetAllTools() {
			hasAny = true
			b.WriteString(fmt.Sprintf("## %s (%s) [鏈湴/stdio]\n", ts.ServerName, ts.ServerID))
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
			b.WriteString(fmt.Sprintf("## %s (%s) [杩滅▼/HTTP] 鐘舵€?%s\n", s.Name, s.ID, s.HealthStatus))
			tools := registry.GetServerTools(s.ID)
			if len(tools) == 0 {
				b.WriteString("  (鏃犲伐鍏锋垨鏃犳硶鑾峰彇)\n")
				continue
			}
			for _, t := range tools {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", t.Name, t.Description))
			}
		}
	}

	if !hasAny {
		return "娌℃湁宸叉敞鍐岀殑 MCP Server"
	}
	return b.String()
}

func (h *IMMessageHandler) toolCallMCPTool(args map[string]interface{}) string {
	serverID, _ := args["server_id"].(string)
	toolName, _ := args["tool_name"].(string)
	if serverID == "" || toolName == "" {
		return "缂哄皯 server_id 鎴?tool_name 鍙傛暟"
	}
	toolArgs, _ := args["arguments"].(map[string]interface{})

	// Try local MCP manager first (stdio-based servers)
	if mgr := h.app.localMCPManager; mgr != nil && mgr.IsRunning(serverID) {
		result, err := mgr.CallTool(serverID, toolName, toolArgs)
		if err != nil {
			return fmt.Sprintf("鏈湴 MCP 璋冪敤澶辫触: %s", err.Error())
		}
		return result
	}

	// Fall back to remote MCP registry (HTTP-based servers)
	registry := h.app.mcpRegistry
	if registry == nil {
		return "MCP Registry 鏈垵濮嬪寲"
	}
	result, err := registry.CallTool(serverID, toolName, toolArgs)
	if err != nil {
		return fmt.Sprintf("MCP 璋冪敤澶辫触: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolListSkills() string {
	exec := h.app.skillExecutor
	if exec == nil {
		return "Skill Executor 鏈垵濮嬪寲"
	}
	skills := exec.List()

	var b strings.Builder

	// Show local skills
	if len(skills) > 0 {
		b.WriteString("=== 鏈湴宸叉敞鍐?Skill ===\n")
		for _, s := range skills {
			line := fmt.Sprintf("- %s [%s]: %s", s.Name, s.Status, s.Description)
			if s.Source == "hub" {
				line += fmt.Sprintf(" (鏉ユ簮: Hub, trust: %s)", s.TrustLevel)
			} else if s.Source == "file" {
				line += " (鏉ユ簮: 鏈湴鏂囦欢)"
			}
			if s.UsageCount > 0 {
				line += fmt.Sprintf(" (鐢ㄨ繃%d娆? 鎴愬姛鐜?.0f%%)", s.UsageCount, s.SuccessRate*100)
			}
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString("鏈湴娌℃湁宸叉敞鍐岀殑 Skill銆俓n")
	}

	// If local skills are empty or few, also show Hub recommendations
	if len(skills) < 3 && h.app.skillHubClient != nil {
		recs := h.app.skillHubClient.GetRecommendations()
		if len(recs) > 0 {
			b.WriteString("\n=== SkillHub 鎺ㄨ崘 Skill锛堝彲鐢?install_skill_hub 瀹夎锛?==\n")
			for _, r := range recs {
				b.WriteString(fmt.Sprintf("- [%s] %s: %s (trust: %s, downloads: %d, hub: %s)\n",
					r.ID, r.Name, r.Description, r.TrustLevel, r.Downloads, r.HubURL))
			}
		} else {
			b.WriteString("\n鎻愮ず锛氬彲浠ヤ娇鐢?search_skill_hub 宸ュ叿鍦?SkillHub 涓婃悳绱㈡洿澶?Skill銆俓n")
		}
	}

	return b.String()
}

func (h *IMMessageHandler) toolSearchSkillHub(args map[string]interface{}) string {
	query, _ := args["query"].(string)
	if query == "" {
		return "缂哄皯 query 鍙傛暟"
	}

	if h.app.skillHubClient == nil {
		h.app.ensureRemoteInfra()
	}
	if h.app.skillHubClient == nil {
		return "SkillHub 瀹㈡埛绔湭鍒濆鍖栵紝璇锋鏌ラ厤缃腑鐨?skill_hub_urls"
	}

	results, err := h.app.skillHubClient.Search(context.Background(), query)
	if err != nil {
		return fmt.Sprintf("鎼滅储澶辫触: %s", err.Error())
	}
	if len(results) == 0 {
		return fmt.Sprintf("鍦?SkillHub 涓婃湭鎵惧埌涓?%q 鐩稿叧鐨?Skill", query)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("鎵惧埌 %d 涓?Skill锛歕n", len(results)))
	for _, r := range results {
		tags := ""
		if len(r.Tags) > 0 {
			tags = " [" + strings.Join(r.Tags, ", ") + "]"
		}
		b.WriteString(fmt.Sprintf("- ID: %s | %s: %s%s (trust: %s, downloads: %d, hub: %s)\n",
			r.ID, r.Name, r.Description, tags, r.TrustLevel, r.Downloads, r.HubURL))
	}
	b.WriteString("\n浣跨敤 install_skill_hub 宸ュ叿瀹夎锛岄渶鎻愪緵 skill_id 鍜?hub_url 鍙傛暟銆?)
	return b.String()
}

func (h *IMMessageHandler) toolInstallSkillHub(args map[string]interface{}) string {
	skillID, _ := args["skill_id"].(string)
	hubURL, _ := args["hub_url"].(string)
	if skillID == "" {
		return "缂哄皯 skill_id 鍙傛暟"
	}
	if hubURL == "" {
		return "缂哄皯 hub_url 鍙傛暟"
	}

	if h.app.skillHubClient == nil {
		h.app.ensureRemoteInfra()
	}
	if h.app.skillHubClient == nil {
		return "SkillHub 瀹㈡埛绔湭鍒濆鍖?
	}
	if h.app.skillExecutor == nil {
		return "Skill Executor 鏈垵濮嬪寲"
	}

	// Download from Hub
	entry, err := h.app.skillHubClient.Install(context.Background(), skillID, hubURL)
	if err != nil {
		return fmt.Sprintf("瀹夎澶辫触: %s", err.Error())
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
			return fmt.Sprintf("鈿狅笍 Skill %q 鍖呭惈楂橀闄╂搷浣滐紝宸叉嫆缁濊嚜鍔ㄥ畨瑁呫€傞闄╁洜绱? %s",
				entry.Name, strings.Join(assessment.Factors, ", "))
		}
	}

	// Register locally
	if err := h.app.skillExecutor.Register(*entry); err != nil {
		return fmt.Sprintf("娉ㄥ唽澶辫触: %s", err.Error())
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
	b.WriteString(fmt.Sprintf("鉁?宸叉垚鍔熷畨瑁?Skill銆?s銆峔n鎻忚堪: %s\n鏉ユ簮: %s\n淇′换绛夌骇: %s\n",
		entry.Name, entry.Description, hubURL, entry.TrustLevel))

	if autoRun {
		b.WriteString(fmt.Sprintf("\n姝ｅ湪绔嬪嵆鎵ц Skill銆?s銆?..\n", entry.Name))
		result, err := h.app.skillExecutor.Execute(entry.Name)
		if err != nil {
			b.WriteString(fmt.Sprintf("鎵ц澶辫触: %s\n%s", err.Error(), result))
		} else {
			b.WriteString(fmt.Sprintf("鎵ц缁撴灉:\n%s", result))
		}
	} else {
		b.WriteString(fmt.Sprintf("\n鍙互浣跨敤 run_skill 宸ュ叿鎵ц锛屽悕绉颁负: %s", entry.Name))
	}

	return b.String()
}

func (h *IMMessageHandler) toolRunSkill(args map[string]interface{}) string {
	exec := h.app.skillExecutor
	if exec == nil {
		return "Skill Executor 鏈垵濮嬪寲"
	}
	name, _ := args["name"].(string)
	if name == "" {
		return "缂哄皯 name 鍙傛暟"
	}
	result, err := exec.Execute(name)
	if err != nil {
		return fmt.Sprintf("Skill 鎵ц澶辫触: %s\n%s", err.Error(), result)
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
		return "Orchestrator 鏈垵濮嬪寲"
	}
	tasksRaw, ok := args["tasks"].([]interface{})
	if !ok || len(tasksRaw) == 0 {
		return "缂哄皯 tasks 鍙傛暟"
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
		return "娌℃湁鏈夋晥鐨勪换鍔?
	}
	result, err := orch.ExecuteParallel(tasks)
	if err != nil {
		return fmt.Sprintf("骞惰鎵ц澶辫触: %s", err.Error())
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("浠诲姟 %s: %s\n", result.TaskID, result.Summary))
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
		return "ToolSelector 鏈垵濮嬪寲"
	}
	desc, _ := args["task_description"].(string)
	if desc == "" {
		return "缂哄皯 task_description 鍙傛暟"
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
	return fmt.Sprintf("鎺ㄨ崘宸ュ叿: %s\n鐞嗙敱: %s", name, reason)
}

// ---------------------------------------------------------------------------
// 鏈満鐩存帴鎿嶄綔宸ュ叿 (bash, read_file, write_file, list_directory)
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
		return "缂哄皯 command 鍙傛暟"
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
		return fmt.Sprintf("[閿欒] 鍛戒护鍚姩澶辫触: %v", err)
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
					displayCmd = displayCmd[:60] + "鈥?
				}
				if onProgress != nil {
					onProgress(fmt.Sprintf("鈴?鍛戒护浠嶅湪鎵ц涓紙宸?%ds锛? %s", elapsed, displayCmd))
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
			out = out[:8192] + "\n... (杈撳嚭宸叉埅鏂?"
		}
		b.WriteString(out)
	}
	if stderr.Len() > 0 {
		errOut := stderr.String()
		if len(errOut) > 4096 {
			errOut = errOut[:4096] + "\n... (閿欒杈撳嚭宸叉埅鏂?"
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr] ")
		b.WriteString(errOut)
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			b.WriteString(fmt.Sprintf("\n[閿欒] 鍛戒护瓒呮椂锛?d 绉掞級", timeout))
		} else {
			b.WriteString(fmt.Sprintf("\n[閿欒] 閫€鍑虹爜: %v", err))
		}
	}

	if b.Len() == 0 {
		return "(鍛戒护鎵ц瀹屾垚锛屾棤杈撳嚭)"
	}
	return b.String()
}

func (h *IMMessageHandler) toolReadFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	if p == "" {
		return "缂哄皯 path 鍙傛暟"
	}
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("鏂囦欢涓嶅瓨鍦ㄦ垨鏃犳硶璁块棶: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 鏄洰褰曪紝璇蜂娇鐢?list_directory 宸ュ叿", absPath)
	}

	maxLines := readFileMaxLines
	if n, ok := args["lines"].(float64); ok && n > 0 {
		maxLines = int(n)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("璇诲彇澶辫触: %s", err.Error())
	}

	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		return strings.Join(lines, "") + fmt.Sprintf("\n... (宸叉埅鏂紝鍏?%d 琛岋紝鏄剧ず鍓?%d 琛?", len(strings.SplitAfter(string(data), "\n")), maxLines)
	}
	return string(data)
}

func (h *IMMessageHandler) toolWriteFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	content, _ := args["content"].(string)
	if p == "" || content == "" {
		return "缂哄皯 path 鎴?content 鍙傛暟"
	}
	if len(content) > writeFileMaxSize {
		return fmt.Sprintf("鍐呭杩囧ぇ锛?d 瀛楄妭锛夛紝鏈€澶у厑璁?%d 瀛楄妭", len(content), writeFileMaxSize)
	}

	absPath := resolvePath(p)

	// 鑷姩鍒涘缓鐖剁洰褰?
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Sprintf("鍒涘缓鐩綍澶辫触: %s", err.Error())
	}

	if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
		return fmt.Sprintf("鍐欏叆澶辫触: %s", err.Error())
	}

	// 楠岃瘉鍐欏叆
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("鍐欏叆鍚庨獙璇佸け璐? %s", err.Error())
	}
	return fmt.Sprintf("宸插啓鍏?%s锛?d 瀛楄妭锛?, absPath, info.Size())
}

func (h *IMMessageHandler) toolListDirectory(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("璺緞涓嶅瓨鍦ㄦ垨鏃犳硶璁块棶: %s", err.Error())
	}
	if !info.IsDir() {
		return fmt.Sprintf("%s 涓嶆槸鐩綍", absPath)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return fmt.Sprintf("璇诲彇鐩綍澶辫触: %s", err.Error())
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("鐩綍: %s锛堝叡 %d 椤癸級\n", absPath, len(entries)))
	shown := 0
	for _, entry := range entries {
		if shown >= 100 {
			b.WriteString(fmt.Sprintf("... 杩樻湁 %d 椤规湭鏄剧ず\n", len(entries)-shown))
			break
		}
		info, _ := entry.Info()
		if entry.IsDir() {
			b.WriteString(fmt.Sprintf("  馃搧 %s/\n", entry.Name()))
		} else if info != nil {
			b.WriteString(fmt.Sprintf("  馃搫 %s (%d bytes)\n", entry.Name(), info.Size()))
		} else {
			b.WriteString(fmt.Sprintf("  馃搫 %s\n", entry.Name()))
		}
		shown++
	}
	return b.String()
}

const sendFileMaxSize = 200 << 20 // 200 MB 鈥?large files are handled by plugin-level fallback (temp URL)

func (h *IMMessageHandler) toolSendFile(args map[string]interface{}) string {
	p, _ := args["path"].(string)
	if p == "" {
		return "缂哄皯 path 鍙傛暟"
	}
	absPath := resolvePath(p)

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("鏂囦欢涓嶅瓨鍦ㄦ垨鏃犳硶璁块棶: %s", err.Error())
	}
	if info.IsDir() {
		return fmt.Sprintf("%s 鏄洰褰曪紝涓嶈兘浣滀负鏂囦欢鍙戦€?, absPath)
	}
	if info.Size() > sendFileMaxSize {
		return fmt.Sprintf("鏂囦欢杩囧ぇ锛?d 瀛楄妭锛夛紝鏈€澶у厑璁?%d 瀛楄妭", info.Size(), sendFileMaxSize)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("璇诲彇鏂囦欢澶辫触: %s", err.Error())
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

	// Check if the caller wants to forward the file to IM channels.
	forwardIM, _ := args["forward_to_im"].(bool)
	if forwardIM {
		// Use | as delimiter; append |im flag so the interceptor knows to forward.
		return fmt.Sprintf("[file_base64|%s|%s|im]%s", fileName, mimeType, b64)
	}
	// Use | as delimiter to avoid conflicts with : in filenames or MIME types.
	return fmt.Sprintf("[file_base64|%s|%s]%s", fileName, mimeType, b64)
}

func (h *IMMessageHandler) toolOpen(args map[string]interface{}) string {
	target, _ := args["target"].(string)
	if target == "" {
		return "缂哄皯 target 鍙傛暟"
	}

	// Detect URLs (http, https, file, mailto, etc.)
	isURL := strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
	if !isURL {
		target = resolvePath(target)
		// Verify the path exists before attempting to open.
		if _, err := os.Stat(target); err != nil {
			return fmt.Sprintf("璺緞涓嶅瓨鍦ㄦ垨鏃犳硶璁块棶: %s", err.Error())
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Use rundll32 url.dll,FileProtocolHandler 鈥?opens files/URLs with
		// the default handler without spawning a visible console window.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("鎵撳紑澶辫触: %s", err.Error())
	}

	// Don't wait for the process 鈥?it's a GUI application.
	go cmd.Wait()

	if isURL {
		return fmt.Sprintf("宸茬敤榛樿娴忚鍣ㄦ墦寮€: %s", target)
	}
	return fmt.Sprintf("宸茬敤榛樿绋嬪簭鎵撳紑: %s", target)
}

// ---------------------------------------------------------------------------
// Memory Tools
// ---------------------------------------------------------------------------

// toolMemory merges save/list/delete/recall memory operations into a single tool.
func (h *IMMessageHandler) toolMemory(args map[string]interface{}) string {
	if h.memoryStore == nil {
		return "闀挎湡璁板繂鏈垵濮嬪寲"
	}

	action := stringVal(args, "action")
	switch action {
	case "recall":
		query := stringVal(args, "query")
		if query == "" {
			return "缂哄皯 query 鍙傛暟"
		}
		category := MemoryCategory(stringVal(args, "category"))
		// Resolve current project path for affinity boosting.
		var projectPath string
		if h.contextResolver != nil {
			projectPath, _ = h.contextResolver.ResolveProject()
		}
		entries := h.memoryStore.RecallDynamic(query, category, projectPath)
		if len(entries) == 0 {
			return "娌℃湁鎵惧埌鐩稿叧璁板繂銆?
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("鍙洖 %d 鏉＄浉鍏宠蹇?\n", len(entries)))
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
			return "缂哄皯 content 鍙傛暟"
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
			return fmt.Sprintf("淇濆瓨璁板繂澶辫触: %s", err.Error())
		}
		summary := content
		if len(summary) > 50 {
			summary = summary[:50] + "..."
		}
		return fmt.Sprintf("宸蹭繚瀛樿蹇? %s", summary)

	case "list":
		category := MemoryCategory(stringVal(args, "category"))
		keyword := stringVal(args, "keyword")
		entries := h.memoryStore.List(category, keyword)
		if len(entries) == 0 {
			return "娌℃湁鎵惧埌鍖归厤鐨勮蹇嗘潯鐩€?
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("鎵惧埌 %d 鏉¤蹇?\n", len(entries)))
		for _, e := range entries {
			b.WriteString(fmt.Sprintf("- [%s] (%s) %s", e.ID, e.Category, e.Content))
			if len(e.Tags) > 0 {
				b.WriteString(fmt.Sprintf(" 鏍囩=%v", e.Tags))
			}
			b.WriteString("\n")
		}
		return b.String()

	case "delete":
		id := stringVal(args, "id")
		if id == "" {
			return "缂哄皯 id 鍙傛暟"
		}
		if err := h.memoryStore.Delete(id); err != nil {
			return fmt.Sprintf("鍒犻櫎璁板繂澶辫触: %s", err.Error())
		}
		return fmt.Sprintf("宸插垹闄よ蹇? %s", id)

	default:
		return "action 鍙傛暟鏃犳晥锛屽彲閫夊€? recall, save, list, delete"
	}
}

// ---------------------------------------------------------------------------
// Template Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateTemplate(args map[string]interface{}) string {
	if h.templateManager == nil {
		return "妯℃澘绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	name := stringVal(args, "name")
	tool := stringVal(args, "tool")
	if name == "" || tool == "" {
		return "缂哄皯 name 鎴?tool 鍙傛暟"
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
		return fmt.Sprintf("鍒涘缓妯℃澘澶辫触: %s", err.Error())
	}
	return fmt.Sprintf("妯℃澘宸插垱寤? %s锛堝伐鍏?%s, 椤圭洰=%s锛?, name, tool, tpl.ProjectPath)
}

func (h *IMMessageHandler) toolListTemplates() string {
	if h.templateManager == nil {
		return "妯℃澘绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	templates := h.templateManager.List()
	if len(templates) == 0 {
		return "褰撳墠娌℃湁浼氳瘽妯℃澘銆?
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("鍏?%d 涓ā鏉?\n", len(templates)))
	for _, t := range templates {
		b.WriteString(fmt.Sprintf("- %s: 宸ュ叿=%s 椤圭洰=%s", t.Name, t.Tool, t.ProjectPath))
		if t.ModelConfig != "" {
			b.WriteString(fmt.Sprintf(" 妯″瀷=%s", t.ModelConfig))
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
		return "妯℃澘绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	name := stringVal(args, "template_name")
	if name == "" {
		return "缂哄皯 template_name 鍙傛暟"
	}

	tpl, err := h.templateManager.Get(name)
	if err != nil {
		return fmt.Sprintf("鑾峰彇妯℃澘澶辫触: %s", err.Error())
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
		return "閰嶇疆绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	section := stringVal(args, "section")
	result, err := h.configManager.GetConfig(section, true)
	if err != nil {
		return fmt.Sprintf("璇诲彇閰嶇疆澶辫触: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "閰嶇疆绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	section := stringVal(args, "section")
	key := stringVal(args, "key")
	value := stringVal(args, "value")
	if section == "" || key == "" {
		return "缂哄皯 section 鎴?key 鍙傛暟"
	}

	oldValue, err := h.configManager.UpdateConfig(section, key, value)
	if err != nil {
		return fmt.Sprintf("淇敼閰嶇疆澶辫触: %s", err.Error())
	}
	return fmt.Sprintf("閰嶇疆宸叉洿鏂? %s.%s\n鏃у€? %s\n鏂板€? %s", section, key, oldValue, value)
}

func (h *IMMessageHandler) toolBatchUpdateConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "閰嶇疆绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	changesStr := stringVal(args, "changes")
	if changesStr == "" {
		return "缂哄皯 changes 鍙傛暟"
	}

	var changes []ConfigChange
	if err := json.Unmarshal([]byte(changesStr), &changes); err != nil {
		return fmt.Sprintf("瑙ｆ瀽 changes 鍙傛暟澶辫触: %s", err.Error())
	}
	if len(changes) == 0 {
		return "changes 鍒楄〃涓虹┖"
	}

	if err := h.configManager.BatchUpdate(changes); err != nil {
		return fmt.Sprintf("鎵归噺鏇存柊閰嶇疆澶辫触: %s", err.Error())
	}
	return fmt.Sprintf("鎵归噺鏇存柊鎴愬姛锛屽叡搴旂敤 %d 椤瑰彉鏇?, len(changes))
}

func (h *IMMessageHandler) toolListConfigSchema() string {
	if h.configManager == nil {
		return "閰嶇疆绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	result, err := h.configManager.SchemaJSON()
	if err != nil {
		return fmt.Sprintf("鑾峰彇閰嶇疆 Schema 澶辫触: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolExportConfig() string {
	if h.configManager == nil {
		return "閰嶇疆绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	result, err := h.configManager.ExportConfig()
	if err != nil {
		return fmt.Sprintf("瀵煎嚭閰嶇疆澶辫触: %s", err.Error())
	}
	return result
}

func (h *IMMessageHandler) toolImportConfig(args map[string]interface{}) string {
	if h.configManager == nil {
		return "閰嶇疆绠＄悊鍣ㄦ湭鍒濆鍖?
	}

	jsonData := stringVal(args, "json_data")
	if jsonData == "" {
		return "缂哄皯 json_data 鍙傛暟"
	}

	report, err := h.configManager.ImportConfig(jsonData)
	if err != nil {
		return fmt.Sprintf("瀵煎叆閰嶇疆澶辫触: %s", err.Error())
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("閰嶇疆瀵煎叆瀹屾垚: 搴旂敤 %d 椤? 璺宠繃 %d 椤?, report.Applied, report.Skipped))
	if len(report.Warnings) > 0 {
		b.WriteString("\n璀﹀憡:")
		for _, w := range report.Warnings {
			b.WriteString(fmt.Sprintf("\n  - %s", w))
		}
	}
	return b.String()
}

// toolSetMaxIterations allows the agent to dynamically adjust the max
// iterations for the current conversation loop. This does NOT change the
// persisted config 鈥?it only affects the in-flight loop.
func (h *IMMessageHandler) toolSetMaxIterations(args map[string]interface{}) string {
	n, ok := args["max_iterations"].(float64)
	if !ok || n < 1 {
		return fmt.Sprintf("缂哄皯鎴栨棤鏁堢殑 max_iterations 鍙傛暟锛堥渶瑕?%d-%d 鐨勬暣鏁帮級", minAgentIterations, maxAgentIterationsCap)
	}
	limit := int(n)
	if limit < minAgentIterations {
		limit = minAgentIterations
	}
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
		return fmt.Sprintf("鉁?宸插皢鏈€澶ц疆鏁拌皟鏁翠负 %d锛堝凡鎸佷箙鍖栵紝鍘熷洜: %s锛?, limit, reason)
	}
	return fmt.Sprintf("鉁?宸插皢鏈€澶ц疆鏁拌皟鏁翠负 %d锛堝凡鎸佷箙鍖栵級", limit)
}

// ---------------------------------------------------------------------------
// Nickname (set_nickname) tool
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolSetNickname(args map[string]interface{}) string {
	nickname := strings.TrimSpace(stringVal(args, "nickname"))
	if nickname == "" {
		return "鉂?nickname 涓嶈兘涓虹┖"
	}
	// Persist to local config.
	cfg, err := h.app.LoadConfig()
	if err == nil {
		cfg.RemoteNickname = nickname
		_ = h.app.SaveConfig(cfg)
	}
	// Send to Hub via WebSocket.
	if hc := h.app.hubClient(); hc != nil {
		if err := hc.SendNicknameUpdate(nickname); err != nil {
			log.Printf("[set_nickname] SendNicknameUpdate error: %v", err)
			return fmt.Sprintf("鈿狅笍 鏄电О宸蹭繚瀛樺埌鏈湴锛?s锛夛紝浣嗕笂鎶?Hub 澶辫触锛?v", nickname, err)
		}
	}
	return fmt.Sprintf("鉁?鏄电О宸叉洿鏂颁负銆?s銆嶏紝Hub 宸插悓姝ャ€?, nickname)
}

// ---------------------------------------------------------------------------
// LLM provider switch tool
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolSwitchLLMProvider(args map[string]interface{}) string {
	providerName := stringVal(args, "provider")
	if providerName == "" {
		// No provider specified 鈥?list available providers and current selection.
		info := h.app.GetMaclawLLMProviders()
		var b strings.Builder
		b.WriteString(fmt.Sprintf("褰撳墠 LLM 鏈嶅姟鍟? %s\n鍙敤鏈嶅姟鍟?\n", info.Current))
		for _, p := range info.Providers {
			if p.URL == "" && p.Key == "" && p.Model == "" {
				continue // skip unconfigured custom slots
			}
			marker := ""
			if p.Name == info.Current {
				marker = " [褰撳墠]"
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
		return fmt.Sprintf("鏈壘鍒版湇鍔″晢 %q锛屽彲鐢? %s", providerName, strings.Join(names, ", "))
	}

	if match.Name == info.Current {
		return fmt.Sprintf("褰撳墠宸茬粡鏄?%s锛屾棤闇€鍒囨崲", match.Name)
	}

	if err := h.app.SaveMaclawLLMProviders(info.Providers, match.Name); err != nil {
		return fmt.Sprintf("鍒囨崲澶辫触: %s", err.Error())
	}
	return fmt.Sprintf("鉁?宸插皢 LLM 鏈嶅姟鍟嗗垏鎹负 %s (model=%s)", match.Name, match.Model)
}

// ---------------------------------------------------------------------------
// Scheduled task tool implementations
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolCreateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "瀹氭椂浠诲姟绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	name := stringVal(args, "name")
	action := stringVal(args, "action")
	if name == "" || action == "" {
		return "缂哄皯 name 鎴?action 鍙傛暟"
	}
	hour := -1
	if v, ok := args["hour"].(float64); ok {
		hour = int(v)
	}
	if hour < 0 || hour > 23 {
		return "hour 蹇呴』鍦?0-23 涔嬮棿"
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
		return fmt.Sprintf("鍒涘缓瀹氭椂浠诲姟澶辫触: %s", err.Error())
	}

	// Notify frontend to refresh the scheduled tasks panel.
	h.app.emitEvent("scheduled-tasks-changed")

	// 闈炰竴娆℃€т换鍔″悓姝ュ埌绯荤粺鏃ュ巻
	if created := h.scheduledTaskManager.Get(id); created != nil && isRecurringTask(created) {
		go func() {
			if err := SyncTaskToSystemCalendar(created); err != nil {
				h.app.log(fmt.Sprintf("[scheduled-task] calendar sync failed: %v", err))
			}
		}()
	}

	// Format next run time for display.
	if task := h.scheduledTaskManager.Get(id); task != nil && task.NextRunAt != nil {
		return fmt.Sprintf("鉁?瀹氭椂浠诲姟宸插垱寤篭nID: %s\n鍚嶇О: %s\n鎿嶄綔: %s\n涓嬫鎵ц: %s", id, name, action, task.NextRunAt.Format("2006-01-02 15:04"))
	}
	return fmt.Sprintf("鉁?瀹氭椂浠诲姟宸插垱寤猴紙ID: %s锛?, id)
}

func (h *IMMessageHandler) toolListScheduledTasks() string {
	if h.scheduledTaskManager == nil {
		return "瀹氭椂浠诲姟绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	tasks := h.scheduledTaskManager.List()
	if len(tasks) == 0 {
		return "褰撳墠娌℃湁瀹氭椂浠诲姟銆?
	}

	weekdays := []string{"鍛ㄦ棩", "鍛ㄤ竴", "鍛ㄤ簩", "鍛ㄤ笁", "鍛ㄥ洓", "鍛ㄤ簲", "鍛ㄥ叚"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("鍏?%d 涓畾鏃朵换鍔★細\n\n", len(tasks)))
	for _, t := range tasks {
		b.WriteString(fmt.Sprintf("馃搵 [%s] %s\n", t.ID, t.Name))
		b.WriteString(fmt.Sprintf("   鎿嶄綔: %s\n", t.Action))

		// Schedule description
		sched := fmt.Sprintf("姣忓ぉ %02d:%02d", t.Hour, t.Minute)
		if t.DayOfWeek >= 0 && t.DayOfWeek <= 6 {
			sched = fmt.Sprintf("姣?s %02d:%02d", weekdays[t.DayOfWeek], t.Hour, t.Minute)
		}
		if t.DayOfMonth > 0 {
			sched = fmt.Sprintf("姣忔湀%d鍙?%02d:%02d", t.DayOfMonth, t.Hour, t.Minute)
		}
		if t.StartDate != "" || t.EndDate != "" {
			sched += fmt.Sprintf("锛?s ~ %s锛?, t.StartDate, t.EndDate)
		}
		b.WriteString(fmt.Sprintf("   鏃堕棿: %s\n", sched))
		b.WriteString(fmt.Sprintf("   鐘舵€? %s", t.Status))
		if t.NextRunAt != nil {
			b.WriteString(fmt.Sprintf(" | 涓嬫鎵ц: %s", t.NextRunAt.Format("2006-01-02 15:04")))
		}
		if t.RunCount > 0 {
			b.WriteString(fmt.Sprintf(" | 宸叉墽琛?%d 娆?, t.RunCount))
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func (h *IMMessageHandler) toolDeleteScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "瀹氭椂浠诲姟绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	id := stringVal(args, "id")
	name := stringVal(args, "name")
	if id == "" && name == "" {
		return "璇锋彁渚?id 鎴?name 鍙傛暟"
	}
	var err error
	if id != "" {
		err = h.scheduledTaskManager.Delete(id)
	} else {
		err = h.scheduledTaskManager.DeleteByName(name)
	}
	if err != nil {
		return fmt.Sprintf("鍒犻櫎澶辫触: %s", err.Error())
	}
	h.app.emitEvent("scheduled-tasks-changed")
	return "鉁?瀹氭椂浠诲姟宸插垹闄?
}

func (h *IMMessageHandler) toolUpdateScheduledTask(args map[string]interface{}) string {
	if h.scheduledTaskManager == nil {
		return "瀹氭椂浠诲姟绠＄悊鍣ㄦ湭鍒濆鍖?
	}
	id := stringVal(args, "id")
	if id == "" {
		return "缂哄皯 id 鍙傛暟"
	}
	err := h.scheduledTaskManager.Update(id, args)
	if err != nil {
		return fmt.Sprintf("鏇存柊澶辫触: %s", err.Error())
	}
	h.app.emitEvent("scheduled-tasks-changed")
	// Show updated task info.
	if t := h.scheduledTaskManager.Get(id); t != nil {
		next := "-"
		if t.NextRunAt != nil {
			next = t.NextRunAt.Format("2006-01-02 15:04")
		}
		return fmt.Sprintf("鉁?瀹氭椂浠诲姟宸叉洿鏂癨nID: %s\n鍚嶇О: %s\n鎿嶄綔: %s\n鏃堕棿: %02d:%02d\n涓嬫鎵ц: %s", t.ID, t.Name, t.Action, t.Hour, t.Minute, next)
	}
	return "鉁?瀹氭椂浠诲姟宸叉洿鏂?
}

// ---------- ClawNet Knowledge Tools ----------

func (h *IMMessageHandler) toolClawNetSearch(args map[string]interface{}) string {
	if h.app.clawNetClient == nil || !h.app.clawNetClient.IsRunning() {
		return "铏剧綉鏈繛鎺ワ紝璇峰厛鍦ㄨ缃腑鍚敤 ClawNet"
	}
	query := stringVal(args, "query")
	if query == "" {
		return "缂哄皯 query 鍙傛暟"
	}
	entries, err := h.app.clawNetClient.SearchKnowledge(query)
	if err != nil {
		return fmt.Sprintf("鎼滅储澶辫触: %s", err.Error())
	}
	if len(entries) == 0 {
		return fmt.Sprintf("鏈壘鍒颁笌銆?s銆嶇浉鍏崇殑鐭ヨ瘑鏉＄洰", query)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("馃攳 铏剧綉鐭ヨ瘑鎼滅储銆?s銆嶁€?鎵惧埌 %d 鏉?\n\n", query, len(entries)))
	for i, e := range entries {
		if i >= 10 {
			b.WriteString(fmt.Sprintf("... 杩樻湁 %d 鏉＄粨鏋淺n", len(entries)-10))
			break
		}
		b.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, e.Title))
		if e.Body != "" {
			body := e.Body
			if len(body) > 200 {
				body = body[:200] + "鈥?
			}
			b.WriteString(fmt.Sprintf("   %s\n", body))
		}
		if e.Author != "" {
			b.WriteString(fmt.Sprintf("   鈥?%s", e.Author))
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
		return "铏剧綉鏈繛鎺ワ紝璇峰厛鍦ㄨ缃腑鍚敤 ClawNet"
	}
	title := stringVal(args, "title")
	body := stringVal(args, "body")
	if title == "" {
		return "缂哄皯 title 鍙傛暟"
	}
	if body == "" {
		return "缂哄皯 body 鍙傛暟"
	}
	entry, err := h.app.clawNetClient.PublishKnowledge(title, body)
	if err != nil {
		return fmt.Sprintf("鍙戝竷澶辫触: %s", err.Error())
	}
	return fmt.Sprintf("鉁?鐭ヨ瘑宸插彂甯冨埌铏剧綉\nID: %s\n鏍囬: %s", entry.ID, entry.Title)
}

func (h *IMMessageHandler) toolQueryAuditLog(args map[string]interface{}) string {
	if h.app == nil || h.app.auditLog == nil {
		return "瀹¤鏃ュ織鏈垵濮嬪寲"
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
		return fmt.Sprintf("鏌ヨ澶辫触: %s", err.Error())
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
		return "娌℃湁鎵惧埌鍖归厤鐨勫璁¤褰?
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("鎵惧埌 %d 鏉″璁¤褰?\n\n", len(entries)))
	for i, e := range entries {
		b.WriteString(fmt.Sprintf("%d. [%s] %s | 椋庨櫓: %s | 鍐崇瓥: %s | 缁撴灉: %s\n",
			i+1, e.Timestamp.Format("01-02 15:04"), e.ToolName, e.RiskLevel, e.PolicyAction, e.Result))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Web Search & Fetch Tools
// ---------------------------------------------------------------------------

func (h *IMMessageHandler) toolWebSearch(args map[string]interface{}) string {
	query := stringVal(args, "query")
	if query == "" {
		return "缂哄皯 query 鍙傛暟"
	}
	maxResults := 8
	if n, ok := args["max_results"].(float64); ok && n > 0 {
		maxResults = int(n)
	}

	results, err := websearch.Search(query, maxResults)
	if err != nil {
		return fmt.Sprintf("鎼滅储澶辫触: %s", err.Error())
	}
	if len(results) == 0 {
		return "鏈壘鍒扮浉鍏崇粨鏋?
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("鎼滅储 \"%s\" 鎵惧埌 %d 鏉＄粨鏋?\n\n", query, len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n", i+1, r.Title, r.URL))
		if r.Snippet != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Snippet))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (h *IMMessageHandler) toolWebFetch(args map[string]interface{}) string {
	rawURL := stringVal(args, "url")
	if rawURL == "" {
		return "缂哄皯 url 鍙傛暟"
	}

	opts := &websearch.FetchOptions{}
	if renderJS, ok := args["render_js"].(bool); ok {
		opts.RenderJS = renderJS
	}
	if savePath := stringVal(args, "save_path"); savePath != "" {
		opts.SavePath = resolvePath(savePath)
		opts.MaxBytes = 10 * 1024 * 1024 // 10MB for file downloads
	} else {
		// For text content, allow up to 2MB raw, we'll truncate the output
		opts.MaxBytes = 2 * 1024 * 1024
	}
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		opts.TimeoutS = int(t)
	}

	result, err := websearch.Fetch(rawURL, opts)
	if err != nil {
		return fmt.Sprintf("鎶撳彇澶辫触: %s", err.Error())
	}

	// If saved to file, return short message
	if result.SavedTo != "" {
		return result.Content
	}

	var sb strings.Builder
	if result.Title != "" {
		sb.WriteString(fmt.Sprintf("鏍囬: %s\n", result.Title))
	}
	sb.WriteString(fmt.Sprintf("URL: %s\n", result.URL))
	sb.WriteString(fmt.Sprintf("绫诲瀷: %s | 澶у皬: %d 瀛楄妭\n\n", result.ContentType, result.BytesRead))

	content := result.Content
	// web_fetch allows longer content: up to 16KB for text return
	const webFetchMaxContent = 16384
	if len(content) > webFetchMaxContent {
		headLen := webFetchMaxContent * 2 / 3
		tailLen := webFetchMaxContent - headLen - 60
		content = content[:headLen] + "\n\n... (鍐呭宸叉埅鏂紝鍏?" + fmt.Sprintf("%d", len(content)) + " 瀛楃) ...\n\n" + content[len(content)-tailLen:]
	}
	sb.WriteString(content)
	return sb.String()
}
