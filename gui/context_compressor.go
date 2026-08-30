package main

// Context Compression for the agent loop conversation.
//
// Inspired by Hermes' two-layer compression strategy:
//   Layer 1 (50% context usage): pre-compression — summarize old messages,
//     keep recent 20 messages + system prompt.
//   Layer 2 (85% context usage): aggressive compression — keep only recent
//     8 messages + system prompt + minimal summary.
//
// Before any compression, memory.Store.Flush() is called to prevent data loss.
// Each compression event creates a SessionLineage record for traceability.

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// ---------------------------------------------------------------------------
// Compression thresholds (percentage of effective context window)
// ---------------------------------------------------------------------------

const (
	preCompressThresholdPct        = 50   // trigger pre-compression at 50%
	aggressiveCompressThresholdPct = 85   // trigger aggressive compression at 85%
	preCompressKeepMessages        = 20   // messages to keep in pre-compression
	aggressiveKeepMessages         = 8    // messages to keep in aggressive compression
	maxSummaryChars                = 5000 // cap summary text length
)

// ---------------------------------------------------------------------------
// SessionLineage — tracks compression events for traceability
// ---------------------------------------------------------------------------

// SessionLineage records a single compression event so child sessions can
// trace back to the parent context that was compressed away.
type SessionLineage struct {
	ParentSessionID    string    `json:"parent_session_id"`
	CompressedAt       time.Time `json:"compressed_at"`
	Layer              int       `json:"layer"` // 1 = pre-compress, 2 = aggressive
	DroppedMsgCount    int       `json:"dropped_msg_count"`
	SummaryTokens      int       `json:"summary_tokens"`
	ContextUsageBefore float64   `json:"context_usage_before"` // 0.0–1.0
	ContextUsageAfter  float64   `json:"context_usage_after"`
}

// ---------------------------------------------------------------------------
// ContextCompressor
// ---------------------------------------------------------------------------

// ContextCompressor implements Hermes-style two-layer conversation context
// compression for the maclaw agent loop.
type ContextCompressor struct {
	// contextLimit is the effective context window in tokens (after 20% output reserve).
	contextLimit int
	// toolsTokens is the estimated token budget consumed by tool definitions.
	toolsTokens int
	// summarizer produces a short summary of dropped messages via LLM.
	summarizer func(string) string
	// memoryFlusher flushes memory to disk before compression. Nil-safe.
	memoryFlusher func() error
	// sessionID for lineage tracking.
	sessionID string
	// lineage records compression events during this session.
	lineage []SessionLineage
}

// NewContextCompressor creates a ContextCompressor.
//   - contextLimit: effective context tokens (from cfg.EffectiveContextTokens())
//   - toolsTokens: estimated tokens consumed by tool definitions
//   - summarizer: LLM summarizer callback (from makeSummarizer); nil = no summary
//   - memoryFlusher: calls memory.Store.Flush(); nil = skip
//   - sessionID: loop context ID for lineage
func NewContextCompressor(contextLimit, toolsTokens int, summarizer func(string) string, memoryFlusher func() error, sessionID string) *ContextCompressor {
	return &ContextCompressor{
		contextLimit:  contextLimit,
		toolsTokens:   toolsTokens,
		summarizer:    summarizer,
		memoryFlusher: memoryFlusher,
		sessionID:     sessionID,
	}
}

// CompressLevel indicates which compression layer was applied.
type CompressLevel int

const (
	CompressNone        CompressLevel = 0
	CompressPreCompress CompressLevel = 1
	CompressAggressive  CompressLevel = 2
)

