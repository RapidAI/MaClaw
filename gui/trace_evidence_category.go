package main

import "strings"

type traceEvidenceCategory string

const (
	traceEvidenceCategoryUnknown     traceEvidenceCategory = ""
	traceEvidenceCategoryError       traceEvidenceCategory = "error"
	traceEvidenceCategoryResult      traceEvidenceCategory = "result"
	traceEvidenceCategoryFile        traceEvidenceCategory = "file"
	traceEvidenceCategoryDecision    traceEvidenceCategory = "decision"
	traceEvidenceCategoryRepeatGuard traceEvidenceCategory = "repeat_guard"
)

func normalizeTraceEvidenceCategory(category string) traceEvidenceCategory {
	switch traceEvidenceCategory(strings.ToLower(strings.TrimSpace(category))) {
	case traceEvidenceCategoryError:
		return traceEvidenceCategoryError
	case traceEvidenceCategoryResult:
		return traceEvidenceCategoryResult
	case traceEvidenceCategoryFile:
		return traceEvidenceCategoryFile
	case traceEvidenceCategoryDecision:
		return traceEvidenceCategoryDecision
	case traceEvidenceCategoryRepeatGuard:
		return traceEvidenceCategoryRepeatGuard
	default:
		return traceEvidenceCategoryUnknown
	}
}
