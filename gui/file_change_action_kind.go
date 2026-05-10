package main

type fileChangeActionKind string

const (
	fileChangeActionModify fileChangeActionKind = "modify"
	fileChangeActionCreate fileChangeActionKind = "create"
	fileChangeActionDelete fileChangeActionKind = "delete"
)

func fileChangeActionForEventType(eventType summaryEventType) fileChangeActionKind {
	switch eventType {
	case summaryEventFileCreate:
		return fileChangeActionCreate
	case summaryEventFileDelete:
		return fileChangeActionDelete
	default:
		return fileChangeActionModify
	}
}

func (kind fileChangeActionKind) String() string {
	return string(kind)
}
