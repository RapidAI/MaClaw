package main

import (
	"strings"
)

// resolveSendFileForwardToIM decides whether send_file should deliver to IM
// channels. Control plane is structured tool args only — never free-text
// keyword matching on the user message (that path is unstable).
//
// Supported signals:
//   - destination: im|wechat|weixin|feishu|... → forward;
//     chat|desktop|local → do not forward
//   - forward_to_im: true / "true" / 1 / "yes" / "on"
//
// Known desktop destinations override a true flag. Unknown destinations fall
// through to forward_to_im (no guessing from free text).
// Note: do not use "target" here — open() uses target as a path/URL.
func resolveSendFileForwardToIM(args map[string]interface{}) bool {
	if args == nil {
		return false
	}
	if dest, ok := sendFileDestinationArg(args); ok {
		switch destinationKind(dest) {
		case sendFileDestIM:
			return true
		case sendFileDestDesktop:
			return false
		}
		// unknown: fall through
	}
	return boolArg(args, "forward_to_im", false)
}

type sendFileDestKind int

const (
	sendFileDestUnknown sendFileDestKind = iota
	sendFileDestIM
	sendFileDestDesktop
)

func destinationKind(dest string) sendFileDestKind {
	d := strings.ToLower(strings.TrimSpace(dest))
	// Keep original for Chinese tokens (ToLower is a no-op for them).
	raw := strings.TrimSpace(dest)
	switch d {
	case "im", "wechat", "weixin", "wx", "feishu", "lark", "qq",
		"dingtalk", "ding", "telegram", "tg":
		return sendFileDestIM
	case "chat", "desktop", "local", "ui", "assistant":
		return sendFileDestDesktop
	}
	// Models occasionally pass localized channel names as destination.
	switch raw {
	case "微信", "飞书", "钉钉", "企微", "企业微信":
		return sendFileDestIM
	case "桌面", "对话", "当前对话":
		return sendFileDestDesktop
	default:
		return sendFileDestUnknown
	}
}

// sendFileDestinationArg reads the destination tool arg.
// ok=false when missing or empty.
func sendFileDestinationArg(args map[string]interface{}) (string, bool) {
	if args == nil {
		return "", false
	}
	raw, exists := args["destination"]
	if !exists || raw == nil {
		return "", false
	}
	s, ok := raw.(string)
	if !ok {
		return "", false
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", false
	}
	return s, true
}

// applySendFileForwardArgs normalizes structured delivery args onto
// forward_to_im so toolSendFile and logging see a single flag.
// Returns true when forward was resolved on.
func applySendFileForwardArgs(args map[string]interface{}) bool {
	if args == nil {
		return false
	}
	forward := resolveSendFileForwardToIM(args)
	if forward {
		args["forward_to_im"] = true
	}
	return forward
}

// forceSendFileToIMArgs marks a send_file-family call as IM delivery.
// Used by the dedicated send_to_im tool — the tool name is the intent.
func forceSendFileToIMArgs(args map[string]interface{}) {
	if args == nil {
		return
	}
	args["forward_to_im"] = true
	// Tool name wins over a contradictory destination=desktop.
	if dest, ok := sendFileDestinationArg(args); !ok || destinationKind(dest) != sendFileDestIM {
		args["destination"] = "im"
	}
}

// isSendFileFamilyTool is true for tools that stage a file_base64 payload.
func isSendFileFamilyTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "send_file", "send_to_im":
		return true
	default:
		return false
	}
}
