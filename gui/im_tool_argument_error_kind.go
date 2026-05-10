package main

import "strings"

type toolArgumentErrorKind int

const (
	toolArgumentErrorUnknown toolArgumentErrorKind = iota
	toolArgumentErrorTruncatedJSON
)

func classifyToolArgumentError(err error, rawArgs string) toolArgumentErrorKind {
	if err == nil {
		return toolArgumentErrorUnknown
	}
	if len(rawArgs) > 8000 && strings.Contains(err.Error(), "unexpected end of JSON input") {
		return toolArgumentErrorTruncatedJSON
	}
	return toolArgumentErrorUnknown
}

func (k toolArgumentErrorKind) Hint() string {
	switch k {
	case toolArgumentErrorTruncatedJSON:
		return "The argument JSON appears truncated. Split large write_file content across smaller calls."
	default:
		return ""
	}
}
