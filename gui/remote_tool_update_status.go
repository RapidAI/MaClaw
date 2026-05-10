package main

import "strings"

type remoteToolUpdateStatus string

const (
	remoteToolUpdateStatusUnknown   remoteToolUpdateStatus = ""
	remoteToolUpdateStatusCompleted remoteToolUpdateStatus = "completed"
	remoteToolUpdateStatusFailed    remoteToolUpdateStatus = "failed"
)

func normalizeRemoteToolUpdateStatus(status string) remoteToolUpdateStatus {
	switch remoteToolUpdateStatus(strings.TrimSpace(status)) {
	case remoteToolUpdateStatusCompleted:
		return remoteToolUpdateStatusCompleted
	case remoteToolUpdateStatusFailed:
		return remoteToolUpdateStatusFailed
	default:
		return remoteToolUpdateStatusUnknown
	}
}

func (s remoteToolUpdateStatus) IsTerminal() bool {
	switch s {
	case remoteToolUpdateStatusCompleted, remoteToolUpdateStatusFailed:
		return true
	default:
		return false
	}
}
