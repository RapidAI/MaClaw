package main

import "strings"

type experienceRoutingDirectionKind string

const (
	experienceRoutingDirectionUnknown experienceRoutingDirectionKind = ""
	experienceRoutingDirectionPrefer  experienceRoutingDirectionKind = "prefer"
	experienceRoutingDirectionAvoid   experienceRoutingDirectionKind = "avoid"
)

func normalizeExperienceRoutingDirectionKind(value string) experienceRoutingDirectionKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "prefer", "positive":
		return experienceRoutingDirectionPrefer
	case "avoid", "negative":
		return experienceRoutingDirectionAvoid
	default:
		return experienceRoutingDirectionUnknown
	}
}

func (kind experienceRoutingDirectionKind) Recommendation() string {
	switch kind {
	case experienceRoutingDirectionPrefer:
		return "consider earlier in routing; evidence is bounded and non-executing"
	case experienceRoutingDirectionAvoid:
		return "deprioritize unless the current task has stronger direct evidence"
	default:
		return "inspect evidence; no bounded routing preference is currently implied"
	}
}
