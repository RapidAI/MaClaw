package agentservice

import "strings"

type responseSourceKind string

const (
	responseSourceUnknown responseSourceKind = ""
	responseSourceAskUser responseSourceKind = "ask_user"
)

func normalizeResponseSourceKind(value string) responseSourceKind {
	switch responseSourceKind(strings.TrimSpace(value)) {
	case responseSourceAskUser:
		return responseSourceAskUser
	default:
		return responseSourceUnknown
	}
}

func (k responseSourceKind) IsWaitingForUser() bool {
	return k == responseSourceAskUser
}
