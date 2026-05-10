package main

import "strings"

var adaptiveRetryNetworkKeywords = []string{
	"timeout", "connection refused", "dial",
	"eof", "reset by peer", "i/o timeout",
	"client.timeout", "context deadline exceeded",
}

var adaptiveRetryPermissionKeywords = []string{
	"permission denied", "access denied", "forbidden",
	"unauthorized", "403", "401",
}

var adaptiveRetryArgsKeywords = []string{
	"invalid argument", "invalid parameter", "missing required",
	"bad request", "400",
}

var adaptiveRetryLogicKeywords = []string{
	"not found", "already exists", "conflict", "assertion failed",
}

func classifyAdaptiveRetryFailure(err error) FailureCategory {
	if err == nil {
		return FailureUnknown
	}
	msg := strings.ToLower(err.Error())

	retryKind := classifyLLMRetryError(err)
	if retryKind == llmRetryErrorPeriodLimit {
		return FailurePeriodLimit
	}
	if retryKind == llmRetryErrorTransientServer {
		return FailureTransient
	}
	for _, kw := range adaptiveRetryNetworkKeywords {
		if strings.Contains(msg, kw) {
			return FailureNetwork
		}
	}
	for _, kw := range adaptiveRetryPermissionKeywords {
		if strings.Contains(msg, kw) {
			return FailurePermission
		}
	}
	for _, kw := range adaptiveRetryArgsKeywords {
		if strings.Contains(msg, kw) {
			return FailureArgs
		}
	}
	for _, kw := range adaptiveRetryLogicKeywords {
		if strings.Contains(msg, kw) {
			return FailureLogic
		}
	}
	return FailureUnknown
}
