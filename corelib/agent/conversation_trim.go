package agent

// Conversation trimming: token estimation, context window management,
// and conversation history compaction utilities.
//
// Migrated from gui/im_conversation_trim.go as part of the agent-unification
// plan. This is the single source of truth — gui/ will import and alias
// these functions.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func EstimateConversationEntryTokens(entries []ConversationEntry) int {
	total := 0
	for _, e := range entries {
		data, _ := json.Marshal(e)
		total += EstimateBytesToTokens(data)
	}
	return total
}

// EstimateConversationTokens estimates the token count for a raw conversation
// slice ([]interface{}) used inside the agent loop.
// For Chinese-heavy content the JSON byte length underestimates token count
// because CJK characters are 3 bytes in UTF-8 but typically 1-2 tokens.
// We use len/3 instead of len/4 to be more conservative.
//
// For multimodal messages (content is []interface{} with image_url blocks),
// base64 image data is excluded from the estimate since it doesn't consume
// text tokens — vision tokens are counted separately by the API.
func EstimateConversationTokens(msgs []interface{}) int {
	total := 0
	for _, m := range msgs {
		mm, ok := m.(map[string]interface{})
		if !ok {
			data, _ := json.Marshal(m)
			total += EstimateBytesToTokens(data)
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
					// Vision image block — count a fixed ~85 tokens (low-detail)
					// instead of serializing the huge base64 string.
					total += 85
					continue
				}
				if blockType == "image" {
					// Anthropic-style image block — same treatment.
					total += 85
					continue
				}
				// Text or other block — estimate normally.
				data, _ := json.Marshal(bm)
				total += EstimateBytesToTokens(data)
			}
			// Also count role and other top-level fields (minus content).
			total += 10 // rough overhead for role, etc.
		} else {
			data, _ := json.Marshal(mm)
			total += EstimateBytesToTokens(data)
		}
	}
	return total
}

// EstimateToolsTokens estimates the token count consumed by tool definitions.
func EstimateToolsTokens(tools []map[string]interface{}) int {
	if len(tools) == 0 {
		return 0
	}
	data, _ := json.Marshal(tools)
	return EstimateBytesToTokens(data)
}

// EstimateTextTokens delegates to corelib.EstimateTextTokens.
// Kept as a package-level alias for backward compatibility with callers
// that import corelib/agent.
func EstimateTextTokens(text string) int {
	return corelib.EstimateTextTokens(text)
}

// EstimateBytesToTokens converts JSON bytes to an approximate token count.
// For JSON data, we use a byte-based heuristic rather than character-based
// because JSON structural overhead ({, ", :, etc.) is all ASCII bytes that
// don't represent actual content tokens. The blended ratio of ~2.5 bytes/token
// accounts for mixed CJK (3 bytes/char, ~1.5 tokens) and ASCII (1 byte/char,
// ~0.25 tokens) content within JSON envelopes.
func EstimateBytesToTokens(data []byte) int {
	return (len(data)*10 + 24) / 25 // equivalent to len/2.5, rounded up
}

// defaultContextTokens is re-exported from corelib for local use.
const defaultContextTokens = corelib.DefaultContextTokens

// MsgRole extracts the "role" field from a conversation message regardless
// of whether it's map[string]string or map[string]interface{}.
func MsgRole(m interface{}) string {
	switch v := m.(type) {
	case map[string]interface{}:
		r, _ := v["role"].(string)
		return r
	case map[string]string:
		return v["role"]
	}
	return ""
}

// MsgHasToolCalls checks if a conversation message has a non-nil tool_calls field.
func MsgHasToolCalls(m interface{}) bool {
	if v, ok := m.(map[string]interface{}); ok {
		return v["tool_calls"] != nil
	}
	return false
}

// ---------------------------------------------------------------------------
// Entry groups: the structural primitive for tool_calls integrity.
//
// An assistant message with tool_calls and its immediately following tool
// messages form an indivisible unit. All operations on conversation history
// (trimming, compaction, boundary selection) MUST operate on groups, never
// on individual entries. This structurally prevents broken tool_calls/tool
// pairs — the same pattern used by trimConversation's msgGroup for the
// []interface{} conversation format.
// ---------------------------------------------------------------------------

// EntryGroup represents a contiguous range [Start, End) of entries that
// must be kept or dropped as a unit. An assistant(tool_calls) + its
// following tool entries = one group. All other entries = single-entry groups.
type EntryGroup struct {
	Start, End int // half-open range in the entries slice
}

