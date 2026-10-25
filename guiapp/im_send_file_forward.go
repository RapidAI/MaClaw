package guiapp

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
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
	return agent.SendFileForwardsToIM(args)
}

// sendFileDestinationArg reads the destination tool arg.
// ok=false when missing or empty.
func sendFileDestinationArg(args map[string]interface{}) (string, bool) {
	return agent.SendFileDestinationArg(args)
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
	agent.ForceSendFileToIM(args)
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
