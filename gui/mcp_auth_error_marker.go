package main

import "strings"

type mcpAuthErrorMarker string

const (
	mcpAuthErrorUnknown      mcpAuthErrorMarker = ""
	mcpAuthErrorCredentials  mcpAuthErrorMarker = "credentials"
	mcpAuthErrorUnauthorized mcpAuthErrorMarker = "unauthorized"
)

func classifyMCPAuthError(err error) mcpAuthErrorMarker {
	if err == nil {
		return mcpAuthErrorUnknown
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "api key"):
		return mcpAuthErrorCredentials
	case strings.Contains(msg, "unauthorized"), strings.Contains(msg, "401"), strings.Contains(msg, "auth"):
		return mcpAuthErrorUnauthorized
	default:
		return mcpAuthErrorUnknown
	}
}

func isMCPAuthError(err error) bool {
	return classifyMCPAuthError(err) != mcpAuthErrorUnknown
}
