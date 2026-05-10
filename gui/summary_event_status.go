package main

import "strings"

type summaryEventType string

const (
	summaryEventUnknown        summaryEventType = ""
	summaryEventSessionInit    summaryEventType = "session.init"
	summaryEventFileRead       summaryEventType = "file.read"
	summaryEventFileChange     summaryEventType = "file.change"
	summaryEventFileCreate     summaryEventType = "file.create"
	summaryEventFileDelete     summaryEventType = "file.delete"
	summaryEventCommandStarted summaryEventType = "command.started"
	summaryEventCommandSuccess summaryEventType = "command.success"
	summaryEventCommandFailed  summaryEventType = "command.failed"
	summaryEventTaskCompleted  summaryEventType = "task.completed"
	summaryEventInputRequired  summaryEventType = "input.required"
	summaryEventSessionError   summaryEventType = "session.error"
	summaryEventSessionFailed  summaryEventType = "session.failed"
	summaryEventSessionClosed  summaryEventType = "session.closed"
)

func normalizeSummaryEventType(eventType string) summaryEventType {
	switch summaryEventType(strings.TrimSpace(eventType)) {
	case summaryEventSessionInit:
		return summaryEventSessionInit
	case summaryEventFileRead:
		return summaryEventFileRead
	case summaryEventFileChange:
		return summaryEventFileChange
	case summaryEventFileCreate:
		return summaryEventFileCreate
	case summaryEventFileDelete:
		return summaryEventFileDelete
	case summaryEventCommandStarted:
		return summaryEventCommandStarted
	case summaryEventCommandSuccess:
		return summaryEventCommandSuccess
	case summaryEventCommandFailed:
		return summaryEventCommandFailed
	case summaryEventTaskCompleted:
		return summaryEventTaskCompleted
	case summaryEventInputRequired:
		return summaryEventInputRequired
	case summaryEventSessionError:
		return summaryEventSessionError
	case summaryEventSessionFailed:
		return summaryEventSessionFailed
	case summaryEventSessionClosed:
		return summaryEventSessionClosed
	default:
		return summaryEventUnknown
	}
}

func (eventType summaryEventType) String() string {
	return string(eventType)
}

func (eventType summaryEventType) IsFileEvent() bool {
	return eventType == summaryEventFileRead || eventType == summaryEventFileChange
}

func (eventType summaryEventType) IsFileRead() bool {
	return eventType == summaryEventFileRead
}

func (eventType summaryEventType) IsFileChange() bool {
	return eventType == summaryEventFileChange
}

func (eventType summaryEventType) IsFileMutationEvent() bool {
	return eventType == summaryEventFileChange || eventType == summaryEventFileCreate || eventType == summaryEventFileDelete
}

func (eventType summaryEventType) IsCommandStarted() bool {
	return eventType == summaryEventCommandStarted
}

type summarySeverity string

const (
	summarySeverityInfo  summarySeverity = "info"
	summarySeverityWarn  summarySeverity = "warn"
	summarySeverityError summarySeverity = "error"
)

func normalizeSummarySeverity(severity string) summarySeverity {
	switch summarySeverity(strings.ToLower(strings.TrimSpace(severity))) {
	case summarySeverityError:
		return summarySeverityError
	case summarySeverityWarn:
		return summarySeverityWarn
	default:
		return summarySeverityInfo
	}
}
