package sshtool

import "github.com/RapidAI/CodeClaw/corelib/remote"

func isRunningSessionStatus(status string) bool {
	return remote.SessionStatus(status).IsOpen()
}

func runningSessionStatusLabel() string {
	return remote.SessionRunning.String()
}
