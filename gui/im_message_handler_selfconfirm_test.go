package main

import (
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Bug Condition Exploration Test — Property 1: Self-Confirmed NeedsConfirm
// Responses Are Not Truncated
//
// Feature: workflow-self-confirm-bypass
// Property 1: Bug Condition — Self-Confirmed NeedsConfirm Responses Are
// Not Truncated
//
// **Validates: Requirements 1.1, 1.3, 1.4, 1.5, 1.6**
//
// CRITICAL: This test MUST FAIL on unfixed code — failure confirms the bug
// exists. DO NOT attempt to fix the test or the code when it fails.
//
// The functions containsSelfConfirmationPattern, truncateAtConfirmationBoundary,
// and findConfirmationRequestPos DO NOT EXIST YET. They will be added in
// Task 3. The test will fail to compile until then, which IS the expected
// "failure" proving the bug exists: the NeedsConfirm gate has no mechanism
// to detect or truncate self-confirmation patterns.
//
// The test encodes the expected (correct) behavior: when the LLM's response
// contains a self-confirmation pattern (confirmation request followed by
// self-answer or phase transition), the system SHALL detect the pattern and
// truncate the response at the confirmation request boundary.
// ---------------------------------------------------------------------------

// TestProperty1_BugCondition_SelfConfirmedResponsesTruncated verifies that
// containsSelfConfirmationPattern detects self-confirmation and
// truncateAtConfirmationBoundary truncates at the correct boundary for
// concrete self-confirmed NeedsConfirm responses.
//
// On UNFIXED code this test will fail to compile because
// containsSelfConfirmationPattern, truncateAtConfirmationBoundary, and
// findConfirmationRequestPos do not exist yet — that compilation failure
// IS the proof that the bug exists (there is no mechanism to detect or
// truncate self-confirmation patterns).
func TestProperty1_BugCondition_SelfConfirmedResponsesTruncated(t *testing.T) {
	cases := []struct {
		name  string
		input string
		// selfConfirmContent are substrings that should NOT appear in the
		// truncated result (they are the self-answer / phase transition).
		selfConfirmContent []string
		// confirmRequest is a substring that SHOULD appear in the truncated
		// result (the confirmation prompt is preserved).
		confirmRequest string
	}{
		{
			name: "PPT slide_scripting self-confirm (Chinese)",
			input: generateSubstantivePrefix(300) +
				"\n\n以上是全部20页的逐页脚本。\n\n请确认以上全部20页的逐页脚本，或提出修改意见。\n\n好的，逐页脚本已确认！现在进入最终阶段——PPT生成...",
			selfConfirmContent: []string{"已确认", "现在进入"},
			confirmRequest:     "请确认",
		},
		{
			name: "Coding requirements self-confirm (Chinese)",
			input: "# 需求文档\n\n## 功能需求\n\n" + generateSubstantivePrefix(250) +
				"\n\n请确认以上需求，或提出修改意见。\n\n好的，需求已确认！现在开始技术设计...",
			selfConfirmContent: []string{"已确认", "现在开始技术设计"},
			confirmRequest:     "请确认",
		},
		{
			name: "English workflow self-confirm",
			input: "# Requirements Document\n\n## Functional Requirements\n\n" + generateSubstantivePrefix(250) +
				"\n\nPlease confirm the above requirements, or suggest changes.\n\nConfirmed! Let me proceed to the design phase...",
			selfConfirmContent: []string{"Confirmed!", "Let me proceed"},
			confirmRequest:     "confirm",
		},
		{
			name: "Phase transition without explicit confirm (Chinese)",
			input: generateSubstantivePrefix(300) +
				"\n\n请确认以上内容。\n\n现在进入下一阶段...",
			selfConfirmContent: []string{"现在进入下一阶段"},
			confirmRequest:     "请确认",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trimmed := strings.TrimSpace(stripThinkingTags(tc.input))

			// Pre-condition: input is non-empty and
			// is a substantive phase document.
			if trimmed == "" {
				t.Fatal("precondition failed: input must be non-empty")
			}
			if !isSubstantivePhaseDocument(trimmed) {
				t.Fatalf("precondition failed: input must be a substantive phase document (len=%d runes)", utf8.RuneCountInString(trimmed))
			}

			// --- Bug condition check ---
			// containsSelfConfirmationPattern MUST detect the self-confirm pattern.
			// On unfixed code, this function does not exist → compile error → bug confirmed.
			if !containsSelfConfirmationPattern(trimmed) {
				t.Errorf("containsSelfConfirmationPattern returned false, want true; input contains self-confirmation pattern")
			}

			// --- Expected behavior: truncation ---
			// truncateAtConfirmationBoundary MUST truncate at the confirmation
			// request boundary, removing the self-answer and phase transition.
			truncated := truncateAtConfirmationBoundary(trimmed)

			// 1. Result text does NOT contain self-confirmation content.
			for _, sc := range tc.selfConfirmContent {
				if strings.Contains(truncated, sc) {
					t.Errorf("truncated text still contains self-confirmation content %q; should have been removed", sc)
				}
			}

			// 2. Result text ends at or near the confirmation request
			//    (the confirmation prompt itself is preserved).
			if !strings.Contains(strings.ToLower(truncated), strings.ToLower(tc.confirmRequest)) {
				t.Errorf("truncated text does not contain confirmation request %q; the prompt should be preserved", tc.confirmRequest)
			}

			// 3. Truncated text still passes isSubstantivePhaseDocument
			//    (truncated text is still valid for the gate).
			if !isSubstantivePhaseDocument(truncated) {
				t.Errorf("isSubstantivePhaseDocument(truncated) = false, want true; truncated text (len=%d runes) should still be a valid substantive document", utf8.RuneCountInString(truncated))
			}

			// 4. findConfirmationRequestPos returns a valid position.
			pos := findConfirmationRequestPos(trimmed)
			if pos < 0 {
				t.Errorf("findConfirmationRequestPos returned -1, want >= 0; input contains a confirmation request")
			}
		})
	}
}

