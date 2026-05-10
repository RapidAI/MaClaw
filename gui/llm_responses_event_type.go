package main

import "strings"

type responsesEventType string

const (
	responsesEventUnknown                    responsesEventType = ""
	responsesEventOutputItemAdded            responsesEventType = "response.output_item.added"
	responsesEventOutputTextDelta            responsesEventType = "response.output_text.delta"
	responsesEventFunctionCallArgumentsDelta responsesEventType = "response.function_call_arguments.delta"
	responsesEventFunctionCallArgumentsDone  responsesEventType = "response.function_call_arguments.done"
	responsesEventOutputItemDone             responsesEventType = "response.output_item.done"
	responsesEventCompleted                  responsesEventType = "response.completed"
	responsesEventFailed                     responsesEventType = "response.failed"
	responsesEventIncomplete                 responsesEventType = "response.incomplete"
	responsesEventError                      responsesEventType = "error"
)

func normalizeResponsesEventType(eventType string) responsesEventType {
	switch responsesEventType(strings.TrimSpace(eventType)) {
	case responsesEventOutputItemAdded:
		return responsesEventOutputItemAdded
	case responsesEventOutputTextDelta:
		return responsesEventOutputTextDelta
	case responsesEventFunctionCallArgumentsDelta:
		return responsesEventFunctionCallArgumentsDelta
	case responsesEventFunctionCallArgumentsDone:
		return responsesEventFunctionCallArgumentsDone
	case responsesEventOutputItemDone:
		return responsesEventOutputItemDone
	case responsesEventCompleted:
		return responsesEventCompleted
	case responsesEventFailed:
		return responsesEventFailed
	case responsesEventIncomplete:
		return responsesEventIncomplete
	case responsesEventError:
		return responsesEventError
	default:
		return responsesEventUnknown
	}
}
