package main

import "strings"

type thirdPartyGatewayMessageKind string

const (
	thirdPartyGatewayMessageUnknown thirdPartyGatewayMessageKind = ""
	thirdPartyGatewayMessageText    thirdPartyGatewayMessageKind = "text"
	thirdPartyGatewayMessageImage   thirdPartyGatewayMessageKind = "image"
	thirdPartyGatewayMessageFile    thirdPartyGatewayMessageKind = "file"
	thirdPartyGatewayMessageVoice   thirdPartyGatewayMessageKind = "voice"
	thirdPartyGatewayMessageVideo   thirdPartyGatewayMessageKind = "video"
	thirdPartyGatewayMessageAmbient       thirdPartyGatewayMessageKind = "ambient"
	thirdPartyGatewayMessagePetState      thirdPartyGatewayMessageKind = "pet_state"
	thirdPartyGatewayMessageMeetingResult thirdPartyGatewayMessageKind = "meeting_result"
)

func normalizeThirdPartyGatewayMessageKind(kind string) thirdPartyGatewayMessageKind {
	switch thirdPartyGatewayMessageKind(strings.ToLower(strings.TrimSpace(kind))) {
	case thirdPartyGatewayMessageText:
		return thirdPartyGatewayMessageText
	case thirdPartyGatewayMessageImage:
		return thirdPartyGatewayMessageImage
	case thirdPartyGatewayMessageFile:
		return thirdPartyGatewayMessageFile
	case thirdPartyGatewayMessageVoice:
		return thirdPartyGatewayMessageVoice
	case thirdPartyGatewayMessageVideo:
		return thirdPartyGatewayMessageVideo
	case thirdPartyGatewayMessageAmbient:
		return thirdPartyGatewayMessageAmbient
	case thirdPartyGatewayMessagePetState:
		return thirdPartyGatewayMessagePetState
	case thirdPartyGatewayMessageMeetingResult:
		return thirdPartyGatewayMessageMeetingResult
	default:
		return thirdPartyGatewayMessageUnknown
	}
}

func (kind thirdPartyGatewayMessageKind) String() string {
	return string(kind)
}

func (kind thirdPartyGatewayMessageKind) IsMediaFile() bool {
	switch kind {
	case thirdPartyGatewayMessageFile, thirdPartyGatewayMessageVoice, thirdPartyGatewayMessageVideo:
		return true
	default:
		return false
	}
}

func (kind thirdPartyGatewayMessageKind) IsSupported() bool {
	return kind != thirdPartyGatewayMessageUnknown
}

type thirdPartyGatewayAckStatus string

const (
	thirdPartyGatewayAckDelivered thirdPartyGatewayAckStatus = "delivered"
)

func normalizeThirdPartyGatewayAckStatus(status thirdPartyGatewayAckStatus) thirdPartyGatewayAckStatus {
	normalized := strings.TrimSpace(status.String())
	if normalized == "" {
		return thirdPartyGatewayAckDelivered
	}
	return thirdPartyGatewayAckStatus(normalized)
}

func (status thirdPartyGatewayAckStatus) String() string {
	return string(status)
}
