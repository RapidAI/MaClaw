package main

import "strings"

type imCriticalConfirmReply int

const (
	imCriticalConfirmReplyReject imCriticalConfirmReply = iota
	imCriticalConfirmReplyApprove
)

func classifyIMCriticalConfirmReply(text string) imCriticalConfirmReply {
	switch strings.TrimSpace(strings.ToLower(text)) {
	case "1", "confirm", "yes", "确认", "继续":
		return imCriticalConfirmReplyApprove
	default:
		return imCriticalConfirmReplyReject
	}
}
