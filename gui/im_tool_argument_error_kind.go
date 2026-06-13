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
	if strings.Contains(err.Error(), "unexpected end of JSON input") {
		if len(rawArgs) > 8000 || toolArgumentTextLikelyContainsLargePayload(rawArgs) {
			return toolArgumentErrorTruncatedJSON
		}
	}
	return toolArgumentErrorUnknown
}

func toolArgumentTextLikelyContainsLargePayload(rawArgs string) bool {
	lower := strings.ToLower(rawArgs)
	for _, key := range []string{`"content"`, `"old_string"`, `"new_string"`, `"replacement"`, `"text"`} {
		if strings.Contains(lower, key) {
			return true
		}
	}
	return false
}

func (k toolArgumentErrorKind) Hint() string {
	switch k {
	case toolArgumentErrorTruncatedJSON:
		return "The argument JSON appears truncated. Split large write_file content across smaller calls."
	default:
		return ""
	}
}
