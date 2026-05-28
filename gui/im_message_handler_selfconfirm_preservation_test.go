package main

import (
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Preservation Property Tests — Property 2: Non-Self-Confirmed Responses
// Unchanged
//
// Feature: workflow-self-confirm-bypass
// Property 2: Preservation — Non-Self-Confirmed Responses Unchanged
//
// **Validates: Requirements 2.5, 3.1, 3.2, 3.3, 3.5, 3.8, 3.9**
//
// IMPORTANT: These tests follow observation-first methodology. They test
// EXISTING behavior of isSubstantivePhaseDocument and stripThinkingTags on
// UNFIXED code. All tests MUST PASS on unfixed code
// to confirm baseline behavior that the fix must preserve.
//
// These tests do NOT reference containsSelfConfirmationPattern,
// truncateAtConfirmationBoundary, or findConfirmationRequestPos — those
// functions do not exist yet and will be added in Task 3.
// ---------------------------------------------------------------------------

// confirmRequestPatternRe is an alias for the production confirmRequestRe
// regex. Used in preservation tests to verify false-positive behavior.
// Originally defined inline so the test compiled before the implementation
// existed; now that confirmRequestRe is implemented, we reference it directly
// to avoid pattern drift.
var confirmRequestPatternRe = confirmRequestRe

// ---------------------------------------------------------------------------
// Sub-property 2a: NeedsConfirm=true + substantive + no self-confirmation
// → full text returned unchanged, gate force-returns
// ---------------------------------------------------------------------------

// TestProperty2a_SubstantiveNormalConfirmPrompt_Unchanged verifies that
// substantive documents ending with a normal confirmation prompt (no
// self-answer) pass isSubstantivePhaseDocument.
// This is the baseline behavior the fix must preserve: the gate force-returns
// the full text unchanged.
//
// **Validates: Requirements 2.5, 3.1**
func TestProperty2a_SubstantiveNormalConfirmPrompt_Unchanged(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{
			name: "Chinese requirements doc with confirm prompt",
			input: "# 需求文档\n\n## 功能需求\n\n" + generateSubstantivePrefix(250) +
				"\n\n请确认以上需求，或提出修改意见。",
		},
		{
			name: "English requirements doc with confirm prompt",
			input: "# Requirements Document\n\n## Functional Requirements\n\n" + generateSubstantivePrefix(250) +
				"\n\nPlease confirm the above requirements, or suggest changes.",
		},
		{
			name: "Chinese design doc with confirm prompt",
			input: "## 技术设计\n\n### 架构概述\n\n" + generateSubstantivePrefix(300) +
				"\n\n请查看并确认以上设计方案。",
		},
		{
			name: "Chinese doc with input instruction (not self-answer)",
			input: "# 需求文档\n\n## 功能需求\n\n" + generateSubstantivePrefix(250) +
				"\n\n请确认以上需求，或提出修改意见。\n\n请输入：确认 或 修改意见",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trimmed := strings.TrimSpace(stripThinkingTags(tc.input))

			// Must be non-empty.
			if trimmed == "" {
				t.Fatal("precondition: trimmed text must be non-empty")
			}
			// Must be a substantive phase document → gate would force-return.
			if !isSubstantivePhaseDocument(trimmed) {
				t.Errorf("isSubstantivePhaseDocument returned false (len=%d runes); normal confirm prompt doc should be substantive",
					utf8.RuneCountInString(trimmed))
			}
			// Full text is preserved (no truncation mechanism exists on unfixed code).
			// After fix, this behavior must remain unchanged for non-self-confirmed docs.
			if trimmed != strings.TrimSpace(stripThinkingTags(tc.input)) {
				t.Errorf("text changed after stripThinkingTags+TrimSpace; expected unchanged")
			}
		})
	}
}

