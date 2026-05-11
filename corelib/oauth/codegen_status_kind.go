package oauth

import "strings"

type codeGenScanStatus string

const (
	codeGenScanStatusPending codeGenScanStatus = "pending"
	codeGenScanStatusSuccess codeGenScanStatus = "success"
	codeGenScanStatusExpired codeGenScanStatus = "expired"
)

func normalizeCodeGenScanStatus(status string) codeGenScanStatus {
	return codeGenScanStatus(strings.ToLower(strings.TrimSpace(status))).Normalized()
}

func (s codeGenScanStatus) Normalized() codeGenScanStatus {
	switch codeGenScanStatus(strings.ToLower(strings.TrimSpace(string(s)))) {
	case codeGenScanStatusSuccess:
		return codeGenScanStatusSuccess
	case codeGenScanStatusExpired:
		return codeGenScanStatusExpired
	default:
		return codeGenScanStatusPending
	}
}

func (s codeGenScanStatus) IsSuccess() bool {
	return s.Normalized() == codeGenScanStatusSuccess
}

func (s codeGenScanStatus) IsExpired() bool {
	return s.Normalized() == codeGenScanStatusExpired
}

type codeGenModelStatus string

const (
	codeGenModelStatusUnknown          codeGenModelStatus = ""
	codeGenModelStatusOK               codeGenModelStatus = "ok"
	codeGenModelStatusReady            codeGenModelStatus = "ready"
	codeGenModelStatusEnabled          codeGenModelStatus = "enabled"
	codeGenModelStatusAvailable        codeGenModelStatus = "available"
	codeGenModelStatusActive           codeGenModelStatus = "active"
	codeGenModelStatusDisabled         codeGenModelStatus = "disabled"
	codeGenModelStatusUnavailable      codeGenModelStatus = "unavailable"
	codeGenModelStatusInactive         codeGenModelStatus = "inactive"
	codeGenModelStatusDenied           codeGenModelStatus = "denied"
	codeGenModelStatusForbidden        codeGenModelStatus = "forbidden"
	codeGenModelStatusNoPermission     codeGenModelStatus = "no_permission"
	codeGenModelStatusNoPermissionDash codeGenModelStatus = "no-permission"
)

func normalizeCodeGenModelStatus(status string) codeGenModelStatus {
	return codeGenModelStatus(strings.ToLower(strings.TrimSpace(status)))
}

func (s codeGenModelStatus) Normalized() codeGenModelStatus {
	return codeGenModelStatus(strings.ToLower(strings.TrimSpace(string(s))))
}

func (s codeGenModelStatus) IsUsable() bool {
	switch s.Normalized() {
	case codeGenModelStatusUnknown,
		codeGenModelStatusOK,
		codeGenModelStatusReady,
		codeGenModelStatusEnabled,
		codeGenModelStatusAvailable,
		codeGenModelStatusActive:
		return true
	case codeGenModelStatusDisabled,
		codeGenModelStatusUnavailable,
		codeGenModelStatusInactive,
		codeGenModelStatusDenied,
		codeGenModelStatusForbidden,
		codeGenModelStatusNoPermission,
		codeGenModelStatusNoPermissionDash:
		return false
	default:
		return true
	}
}