// TestProperty1_BugCondition_PBT_SelfConfirmedAlwaysDetected uses
// testing/quick to generate random substantive documents with appended
// self-confirmation patterns and verifies that containsSelfConfirmationPattern
// returns true AND truncateAtConfirmationBoundary removes the self-answer.
//
// **Validates: Requirements 1.1, 1.3, 1.4, 1.5, 1.6**
func TestProperty1_BugCondition_PBT_SelfConfirmedAlwaysDetected(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	err := quick.Check(func(seed int64) bool {
		input, selfAnswer := generateSelfConfirmedDocument(seed)
		trimmed := strings.TrimSpace(stripThinkingTags(input))

		// Pre-conditions must hold.
		if trimmed == "" {
			return true // skip degenerate inputs
		}
		if !isSubstantivePhaseDocument(trimmed) {
			return true // skip if generator produced non-substantive text
		}

		// Property 1: containsSelfConfirmationPattern MUST return true.
		if !containsSelfConfirmationPattern(trimmed) {
			return false
		}

		// Property 2: truncateAtConfirmationBoundary MUST remove the self-answer.
		truncated := truncateAtConfirmationBoundary(trimmed)
		if strings.Contains(truncated, selfAnswer) {
			return false
		}

		// Property 3: truncated text MUST still be a substantive document.
		if !isSubstantivePhaseDocument(truncated) {
			return false
		}

		return true
	}, cfg)
	if err != nil {
		t.Errorf("Property violated: self-confirmed document not properly detected/truncated: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Generators
// ---------------------------------------------------------------------------

// generateSubstantivePrefix produces a string of at least `minRunes` runes
// that looks like a substantive document body (no self-confirmation content).
func generateSubstantivePrefix(minRunes int) string {
	body := "这是一段详细的文档内容，包含了项目的核心需求和技术规格说明。" +
		"系统需要支持多用户并发访问，提供实时数据同步功能。" +
		"前端采用 React 框架，后端使用 Go 语言开发 REST API。" +
		"数据库选用 PostgreSQL，缓存层使用 Redis。" +
		"部署方案采用 Docker 容器化，通过 Kubernetes 编排。" +
		"监控系统集成 Prometheus 和 Grafana，日志收集使用 ELK Stack。"
	for utf8.RuneCountInString(body) < minRunes {
		body += " 额外的文档内容用于确保文本长度满足测试要求。Additional content to ensure sufficient length."
	}
	return body
}

// generateSelfConfirmedDocument produces a random substantive document with
// a self-confirmation pattern appended. Returns the full text and the
// self-answer substring that should be removed by truncation.
func generateSelfConfirmedDocument(seed int64) (fullText string, selfAnswer string) {
	// Document prefixes (substantive content, 200+ runes).
	prefixes := []string{
		"# 需求文档\n\n## 功能需求\n\n" + generateSubstantivePrefix(250),
		"# Requirements\n\n## Features\n\n" + generateSubstantivePrefix(250),
		"## 技术设计\n\n### 架构概述\n\n" + generateSubstantivePrefix(250),
		"# Design Document\n\n## Architecture\n\n" + generateSubstantivePrefix(250),
		generateSubstantivePrefix(300),
	}

	// Confirmation request patterns.
	confirmRequests := []string{
		"\n\n请确认以上内容，或提出修改意见。",
		"\n\n请确认以上需求，或提出修改意见。",
		"\n\n请确认以上全部内容。",
		"\n\nPlease confirm the above requirements, or suggest changes.",
		"\n\nPlease confirm the above, or let me know if you'd like changes.",
		"\n\n请查看并确认以上设计方案。",
	}

	// Self-answer patterns (the content that should be truncated).
	type selfAnswerEntry struct {
		text    string
		keyword string // the key substring to check for in truncated output
	}
	selfAnswers := []selfAnswerEntry{
		{"\n\n好的，已确认！现在进入下一阶段...", "已确认"},
		{"\n\n好的，需求已确认！现在开始技术设计...", "已确认"},
		{"\n\n好的，逐页脚本已确认！现在进入最终阶段——PPT生成...", "已确认"},
		{"\n\nConfirmed! Let me proceed to the design phase...", "Confirmed!"},
		{"\n\n现在进入下一阶段...", "现在进入下一阶段"},
		{"\n\n好的，确认完毕！开始生成代码...", "确认完毕"},
		{"\n\nLet me start the implementation now...", "Let me start"},
		{"\n\n好的，收到确认。现在进入最终阶段...", "收到确认"},
	}

	pidx := seed % int64(len(prefixes))
	if pidx < 0 {
		pidx = -pidx
	}
	cidx := (seed / int64(len(prefixes))) % int64(len(confirmRequests))
	if cidx < 0 {
		cidx = -cidx
	}
	sidx := (seed / int64(len(prefixes)*len(confirmRequests))) % int64(len(selfAnswers))
	if sidx < 0 {
		sidx = -sidx
	}

	prefix := prefixes[pidx]
	confirm := confirmRequests[cidx]
	sa := selfAnswers[sidx]

	return prefix + confirm + sa.text, sa.keyword
}
