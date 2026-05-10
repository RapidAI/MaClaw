package main

import "strings"

type iflowACPMessageType string

const (
	iflowACPMessageUnknown    iflowACPMessageType = ""
	iflowACPMessageAssistant  iflowACPMessageType = "AssistantMessage"
	iflowACPMessageToolCall   iflowACPMessageType = "ToolCallMessage"
	iflowACPMessagePlan       iflowACPMessageType = "PlanMessage"
	iflowACPMessageTaskFinish iflowACPMessageType = "TaskFinishMessage"
)

func normalizeIFlowACPMessageType(messageType string) iflowACPMessageType {
	switch iflowACPMessageType(strings.TrimSpace(messageType)) {
	case iflowACPMessageAssistant:
		return iflowACPMessageAssistant
	case iflowACPMessageToolCall:
		return iflowACPMessageToolCall
	case iflowACPMessagePlan:
		return iflowACPMessagePlan
	case iflowACPMessageTaskFinish:
		return iflowACPMessageTaskFinish
	default:
		return iflowACPMessageUnknown
	}
}