// BuildEntryGroups partitions entries into indivisible groups.
// This is the single source of truth for group boundaries — all trimming
// and selection code must use this function.
func BuildEntryGroups(entries []ConversationEntry) []EntryGroup {
	var groups []EntryGroup
	i := 0
	for i < len(entries) {
		start := i
		if entries[i].Role == "assistant" && entryHasToolCalls(entries[i]) {
			// assistant(tool_calls) + all following tool entries = one group
			i++
			for i < len(entries) && entries[i].Role == "tool" {
				i++
			}
		} else {
			i++
		}
		groups = append(groups, EntryGroup{Start: start, End: i})
	}
	return groups
}

// entryHasToolCalls returns true if the entry has a non-nil, non-empty
// ToolCalls field. Handles both typed slices and []interface{} (from JSON
// round-trip). An empty slice is treated as no tool calls.
func entryHasToolCalls(e ConversationEntry) bool {
	if e.ToolCalls == nil {
		return false
	}
	// After JSON round-trip, ToolCalls (interface{}) may hold an empty
	// []interface{}{} which is non-nil but has no actual tool calls.
	switch v := e.ToolCalls.(type) {
	case []interface{}:
		return len(v) > 0
	default:
		// For typed slices ([]llm.ToolCall etc.), marshal to check length.
		data, err := json.Marshal(v)
		if err != nil {
			return false
		}
		return len(data) > 2 // "[]" is 2 bytes = empty
	}
}

// GroupContaining returns the group that contains the entry at idx.
// Uses binary search since groups are sorted by Start.
func GroupContaining(groups []EntryGroup, idx int) *EntryGroup {
	lo, hi := 0, len(groups)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if idx < groups[mid].Start {
			hi = mid
		} else if idx >= groups[mid].End {
			lo = mid + 1
		} else {
			return &groups[mid]
		}
	}
	return nil
}

