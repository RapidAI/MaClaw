package main

import "strings"

type imMessagePlatformKind string

const (
	imMessagePlatformUnknown         imMessagePlatformKind = ""
	imMessagePlatformDesktop         imMessagePlatformKind = "desktop"
	imMessagePlatformTUI             imMessagePlatformKind = "tui"
	imMessagePlatformFeishu          imMessagePlatformKind = "feishu"
	imMessagePlatformWecom           imMessagePlatformKind = "wecom"
	imMessagePlatformQQBot           imMessagePlatformKind = "qqbot"
	imMessagePlatformQQBotLocal      imMessagePlatformKind = "qqbot_local"
	imMessagePlatformDingTalk        imMessagePlatformKind = "dingtalk"
	imMessagePlatformTelegram        imMessagePlatformKind = "telegram"
	imMessagePlatformTelegramLocal   imMessagePlatformKind = "telegram_local"
	imMessagePlatformWeixin          imMessagePlatformKind = "weixin"
	imMessagePlatformWeixinLocal     imMessagePlatformKind = "weixin_local"
	imMessagePlatformLansenger       imMessagePlatformKind = "lansenger"
	imMessagePlatformLansengerLocal  imMessagePlatformKind = "lansenger_local"
	imMessagePlatformVEGroupExecutor imMessagePlatformKind = "ve_group_executor"
)

func normalizeIMMessagePlatformKind(value string) imMessagePlatformKind {
	switch imMessagePlatformKind(strings.ToLower(strings.TrimSpace(value))) {
	case imMessagePlatformDesktop:
		return imMessagePlatformDesktop
	case imMessagePlatformTUI:
		return imMessagePlatformTUI
	case imMessagePlatformFeishu:
		return imMessagePlatformFeishu
	case imMessagePlatformWecom:
		return imMessagePlatformWecom
	case imMessagePlatformQQBot:
		return imMessagePlatformQQBot
	case imMessagePlatformQQBotLocal:
		return imMessagePlatformQQBotLocal
	case imMessagePlatformDingTalk:
		return imMessagePlatformDingTalk
	case imMessagePlatformTelegram:
		return imMessagePlatformTelegram
	case imMessagePlatformTelegramLocal:
		return imMessagePlatformTelegramLocal
	case imMessagePlatformWeixin:
		return imMessagePlatformWeixin
	case imMessagePlatformWeixinLocal:
		return imMessagePlatformWeixinLocal
	case imMessagePlatformLansenger:
		return imMessagePlatformLansenger
	case imMessagePlatformLansengerLocal:
		return imMessagePlatformLansengerLocal
	case imMessagePlatformVEGroupExecutor:
		return imMessagePlatformVEGroupExecutor
	default:
		return imMessagePlatformUnknown
	}
}

func (kind imMessagePlatformKind) String() string {
	return string(kind)
}

func (kind imMessagePlatformKind) IsDesktop() bool {
	return kind == imMessagePlatformDesktop || kind == imMessagePlatformVEGroupExecutor
}

func (kind imMessagePlatformKind) IsKnown() bool {
	return kind != imMessagePlatformUnknown
}

func (kind imMessagePlatformKind) IsDesktopPlaybackTarget() bool {
	return kind == imMessagePlatformUnknown || kind == imMessagePlatformDesktop || kind == imMessagePlatformTUI
}

func (kind imMessagePlatformKind) IsIMChannel() bool {
	switch kind {
	case imMessagePlatformFeishu,
		imMessagePlatformWecom,
		imMessagePlatformQQBot,
		imMessagePlatformQQBotLocal,
		imMessagePlatformDingTalk,
		imMessagePlatformTelegram,
		imMessagePlatformTelegramLocal,
		imMessagePlatformWeixin,
		imMessagePlatformWeixinLocal,
		imMessagePlatformLansenger,
		imMessagePlatformLansengerLocal:
		return true
	default:
		return false
	}
}

func (kind imMessagePlatformKind) PrefersAMRVoice() bool {
	return kind == imMessagePlatformWecom
}

func (kind imMessagePlatformKind) PrefersWAVVoice() bool {
	switch kind {
	case imMessagePlatformWeixin, imMessagePlatformWeixinLocal, imMessagePlatformQQBot, imMessagePlatformQQBotLocal:
		return true
	default:
		return false
	}
}

func (kind imMessagePlatformKind) PrefersOGGVoice() bool {
	switch kind {
	case imMessagePlatformFeishu, imMessagePlatformTelegram, imMessagePlatformTelegramLocal, imMessagePlatformDingTalk:
		return true
	default:
		return false
	}
}