// Compress applies the appropriate compression layer to the conversation.
// It returns the (possibly compressed) conversation and the compression level.
//
// The conversation slice is expected to have msgs[0] as the system prompt.
// This function replaces trimConversation as the primary context management
// entry point in the agent loop.
func (cc *ContextCompressor) Compress(msgs []interface{}) ([]interface{}, CompressLevel) {
	if len(msgs) <= 3 {
		return msgs, CompressNone
	}

	msgBudget := cc.contextLimit - cc.toolsTokens
	if msgBudget < 4000 {
		msgBudget = 4000
	}

	// Fast short-circuit using corelib/llm.EstimateTokens (content-only, ~len/4).
	// This estimator undercounts (ignores tool_calls, JSON overhead), so we
	// apply a 2x safety margin: only skip compression when even doubling the
	// quick estimate stays below the pre-compress threshold.
	quickTokens := llmEstimateTokensAdapter(msgs)
	if quickTokens*2 < msgBudget*preCompressThresholdPct/100 {
		return msgs, CompressNone
	}

	// Detailed estimation for ratio-based layer selection.
	// estimateConversationTokens handles multimodal, tool_calls, and JSON overhead.
	currentTokens := estimateConversationTokens(msgs)
	usageRatio := float64(currentTokens) / float64(msgBudget)

	if usageRatio*100 < float64(preCompressThresholdPct) {
		return msgs, CompressNone
	}

	// Flush memory before any compression to prevent data loss.
	cc.flushMemory()

	// 85%+ — aggressive compression (Layer 2).
	if usageRatio*100 >= float64(aggressiveCompressThresholdPct) {
		result := cc.compressLayer2(msgs, msgBudget, usageRatio)
		return result, CompressAggressive
	}

	// 50–85% — pre-compression (Layer 1).
	result := cc.compressLayer1(msgs, msgBudget, usageRatio)
	return result, CompressPreCompress
}

// Lineage returns all compression events recorded during this session.
func (cc *ContextCompressor) Lineage() []SessionLineage {
	return cc.lineage
}

// ---------------------------------------------------------------------------
// Layer 1: Pre-compression (50% threshold)
// ---------------------------------------------------------------------------

// compressLayer1 keeps the system prompt + recent N messages, summarizes
// the rest via LLM.
func (cc *ContextCompressor) compressLayer1(msgs []interface{}, budget int, usageBefore float64) []interface{} {
	systemMsg := msgs[:1]
	tail := msgs[1:]

	// Build logical groups (respecting tool-call pairs).
	groups := buildMsgGroups(tail)

	keepCount := preCompressKeepMessages
	if keepCount > len(groups) {
		keepCount = len(groups)
	}

	// Find the split point: drop from front, keep from back.
	dropGroups := len(groups) - keepCount
	if dropGroups <= 0 {
		// Nothing to drop — fall through to legacy trimConversation.
		return trimConversation(msgs, cc.contextLimit, cc.toolsTokens, cc.summarizer)
	}

	dropped := groups[:dropGroups]
	kept := groups[dropGroups:]

	// Build summary of dropped messages.
	placeholder := cc.buildSummaryPlaceholder(msgs, dropped, 1)

	result := make([]interface{}, 0, 1+len(placeholder)+countGroupMsgs(kept))
	result = append(result, systemMsg...)
	result = append(result, placeholder...)
	for _, g := range kept {
		result = append(result, extractGroupMsgs(msgs[1:], g)...)
	}

	// Verify it fits; if not, fall back to aggressive.
	afterTokens := estimateConversationTokens(result)
	if afterTokens > budget {
		return cc.compressLayer2(msgs, budget, usageBefore)
	}

	// Record lineage.
	cc.lineage = append(cc.lineage, SessionLineage{
		ParentSessionID:    cc.sessionID,
		CompressedAt:       time.Now(),
		Layer:              1,
		DroppedMsgCount:    countGroupMsgs(dropped),
		SummaryTokens:      estimateConversationTokens(placeholder),
		ContextUsageBefore: usageBefore,
		ContextUsageAfter:  float64(afterTokens) / float64(budget),
	})

	log.Printf("[context_compress] layer1: dropped %d msgs, usage %.0f%%→%.0f%%",
		countGroupMsgs(dropped), usageBefore*100, float64(afterTokens)/float64(budget)*100)

	return result
}

