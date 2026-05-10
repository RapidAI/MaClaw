package main

import "strings"

type sessionControlAction string

const (
	sessionControlActionUnknown   sessionControlAction = ""
	sessionControlActionInterrupt sessionControlAction = "interrupt"
	sessionControlActionKill      sessionControlAction = "kill"
)

func normalizeSessionControlAction(action string) sessionControlAction {
	switch sessionControlAction(strings.ToLower(strings.TrimSpace(action))) {
	case sessionControlActionInterrupt:
		return sessionControlActionInterrupt
	case sessionControlActionKill:
		return sessionControlActionKill
	default:
		return sessionControlActionUnknown
	}
}
