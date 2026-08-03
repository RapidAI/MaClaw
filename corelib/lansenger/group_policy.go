package lansenger

import "strings"

// GroupPolicy controls which group messages the bot may answer.
// Values align with the OpenClaw Lansenger channel / 蓝信机器人文档:
//
//	open      — all joined groups (except local ignore list)
//	allowlist — only groups listed in AllowedGroupIDs
//	disabled  — never answer group messages
const (
	GroupPolicyOpen      = "open"
	GroupPolicyAllowlist = "allowlist"
	GroupPolicyDisabled  = "disabled"
)

// GroupChatOptions is the effective group-chat policy for one bot account.
type GroupChatOptions struct {
	// Policy is open | allowlist | disabled. Empty defaults to open.
	Policy string
	// RequireMention when true (default) requires @bot (or @all when RespondToAtAll).
	// When false, every group message can trigger the agent.
	RequireMention bool
	// RespondToAtAll treats @所有人 as a valid mention when RequireMention is true.
	RespondToAtAll bool
	// AutoMentionReply causes replies to @ the original sender via Reminder.
	AutoMentionReply bool
	// AutoQuoteReply attaches RefMsgID for a native quote of the inbound message.
	AutoQuoteReply bool
	// AllowedGroupIDs is used when Policy is allowlist.
	AllowedGroupIDs []string
	// IgnoredGroupIDs always blocks agent handling (bot stays in the group).
	IgnoredGroupIDs []string
	// AppID is used to match MentionedBots when IsAtMe is not set.
	AppID string
}

// NormalizeGroupPolicy returns a canonical policy string.
func NormalizeGroupPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case GroupPolicyAllowlist, "allow", "whitelist":
		return GroupPolicyAllowlist
	case GroupPolicyDisabled, "off", "none":
		return GroupPolicyDisabled
	default:
		return GroupPolicyOpen
	}
}

// IsGroupChat reports whether chatType identifies a group conversation.
// Matching is case-insensitive and trims whitespace.
func IsGroupChat(chatType string) bool {
	return strings.EqualFold(strings.TrimSpace(chatType), "group")
}

// NormalizeChatType returns a canonical "group" | "p2p" (or lowercased raw value).
func NormalizeChatType(chatType string) string {
	switch strings.ToLower(strings.TrimSpace(chatType)) {
	case "group":
		return "group"
	case "p2p", "private", "dm", "direct":
		return "p2p"
	default:
		return strings.ToLower(strings.TrimSpace(chatType))
	}
}

// GroupMessageAllowed reports whether a group inbound message should enter the
// agent / survey pipeline. Non-group messages always return true.
// Watch/盯人 may still observe messages separately.
func GroupMessageAllowed(msg IncomingMessage, opts GroupChatOptions) (allowed bool, reason string) {
	if !IsGroupChat(msg.ChatType) {
		return true, ""
	}
	policy := NormalizeGroupPolicy(opts.Policy)
	if policy == GroupPolicyDisabled {
		return false, "group_policy_disabled"
	}
	groupID := strings.TrimSpace(msg.GroupID)
	if groupID != "" && containsTrimmedID(opts.IgnoredGroupIDs, groupID) {
		return false, "group_ignored"
	}
	if policy == GroupPolicyAllowlist {
		if groupID == "" || !containsTrimmedID(opts.AllowedGroupIDs, groupID) {
			return false, "group_not_in_allowlist"
		}
	}
	if !opts.RequireMention {
		return true, ""
	}
	if GroupMessageMentionsBot(msg, opts.AppID) {
		return true, ""
	}
	if opts.RespondToAtAll && msg.IsAtAll {
		return true, ""
	}
	return false, "require_mention"
}

// GroupMessageMentionsBot matches structured mention metadata from the gateway.
// App IDs are often composite (org-bot); reminder.botId may be the suffix only.
func GroupMessageMentionsBot(msg IncomingMessage, appID string) bool {
	if msg.IsAtMe {
		return true
	}
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return false
	}
	candidates := map[string]struct{}{strings.ToLower(appID): {}}
	if separator := strings.IndexAny(appID, "-_:."); separator >= 0 && separator+1 < len(appID) {
		candidates[strings.ToLower(strings.TrimSpace(appID[separator+1:]))] = struct{}{}
	}
	for _, mentioned := range msg.MentionedBots {
		if _, ok := candidates[strings.ToLower(strings.TrimSpace(mentioned.ID))]; ok {
			return true
		}
	}
	return false
}

// BuildReplyDecorations returns Reminder / RefMsgID decorations for an outbound
// reply based on the inbound message that is being answered.
//
// systemNotice should be true for status/fallback messages (hub unavailable,
// LLM not configured, etc.) so we never @mention or native-quote those.
func BuildReplyDecorations(msg IncomingMessage, opts GroupChatOptions) (reminder *OutgoingReminder, refMsgID string) {
	return BuildReplyDecorationsEx(msg, opts, false)
}

// BuildReplyDecorationsEx is BuildReplyDecorations with an explicit system-notice flag.
// AutoMentionReply is intentionally limited to group messages: private chats
// already target the recipient directly and must remain free of @mentions.
func BuildReplyDecorationsEx(msg IncomingMessage, opts GroupChatOptions, systemNotice bool) (reminder *OutgoingReminder, refMsgID string) {
	if systemNotice {
		return nil, ""
	}
	if IsGroupChat(msg.ChatType) && opts.AutoMentionReply {
		if sender := strings.TrimSpace(msg.FromUserID); sender != "" {
			reminder = &OutgoingReminder{UserIDs: []string{sender}}
		}
	}
	if opts.AutoQuoteReply {
		refMsgID = strings.TrimSpace(msg.MessageID)
	}
	return reminder, refMsgID
}

// PreferNativeGroupQuote reports whether text-based "xx问：" quotes should be
// skipped because a native RefMsgID quote will be attached instead.
func PreferNativeGroupQuote(opts GroupChatOptions, refMsgID string) bool {
	return opts.AutoQuoteReply && strings.TrimSpace(refMsgID) != ""
}

func containsTrimmedID(ids []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == want {
			return true
		}
	}
	return false
}
