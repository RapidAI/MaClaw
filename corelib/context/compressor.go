package context

import (
	"fmt"
	"strings"
)

// Message represents a single message in a conversation history.
type Message struct {
	Role    string
	Content string
}

// CompressConfig holds compression parameters.
type CompressConfig struct {
	ThresholdRatio   float64 // 0.80 = trigger at 80% of context window
	ProtectedTurns   int     // 5 = don't compress last 5 turns
	MaxContextTokens int     // from MaclawLLMConfig.EffectiveContextTokens()
}

// CompressResult holds the output of a compression operation.
type CompressResult struct {
	Messages         []Message // compressed message list
	OriginalTokens   int
	CompressedTokens int
	MarkerText       string // "[compressed] 12000→4500 tokens (62% reduction)"
}

// Compressor performs intelligent conversation history compression.
type Compressor struct {
	config    CompressConfig
	summarize func(text string) (string, error) // LLM summarization callback
}

// NewCompressor creates a compressor with the given config and LLM callback.
func NewCompressor(config CompressConfig, summarize func(string) (string, error)) *Compressor {
	return &Compressor{
		config:    config,
		summarize: summarize,
	}
}

// ShouldCompress returns true if the conversation exceeds the threshold.
func (c *Compressor) ShouldCompress(messages []Message) bool {
	total := 0
	for _, msg := range messages {
		total += EstimateTokens(msg.Content)
	}
	threshold := float64(c.config.MaxContextTokens) * c.config.ThresholdRatio
	return float64(total) > threshold
}

// Compress performs intelligent compression on the message history.
// It preserves the last ProtectedTurns and summarizes older content.
func (c *Compressor) Compress(messages []Message) (*CompressResult, error) {
	if len(messages) == 0 {
		return &CompressResult{
			Messages:         messages,
			OriginalTokens:   0,
			CompressedTokens: 0,
		}, nil
	}

	// Calculate original token count
	originalTokens := 0
	for _, msg := range messages {
		originalTokens += EstimateTokens(msg.Content)
	}

	// Split into older (to compress) and protected (last ProtectedTurns)
	protectedCount := c.config.ProtectedTurns
	if protectedCount > len(messages) {
		protectedCount = len(messages)
	}

	splitIdx := len(messages) - protectedCount
	if splitIdx <= 0 {
		// All messages are within the protected window, return unchanged
		return &CompressResult{
			Messages:         messages,
			OriginalTokens:   originalTokens,
			CompressedTokens: originalTokens,
		}, nil
	}

	olderMessages := messages[:splitIdx]
	protectedMessages := messages[splitIdx:]

	// Concatenate older messages' content for summarization
	var textBuilder strings.Builder
	for i, msg := range olderMessages {
		if i > 0 {
			textBuilder.WriteString("\n")
		}
		textBuilder.WriteString(fmt.Sprintf("[%s]: %s", msg.Role, msg.Content))
	}
	olderText := textBuilder.String()

	// Attempt LLM summarization
	summary, err := c.summarize(olderText)
	if err != nil {
		// Fallback: truncate oldest messages until under threshold
		return c.fallbackTruncate(messages, protectedMessages, originalTokens)
	}

	// Create compressed marker message.
	// Include the marker's own tokens in the final count for accuracy.
	summaryTokens := EstimateTokens(summary)
	protectedTokens := 0
	for _, msg := range protectedMessages {
		protectedTokens += EstimateTokens(msg.Content)
	}
	// Pre-estimate marker tokens (~20) so the reported ratio is accurate.
	markerEstimate := 20
	compressedTokens := summaryTokens + protectedTokens + markerEstimate

	reduction := 0
	if originalTokens > 0 {
		reduction = 100 - (compressedTokens*100)/originalTokens
	}
	markerText := fmt.Sprintf("[compressed] %d→%d tokens (%d%% reduction)", originalTokens, compressedTokens, reduction)

	// Build result: [summary message] + [compressed marker] + [protected messages]
	result := make([]Message, 0, len(protectedMessages)+2)
	// Add summary as a system message
	result = append(result, Message{
		Role:    "system",
		Content: summary,
	})
	// Add compressed marker
	result = append(result, Message{
		Role:    "system",
		Content: markerText,
	})
	// Add protected messages maintaining chronological order
	result = append(result, protectedMessages...)

	return &CompressResult{
		Messages:         result,
		OriginalTokens:   originalTokens,
		CompressedTokens: compressedTokens,
		MarkerText:       markerText,
	}, nil
}

