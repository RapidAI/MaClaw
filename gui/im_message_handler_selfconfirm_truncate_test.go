package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Unit tests for truncateAtConfirmationBoundary
//
// Feature: workflow-self-confirm-bypass
// Task: 3.3 — truncateAtConfirmationBoundary implementation
//
// **Validates: Requirements 2.1, 2.2, 2.6**
// ---------------------------------------------------------------------------

func TestTruncateAtConfirmationBoundary_ChineseSelfConfirm(t *testing.T) {
	// Chinese self-confirm: deliverable + confirmation prompt + self-answer.
	// Should truncate at the paragraph break after the confirmation request,
	// preserving the deliverable and confirmation prompt.
	input := generateSubstantivePrefix(300) +
		"\n\n以上是全部20页的逐页脚本。\n\n请确认以上全部20页的逐页脚本，或提出修改意见。\n\n好的，逐页脚本已确认！现在进入最终阶段——PPT生成..."

	result := truncateAtConfirmationBoundary(input)

	// Confirmation request should be preserved.
	if !strings.Contains(result, "请确认") {
		t.Error("truncated text should contain the confirmation request '请确认'")
	}

	// Self-answer content should be removed.
	if strings.Contains(result, "已确认") {
		t.Error("truncated text should NOT contain self-answer '已确认'")
	}
	if strings.Contains(result, "现在进入") {
		t.Error("truncated text should NOT contain phase transition '现在进入'")
	}
	if strings.Contains(result, "PPT生成") {
		t.Error("truncated text should NOT contain next-phase content 'PPT生成'")
	}

	// Deliverable content should be preserved.
	if !strings.Contains(result, "以上是全部20页的逐页脚本") {
		t.Error("truncated text should preserve deliverable content")
	}
}

func TestTruncateAtConfirmationBoundary_EnglishSelfConfirm(t *testing.T) {
	// English self-confirm: deliverable + confirmation prompt + self-answer.
	input := "# Requirements Document\n\n## Functional Requirements\n\n" +
		generateSubstantivePrefix(250) +
		"\n\nPlease confirm the above requirements, or suggest changes.\n\nConfirmed! Let me proceed to the design phase..."

	result := truncateAtConfirmationBoundary(input)

	// Confirmation request should be preserved (case-insensitive check).
	if !strings.Contains(strings.ToLower(result), "please confirm") {
		t.Error("truncated text should contain the confirmation request 'Please confirm'")
	}

	// Self-answer content should be removed.
	if strings.Contains(result, "Confirmed!") {
		t.Error("truncated text should NOT contain self-answer 'Confirmed!'")
	}
	if strings.Contains(result, "Let me proceed") {
		t.Error("truncated text should NOT contain phase transition 'Let me proceed'")
	}

	// Deliverable content should be preserved.
	if !strings.Contains(result, "Requirements Document") {
		t.Error("truncated text should preserve deliverable content")
	}
}

func TestTruncateAtConfirmationBoundary_ShortDeliverable_SafetyFallback(t *testing.T) {
	// Very short deliverable before confirmation — truncated result would be
	// < 50 runes, so safety fallback returns original text unchanged.
	input := "短文。\n\n请确认。\n\n已确认！现在开始。"

	result := truncateAtConfirmationBoundary(input)

	// Safety fallback: result should be the original text unchanged.
	if result != input {
		t.Errorf("expected safety fallback to return original text unchanged;\ngot:  %q\nwant: %q", result, input)
	}
}

func TestTruncateAtConfirmationBoundary_SubstantiveAfterTruncation(t *testing.T) {
	// Truncated result should still pass isSubstantivePhaseDocument for
	// typical documents (deliverable is long enough).
	input := "# 需求文档\n\n## 功能需求\n\n" +
		generateSubstantivePrefix(300) +
		"\n\n请确认以上需求，或提出修改意见。\n\n好的，需求已确认！现在开始技术设计..."

	result := truncateAtConfirmationBoundary(input)

	if !isSubstantivePhaseDocument(result) {
		t.Errorf("isSubstantivePhaseDocument(truncated) = false, want true; truncated text (len=%d runes) should still be substantive",
			utf8.RuneCountInString(result))
	}
}

func TestTruncateAtConfirmationBoundary_ConfirmAtEnd_NoSelfAnswer(t *testing.T) {
	// Confirmation request at end of text with no self-answer following.
	// The text has no \n\n or \n after the confirmation request, so the
	// function should return text unchanged (nothing to truncate).
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上内容，或提出修改意见。"

	result := truncateAtConfirmationBoundary(input)

	// Should return text unchanged or near-unchanged (only trailing whitespace trimmed).
	if strings.TrimSpace(result) != strings.TrimSpace(input) {
		t.Errorf("expected text unchanged when confirmation request is at end;\ngot len=%d, want len=%d",
			len(result), len(input))
	}
}

func TestTruncateAtConfirmationBoundary_NoConfirmationRequest(t *testing.T) {
	// No confirmation request at all — should return text unchanged.
	input := "# 技术设计文档\n\n## 架构设计\n\n" + generateSubstantivePrefix(300)

	result := truncateAtConfirmationBoundary(input)

	if result != input {
		t.Errorf("expected text unchanged when no confirmation request;\ngot len=%d, want len=%d",
			len(result), len(input))
	}
}

func TestTruncateAtConfirmationBoundary_SingleNewlineAfterConfirm(t *testing.T) {
	// Confirmation request followed by a single \n (not \n\n) then self-answer.
	// Should truncate at the \n boundary.
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上全部内容。\n好的，已确认！现在进入下一阶段..."

	result := truncateAtConfirmationBoundary(input)

	// Self-answer should be removed.
	if strings.Contains(result, "已确认") {
		t.Error("truncated text should NOT contain self-answer '已确认'")
	}
	if strings.Contains(result, "现在进入") {
		t.Error("truncated text should NOT contain phase transition '现在进入'")
	}

	// Confirmation request should be preserved.
	if !strings.Contains(result, "请确认") {
		t.Error("truncated text should contain the confirmation request '请确认'")
	}
}

func TestTruncateAtConfirmationBoundary_ConfirmWithTrailingLine(t *testing.T) {
	// Confirmation request on its own line, followed by \n\n and self-answer.
	// The confirmation request line itself should be preserved.
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上需求，或提出修改意见。\n\n好的，需求已确认！现在开始技术设计..."

	result := truncateAtConfirmationBoundary(input)

	// The confirmation request line should be in the result.
	if !strings.Contains(result, "请确认以上需求，或提出修改意见。") {
		t.Error("truncated text should preserve the full confirmation request line")
	}

	// Self-answer should NOT be in the result.
	if strings.Contains(result, "已确认") {
		t.Error("truncated text should NOT contain '已确认'")
	}
}

func TestTruncateAtConfirmationBoundary_PhaseTransitionWithoutExplicitConfirm(t *testing.T) {
	// Phase transition without explicit "已确认" — just "现在进入下一阶段".
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上内容。\n\n现在进入下一阶段..."

	result := truncateAtConfirmationBoundary(input)

	// Phase transition should be removed.
	if strings.Contains(result, "现在进入下一阶段") {
		t.Error("truncated text should NOT contain phase transition '现在进入下一阶段'")
	}

	// Confirmation request should be preserved.
	if !strings.Contains(result, "请确认") {
		t.Error("truncated text should contain the confirmation request '请确认'")
	}
}
