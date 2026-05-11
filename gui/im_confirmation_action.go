package main

type confirmationAction string

const (
	confirmationActionNone    confirmationAction = ""
	confirmationActionConfirm confirmationAction = "confirm"
	confirmationActionCancel  confirmationAction = "cancel"
)

func (action confirmationAction) String() string {
	return string(action)
}
