package main

import "strings"

type pendingReplyPromptIntent string

const (
	pendingReplyPromptIntentUnknown pendingReplyPromptIntent = ""
	pendingReplyPromptIntentPending pendingReplyPromptIntent = "pending"
	pendingReplyPromptIntentDone    pendingReplyPromptIntent = "done"
)

func parsePendingReplyPromptIntent(text string) (pendingReplyPromptIntent, bool) {
	intent := strings.ToLower(strings.TrimSpace(text))
	intent = strings.Trim(intent, " \t\r\n`\"'.,:;!?()[]{}")
	switch pendingReplyPromptIntent(intent) {
	case pendingReplyPromptIntentPending:
		return pendingReplyPromptIntentPending, true
	case pendingReplyPromptIntentDone:
		return pendingReplyPromptIntentDone, true
	default:
		return pendingReplyPromptIntentUnknown, false
	}
}

type pendingReplyAnswerIntent string

const (
	pendingReplyAnswerIntentUnknown pendingReplyAnswerIntent = ""
	pendingReplyAnswerIntentAnswer  pendingReplyAnswerIntent = "answer"
	pendingReplyAnswerIntentNew     pendingReplyAnswerIntent = "new"
)

func parsePendingReplyAnswerIntent(text string) (pendingReplyAnswerIntent, bool) {
	intent := strings.ToLower(strings.TrimSpace(text))
	intent = strings.Trim(intent, " \t\r\n`\"'.,:;!?()[]{}")
	switch pendingReplyAnswerIntent(intent) {
	case pendingReplyAnswerIntentAnswer:
		return pendingReplyAnswerIntentAnswer, true
	case pendingReplyAnswerIntentNew:
		return pendingReplyAnswerIntentNew, true
	default:
		return pendingReplyAnswerIntentUnknown, false
	}
}
