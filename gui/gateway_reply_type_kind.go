package main

import "strings"

type gatewayReplyTypeKind string

const (
	gatewayReplyTypeUnknown gatewayReplyTypeKind = ""
	gatewayReplyTypeText    gatewayReplyTypeKind = "text"
	gatewayReplyTypeImage   gatewayReplyTypeKind = "image"
	gatewayReplyTypeFile    gatewayReplyTypeKind = "file"
	gatewayReplyTypeVoice   gatewayReplyTypeKind = "voice"
	gatewayReplyTypeVideo   gatewayReplyTypeKind = "video"
)

func normalizeGatewayReplyTypeKind(value gatewayReplyTypeKind) gatewayReplyTypeKind {
	switch gatewayReplyTypeKind(strings.ToLower(strings.TrimSpace(value.String()))) {
	case gatewayReplyTypeText:
		return gatewayReplyTypeText
	case gatewayReplyTypeImage:
		return gatewayReplyTypeImage
	case gatewayReplyTypeFile:
		return gatewayReplyTypeFile
	case gatewayReplyTypeVoice:
		return gatewayReplyTypeVoice
	case gatewayReplyTypeVideo:
		return gatewayReplyTypeVideo
	default:
		return gatewayReplyTypeUnknown
	}
}

func (kind gatewayReplyTypeKind) String() string {
	return string(kind)
}
