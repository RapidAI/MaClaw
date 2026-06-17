package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
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
	policyUserID = strings.TrimSpace(policyUserID)
	if policyUserID == "" {
		return false
	}
	if h != nil && h.isWorkflowV2Active(policyUserID) {
		if wf := h.getWorkflowV2(); wf != nil {
			if state := wf.machine.GetActive(policyUserID); state != nil && state.IsExecutionPhase() {
				return true
			}
		}
	}
	if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil {
		if state := wf.machine.GetActive(policyUserID); state != nil {
			return state.Status == v2.StatusActive && state.IsExecutionPhase()
		}
	}
	if state, _ := h.activeWorkflowPhaseFromEngine(policyUserID); state != nil {
		return state.Type == v2.WorkflowCoding && state.CurrentPhase == v2.PhaseCodingImplementation
	}
	return false
}

func (h *IMMessageHandler) activeWorkflowPhaseFromEngine(policyUserID string) (*v2.EngineState, *v2.PhaseSpec) {
	policyUserID = strings.TrimSpace(policyUserID)
	if policyUserID == "" || h == nil || h.app == nil {
		return nil, nil
	}
	// workflowEngine is never instantiated in production — this path only
	// executes in tests that explicitly set h.app.workflowEngine.
	engine := h.app.workflowEngine
	if engine == nil {
		return nil, nil
	}
	state := engine.GetActiveWorkflow(policyUserID)
	if state == nil || engine.GetRegistry() == nil {
		return state, nil
	}
	tmpl := engine.GetRegistry().Match(state.Type)
	if tmpl == nil {
		return state, nil
	}
	for i := range tmpl.Phases {
		if tmpl.Phases[i].ID == state.CurrentPhase {
			return state, &tmpl.Phases[i]
		}
	}
	if state.PhaseIndex >= 0 && state.PhaseIndex < len(tmpl.Phases) {
		return state, &tmpl.Phases[state.PhaseIndex]
	}
	return state, nil
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

var workflowArtifactPhaseAllowedTools = map[string]bool{
	"generate_pdf": true,
	"office":       true,
	"read_file":    true,
	"send_file":    true,
	"web_fetch":    true,
	"web_search":   true,
	"write_file":   true,
}

func isWorkflowArtifactPhaseToolAllowed(name string) bool {
	return workflowArtifactPhaseAllowedTools[strings.TrimSpace(name)]
}

func filterWorkflowArtifactPhaseTools(tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if isWorkflowArtifactPhaseToolAllowed(tool.ExtractToolName(def)) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

func workflowArtifactPhaseRequiredTools() []string {
	return []string{"write_file", "office", "generate_pdf", "send_file"}
}

func (h *IMMessageHandler) isWorkflowArtifactPhase(policyUserID string) bool {
	_, phase := h.activeWorkflowPhaseFromEngine(policyUserID)
	return phase != nil && (phase.Kind == v2.PhaseKindArtifactGeneration || phase.MutationScope == v2.MutationScopeArtifact)
}

func validateWorkflowArtifactPhaseToolCall(name string, args map[string]interface{}) string {
	name = strings.TrimSpace(name)
	if !isWorkflowArtifactPhaseToolAllowed(name) {
		return "artifact workflow phase cannot run project mutation tools"
	}
	path := ""
	switch name {
	case "write_file":
		path = stringVal(args, "path")
	case "office":
		path = firstNonEmptyStringValue(args, "file_path", "path", "output")
	case "web_fetch":
		path = firstNonEmptyStringValue(args, "save_path", "output")
	}
	if path != "" && isWorkflowProjectMutationPath(path) {
		return "artifact workflow phase cannot write into source/project paths"
	}
	return ""
}

func firstNonEmptyStringValue(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringVal(args, key)); value != "" {
			return value
		}
	}
	return ""
}

func isWorkflowProjectMutationPath(path string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
	normalized = strings.TrimPrefix(normalized, "./")
	for _, prefix := range []string{"src/", "app/", "cmd/", "internal/", "pkg/", "web/", "frontend/", "backend/"} {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	for _, suffix := range []string{".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".rs", ".cpp", ".c", ".h", ".cs"} {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}
	return false
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
