package main

import (
	"fmt"
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
				// Only constrain the CODING workflow's implementation phase.
				// Other workflows with ToolPolicyFull (e.g. pa_disclosure_parsing
				// in patent_application) need full tool access for document parsing.
				if state.Type != string(v2.WorkflowCoding) {
					return false
				}
				return true
			}
		}
	}
	if wf := h.getWorkflowV2(); wf != nil && wf.machine != nil {
		if state := wf.machine.GetActive(policyUserID); state != nil {
			if state.Status == v2.StatusActive && state.IsExecutionPhase() {
				if state.Type != string(v2.WorkflowCoding) {
					return false
				}
				return true
			}
		}
	}
	if h != nil && h.app != nil && h.app.workflowEngine != nil {
		if state := h.app.workflowEngine.GetActiveWorkflow(policyUserID); state != nil {
			return state.Type == v2.WorkflowCoding && state.CurrentPhase == v2.PhaseCodingImplementation
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
	// V2 StateMachine is the sole source of truth. Convert V2 state to
	// the EngineState type expected by callers.
	wf := h.getWorkflowV2()
	if wf == nil || wf.machine == nil {
		return nil, nil
	}
	v2State := wf.machine.GetActive(policyUserID)
	if v2State == nil {
		return nil, nil
	}
	state := mapV2StateToV1(v2State)
	if state == nil {
		return nil, nil
	}
	// Use the engine registry as the canonical source for phase semantics.
	// It preserves normalized metadata such as Kind and MutationScope, which
	// some runtime policy checks depend on for artifact-generation phases.
	if h.app.workflowEngine != nil {
		if registry := h.app.workflowEngine.GetRegistry(); registry != nil {
			if tmpl := registry.Match(v2.WorkflowType(v2State.Type)); tmpl != nil {
				metaByID := make(map[string]v2.PhaseMeta, len(tmpl.Phases))
				for _, meta := range v2.PhaseMetadata(tmpl) {
					metaByID[meta.ID] = meta
				}
				for i := range tmpl.Phases {
					if tmpl.Phases[i].ID == state.CurrentPhase {
						ps := tmpl.Phases[i]
						if meta, ok := metaByID[ps.ID]; ok {
							if ps.Kind == "" {
								ps.Kind = meta.Kind
							}
							if ps.MutationScope == "" {
								ps.MutationScope = meta.MutationScope
							}
						}
						return state, &ps
					}
				}
			}
		}
	}
	if wf.registry == nil {
		return state, nil
	}
	tmpl := wf.registry.Get(v2State.Type)
	if tmpl == nil {
		return state, nil
	}
	for i := range tmpl.Phases {
		if tmpl.Phases[i].ID == state.CurrentPhase {
			ps := v2.PhaseSpec{
				ID:            tmpl.Phases[i].ID,
				Name:          tmpl.Phases[i].Name,
				NeedsConfirm:  tmpl.Phases[i].NeedsConfirm,
				ToolPolicy:    v2.ToolFilterPolicy(tmpl.Phases[i].ToolPolicy),
				Kind:          tmpl.Phases[i].Kind,
				MutationScope: tmpl.Phases[i].MutationScope,
			}
			return state, &ps
		}
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
	"bash":                     true,
	"craft_tool":               true,
	"generate_pdf":             true,
	"list_directory":           true,
	"manage_skill":             true,
	"office":                   true,
	"read_file":                true,
	"search_and_install_skill": true,
	"send_file":                true,
	"web_fetch":                true,
	"web_search":               true,
	"write_file":               true,
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
	return []string{
		"bash",
		"list_directory",
		"write_file",
		"manage_skill",
		"search_and_install_skill",
		"craft_tool",
		"office",
		"generate_pdf",
		"send_file",
	}
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
	case "bash":
		// Check if the bash command explicitly references source directories.
		// Unlike craft_tool (which has natural-language task text), bash commands
		// directly contain paths, so we check path tokens without requiring
		// mutation-intent keywords — any bash command targeting a source dir
		// in artifact phase is suspicious.
		if cmd := stringVal(args, "command"); cmd != "" {
			for _, token := range workflowMutationReferenceTokens(cmd) {
				if isWorkflowProjectMutationPath(token) {
					return "artifact workflow phase cannot run bash commands that target source/project paths. Use a non-source output directory instead."
				}
			}
		}
	case "craft_tool":
		if text := firstNonEmptyStringValue(args, "task", "instructions", "description", "user_prompt"); text != "" && containsWorkflowProjectMutationReference(text) {
			return "artifact workflow phase cannot craft tools that mutate source/project paths"
		}
	}
	if path != "" {
		if dir := matchedProjectMutationDir(path); dir != "" {
			return fmt.Sprintf("artifact workflow phase cannot write into source/project paths (matched: %s/). Use a temp or output directory instead.", dir)
		}
		if v2.IsProjectControlPath(path) {
			return fmt.Sprintf("artifact workflow phase cannot write project control files (matched: %s). Use a temp or output directory instead.", path)
		}
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
	return matchedProjectMutationDir(path) != ""
}

// matchedProjectMutationDir returns the matched source directory name (e.g., "src")
// if the path appears to target a project source directory, or "" if not.
func matchedProjectMutationDir(path string) string {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"))
	normalized = strings.TrimPrefix(normalized, "./")

	// Strip Windows drive letter prefix (e.g. "d:/专利申请测试1/gen.js" → "专利申请测试1/gen.js")
	if len(normalized) >= 3 && normalized[1] == ':' && normalized[2] == '/' {
		normalized = normalized[3:]
	}

	// Only directory-prefix matching determines "project mutation".
	// File extension is NOT a valid signal — artifact generation phases
	// legitimately write .js/.py/.ps1 tool scripts to produce deliverables
	// (e.g., a Node.js script that calls pptxgenjs to generate a .pptx file).
	// Using extension matching causes false positives that create unbreakable
	// dead loops (LLM cannot change the extension because it needs that
	// language to generate the artifact).
	//
	// We check both as prefix (relative paths: "src/main.go") and as a path
	// component anywhere in the path (absolute paths: "workprj/myproject/src/main.go").
	// The component check uses "/dir/" to avoid substring matches within longer
	// directory names (e.g., "/webapp/" should not match "app/").
	for _, dir := range []string{"src", "cmd", "internal", "pkg", "frontend", "backend"} {
		if strings.HasPrefix(normalized, dir+"/") {
			return dir
		}
		if strings.Contains(normalized, "/"+dir+"/") {
			return dir
		}
	}
	// "app/" and "web/" are common false-positive sources in absolute paths:
	// "AppData" contains "app", "webapp" contains "web".
	// Only match as exact first path component (relative paths from project root).
	for _, dir := range []string{"app", "web"} {
		if strings.HasPrefix(normalized, dir+"/") {
			return dir
		}
	}
	return ""
}

func containsWorkflowProjectMutationReference(text string) bool {
	if !workflowTextHasProjectMutationIntent(text) {
		return false
	}
	for _, token := range workflowMutationReferenceTokens(text) {
		if isWorkflowProjectMutationPath(token) {
			return true
		}
	}
	return false
}

func workflowTextHasProjectMutationIntent(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	for _, marker := range []string{
		"write", "modify", "update", "edit", "patch", "refactor", "overwrite", "save", "create",
		"写", "写入", "修改", "更新", "编辑", "补丁", "重构", "覆盖", "保存", "创建", "新建",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func workflowMutationReferenceTokens(text string) []string {
	fields := strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(text, "\\", "/")), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == '/')
	})
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.Trim(strings.TrimSpace(field), `"'“”‘’()[]{}<>，。！？；：、,;:`)
		token = strings.TrimPrefix(token, "./")
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
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
