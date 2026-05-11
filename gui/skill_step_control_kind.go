package main

import "strings"

type skillStepConditionKind string

const (
	skillStepConditionNone      skillStepConditionKind = ""
	skillStepConditionOnFailure skillStepConditionKind = "on_failure"
	skillStepConditionOnSuccess skillStepConditionKind = "on_success"
)

func normalizeSkillStepConditionKind(value string) skillStepConditionKind {
	switch skillStepConditionKind(strings.ToLower(strings.TrimSpace(value))) {
	case skillStepConditionOnFailure:
		return skillStepConditionOnFailure
	case skillStepConditionOnSuccess:
		return skillStepConditionOnSuccess
	default:
		return skillStepConditionNone
	}
}

type skillStepOnErrorKind string

const (
	skillStepOnErrorStop     skillStepOnErrorKind = "stop"
	skillStepOnErrorContinue skillStepOnErrorKind = "continue"
	skillStepOnErrorSkip     skillStepOnErrorKind = "skip"
)

func normalizeSkillStepOnErrorKind(value string) skillStepOnErrorKind {
	switch skillStepOnErrorKind(strings.ToLower(strings.TrimSpace(value))) {
	case skillStepOnErrorContinue:
		return skillStepOnErrorContinue
	case skillStepOnErrorSkip:
		return skillStepOnErrorSkip
	default:
		return skillStepOnErrorStop
	}
}

func (kind skillStepOnErrorKind) ShouldContinue() bool {
	return kind == skillStepOnErrorContinue || kind == skillStepOnErrorSkip
}
