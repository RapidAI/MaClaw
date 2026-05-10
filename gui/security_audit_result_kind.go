package main

import "strings"

type securityAuditResultKind int

const (
	securityAuditResultOther securityAuditResultKind = iota
	securityAuditResultDenied
	securityAuditResultRejected
)

func (k securityAuditResultKind) IsDenied() bool {
	switch k {
	case securityAuditResultDenied, securityAuditResultRejected:
		return true
	default:
		return false
	}
}

func classifySecurityAuditResult(result string) securityAuditResultKind {
	lower := strings.ToLower(result)
	switch {
	case strings.Contains(lower, "denied") || strings.Contains(lower, "鎷掔粷"):
		return securityAuditResultDenied
	case strings.Contains(lower, "rejected"):
		return securityAuditResultRejected
	default:
		return securityAuditResultOther
	}
}
