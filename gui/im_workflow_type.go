package main

import (
	"strings"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func normalizeWorkflowType(value string) v2.WorkflowType {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return v2.WorkflowType(trimmed)
}

func isConcreteWorkflowType(value string) bool {
	wfType := normalizeWorkflowType(value)
	return wfType != "" && wfType != v2.WorkflowNone
}
