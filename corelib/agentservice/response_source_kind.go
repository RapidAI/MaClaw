package agentservice

import "strings"

type responseSourceKind string

const (
	responseSourceUnknown     responseSourceKind = ""
	responseSourceChat        responseSourceKind = "chat"
	responseSourceAskUser     responseSourceKind = "ask_user"
	responseSourcePlanConfirm responseSourceKind = "plan_confirm"
)

func normalizeResponseSourceKind(value string) responseSourceKind {
	switch responseSourceKind(strings.TrimSpace(value)) {
	case responseSourceChat:
		return responseSourceChat
	case responseSourceAskUser:
		return responseSourceAskUser
	case responseSourcePlanConfirm:
		return responseSourcePlanConfirm
	default:
		return responseSourceUnknown
	}
}

func (k responseSourceKind) IsWaitingForUser() bool {
	return k == responseSourceAskUser
}
