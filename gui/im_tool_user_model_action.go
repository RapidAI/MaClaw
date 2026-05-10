package main

import "strings"

type userModelAction string

const (
	userModelActionUnknown userModelAction = ""
	userModelActionView    userModelAction = "view"
	userModelActionCorrect userModelAction = "correct"
	userModelActionReset   userModelAction = "reset"
)

func normalizeUserModelAction(action string) userModelAction {
	switch userModelAction(strings.ToLower(strings.TrimSpace(action))) {
	case userModelActionView:
		return userModelActionView
	case userModelActionCorrect:
		return userModelActionCorrect
	case userModelActionReset:
		return userModelActionReset
	default:
		return userModelAction(strings.TrimSpace(action))
	}
}
