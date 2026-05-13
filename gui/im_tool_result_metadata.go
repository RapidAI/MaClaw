package main

import (
	"regexp"
	"strings"
)

var skillRunIDMetadataPattern = regexp.MustCompile(`run_id[:=]\s*([A-Za-z0-9._-]+)`)
var skillRunStatusPattern = regexp.MustCompile(`(?m)^[-\s]*status[:=]\s*(success|failed|error|cancelled|timeout)`)

func inferToolResultMetadata(kind agentToolKind, text string) toolResultMetadata {
	switch kind {
	case agentToolKindRunSkill, agentToolKindGetSkillRun, agentToolKindManageSkill:
		return toolResultMetadata{
			SkillRunID:     extractSkillRunIDFromToolText(text),
			SkillRunTerminal: isSkillRunTerminalFromText(text),
		}
	default:
		return toolResultMetadata{}
	}
}

func mergeToolResultMetadata(base, inferred toolResultMetadata) toolResultMetadata {
	if strings.TrimSpace(base.SkillRunID) == "" {
		base.SkillRunID = strings.TrimSpace(inferred.SkillRunID)
	}
	if !base.SkillRunTerminal {
		base.SkillRunTerminal = inferred.SkillRunTerminal
	}
	return base
}

func extractSkillRunIDFromToolText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if matches := skillRunIDMetadataPattern.FindStringSubmatch(text); len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// isSkillRunTerminalFromText returns true if the skill result text indicates
// the run has reached a terminal state (success, failed, error, cancelled, timeout).
// A terminal run does not need further polling or follow-up.
func isSkillRunTerminalFromText(text string) bool {
	if text == "" {
		return false
	}
	return skillRunStatusPattern.MatchString(text)
}
