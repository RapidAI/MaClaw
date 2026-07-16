package lansenger

import (
	"strings"
	"testing"
)

func TestFormatAgentGroupContextSkipsPrivate(t *testing.T) {
	got := FormatAgentGroupContext(IncomingMessage{ChatType: "p2p", Text: "hi", GroupName: "x"}, nil)
	if got != "" {
		t.Fatalf("private chat must not get group context, got %q", got)
	}
}

func TestFormatAgentGroupContextMessageFields(t *testing.T) {
	msg := IncomingMessage{
		ChatType:   "group",
		GroupID:    "g-1",
		GroupName:  "我的机器人测试",
		FromUserID: "staff-1",
		SenderName: "王占一",
		IsAtMe:     true,
		MentionedBots: []MentionedBot{
			{ID: "bot-a", Name: "M-Wiggins"},
			{ID: "bot-b", Name: "Alpha"},
		},
		MentionedStaffs: []MentionedStaff{
			{ID: "s2", Name: "李四"},
		},
		ReferenceText: "昨天的纪要",
	}
	got := FormatAgentGroupContext(msg, nil)
	for _, want := range []string{
		"[群聊上下文]",
		"我的机器人测试",
		"g-1",
		"王占一",
		"staff-1",
		"李四(s2)",
		"M-Wiggins(bot-a)",
		"Alpha(bot-b)",
		"明确 @ 了当前机器人",
		"引用消息: 昨天的纪要",
		"完整成员/机器人名册: 不可用",
		"禁止猜测",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("context missing %q:\n%s", want, got)
		}
	}
	// Without GetGroupInfo, must not invent member counts.
	if strings.Contains(got, "成员数:") {
		t.Fatalf("must not invent member count without GroupInfo:\n%s", got)
	}
}

func TestFormatAgentGroupContextWithGroupInfo(t *testing.T) {
	msg := IncomingMessage{ChatType: "group", GroupID: "g-9", FromUserID: "u1"}
	info := &GroupInfo{
		GroupID:      "g-9",
		Name:         "工程群",
		Description:  "讨论发布节奏",
		TotalMembers: 42,
		MaxMembers:   200,
		OwnerName:    "张三",
		OwnerID:      "owner-1",
		State:        0,
		IsPublic:     true,
	}
	got := FormatAgentGroupContext(msg, info)
	for _, want := range []string{
		"工程群",
		"g-9",
		"群描述: 讨论发布节奏",
		"成员数: 42 / 200",
		"群主: 张三 (staffId=owner-1)",
		"公开群: 是",
		"完整成员/机器人名册: 不可用",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("context missing %q:\n%s", want, got)
		}
	}
}

func TestWithAgentGroupContextPrependsUserText(t *testing.T) {
	msg := IncomingMessage{
		ChatType:  "group",
		GroupID:   "g-1",
		GroupName: "测试群",
	}
	got := WithAgentGroupContext("看下群里还有哪些机器人？", msg, nil)
	if !strings.Contains(got, "[群聊上下文]") {
		t.Fatalf("expected context block, got %q", got)
	}
	if !strings.Contains(got, "用户消息:\n看下群里还有哪些机器人？") {
		t.Fatalf("expected user text section, got %q", got)
	}
	// Private: unchanged.
	priv := WithAgentGroupContext("hello", IncomingMessage{ChatType: "p2p"}, nil)
	if priv != "hello" {
		t.Fatalf("private text changed: %q", priv)
	}
}

func TestFormatAgentGroupContextDisbandedState(t *testing.T) {
	got := FormatAgentGroupContext(
		IncomingMessage{ChatType: "group", GroupID: "g"},
		&GroupInfo{GroupID: "g", Name: "旧群", State: 1},
	)
	if !strings.Contains(got, "已解散") {
		t.Fatalf("expected disbanded state, got %q", got)
	}
}
