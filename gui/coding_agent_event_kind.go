package main

import "strings"

type codingAgentEventKind int

const (
	codingAgentEventKindUnknown codingAgentEventKind = iota
	codingAgentEventKindTaskStatus
	codingAgentEventKindToolStarted
	codingAgentEventKindToolFinished
	codingAgentEventKindDiffUpdated
	codingAgentEventKindDiffSummary
	codingAgentEventKindDiffCheck
	codingAgentEventKindVerificationSummary
	codingAgentEventKindExplorationSummary
	codingAgentEventKindGuardrailSummary
	codingAgentEventKindCommandSummary
	codingAgentEventKindFileActivitySummary
	codingAgentEventKindQualitySummary
)

func classifyCodingAgentEventKind(event string) codingAgentEventKind {
	switch strings.TrimSpace(strings.ToLower(event)) {
	case "task_status":
		return codingAgentEventKindTaskStatus
	case "tool_started":
		return codingAgentEventKindToolStarted
	case "tool_finished":
		return codingAgentEventKindToolFinished
	case "diff_updated":
		return codingAgentEventKindDiffUpdated
	case "diff_summary":
		return codingAgentEventKindDiffSummary
	case "diff_check":
		return codingAgentEventKindDiffCheck
	case "verification_summary":
		return codingAgentEventKindVerificationSummary
	case "exploration_summary":
		return codingAgentEventKindExplorationSummary
	case "guardrail_summary":
		return codingAgentEventKindGuardrailSummary
	case "command_summary":
		return codingAgentEventKindCommandSummary
	case "file_activity_summary":
		return codingAgentEventKindFileActivitySummary
	case "quality_summary":
		return codingAgentEventKindQualitySummary
	default:
		return codingAgentEventKindUnknown
	}
}

func (k codingAgentEventKind) String() string {
	switch k {
	case codingAgentEventKindTaskStatus:
		return "task_status"
	case codingAgentEventKindToolStarted:
		return "tool_started"
	case codingAgentEventKindToolFinished:
		return "tool_finished"
	case codingAgentEventKindDiffUpdated:
		return "diff_updated"
	case codingAgentEventKindDiffSummary:
		return "diff_summary"
	case codingAgentEventKindDiffCheck:
		return "diff_check"
	case codingAgentEventKindVerificationSummary:
		return "verification_summary"
	case codingAgentEventKindExplorationSummary:
		return "exploration_summary"
	case codingAgentEventKindGuardrailSummary:
		return "guardrail_summary"
	case codingAgentEventKindCommandSummary:
		return "command_summary"
	case codingAgentEventKindFileActivitySummary:
		return "file_activity_summary"
	case codingAgentEventKindQualitySummary:
		return "quality_summary"
	default:
		return ""
	}
}

func (k codingAgentEventKind) CarriesDuration() bool {
	return k == codingAgentEventKindToolFinished
}

func (k codingAgentEventKind) CarriesCount() bool {
	switch k {
	case codingAgentEventKindDiffUpdated,
		codingAgentEventKindDiffSummary,
		codingAgentEventKindDiffCheck,
		codingAgentEventKindVerificationSummary,
		codingAgentEventKindExplorationSummary,
		codingAgentEventKindGuardrailSummary,
		codingAgentEventKindCommandSummary,
		codingAgentEventKindFileActivitySummary,
		codingAgentEventKindQualitySummary:
		return true
	default:
		return false
	}
}
