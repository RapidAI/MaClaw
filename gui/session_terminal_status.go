package main

import "strings"

type terminalSessionStatus string

const (
	terminalSessionStatusCompleted terminalSessionStatus = "completed"
	terminalSessionStatusFailed    terminalSessionStatus = "failed"
	terminalSessionStatusExited    terminalSessionStatus = "exited"
	terminalSessionStatusCancelled terminalSessionStatus = "cancelled"
	terminalSessionStatusError     terminalSessionStatus = "error"
)

func normalizeTerminalSessionStatus(status string) terminalSessionStatus {
	switch terminalSessionStatus(strings.TrimSpace(status)) {
	case terminalSessionStatusCompleted:
		return terminalSessionStatusCompleted
	case terminalSessionStatusFailed:
		return terminalSessionStatusFailed
	case terminalSessionStatusExited:
		return terminalSessionStatusExited
	case terminalSessionStatusCancelled:
		return terminalSessionStatusCancelled
	case terminalSessionStatusError:
		return terminalSessionStatusError
	default:
		return ""
	}
}

func isTerminalSessionStatus(status string) bool {
	return normalizeTerminalSessionStatus(status) != ""
}
