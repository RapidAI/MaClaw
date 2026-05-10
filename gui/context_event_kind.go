package main

import "strings"

type contextEventKind string

const (
	contextEventUnknown        contextEventKind = ""
	contextEventCommandExecute contextEventKind = "command.execute"
)

func normalizeContextEventKind(eventType string) contextEventKind {
	switch contextEventKind(strings.TrimSpace(eventType)) {
	case contextEventCommandExecute:
		return contextEventCommandExecute
	default:
		return contextEventUnknown
	}
}