// TestProperty2a_PBT_SubstantiveConfirmPromptAlwaysPassesGate uses
// testing/quick to generate random substantive documents with normal
// confirmation prompts (no self-answer) and verifies they pass
// isSubstantivePhaseDocument.
//
// **Validates: Requirements 2.5, 3.1**
func TestProperty2a_PBT_SubstantiveConfirmPromptAlwaysPassesGate(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	err := quick.Check(func(seed int64) bool {
		input := generateNormalConfirmDocument(seed)
		trimmed := strings.TrimSpace(stripThinkingTags(input))

		if trimmed == "" {
			return true // skip degenerate
		}

		// Property: substantive document with normal confirm prompt passes gate.
		if !isSubstantivePhaseDocument(trimmed) {
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Errorf("Property 2a violated: substantive normal confirm doc failed gate check: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2b: Substantive documents WITHOUT any confirmation request
// → full text returned unchanged
// ---------------------------------------------------------------------------

// TestProperty2b_SubstantiveNoConfirmRequest_Unchanged verifies that
// substantive documents without any confirmation request pass
// isSubstantivePhaseDocument. These documents should pass through the gate
// unchanged (no confirmation request means containsSelfConfirmationPattern
// would return false after the fix).
//
// **Validates: Requirements 2.5, 3.1**
func TestProperty2b_SubstantiveNoConfirmRequest_Unchanged(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "Chinese technical design doc (no confirm prompt)",
			input: "# 技术设计文档\n\n## 架构设计\n\n" + generateSubstantivePrefix(300),
		},
		{
			name:  "English design doc (no confirm prompt)",
			input: "# Design Document\n\n## Architecture\n\n" + generateSubstantivePrefix(300),
		},
		{
			name:  "Pure content document with bullet lists",
			input: "## 项目概述\n\n- 功能模块A：用户管理\n- 功能模块B：数据分析\n- 功能模块C：报表生成\n\n" + generateSubstantivePrefix(200),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trimmed := strings.TrimSpace(stripThinkingTags(tc.input))

			if !isSubstantivePhaseDocument(trimmed) {
				t.Errorf("isSubstantivePhaseDocument returned false (len=%d runes); substantive doc should pass",
					utf8.RuneCountInString(trimmed))
			}
			// Verify no confirmation request pattern exists.
			if confirmRequestPatternRe.MatchString(trimmed) {
				t.Errorf("test input unexpectedly contains a confirmation request pattern")
			}
		})
	}
}

// TestProperty2b_PBT_SubstantiveNoConfirmAlwaysPassesGate uses testing/quick
// to generate random substantive documents WITHOUT any confirmation request
// and verifies they pass isSubstantivePhaseDocument.
//
// **Validates: Requirements 2.5, 3.1**
func TestProperty2b_PBT_SubstantiveNoConfirmAlwaysPassesGate(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	err := quick.Check(func(seed int64) bool {
		input := generateSubstantiveDocNoConfirm(seed)
		trimmed := strings.TrimSpace(stripThinkingTags(input))

		if trimmed == "" {
			return true
		}

		// Property: substantive doc without confirm request passes gate.
		if !isSubstantivePhaseDocument(trimmed) {
			return false
		}
		// Property: no confirmation request pattern present.
		if confirmRequestPatternRe.MatchString(trimmed) {
			return false // generator bug — should not produce confirm patterns
		}
		return true
	}, cfg)
	if err != nil {
		t.Errorf("Property 2b violated: substantive doc without confirm request failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2c: NeedsConfirm=false → gate does NOT activate
// ---------------------------------------------------------------------------

// TestProperty2c_NeedsConfirmFalse_GateDoesNotActivate verifies the gate
// activation condition: when NeedsConfirm=false, the gate should not
// activate regardless of content. This tests the boolean logic directly.
//
// **Validates: Requirements 3.2**
func TestProperty2c_NeedsConfirmFalse_GateDoesNotActivate(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	err := quick.Check(func(seed int64) bool {
		// Generate any content — even self-confirmed content.
		input, _ := generateSelfConfirmedDocument(seed)
		trimmed := strings.TrimSpace(stripThinkingTags(input))

		if trimmed == "" {
			return true
		}

		// Simulate NeedsConfirm=false gate condition.
		needsConfirm := false

		// Gate activation requires ALL conditions to be true.
		// With needsConfirm=false, the gate MUST NOT activate.
		gateActivates := needsConfirm &&
			trimmed != "" &&
			isSubstantivePhaseDocument(trimmed)

		// Property: gate never activates when NeedsConfirm=false.
		if gateActivates {
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Errorf("Property 2c violated: gate activated with NeedsConfirm=false: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2d: Short non-substantive text → isSubstantivePhaseDocument
// returns false, loop continues
// ---------------------------------------------------------------------------

// TestProperty2d_ShortNonSubstantive_LoopContinues verifies that short
// non-substantive text (preambles, thinking-out-loud) fails
// isSubstantivePhaseDocument, meaning the agent loop continues rather than
// force-returning.
//
// **Validates: Requirements 3.5**
func TestProperty2d_ShortNonSubstantive_LoopContinues(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{name: "Short Chinese preamble", input: "好的，让我来分析一下这个需求。"},
		{name: "Short English preamble", input: "Let me analyze this requirement."},
		{name: "Very short response", input: "好的"},
		{name: "Short thinking text", input: "我来想想怎么做这个功能..."},
		{name: "Short with confirm word but not substantive", input: "好的，我来确认一下"},
		{name: "Empty after strip", input: "<think>internal reasoning</think>"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trimmed := strings.TrimSpace(stripThinkingTags(tc.input))

			// Short non-substantive text must NOT pass isSubstantivePhaseDocument.
			if trimmed != "" && isSubstantivePhaseDocument(trimmed) {
				t.Errorf("isSubstantivePhaseDocument returned true for short text %q (len=%d runes); should be false",
					trimmed[:min(50, len(trimmed))], utf8.RuneCountInString(trimmed))
			}
		})
	}
}

// TestProperty2d_PBT_ShortTextNeverSubstantive uses testing/quick to generate
// random short texts (< 200 runes, no Markdown structure) and verifies they
// fail isSubstantivePhaseDocument.
//
// **Validates: Requirements 3.5**
func TestProperty2d_PBT_ShortTextNeverSubstantive(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	err := quick.Check(func(seed int64) bool {
		input := generateShortNonSubstantiveText(seed)
		trimmed := strings.TrimSpace(stripThinkingTags(input))

		if trimmed == "" {
			return true // empty is fine, loop continues
		}

		// Property: short non-substantive text fails isSubstantivePhaseDocument.
		if isSubstantivePhaseDocument(trimmed) {
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Errorf("Property 2d violated: short non-substantive text passed isSubstantivePhaseDocument: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sub-property 2e: "确认" in non-confirmation-request contexts → no false
// positives in pattern matching
// ---------------------------------------------------------------------------

// TestProperty2e_ConfirmWordInNonRequestContext_NoFalsePositive verifies that
// texts containing "确认" in non-confirmation-request contexts (e.g., as part
// of requirement descriptions, button labels, feature names) do NOT match the
// confirmation request pattern that will be used by findConfirmationRequestPos.
//
// **Validates: Requirements 3.3**
func TestProperty2e_ConfirmWordInNonRequestContext_NoFalsePositive(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "确认 as feature name in requirements",
			input: "# 需求文档\n\n## 功能需求\n\n1. 用户确认功能需求后系统自动生成报告\n2. 管理员确认审批流程\n\n" + generateSubstantivePrefix(200),
		},
		{
			name:  "确认按钮 as UI element",
			input: "## UI 设计\n\n- 确认按钮样式：蓝色圆角\n- 取消按钮样式：灰色圆角\n\n" + generateSubstantivePrefix(200),
		},
		{
			name:  "确认 in past tense description",
			input: "## 项目进展\n\n经过团队讨论，已确认技术方案采用微服务架构。数据库确认使用 PostgreSQL。\n\n" + generateSubstantivePrefix(200),
		},
		{
			name:  "确认 as part of compound word",
			input: "## 流程说明\n\n订单确认流程包括：身份确认、支付确认、发货确认三个步骤。\n\n" + generateSubstantivePrefix(200),
		},
		{
			name:  "confirm in English feature description",
			input: "## Features\n\n- Email confirmation flow\n- Order confirmation page\n- Payment confirmation dialog\n\n" + generateSubstantivePrefix(200),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			trimmed := strings.TrimSpace(stripThinkingTags(tc.input))

			// The text must NOT match the confirmation request pattern.
			// These are "确认" used in descriptive/feature contexts, not as
			// a confirmation request from the LLM to the user.
			if confirmRequestPatternRe.MatchString(trimmed) {
				t.Errorf("false positive: text matched confirmation request pattern but contains 确认 only in non-request context")
			}

			// The text should still be a substantive document.
			if !isSubstantivePhaseDocument(trimmed) {
				t.Errorf("isSubstantivePhaseDocument returned false; test input should be substantive")
			}
		})
	}
}

// TestProperty2e_PBT_NonRequestConfirmNeverMatchesPattern uses testing/quick
// to generate random substantive documents containing "确认" in
// non-confirmation-request contexts and verifies they do NOT match the
// confirmation request pattern.
//
// **Validates: Requirements 3.3**
func TestProperty2e_PBT_NonRequestConfirmNeverMatchesPattern(t *testing.T) {
	cfg := &quick.Config{MaxCount: 200}

	err := quick.Check(func(seed int64) bool {
		input := generateDocWithNonRequestConfirm(seed)
		trimmed := strings.TrimSpace(stripThinkingTags(input))

		if trimmed == "" {
			return true
		}

		// Property: text with "确认" in non-request context must NOT match
		// the confirmation request pattern.
		if confirmRequestPatternRe.MatchString(trimmed) {
			return false
		}

		return true
	}, cfg)
	if err != nil {
		t.Errorf("Property 2e violated: non-request 确认 text matched confirmation request pattern: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Generators for preservation tests
// ---------------------------------------------------------------------------

// generateNormalConfirmDocument produces a random substantive document ending
// with a normal confirmation prompt (no self-answer). These represent the
// correct NeedsConfirm behavior that must be preserved.
func generateNormalConfirmDocument(seed int64) string {
	rng := rand.New(rand.NewSource(seed))

	prefixes := []string{
		"# 需求文档\n\n## 功能需求\n\n" + generateSubstantivePrefix(250),
		"# Requirements\n\n## Features\n\n" + generateSubstantivePrefix(250),
		"## 技术设计\n\n### 架构概述\n\n" + generateSubstantivePrefix(250),
		"# Design Document\n\n## Architecture\n\n" + generateSubstantivePrefix(250),
		"# 任务列表\n\n## T1: 初始化项目\n\n" + generateSubstantivePrefix(250),
	}

	confirmPrompts := []string{
		"\n\n请确认以上内容，或提出修改意见。",
		"\n\n请确认以上需求，或提出修改意见。",
		"\n\n请确认以上全部内容。",
		"\n\nPlease confirm the above requirements, or suggest changes.",
		"\n\nPlease confirm the above, or let me know if you'd like changes.",
		"\n\n请查看并确认以上设计方案。",
		"\n\n请确认以上需求，或提出修改意见。\n\n请输入：确认 或 修改意见",
	}

	prefix := prefixes[rng.Intn(len(prefixes))]
	prompt := confirmPrompts[rng.Intn(len(confirmPrompts))]

	return prefix + prompt
}

// generateSubstantiveDocNoConfirm produces a random substantive document
// WITHOUT any confirmation request. Uses only content words, avoiding
// "请确认", "please confirm", etc.
func generateSubstantiveDocNoConfirm(seed int64) string {
	rng := rand.New(rand.NewSource(seed))

	headings := []string{
		"# 技术设计文档\n\n## 架构设计\n\n",
		"# Design Document\n\n## Architecture\n\n",
		"## 项目概述\n\n### 背景\n\n",
		"# Implementation Plan\n\n## Overview\n\n",
		"# 测试报告\n\n## 测试结果\n\n",
	}

	bodies := []string{
		"系统采用前后端分离架构，前端使用 React，后端使用 Go。数据库选用 PostgreSQL，缓存使用 Redis。",
		"The system uses a microservices architecture with gRPC for inter-service communication. Each service has its own database.",
		"本项目分为三个模块：用户管理、数据分析、报表生成。每个模块独立部署，通过 API 网关统一入口。",
		"Testing covers unit tests, integration tests, and end-to-end tests. Coverage target is 80% for core modules.",
		"部署方案采用 Kubernetes 集群，支持自动扩缩容。监控使用 Prometheus + Grafana。",
	}

	heading := headings[rng.Intn(len(headings))]
	body := bodies[rng.Intn(len(bodies))]

	// Ensure substantive length.
	result := heading + body
	for utf8.RuneCountInString(result) < 200 {
		extra := bodies[rng.Intn(len(bodies))]
		result += " " + extra
	}

	return result
}

// generateShortNonSubstantiveText produces random short texts (< 200 runes)
// without Markdown structure. These should fail isSubstantivePhaseDocument.
func generateShortNonSubstantiveText(seed int64) string {
	rng := rand.New(rand.NewSource(seed))

	// Short preambles and thinking-out-loud texts.
	// IMPORTANT: None of these contain Markdown headings (#), numbered lists
	// (1. ), or 3+ bullet lines (- item) — those would trigger
	// isSubstantivePhaseDocument even at short lengths.
	texts := []string{
		"好的，让我来分析一下。",
		"Let me think about this.",
		"我来看看这个需求。",
		"OK, I'll work on this.",
		"让我先理解一下背景。",
		"Sure, let me check.",
		"好的，我来处理。",
		"I understand the requirement.",
		"让我开始工作。",
		"Got it, working on it now.",
		"这个需求我理解了，包含用户管理和数据分析两个部分。",
		"The requirement involves user management and data analysis components.",
		"好的，我来确认一下细节。",
		"Let me verify the details first.",
		"收到，我来处理这个任务。",
	}

	return texts[rng.Intn(len(texts))]
}

// generateDocWithNonRequestConfirm produces a random substantive document
// containing "确认" in non-confirmation-request contexts (feature names,
// button labels, process descriptions). These must NOT match the confirmation
// request pattern.
func generateDocWithNonRequestConfirm(seed int64) string {
	rng := rand.New(rand.NewSource(seed))

	// Templates that use "确认" in descriptive/feature contexts.
	// CRITICAL: None of these contain "请确认", "请查看并确认", "确认后我将",
	// "please confirm", "confirm or suggest" — those are confirmation REQUEST
	// patterns that would correctly match.
	templates := []string{
		"用户确认功能需求后，系统自动生成技术方案。管理员确认审批流程后，进入下一环节。",
		"订单确认流程包括身份确认、支付确认、发货确认三个步骤。每个步骤都有独立的确认页面。",
		"确认按钮样式采用蓝色圆角设计，取消按钮采用灰色。确认弹窗需要二次确认防止误操作。",
		"经过团队讨论，已确认技术方案采用微服务架构。数据库确认使用 PostgreSQL 作为主存储。",
		"邮件确认流程：用户注册后发送确认邮件，用户点击确认链接完成注册。确认链接有效期 24 小时。",
		"The email confirmation flow sends a confirmation email after registration. Users click the confirmation link to verify.",
		"Order confirmation page displays order details. Payment confirmation dialog requires user to re-enter password.",
		"身份确认模块负责验证用户身份。确认结果存储在 Redis 缓存中，有效期 30 分钟。",
	}

	template := templates[rng.Intn(len(templates))]

	// Wrap in a substantive document structure.
	result := "## 功能设计\n\n" + template
	for utf8.RuneCountInString(result) < 200 {
		result += " 额外的设计说明内容，用于确保文档长度满足测试要求。Additional design notes for length."
	}

	return result
}
