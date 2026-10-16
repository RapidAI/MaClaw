package guiapp

// Conversation trimming: token estimation, context window management,
// and conversation history compaction utilities.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
		total += entryTokenCount(e)
	}
	return total
}

// estimateConversationTokens is the sum of the per-message count shared with
// the checkpoint and trimConversation. The first-request gate uses this sum
// to decide whether the latency budget applies. A separate JSON encoding is
// larger once arguments contain quotes, so the gate was opening the 24k
// budget for a transcript the checkpoint still considered inside the
// provider window.
func estimateConversationTokens(msgs []interface{}) int {
	total := 0
	for _, m := range msgs {
		total += estimateSingleMsgTokens(m)
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
		// Arguments can be a whole file. Count them by length; encoding the
		// message to JSON here copies that payload on every fit check.
		if v["tool_calls"] != nil {
			return agent.EstimateMessageTokens(m)
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

// estimateSingleMsgTokens uses the same allocation-free count as the
// checkpoint. A separate JSON pass is larger (quotes, escapes, keys), so a
// transcript the checkpoint left inline was then rewritten with the omission
// placeholder. Multimodal messages still go through that shared count, which
// keeps image blocks at a fixed cost. Unknown shapes fall back to JSON.
func estimateSingleMsgTokens(m interface{}) int {
	switch m.(type) {
	case map[string]interface{}, map[string]string:
		return agent.EstimateMessageTokens(m)
	default:
		data, _ := json.Marshal(m)
		return estimateBytesToTokens(data)
	}
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

	// Fast short-circuit: measure string length, and tool-call arguments by
	// length, so a large payload is not encoded just to see that the
	// transcript fits. Text-only messages under-count, so the short-circuit
	// is trusted only when doubling that estimate stays inside the budget.
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
		dropped := append([]msgGroup(nil), groups[:bestDropCount]...)
		kept := append([]msgGroup(nil), groups[bestDropCount:]...)
		collect := func(gs []msgGroup) []interface{} {
			out := make([]interface{}, 0)
			for _, g := range gs {
				out = append(out, msgs[g.start:g.end]...)
			}
			return out
		}
		assemble := func(placeholder []interface{}, keptGroups []msgGroup) []interface{} {
			result := make([]interface{}, 0, len(systemMsg)+len(placeholder)+8)
			result = append(result, systemMsg...)
			result = append(result, placeholder...)
			for _, g := range keptGroups {
				result = append(result, msgs[g.start:g.end]...)
			}
			return result
		}

		// One summary of the first dropped prefix. A timeout, rejection, or
		// oversized summary falls back to a deterministic handoff. Dropping
		// more groups after that does not call the model again.
		summary := ""
		fileBlock := ""
		if summarizer != nil {
			material, files := liveCompactionMaterial(collect(dropped))
			fileBlock = files
			summaryCh := make(chan string, 1)
			go func() { summaryCh <- summarizer(material) }()
			select {
			case summary = <-summaryCh:
			case <-time.After(15 * time.Second):
				log.Printf("[trim-conversation] summarizer timed out after 15s, using deterministic handoff")
				summary = ""
			}
			if compactionSummaryUnusable(summary, "") {
				summary = ""
			}
		}
		for {
			nextRole := ""
			if len(kept) > 0 {
				nextRole = msgRole(msgs[kept[0].start])
			}
			var placeholder []interface{}
			kind := "static"
			if summary != "" {
				kind = "summary"
				placeholder = handoffTurn("[对话历史摘要]\n"+compactionHandoffContent(summary, fileBlock), nextRole)
			} else if handoff := liveTrimFallbackHandoff(collect(dropped)); handoff != "" {
				kind = "handoff"
				placeholder = handoffTurn(handoff, nextRole)
			} else {
				placeholder = avoidAdjacentUsers(fallbackPlaceholder, nextRole)
			}
			result := assemble(placeholder, kept)
			if estimateConversationTokens(result) <= msgBudget {
				log.Printf("[trim-conversation] dropped_groups=%d placeholder=%s msg_budget=%d",
					len(dropped), kind, msgBudget)
				return result
			}
			if summary != "" {
				summary = ""
				continue
			}
			if len(kept) <= 1 {
				nextRole := ""
				if len(kept) > 0 {
					nextRole = msgRole(msgs[kept[0].start])
				}
				result = assemble(avoidAdjacentUsers(fallbackPlaceholder, nextRole), kept)
				log.Printf("[trim-conversation] dropped_groups=%d placeholder=static msg_budget=%d",
					len(dropped), msgBudget)
				return result
			}
			dropped = append(dropped, kept[0])
			kept = kept[1:]
		}
	}

	// Even keeping only the last group doesn't fit — try secondary truncation
	// of tool results within the last group to squeeze it in. Prefer a handoff
	// of everything before that group when the shortened tail still fits.
	lastG := groups[len(groups)-1]
	nextRole := msgRole(msgs[lastG.start])
	tryFit := func(placeholder []interface{}) ([]interface{}, bool) {
		result := truncateLastGroup(msgs, lastG.start, lastG.end, systemMsg, placeholder)
		if estimateConversationTokens(result) <= msgBudget {
			return result, true
		}
		result = truncateAssistantContent(result, msgBudget)
		if estimateConversationTokens(result) <= msgBudget {
			return result, true
		}
		return nil, false
	}
	if lastG.start > 1 {
		if handoff := liveTrimFallbackHandoff(msgs[1:lastG.start]); handoff != "" {
			if result, ok := tryFit(handoffTurn(handoff, nextRole)); ok {
				log.Printf("[trim-conversation] mode=last_group_handoff msg_budget=%d msgs=%d", msgBudget, len(msgs))
				return result
			}
		}
	}
	if result, ok := tryFit(avoidAdjacentUsers(fallbackPlaceholder, nextRole)); ok {
		log.Printf("[trim-conversation] mode=last_group_truncate msg_budget=%d msgs=%d", msgBudget, len(msgs))
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
		placeholder := fallbackPlaceholder
		if i > 1 {
			if handoff := liveTrimFallbackHandoff(msgs[1:i]); handoff != "" {
				candidate := handoffTurn(handoff, "user")
				trial := make([]interface{}, 0, len(systemMsg)+len(candidate)+len(msgs)-i)
				trial = append(trial, systemMsg...)
				trial = append(trial, candidate...)
				trial = append(trial, msgs[i:]...)
				if estimateConversationTokens(trial) <= msgBudget {
					log.Printf("[trim-conversation] mode=tail_keep_handoff from_idx=%d msg_budget=%d", i, msgBudget)
					return trial
				}
			}
		}
		result := make([]interface{}, 0, len(systemMsg)+len(placeholder)+1+len(msgs)-i)
		result = append(result, systemMsg...)
		result = append(result, avoidAdjacentUsers(placeholder, "user")...)
		result = append(result, msgs[i:]...)
		log.Printf("[trim-conversation] mode=tail_keep_over_budget from_idx=%d msg_budget=%d", i, msgBudget)
		return result
	}

	// A malformed history without a user turn has no request to protect. Keep
	// the provider-valid minimal fallback rather than risking an orphaned tool
	// result.
	log.Printf("[trim-conversation] mode=minimal_system_only msg_budget=%d msgs=%d", msgBudget, len(msgs))
	minimal := make([]interface{}, 0, len(systemMsg)+len(fallbackPlaceholder))
	minimal = append(minimal, systemMsg...)
	return append(minimal, fallbackPlaceholder...)
}

// liveTrimFallbackHandoff is the no-model note used when live trim cannot
// get a summary. It keeps the latest user requests, file paths, and any
// previous summary, and stays bounded so it can replace the one-line omission.
func liveTrimFallbackHandoff(msgs []interface{}) string {
	entries := messagesToEntries(msgs)
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("[上下文恢复] 更早的对话因长度限制被压缩。请基于下面的要点和最近的原文继续，不要重复已完成的工作。")
	prev := previousCompactionSummary(entries)
	var users []string
	seenUser := map[string]bool{}
	addUser := func(text string) {
		text = strings.Join(strings.Fields(text), " ")
		if text == "" || seenUser[text] {
			return
		}
		seenUser[text] = true
		users = append(users, trimOneLine(text, 180))
	}
	for _, entry := range entries {
		if entry.Role != "user" {
			continue
		}
		text, ok := entry.Content.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if strings.HasPrefix(text, "[上下文恢复]") {
			if prev == "" {
				if parsed := handoffSectionBullets(text, "先前摘要:"); len(parsed) > 0 {
					prev = parsed[len(parsed)-1]
				}
			}
			for _, item := range handoffSectionBullets(text, "用户要求:") {
				addUser(item)
			}
			continue
		}
		if corelib.IsSyntheticUserContent(text) {
			continue
		}
		addUser(text)
	}
	if len(users) > 6 {
		users = users[len(users)-6:]
	}
	if prev != "" {
		b.WriteString("\n\n先前摘要:\n- ")
		b.WriteString(trimOneLine(prev, 600))
		b.WriteByte('\n')
	}
	if len(users) > 0 {
		b.WriteString("\n用户要求:\n")
		for _, text := range users {
			b.WriteString("- ")
			b.WriteString(text)
			b.WriteByte('\n')
		}
	}
	readFiles, modifiedFiles := compactionFileLists(entries)
	if len(readFiles) > 20 {
		readFiles = readFiles[len(readFiles)-20:]
	}
	if len(modifiedFiles) > 20 {
		modifiedFiles = modifiedFiles[len(modifiedFiles)-20:]
	}
	if block := formatCompactionFileLists(readFiles, modifiedFiles); block != "" {
		b.WriteByte('\n')
		b.WriteString(block)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func handoffTurn(text, nextRole string) []interface{} {
	return avoidAdjacentUsers([]interface{}{
		map[string]string{"role": "user", "content": text},
	}, nextRole)
}

// avoidAdjacentUsers inserts a short assistant ack when a user placeholder
// would otherwise sit directly against another user message.
func avoidAdjacentUsers(placeholder []interface{}, nextRole string) []interface{} {
	if len(placeholder) == 0 || nextRole == "assistant" || nextRole == "tool" {
		return placeholder
	}
	if msgRole(placeholder[len(placeholder)-1]) != "user" {
		return placeholder
	}
	out := append([]interface{}{}, placeholder...)
	out = append(out, map[string]string{
		"role":              "assistant",
		"content":           "好的，我已了解之前被省略的工作。",
		"reasoning_content": "",
	})
	return out
}

func trimOneLine(text string, maxRunes int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if maxRunes > 0 && len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return text
}

func handoffSectionBullets(text, header string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if trim == header {
			in = true
			continue
		}
		if !in {
			continue
		}
		if trim == "" {
			continue
		}
		if !strings.HasPrefix(trim, "- ") {
			break
		}
		item := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
		if item != "" {
			out = append(out, item)
		}
	}
	return out
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
						truncated := agent.TruncateToolContentPreservingHandle(content, headRunes, tailRunes)
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

// compactionPlaceholder is the synchronous checkpoint text. The async summary
// replaces it when the model returns a complete handoff.
const compactionPlaceholder = "[...中间的工具调用和执行细节已省略...]"

// compactionSummaryRuneCap is the largest summary that may be stored.
// A longer result was cut off and must not become the checkpoint.
const compactionSummaryRuneCap = 5000

const compactionUpdatePrompt = `你正在更新一份已有的上下文交接摘要。规则:
- 保留上一份摘要中的目标、约束、关键决定和已完成项
- 只并入新进展；已完成的事项从待完成移到当前进度
- 保留精确的文件路径、函数名和报错
- 仍使用这四个部分: 当前进度、重要上下文、待完成工作、关键数据

上一份摘要:
%s

新增的对话材料:
%s
`

const (
	compactionPreviousOpen  = "<previous-summary>\n"
	compactionPreviousClose = "\n</previous-summary>"
	compactionDroppedOpen   = "<dropped>\n"
	compactionDroppedClose  = "\n</dropped>"
	compactionReadOpen      = "<read-files>\n"
	compactionReadClose     = "\n</read-files>"
	compactionModifiedOpen  = "<modified-files>\n"
	compactionModifiedClose = "\n</modified-files>"
)

// compactionSummaryUnusable reports whether a summarizer result must be discarded.
// Incomplete, empty, or tool-calling output is not a checkpoint.
func compactionSummaryUnusable(content, finishReason string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(finishReason)) {
	case "length", "max_tokens", "error", "tool_calls", "tool_use":
		return true
	}
	if strings.Contains(content, `"tool_calls"`) || strings.Contains(content, "<tool_call") {
		return true
	}
	if len([]rune(content)) > compactionSummaryRuneCap {
		return true
	}
	return false
}

func compactionPromptFor(material string) string {
	prev, dropped := splitCompactionSource(material)
	if prev == "" {
		body := material
		if dropped != "" {
			body = dropped
		}
		return compactionHandoffPrompt + body
	}
	body := dropped
	if body == "" {
		body = material
	}
	return fmt.Sprintf(compactionUpdatePrompt, prev, body)
}

func splitCompactionSource(material string) (prev, dropped string) {
	prev = firstTaggedSection(material, compactionPreviousOpen, compactionPreviousClose)
	dropped = firstTaggedSection(material, compactionDroppedOpen, compactionDroppedClose)
	return prev, dropped
}

func firstTaggedSection(s, open, close string) string {
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, close)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func buildCompactionSource(previous, dropped string) string {
	var b strings.Builder
	if strings.TrimSpace(previous) != "" {
		b.WriteString(compactionPreviousOpen)
		b.WriteString(strings.TrimSpace(previous))
		b.WriteString(compactionPreviousClose)
		b.WriteString("\n")
	}
	if strings.TrimSpace(dropped) != "" {
		b.WriteString(compactionDroppedOpen)
		b.WriteString(strings.TrimSpace(dropped))
		b.WriteString(compactionDroppedClose)
	}
	return b.String()
}

func compactionPlaceholderContent(fileBlock string) string {
	if strings.TrimSpace(fileBlock) == "" {
		return compactionPlaceholder
	}
	return compactionPlaceholder + "\n\n" + fileBlock
}

func compactionHandoffContent(summary, fileBlock string) string {
	body := compactionRecoveryPrefix + strings.TrimSpace(summary)
	if strings.TrimSpace(fileBlock) != "" {
		body += "\n\n" + fileBlock
	}
	return body
}

func newCompactionID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("cmp-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func previousCompactionSummary(entries []agent.ConversationEntry) string {
	finished := ""
	pending := ""
	for _, entry := range entries {
		if body := compactionSummaryBody(entry); body != "" {
			// A later finished summary already covers earlier pending material.
			finished = body
			pending = ""
			continue
		}
		// The async summary has not landed. Keep the pending source so a
		// second compaction does not discard the conversation it described.
		if next := pendingCompactionMaterial(entry.CompactionSource); next != "" {
			pending = next
		}
	}
	switch {
	case finished != "" && pending != "":
		if strings.Contains(pending, finished) {
			return pending
		}
		return capCompactionText(finished+"\n\n"+pending, 12000)
	case finished != "":
		return finished
	default:
		return pending
	}
}

func pendingCompactionMaterial(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}
	prev, dropped := splitCompactionSource(source)
	var b strings.Builder
	if prev != "" {
		b.WriteString(prev)
	}
	if dropped != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(dropped)
	}
	if b.Len() == 0 {
		b.WriteString(source)
	}
	return capCompactionText(b.String(), 12000)
}

func capCompactionText(s string, maxRunes int) string {
	runes := []rune(s)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return s
	}
	if maxRunes < 32 {
		return string(runes[:maxRunes])
	}
	head := maxRunes * 2 / 3
	tail := maxRunes - head - 1
	if tail < 1 {
		tail = 1
		head = maxRunes - tail - 1
	}
	return string(runes[:head]) + "\n…\n" + string(runes[len(runes)-tail:])
}

