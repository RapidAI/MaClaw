package guiapp

import "github.com/RapidAI/CodeClaw/corelib/agentruntime"

// The platform kind vocabulary and its methods live in corelib/agentruntime
// so every host (GUI, srv, TUI) shares one channel-governance definition.
// These aliases keep existing GUI call sites unchanged.
type imMessagePlatformKind = agentruntime.IMMessagePlatformKind

const (
	imMessagePlatformUnknown         = agentruntime.IMMessagePlatformUnknown
	imMessagePlatformDesktop         = agentruntime.IMMessagePlatformDesktop
	imMessagePlatformTUI             = agentruntime.IMMessagePlatformTUI
	imMessagePlatformFeishu          = agentruntime.IMMessagePlatformFeishu
	imMessagePlatformWecom           = agentruntime.IMMessagePlatformWecom
	imMessagePlatformQQBot           = agentruntime.IMMessagePlatformQQBot
	imMessagePlatformQQBotLocal      = agentruntime.IMMessagePlatformQQBotLocal
	imMessagePlatformDingTalk        = agentruntime.IMMessagePlatformDingTalk
	imMessagePlatformTelegram        = agentruntime.IMMessagePlatformTelegram
	imMessagePlatformTelegramLocal   = agentruntime.IMMessagePlatformTelegramLocal
	imMessagePlatformWeixin          = agentruntime.IMMessagePlatformWeixin
	imMessagePlatformWeixinLocal     = agentruntime.IMMessagePlatformWeixinLocal
	imMessagePlatformLansenger       = agentruntime.IMMessagePlatformLansenger
	imMessagePlatformLansengerLocal  = agentruntime.IMMessagePlatformLansengerLocal
	imMessagePlatformVEGroupExecutor = agentruntime.IMMessagePlatformVEGroupExecutor
	imMessagePlatformScheduler       = agentruntime.IMMessagePlatformScheduler
)

func normalizeIMMessagePlatformKind(value string) imMessagePlatformKind {
	return agentruntime.NormalizeIMMessagePlatformKind(value)
}
