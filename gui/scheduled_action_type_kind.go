package main

import "strings"

type scheduledActionTypeKind string

const (
	scheduledActionTypeUnknown       scheduledActionTypeKind = ""
	scheduledActionTypeBrowserReplay scheduledActionTypeKind = "browser_replay"
)

func normalizeScheduledActionTypeKind(value string) scheduledActionTypeKind {
	switch scheduledActionTypeKind(strings.TrimSpace(value)) {
	case scheduledActionTypeBrowserReplay:
		return scheduledActionTypeBrowserReplay
	default:
		return scheduledActionTypeUnknown
	}
}

func (kind scheduledActionTypeKind) IsBrowserReplay() bool {
	return kind == scheduledActionTypeBrowserReplay
}
