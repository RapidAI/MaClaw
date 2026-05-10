package main

import "strings"

type maclawLLMAuthTypeKind string

const (
	maclawLLMAuthTypeUnknown maclawLLMAuthTypeKind = ""
	maclawLLMAuthTypeOAuth   maclawLLMAuthTypeKind = "oauth"
)

func normalizeMaclawLLMAuthTypeKind(value string) maclawLLMAuthTypeKind {
	switch maclawLLMAuthTypeKind(strings.TrimSpace(value)) {
	case maclawLLMAuthTypeOAuth:
		return maclawLLMAuthTypeOAuth
	default:
		return maclawLLMAuthTypeUnknown
	}
}

func (kind maclawLLMAuthTypeKind) IsOAuth() bool {
	return kind == maclawLLMAuthTypeOAuth
}

type maclawLLMProviderStatusKind string

const (
	maclawLLMProviderStatusDefault          maclawLLMProviderStatusKind = ""
	maclawLLMProviderStatusOK               maclawLLMProviderStatusKind = "ok"
	maclawLLMProviderStatusReady            maclawLLMProviderStatusKind = "ready"
	maclawLLMProviderStatusEnabled          maclawLLMProviderStatusKind = "enabled"
	maclawLLMProviderStatusAvailable        maclawLLMProviderStatusKind = "available"
	maclawLLMProviderStatusActive           maclawLLMProviderStatusKind = "active"
	maclawLLMProviderStatusDisabled         maclawLLMProviderStatusKind = "disabled"
	maclawLLMProviderStatusUnavailable      maclawLLMProviderStatusKind = "unavailable"
	maclawLLMProviderStatusInactive         maclawLLMProviderStatusKind = "inactive"
	maclawLLMProviderStatusDenied           maclawLLMProviderStatusKind = "denied"
	maclawLLMProviderStatusForbidden        maclawLLMProviderStatusKind = "forbidden"
	maclawLLMProviderStatusNoPermission     maclawLLMProviderStatusKind = "no_permission"
	maclawLLMProviderStatusNoPermissionDash maclawLLMProviderStatusKind = "no-permission"
)

func normalizeMaclawLLMProviderStatusKind(value string) maclawLLMProviderStatusKind {
	switch maclawLLMProviderStatusKind(strings.ToLower(strings.TrimSpace(value))) {
	case maclawLLMProviderStatusDefault:
		return maclawLLMProviderStatusDefault
	case maclawLLMProviderStatusOK:
		return maclawLLMProviderStatusOK
	case maclawLLMProviderStatusReady:
		return maclawLLMProviderStatusReady
	case maclawLLMProviderStatusEnabled:
		return maclawLLMProviderStatusEnabled
	case maclawLLMProviderStatusAvailable:
		return maclawLLMProviderStatusAvailable
	case maclawLLMProviderStatusActive:
		return maclawLLMProviderStatusActive
	case maclawLLMProviderStatusDisabled:
		return maclawLLMProviderStatusDisabled
	case maclawLLMProviderStatusUnavailable:
		return maclawLLMProviderStatusUnavailable
	case maclawLLMProviderStatusInactive:
		return maclawLLMProviderStatusInactive
	case maclawLLMProviderStatusDenied:
		return maclawLLMProviderStatusDenied
	case maclawLLMProviderStatusForbidden:
		return maclawLLMProviderStatusForbidden
	case maclawLLMProviderStatusNoPermission:
		return maclawLLMProviderStatusNoPermission
	case maclawLLMProviderStatusNoPermissionDash:
		return maclawLLMProviderStatusNoPermissionDash
	default:
		return maclawLLMProviderStatusOK
	}
}

func (kind maclawLLMProviderStatusKind) IsAvailable() bool {
	switch kind {
	case maclawLLMProviderStatusDisabled,
		maclawLLMProviderStatusUnavailable,
		maclawLLMProviderStatusInactive,
		maclawLLMProviderStatusDenied,
		maclawLLMProviderStatusForbidden,
		maclawLLMProviderStatusNoPermission,
		maclawLLMProviderStatusNoPermissionDash:
		return false
	default:
		return true
	}
}

type hubLLMServiceGrantStatusKind string

const (
	hubLLMServiceGrantStatusUnknown       hubLLMServiceGrantStatusKind = ""
	hubLLMServiceGrantStatusPeriodLimited hubLLMServiceGrantStatusKind = "period_limited"
	hubLLMServiceGrantStatusQueued        hubLLMServiceGrantStatusKind = "queued"
	hubLLMServiceGrantStatusExhausted     hubLLMServiceGrantStatusKind = "exhausted"
	hubLLMServiceGrantStatusExpired       hubLLMServiceGrantStatusKind = "expired"
	hubLLMServiceGrantStatusActive        hubLLMServiceGrantStatusKind = "active"
)

func normalizeHubLLMServiceGrantStatusKind(value string) hubLLMServiceGrantStatusKind {
	switch hubLLMServiceGrantStatusKind(strings.ToLower(strings.TrimSpace(value))) {
	case hubLLMServiceGrantStatusPeriodLimited:
		return hubLLMServiceGrantStatusPeriodLimited
	case hubLLMServiceGrantStatusQueued:
		return hubLLMServiceGrantStatusQueued
	case hubLLMServiceGrantStatusExhausted:
		return hubLLMServiceGrantStatusExhausted
	case hubLLMServiceGrantStatusExpired:
		return hubLLMServiceGrantStatusExpired
	case hubLLMServiceGrantStatusActive:
		return hubLLMServiceGrantStatusActive
	default:
		return hubLLMServiceGrantStatusUnknown
	}
}

func (kind hubLLMServiceGrantStatusKind) Rank() int {
	switch kind {
	case hubLLMServiceGrantStatusPeriodLimited:
		return 0
	case hubLLMServiceGrantStatusQueued:
		return 1
	case hubLLMServiceGrantStatusExhausted:
		return 2
	case hubLLMServiceGrantStatusExpired:
		return 3
	case hubLLMServiceGrantStatusActive:
		return 4
	default:
		return 9
	}
}
