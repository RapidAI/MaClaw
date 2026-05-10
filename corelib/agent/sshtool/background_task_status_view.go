package sshtool

import "github.com/RapidAI/CodeClaw/corelib/remote"

func backgroundTaskStatusIcon(status remote.SSHBackgroundTaskStatus) string {
	switch {
	case status.IsCompleted():
		return "✅"
	case status.IsFailed():
		return "❌"
	case status.IsKilled():
		return "🛑"
	case status.IsUnknown():
		return "❓"
	default:
		return "🔄"
	}
}
