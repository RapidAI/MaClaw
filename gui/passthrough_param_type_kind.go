package main

import "strings"

type passthroughParamTypeKind string

const (
	passthroughParamTypeText    passthroughParamTypeKind = "text"
	passthroughParamTypeNumber  passthroughParamTypeKind = "number"
	passthroughParamTypeBoolean passthroughParamTypeKind = "boolean"
	passthroughParamTypePath    passthroughParamTypeKind = "path"
	passthroughParamTypeUnknown passthroughParamTypeKind = ""
)

func normalizePassthroughParamTypeKind(value string) passthroughParamTypeKind {
	switch passthroughParamTypeKind(strings.ToLower(strings.TrimSpace(value))) {
	case passthroughParamTypeNumber:
		return passthroughParamTypeNumber
	case passthroughParamTypeBoolean:
		return passthroughParamTypeBoolean
	case passthroughParamTypePath:
		return passthroughParamTypePath
	case passthroughParamTypeText, passthroughParamTypeUnknown:
		return passthroughParamTypeText
	default:
		return passthroughParamTypeUnknown
	}
}

func (kind passthroughParamTypeKind) String() string {
	return string(kind)
}

func (kind passthroughParamTypeKind) IsSupported() bool {
	return kind != passthroughParamTypeUnknown
}
