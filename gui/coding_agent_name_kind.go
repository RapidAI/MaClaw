package main

import "strings"

type codingAgentNameKind string

const (
	codingAgentNameUnknown codingAgentNameKind = ""
	codingAgentNameCoding  codingAgentNameKind = "coding"
)

func normalizeCodingAgentNameKind(value string) codingAgentNameKind {
	switch codingAgentNameKind(strings.ToLower(strings.TrimSpace(value))) {
	case codingAgentNameCoding:
		return codingAgentNameCoding
	default:
		return codingAgentNameUnknown
	}
}

func (kind codingAgentNameKind) String() string {
	return string(kind)
}
