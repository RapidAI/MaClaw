package main

import "strings"

type anthropicStreamEventType string

const (
	anthropicStreamEventUnknown           anthropicStreamEventType = ""
	anthropicStreamEventMessageStart      anthropicStreamEventType = "message_start"
	anthropicStreamEventContentBlockStart anthropicStreamEventType = "content_block_start"
	anthropicStreamEventContentBlockDelta anthropicStreamEventType = "content_block_delta"
	anthropicStreamEventMessageDelta      anthropicStreamEventType = "message_delta"
	anthropicStreamEventMessageStop       anthropicStreamEventType = "message_stop"
)

func normalizeAnthropicStreamEventType(eventType string) anthropicStreamEventType {
	switch anthropicStreamEventType(strings.TrimSpace(eventType)) {
	case anthropicStreamEventMessageStart:
		return anthropicStreamEventMessageStart
	case anthropicStreamEventContentBlockStart:
		return anthropicStreamEventContentBlockStart
	case anthropicStreamEventContentBlockDelta:
		return anthropicStreamEventContentBlockDelta
	case anthropicStreamEventMessageDelta:
		return anthropicStreamEventMessageDelta
	case anthropicStreamEventMessageStop:
		return anthropicStreamEventMessageStop
	default:
		return anthropicStreamEventUnknown
	}
}
