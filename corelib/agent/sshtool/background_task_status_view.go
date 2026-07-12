package sshtool

import "github.com/RapidAI/CodeClaw/corelib/remote"

func backgroundTaskStatusIcon(status remote.SSHBackgroundTaskStatus) string {
	switch {
	case status.IsCompleted():
		return "[OK]"
	case status.IsFailed():
		return "[ERR]"
	case status.IsKilled():
		return "[STOP]"
	case status.IsUnknown():
		return "[?]"
	default:
		return "[..]"
	}
}
