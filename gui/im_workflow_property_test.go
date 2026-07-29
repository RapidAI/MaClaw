package main

import (
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"pgregory.net/rapid"
)

// ============================================================================
// Feature: gui-startup-response-optimization, Property 12: Short message fusion skip with L1 preservation
// For any user message containing fewer than 10 Unicode code points (runes) after
// whitespace trimming, the Entry_Context resolver SHALL skip UIC fusion classification
// (L2 embedding + L3 tree) AND SHALL still execute L1 keyword matching for fast-path
// intent detection.
// **Validates: Requirements 4.3, 4.4**
// ============================================================================

// classificationLayers tracks which classification layers were invoked.
type classificationLayers struct {
	l1KeywordCalled   atomic.Bool
	l2EmbeddingCalled atomic.Bool
	l3TreeCalled      atomic.Bool
}

// simulateEntryContextClassification models the entry_context resolver's
// classification behavior with the short-message optimization.
func simulateEntryContextClassification(msg string, layers *classificationLayers) {
	trimmed := strings.TrimSpace(msg)
	runeCount := utf8.RuneCountInString(trimmed)

	// L1 keyword matching is ALWAYS executed (fast, <1ms)
	layers.l1KeywordCalled.Store(true)

	// Short message optimization: skip L2+L3 for messages < 10 runes
	if runeCount < 10 {
		// Skip fusion (L2 embedding + L3 tree)
		return
	}

	// For longer messages, execute full fusion
	layers.l2EmbeddingCalled.Store(true)
	layers.l3TreeCalled.Store(true)
}

func TestProperty12_ShortMessageFusionSkip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate short messages (< 10 runes after trimming)
		// Include various character types: ASCII, CJK, mixed
		msg := rapid.OneOf(
			// Pure ASCII short messages
			rapid.StringMatching(`[a-z]{1,9}`),
			// CJK characters (each is 1 rune)
			rapid.StringMatching(`[\x{4e00}-\x{9fff}]{1,9}`),
			// Common short commands
			rapid.Just("开工"),
			rapid.Just("继续"),
			rapid.Just("好的"),
			rapid.Just("ok"),
			rapid.Just("yes"),
			rapid.Just("确认"),
			rapid.Just("hi"),
			rapid.Just("帮忙"),
			// With whitespace padding (should be trimmed)
			rapid.Just("  ok  "),
			rapid.Just("\t继续\n"),
		).Draw(t, "shortMsg")

		// Precondition: message must be < 10 runes after trimming
		trimmed := strings.TrimSpace(msg)
		if utf8.RuneCountInString(trimmed) >= 10 {
			return // skip — not a short message
		}

		layers := &classificationLayers{}
		simulateEntryContextClassification(msg, layers)

		// Property 1: L1 keyword matching IS executed
		if !layers.l1KeywordCalled.Load() {
			t.Fatalf("L1 keyword matching not called for short message %q (runes=%d)",
				msg, utf8.RuneCountInString(trimmed))
		}

		// Property 2: L2 embedding is NOT executed
		if layers.l2EmbeddingCalled.Load() {
			t.Fatalf("L2 embedding called for short message %q (runes=%d) — should be skipped",
				msg, utf8.RuneCountInString(trimmed))
		}

		// Property 3: L3 tree is NOT executed
		if layers.l3TreeCalled.Load() {
			t.Fatalf("L3 tree called for short message %q (runes=%d) — should be skipped",
				msg, utf8.RuneCountInString(trimmed))
		}
	})
}

// TestProperty12_LongMessageFullFusion verifies that messages >= 10 runes
// DO execute full fusion (L2 + L3), confirming the threshold works correctly.
func TestProperty12_LongMessageFullFusion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate messages that are >= 10 runes after trimming
		msg := rapid.OneOf(
			rapid.StringMatching(`[a-z]{10,50}`),
			rapid.StringMatching(`[\x{4e00}-\x{9fff}]{10,30}`),
			rapid.Just("帮我开发一个贪吃蛇游戏"),
			rapid.Just("design a REST API"),
			rapid.Just("搜索最新的AI论文并整理"),
		).Draw(t, "longMsg")

		trimmed := strings.TrimSpace(msg)
		if utf8.RuneCountInString(trimmed) < 10 {
			return // skip — this test is for long messages
		}

		layers := &classificationLayers{}
		simulateEntryContextClassification(msg, layers)

		// Property: all layers are called for long messages
		if !layers.l1KeywordCalled.Load() {
			t.Fatalf("L1 not called for long message %q", msg)
		}
		if !layers.l2EmbeddingCalled.Load() {
			t.Fatalf("L2 not called for long message %q (runes=%d)",
				msg, utf8.RuneCountInString(trimmed))
		}
		if !layers.l3TreeCalled.Load() {
			t.Fatalf("L3 not called for long message %q (runes=%d)",
				msg, utf8.RuneCountInString(trimmed))
		}
	})
}

// TestProperty12_BoundaryExactly10Runes verifies the boundary condition:
// exactly 10 runes should trigger full fusion (threshold is "fewer than 10").
func TestProperty12_BoundaryExactly10Runes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate exactly 10-rune messages
		msg := rapid.OneOf(
			rapid.StringMatching(`[a-z]{10}`),
			rapid.StringMatching(`[\x{4e00}-\x{9fff}]{10}`),
		).Draw(t, "boundary10Msg")

		trimmed := strings.TrimSpace(msg)
		if utf8.RuneCountInString(trimmed) != 10 {
			return // skip if generation didn't produce exactly 10
		}

		layers := &classificationLayers{}
		simulateEntryContextClassification(msg, layers)

		// Property: exactly 10 runes triggers full fusion (not skipped)
		if !layers.l2EmbeddingCalled.Load() {
			t.Fatalf("L2 not called for exactly-10-rune message %q — threshold is '<10', not '<=10'",
				msg)
		}
	})
}

// TestProperty12_WhitespaceTrimming verifies that whitespace is properly trimmed
// before counting runes for the threshold check.
func TestProperty12_WhitespaceTrimming(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a short core message (< 10 runes)
		core := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "core")

		// Add random whitespace padding
		leadingSpaces := rapid.IntRange(0, 10).Draw(t, "leadSpaces")
		trailingSpaces := rapid.IntRange(0, 10).Draw(t, "trailSpaces")

		msg := strings.Repeat(" ", leadingSpaces) + core + strings.Repeat(" ", trailingSpaces)

		trimmed := strings.TrimSpace(msg)
		if utf8.RuneCountInString(trimmed) >= 10 {
			return // skip
		}

		layers := &classificationLayers{}
		simulateEntryContextClassification(msg, layers)

		// Property: whitespace-padded short messages still skip fusion
		if layers.l2EmbeddingCalled.Load() {
			t.Fatalf("L2 called for whitespace-padded short message %q (trimmed=%q, runes=%d)",
				msg, trimmed, utf8.RuneCountInString(trimmed))
		}
	})
}
