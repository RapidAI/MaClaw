package main

import "strings"

type askUserInputKind string

const (
	askUserInputUnknown askUserInputKind = ""
	askUserInputText    askUserInputKind = "text"
	askUserInputChoice  askUserInputKind = "choice"
	askUserInputConfirm askUserInputKind = "confirm"
)

func normalizeAskUserInputKind(value string) askUserInputKind {
	switch askUserInputKind(strings.TrimSpace(value)) {
	case askUserInputChoice:
		return askUserInputChoice
	case askUserInputConfirm:
		return askUserInputConfirm
	case askUserInputText, askUserInputUnknown:
		return askUserInputText
	default:
		return askUserInputText
	}
}

func (kind askUserInputKind) String() string {
	return string(kind)
}

func (kind askUserInputKind) IsChoice() bool {
	return kind == askUserInputChoice
}

func (kind askUserInputKind) IsConfirm() bool {
	return kind == askUserInputConfirm
}
