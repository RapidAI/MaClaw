package main

type toolFailureKind string

const (
	toolFailureNone              toolFailureKind = ""
	toolFailureArgumentParse     toolFailureKind = "argument_parse"
	toolFailureMissingParameters toolFailureKind = "missing_parameters"
	toolFailureValidation        toolFailureKind = "validation"
	toolFailureApprovalRequired  toolFailureKind = "approval_required"
	toolFailureFirewallRejected  toolFailureKind = "firewall_rejected"
	toolFailurePolicyRejected    toolFailureKind = "policy_rejected"
	toolFailureExecutionPanic    toolFailureKind = "execution_panic"
	toolFailureUnknownTool       toolFailureKind = "unknown_tool"
	toolFailureTruncationBlocked toolFailureKind = "truncation_blocked"
	toolFailureHandlerReported   toolFailureKind = "handler_reported"
)

type toolExecutionResult struct {
	Text        string
	ToolName    string
	ToolKind    agentToolKind
	Outcome     toolOutcome
	FailureKind toolFailureKind
	Metadata    toolResultMetadata
}

func (r toolExecutionResult) IsFailure() bool {
	return r.Outcome == toolOutcomeFailed
}

func (r toolExecutionResult) IsWriteFileRecoverableFailure() bool {
	if r.ToolKind != agentToolKindWriteFile {
		return false
	}
	switch r.FailureKind {
	case toolFailureArgumentParse, toolFailureMissingParameters, toolFailureValidation:
		return true
	default:
		return false
	}
}

func failureKindForOutcome(outcome toolOutcome) toolFailureKind {
	if outcome == toolOutcomeFailed {
		return toolFailureHandlerReported
	}
	return toolFailureNone
}

type toolResultMetadata struct {
	SkillRunID       string
	SkillRunTerminal bool // true when the skill run reached a terminal state (success/failed/etc.)
}
