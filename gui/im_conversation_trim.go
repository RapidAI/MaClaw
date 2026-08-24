package main

// Conversation trimming: token estimation, context window management,
// and conversation history compaction utilities.

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/toolresult"
)

func estimateConversationEntryTokens(entries []agent.ConversationEntry) int {
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
				blockKind := normalizeIMContentBlockKind(blockType)
				if blockKind == imContentBlockImageURL {
					// Vision image block — count a fixed ~85 tokens (low-detail)
					// instead of serializing the huge base64 string.
					total += 85
					continue
				}
				if blockKind == imContentBlockImage {
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
// For JSON data, uses a byte-based heuristic (~2.5 bytes/token) rather than
// character-based, because JSON structural overhead ({, ", :) inflates the
// ASCII char count beyond what represents actual content tokens.
func estimateBytesToTokens(data []byte) int {
	return (len(data)*10 + 24) / 25 // equivalent to len/2.5, rounded up
}

// precomputeMsgTokens computes per-message token estimates for an entire
// conversation slice in a single pass. This eliminates repeated json.Marshal
// calls when trimConversation iteratively tries different drop counts.
func precomputeMsgTokens(msgs []interface{}) []int {
	tokens := make([]int, len(msgs))
	for i, m := range msgs {
		tokens[i] = estimateSingleMsgTokens(m)
	}
	return tokens
}

// quickConversationTokenEstimate provides a fast lower-bound token estimate
// by measuring content string lengths directly (no json.Marshal). This
// intentionally under-counts (ignores JSON structural overhead, field names,
// quoting), so the caller uses a 2x safety margin: only short-circuits when
// quickEstimate*2 <= budget.
func quickConversationTokenEstimate(msgs []interface{}) int {
	total := 0
	for _, m := range msgs {
		total += quickSingleMsgTokenEstimate(m)
	}
	return total
}

func quickSingleMsgTokenEstimate(m interface{}) int {
	// Per-message JSON overhead: {"role":"assistant","content":"..."} ≈ 30 bytes ≈ 12 tokens
	const perMsgOverhead = 12
	switch v := m.(type) {
	case map[string]interface{}:
		// Messages with tool_calls have unpredictable size (function args can
		// be thousands of bytes). Fall back to json.Marshal for accuracy.
		if v["tool_calls"] != nil {
			data, _ := json.Marshal(v)
			return estimateBytesToTokens(data)
		}
		tokens := perMsgOverhead
		// Handle multimodal content ([]interface{} with image blocks).
		if blocks, ok := v["content"].([]interface{}); ok {
			for _, block := range blocks {
				bm, ok := block.(map[string]interface{})
				if !ok {
					continue
				}
				blockType, _ := bm["type"].(string)
				blockKind := normalizeIMContentBlockKind(blockType)
				if blockKind == imContentBlockImageURL || blockKind == imContentBlockImage {
					tokens += 85
				} else if text, ok := bm["text"].(string); ok {
					tokens += len(text) / 3
				}
			}
			return tokens
		}
		if content, ok := v["content"].(string); ok {
			tokens += len(content) / 3 // ~3 bytes per token for mixed CJK/English
		}
		if rc, ok := v["reasoning_content"].(string); ok {
			tokens += len(rc) / 4
		}
		return tokens
	case map[string]string:
		tokens := perMsgOverhead
		if content := v["content"]; content != "" {
			tokens += len(content) / 3
		}
		return tokens
	default:
		// Unknown type — use conservative estimate
		return 100
	}
}

// estimateSingleMsgTokens estimates the token count for a single conversation
// message. Same logic as estimateConversationTokens but for one message.
func estimateSingleMsgTokens(m interface{}) int {
	mm, ok := m.(map[string]interface{})
	if !ok {
		data, _ := json.Marshal(m)
		return estimateBytesToTokens(data)
	}
	// Check if content is a multimodal array (vision messages).
	if content, ok := mm["content"].([]interface{}); ok {
		total := 0
		for _, block := range content {
			bm, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := bm["type"].(string)
			blockKind := normalizeIMContentBlockKind(blockType)
			if blockKind == imContentBlockImageURL || blockKind == imContentBlockImage {
				total += 85
				continue
			}
			data, _ := json.Marshal(bm)
			total += estimateBytesToTokens(data)
		}
		total += 10 // role overhead
		return total
	}
	data, _ := json.Marshal(mm)
	return estimateBytesToTokens(data)
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
	data, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	var obj map[string]interface{}
	if json.Unmarshal(data, &obj) != nil {
		return ""
	}
	r, _ := obj["role"].(string)
	return r
}

// msgHasToolCalls checks if a conversation message has a non-nil tool_calls field.
func msgHasToolCalls(m interface{}) bool {
	switch v := m.(type) {
	case map[string]interface{}:
		return v["tool_calls"] != nil
	case map[string]string:
		return false
	}
	data, err := json.Marshal(m)
	if err != nil {
		return false
	}
	var obj map[string]interface{}
	if json.Unmarshal(data, &obj) != nil {
		return false
	}
	return obj["tool_calls"] != nil
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

	// Early exit: conversations with ≤3 messages are never trimmed.
	if len(msgs) <= 3 {
		return msgs
	}

	// Fast short-circuit: use a cheaper estimate (only marshals tool_call msgs,
	// uses string length for text msgs) to avoid the full O(n) json.Marshal
	// precompute when the conversation clearly fits within budget.
	// The quick estimate under-counts text-only messages, so we require
	// doubling it stays below budget before trusting the short-circuit.
	quickEstimate := quickConversationTokenEstimate(msgs)
	if quickEstimate*2 <= msgBudget {
		return msgs
	}

	// Pre-compute per-message token estimates once. This avoids repeated
	// json.Marshal calls in the drop-groups loop below (P1 optimization).
	msgTokens := precomputeMsgTokens(msgs)
	totalTokens := 0
	for _, t := range msgTokens {
		totalTokens += t
	}

	if totalTokens <= msgBudget {
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

	// Pre-compute per-group token totals for incremental subtraction.
	groupTokens := make([]int, len(groups))
	for gi, g := range groups {
		for idx := g.start; idx < g.end; idx++ {
			groupTokens[gi] += msgTokens[idx]
		}
	}

	// Try dropping the fewest groups from the front first (dropCount=1),
	// increasing until the remaining tail fits within the budget.
	// This preserves as much recent context as possible.
	systemMsg := msgs[:1]
	fallbackPlaceholder := []interface{}{map[string]string{
		"role":    "user",
		"content": "[注意：中间的对话历史因上下文长度限制已被省略，请基于最近的上下文继续工作]",
	}}
	placeholderTokens := estimateConversationTokens(fallbackPlaceholder)

	// Start from keeping all groups, then drop from the front.
	// First pass: find the minimum dropCount using incremental subtraction
	// instead of re-estimating the full result each iteration.
	// resultTokens = system + placeholder + all groups - dropped groups
	allGroupsTokens := 0
	for _, gt := range groupTokens {
		allGroupsTokens += gt
	}
	baseResultTokens := msgTokens[0] + placeholderTokens + allGroupsTokens

	bestDropCount := -1
	cumulativeDropped := 0
	for dropCount := 1; dropCount < len(groups); dropCount++ {
		cumulativeDropped += groupTokens[dropCount-1]
		resultTokens := baseResultTokens - cumulativeDropped
		if resultTokens <= msgBudget {
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
			// Summarizer is an LLM call — protect against hangs with a timeout.
			// If the summarizer doesn't return within 15s, fall back to the
			// static placeholder. This prevents the agent loop from blocking
			// indefinitely when the LLM API is overloaded/unreachable.
			summaryCh := make(chan string, 1)
			go func() { summaryCh <- summarizer(raw) }()
			var summary string
			select {
			case summary = <-summaryCh:
			case <-time.After(15 * time.Second):
				log.Printf("[trim-conversation] summarizer timed out after 15s, using fallback placeholder")
			}
			if summary != "" {
				// Cap summary to ~2000 tokens (~5000 chars) to avoid blowing the budget.
				if len(summary) > 5000 {
					runes := []rune(summary)
					if len(runes) > 5000 {
						summary = string(runes[:5000]) + "…"
					}
				}
				placeholder = []interface{}{
					map[string]string{"role": "user", "content": "[对话历史摘要]\n" + summary},
					map[string]string{"role": "assistant", "content": "好的，我已了解之前的对话上下文。", "reasoning_content": ""},
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

	// Last resort: the conversation may still be over budget when the current
	// user turn itself is very large. Never discard that turn: doing so makes a
	// latency-oriented trim silently change the user's request into a generic
	// placeholder. Keeping it can exceed the requested budget, but is the only
	// safe representation; the provider can then report an explicit context
	// limit error instead of answering a different request. Everything from the
	// newest user message forward is valid as a standalone tail (it cannot
	// orphan a preceding assistant tool call).
	for i := len(msgs) - 1; i >= 1; i-- {
		if msgRole(msgs[i]) != "user" {
			continue
		}
		result := make([]interface{}, 0, len(systemMsg)+len(fallbackPlaceholder)+len(msgs)-i)
		result = append(result, systemMsg...)
		result = append(result, fallbackPlaceholder...)
		result = append(result, msgs[i:]...)
		return result
	}

	// A malformed history without a user turn has no request to protect. Keep
	// the provider-valid minimal fallback rather than risking an orphaned tool
	// result.
	result = make([]interface{}, 0, len(systemMsg)+len(fallbackPlaceholder))
	result = append(result, systemMsg...)
	return append(result, fallbackPlaceholder...)
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

	// DeepSeek V4+ thinking mode rule (with tools present):
	//   When the conversation contains ANY tool-call message, the API
	//   requires reasoning_content to be preserved on ALL assistant
	//   messages — not just those with tool_calls. This is because
	//   drop_thinking is automatically disabled when tools are present.
	//
	// See: https://api-docs.deepseek.com/guides/thinking_mode
	//   "Between two user messages, if the model performed a tool call,
	//    the intermediate assistant's reasoning_content must participate
	//    in the context concatenation and must be passed back to the API
	//    in all subsequent user interaction turns."
	//
	// Also from the V4 encoding doc (HuggingFace):
	//   "With tools (on system or developer message): drop_thinking is
	//    automatically disabled. All turns retain their reasoning."
	conversationHasToolCalls := false
	for _, m := range result {
		if msgHasToolCalls(m) {
			conversationHasToolCalls = true
			break
		}
	}

	// Pre-compute current total to avoid repeated full-scan estimation.
	currentTotal := 0
	msgToks := make([]int, len(result))
	for i, m := range result {
		msgToks[i] = estimateSingleMsgTokens(m)
		currentTotal += msgToks[i]
	}

	for i, m := range result {
		if currentTotal <= budget {
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
		// DeepSeek thinking mode reasoning_content preservation:
		//
		// When the conversation has tool calls (conversationHasToolCalls),
		// ALL assistant messages must retain reasoning_content. The field
		// must exist (even as empty string ""); missing field → HTTP 400.
		//
		// When the conversation has NO tool calls, reasoning_content on
		// non-tool-call messages is ignored by the API and can be deleted
		// to reclaim token budget.
		//
		// Verified empirically:
		//   Full reasoning_content      → 200 		//   Truncated reasoning_content → 200 		//   Empty string ""             → 200 		//   Field missing entirely      → 400 (when tools present)
		if conversationHasToolCalls {
			// Conversation has tool calls: reasoning_content field must exist.
			// Truncate long reasoning to reclaim token budget (API accepts
			// truncated values), but never delete the field entirely.
			if rc, _ := cp["reasoning_content"].(string); len([]rune(rc)) > 200 {
				runes := []rune(rc)
				cp["reasoning_content"] = string(runes[:100]) + "…(truncated)…" + string(runes[len(runes)-50:])
			}
		} else {
			// No tool calls in conversation: API ignores reasoning_content.
			// Delete it entirely to reclaim token budget.
			delete(cp, "reasoning_content")
		}
		content, _ := cp["content"].(string)
		if len(content) <= 200 {
			result[i] = cp
			newToks := estimateSingleMsgTokens(cp)
			currentTotal += newToks - msgToks[i]
			msgToks[i] = newToks
			continue
		}
		runes := []rune(content)
		if len(runes) <= 200 {
			result[i] = cp
			newToks := estimateSingleMsgTokens(cp)
			currentTotal += newToks - msgToks[i]
			msgToks[i] = newToks
			continue
		}
		cp["content"] = string(runes[:100]) + "\n…(截断)…\n" + string(runes[len(runes)-50:])
		result[i] = cp
		newToks := estimateSingleMsgTokens(cp)
		currentTotal += newToks - msgToks[i]
		msgToks[i] = newToks
	}
	return result
}

// compactionHandoffPrompt is the structured prompt for LLM-based conversation
// compaction. Inspired by Codex CLI's "CONTEXT CHECKPOINT COMPACTION" design:
// the summarizer writes a handoff document for the next LLM, not meeting minutes.
//
// Four required sections ensure stable, structured output regardless of
// conversation content or language.
const compactionHandoffPrompt = `你正在执行上下文检查点压缩（Context Checkpoint Compaction）。
为将要继续此任务的另一个 LLM 生成一份交接摘要。

必须包含以下四个部分:
1. **当前进度**: 已完成的工作和已做的关键决策
2. **重要上下文**: 约束条件、用户偏好、技术选型等
3. **待完成工作**: 明确的下一步行动
4. **关键数据**: 继续工作所需的文件路径、变量名、配置值等

要求:
- 简洁、结构化，使用 Markdown 列表
- 直接引用关键短语而非转述（防止语义漂移）
- 不要包含工具调用的原始输出（文件内容、命令输出等）
- 聚焦于帮助下一个 LLM 无缝继续工作

以下是需要压缩的对话内容:
`

// compactionRecoveryPrefix is prepended to the LLM-generated summary when
// injecting it back into the conversation history. It tells the resuming LLM
// three critical things:
//  1. Someone already did part of the work
//  2. The tool state (filesystem, code) reflects that completed work
//  3. Do not repeat what was already done
//
// This is the MacLaw equivalent of Codex CLI's summary_prefix.md.
const compactionRecoveryPrefix = `[上下文恢复] 之前的对话因长度限制被压缩为以下交接摘要。

另一个语言模型已经开始处理此任务并产出了以下工作摘要。你可以访问该模型使用过的工具的当前状态（文件系统、代码等反映了已完成的工作）。请基于已完成的工作继续，避免重复已做过的事情。

以下是之前模型产出的交接摘要:

`

// makeSummarizer returns a summarizer callback that uses doSimpleLLMRequest
// to condense dropped conversation history into a structured handoff summary.
//
// The prompt uses the "Context Checkpoint Compaction" pattern from Codex CLI:
// instead of generic "please summarize", it asks for a structured handoff
// document with 4 sections (progress, context, TODOs, critical data).
func makeSummarizer(cfg corelib.MaclawLLMConfig, httpClient *http.Client) func(string) string {
	return func(text string) string {
		msgs := []interface{}{
			map[string]string{"role": "user", "content": compactionHandoffPrompt + text},
		}
		ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "conversation-trim-summary"})
		result, err := doSimpleLLMRequest(ctx, attachLightweightHubHint(cfg, llm.TaskSummary), msgs, httpClient, 30*time.Second)
		if err != nil || result.Content == "" {
			return ""
		}
		return result.Content
	}
}

func trimHistory(entries []agent.ConversationEntry) []agent.ConversationEntry {
	return trimHistoryWithSummary(entries, nil, nil, 0, 0)
}

// trimHistoryWithSummary performs structured conversation compaction with
// three preservation tiers, inspired by Codex CLI's compaction architecture:
//
//  1. Turn boundaries (tier-1): first user msg + first assistant response per turn
//  2. User messages (tier-user): ALL user messages from dropped region, preserved
//     verbatim with a token budget (Codex insight: user intent must never be lost)
//  3. Recent window: most recent entries kept in full
//
// Between tier-1+tier-user and the recent window, a separator is inserted:
//   - With summarizer: a structured handoff summary with recovery prompt
//     (tells the LLM "another model already did this work, continue from here")
//   - Without summarizer: static placeholder
//
// summarizer: if non-nil, called with the text of dropped entries to produce
// a compressed handoff summary. Returns empty string on failure — caller falls
// back to static placeholder.
//
// memorySink: if non-nil, substantial assistant messages (>500 runes) that
// are being dropped are saved to long-term memory as task_artifact entries.
//
// maxEntries: if > 0, overrides MaxConversationTurns as the entry count limit.
// Used by saveConversationHistoryTimed to pass a dynamic limit based on the
// model's effective context window.
//
// maxTokens: if > 0, also triggers compaction when token count exceeds this
// limit, even if entry count is within maxEntries. This covers the case where
// few entries have very large content (e.g., huge tool results).
func trimHistoryWithSummary(entries []agent.ConversationEntry, summarizer func(string) string, memorySink func(string, []string), maxEntries int, maxTokens int) []agent.ConversationEntry {
	return trimHistoryWithSummaryPrecomputed(entries, summarizer, memorySink, maxEntries, maxTokens, 0)
}

// trimHistoryWithSummaryPrecomputed is like trimHistoryWithSummary but accepts
// a pre-computed token estimate to skip redundant estimation at the entrance.
// Pass precomputedTokens=0 to compute it internally.
func trimHistoryWithSummaryPrecomputed(entries []agent.ConversationEntry, summarizer func(string) string, memorySink func(string, []string), maxEntries int, maxTokens int, precomputedTokens int) []agent.ConversationEntry {
	limit := agent.MaxConversationTurns
	if maxEntries > 0 {
		limit = maxEntries
	}
	entryOverLimit := len(entries) > limit
	var tokenOverLimit bool
	if maxTokens > 0 {
		tokEst := precomputedTokens
		if tokEst <= 0 {
			tokEst = estimateConversationEntryTokens(entries)
		}
		tokenOverLimit = tokEst > maxTokens
	}
	if !entryOverLimit && !tokenOverLimit {
		return entries
	}

	// --- Three-tier compaction ---
	//
	// Tier 1: Turn boundaries — structural invariant (first user + first
	//         assistant per conversational turn). Task-level semantics.
	// Tier U: User messages — all user messages from the dropped region,
	//         preserved verbatim. Codex CLI's key insight: user intent
	//         (constraints, preferences, corrections) must survive compaction.
	// Tier R: Recent window — most recent entries in full.

	const maxTier1 = 10
	const maxPreservedUserTokens = 8000 // Codex uses 20K; conservative for smaller contexts

	// Build entry groups once — used by tier-1 extraction, recentStart
	// group-alignment, and groupAlignedSlice. This is the single source of
	// truth for group boundaries in this compaction pass.
	groups := agent.BuildEntryGroups(entries)

	tier1Indices := extractTurnBoundaryIndices(entries, maxTier1)

	// First pass: compute recent window.
	// When triggered by entry count: keep the last `limit` entries.
	// When triggered by token overflow only: scan backwards to find how
	// many entries fit within 70% of maxTokens (leaving room for the
	// compacted summary).
	var recentStart int
	if entryOverLimit {
		recentStart = len(entries) - limit
		// Group-align so we don't split a tool-call group.
		g := agent.GroupContaining(groups, recentStart)
		if g != nil && recentStart > g.Start {
			recentStart = g.End
		}
	} else {
		// Token-only overflow: density-aware split.
		tokenBudget := maxTokens * 7 / 10
		runningTokens := 0
		recentStart = 0
		for i := len(entries) - 1; i >= 0; i-- {
			entryData, _ := json.Marshal(entries[i])
			entryTokens := estimateBytesToTokens(entryData)
			if runningTokens+entryTokens > tokenBudget {
				recentStart = i + 1
				break
			}
			runningTokens += entryTokens
		}
		// Group-align so we don't split a tool-call group.
		g := agent.GroupContaining(groups, recentStart)
		if g != nil && recentStart > g.Start {
			recentStart = g.End
		}
	}
	if recentStart < 0 {
		recentStart = 0
	}

	// Count tier-1 entries outside the recent window.
	outsideTier1 := 0
	for _, idx := range tier1Indices {
		if idx < recentStart {
			outsideTier1++
		}
	}

	// If all tier-1 entries are inside the recent window, simple FIFO.
	// Use groups to ensure we never cut in the middle of a tool_calls group.
	if outsideTier1 == 0 {
		trimmed := groupAlignedSlice(entries, recentStart)
		return trimmed
	}

	// Second pass: shrink recent window to make room for outside tier-1.
	recentCount := limit - outsideTier1
	if recentCount < limit/2 {
		recentCount = limit / 2
	}
	recentStart = len(entries) - recentCount
	if recentStart < 0 {
		recentStart = 0
	}
	// Group-align the second-pass recentStart so we never split a
	// tool_calls group between outsideSet and the recent window.
	// Without this, an assistant(tool_calls) at index N can end up in
	// outsideSet while its tool result at N+1 falls into the recent window
	// (excluded from outsideSet), producing an orphaned tool_calls message.
	{
		g := agent.GroupContaining(groups, recentStart)
		if g != nil && recentStart > g.Start {
			recentStart = g.End
		}
	}

	// Build a set of outside-tier-1 indices against the FINAL recentStart
	// to avoid duplicating entries that moved into the new recent window.
	outsideSet := make(map[int]bool)
	for _, idx := range tier1Indices {
		if idx < recentStart {
			outsideSet[idx] = true
		}
	}

	// If recalculation moved all tier-1 inside, fall back to FIFO.
	if len(outsideSet) == 0 {
		trimmed := groupAlignedSlice(entries, recentStart)
		return trimmed
	}

	// --- Collect preserved user messages from the dropped region ---
	// Codex CLI's collect_user_messages + build_compacted_history pattern:
	// iterate from most recent dropped entry backwards, preserving user
	// messages verbatim until the token budget is exhausted.
	//
	// Budget is capped to ensure total output stays within the entry limit
	// + a small margin. Each preserved user message takes one slot.
	maxPreservedUserSlots := limit / 8 // max 5 extra slots (at limit=40)
	preservedUserMsgs := collectPreservedUserMessages(entries, recentStart, outsideSet, maxPreservedUserTokens, maxPreservedUserSlots)

	// Build result: outside tier-1 entries + preserved user msgs + separator + recent window.
	result := make([]agent.ConversationEntry, 0, len(outsideSet)+len(preservedUserMsgs)+1+recentCount)
	for i := 0; i < recentStart; i++ {
		if outsideSet[i] {
			result = append(result, entries[i])
		}
	}

	// Append preserved user messages (between tier-1 and separator).
	result = append(result, preservedUserMsgs...)

	// Sink substantial assistant messages that are being dropped to long-term
	// memory (Phase 1 supplement: catches non-workflow documents like analysis
	// reports, research summaries, etc. that aren't captured by SavePhaseOutput).
	if memorySink != nil {
		for i := 0; i < recentStart; i++ {
			if outsideSet[i] {
				continue // already preserved as tier-1
			}
			if entries[i].Role != "assistant" {
				continue
			}
			text, ok := entries[i].Content.(string)
			if !ok || len([]rune(text)) < 500 {
				continue
			}
			memorySink(text, []string{"trimmed", "auto_salvaged"})
		}
	}

	// Build separator: structured handoff summary with recovery prompt,
	// or static placeholder when no summarizer is available.
	//
	// Uses a structured 4-section input (turn boundaries, key data, tool
	// operations, final assistant summaries) instead of raw entry dumps.
	// This produces much higher quality summaries because the LLM receives
	// organized context rather than truncated role/text lines.
	separator := "[...中间的工具调用和执行细节已省略...]"
	if summarizer != nil {
		droppedEntries := make([]agent.ConversationEntry, 0, recentStart)
		for i := 0; i < recentStart; i++ {
			if outsideSet[i] {
				continue // tier-1, already preserved
			}
			droppedEntries = append(droppedEntries, entries[i])
		}
		if len(droppedEntries) > 0 {
			structuredInput := buildCompactionSummarizerInput(droppedEntries)
			if structuredInput != "" {
				if summary := summarizer(structuredInput); summary != "" {
					separator = compactionRecoveryPrefix + summary
				}
			}
		}
	}

	result = append(result, agent.ConversationEntry{
		Role:    "system",
		Content: separator,
	})
	// Append the recent window, aligned to group boundaries so we never
	// start with orphaned tool messages from a split group.
	result = append(result, groupAlignedSlice(entries, recentStart)...)

	return result
}

// groupAlignedSlice returns entries[start:] but adjusts start forward to
// the nearest group boundary so we never start in the middle of a
// tool_calls group (which would orphan tool messages without their
// preceding assistant). This replaces the old pattern of:
//
//	trimmed = entries[start:]
//	for len(trimmed) > 0 && trimmed[0].Role == "tool" { trimmed = trimmed[1:] }
//
// which could leave an assistant(tool_calls) at the end of the dropped
// region without its tool messages in the kept region.
func groupAlignedSlice(entries []agent.ConversationEntry, start int) []agent.ConversationEntry {
	if start <= 0 {
		return entries
	}
	if start >= len(entries) {
		return nil
	}
	groups := agent.BuildEntryGroups(entries)
	g := agent.GroupContaining(groups, start)
	if g != nil && start > g.Start {
		// start is in the middle of a group — advance to the next group.
		start = g.End
	}
	if start >= len(entries) {
		return nil
	}
	return entries[start:]
}

// collectPreservedUserMessages extracts user messages from the dropped region
// (indices 0..recentStart-1, excluding tier-1 entries) and returns them in
// chronological order, respecting a token budget.
//
// This implements Codex CLI's core compaction insight: user messages carry
// intent (constraints, preferences, corrections) that must survive compaction.
// The LLM summary captures what was *done*; user messages capture what was
// *requested*. Both are needed for seamless continuation.
//
// Messages are collected from most recent to oldest (recency bias), then
// reversed to chronological order. Overly long messages are truncated.
func collectPreservedUserMessages(entries []agent.ConversationEntry, recentStart int, outsideSet map[int]bool, maxTokens int, maxSlots int) []agent.ConversationEntry {
	var collected []agent.ConversationEntry
	remaining := maxTokens

	for i := recentStart - 1; i >= 0; i-- {
		if len(collected) >= maxSlots {
			break // slot budget exhausted
		}
		if outsideSet[i] {
			continue // already in tier-1
		}
		if entries[i].Role != "user" {
			continue
		}
		text, ok := entries[i].Content.(string)
		if !ok || text == "" {
			continue
		}

		tokens := len(text) / 3 // rough estimate: ~3 bytes per token for mixed CJK+code
		if tokens <= 0 {
			tokens = 1
		}

		if tokens <= remaining {
			collected = append(collected, entries[i])
			remaining -= tokens
		} else if remaining > 200 {
			// Truncate overly long message but still preserve it.
			runes := []rune(text)
			// Approximate: remaining tokens * 3 bytes / ~3 bytes per rune ≈ remaining runes
			cutoff := remaining
			if cutoff > len(runes) {
				cutoff = len(runes)
			}
			truncated := string(runes[:cutoff])
			collected = append(collected, agent.ConversationEntry{
				Role:    "user",
				Content: truncated + "\n[...消息被截断...]",
			})
			break
		} else {
			break // budget exhausted
		}
	}

	// Reverse to chronological order.
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected
}

// extractTurnBoundaryIndices returns indices of "turn boundary" entries:
// the first user message and the first assistant response of each
// conversational turn. This is a structural invariant that doesn't depend
// on content, language, or keywords.
//
// A "turn" starts when a user message appears after a non-user role
// (or at the beginning). The first assistant message after that user
// message completes the turn boundary pair.
//
// Fork-turn awareness (inspired by Codex CLI's fork_turn_positions_in_rollout):
// System-injected user messages (SubAgent context, recover prompts, steering
// injections) are deprioritized — real user turns fill the budget first,
// then synthetic turns fill remaining slots. This ensures user intent is
// preserved over framework-generated context during compaction.
//
// Tool-call group integrity: when an assistant(tool_calls) entry is selected
// as a boundary, ALL entries in its group (the assistant + following tool
// entries) are included. This uses BuildEntryGroups to ensure the same
// grouping logic as TrimHistory and TrimConversation.
func extractTurnBoundaryIndices(entries []agent.ConversationEntry, maxCount int) []int {
	// Build groups first — we need them to expand assistant selections.
	groups := agent.BuildEntryGroups(entries)

	var realTurns []int      // user-initiated turn boundaries
	var syntheticTurns []int // system-injected turn boundaries
	prevRole := ""
	lastUserWasSynthetic := false // tracks whether the most recent user msg was synthetic
	for i, e := range entries {
		switch e.Role {
		case "user":
			if prevRole != "user" {
				if isSyntheticUserMessage(e) {
					syntheticTurns = append(syntheticTurns, i)
					lastUserWasSynthetic = true
				} else {
					realTurns = append(realTurns, i)
					lastUserWasSynthetic = false
				}
			}
		case "assistant":
			if prevRole == "user" {
				if lastUserWasSynthetic {
					syntheticTurns = append(syntheticTurns, i)
				} else {
					realTurns = append(realTurns, i)
				}
			}
		}
		if e.Role != "" {
			prevRole = e.Role
		}
	}

	// Merge: real turns first, then synthetic turns, up to maxCount.
	selected := make([]int, 0, maxCount)
	for _, idx := range realTurns {
		if len(selected) >= maxCount {
			break
		}
		selected = append(selected, idx)
	}
	for _, idx := range syntheticTurns {
		if len(selected) >= maxCount {
			break
		}
		selected = append(selected, idx)
	}

	// Expand: if a selected index is an assistant(tool_calls), include all
	// entries in its group. This is the mechanism-level fix — we never
	// select an assistant without its tool messages.
	expandedSet := make(map[int]bool, len(selected)*2)
	for _, idx := range selected {
		g := agent.GroupContaining(groups, idx)
		if g == nil {
			expandedSet[idx] = true
			continue
		}
		for j := g.Start; j < g.End; j++ {
			expandedSet[j] = true
		}
	}

	// Convert set to sorted slice.
	result := make([]int, 0, len(expandedSet))
	for idx := range expandedSet {
		result = append(result, idx)
	}
	sort.Ints(result)
	return result
}

// isSyntheticUserMessage returns true for user-role messages that were
// injected by the framework rather than typed by the actual user.
// These include SubAgent context, recover prompts, system notifications,
// and other framework-generated messages.
//
// This is the MacLaw equivalent of Codex CLI's distinction between
// "real user messages" and "trigger_turn" messages in fork-turn boundaries.
func isSyntheticUserMessage(e agent.ConversationEntry) bool {
	text, ok := e.Content.(string)
	if !ok || text == "" {
		return false
	}
	return corelib.IsSyntheticUserContent(text)
}

// extractTurnBoundaryTexts returns the text content of turn-boundary entries.
// Shared by TopicDetector and buildCompactionSummarizerInput.
func extractTurnBoundaryTexts(entries []agent.ConversationEntry, maxTexts int) []string {
	var texts []string
	prevRole := ""
	for _, e := range entries {
		if len(texts) >= maxTexts {
			break
		}
		text, ok := e.Content.(string)
		switch e.Role {
		case "user":
			if prevRole != "user" && ok && text != "" {
				texts = append(texts, text)
			}
		case "assistant":
			if prevRole == "user" && ok && text != "" {
				texts = append(texts, text)
			}
		}
		if e.Role != "" {
			prevRole = e.Role
		}
	}
	return texts
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
// webFetchMaxToolResult allows web_fetch to return up to 32KB to the LLM,
// since its content is already windowed inside the handler and carries
// continuation metadata that must survive truncation.
//
// Beyond simple size truncation, this function also applies semantic
// compression inspired by GenericAgent's context information density
// maximization principle: deduplicate repeated lines (common in compiler
// warnings and log output), and collapse long homogeneous blocks (e.g.
// 200 lines of "PASS" test output) into a summary line.
const webFetchMaxToolResult = 32768

func truncateToolResultForTool(toolName, s string) string {
	return agent.PreviewToolResultForTool(toolName, s)
}

// truncateToolResultForToolWithSession compresses/truncates for the model and
// spills the original full result to a tool_result handle when truncated.
func truncateToolResultForToolWithSession(toolName, sessionKey, original string) string {
	return agent.TruncateToolResultForToolWithSession(toolName, sessionKey, original)
}

// projectToolResultHandle spills oversized full results and appends a handle
// footer so the model can re-read via read_tool_result. Failures fall back to preview.
func projectToolResultHandle(toolName, sessionKey, original, preview string, limit int) string {
	proj, err := toolresult.Project(toolresult.ProjectOptions{
		ToolName:   toolName,
		SessionKey: sessionKey,
		Content:    original,
		Preview:    preview,
		Limit:      limit,
	})
	if err != nil {
		log.Printf("[toolresult] spill failed tool=%s: %v", toolName, err)
		if proj.Preview != "" {
			return proj.Preview
		}
		return preview
	}
	return proj.Preview
}

// compressToolResultSemantic applies content-aware compression to tool
// results before size truncation. Two mechanisms:
//
//  1. Line deduplication: consecutive identical or near-identical lines
//     (common in compiler warnings, npm install output, test results)
//     are collapsed into "... (重复 N 行) ...".
//
//  2. Homogeneous block collapse: when >10 consecutive lines match the
//     same pattern (e.g. all start with "PASS", "ok", "  OK"), keep the
//     first 3 + last 2 and insert a summary.
//
// This is inspired by GenericAgent's _clean_content which shrinks code
// blocks >6 lines to 5 lines + count. The principle: maximize information
// density by removing redundant content before the budget truncation
// removes unique content.
func compressToolResultSemantic(toolName string, s string) string {
	// Content-based activation: compress any tool result that is large
	// enough and has enough lines to benefit from deduplication. This
	// avoids maintaining a hardcoded tool name whitelist — new tools
	// (MCP tools, future code_run equivalents) automatically get
	// compression when their output is verbose and repetitive.
	_ = toolName // reserved for future per-tool strategy tuning

	if len(s) < 500 {
		return s
	}

	lines := strings.Split(s, "\n")
	if len(lines) < 10 {
		return s
	}

	var result []string
	compressed := false
	i := 0
	for i < len(lines) {
		line := lines[i]

		// --- Deduplication: collapse consecutive identical lines ---
		j := i + 1
		for j < len(lines) && lines[j] == line {
			j++
		}
		dupCount := j - i
		if dupCount >= 3 {
			result = append(result, line)
			result = append(result, fmt.Sprintf("... (重复 %d 行) ...", dupCount-1))
			i = j
			compressed = true
			continue
		}

		// --- Homogeneous block collapse: structurally repetitive lines ---
		// Collapse blocks where lines share a common prefix AND have similar
		// lengths (low variance = same format, only a counter/name changes).
		// This avoids collapsing blocks like import statements where each
		// line has genuinely different content despite sharing a prefix.
		prefix := extractLinePrefix(line)
		if prefix != "" && len(prefix) >= 2 {
			k := i + 1
			for k < len(lines) && strings.HasPrefix(lines[k], prefix) {
				k++
			}
			blockLen := k - i
			if blockLen > 10 {
				// Check structural repetitiveness: compute length variance.
				// If max-min length difference is <30% of average, the lines
				// are structurally similar (same format, different values).
				totalLen := 0
				minLen := len(lines[i])
				maxLen := len(lines[i])
				for x := i; x < k; x++ {
					l := len(lines[x])
					totalLen += l
					if l < minLen {
						minLen = l
					}
					if l > maxLen {
						maxLen = l
					}
				}
				avgLen := totalLen / blockLen
				lengthVariance := maxLen - minLen
				isStructurallyRepetitive := avgLen > 0 && lengthVariance*100/avgLen < 30

				if isStructurallyRepetitive {
					for x := i; x < i+3; x++ {
						result = append(result, lines[x])
					}
					result = append(result, fmt.Sprintf("... (省略 %d 行相似内容，前缀 %q) ...", blockLen-5, prefix))
					for x := k - 2; x < k; x++ {
						result = append(result, lines[x])
					}
					i = k
					compressed = true
					continue
				}
			}
		}

		result = append(result, line)
		i++
	}

	if !compressed {
		return s // no compression happened — return original to avoid alloc
	}
	return strings.Join(result, "\n")
}

// extractLinePrefix returns the leading "tag" of a line — the portion
// before the first space or colon, if it looks like a repeated prefix.
// Returns "" if no useful prefix is detected.
func extractLinePrefix(line string) string {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 {
		return ""
	}
	// If the line starts with whitespace, it's likely indented content
	// (not a tag-prefixed line). Skip to avoid false positives on
	// indented code or bullet points.
	if line != "" && (line[0] == ' ' || line[0] == '\t') {
		return ""
	}
	// Common patterns: "PASS ", "FAIL ", "warning: ", "error: ", "ok  "
	for _, delim := range []string{": ", " "} {
		idx := strings.Index(trimmed, delim)
		if idx > 0 && idx <= 20 {
			return trimmed[:idx+len(delim)]
		}
	}
	// Tab delimiter
	if idx := strings.Index(trimmed, "\t"); idx > 0 && idx <= 20 {
		return trimmed[:idx+1]
	}
	return ""
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

// fileDeliveryMessageForDocType generates a user-facing prompt from structured
// document metadata when no explicit message was provided.
func fileDeliveryMessageForDocType(docType, fileName string) string {
	return fileDeliveryMessageForPhaseKind(normalizeWorkflowPhaseKind(docType), fileName)
}

func fileDeliveryMessageForPhaseKind(phase workflowPhaseKind, fileName string) string {
	switch phase {
	case workflowPhaseKind(workflowPhaseRequirements):
		return i18n.T(i18n.MsgFileRequirements, "zh")
	case workflowPhaseKind(workflowPhaseDesign):
		return i18n.T(i18n.MsgFileDesign, "zh")
	case workflowPhaseKind(workflowPhaseTasks):
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

// buildCompactionSummarizerInput constructs a structured 4-section input for
// the LLM summarizer from dropped conversation entries. This is the single
// implementation used by trimHistoryWithSummary's separator construction.
//
// Sections:
//  1. Turn boundaries — user requests and LLM first responses
//  2. Key data — file paths, URLs, data statistics from tool results
//  3. Tool operations — what tools were called with what key arguments
//  4. Final assistant summaries — conclusion messages before each new user turn
func buildCompactionSummarizerInput(entries []agent.ConversationEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder

	// Section 1: Turn boundaries.
	sb.WriteString("## 对话轮次\n\n")
	turnTexts := extractTurnBoundaryTexts(entries, 20)
	for _, text := range turnTexts {
		runes := []rune(text)
		if len(runes) > 500 {
			text = string(runes[:500]) + "..."
		}
		sb.WriteString(text)
		sb.WriteString("\n\n")
	}

	// Section 2: Key data from tool outputs.
	keyData := extractKeyDataFromEntries(entries)
	if len(keyData) > 0 {
		sb.WriteString("## 工具产出的关键数据\n\n")
		for _, kd := range keyData {
			sb.WriteString("- ")
			sb.WriteString(kd)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Section 3: Tool operation summary.
	toolOps := extractToolOperationSummary(entries, 15)
	if len(toolOps) > 0 {
		sb.WriteString("## 执行的工具操作\n\n")
		for _, op := range toolOps {
			sb.WriteString("- ")
			sb.WriteString(op)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// Section 4: Final assistant messages.
	finalTexts := extractFinalAssistantTexts(entries, 5)
	if len(finalTexts) > 0 {
		sb.WriteString("## 任务结果摘要\n\n")
		for _, text := range finalTexts {
			sb.WriteString(text)
			sb.WriteString("\n\n")
		}
	}

	result := sb.String()
	if len([]rune(result)) > 12000 {
		result = string([]rune(result)[:12000]) + "\n...(truncated)"
	}
	return result
}

// stripThinkingTags removes <think>...</think> blocks from LLM output and
// trims any leading whitespace left behind.
func stripThinkingTags(s string) string {
	if !strings.Contains(s, "<think>") {
		return strings.TrimSpace(s)
	}
	cleaned := thinkTagPattern.ReplaceAllString(s, "")
	return strings.TrimSpace(cleaned)
}

func splitThinkingTagsForDisplay(s string) (visible string, reasoning string) {
	if !strings.Contains(strings.ToLower(s), "<think>") {
		return strings.TrimSpace(s), ""
	}
	var parts []string
	cleaned := thinkTagPattern.ReplaceAllStringFunc(s, func(match string) string {
		body := match
		lower := strings.ToLower(body)
		if strings.HasPrefix(lower, "<think>") {
			body = body[len("<think>"):]
			lower = lower[len("<think>"):]
		}
		if end := strings.LastIndex(lower, "</think>"); end >= 0 {
			body = body[:end]
		}
		if trimmed := strings.TrimSpace(body); trimmed != "" {
			parts = append(parts, trimmed)
		}
		return ""
	})
	return strings.TrimSpace(cleaned), strings.Join(parts, "\n")
}

func stripRolePrefixHallucination(s string) string {
	return agent.StripRolePrefixHallucination(s)
}

// ---------------------------------------------------------------------------
// Tool availability hallucination detection
// ---------------------------------------------------------------------------

// toolClaimPatterns extracts tool-name-like identifiers that the LLM claims
// are unavailable. Patterns use a generic [a-z][a-z0-9_]+ capture instead of
// hardcoded tool names — the actual verification is done against the real
// tool list passed to detectToolAvailabilityHallucination.
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

// detectToolAvailabilityHallucination checks if the LLM output claims a tool
// is unavailable, then verifies against the actual tool list sent to the LLM.
// Only returns a correction when the claimed tool IS in the actual list —
// meaning the LLM is lying about its capabilities.
//
// This is mechanism-level: no hardcoded tool name set. Any tool the LLM
// falsely claims is missing will be caught, whether it's bash, ssh, or a
// future tool not yet invented.
//
// actualTools is the tool definitions sent to the LLM in this iteration.
func detectToolAvailabilityHallucination(text string, actualTools []map[string]interface{}) string {
	if text == "" || len(actualTools) == 0 {
		return ""
	}

	// Build set of tool names actually sent to the LLM.
	available := make(map[string]bool, len(actualTools))
	for _, t := range actualTools {
		if name := extractToolName(t); name != "" {
			available[name] = true
		}
	}
	if len(available) == 0 {
		return ""
	}

	// Strip fenced code blocks to avoid false positives.
	stripped := stripCodeBlocks(text)
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

// stripCodeBlocks removes ``` fenced code blocks from text, returning only
// the prose portions for hallucination scanning.
func stripCodeBlocks(s string) string {
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

// ---------------------------------------------------------------------------
// IMMessageHandler
// ---------------------------------------------------------------------------

// truncateToolCallArgsInConversation finds the assistant message containing
// the given tool call ID and truncates its arguments to a short summary.
// This prevents failed tool calls with oversized arguments (e.g. 13K chars
// of write_file content) from bloating the conversation context.
//
// The function walks backward through conversation to find the most recent
// assistant message with matching tool_calls, then replaces the Arguments
// string in-place with a truncated version.
func truncateToolCallArgsInConversation(conversation []interface{}, toolCallID, originalArgs string) {
	const maxArgsSummaryRunes = 200

	// Build a rune-safe truncated summary of the arguments
	summary := originalArgs
	runes := []rune(summary)
	if len(runes) > maxArgsSummaryRunes {
		summary = string(runes[:maxArgsSummaryRunes]) + fmt.Sprintf("... [截断，原始 %d 字符]", len(runes))
	}

	// Walk backward to find the assistant message with this tool call
	for i := len(conversation) - 1; i >= 0; i-- {
		msg, ok := conversation[i].(map[string]interface{})
		if !ok {
			continue
		}
		if msgRole(msg) != "assistant" {
			continue
		}
		toolCalls, ok := msg["tool_calls"]
		if !ok || toolCalls == nil {
			continue
		}

		// tool_calls can be []llm.ToolCall or []interface{}
		switch tcs := toolCalls.(type) {
		case []llm.ToolCall:
			for j := range tcs {
				if tcs[j].ID == toolCallID {
					tcs[j].Function.Arguments = summary
					return
				}
			}
		case []interface{}:
			for _, tc := range tcs {
				if tcMap, ok := tc.(map[string]interface{}); ok {
					if tcMap["id"] == toolCallID {
						if fn, ok := tcMap["function"].(map[string]interface{}); ok {
							fn["arguments"] = summary
						}
						return
					}
				}
			}
		}
	}
}
