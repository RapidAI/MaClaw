package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

func normalizeWorkflowType(value string) workflow.WorkflowType {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return workflow.WorkflowType(trimmed)
}

func isConcreteWorkflowType(value string) bool {
	wfType := normalizeWorkflowType(value)
	return wfType != "" && wfType != workflow.WorkflowNone
}
