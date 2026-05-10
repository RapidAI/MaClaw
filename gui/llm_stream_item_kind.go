package main

import "strings"

type responsesOutputItemKind string

const (
	responsesOutputItemUnknown      responsesOutputItemKind = ""
	responsesOutputItemFunctionCall responsesOutputItemKind = "function_call"
)

func normalizeResponsesOutputItemKind(kind string) responsesOutputItemKind {
	switch responsesOutputItemKind(strings.TrimSpace(kind)) {
	case responsesOutputItemFunctionCall:
		return responsesOutputItemFunctionCall
	default:
		return responsesOutputItemUnknown
	}
}

type anthropicContentBlockKind string

const (
	anthropicContentBlockUnknown anthropicContentBlockKind = ""
	anthropicContentBlockText    anthropicContentBlockKind = "text"
	anthropicContentBlockToolUse anthropicContentBlockKind = "tool_use"
)

func normalizeAnthropicContentBlockKind(kind string) anthropicContentBlockKind {
	switch anthropicContentBlockKind(strings.TrimSpace(kind)) {
	case anthropicContentBlockText:
		return anthropicContentBlockText
	case anthropicContentBlockToolUse:
		return anthropicContentBlockToolUse
	default:
		return anthropicContentBlockUnknown
	}
}

type anthropicDeltaKind string

const (
	anthropicDeltaUnknown   anthropicDeltaKind = ""
	anthropicDeltaText      anthropicDeltaKind = "text_delta"
	anthropicDeltaInputJSON anthropicDeltaKind = "input_json_delta"
)

func normalizeAnthropicDeltaKind(kind string) anthropicDeltaKind {
	switch anthropicDeltaKind(strings.TrimSpace(kind)) {
	case anthropicDeltaText:
		return anthropicDeltaText
	case anthropicDeltaInputJSON:
		return anthropicDeltaInputJSON
	default:
		return anthropicDeltaUnknown
	}
}
