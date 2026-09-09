package agentruntime

import "strings"

// RegisteredToolOutcome is the three-state classification of a legacy
// text-only registered tool response. Unlike ToolTextFailure (a boolean
// failure probe for host adapters), registered tools need an explicit
// "uncertain" state for empty output so callers can distinguish "no signal"
// from success.
type RegisteredToolOutcome int

const (
	RegisteredToolOutcomeUncertain RegisteredToolOutcome = iota
	RegisteredToolOutcomeSucceeded
	RegisteredToolOutcomeFailed
)

func (o RegisteredToolOutcome) String() string {
	switch o {
	case RegisteredToolOutcomeSucceeded:
		return "succeeded"
	case RegisteredToolOutcomeFailed:
		return "failed"
	default:
		return "uncertain"
	}
}

// RegisteredToolTextOutcome classifies a legacy text-only registered tool
// response. Empty text is uncertain; failure is decided by the shared
// ToolTextFailure vocabulary (host adapters, MCP envelope, parse failures and
// the native PDF generator's stable error strings all share one marker set);
// everything else counts as success because registered handlers historically
// returned plain text.
//
// The vocabulary is deliberately NOT forked per host: extending
// ToolTextFailure changes every host's retry/recovery boundary at once.
func RegisteredToolTextOutcome(text string) RegisteredToolOutcome {
	if strings.TrimSpace(text) == "" {
		return RegisteredToolOutcomeUncertain
	}
	if ToolTextFailure(text) {
		return RegisteredToolOutcomeFailed
	}
	return RegisteredToolOutcomeSucceeded
}
