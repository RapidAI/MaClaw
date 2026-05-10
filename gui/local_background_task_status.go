package main

import "strings"

type localBackgroundTaskStatus string

const (
	localBackgroundTaskStatusUnknown   localBackgroundTaskStatus = ""
	localBackgroundTaskStatusCompleted localBackgroundTaskStatus = "completed"
	localBackgroundTaskStatusFailed    localBackgroundTaskStatus = "failed"
	localBackgroundTaskStatusKilled    localBackgroundTaskStatus = "killed"
	localBackgroundTaskStatusUnknownID localBackgroundTaskStatus = "unknown"
)

func normalizeLocalBackgroundTaskStatus(status interface{}) localBackgroundTaskStatus {
	switch localBackgroundTaskStatus(strings.TrimSpace(statusString(status))) {
	case localBackgroundTaskStatusCompleted:
		return localBackgroundTaskStatusCompleted
	case localBackgroundTaskStatusFailed:
		return localBackgroundTaskStatusFailed
	case localBackgroundTaskStatusKilled:
		return localBackgroundTaskStatusKilled
	case localBackgroundTaskStatusUnknownID:
		return localBackgroundTaskStatusUnknownID
	default:
		return localBackgroundTaskStatusUnknown
	}
}

func statusString(status interface{}) string {
	switch v := status.(type) {
	case string:
		return v
	case interface{ String() string }:
		return v.String()
	default:
		return ""
	}
}
