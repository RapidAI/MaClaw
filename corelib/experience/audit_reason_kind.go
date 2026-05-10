package experience

import "strings"

type auditReasonKind int

const (
	auditReasonNoSkillsLearned auditReasonKind = iota
	auditReasonQualityBelowThreshold
	auditReasonInsufficientEvidence
	auditReasonExistingSkillBetter
	auditReasonPatternBudgetExceeded
	auditReasonStoreWriteFailed
	auditReasonUnsupportedStepAction
)

func classifyAuditReason(reason string) auditReasonKind {
	lower := strings.ToLower(reason)
	switch {
	case strings.Contains(lower, "quality score below threshold"):
		return auditReasonQualityBelowThreshold
	case strings.Contains(lower, "insufficient session evidence"):
		return auditReasonInsufficientEvidence
	case strings.Contains(lower, "existing skill is equal or better"):
		return auditReasonExistingSkillBetter
	case strings.Contains(lower, "pattern budget exceeded"):
		return auditReasonPatternBudgetExceeded
	case strings.Contains(lower, "register failed"), strings.Contains(lower, "update failed"):
		return auditReasonStoreWriteFailed
	case strings.Contains(lower, "unsupported step action"):
		return auditReasonUnsupportedStepAction
	default:
		return auditReasonNoSkillsLearned
	}
}

func (k auditReasonKind) IssueCode() string {
	switch k {
	case auditReasonQualityBelowThreshold:
		return AuditIssueQualityBelowThreshold
	case auditReasonInsufficientEvidence:
		return AuditIssueInsufficientEvidence
	case auditReasonExistingSkillBetter:
		return AuditIssueExistingSkillBetter
	case auditReasonPatternBudgetExceeded:
		return AuditIssuePatternBudgetExceeded
	case auditReasonStoreWriteFailed:
		return AuditIssueStoreWriteFailed
	case auditReasonUnsupportedStepAction:
		return AuditIssueUnsupportedStepAction
	default:
		return AuditIssueNoSkillsLearned
	}
}

func (k auditReasonKind) SuggestedAction() string {
	switch k {
	case auditReasonQualityBelowThreshold:
		return "capture broader repeatable workflows before learning a skill"
	case auditReasonInsufficientEvidence:
		return "keep enough command output and important events to support every learned step"
	case auditReasonExistingSkillBetter:
		return "no action needed unless the existing skill should be replaced"
	case auditReasonPatternBudgetExceeded:
		return "increase extraction budget only after confirming candidates are high quality"
	case auditReasonStoreWriteFailed:
		return "check learned skill store permissions and validation errors"
	case auditReasonUnsupportedStepAction:
		return "teach extraction to use supported actions or add support for the missing action"
	default:
		return "inspect recent audit decisions for the skipped candidate details"
	}
}
