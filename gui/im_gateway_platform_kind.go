package main

import "strings"

type imGatewayPlatformKind string

const (
	imGatewayPlatformUnknown     imGatewayPlatformKind = ""
	imGatewayPlatformWeixin      imGatewayPlatformKind = "weixin"
	imGatewayPlatformLansenger   imGatewayPlatformKind = "lansenger"
	imGatewayPlatformTelegram    imGatewayPlatformKind = "telegram"
	imGatewayPlatformQQBotRemote imGatewayPlatformKind = "qqbot_remote"
	imGatewayPlatformThirdParty  imGatewayPlatformKind = "thirdparty"
)

func normalizeIMGatewayPlatformKind(value string) imGatewayPlatformKind {
	switch imGatewayPlatformKind(strings.ToLower(strings.TrimSpace(value))) {
	case imGatewayPlatformWeixin:
		return imGatewayPlatformWeixin
	case imGatewayPlatformLansenger:
		return imGatewayPlatformLansenger
	case imGatewayPlatformTelegram:
		return imGatewayPlatformTelegram
	case imGatewayPlatformQQBotRemote:
		return imGatewayPlatformQQBotRemote
	case imGatewayPlatformThirdParty:
		return imGatewayPlatformThirdParty
	default:
		return imGatewayPlatformUnknown
	}
}

func (kind imGatewayPlatformKind) String() string {
	return string(kind)
}
