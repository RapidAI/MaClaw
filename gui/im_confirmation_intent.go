package main

import "strings"

type confirmationIntent string

const (
	confirmationIntentUnknown confirmationIntent = ""
	confirmationIntentConfirm confirmationIntent = "confirm"
	confirmationIntentCancel  confirmationIntent = "cancel"
	confirmationIntentModify  confirmationIntent = "modify"
)

func (i confirmationIntent) String() string {
	return string(i)
}

func normalizeConfirmationIntent(text string) confirmationIntent {
	intent := strings.ToLower(strings.TrimSpace(text))
	intent = strings.Trim(intent, " \t\r\n`\"'.,:;!?()[]{}")
	switch intent {
	case confirmationIntentConfirm.String():
		return confirmationIntentConfirm
	case confirmationIntentCancel.String():
		return confirmationIntentCancel
	case confirmationIntentModify.String():
		return confirmationIntentModify
	default:
		return confirmationIntentUnknown
	}
}
