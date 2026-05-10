package main

import "strings"

type misIntentDecisionKind string

const (
	misIntentDecisionUnknown           misIntentDecisionKind = ""
	misIntentDecisionAskUserToChoose   misIntentDecisionKind = "ask_user_to_choose"
	misIntentDecisionAutoOpenTaskPanel misIntentDecisionKind = "auto_open_task_panel"
)

func normalizeMISIntentDecisionKind(value string) misIntentDecisionKind {
	switch misIntentDecisionKind(strings.ToLower(strings.TrimSpace(value))) {
	case misIntentDecisionAskUserToChoose:
		return misIntentDecisionAskUserToChoose
	case misIntentDecisionAutoOpenTaskPanel:
		return misIntentDecisionAutoOpenTaskPanel
	default:
		return misIntentDecisionUnknown
	}
}
