package lansenger

import (
	"fmt"
	"strings"
)

// AgentGroupContextRules is the hard policy injected with every group-chat
// context block. Models must not invent member/bot rosters when the platform
// has not supplied a full member list.
const AgentGroupContextRules = "" +
	"【群状态数据规则】\n" +
	"- 下列「已确认」信息来自本条消息元数据和/或开放平台群详情 API，可直接引用。\n" +
	"- 完整群成员名单、群内机器人清单当前未提供；禁止猜测、编造或根据命名习惯推断成员/机器人名称与数量。\n" +
	"- 若用户询问「群里还有谁/还有哪些机器人」等依赖名册的问题，应明确说明：当前无法获取完整成员列表，仅能基于下方已确认字段与本条消息中的 @ 信息作答。\n" +
	"- 不要假装已经扫过群成员列表。"

// FormatAgentGroupContext builds a structured prefix for LLM turns on group
// chats. Returns "" for non-group messages. info may be nil when GetGroupInfo
// was skipped or failed — message-level fields are still emitted.
func FormatAgentGroupContext(msg IncomingMessage, info *GroupInfo) string {
	if !strings.EqualFold(strings.TrimSpace(msg.ChatType), "group") {
		return ""
	}

	var b strings.Builder
	b.WriteString("[群聊上下文]\n")

	groupLabel := strings.TrimSpace(msg.GroupName)
	if info != nil {
		if n := strings.TrimSpace(info.Name); n != "" && n != strings.TrimSpace(info.GroupID) {
			groupLabel = firstNonEmpty(groupLabel, n)
		}
	}
	groupID := strings.TrimSpace(msg.GroupID)
	if info != nil && groupID == "" {
		groupID = strings.TrimSpace(info.GroupID)
	}
	switch {
	case groupLabel != "" && groupID != "" && groupLabel != groupID:
		b.WriteString(fmt.Sprintf("- 群: %s (id=%s)\n", groupLabel, groupID))
	case groupLabel != "":
		b.WriteString(fmt.Sprintf("- 群: %s\n", groupLabel))
	case groupID != "":
		b.WriteString(fmt.Sprintf("- 群 id: %s\n", groupID))
	default:
		b.WriteString("- 群: (未知)\n")
	}

	sender := strings.TrimSpace(msg.SenderName)
	staffID := strings.TrimSpace(msg.FromUserID)
	switch {
	case sender != "" && staffID != "" && sender != staffID:
		b.WriteString(fmt.Sprintf("- 发送者: %s (staffId=%s)\n", sender, staffID))
	case sender != "":
		b.WriteString(fmt.Sprintf("- 发送者: %s\n", sender))
	case staffID != "":
		b.WriteString(fmt.Sprintf("- 发送者 staffId: %s\n", staffID))
	}

	if info != nil {
		if d := strings.TrimSpace(info.Description); d != "" {
			b.WriteString(fmt.Sprintf("- 群描述: %s\n", compactOneLine(d, 200)))
		}
		if info.TotalMembers > 0 {
			if info.MaxMembers > 0 {
				b.WriteString(fmt.Sprintf("- 成员数: %d / %d\n", info.TotalMembers, info.MaxMembers))
			} else {
				b.WriteString(fmt.Sprintf("- 成员数: %d\n", info.TotalMembers))
			}
		}
		owner := strings.TrimSpace(info.OwnerName)
		ownerID := strings.TrimSpace(info.OwnerID)
		switch {
		case owner != "" && ownerID != "" && owner != ownerID:
			b.WriteString(fmt.Sprintf("- 群主: %s (staffId=%s)\n", owner, ownerID))
		case owner != "":
			b.WriteString(fmt.Sprintf("- 群主: %s\n", owner))
		case ownerID != "":
			b.WriteString(fmt.Sprintf("- 群主 staffId: %s\n", ownerID))
		}
		switch info.State {
		case 1:
			b.WriteString("- 群状态: 已解散\n")
		case 0:
			// normal — omit noise unless useful; keep silent
		default:
			if info.State != 0 {
				b.WriteString(fmt.Sprintf("- 群状态码: %d\n", info.State))
			}
		}
		if info.IsPublic {
			b.WriteString("- 公开群: 是\n")
		}
	}

	if staffs := formatMentionedStaffs(msg.MentionedStaffs); staffs != "" {
		b.WriteString("- 本消息@的人: ")
		b.WriteString(staffs)
		b.WriteByte('\n')
	}
	if bots := formatMentionedBots(msg.MentionedBots); bots != "" {
		b.WriteString("- 本消息@的机器人: ")
		b.WriteString(bots)
		b.WriteByte('\n')
	}
	if msg.IsAtMe {
		b.WriteString("- 本消息明确 @ 了当前机器人\n")
	}
	if msg.IsAtAll {
		b.WriteString("- 本消息 @所有人\n")
	}
	if ref := strings.TrimSpace(msg.ReferenceText); ref != "" {
		b.WriteString(fmt.Sprintf("- 引用消息: %s\n", compactOneLine(ref, 160)))
	}

	b.WriteString("- 完整成员/机器人名册: 不可用\n")
	b.WriteByte('\n')
	b.WriteString(AgentGroupContextRules)
	return b.String()
}

// WithAgentGroupContext prepends FormatAgentGroupContext to user text for group
// chats. Non-group messages and empty context leave text unchanged.
func WithAgentGroupContext(text string, msg IncomingMessage, info *GroupInfo) string {
	ctx := FormatAgentGroupContext(msg, info)
	if ctx == "" {
		return text
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return ctx
	}
	return ctx + "\n\n用户消息:\n" + text
}

func formatMentionedStaffs(staffs []MentionedStaff) string {
	if len(staffs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(staffs))
	for _, s := range staffs {
		name := strings.TrimSpace(s.Name)
		id := strings.TrimSpace(s.ID)
		switch {
		case name != "" && id != "" && name != id:
			parts = append(parts, fmt.Sprintf("%s(%s)", name, id))
		case name != "":
			parts = append(parts, name)
		case id != "":
			parts = append(parts, id)
		}
	}
	return strings.Join(parts, ", ")
}

func formatMentionedBots(bots []MentionedBot) string {
	if len(bots) == 0 {
		return ""
	}
	parts := make([]string, 0, len(bots))
	for _, bot := range bots {
		name := strings.TrimSpace(bot.Name)
		id := strings.TrimSpace(bot.ID)
		switch {
		case name != "" && id != "" && name != id:
			parts = append(parts, fmt.Sprintf("%s(%s)", name, id))
		case name != "":
			parts = append(parts, name)
		case id != "":
			parts = append(parts, id)
		}
	}
	return strings.Join(parts, ", ")
}

func compactOneLine(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "…"
}
