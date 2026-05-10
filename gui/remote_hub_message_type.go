package main

import "strings"

type hubInboundMessageType string

const (
	hubInboundMessageUnknown            hubInboundMessageType = ""
	hubInboundMessageError              hubInboundMessageType = "error"
	hubInboundMessageAuthOK             hubInboundMessageType = "auth.ok"
	hubInboundMessageSessionStart       hubInboundMessageType = "session.start"
	hubInboundMessageSessionInput       hubInboundMessageType = "session.input"
	hubInboundMessageSessionInterrupt   hubInboundMessageType = "session.interrupt"
	hubInboundMessageSessionKill        hubInboundMessageType = "session.kill"
	hubInboundMessageSessionImageInput  hubInboundMessageType = "session.image_input"
	hubInboundMessageSessionScreenshot  hubInboundMessageType = "session.screenshot"
	hubInboundMessageIMUserMessage      hubInboundMessageType = "im.user_message"
	hubInboundMessageIMCancelSession    hubInboundMessageType = "im.cancel_session"
	hubInboundMessageIMGatewayReply     hubInboundMessageType = "im.gateway_reply"
	hubInboundMessageGatewayClaimResult hubInboundMessageType = "im.gateway_claim_result"
	hubInboundMessageNicknameAssigned   hubInboundMessageType = "machine.nickname_assigned"
	hubInboundMessageAck                hubInboundMessageType = "ack"
)

func normalizeHubInboundMessageType(messageType string) hubInboundMessageType {
	switch hubInboundMessageType(strings.TrimSpace(messageType)) {
	case hubInboundMessageError:
		return hubInboundMessageError
	case hubInboundMessageAuthOK:
		return hubInboundMessageAuthOK
	case hubInboundMessageSessionStart:
		return hubInboundMessageSessionStart
	case hubInboundMessageSessionInput:
		return hubInboundMessageSessionInput
	case hubInboundMessageSessionInterrupt:
		return hubInboundMessageSessionInterrupt
	case hubInboundMessageSessionKill:
		return hubInboundMessageSessionKill
	case hubInboundMessageSessionImageInput:
		return hubInboundMessageSessionImageInput
	case hubInboundMessageSessionScreenshot:
		return hubInboundMessageSessionScreenshot
	case hubInboundMessageIMUserMessage:
		return hubInboundMessageIMUserMessage
	case hubInboundMessageIMCancelSession:
		return hubInboundMessageIMCancelSession
	case hubInboundMessageIMGatewayReply:
		return hubInboundMessageIMGatewayReply
	case hubInboundMessageGatewayClaimResult:
		return hubInboundMessageGatewayClaimResult
	case hubInboundMessageNicknameAssigned:
		return hubInboundMessageNicknameAssigned
	case hubInboundMessageAck:
		return hubInboundMessageAck
	default:
		return hubInboundMessageUnknown
	}
}

func (messageType hubInboundMessageType) IsError() bool {
	return messageType == hubInboundMessageError
}

func (messageType hubInboundMessageType) IsAuthOK() bool {
	return messageType == hubInboundMessageAuthOK
}
