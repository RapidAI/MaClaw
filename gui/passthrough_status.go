package main

import "strings"

type passthroughRunStatus string

const (
	passthroughRunStatusUnknown passthroughRunStatus = ""
	passthroughRunStatusSuccess passthroughRunStatus = "success"
	passthroughRunStatusFailed  passthroughRunStatus = "failed"
	passthroughRunStatusTimeout passthroughRunStatus = "timeout"
)

func normalizePassthroughRunStatus(status passthroughRunStatus) passthroughRunStatus {
	switch passthroughRunStatus(strings.TrimSpace(string(status))) {
	case passthroughRunStatusSuccess:
		return passthroughRunStatusSuccess
	case passthroughRunStatusFailed:
		return passthroughRunStatusFailed
	case passthroughRunStatusTimeout:
		return passthroughRunStatusTimeout
	default:
		return passthroughRunStatusUnknown
	}
}

func passthroughRunStatusForError(err error) passthroughRunStatus {
	if err != nil {
		return passthroughRunStatusFailed
	}
	return passthroughRunStatusSuccess
}

type passthroughControlAction string

const (
	passthroughControlActionUnknown passthroughControlAction = ""
	passthroughControlActionEnable  passthroughControlAction = "enable"
	passthroughControlActionDisable passthroughControlAction = "disable"
)

func passthroughControlActionForEnabled(enabled bool) passthroughControlAction {
	if enabled {
		return passthroughControlActionEnable
	}
	return passthroughControlActionDisable
}

func normalizePassthroughControlAction(action string) passthroughControlAction {
	switch passthroughControlAction(strings.TrimSpace(action)) {
	case passthroughControlActionEnable:
		return passthroughControlActionEnable
	case passthroughControlActionDisable:
		return passthroughControlActionDisable
	default:
		return passthroughControlActionUnknown
	}
}

func (a passthroughControlAction) Enabled() bool {
	return a == passthroughControlActionEnable
}
