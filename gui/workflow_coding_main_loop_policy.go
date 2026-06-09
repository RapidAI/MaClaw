package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

var codingWorkflowImplementationMainLoopAllowedTools = map[string]bool{
	"ask_user":       true,
	"delegate_task":  true,
	"list_directory": true,
	"read_file":      true,
	"send_file":      true,
}

func codingWorkflowImplementationMainLoopRequiredTools() []string {
	return []string{"read_file", "list_directory", "delegate_task"}
}

func (h *IMMessageHandler) shouldConstrainCodingWorkflowImplementationMainLoop(policyUserID string) bool {
	engine := h.getWorkflowEngine()
	policyUserID = strings.TrimSpace(policyUserID)
	if engine == nil || policyUserID == "" || !engine.IsActivePhaseExecutionOrchestrator(policyUserID) {
		return false
	}
	ws := engine.GetActiveWorkflow(policyUserID)
	return ws != nil && ws.Type == workflow.WorkflowCoding && ws.CurrentPhase == workflow.PhaseCodingImplementation
}

func isCodingWorkflowImplementationMainLoopToolAllowed(name string) bool {
	return codingWorkflowImplementationMainLoopAllowedTools[strings.TrimSpace(name)]
}

func validateCodingWorkflowImplementationMainLoopToolCall(name string, args map[string]interface{}) string {
	name = strings.TrimSpace(name)
	if !isCodingWorkflowImplementationMainLoopToolAllowed(name) {
		return "coding workflow implementation is executed by CodingSubAgent; main agent cannot run local project mutation tools"
	}
	switch name {
	case "delegate_task":
		agentName := strings.ToLower(strings.TrimSpace(stringVal(args, "agent")))
		if agentName != "coding_workflow" {
			return "coding workflow implementation only allows delegate_task(agent=\"coding_workflow\")"
		}
	}
	return ""
}

func filterCodingWorkflowImplementationMainLoopTools(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if isCodingWorkflowImplementationMainLoopToolAllowed(tool.ExtractToolName(def)) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}
