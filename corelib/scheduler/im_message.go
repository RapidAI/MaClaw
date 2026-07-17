package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultIMDeliveryTimeout is the shared deadline for proactive / scheduled IM send.
const DefaultIMDeliveryTimeout = 45 * time.Second

// NormalizeIMMessageAction maps im_message action aliases to canonical values.
// Unknown non-empty strings are returned lowercased; empty stays empty.
func NormalizeIMMessageAction(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "list_targets", "list_groups", "list_im_targets", "list_delivery_targets", "list":
		return "list_targets"
	case "send", "push", "deliver", "notify":
		return "send"
	default:
		return s
	}
}

// ResolveIMMessageAction returns the effective action for im_message.
// When action is omitted, infers:
//   - send  if message text is present
//   - list_targets if only query/filter fields are present
//   - empty otherwise
func ResolveIMMessageAction(args map[string]interface{}) string {
	raw := strings.TrimSpace(argString(args, "action"))
	n := NormalizeIMMessageAction(raw)
	if n == "list_targets" || n == "send" {
		return n
	}
	if n != "" {
		return n // unknown verb — leave for caller error
	}
	// Infer when models omit action (common).
	if strings.TrimSpace(IMMessageTextFromArgs(args)) != "" {
		return "send"
	}
	if argString(args, "query") != "" ||
		argString(args, "group_name") != "" ||
		argString(args, "group_id") != "" ||
		argString(args, "user_id") != "" ||
		argString(args, "name") != "" ||
		argString(args, "channel") != "" ||
		argString(args, "platform") != "" {
		return "list_targets"
	}
	return ""
}

// IsIMMessageSendIntent reports whether args mean proactive send (for security gates).
// Uses the same inference as ResolveIMMessageAction so policy cannot be bypassed by omitting action.
func IsIMMessageSendIntent(args map[string]interface{}) bool {
	return ResolveIMMessageAction(args) == "send"
}

// IMMessageTextFromArgs picks the first non-empty message body field.
func IMMessageTextFromArgs(args map[string]interface{}) string {
	for _, key := range []string{"text", "message", "content", "body"} {
		if s := argString(args, key); s != "" {
			return s
		}
	}
	return ""
}

// RunIMMessageTool dispatches list_targets / send for host tool handlers.
func RunIMMessageTool(args map[string]interface{}, listTargets, send func(map[string]interface{}) string) string {
	if listTargets == nil {
		listTargets = func(map[string]interface{}) string { return "list_targets 未实现" }
	}
	if send == nil {
		send = func(map[string]interface{}) string { return "send 未实现" }
	}
	action := ResolveIMMessageAction(args)
	switch action {
	case "list_targets":
		return listTargets(args)
	case "send":
		return send(args)
	case "":
		return "缺少 action：请使用 list_targets 或 send（发送时也可省略 action，并提供 text + group_name/group_id/user_id）"
	default:
		return fmt.Sprintf("未知 im_message action: %s（支持: list_targets, send）", action)
	}
}

// FormatIMMessageSendOK builds a stable success observation for agents.
// original is the pre-truncate body; summary is channel→targets (e.g. from SummarizeDelivery).
func FormatIMMessageSendOK(summary, original string) string {
	sent := TruncateDeliveryBody(original)
	n := len([]rune(sent))
	if summary == "" {
		summary = "(unknown target)"
	}
	msg := fmt.Sprintf("已发送到 %s\n正文长度: %d 字", summary, n)
	if len([]rune(strings.TrimSpace(original))) > n {
		msg += fmt.Sprintf("\n（已按上限 %d 字截断后发送）", MaxDeliveryBodyRunes)
	}
	return msg
}

// CanonicalDeliveryChannel maps human / alias channel names to stable keys.
// Empty input stays empty (use DefaultDeliveryChannel when a default is required).
func CanonicalDeliveryChannel(channel string) string {
	raw := strings.TrimSpace(channel)
	if raw == "" {
		return ""
	}
	// Strip common separators so "蓝信-IM" / "wechat_bot" still match.
	compact := strings.NewReplacer(" ", "", "-", "", "_", "", "　", "").Replace(raw)
	// Localized labels (ToLower is a no-op for CJK).
	switch compact {
	case "蓝信", "蓝信IM", "蓝信im":
		return DeliveryChannelLansenger
	case "微信", "企微", "企业微信":
		return DeliveryChannelWeixin
	case "电报":
		return DeliveryChannelTelegram
	case "QQ", "qq", "qq机器人":
		return DeliveryChannelQQ
	}
	c := strings.ToLower(compact)
	switch c {
	case DeliveryChannelLansenger, "lanxin", "lx", "lan":
		return DeliveryChannelLansenger
	case DeliveryChannelWeixin, "wechat", "wx", "wecom":
		return DeliveryChannelWeixin
	case DeliveryChannelTelegram, "tg":
		return DeliveryChannelTelegram
	case DeliveryChannelQQ, "qqbot":
		return DeliveryChannelQQ
	default:
		// Unknown: keep lowercased original trim (not compact) for forward-compat channels.
		return strings.ToLower(raw)
	}
}

// DefaultDeliveryChannel returns a canonical channel, defaulting to lansenger when empty.
func DefaultDeliveryChannel(channel string) string {
	c := CanonicalDeliveryChannel(channel)
	if c == "" {
		return DeliveryChannelLansenger
	}
	return c
}

// WithDeliveryTimeout ensures ctx has a deadline. The returned cancel is always non-nil (safe to defer).
func WithDeliveryTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		d = DefaultIMDeliveryTimeout
	}
	if ctx == nil {
		return context.WithTimeout(context.Background(), d)
	}
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

func argString(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	switch t := raw.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}
