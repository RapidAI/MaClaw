package agent

import "strings"

// Send-file delivery is decided from structured args. A desktop destination
// stays in the chat even when forward_to_im is set. An unknown destination
// does not count as a channel; only an explicit forward flag sends it.
const (
	SendFileDestUnknown = iota
	SendFileDestIM
	SendFileDestDesktop
)

// ForceSendFileToIM marks a call as IM delivery. The tool name wins over a
// missing, desktop, or unknown destination. A destination that is already an
// IM channel is left in place.
func ForceSendFileToIM(args map[string]interface{}) {
	if args == nil {
		return
	}
	args["forward_to_im"] = true
	if dest, ok := SendFileDestinationArg(args); !ok || SendFileDestinationKind(dest) != SendFileDestIM {
		args["destination"] = "im"
	}
}

// SendFileForwardsToIM reports whether send_file will leave the chat.
// send_to_im is a different tool: its executor calls ForceSendFileToIM first.
func SendFileForwardsToIM(args map[string]interface{}) bool {
	if dest, ok := SendFileDestinationArg(args); ok {
		switch SendFileDestinationKind(dest) {
		case SendFileDestIM:
			return true
		case SendFileDestDesktop:
			return false
		}
	}
	return commitFlagExact(args, "forward_to_im")
}

// SendFileDestinationArg returns the destination argument, trimmed and
// lowercased. ok is false when it is missing or empty.
func SendFileDestinationArg(args map[string]interface{}) (string, bool) {
	if args == nil {
		return "", false
	}
	raw, exists := args["destination"]
	if !exists || raw == nil {
		return "", false
	}
	text, ok := raw.(string)
	if !ok {
		return "", false
	}
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return "", false
	}
	return text, true
}

// SendFileDestinationKind classifies a destination token.
func SendFileDestinationKind(dest string) int {
	folded := strings.ToLower(strings.TrimSpace(dest))
	raw := strings.TrimSpace(dest)
	switch folded {
	case "im", "wechat", "weixin", "wx", "feishu", "lark", "qq",
		"dingtalk", "ding", "telegram", "tg":
		return SendFileDestIM
	case "chat", "desktop", "local", "ui", "assistant":
		return SendFileDestDesktop
	}
	switch raw {
	case "微信", "飞书", "钉钉", "企微", "企业微信":
		return SendFileDestIM
	case "桌面", "对话", "当前对话":
		return SendFileDestDesktop
	default:
		return SendFileDestUnknown
	}
}

func commitFlagExact(args map[string]interface{}, key string) bool {
	if args == nil {
		return false
	}
	value, exists := args[key]
	if !exists || value == nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	case float64:
		return v != 0
	case int:
		return v != 0
	default:
		return false
	}
}
