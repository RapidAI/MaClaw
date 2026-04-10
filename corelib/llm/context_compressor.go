package llm

import (
	"fmt"
	"log"
	"strings"
)

// ContextCompressor summarizes middle conversation turns when the context
// exceeds a threshold, preserving the most recent N messages intact.
// Inspired by Hermes Agent's context_compressor.py.
type ContextCompressor struct {
	caller       ChatCaller
	protectLastN int // number of recent messages to preserve (default 20)
}

// ChatCaller abstracts the LLM call for compression.
type ChatCaller interface {
	ChatCall(messages []map[string]string) (string, error)
	IsConfigured() bool
}

// NewContextCompressor creates a compressor that preserves the last N messages.
func NewContextCompressor(caller ChatCaller, protectLastN int) *ContextCompressor {
	if protectLastN <= 0 {
		protectLastN = 20
	}
	return &ContextCompressor{
		caller:       caller,
		protectLastN: protectLastN,
	}
}

// CompressResult holds the outcome of a context compression.
type ContextCompressResult struct {
	OriginalCount   int
	CompressedCount int
	SummaryTokens   int // estimated tokens in the summary
}

// ShouldCompress returns true if the conversation exceeds the threshold.
// threshold is the fraction of context window used (e.g. 0.5 for 50%).
func ShouldCompress(messageCount int, estimatedTokens int, contextWindow int, threshold float64) bool {
	if contextWindow <= 0 || threshold <= 0 {
		return false
	}
	return float64(estimatedTokens) > float64(contextWindow)*threshold
}

// EstimateTokens provides a rough token count for a conversation.
// Uses the heuristic of ~4 chars per token for mixed CJK/English.
func EstimateTokens(messages []map[string]interface{}) int {
	total := 0
	for _, m := range messages {
		if content, ok := m["content"].(string); ok {
			total += len(content) / 4
		}
	}
	return total
}

// Compress summarizes the middle portion of a conversation, preserving
// the system message (index 0) and the last protectLastN messages.
// Returns the compressed conversation with a summary injected after
// the system message.
//
// If the caller is not configured or compression fails, returns the
// original messages unchanged (graceful degradation).
func (cc *ContextCompressor) Compress(messages []map[string]interface{}) ([]map[string]interface{}, *ContextCompressResult) {
	if cc.caller == nil || !cc.caller.IsConfigured() {
		return messages, nil
	}
	if len(messages) <= cc.protectLastN+2 {
		return messages, nil // not enough messages to compress
	}

	// Split: system (0) | middle (1..N-protectLastN) | tail (last protectLastN)
	splitIdx := len(messages) - cc.protectLastN
	if splitIdx <= 1 {
		return messages, nil
	}

	middle := messages[1:splitIdx]
	tail := messages[splitIdx:]

	// Build summary prompt from middle messages.
	var sb strings.Builder
	for _, m := range middle {
		role, _ := m["role"].(string)
		content, _ := m["content"].(string)
		if content == "" {
			continue
		}
		// Truncate very long messages for the summary prompt.
		runes := []rune(content)
		if len(runes) > 500 {
			content = string(runes[:500]) + "..."
		}
		fmt.Fprintf(&sb, "[%s] %s\n", role, content)
	}

	if sb.Len() == 0 {
		return messages, nil
	}

	summary, err := cc.caller.ChatCall([]map[string]string{
		{"role": "system", "content": `Summarize the following conversation excerpt into a concise paragraph.
Preserve: key decisions, tool results, file paths, error messages, and action items.
Drop: verbose tool output, repeated attempts, thinking/reasoning blocks.
Keep it under 300 words. Return ONLY the summary, no commentary.`},
		{"role": "user", "content": sb.String()},
	})
	if err != nil {
		log.Printf("[ContextCompressor] compression failed, keeping original: %v", err)
		return messages, nil
	}

	// Build compressed conversation: system + summary + tail
	compressed := make([]map[string]interface{}, 0, 2+len(tail))
	compressed = append(compressed, messages[0]) // system message
	compressed = append(compressed, map[string]interface{}{
		"role":    "user",
		"content": fmt.Sprintf("[对话摘要 — 已压缩 %d 轮对话]\n%s", len(middle), summary),
	})
	compressed = append(compressed, tail...)

	result := &ContextCompressResult{
		OriginalCount:   len(messages),
		CompressedCount: len(compressed),
		SummaryTokens:   len(summary) / 4,
	}

	log.Printf("[ContextCompressor] compressed %d → %d messages (summary ~%d tokens)",
		result.OriginalCount, result.CompressedCount, result.SummaryTokens)

	return compressed, result
}
