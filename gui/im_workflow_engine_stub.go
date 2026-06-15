package main

import v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"

// getWorkflowEngine returns nil while V1 workflow call sites are phased out.
func (h *IMMessageHandler) getWorkflowEngine() *v2.WorkflowEngine {
	return nil
}
