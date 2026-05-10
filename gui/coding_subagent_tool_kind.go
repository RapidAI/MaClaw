package main

import "strings"

type codingSubAgentToolKind string

const (
	codingSubAgentToolUnknown codingSubAgentToolKind = ""
	codingSubAgentToolBash    codingSubAgentToolKind = "bash"
	codingSubAgentToolGlob    codingSubAgentToolKind = "glob"
)

func classifyCodingSubAgentTool(toolName string) codingSubAgentToolKind {
	switch codingSubAgentToolKind(strings.ToLower(strings.TrimSpace(toolName))) {
	case codingSubAgentToolBash:
		return codingSubAgentToolBash
	case codingSubAgentToolGlob:
		return codingSubAgentToolGlob
	default:
		return codingSubAgentToolUnknown
	}
}
