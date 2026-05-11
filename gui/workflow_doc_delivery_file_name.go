package main

import (
	"path/filepath"
	"strings"
)

func workflowDocDeliveryFileName(fileName string, args map[string]interface{}) string {
	return workflowDocDeliveryFileNameWithFallbackExt(fileName, args, "")
}

func workflowDocDeliveryFileNameWithFallbackExt(fileName string, args map[string]interface{}, fallbackExt string) string {
	fileName = filepath.Base(strings.TrimSpace(fileName))
	phase := workflowPhaseKindFromMetadata(stringVal(args, "phase_id"), stringVal(args, "doc_type"))
	if phase == workflowPhaseUnknown {
		return fileName
	}
	ext := stableWorkflowFileExt(filepath.Ext(fileName))
	if ext == "" {
		ext = stableWorkflowFileExt(fallbackExt)
	}
	return workflowPhaseKindFileNameWithExt(phase, ext)
}
