package main

import "strings"

type remoteTraceCategoryKind string

const (
	remoteTraceCategoryEvent   remoteTraceCategoryKind = "event"
	remoteTraceCategoryError   remoteTraceCategoryKind = "error"
	remoteTraceCategoryFile    remoteTraceCategoryKind = "file"
	remoteTraceCategoryCommand remoteTraceCategoryKind = "command"
	remoteTraceCategoryResult  remoteTraceCategoryKind = "result"
)

func classifyRemoteTraceCategory(evt ImportantEvent) remoteTraceCategoryKind {
	typeLower := strings.ToLower(evt.Type)
	severityLower := strings.ToLower(evt.Severity)
	switch {
	case severityLower == "error" || strings.Contains(typeLower, "error") || strings.Contains(typeLower, "fail"):
		return remoteTraceCategoryError
	case strings.Contains(typeLower, "file") || evt.RelatedFile != "":
		return remoteTraceCategoryFile
	case strings.Contains(typeLower, "command") || evt.Command != "":
		return remoteTraceCategoryCommand
	case strings.Contains(typeLower, "result") || strings.Contains(typeLower, "close") || strings.Contains(typeLower, "complete"):
		return remoteTraceCategoryResult
	default:
		return remoteTraceCategoryEvent
	}
}

func (kind remoteTraceCategoryKind) String() string {
	return string(kind)
}