func compactionSummaryBody(entry agent.ConversationEntry) string {
	text, ok := entry.Content.(string)
	if !ok {
		return ""
	}
	idx := strings.Index(text, compactionRecoveryPrefix)
	if idx < 0 {
		return ""
	}
	body := strings.TrimSpace(text[idx+len(compactionRecoveryPrefix):])
	if cut := strings.Index(body, compactionReadOpen); cut >= 0 {
		body = strings.TrimSpace(body[:cut])
	}
	return body
}

func formatCompactionFileLists(readFiles, modifiedFiles []string) string {
	readFiles = dedupePaths(readFiles)
	modifiedFiles = dedupePaths(modifiedFiles)
	if len(readFiles) == 0 && len(modifiedFiles) == 0 {
		return ""
	}
	var b strings.Builder
	if len(readFiles) > 0 {
		b.WriteString(compactionReadOpen)
		for _, path := range readFiles {
			b.WriteString(path)
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimPrefix(compactionReadClose, "\n"))
	}
	if len(modifiedFiles) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(compactionModifiedOpen)
		for _, path := range modifiedFiles {
			b.WriteString(path)
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimPrefix(compactionModifiedClose, "\n"))
	}
	return strings.TrimSpace(b.String())
}

func compactionFileLists(entries []agent.ConversationEntry) (readFiles, modifiedFiles []string) {
	for _, entry := range entries {
		text, _ := entry.Content.(string)
		readFiles = append(readFiles, taggedPathLines(text, compactionReadOpen, compactionReadClose)...)
		modifiedFiles = append(modifiedFiles, taggedPathLines(text, compactionModifiedOpen, compactionModifiedClose)...)
		// Recovery handoffs list paths as bullets instead of read/modified tags.
		readFiles = append(readFiles, handoffSectionBullets(text, "涉及文件:")...)
		for _, call := range assistantToolCalls(entry) {
			switch classifyAgentToolKind(call.name) {
			case agentToolKindReadFile:
				if call.path != "" {
					readFiles = append(readFiles, call.path)
				}
			case agentToolKindWriteFile, agentToolKindEditFile:
				if call.path != "" {
					modifiedFiles = append(modifiedFiles, call.path)
				}
			}
		}
	}
	return dedupePaths(readFiles), dedupePaths(modifiedFiles)
}

