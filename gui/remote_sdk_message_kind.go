package main

import "strings"

type remoteSDKMessageTypeKind string

const (
	remoteSDKMessageTypeUnknown              remoteSDKMessageTypeKind = ""
	remoteSDKMessageTypeSystem               remoteSDKMessageTypeKind = "system"
	remoteSDKMessageTypeAssistant            remoteSDKMessageTypeKind = "assistant"
	remoteSDKMessageTypeUser                 remoteSDKMessageTypeKind = "user"
	remoteSDKMessageTypeResult               remoteSDKMessageTypeKind = "result"
	remoteSDKMessageTypeStreamEvent          remoteSDKMessageTypeKind = "stream_event"
	remoteSDKMessageTypeControlRequest       remoteSDKMessageTypeKind = "control_request"
	remoteSDKMessageTypeControlCancelRequest remoteSDKMessageTypeKind = "control_cancel_request"
)

func normalizeRemoteSDKMessageTypeKind(value string) remoteSDKMessageTypeKind {
	switch remoteSDKMessageTypeKind(strings.TrimSpace(value)) {
	case remoteSDKMessageTypeSystem:
		return remoteSDKMessageTypeSystem
	case remoteSDKMessageTypeAssistant:
		return remoteSDKMessageTypeAssistant
	case remoteSDKMessageTypeUser:
		return remoteSDKMessageTypeUser
	case remoteSDKMessageTypeResult:
		return remoteSDKMessageTypeResult
	case remoteSDKMessageTypeStreamEvent:
		return remoteSDKMessageTypeStreamEvent
	case remoteSDKMessageTypeControlRequest:
		return remoteSDKMessageTypeControlRequest
	case remoteSDKMessageTypeControlCancelRequest:
		return remoteSDKMessageTypeControlCancelRequest
	default:
		return remoteSDKMessageTypeUnknown
	}
}

type remoteSDKMessageSubtypeKind string

const (
	remoteSDKMessageSubtypeUnknown  remoteSDKMessageSubtypeKind = ""
	remoteSDKMessageSubtypeInit     remoteSDKMessageSubtypeKind = "init"
	remoteSDKMessageSubtypeAPIRetry remoteSDKMessageSubtypeKind = "api_retry"
)

func normalizeRemoteSDKMessageSubtypeKind(value string) remoteSDKMessageSubtypeKind {
	switch remoteSDKMessageSubtypeKind(strings.TrimSpace(value)) {
	case remoteSDKMessageSubtypeInit:
		return remoteSDKMessageSubtypeInit
	case remoteSDKMessageSubtypeAPIRetry:
		return remoteSDKMessageSubtypeAPIRetry
	default:
		return remoteSDKMessageSubtypeUnknown
	}
}

type remoteSDKContentBlockTypeKind string

const (
	remoteSDKContentBlockUnknown    remoteSDKContentBlockTypeKind = ""
	remoteSDKContentBlockText       remoteSDKContentBlockTypeKind = "text"
	remoteSDKContentBlockThinking   remoteSDKContentBlockTypeKind = "thinking"
	remoteSDKContentBlockToolUse    remoteSDKContentBlockTypeKind = "tool_use"
	remoteSDKContentBlockToolResult remoteSDKContentBlockTypeKind = "tool_result"
	remoteSDKContentBlockImage      remoteSDKContentBlockTypeKind = "image"
)

func normalizeRemoteSDKContentBlockTypeKind(value string) remoteSDKContentBlockTypeKind {
	switch remoteSDKContentBlockTypeKind(strings.TrimSpace(value)) {
	case remoteSDKContentBlockText:
		return remoteSDKContentBlockText
	case remoteSDKContentBlockThinking:
		return remoteSDKContentBlockThinking
	case remoteSDKContentBlockToolUse:
		return remoteSDKContentBlockToolUse
	case remoteSDKContentBlockToolResult:
		return remoteSDKContentBlockToolResult
	case remoteSDKContentBlockImage:
		return remoteSDKContentBlockImage
	default:
		return remoteSDKContentBlockUnknown
	}
}

type remoteSDKStreamDeltaTypeKind string

const (
	remoteSDKStreamDeltaUnknown  remoteSDKStreamDeltaTypeKind = ""
	remoteSDKStreamDeltaText     remoteSDKStreamDeltaTypeKind = "text_delta"
	remoteSDKStreamDeltaThinking remoteSDKStreamDeltaTypeKind = "thinking_delta"
)

func normalizeRemoteSDKStreamDeltaTypeKind(value string) remoteSDKStreamDeltaTypeKind {
	switch remoteSDKStreamDeltaTypeKind(strings.TrimSpace(value)) {
	case remoteSDKStreamDeltaText:
		return remoteSDKStreamDeltaText
	case remoteSDKStreamDeltaThinking:
		return remoteSDKStreamDeltaThinking
	default:
		return remoteSDKStreamDeltaUnknown
	}
}
