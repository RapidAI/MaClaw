package main

import "strings"

type geminiACPPermissionKind string

const (
	geminiACPPermissionUnknown     geminiACPPermissionKind = ""
	geminiACPPermissionAllowOnce   geminiACPPermissionKind = "allow_once"
	geminiACPPermissionAllowAlways geminiACPPermissionKind = "allow_always"
	geminiACPPermissionDeny        geminiACPPermissionKind = "deny"
)

func normalizeGeminiACPPermissionKind(kind string) geminiACPPermissionKind {
	switch geminiACPPermissionKind(strings.TrimSpace(kind)) {
	case geminiACPPermissionAllowOnce:
		return geminiACPPermissionAllowOnce
	case geminiACPPermissionAllowAlways:
		return geminiACPPermissionAllowAlways
	case geminiACPPermissionDeny:
		return geminiACPPermissionDeny
	default:
		return geminiACPPermissionUnknown
	}
}

func (k geminiACPPermissionKind) IsAllow() bool {
	switch k {
	case geminiACPPermissionAllowOnce, geminiACPPermissionAllowAlways:
		return true
	default:
		return false
	}
}
