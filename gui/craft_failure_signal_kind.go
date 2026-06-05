package main

import "strings"

type craftFailureSignalKind int

const (
	craftFailureSignalNone craftFailureSignalKind = iota
	craftFailureSignalArtifactReportedMissing
	craftFailureSignalArtifactNotReported
	craftFailureSignalPermission
	craftFailureSignalEnvironment
	craftFailureSignalCapabilityBoundary
	craftFailureSignalScript
)

func (k craftFailureSignalKind) DisallowsRetry() bool {
	switch k {
	case craftFailureSignalArtifactNotReported,
		craftFailureSignalPermission,
		craftFailureSignalEnvironment,
		craftFailureSignalCapabilityBoundary:
		return true
	default:
		return false
	}
}

func (k craftFailureSignalKind) FailureCategory() craftFailureCategory {
	switch k {
	case craftFailureSignalPermission:
		return craftFailureCategoryPermission
	case craftFailureSignalEnvironment:
		return craftFailureCategoryEnvironment
	case craftFailureSignalCapabilityBoundary:
		return craftFailureCategoryCapability
	case craftFailureSignalScript:
		return craftFailureCategoryScript
	case craftFailureSignalArtifactReportedMissing, craftFailureSignalArtifactNotReported:
		return craftFailureCategoryArtifact
	default:
		return craftFailureCategoryUnknown
	}
}

func isSuspiciousCraftOutputSignal(kind craftFailureSignalKind) bool {
	switch kind {
	case craftFailureSignalEnvironment, craftFailureSignalScript:
		return true
	default:
		return false
	}
}

func classifyCraftArtifactFailureSignal(message string) craftFailureSignalKind {
	switch {
	case strings.Contains(message, "报告了产物路径") || strings.Contains(message, "鎶ュ憡浜嗕骇鐗╄矾寰?"):
		return craftFailureSignalArtifactReportedMissing
	case strings.Contains(message, "未报告产物路径") || strings.Contains(message, "鏈姤鍛婁骇鐗╄矾寰?"):
		return craftFailureSignalArtifactNotReported
	default:
		return craftFailureSignalNone
	}
}

func classifyCraftFailureSignal(message string) craftFailureSignalKind {
	lower := strings.ToLower(message)
	switch {
	case containsAnyCraftMarker(lower, craftPermissionFailureMarkers):
		return craftFailureSignalPermission
	case containsAnyCraftMarker(lower, craftEnvironmentFailureMarkers):
		return craftFailureSignalEnvironment
	case containsAnyCraftMarker(lower, craftCapabilityFailureMarkers):
		return craftFailureSignalCapabilityBoundary
	case containsAnyCraftMarker(lower, craftScriptFailureMarkers):
		return craftFailureSignalScript
	default:
		return craftFailureSignalNone
	}
}

func containsAnyCraftMarker(text string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

var craftPermissionFailureMarkers = []string{
	"permission denied", "access denied", "unauthorized", "forbidden", "authentication", "credential", "login required",
}

var craftEnvironmentFailureMarkers = []string{
	"no such host", "name resolution", "temporary failure in name resolution", "connection refused", "tls handshake", "x509", "certificate", "rate limit", "429 ",
	"not a directory", "no such file or directory", "cannot find path", "working directory",
}

var craftCapabilityFailureMarkers = []string{
	"manual login", "interactive login", "browser interaction", "captcha", "human verification",
	"repository-wide", "repo-wide", "whole codebase", "large codebase", "multi-file refactor",
}

var craftScriptFailureMarkers = []string{
	"traceback", "syntaxerror", "exception", "command not found", "is not recognized as an internal or external command", "no module named", "error:",
}