// fallbackTruncate drops oldest messages until the total token count is under the threshold.
// This is used when LLM summarization fails.
func (c *Compressor) fallbackTruncate(allMessages []Message, protectedMessages []Message, originalTokens int) (*CompressResult, error) {
	threshold := float64(c.config.MaxContextTokens) * c.config.ThresholdRatio

	protectedTokens := 0
	for _, msg := range protectedMessages {
		protectedTokens += EstimateTokens(msg.Content)
	}

	// Reserve tokens for the marker message itself (~20 tokens).
	markerReserve := 20

	// If protected messages alone exceed the threshold, we can only return
	// them with a marker — no room for older messages.
	if float64(protectedTokens+markerReserve) >= threshold {
		markerText := fmt.Sprintf("[compressed] %d→%d tokens (%d%% reduction)",
			originalTokens, protectedTokens+markerReserve,
			100-(protectedTokens+markerReserve)*100/max(originalTokens, 1))
		result := make([]Message, 0, len(protectedMessages)+1)
		result = append(result, Message{Role: "system", Content: markerText})
		result = append(result, protectedMessages...)
		return &CompressResult{
			Messages:         result,
			OriginalTokens:   originalTokens,
			CompressedTokens: protectedTokens + markerReserve,
			MarkerText:       markerText,
		}, nil
	}

	// Determine how many older messages we can keep within the remaining budget.
	splitIdx := len(allMessages) - len(protectedMessages)
	olderMessages := allMessages[:splitIdx]

	keepFrom := len(olderMessages) // start by keeping none
	runningTokens := protectedTokens + markerReserve
	for i := len(olderMessages) - 1; i >= 0; i-- {
		msgTokens := EstimateTokens(olderMessages[i].Content)
		if float64(runningTokens+msgTokens) > threshold {
			break
		}
		runningTokens += msgTokens
		keepFrom = i
	}

	// Build result with marker
	droppedTokens := 0
	for i := 0; i < keepFrom; i++ {
		droppedTokens += EstimateTokens(olderMessages[i].Content)
	}

	compressedTokens := originalTokens - droppedTokens
	reduction := 0
	if originalTokens > 0 {
		reduction = 100 - (compressedTokens*100)/originalTokens
	}
	markerText := fmt.Sprintf("[compressed] %d→%d tokens (%d%% reduction)", originalTokens, compressedTokens, reduction)

	result := make([]Message, 0, (len(olderMessages)-keepFrom)+len(protectedMessages)+1)
	result = append(result, Message{Role: "system", Content: markerText})
	result = append(result, olderMessages[keepFrom:]...)
	result = append(result, protectedMessages...)

	return &CompressResult{
		Messages:         result,
		OriginalTokens:   originalTokens,
		CompressedTokens: compressedTokens,
		MarkerText:       markerText,
	}, nil
}

// EstimateTokens estimates the token count of a text string using
// character-based heuristics: 4 chars/token for ASCII, 1.5 chars/token for CJK.
// Formula: ceil(asciiChars/4) + ceil(cjkChars/1.5)
func EstimateTokens(text string) int {
	var asciiChars, cjkChars int
	for _, r := range text {
		if isCJK(r) {
			cjkChars++
		} else {
			asciiChars++
		}
	}
	// Integer ceiling division: (n + d - 1) / d
	asciiTokens := (asciiChars + 3) / 4
	// For CJK: ceil(cjkChars / 1.5) = ceil(cjkChars * 2 / 3) = (cjkChars*2 + 2) / 3
	cjkTokens := (cjkChars*2 + 2) / 3
	return asciiTokens + cjkTokens
}

// isCJK returns true if the rune is a CJK character.
// Covers: CJK Unified Ideographs, CJK Extension A, CJK Compatibility Ideographs,
// CJK Radicals Supplement, CJK Symbols and Punctuation, Hiragana, Katakana, Hangul Syllables.
func isCJK(r rune) bool {
	switch {
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK Extension A
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK Compatibility Ideographs
		return true
	case r >= 0x2E80 && r <= 0x2EFF: // CJK Radicals Supplement
		return true
	case r >= 0x3000 && r <= 0x303F: // CJK Symbols and Punctuation
		return true
	case r >= 0x3040 && r <= 0x309F: // Hiragana
		return true
	case r >= 0x30A0 && r <= 0x30FF: // Katakana
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // Hangul Syllables
		return true
	default:
		return false
	}
}
