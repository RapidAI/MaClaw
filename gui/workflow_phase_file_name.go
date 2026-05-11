package main

import (
	"path/filepath"
	"strings"
	"unicode"
)

const (
	workflowPhaseOpsIntake           = "ops_intake"
	workflowPhaseReadonlyCollection  = "readonly_collection"
	workflowPhaseArtifactPlan        = "artifact_plan"
	workflowPhaseRiskPolicy          = "risk_policy"
	workflowPhaseControlledExecution = "controlled_execution"
)

func workflowPhaseFileName(phaseID string) string {
	if fileName := workflowPhaseKindFileName(normalizeWorkflowPhaseKind(phaseID)); fileName != "" {
		return fileName
	}

	switch strings.TrimSpace(phaseID) {
	case workflowPhaseOpsIntake:
		return "01-ops-intake.md"
	case workflowPhaseReadonlyCollection:
		return "02-readonly-collection.md"
	case workflowPhaseArtifactPlan:
		return "03-maintenance-artifacts.md"
	case workflowPhaseRiskPolicy:
		return "04-risk-policy.md"
	case workflowPhaseControlledExecution:
		return "05-controlled-execution.md"
	default:
		return sanitizeWorkflowPhaseFileStem(phaseID) + ".md"
	}
}

func workflowPhaseKindFileName(phase workflowPhaseKind) string {
	switch phase {
	case workflowPhaseKind(workflowPhaseRequirements):
		return "01-requirements.md"
	case workflowPhaseKind(workflowPhaseDesign):
		return "02-technical-design.md"
	case workflowPhaseKind(workflowPhaseTasks):
		return "03-task-breakdown.md"
	default:
		return ""
	}
}

func workflowPhaseFileNameWithExt(phaseID, ext string) string {
	fileName := workflowPhaseFileName(phaseID)
	return workflowPhaseFileNameApplyExt(fileName, ext)
}

func workflowPhaseKindFileNameWithExt(phase workflowPhaseKind, ext string) string {
	fileName := workflowPhaseKindFileName(phase)
	if fileName == "" {
		fileName = workflowPhaseFileName(phase.String())
	}
	return workflowPhaseFileNameApplyExt(fileName, ext)
}

func workflowPhaseFileNameApplyExt(fileName, ext string) string {
	ext = stableWorkflowFileExt(ext)
	if ext == "" {
		return fileName
	}
	return strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ext
}

func stableWorkflowFileExt(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		return ""
	}
	for _, r := range ext {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	return "." + ext
}

func sanitizeWorkflowPhaseFileStem(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if r == '-' || r == '_' || unicode.IsSpace(r) {
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	stem := strings.Trim(b.String(), "-")
	if stem == "" {
		return "workflow-phase"
	}
	return stem
}
