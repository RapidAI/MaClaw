package main

import "strings"

type hubInboundMessageType string

const (
	hubInboundMessageUnknown                      hubInboundMessageType = ""
	hubInboundMessageError                        hubInboundMessageType = "error"
	hubInboundMessageAuthOK                       hubInboundMessageType = "auth.ok"
	hubInboundMessageSessionStart                 hubInboundMessageType = "session.start"
	hubInboundMessageSessionInput                 hubInboundMessageType = "session.input"
	hubInboundMessageSessionInterrupt             hubInboundMessageType = "session.interrupt"
	hubInboundMessageSessionKill                  hubInboundMessageType = "session.kill"
	hubInboundMessageSessionImageInput            hubInboundMessageType = "session.image_input"
	hubInboundMessageSessionScreenshot            hubInboundMessageType = "session.screenshot"
	hubInboundMessageIMUserMessage                hubInboundMessageType = "im.user_message"
	hubInboundMessageIMCancelSession              hubInboundMessageType = "im.cancel_session"
	hubInboundMessageIMGatewayReply               hubInboundMessageType = "im.gateway_reply"
	hubInboundMessageDeviceGatewayPlaybackReceipt hubInboundMessageType = "im.device_gateway_playback_receipt"
	hubInboundMessageDeviceGatewayDevices         hubInboundMessageType = "im.device_gateway_devices"
	hubInboundMessageGatewayClaimResult           hubInboundMessageType = "im.gateway_claim_result"
	hubInboundMessageNicknameAssigned             hubInboundMessageType = "machine.nickname_assigned"
	hubInboundMessageNotificationPush             hubInboundMessageType = "notification.push"
	hubInboundMessageMobileDigitalEmployeeTask    hubInboundMessageType = "mobile.digital_employee_task"
	hubInboundMessageAck                          hubInboundMessageType = "ack"
	hubInboundMessageVEEvent                      hubInboundMessageType = "ve_event" // sentinel for all ve:* events
)

func normalizeHubInboundMessageType(messageType string) hubInboundMessageType {
	trimmed := strings.TrimSpace(messageType)
	// VE events all start with "ve:"; route them to a single handler
	if strings.HasPrefix(trimmed, "ve:") {
		return hubInboundMessageVEEvent
	}
	switch hubInboundMessageType(trimmed) {
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
	case hubInboundMessageDeviceGatewayPlaybackReceipt:
		return hubInboundMessageDeviceGatewayPlaybackReceipt
	case hubInboundMessageDeviceGatewayDevices:
		return hubInboundMessageDeviceGatewayDevices
	case hubInboundMessageGatewayClaimResult:
		return hubInboundMessageGatewayClaimResult
	case hubInboundMessageNicknameAssigned:
		return hubInboundMessageNicknameAssigned
	case hubInboundMessageNotificationPush:
		return hubInboundMessageNotificationPush
	case hubInboundMessageMobileDigitalEmployeeTask:
		return hubInboundMessageMobileDigitalEmployeeTask
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
