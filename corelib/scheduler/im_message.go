package scheduler

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultIMDeliveryTimeout is the shared deadline for proactive / scheduled IM send.
const DefaultIMDeliveryTimeout = 45 * time.Second

// DefaultIMFileDeliveryTimeout is the shared deadline for IM file upload
// (im_message send_file). Files up to agent.SendFileMaxSize cannot realistically
// upload within the 45s text budget; this matches the lansenger media client's
// own five-minute transfer window.
const DefaultIMFileDeliveryTimeout = 5 * time.Minute

// NormalizeIMMessageAction maps im_message action aliases to canonical values.
// Unknown non-empty strings are returned lowercased; empty stays empty.
func NormalizeIMMessageAction(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "list_targets", "list_groups", "list_im_targets", "list_delivery_targets", "list":
		return "list_targets"
	case "send", "push", "deliver", "notify":
		return "send"
	case "send_file", "sendfile", "upload_file", "upload", "file", "send_media":
		return "send_file"
	default:
		return s
	}
}

// ResolveIMMessageAction returns the effective action for im_message.
// When action is omitted, infers:
//   - send_file if a local file path is present
//   - send  if message text is present
//   - list_targets if only query/filter fields are present
//   - empty otherwise
func ResolveIMMessageAction(args map[string]interface{}) string {
	raw := strings.TrimSpace(argString(args, "action"))
	n := NormalizeIMMessageAction(raw)
	if n == "list_targets" || n == "send" || n == "send_file" {
		return n
	}
	if n != "" {
		return n // unknown verb — leave for caller error
	}
	// Infer when models omit action (common). A file path wins over text so
	// "send this file with a caption" does not degrade to a text-only send.
	if strings.TrimSpace(IMMessageFilePathFromArgs(args)) != "" {
		return "send_file"
	}
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
	action := ResolveIMMessageAction(args)
	return action == "send" || action == "send_file"
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

// IMMessageFilePathFromArgs picks the first non-empty local file path field.
func IMMessageFilePathFromArgs(args map[string]interface{}) string {
	for _, key := range []string{"path", "file_path", "file"} {
		if s := argString(args, key); s != "" {
			return s
		}
	}
	return ""
}

// IMMessageFileNameFromArgs returns the optional display name for send_file.
func IMMessageFileNameFromArgs(args map[string]interface{}) string {
	return argString(args, "file_name")
}

// RunIMMessageTool dispatches list_targets / send / send_file for host tool handlers.
func RunIMMessageTool(args map[string]interface{}, listTargets, send, sendFile func(map[string]interface{}) string) string {
	if listTargets == nil {
		listTargets = func(map[string]interface{}) string { return "list_targets 未实现" }
	}
	if send == nil {
		send = func(map[string]interface{}) string { return "send 未实现" }
	}
	if sendFile == nil {
		sendFile = func(map[string]interface{}) string { return "send_file 未实现" }
	}
	action := ResolveIMMessageAction(args)
	switch action {
	case "list_targets":
		return listTargets(args)
	case "send":
		return send(args)
	case "send_file":
		return sendFile(args)
	case "":
		return "缺少 action：请使用 list_targets、send 或 send_file（发文本可省略 action 并提供 text；发文件提供 path + group_name/group_id/user_id）"
	default:
		return fmt.Sprintf("未知 im_message action: %s（支持: list_targets, send, send_file）", action)
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

// FormatIMMessageSendFileOK builds a stable success observation for send_file.
// summary is channel→targets (e.g. from SummarizeDelivery); size is in bytes.
func FormatIMMessageSendFileOK(summary, name string, size int64) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(unnamed file)"
	}
	if summary == "" {
		summary = "(unknown target)"
	}
	return fmt.Sprintf("已发送文件到 %s\n文件名: %s\n大小: %d 字节", summary, name, size)
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
	case DeliveryChannelLansenger, "lansengerlocal", "lanxin", "lanxinlocal", "lx", "lan":
		return DeliveryChannelLansenger
	case DeliveryChannelWeixin, "weixinlocal", "wechat", "wechatlocal", "wx", "wecom":
		return DeliveryChannelWeixin
	case DeliveryChannelTelegram, "telegramlocal", "tg":
		return DeliveryChannelTelegram
	case DeliveryChannelQQ, "qqlocal", "qqbot", "qqbotlocal":
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
