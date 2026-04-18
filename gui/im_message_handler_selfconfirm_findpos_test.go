package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests for findConfirmationRequestPos
//
// Feature: workflow-self-confirm-bypass
// Task: 3.1 — findConfirmationRequestPos implementation
// ---------------------------------------------------------------------------

func TestFindConfirmationRequestPos_ChinesePatterns(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantGe0 bool   // expect position >= 0
		wantSub string // substring at the returned position should start with this
	}{
		{
			name:    "请确认 pattern",
			input:   "以上是需求文档。\n\n请确认以上需求，或提出修改意见。",
			wantGe0: true,
			wantSub: "请确认",
		},
		{
			name:    "请输入：确认 (full-width colon)",
			input:   "以上是全部内容。\n\n请输入：确认 或 修改意见",
			wantGe0: true,
			wantSub: "请输入：确认",
		},
		{
			name:    "请输入: 确认 (half-width colon with space)",
			input:   "文档内容结束。\n\n请输入: 确认",
			wantGe0: true,
			wantSub: "请输入: 确认",
		},
		{
			name:    "请查看并确认 pattern",
			input:   "设计方案如上。\n\n请查看并确认以上设计方案。",
			wantGe0: true,
			wantSub: "请查看并确认",
		},
		{
			name:    "确认后我将 pattern",
			input:   "以上是逐页脚本。确认后我将开始生成PPT。",
			wantGe0: true,
			wantSub: "确认后我将",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := findConfirmationRequestPos(tc.input)
			if tc.wantGe0 && pos < 0 {
				t.Fatalf("findConfirmationRequestPos returned -1, want >= 0 for input containing %q", tc.wantSub)
			}
			if tc.wantGe0 && !strings.HasPrefix(tc.input[pos:], tc.wantSub) {
				t.Errorf("position %d does not start with %q; got %q", pos, tc.wantSub, tc.input[pos:pos+min(len(tc.wantSub)+10, len(tc.input)-pos)])
			}
		})
	}
}

func TestFindConfirmationRequestPos_EnglishPatterns(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantGe0 bool
		wantSub string
	}{
		{
			name:    "please confirm (lowercase)",
			input:   "Above are the requirements.\n\nPlease confirm the above, or suggest changes.",
			wantGe0: true,
			wantSub: "Please confirm",
		},
		{
			name:    "please confirm (mixed case)",
			input:   "Here is the design.\n\nPLEASE CONFIRM or let me know.",
			wantGe0: true,
			wantSub: "PLEASE CONFIRM",
		},
		{
			name:    "please review and confirm",
			input:   "The task list is ready.\n\nPlease review and confirm the tasks.",
			wantGe0: true,
			wantSub: "Please review and confirm",
		},
		{
			name:    "confirm or suggest",
			input:   "Requirements document complete.\n\nConfirm or suggest changes.",
			wantGe0: true,
			wantSub: "Confirm or suggest",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := findConfirmationRequestPos(tc.input)
			if tc.wantGe0 && pos < 0 {
				t.Fatalf("findConfirmationRequestPos returned -1, want >= 0 for input containing %q", tc.wantSub)
			}
			if tc.wantGe0 {
				got := tc.input[pos:]
				if !strings.HasPrefix(strings.ToLower(got), strings.ToLower(tc.wantSub)) {
					t.Errorf("position %d does not start with %q (case-insensitive); got %q", pos, tc.wantSub, got[:min(len(tc.wantSub)+10, len(got))])
				}
			}
		})
	}
}

func TestFindConfirmationRequestPos_NoMatch(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{
			name:  "plain text without any confirmation pattern",
			input: "这是一段普通的文档内容，不包含任何确认请求。",
		},
		{
			name:  "English text without confirmation pattern",
			input: "This is a regular document without any confirmation request.",
		},
		{
			name:  "empty string",
			input: "",
		},
		{
			name:  "确认 in non-request context: 用户确认功能",
			input: "用户确认功能需求后系统自动生成报告。管理员确认审批流程。",
		},
		{
			name:  "确认 in non-request context: 确认按钮样式",
			input: "确认按钮样式采用蓝色圆角设计，取消按钮采用灰色。",
		},
		{
			name:  "确认 in non-request context: 订单确认流程",
			input: "订单确认流程包括身份确认、支付确认、发货确认三个步骤。",
		},
		{
			name:  "确认 in past tense: 已确认技术方案",
			input: "经过团队讨论，已确认技术方案采用微服务架构。数据库确认使用 PostgreSQL。",
		},
		{
			name:  "English confirm in feature description",
			input: "Email confirmation flow sends a confirmation email after registration.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pos := findConfirmationRequestPos(tc.input)
			if pos >= 0 {
				t.Errorf("findConfirmationRequestPos returned %d, want -1 for non-request context; matched at: %q",
					pos, tc.input[pos:min(pos+30, len(tc.input))])
			}
		})
	}
}

func TestFindConfirmationRequestPos_MultipleMatches_ReturnsLast(t *testing.T) {
	// Text with two confirmation requests — should return position of the LAST one.
	input := "第一部分内容。\n\n请确认第一部分。\n\n第二部分内容。\n\n请确认以上全部内容。"

	pos := findConfirmationRequestPos(input)
	if pos < 0 {
		t.Fatal("findConfirmationRequestPos returned -1, want >= 0")
	}

	// The last "请确认" should be the one before "以上全部内容"
	remaining := input[pos:]
	if !strings.Contains(remaining, "以上全部内容") {
		t.Errorf("expected last match to be near '以上全部内容', but got position %d: %q", pos, remaining[:min(40, len(remaining))])
	}

	// Verify it's not the first match
	firstPos := strings.Index(input, "请确认")
	if pos == firstPos {
		t.Errorf("returned first match position %d instead of last; there are multiple matches", pos)
	}
}

func TestFindConfirmationRequestPos_MultipleMatches_EnglishAndChinese(t *testing.T) {
	// Mixed language with multiple patterns — should return the last one.
	input := "请确认第一部分。\n\nSome content here.\n\nPlease confirm the above."

	pos := findConfirmationRequestPos(input)
	if pos < 0 {
		t.Fatal("findConfirmationRequestPos returned -1, want >= 0")
	}

	remaining := input[pos:]
	if !strings.HasPrefix(remaining, "Please confirm") {
		t.Errorf("expected last match to be 'Please confirm', got: %q", remaining[:min(30, len(remaining))])
	}
}
