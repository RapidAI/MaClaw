package main

import "github.com/RapidAI/CodeClaw/corelib/workflow"

// getWorkflowEngine returns nil while V1 workflow call sites are phased out.
func (h *IMMessageHandler) getWorkflowEngine() *workflow.WorkflowEngine {
	return nil
}
