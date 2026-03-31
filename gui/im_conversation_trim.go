package main

// Conversation trimming: token estimation, context window management,
// and conversation history compaction utilities.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

func estimateConversationEntryTokens(entries []conversationEntry) int {
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
// text tokens — vision tokens are counted separately by the API.
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
// webFetchMaxToolResult allows web_fetch to return up to 20KB to the LLM,
// since its content is already pre-truncated inside the handler.
const webFetchMaxToolResult = 20480

func truncateToolResultForTool(toolName, s string) string {
	// web_fetch gets a higher budget — content is already truncated in handler
	limit := maxToolResultLen
	if toolName == "web_fetch" {
		limit = webFetchMaxToolResult
	}
	if len(s) <= limit {
		return s
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

// inferFileDeliveryMessage generates a user-facing prompt based on the file name
// when no explicit message was provided. This ensures PDF documents sent via IM
// always include a hint telling the user what the document is and what to do.
func inferFileDeliveryMessage(fileName string) string {
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
