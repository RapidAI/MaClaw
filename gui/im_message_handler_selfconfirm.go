package main

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Self-confirmation detection helpers for NeedsConfirm gate.
//
// These functions detect when the LLM both requests confirmation AND
// self-answers it in the same response, then truncate the response at
// the confirmation request boundary.
//
// Feature: workflow-self-confirm-bypass
// ---------------------------------------------------------------------------

// confirmRequestRe matches confirmation request patterns — phrases where the
// LLM asks the user to confirm. Compiled at package level for performance.
//
// Chinese patterns:
//   - 请确认       — "please confirm" (most common)
//   - 请输入：确认  — "please enter: confirm" (full-width colon)
//   - 请输入: 确认  — "please enter: confirm" (half-width colon)
//   - 请查看并确认  — "please review and confirm"
//   - 确认后我将    — "after you confirm, I will..."
//
// English patterns (case-insensitive):
//   - please confirm
//   - please review and confirm
//   - confirm or suggest
//
// IMPORTANT: This regex must NOT match "确认" in non-request contexts such as
// "用户确认功能", "确认按钮样式", "订单确认流程". The key insight is that
// confirmation requests typically use "请" (please) before "确认", or use
// specific phrases like "请输入：确认" / "确认后我将".
var confirmRequestRe = regexp.MustCompile(
	`(?i)请确认|请输入[：:]\s*确认|请查看并确认|确认后我将|please confirm|please review and confirm|confirm or suggest`,
)

// selfAnswerRe matches self-answer and phase transition patterns — phrases
// where the LLM confirms its own request or transitions to the next phase.
// Compiled at package level for performance.
//
// Chinese self-answer patterns:
//   - 已确认         — "already confirmed"
//   - 确认完毕       — "confirmation complete"
//   - 确认！         — "confirmed!" (with Chinese exclamation mark)
//   - 好的，[^\n]*确认   — "OK, ... confirmed" (same line only)
//   - 收到确认       — "received confirmation"
//   - 确认后[^\n]*现在   — "after confirmation ... now" (same line only)
//
// Chinese phase transition patterns:
//   - 现在进入       — "now entering"
//   - 开始生成       — "start generating"
//   - 进入下一       — "entering next"
//   - 进入最终       — "entering final"
//
// English self-answer patterns (case-insensitive):
//   - confirmed
//   - proceeding to
//   - moving on to
//   - let me start
//   - let me proceed
//   - now entering
var selfAnswerRe = regexp.MustCompile(
	`(?i)已确认|确认完毕|确认！|好的，[^\n]*确认|收到确认|确认后[^\n]*现在|现在进入|开始生成|进入下一|进入最终|confirmed|proceeding to|moving on to|let me start|let me proceed|now entering`,
)

// findConfirmationRequestPos finds the byte position of the LAST confirmation
// request pattern in the text. Returns -1 if no match is found.
//
// We search for the last occurrence because the confirmation request typically
// appears near the end of the deliverable, before the self-answer. If the
// deliverable itself mentions "请确认" earlier (unlikely but possible), we
// want the final one which is the actual request to the user.
func findConfirmationRequestPos(text string) int {
	start, _ := findConfirmationRequestRange(text)
	return start
}

// findConfirmationRequestRange finds the byte range [start, end) of the LAST
// confirmation request pattern in the text. Returns (-1, -1) if no match.
// This is the core helper used by containsSelfConfirmationPattern and
// truncateAtConfirmationBoundary to avoid redundant regex evaluation.
func findConfirmationRequestRange(text string) (start, end int) {
	matches := confirmRequestRe.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return -1, -1
	}
	last := matches[len(matches)-1]
	return last[0], last[1]
}

// containsSelfConfirmationPattern detects when the LLM both requests
// confirmation AND self-answers it in the same response.
//
// The function accepts already-trimmed text (post stripThinkingTags + TrimSpace).
//
// Detection logic:
//  1. Find the last confirmation request range via findConfirmationRequestRange.
//     If no confirmation request is found, return false.
//  2. Extract the text AFTER the confirmation request pattern.
//  3. Check if the text after the request contains a self-answer or phase
//     transition pattern via selfAnswerRe.
//
// Returns true if a self-confirmation pattern is detected, false otherwise.
func containsSelfConfirmationPattern(text string) bool {
	_, end := findConfirmationRequestRange(text)
	if end < 0 {
		return false // No confirmation request found.
	}
	// Check if text after the request contains a self-answer.
	return selfAnswerRe.MatchString(text[end:])
}

// truncateAtConfirmationBoundary truncates text at the confirmation request
// boundary, removing self-answer and phase transition content that follows.
//
// The function:
//  1. Finds the last confirmation request range via findConfirmationRequestRange.
//  2. From the end of the matched pattern, scans forward to find the end of
//     the confirmation request paragraph:
//     - If a paragraph break (\n\n) is found after the request, truncate there.
//     - If only a line break (\n) is found, truncate there.
//     - If neither is found, the request is at the end — return text as-is.
//  3. Trims trailing whitespace from the result.
//  4. Safety fallback: if the truncated text has fewer than 50 runes, return
//     the original text unchanged (the deliverable is too short to be useful).
//
// This function is called when containsSelfConfirmationPattern returns true,
// to strip the self-answer portion before the NeedsConfirm gate evaluates
// the response.
func truncateAtConfirmationBoundary(text string) string {
	_, end := findConfirmationRequestRange(text)
	if end < 0 {
		return text // No confirmation request found — return unchanged.
	}

	// Scan forward from the end of the matched pattern for the paragraph/line
	// boundary that marks the end of the confirmation request paragraph.
	remaining := text[end:]

	// Look for the end of the confirmation request paragraph.
	var truncateAt int

	// First, look for a paragraph break (\n\n) after the confirmation request.
	paraBreak := strings.Index(remaining, "\n\n")
	if paraBreak >= 0 {
		// Truncate at the paragraph break — include the confirmation request
		// line/paragraph but exclude everything after the \n\n.
		truncateAt = end + paraBreak
	} else {
		// No paragraph break — look for a single line break (\n).
		lineBreak := strings.Index(remaining, "\n")
		if lineBreak >= 0 {
			truncateAt = end + lineBreak
		} else {
			// No line break at all — the confirmation request is at the very
			// end of the text. Return text as-is (nothing to truncate).
			return text
		}
	}

	result := strings.TrimRight(text[:truncateAt], " \t\r\n")

	// Safety fallback: if the truncated text is too short, return original.
	if utf8.RuneCountInString(result) < 50 {
		return text
	}

	return result
}
