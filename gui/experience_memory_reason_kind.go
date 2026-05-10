package main

import "strings"

type experienceMemoryReasonKind string

const (
	experienceMemoryReasonUnknown      experienceMemoryReasonKind = ""
	experienceMemoryReasonPinned       experienceMemoryReasonKind = "pinned"
	experienceMemoryReasonInstruction  experienceMemoryReasonKind = "instruction"
	experienceMemoryReasonSelfIdentity experienceMemoryReasonKind = "self_identity"
	experienceMemoryReasonHighStrength experienceMemoryReasonKind = "high_strength"
	experienceMemoryReasonA2ADiscussion experienceMemoryReasonKind = "a2a_discussion"
	experienceMemoryReasonToolUsage    experienceMemoryReasonKind = "tool_usage"
	experienceMemoryReasonSwarmTrace   experienceMemoryReasonKind = "swarm_trace"
)

func normalizeExperienceMemoryReasonKind(value string) experienceMemoryReasonKind {
	switch experienceMemoryReasonKind(strings.TrimSpace(value)) {
	case experienceMemoryReasonPinned:
		return experienceMemoryReasonPinned
	case experienceMemoryReasonInstruction:
		return experienceMemoryReasonInstruction
	case experienceMemoryReasonSelfIdentity:
		return experienceMemoryReasonSelfIdentity
	case experienceMemoryReasonHighStrength:
		return experienceMemoryReasonHighStrength
	case experienceMemoryReasonA2ADiscussion:
		return experienceMemoryReasonA2ADiscussion
	case experienceMemoryReasonToolUsage:
		return experienceMemoryReasonToolUsage
	case experienceMemoryReasonSwarmTrace:
		return experienceMemoryReasonSwarmTrace
	default:
		return experienceMemoryReasonUnknown
	}
}

func (kind experienceMemoryReasonKind) Rank() int {
	switch kind {
	case experienceMemoryReasonPinned:
		return 80
	case experienceMemoryReasonInstruction:
		return 70
	case experienceMemoryReasonSelfIdentity:
		return 65
	case experienceMemoryReasonHighStrength:
		return 60
	case experienceMemoryReasonA2ADiscussion:
		return 50
	case experienceMemoryReasonToolUsage:
		return 40
	case experienceMemoryReasonSwarmTrace:
		return 30
	default:
		return 0
	}
}
