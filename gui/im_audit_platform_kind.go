package main

import "strings"

type imAuditPlatformKind string

const (
	imAuditPlatformUnknown    imAuditPlatformKind = ""
	imAuditPlatformQQ         imAuditPlatformKind = "qq"
	imAuditPlatformTelegram   imAuditPlatformKind = "telegram"
	imAuditPlatformWeixin     imAuditPlatformKind = "weixin"
	imAuditPlatformLansenger  imAuditPlatformKind = "lansenger"
	imAuditPlatformThirdParty imAuditPlatformKind = "thirdparty"
)

func normalizeIMAuditPlatformKind(platform string) imAuditPlatformKind {
	trimmed := strings.ToLower(strings.TrimSpace(platform))
	if strings.HasPrefix(trimmed, imAuditPlatformThirdParty.String()+":") {
		return imAuditPlatformThirdParty
	}
	switch normalizeIMMessagePlatformKind(trimmed) {
	case imMessagePlatformQQBot, imMessagePlatformQQBotLocal:
		return imAuditPlatformQQ
	case imMessagePlatformTelegram, imMessagePlatformTelegramLocal:
		return imAuditPlatformTelegram
	case imMessagePlatformWeixin, imMessagePlatformWeixinLocal:
		return imAuditPlatformWeixin
	case imMessagePlatformLansenger, imMessagePlatformLansengerLocal:
		return imAuditPlatformLansenger
	default:
		switch imAuditPlatformKind(trimmed) {
		case imAuditPlatformQQ:
			return imAuditPlatformQQ
		case imAuditPlatformTelegram:
			return imAuditPlatformTelegram
		case imAuditPlatformWeixin:
			return imAuditPlatformWeixin
		case imAuditPlatformLansenger:
			return imAuditPlatformLansenger
		case imAuditPlatformThirdParty:
			return imAuditPlatformThirdParty
		default:
			return imAuditPlatformUnknown
		}
	}
}

func (kind imAuditPlatformKind) String() string {
	return string(kind)
}

func (kind imAuditPlatformKind) IsThirdParty() bool {
	return kind == imAuditPlatformThirdParty
}
