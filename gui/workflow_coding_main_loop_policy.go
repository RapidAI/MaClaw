package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
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
	return false
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

func specializeCodingWorkflowImplementationMainLoopTools(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	out := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if tool.ExtractToolName(def) != "delegate_task" {
			out = append(out, def)
			continue
		}
		cloned := cloneToolDefinitionMap(def)
		specializeCodingWorkflowDelegateTool(cloned)
		out = append(out, cloned)
	}
	return out
}

func specializeCodingWorkflowDelegateTool(def map[string]interface{}) {
	fn, _ := def["function"].(map[string]interface{})
	if fn == nil {
		return
	}
	fn["description"] = "Delegate this coding implementation phase to the internal CodingSubAgent. In this phase, call only delegate_task with agent=\"coding_workflow\" and a concrete coding request."
	params, _ := fn["parameters"].(map[string]interface{})
	if params == nil {
		params = map[string]interface{}{"type": "object"}
		fn["parameters"] = params
	}
	props, _ := params["properties"].(map[string]interface{})
	if props == nil {
		props = map[string]interface{}{}
		params["properties"] = props
	}
	props["agent"] = map[string]interface{}{
		"type":        "string",
		"description": "Must be coding_workflow in coding implementation phases.",
		"enum":        []string{"coding_workflow"},
	}
	if _, ok := props["request"]; !ok {
		props["request"] = map[string]interface{}{
			"type":        "string",
			"description": "Concrete coding task for CodingSubAgent.",
		}
	}
	params["required"] = []string{"agent", "request"}
}

func cloneToolDefinitionMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = cloneToolDefinitionValue(v)
	}
	return out
}

func cloneToolDefinitionValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		return cloneToolDefinitionMap(typed)
	case map[string]string:
		out := make(map[string]string, len(typed))
		for k, value := range typed {
			out[k] = value
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, value := range typed {
			out[i] = cloneToolDefinitionValue(value)
		}
		return out
	default:
		return v
	}
}
