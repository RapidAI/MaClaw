package main

import "strings"

type fatalSessionErrorKind int

const (
	fatalSessionErrorNone fatalSessionErrorKind = iota
	fatalSessionErrorAPIKey
	fatalSessionErrorAuthentication
	fatalSessionErrorHTTPUnauthorized
	fatalSessionErrorToolMissing
	fatalSessionErrorPermission
)

func classifyFatalSessionOutputLine(line string) fatalSessionErrorKind {
	lower := strings.ToLower(line)
	switch {
	case hasFatalSessionAPIKeyMarker(lower):
		return fatalSessionErrorAPIKey
	case strings.Contains(lower, "authentication failed"):
		return fatalSessionErrorAuthentication
	case strings.Contains(lower, "invalid_api_key") || strings.Contains(lower, "invalid api key"):
		return fatalSessionErrorAPIKey
	case hasFatalSessionHTTPUnauthorizedMarker(lower):
		return fatalSessionErrorHTTPUnauthorized
	case hasFatalSessionToolMissingMarker(lower):
		return fatalSessionErrorToolMissing
	case hasFatalSessionOSPermissionMarker(lower):
		return fatalSessionErrorPermission
	default:
		return fatalSessionErrorNone
	}
}

func hasFatalSessionAPIKeyMarker(lower string) bool {
	if !strings.Contains(lower, "api key") {
		return false
	}
	return strings.Contains(lower, "missing") ||
		strings.Contains(lower, "invalid") ||
		strings.Contains(lower, "not set") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "not configured")
}

func hasFatalSessionHTTPUnauthorizedMarker(lower string) bool {
	return strings.Contains(lower, "status 401") ||
		strings.Contains(lower, "http 401") ||
		strings.Contains(lower, "error 401") ||
		strings.Contains(lower, "code 401") ||
		strings.Contains(lower, "401 unauthorized")
}

func hasFatalSessionToolMissingMarker(lower string) bool {
	if strings.Contains(lower, "command not found") ||
		strings.Contains(lower, "not recognized as") ||
		strings.Contains(lower, "is not recognized") {
		return true
	}
	return strings.Contains(lower, "no such file or directory") &&
		(strings.Contains(lower, "claude") ||
			strings.Contains(lower, "codex") ||
			strings.Contains(lower, "gemini"))
}

func hasFatalSessionOSPermissionMarker(lower string) bool {
	return strings.Contains(lower, "permission denied") &&
		!strings.Contains(lower, "rate") &&
		!strings.Contains(lower, "api")
}
