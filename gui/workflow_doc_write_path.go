package main

import (
	"path/filepath"
	"strings"
	"unicode"
)

func workflowDocWritePath(path string, args map[string]interface{}) string {
	phase := workflowPhaseKindFromMetadata(stringVal(args, "phase_id"), stringVal(args, "doc_type"))
	if phase == workflowPhaseUnknown {
		return path
	}
	fileName := workflowPhaseKindFileName(phase)
	dir := workflowDocWriteDir(strings.TrimSpace(path))
	if dir == "" || dir == "." {
		return fileName
	}
	return filepath.Join(dir, fileName)
}

func workflowDocWriteDir(path string) string {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return dir
	}
	if isLocalizedWorkflowDocDir(filepath.Base(dir)) {
		parent := filepath.Dir(dir)
		if parent == "." {
			return "."
		}
		return parent
	}
	return dir
}

func isLocalizedWorkflowDocDir(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, r := range name {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}
