package main

import (
	"regexp"
	"strings"
)

var skillRunIDMetadataPattern = regexp.MustCompile(`run_id[:=]\s*([A-Za-z0-9._-]+)`)

func inferToolResultMetadata(kind agentToolKind, text string) toolResultMetadata {
	switch kind {
	case agentToolKindRunSkill, agentToolKindGetSkillRun, agentToolKindManageSkill:
		return toolResultMetadata{SkillRunID: extractSkillRunIDFromToolText(text)}
	default:
		return toolResultMetadata{}
	}
}

func mergeToolResultMetadata(base, inferred toolResultMetadata) toolResultMetadata {
	if strings.TrimSpace(base.SkillRunID) == "" {
		base.SkillRunID = strings.TrimSpace(inferred.SkillRunID)
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
