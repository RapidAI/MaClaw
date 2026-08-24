package longhorizon

import "strings"

func ExperienceEligible(in EligibilityInput) bool {
	if in.Cancelled || in.Interrupted || in.Untrusted || in.MissingControlHeader {
		return false
	}
	if in.AttemptTerminal {
		return false
	}
	if strings.TrimSpace(in.HorizonTaskID) == "" || in.RoundIndex <= 0 || strings.TrimSpace(in.AuditDigest) == "" {
		return false
	}
	if in.Audit == nil || in.Audit.Synthetic || in.Audit.Mechanical {
		return false
	}
	if in.Audit.Status == "complete" && in.Audit.Integrity == "clean" && in.Audit.Alignment == "aligned" {
		return true
	}
	if in.Audit.Status == "blocked" || in.Audit.Integrity == "violation" {
		return true
	}
	return false
}
