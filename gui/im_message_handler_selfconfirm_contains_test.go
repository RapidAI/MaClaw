package main

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests for containsSelfConfirmationPattern
//
// Feature: workflow-self-confirm-bypass
// Task: 3.2 — containsSelfConfirmationPattern implementation
// ---------------------------------------------------------------------------

func TestContainsSelfConfirmationPattern_ChineseSelfConfirm(t *testing.T) {
	// Chinese self-confirm: 请确认 + 已确认 → returns true
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上全部内容，或提出修改意见。\n\n好的，已确认！现在进入下一阶段..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for Chinese self-confirm pattern (请确认 + 已确认)")
	}
}

func TestContainsSelfConfirmationPattern_EnglishSelfConfirm(t *testing.T) {
	// English self-confirm: please confirm + confirmed → returns true
	input := generateSubstantivePrefix(300) +
		"\n\nPlease confirm the above requirements, or suggest changes.\n\nConfirmed! Let me proceed to the design phase..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for English self-confirm pattern (please confirm + confirmed)")
	}
}

func TestContainsSelfConfirmationPattern_PhaseTransitionWithoutExplicitConfirm(t *testing.T) {
	// Phase transition without explicit confirm word: 请确认 + 现在进入下一阶段 → returns true
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上内容。\n\n现在进入下一阶段..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for phase transition pattern (请确认 + 现在进入下一阶段)")
	}
}

func TestContainsSelfConfirmationPattern_NormalConfirmPrompt_NoSelfAnswer(t *testing.T) {
	// Normal confirmation prompt with input instruction (no self-answer) → returns false
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上需求，或提出修改意见。\n\n请输入：确认 或 修改意见"
	if containsSelfConfirmationPattern(input) {
		t.Error("expected false for normal confirmation prompt without self-answer")
	}
}

func TestContainsSelfConfirmationPattern_NoConfirmationRequest(t *testing.T) {
	// No confirmation request at all → returns false
	input := "# 技术设计文档\n\n## 架构设计\n\n" + generateSubstantivePrefix(300)
	if containsSelfConfirmationPattern(input) {
		t.Error("expected false when no confirmation request is present")
	}
}

func TestContainsSelfConfirmationPattern_ConfirmInRequirementText(t *testing.T) {
	// "确认" in requirement text (not as confirmation request) → returns false
	input := "# 需求文档\n\n## 功能需求\n\n1. 用户确认功能需求后系统自动生成报告\n2. 管理员确认审批流程\n3. 订单确认流程包括身份确认、支付确认、发货确认三个步骤。\n\n" +
		generateSubstantivePrefix(200)
	if containsSelfConfirmationPattern(input) {
		t.Error("expected false for 确认 in requirement text (non-request context)")
	}
}

// ---------------------------------------------------------------------------
// Additional edge case tests
// ---------------------------------------------------------------------------

func TestContainsSelfConfirmationPattern_ChineseConfirmWanBi(t *testing.T) {
	// Chinese: 请确认 + 确认完毕 → returns true
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上设计方案。\n\n好的，确认完毕！开始生成代码..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for 确认完毕 pattern")
	}
}

func TestContainsSelfConfirmationPattern_EnglishLetMeStart(t *testing.T) {
	// English: please confirm + let me start → returns true
	input := generateSubstantivePrefix(300) +
		"\n\nPlease confirm the above design.\n\nLet me start the implementation now..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for 'let me start' pattern")
	}
}

func TestContainsSelfConfirmationPattern_ChineseKaiShiShengCheng(t *testing.T) {
	// Chinese: 请确认 + 开始生成 → returns true
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上逐页脚本。\n\n好的，开始生成PPT..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for 开始生成 pattern")
	}
}

func TestContainsSelfConfirmationPattern_EnglishMovingOnTo(t *testing.T) {
	// English: confirm or suggest + moving on to → returns true
	input := generateSubstantivePrefix(300) +
		"\n\nConfirm or suggest changes.\n\nMoving on to the next phase..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for 'moving on to' pattern")
	}
}

func TestContainsSelfConfirmationPattern_ChineseShouDaoQueRen(t *testing.T) {
	// Chinese: 请确认 + 收到确认 → returns true
	input := generateSubstantivePrefix(300) +
		"\n\n请确认以上内容。\n\n好的，收到确认。现在进入最终阶段..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for 收到确认 pattern")
	}
}

func TestContainsSelfConfirmationPattern_EmptyString(t *testing.T) {
	// Empty string → returns false
	if containsSelfConfirmationPattern("") {
		t.Error("expected false for empty string")
	}
}

func TestContainsSelfConfirmationPattern_OnlyConfirmRequest_NoFollowUp(t *testing.T) {
	// Only confirmation request, nothing after → returns false
	input := generateSubstantivePrefix(300) + "\n\n请确认以上内容。"
	if containsSelfConfirmationPattern(input) {
		t.Error("expected false when confirmation request has no self-answer after it")
	}
}

func TestContainsSelfConfirmationPattern_SelfAnswerBeforeRequest(t *testing.T) {
	// Self-answer text appears BEFORE the confirmation request → returns false
	// (the function should only check text AFTER the request)
	input := "已确认之前的方案。" + generateSubstantivePrefix(300) +
		"\n\n请确认以上新的内容，或提出修改意见。"
	if containsSelfConfirmationPattern(input) {
		t.Error("expected false when self-answer appears before the confirmation request")
	}
}

func TestContainsSelfConfirmationPattern_ChineseJinRuZuiZhong(t *testing.T) {
	// Chinese: 确认后我将 + 进入最终 → returns true
	input := generateSubstantivePrefix(300) +
		"\n\n确认后我将开始生成PPT。\n\n好的，进入最终阶段..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for 进入最终 pattern")
	}
}

func TestContainsSelfConfirmationPattern_EnglishNowEntering(t *testing.T) {
	// English: please review and confirm + now entering → returns true
	input := generateSubstantivePrefix(300) +
		"\n\nPlease review and confirm the above.\n\nNow entering the final phase..."
	if !containsSelfConfirmationPattern(input) {
		t.Error("expected true for 'now entering' pattern")
	}
}