// TrimConversation keeps the first message (system prompt) and trims older
// middle messages so the total estimated tokens stay under the limit.
// It preserves tool-call integrity: assistant messages with tool_calls and
// their corresponding tool-result messages are always kept or dropped together.
// TrimConversation trims conversation messages to fit within tokenLimit.
// toolsTokens is the estimated token count consumed by tool definitions,
// which must be subtracted from the available budget for messages.
// summarizer is an optional callback that summarizes dropped messages into a
// short text so the LLM retains key context. When nil, dropped messages are
// replaced with a generic placeholder.
func TrimConversation(msgs []interface{}, tokenLimit int, toolsTokens int, summarizer func(string) string) []interface{} {
	if tokenLimit <= 0 {
		tokenLimit = defaultContextTokens * 80 / 100
	}
	// Reserve space for tool definitions.
	msgBudget := tokenLimit - toolsTokens
	if msgBudget < 4000 {
		msgBudget = 4000 // absolute minimum to avoid degenerate cases
	}
	if EstimateConversationTokens(msgs) <= msgBudget {
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
		role := MsgRole(msgs[i])
		if role == "assistant" && MsgHasToolCalls(msgs[i]) {
			// This assistant message + all following tool messages = one group
			i++
			for i < len(msgs) {
				if MsgRole(msgs[i]) != "tool" {
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
		if EstimateConversationTokens(result) <= msgBudget {
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
		if EstimateConversationTokens(result) > msgBudget {
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
	if EstimateConversationTokens(result) <= msgBudget {
		return result
	}

	// Still over budget — aggressively truncate assistant content in the result
	// while keeping tool-call pairs intact.
	result = TruncateAssistantContent(result, msgBudget)
	if EstimateConversationTokens(result) <= msgBudget {
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

// TruncateAssistantContent shrinks assistant message text content in the
// conversation to help fit within the token budget. It never touches
// tool_calls or tool messages to avoid breaking call/result pairing.
func TruncateAssistantContent(msgs []interface{}, budget int) []interface{} {
	result := make([]interface{}, len(msgs))
	copy(result, msgs)
	for i, m := range result {
		if EstimateConversationTokens(result) <= budget {
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

// MakeSummarizer returns a summarizer callback that uses DoSimpleLLMRequest
// to condense dropped conversation history into a short summary.
func MakeSummarizer(cfg corelib.MaclawLLMConfig, httpClient *http.Client) func(string) string {
	return func(text string) string {
		msgs := []interface{}{
			map[string]string{"role": "user", "content": "请简洁总结以下对话历史，保留关键事实、决策和待办事项：\n\n" + text},
		}
		result, err := DoSimpleLLMRequest(cfg, msgs, httpClient, 30*time.Second)
		if err != nil || result.Content == "" {
			return ""
		}
		return result.Content
	}
}

func TrimHistory(entries []ConversationEntry) []ConversationEntry {
	if len(entries) <= MaxConversationTurns {
		// Within turn limit — check token budget.
		if EstimateConversationEntryTokens(entries) <= MaxMemoryTokenEstimate {
			return entries
		}
		// Over token budget but within turn limit — skip turn-based trim,
		// go straight to token-level trimming below.
	}

	// Build indivisible groups so we never split an assistant(tool_calls)
	// from its tool result entries. This is the same pattern used by
	// TrimConversation's msgGroup for the []interface{} format.
	groups := BuildEntryGroups(entries)

	// Turn-based trim: drop oldest groups until the total entry count
	// fits within MaxConversationTurns.
	dropCount := 0
	totalEntries := len(entries)
	for dropCount < len(groups) && totalEntries > MaxConversationTurns {
		totalEntries -= groups[dropCount].End - groups[dropCount].Start
		dropCount++
	}

	// Assemble the kept entries.
	var trimmed []ConversationEntry
	for _, g := range groups[dropCount:] {
		trimmed = append(trimmed, entries[g.Start:g.End]...)
	}

	// Token-level secondary validation: if the turn-trimmed result still
	// exceeds the token budget (e.g. turns with large tool outputs), drop
	// additional oldest groups until we fit.
	keptGroups := groups[dropCount:]
	for len(keptGroups) > 1 && EstimateConversationEntryTokens(trimmed) > MaxMemoryTokenEstimate {
		groupSize := keptGroups[0].End - keptGroups[0].Start
		trimmed = trimmed[groupSize:]
		keptGroups = keptGroups[1:]
	}

	return trimmed
}

// MaxToolResultLen caps individual tool results to ~4KB before they enter
// the conversation. This prevents a single verbose tool output (e.g. bash
// stdout, large file read) from dominating the context window.
const MaxToolResultLen = 4096

// TruncateToolResult caps a tool result string to MaxToolResultLen bytes.
// If truncated, it keeps the first and last portions so the LLM sees both
// the beginning (often headers/status) and the end (often the conclusion).
func TruncateToolResult(s string) string {
	if len(s) <= MaxToolResultLen {
		return s
	}
	headLen := MaxToolResultLen * 2 / 3
	tailLen := MaxToolResultLen - headLen - 40 // 40 bytes for the separator
	return s[:headLen] + "\n\n... (已截断，共 " + fmt.Sprintf("%d", len(s)) + " 字节) ...\n\n" + s[len(s)-tailLen:]
}

// TruncateToolResultForTool applies tool-specific truncation strategies.
// Terminal output (get_session_output, bash) keeps more tail (recent output
// is more relevant). Structured data keeps more head (headers/schema).
// WebFetchMaxToolResult allows web_fetch to return up to 32KB to the LLM,
// since its content is already windowed inside the handler and carries
// continuation metadata that must survive truncation.
const WebFetchMaxToolResult = 32768

func TruncateToolResultForTool(toolName, s string) string {
	// web_fetch gets a higher budget — content is already windowed in handler
	limit := MaxToolResultLen
	if toolName == "web_fetch" {
		limit = WebFetchMaxToolResult
	}
	if strings.HasPrefix(toolName, "browser") {
		limit = max(limit, 4096)
	}
	if len(s) <= limit {
		return s
	}
	if toolName == "web_fetch" {
		return truncateWebFetchToolResult(s, limit)
	}
	sep := "\n\n... (已截断，共 " + fmt.Sprintf("%d", len(s)) + " 字节) ...\n\n"
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

func truncateWebFetchToolResult(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	marker := "\n\n--- 完整性信号 ---\n"
	idx := strings.LastIndex(s, marker)
	if idx < 0 {
		sep := "\n\n... (已截断，共 " + fmt.Sprintf("%d", len(s)) + " 字节) ...\n\n"
		budget := limit - len(sep)
		headLen := budget * 2 / 3
		tailLen := budget - headLen
		return s[:headLen] + sep + s[len(s)-tailLen:]
	}
	meta := s[idx:]
	head := s[:idx]
	sep := "\n\n... (已截断，共 " + fmt.Sprintf("%d", len(s)) + " 字节) ...\n\n"
	if len(meta)+len(sep) >= limit {
		return sep + meta[len(meta)-(limit-len(sep)):]
	}
	headBudget := limit - len(meta) - len(sep)
	if headBudget <= 0 {
		return sep + meta
	}
	if len(head) > headBudget {
		head = head[:headBudget]
	}
	return head + sep + meta
}

// InferFileDeliveryMessage generates a user-facing prompt based on the file name
// when no explicit message was provided. This ensures PDF documents sent via IM
// always include a hint telling the user what the document is and what to do.
func InferFileDeliveryMessage(fileName string) string {
	lower := strings.ToLower(fileName)
	switch {
	case strings.Contains(lower, "requirement") || strings.Contains(lower, "需求"):
		return i18n.T(i18n.MsgFileRequirements, "zh")
	case strings.Contains(lower, "design") || strings.Contains(lower, "设计"):
		return i18n.T(i18n.MsgFileDesign, "zh")
	case strings.Contains(lower, "task") || strings.Contains(lower, "任务"):
		return i18n.T(i18n.MsgFileTaskList, "zh")
	default:
		return i18n.Tf(i18n.MsgFileGeneric, "zh", fileName)
	}
}

// NOTE: thinkTagPattern and StripThinkingTags are defined in llm_helper.go
// within this same package. They are not redeclared here.

// rolePrefixRe matches hallucinated role prefixes that some LLMs produce
// when tool definitions (especially 25+ browser tools) are in context.
// The LLM confuses tool categories with chat roles and emits lines like
// "Browser: 伯伯，API 服务器资源状况如下：" in the middle or at the start
// of an otherwise normal response.
//
// Only "Browser:" and "Tool:" are matched — these are the observed
// hallucination patterns. "Assistant:"/"System:"/"User:" are intentionally
// excluded because they appear too often in legitimate LLM output (e.g.
// explaining chat roles, quoting API docs).
// Allows optional leading whitespace, Markdown block-level markers (>, -, *, digits),
// and an optional space before the colon.
// Also matches fullwidth colon (：U+FF1A) which Chinese LLMs sometimes produce.
var rolePrefixRe = regexp.MustCompile(`(?m)^[\s>*\-]*(?:\d+\.\s*)?(Browser|Tool)\s*(?::[ \t]?|：)`)

// StripRolePrefixHallucination removes hallucinated role-prefix lines from
// LLM output. Two cases:
//
//  1. Prefix at the very start of the text → strip the prefix token, keep
//     the rest (the content after "Browser: " is the actual response).
//  2. Prefix in the middle of the text → the content after the prefix is
//     almost always a duplicate of the preceding text. Truncate at the
//     prefix boundary to remove the duplicate.
//
// Code blocks (``` fenced) are excluded from matching to avoid false positives
// on lines like "Browser: connected" inside a code sample.
func StripRolePrefixHallucination(s string) string {
	if s == "" {
		return s
	}

	// Fast path: no known prefix present at all.
	// Check both ASCII colon (:) and fullwidth colon (：U+FF1A).
	if !strings.Contains(s, "Browser:") && !strings.Contains(s, "Tool:") &&
		!strings.Contains(s, "Browser：") && !strings.Contains(s, "Tool：") {
		return s
	}

	// Split into code-block-aware segments. We only process segments that
	// are outside fenced code blocks.
	type segment struct {
		text   string
		isCode bool
	}
	var segments []segment
	rest := s
	for {
		idx := strings.Index(rest, "```")
		if idx < 0 {
			segments = append(segments, segment{text: rest, isCode: false})
			break
		}
		if idx > 0 {
			segments = append(segments, segment{text: rest[:idx], isCode: false})
		}
		// Find closing ```.
		closeIdx := strings.Index(rest[idx+3:], "```")
		if closeIdx < 0 {
			// Unclosed code block — treat the rest as code.
			segments = append(segments, segment{text: rest[idx:], isCode: true})
			break
		}
		end := idx + 3 + closeIdx + 3
		segments = append(segments, segment{text: rest[idx:end], isCode: true})
		rest = rest[end:]
	}

	// Scan non-code segments for role prefix.
	for i, seg := range segments {
		if seg.isCode {
			continue
		}
		loc := rolePrefixRe.FindStringIndex(seg.text)
		if loc == nil {
			continue
		}

		// Compute absolute offset of the match in the original string.
		absOffset := 0
		for j := 0; j < i; j++ {
			absOffset += len(segments[j].text)
		}
		absOffset += loc[0]

		trimmedBefore := strings.TrimSpace(s[:absOffset])
		if trimmedBefore == "" {
			// Case 1: prefix at the start — strip the "Browser: " token.
			// Find the end of the matched prefix (e.g. "Browser: ").
			prefixEnd := absOffset + loc[1]
			return strings.TrimSpace(s[prefixEnd:])
		}
		// Case 2: prefix in the middle — truncate before it.
		return strings.TrimSpace(trimmedBefore)
	}

	return s
}

// ---------------------------------------------------------------------------
// Tool availability hallucination detection
// ---------------------------------------------------------------------------

// toolClaimPatterns extracts tool-name-like identifiers that the LLM claims
// are unavailable. Patterns use a generic [a-z][a-z0-9_]+ capture instead of
// hardcoded tool names — the actual verification is done against the real
// tool list passed to DetectToolAvailabilityHallucination.
var toolClaimPatterns = []*regexp.Regexp{
	// Chinese: 没有/不具备/无法使用 + identifier + 工具/命令/可用
	// Requires a tool-related suffix to avoid false positives like "没有找到 bash 脚本".
	regexp.MustCompile(`(?:没有|不具备|无法使用|不可用|没有找到|缺少)\s{0,5}([a-z][a-z0-9_]{1,30})\s{0,3}(?:工具|命令|可用)`),
	// Chinese: identifier + 工具 + 不可用/不存在/没有
	regexp.MustCompile(`([a-z][a-z0-9_]{1,30})\s{0,3}(?:工具|命令)\s{0,2}(?:不可用|不存在|没有|缺失|不在)`),
	// Chinese: 没有 X 和 Y 工具 (two identifiers joined by 和/以及/、)
	regexp.MustCompile(`没有\s{0,3}([a-z][a-z0-9_]{1,30})\s{0,3}(?:和|以及|、)\s{0,3}([a-z][a-z0-9_]{1,30})\s{0,3}(?:工具|命令)?(?:可用)?`),
	// English: don't have / do not have / unavailable + the? + identifier + tool?
	regexp.MustCompile(`(?i)(?:don'?t have|do not have|not have|unavailable|no access to)\s{1,10}(?:the\s+)?([a-z][a-z0-9_]{1,30})\s{0,3}(?:tool)?`),
}

// DetectToolAvailabilityHallucination checks if the LLM output claims a tool
// is unavailable, then verifies against the actual tool list sent to the LLM.
// Only returns a correction when the claimed tool IS in the actual list —
// meaning the LLM is lying about its capabilities.
//
// This is mechanism-level: no hardcoded tool name set. Any tool the LLM
// falsely claims is missing will be caught, whether it's bash, ssh, or a
// future tool not yet invented.
//
// actualTools is the tool definitions sent to the LLM in this iteration.
func DetectToolAvailabilityHallucination(text string, actualTools []map[string]interface{}) string {
	if text == "" || len(actualTools) == 0 {
		return ""
	}

	// Build set of tool names actually sent to the LLM.
	available := make(map[string]bool, len(actualTools))
	for _, t := range actualTools {
		if name := tool.ExtractToolName(t); name != "" {
			available[name] = true
		}
	}
	if len(available) == 0 {
		return ""
	}

	// Strip fenced code blocks to avoid false positives.
	stripped := StripCodeBlocks(text)
	if stripped == "" {
		return ""
	}

	// Extract identifiers the LLM claims are unavailable,
	// then verify each against the actual tool list.
	claimed := make(map[string]bool)
	for _, re := range toolClaimPatterns {
		for _, m := range re.FindAllStringSubmatch(stripped, 5) {
			for i := 1; i < len(m); i++ {
				name := m[i]
				if name != "" && available[name] {
					claimed[name] = true
				}
			}
		}
	}
	if len(claimed) == 0 {
		return ""
	}

	var tools []string
	for name := range claimed {
		tools = append(tools, name)
	}
	sort.Strings(tools)

	return fmt.Sprintf("[系统纠正] 你声称没有 %s 工具，但这些工具在你当前的工具列表中。"+
		"请直接使用它们完成任务。", strings.Join(tools, "、"))
}

// StripCodeBlocks removes ``` fenced code blocks from text, returning only
// the prose portions for hallucination scanning.
func StripCodeBlocks(s string) string {
	var b strings.Builder
	rest := s
	for {
		idx := strings.Index(rest, "```")
		if idx < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:idx])
		closeIdx := strings.Index(rest[idx+3:], "```")
		if closeIdx < 0 {
			break
		}
		rest = rest[idx+3+closeIdx+3:]
	}
	return b.String()
}