// ---------------------------------------------------------------------------
// Layer 2: Aggressive compression (85% threshold)
// ---------------------------------------------------------------------------

// compressLayer2 keeps only the system prompt + recent 8 messages with a
// minimal summary. Tool results in kept messages are truncated.
func (cc *ContextCompressor) compressLayer2(msgs []interface{}, budget int, usageBefore float64) []interface{} {
	systemMsg := msgs[:1]
	tail := msgs[1:]

	groups := buildMsgGroups(tail)

	keepCount := aggressiveKeepMessages
	if keepCount > len(groups) {
		keepCount = len(groups)
	}

	dropGroups := len(groups) - keepCount
	if dropGroups <= 0 {
		// Even keeping all groups, still over budget — truncate content.
		return truncateAssistantContent(
			trimConversation(msgs, cc.contextLimit, cc.toolsTokens, cc.summarizer),
			budget,
		)
	}

	dropped := groups[:dropGroups]
	kept := groups[dropGroups:]

	// Build a minimal summary (no LLM call for aggressive — just a marker).
	placeholder := cc.buildAggressivePlaceholder(dropped)

	result := make([]interface{}, 0, 1+len(placeholder)+countGroupMsgs(kept))
	result = append(result, systemMsg...)
	result = append(result, placeholder...)
	for _, g := range kept {
		result = append(result, extractGroupMsgs(tail, g)...)
	}

	// Truncate tool results in kept messages to save more space.
	result = truncateToolResultsInConversation(result)

	// If still over budget, apply assistant content truncation.
	afterTokens := estimateConversationTokens(result)
	if afterTokens > budget {
		result = truncateAssistantContent(result, budget)
		afterTokens = estimateConversationTokens(result)
	}

	// Last resort: if still over, fall back to system + placeholder only.
	if afterTokens > budget {
		result = make([]interface{}, 0, 1+len(placeholder))
		result = append(result, systemMsg...)
		result = append(result, placeholder...)
		afterTokens = estimateConversationTokens(result)
	}

	cc.lineage = append(cc.lineage, SessionLineage{
		ParentSessionID:    cc.sessionID,
		CompressedAt:       time.Now(),
		Layer:              2,
		DroppedMsgCount:    countGroupMsgs(dropped),
		SummaryTokens:      estimateConversationTokens(placeholder),
		ContextUsageBefore: usageBefore,
		ContextUsageAfter:  float64(afterTokens) / float64(budget),
	})

	log.Printf("[context_compress] layer2: dropped %d msgs, usage %.0f%%→%.0f%%",
		countGroupMsgs(dropped), usageBefore*100, float64(afterTokens)/float64(budget)*100)

	return result
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (cc *ContextCompressor) flushMemory() {
	if cc.memoryFlusher == nil {
		return
	}
	if err := cc.memoryFlusher(); err != nil {
		log.Printf("[context_compress] memory flush failed: %v", err)
	}
}

// msgGroup represents a logical group of messages (assistant+tool_calls + tool results).
type ctxMsgGroup struct {
	start, end int // half-open range in the tail slice (msgs[1:])
}

func buildMsgGroups(tail []interface{}) []ctxMsgGroup {
	var groups []ctxMsgGroup
	i := 0
	for i < len(tail) {
		gStart := i
		role := msgRole(tail[i])
		if role == "assistant" && msgHasToolCalls(tail[i]) {
			i++
			for i < len(tail) && msgRole(tail[i]) == "tool" {
				i++
			}
		} else {
			i++
		}
		groups = append(groups, ctxMsgGroup{start: gStart, end: i})
	}
	return groups
}

func countGroupMsgs(groups []ctxMsgGroup) int {
	n := 0
	for _, g := range groups {
		n += g.end - g.start
	}
	return n
}

func extractGroupMsgs(tail []interface{}, g ctxMsgGroup) []interface{} {
	if g.start >= len(tail) {
		return nil
	}
	end := g.end
	if end > len(tail) {
		end = len(tail)
	}
	return tail[g.start:end]
}

// buildSummaryPlaceholder creates a summary placeholder for Layer 1.
// Uses the LLM summarizer if available.
func (cc *ContextCompressor) buildSummaryPlaceholder(msgs []interface{}, dropped []ctxMsgGroup, layer int) []interface{} {
	tail := msgs[1:]

	if cc.summarizer != nil {
		var sb strings.Builder
		for _, g := range dropped {
			for idx := g.start; idx < g.end && idx < len(tail); idx++ {
				data, _ := json.Marshal(tail[idx])
				sb.Write(data)
				sb.WriteByte('\n')
			}
		}
		raw := sb.String()
		rawRunes := []rune(raw)
		if len(rawRunes) > 16000 {
			raw = string(rawRunes[:16000]) + "\n...(truncated)"
		}
		if summary := cc.summarizer(raw); summary != "" {
			// Cap summary length.
			if len([]rune(summary)) > maxSummaryChars {
				runes := []rune(summary)
				summary = string(runes[:maxSummaryChars]) + "…"
			}
			return []interface{}{
				map[string]string{
					"role":    "user",
					"content": fmt.Sprintf("[会话上下文摘要 | 压缩层级: %d | 已压缩 %d 条消息]\n%s", layer, countGroupMsgs(dropped), summary),
				},
				map[string]string{
					"role":              "assistant",
					"content":           "好的，我已了解之前的对话上下文摘要，将基于此继续工作。",
					"reasoning_content": "",
				},
			}
		}
	}

	// Fallback: no summarizer or summarizer failed.
	return []interface{}{
		map[string]string{
			"role":    "user",
			"content": fmt.Sprintf("[注意：前 %d 条对话消息因上下文压缩已被省略，请基于最近的上下文继续工作]", countGroupMsgs(dropped)),
		},
	}
}

// buildAggressivePlaceholder creates a minimal placeholder for Layer 2.
// No LLM call — just a short marker to save tokens.
func (cc *ContextCompressor) buildAggressivePlaceholder(dropped []ctxMsgGroup) []interface{} {
	return []interface{}{
		map[string]string{
			"role":    "user",
			"content": fmt.Sprintf("[上下文已激进压缩 | 已省略 %d 条消息 | 仅保留最近 %d 条 | 请基于当前上下文继续]", countGroupMsgs(dropped), aggressiveKeepMessages),
		},
	}
}

// truncateToolResultsInConversation truncates tool result content in the
// conversation to save space during aggressive compression.
func truncateToolResultsInConversation(msgs []interface{}) []interface{} {
	result := make([]interface{}, len(msgs))
	for i, m := range msgs {
		mm, ok := m.(map[string]interface{})
		if !ok {
			result[i] = m
			continue
		}
		role, _ := mm["role"].(string)
		if role != "tool" {
			result[i] = m
			continue
		}
		content, _ := mm["content"].(string)
		runes := []rune(content)
		if len(runes) <= 1024 {
			result[i] = m
			continue
		}
		truncated := agent.TruncateToolContentPreservingHandle(content, 400, 200)
		cp := make(map[string]interface{}, len(mm))
		for k, v := range mm {
			cp[k] = v
		}
		cp["content"] = truncated
		result[i] = cp
	}
	return result
}

// llmEstimateTokensAdapter converts a []interface{} conversation to the
// format expected by llm.EstimateTokens ([]map[string]interface{}).
// Messages that are map[string]string are promoted; others are best-effort.
func llmEstimateTokensAdapter(msgs []interface{}) int {
	adapted := make([]map[string]interface{}, 0, len(msgs))
	for _, m := range msgs {
		switch v := m.(type) {
		case map[string]interface{}:
			adapted = append(adapted, v)
		case map[string]string:
			mm := make(map[string]interface{}, len(v))
			for k, val := range v {
				mm[k] = val
			}
			adapted = append(adapted, mm)
		}
	}
	return llm.EstimateTokens(adapted)
}