type namedToolCall struct {
	id   string
	name string
	path string
}

func assistantToolCalls(entry agent.ConversationEntry) []namedToolCall {
	if entry.Role != "assistant" || entry.ToolCalls == nil {
		return nil
	}
	data, err := json.Marshal(entry.ToolCalls)
	if err != nil {
		return nil
	}
	var calls []map[string]interface{}
	if json.Unmarshal(data, &calls) != nil {
		return nil
	}
	var out []namedToolCall
	for _, call := range calls {
		id, _ := call["id"].(string)
		name, _ := call["name"].(string)
		args := toolArgumentsJSON(call["arguments"])
		if fn, ok := call["function"].(map[string]interface{}); ok {
			if fnName, _ := fn["name"].(string); fnName != "" {
				name = fnName
			}
			if fnArgs := toolArgumentsJSON(fn["arguments"]); fnArgs != "" {
				args = fnArgs
			}
		}
		out = append(out, namedToolCall{id: id, name: name, path: extractKeyToolArg(name, args)})
	}
	return out
}

func toolArgumentsJSON(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		text := strings.TrimSpace(string(data))
		if text == "null" || text == "" {
			return ""
		}
		return text
	}
}

func taggedPathLines(text, open, close string) []string {
	block := firstTaggedSection(text, open, close)
	if block == "" {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

func dedupePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// makeSummarizer returns a summarizer callback that uses doSimpleLLMRequest
// to condense dropped conversation history into a structured handoff summary.
//
// The prompt uses the "Context Checkpoint Compaction" pattern from Codex CLI:
// instead of generic "please summarize", it asks for a structured handoff
// document with 4 sections (progress, context, TODOs, critical data).
func makeSummarizer(cfg corelib.MaclawLLMConfig, httpClient *http.Client) func(string) string {
	return func(text string) string {
		return summarizeCompactionMaterial(cfg, httpClient, text)
	}
}

// summarizeCompactionMaterial asks for a handoff summary. When material contains
// a previous summary, the prompt updates that checkpoint instead of rewriting it.
// Incomplete model output is discarded.
func summarizeCompactionMaterial(cfg corelib.MaclawLLMConfig, httpClient *http.Client, material string) string {
	if httpClient == nil || strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return ""
	}
	msgs := []interface{}{
		map[string]string{"role": "user", "content": compactionPromptFor(material)},
	}
	ctx := llm.WithRequestTrace(context.Background(), llm.RequestTrace{Caller: "conversation-trim-summary"})
	result, err := doSimpleLLMRequest(ctx, attachLightweightHubHint(cfg, llm.TaskSummary), msgs, httpClient, 30*time.Second)
	if err != nil || result == nil || compactionSummaryUnusable(result.Content, result.FinishReason) {
		return ""
	}
	return strings.TrimSpace(result.Content)
}

// guardedCompactionSummarizer wraps makeSummarizer with the nil/unconfigured
// guards every compaction caller needs: no HTTP client or no LLM endpoint
// configured (tests, standalone handlers) → nil, so trimConversation keeps its
// static-placeholder fallback. trimConversation also guards summarizer calls
// with a 15s watchdog.
func guardedCompactionSummarizer(cfg corelib.MaclawLLMConfig, httpClient *http.Client) func(string) string {
	if httpClient == nil || strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	return makeSummarizer(cfg, httpClient)
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

	// First pass: compute recent window.
	// When triggered by entry count: keep the last `limit` entries.
	// When triggered by token overflow only: scan backwards within 70% of
	// maxTokens. finalizeRecentCut then tightens either path to ~30% and
	// splits an oversized tool group instead of dropping it whole.
	var recentStart int
	if entryOverLimit {
		recentStart = len(entries) - limit
	} else {
		recentStart = recentStartByTokenBudget(entries, maxTokens*7/10)
	}
	if recentStart < 0 {
		recentStart = 0
	}
	entries, recentStart = finalizeRecentCut(entries, recentStart, maxTokens)
	tier1Indices := extractTurnBoundaryIndices(entries, maxTier1)

	// Count tier-1 entries outside the recent window.
	outsideTier1 := 0
	for _, idx := range tier1Indices {
		if idx < recentStart {
			outsideTier1++
		}
	}

	// If all tier-1 entries are inside the recent window, simple FIFO.
	// A token budget still needs a checkpoint for the entries it drops.
	if outsideTier1 == 0 && maxTokens <= 0 {
		trimmed := groupAlignedSlice(entries, recentStart)
		return trimmed
	}

	// Second pass: shrink recent window to make room for outside tier-1.
	if outsideTier1 > 0 {
		recentCount := limit - outsideTier1
		if recentCount < limit/2 {
			recentCount = limit / 2
		}
		recentStart = len(entries) - recentCount
		if recentStart < 0 {
			recentStart = 0
		}
		entries, recentStart = finalizeRecentCut(entries, recentStart, maxTokens)
		tier1Indices = extractTurnBoundaryIndices(entries, maxTier1)
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
	if len(outsideSet) == 0 && maxTokens <= 0 {
		trimmed := groupAlignedSlice(entries, recentStart)
		return trimmed
	}
	if recentStart == 0 {
		return entries
	}
	recentCount := len(entries) - recentStart

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
	readFiles, modifiedFiles := compactionFileLists(entries)
	fileBlock := formatCompactionFileLists(readFiles, modifiedFiles)
	separator := compactionPlaceholderContent(fileBlock)
	droppedEntries := make([]agent.ConversationEntry, 0, recentStart)
	for i := 0; i < recentStart; i++ {
		if outsideSet[i] {
			continue // tier-1, already preserved
		}
		// The handoff payload is merged via previousCompactionSummary.
		// Leaving the expanded text in the dropped section repeats it.
		if isCompactionHandoffEntry(entries[i]) {
			continue
		}
		droppedEntries = append(droppedEntries, entries[i])
	}
	structuredInput := buildCompactionSummarizerInput(droppedEntries)
	// Only material that is actually leaving the transcript. A handoff that
	// tier-1 kept is already visible, and its source must not be copied again.
	var carried []agent.ConversationEntry
	for i := 0; i < recentStart; i++ {
		if !outsideSet[i] {
			carried = append(carried, entries[i])
		}
	}
	previous := previousCompactionSummary(carried)
	compactionSource := buildCompactionSource(previous, structuredInput)
	compactionID := ""
	if compactionSource != "" {
		compactionID = newCompactionID()
	}
	if summarizer != nil && compactionSource != "" {
		if summary := summarizer(compactionSource); summary != "" && !compactionSummaryUnusable(summary, "") {
			separator = compactionHandoffContent(summary, fileBlock)
			compactionSource = ""
			compactionID = ""
		}
	}

	result = append(result, agent.ConversationEntry{
		Role:             "system",
		Content:          separator,
		CompactionID:     compactionID,
		CompactionSource: compactionSource,
		CompactionFiles:  fileBlock,
	})
	// Append the recent window, aligned to group boundaries so we never
	// start with orphaned tool messages from a split group.
	result = append(result, groupAlignedSlice(entries, recentStart)...)

	return result
}

func entryTokenCount(entry agent.ConversationEntry) int {
	// CompactionSource is summarizer input, not model context. Counting it
	// makes a pending checkpoint look over budget and trims real turns.
	data, _ := json.Marshal(entry.ToMessage())
	return estimateBytesToTokens(data)
}

func recentStartByTokenBudget(entries []agent.ConversationEntry, keepBudget int) int {
	if keepBudget <= 0 || len(entries) == 0 {
		return 0
	}
	running := 0
	start := 0
	for i := len(entries) - 1; i >= 0; i-- {
		tokens := entryTokenCount(entries[i])
		if running+tokens > keepBudget {
			return i + 1
		}
		running += tokens
	}
	return start
}

// finalizeRecentCut keeps about 30% of maxTokens when a token budget is set,
// and splits a boundary tool group that does not fit. Without a token budget
// it only moves the cut forward to the next group boundary.
func finalizeRecentCut(entries []agent.ConversationEntry, recentStart, maxTokens int) ([]agent.ConversationEntry, int) {
	if recentStart < 0 {
		recentStart = 0
	}
	if recentStart > len(entries) {
		recentStart = len(entries)
	}
	if maxTokens <= 0 {
		return entries, alignRecentStartDropGroup(entries, recentStart)
	}
	keepBudget := maxTokens * 3 / 10
	if keepBudget < 400 {
		keepBudget = 400
	}
	tokenStart := recentStartByTokenBudget(entries, keepBudget)
	if tokenStart > recentStart {
		recentStart = tokenStart
	}
	return splitOrDropBoundaryGroup(entries, recentStart, keepBudget)
}

func alignRecentStartDropGroup(entries []agent.ConversationEntry, recentStart int) int {
	groups := agent.BuildEntryGroups(entries)
	g := agent.GroupContaining(groups, recentStart)
	if g != nil && recentStart > g.Start && recentStart < g.End {
		return g.End
	}
	return recentStart
}

func splitOrDropBoundaryGroup(entries []agent.ConversationEntry, recentStart, keepBudget int) ([]agent.ConversationEntry, int) {
	groups := agent.BuildEntryGroups(entries)
	g := agent.GroupContaining(groups, recentStart)
	if g == nil {
		return entries, recentStart
	}
	needsSplit := (recentStart > g.Start && recentStart < g.End) ||
		(recentStart == g.Start && groupTokenCount(entries, *g) > keepBudget)
	if !needsSplit {
		return entries, recentStart
	}
	tailTokens := 0
	for i := g.End; i < len(entries); i++ {
		tailTokens += entryTokenCount(entries[i])
	}
	budgetForGroup := keepBudget - tailTokens
	if budgetForGroup < 1 {
		return entries, g.End
	}
	return splitGroupSuffix(entries, *g, budgetForGroup)
}

func groupTokenCount(entries []agent.ConversationEntry, g agent.EntryGroup) int {
	total := 0
	for i := g.Start; i < g.End && i < len(entries); i++ {
		total += entryTokenCount(entries[i])
	}
	return total
}

// splitGroupSuffix keeps the assistant tool call plus the trailing tool
// results that fit in keepBudget. Dropped tool results stay before the cut
// so the checkpoint can summarize them. If the suffix cannot keep a paired
// tool result, the whole group is dropped from the kept window.
func splitGroupSuffix(entries []agent.ConversationEntry, g agent.EntryGroup, keepBudget int) ([]agent.ConversationEntry, int) {
	if g.Start < 0 || g.End > len(entries) || g.End-g.Start < 2 || entries[g.Start].Role != "assistant" {
		return entries, g.End
	}
	assistantTokens := entryTokenCount(entries[g.Start])
	if assistantTokens >= keepBudget {
		return entries, g.End
	}
	remaining := keepBudget - assistantTokens
	firstKept := g.End
	used := 0
	for i := g.End - 1; i > g.Start; i-- {
		tokens := entryTokenCount(entries[i])
		if used+tokens > remaining {
			break
		}
		used += tokens
		firstKept = i
	}
	if firstKept >= g.End {
		last := entries[g.End-1]
		maxRunes := remaining * 2
		if maxRunes < 200 {
			maxRunes = 200
		}
		last.Content = truncateEntryContent(last.Content, maxRunes)
		if strings.TrimSpace(last.ToolCallID) == "" {
			return entries, g.End
		}
		filtered, ok := filterAssistantToKeptTools(entries[g.Start], []agent.ConversationEntry{last})
		if !ok {
			return entries, g.End
		}
		droppedTools := append([]agent.ConversationEntry(nil), entries[g.Start+1:g.End-1]...)
		out := make([]agent.ConversationEntry, 0, len(entries))
		out = append(out, entries[:g.Start]...)
		out = append(out, droppedTools...)
		newStart := len(out)
		out = append(out, filtered)
		out = append(out, last)
		out = append(out, entries[g.End:]...)
		return out, newStart
	}
	keptTools := entries[firstKept:g.End]
	filtered, ok := filterAssistantToKeptTools(entries[g.Start], keptTools)
	if !ok {
		return entries, g.End
	}
	droppedTools := append([]agent.ConversationEntry(nil), entries[g.Start+1:firstKept]...)
	out := make([]agent.ConversationEntry, 0, len(entries))
	out = append(out, entries[:g.Start]...)
	out = append(out, droppedTools...)
	newStart := len(out)
	out = append(out, filtered)
	out = append(out, keptTools...)
	out = append(out, entries[g.End:]...)
	return out, newStart
}

func truncateEntryContent(content interface{}, maxRunes int) interface{} {
	text, ok := content.(string)
	if !ok || maxRunes <= 0 {
		return content
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	head := maxRunes * 2 / 3
	tail := maxRunes - head
	if tail < 1 {
		return string(runes[:maxRunes])
	}
	return string(runes[:head]) + "\n…(截断)…\n" + string(runes[len(runes)-tail:])
}

func messagesToEntries(msgs []interface{}) []agent.ConversationEntry {
	out := make([]agent.ConversationEntry, 0, len(msgs))
	for _, msg := range msgs {
		switch m := msg.(type) {
		case agent.ConversationEntry:
			out = append(out, m)
		case map[string]string:
			out = append(out, agent.ConversationEntry{Role: m["role"], Content: m["content"]})
		case map[string]interface{}:
			entry := agent.ConversationEntry{}
			if role, ok := m["role"].(string); ok {
				entry.Role = role
			}
			if content, ok := m["content"].(string); ok {
				entry.Content = content
			}
			if id, ok := m["tool_call_id"].(string); ok {
				entry.ToolCallID = id
			}
			if name, ok := m["tool_name"].(string); ok {
				entry.ToolName = name
			}
			if calls, ok := m["tool_calls"]; ok {
				entry.ToolCalls = calls
			}
			out = append(out, entry)
		}
	}
	return out
}

// liveCompactionMaterial turns dropped request messages into an update-aware
// summarizer payload plus the cumulative file list to append after the summary.
func liveCompactionMaterial(msgs []interface{}) (string, string) {
	entries := messagesToEntries(msgs)
	readFiles, modifiedFiles := compactionFileLists(entries)
	fileBlock := formatCompactionFileLists(readFiles, modifiedFiles)
	structured := buildCompactionSummarizerInput(entries)
	material := buildCompactionSource(previousCompactionSummary(entries), structured)
	if strings.TrimSpace(material) == "" {
		material = boundedMessageText(msgs, 12000)
	}
	return material, fileBlock
}

func boundedMessageText(msgs []interface{}, maxRunes int) string {
	var b strings.Builder
	for _, msg := range msgs {
		entries := messagesToEntries([]interface{}{msg})
		if len(entries) == 0 {
			continue
		}
		text, _ := entries[0].Content.(string)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(entries[0].Role)
		b.WriteString(": ")
		b.WriteString(text)
		if len([]rune(b.String())) >= maxRunes {
			break
		}
	}
	runes := []rune(b.String())
	if len(runes) > maxRunes {
		return string(runes[:maxRunes])
	}
	return b.String()
}

func filterAssistantToKeptTools(assistant agent.ConversationEntry, keptTools []agent.ConversationEntry) (agent.ConversationEntry, bool) {
	keep := make(map[string]bool, len(keptTools))
	for _, toolEntry := range keptTools {
		if toolEntry.ToolCallID == "" {
			return assistant, false
		}
		keep[toolEntry.ToolCallID] = true
	}
	switch calls := assistant.ToolCalls.(type) {
	case []llm.ToolCall:
		filtered := make([]llm.ToolCall, 0, len(keep))
		for _, call := range calls {
			if keep[call.ID] {
				filtered = append(filtered, call)
			}
		}
		if len(filtered) == 0 {
			return assistant, false
		}
		assistant.ToolCalls = filtered
		return assistant, true
	case []interface{}:
		filtered := make([]interface{}, 0, len(keep))
		for _, item := range calls {
			tc, ok := item.(map[string]interface{})
			if !ok {
				return assistant, false
			}
			id, _ := tc["id"].(string)
			if keep[id] {
				filtered = append(filtered, item)
			}
		}
		if len(filtered) == 0 {
			return assistant, false
		}
		assistant.ToolCalls = filtered
		return assistant, true
	default:
		filtered, ok := filterToolCallMaps(calls, keep)
		if !ok {
			return assistant, false
		}
		assistant.ToolCalls = filtered
		return assistant, true
	}
}

// filterToolCallMaps keeps tool calls whose id is in keep. History reloaded
// from JSON or built as a map slice is not []llm.ToolCall; treating that as
// "cannot split" dropped the whole turn, including a tool result that fit.
func filterToolCallMaps(calls interface{}, keep map[string]bool) ([]map[string]interface{}, bool) {
	data, err := json.Marshal(calls)
	if err != nil {
		return nil, false
	}
	var items []map[string]interface{}
	if json.Unmarshal(data, &items) != nil || len(items) == 0 {
		return nil, false
	}
	filtered := make([]map[string]interface{}, 0, len(keep))
	for _, item := range items {
		id, _ := item["id"].(string)
		if keep[id] {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		return nil, false
	}
	return filtered, true
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
		// A checkpoint handoff is not a user request. Its payload is carried
		// by CompactionSource; keeping the visible text here repeats it.
		if isCompactionHandoffEntry(entries[i]) {
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
	if isCompactionHandoffEntry(e) {
		return true
	}
	text, ok := e.Content.(string)
	if !ok || text == "" {
		return false
	}
	return corelib.IsSyntheticUserContent(text)
}

// isCompactionHandoffEntry is a checkpoint or summary placeholder. After the
// checkpoint is made provider-valid, its role is user and the pending source
// has been copied into the text, but it is still not something the user typed.
func isCompactionHandoffEntry(e agent.ConversationEntry) bool {
	if e.CompactionID != "" || strings.TrimSpace(e.CompactionSource) != "" {
		return true
	}
	text, ok := e.Content.(string)
	if !ok {
		return false
	}
	return strings.Contains(text, compactionPlaceholder) || strings.Contains(text, compactionRecoveryPrefix)
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
