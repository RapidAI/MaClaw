package main

import (
	"fmt"
	"strings"

	mcputil "github.com/RapidAI/CodeClaw/corelib/mcp"
)

const mcpAgentViewCallArgsField = "_mcp_call_args"
const mcpAgentViewCorrectionMessage = "MCP tool parameters need correction. A task panel form has been opened on the right."

func (h *IMMessageHandler) emitMCPToolAgentView(serverRef, resolvedID, toolName string, inputSchema map[string]interface{}, toolArgs map[string]interface{}, validationErrs []mcputil.ValidationError) bool {
	return h.emitMCPToolAgentViewForOwner(serverRef, resolvedID, toolName, inputSchema, toolArgs, validationErrs, h.currentRuntimePolicyOwnerID())
}

func (h *IMMessageHandler) emitMCPToolAgentViewForOwner(serverRef, resolvedID, toolName string, inputSchema map[string]interface{}, toolArgs map[string]interface{}, validationErrs []mcputil.ValidationError, policyOwnerID string) bool {
	if h == nil || h.app == nil || len(inputSchema) == 0 || strings.TrimSpace(toolName) == "" {
		return false
	}
	view := buildMCPToolAgentViewWithPolicyOwner(serverRef, resolvedID, toolName, inputSchema, toolArgs, validationErrs, policyOwnerID)
	if view == nil {
		return false
	}
	return h.app.emitAgentView(view)
}

func (h *IMMessageHandler) handleMCPToolAgentViewSubmit(data map[string]interface{}) *IMAgentResponse {
	baseArgs, _ := data[mcpAgentViewCallArgsField].(map[string]interface{})
	callArgs := cloneMISInterfaceMap(baseArgs)
	toolArgs, _ := callArgs["arguments"].(map[string]interface{})
	if toolArgs == nil {
		toolArgs = map[string]interface{}{}
	}
	policyOwnerID, explicitRuntimeOwner := runtimePolicyOwnerIDFromToolArgsWithPresence(callArgs)

	serverRef := strings.TrimSpace(fmt.Sprint(callArgs["server_id"]))
	toolName := strings.TrimSpace(fmt.Sprint(callArgs["tool_name"]))
	if isDisabledExternalCodingSessionTool(toolName) {
		return &IMAgentResponse{Text: disabledExternalCodingSessionToolText(toolName), Error: "external coding-session MCP target disabled: " + toolName, ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	inputSchema := map[string]interface{}{}
	if serverRef != "" && toolName != "" {
		if resolvedID, isLocal, err := h.resolveMCPServerRef(serverRef); err == nil {
			inputSchema = h.lookupMCPInputSchema(resolvedID, toolName, isLocal)
		}
	}
	schemaTool := &RegisteredTool{InputSchema: inputSchema}
	for key, value := range data {
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(key, "_") {
			continue
		}
		toolArgs[key] = coerceRegisteredToolValue(schemaTool, key, value, toolArgs)
	}
	if validationIssues := registeredToolValidateArgIssues(*schemaTool, toolArgs); len(validationIssues) > 0 {
		validationErrors := registeredToolValidationMessages(validationIssues)
		if h != nil && h.app != nil {
			h.emitMCPToolAgentViewForOwner(serverRef, strings.TrimSpace(fmt.Sprint(callArgs["resolved_id"])), toolName, inputSchema, toolArgs, registeredToolValidationIssuesForMCP(validationIssues), policyOwnerID)
		}
		return &IMAgentResponse{Text: "MCP tool parameters need correction. Review the task panel.", Error: strings.Join(validationErrors, "; "), ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	callArgs["arguments"] = toolArgs
	policyOwnerID, explicitRuntimeOwner = consumeRuntimePolicyOwnerIDFromToolArgsWithPresence(callArgs)
	if explicitRuntimeOwner && policyOwnerID == "" {
		return &IMAgentResponse{Text: "MCP tool execution failed: runtime owner is missing; isolated runtime will not fall back to desktop loop.", Error: "runtime owner is missing", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if rejection := h.registeredToolWorkflowPolicyRejectionForOwner(policyOwnerID, "call_mcp_tool", callArgs); rejection != nil {
		return rejection
	}

	result := h.toolCallMCPTool(callArgs)
	if strings.TrimSpace(result) == mcpAgentViewCorrectionMessage {
		return &IMAgentResponse{Text: "MCP tool parameters need correction. Review the task panel.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
	}
	if h != nil && h.app != nil {
		h.app.emitAgentView(buildMCPToolResultAgentView(toolName, result))
	}
	return &IMAgentResponse{Text: "MCP tool completed from task panel.", ResponseSource: imResponseSourceAgentViewSubmit.String()}
}

func registeredToolValidationIssuesForMCP(issues []registeredToolValidationIssue) []mcputil.ValidationError {
	out := make([]mcputil.ValidationError, 0, len(issues))
	for _, issue := range issues {
		message := strings.TrimSpace(issue.Message)
		if message == "" {
			continue
		}
		out = append(out, mcputil.ValidationError{
			Code:    "schema_constraint",
			Param:   registeredToolTopLevelPath(issue.Path),
			Message: message,
		})
	}
	return out
}

func buildMCPToolAgentView(serverRef, resolvedID, toolName string, inputSchema map[string]interface{}, toolArgs map[string]interface{}, validationErrs []mcputil.ValidationError) map[string]interface{} {
	return buildMCPToolAgentViewWithPolicyOwner(serverRef, resolvedID, toolName, inputSchema, toolArgs, validationErrs, "")
}

func buildMCPToolAgentViewWithPolicyOwner(serverRef, resolvedID, toolName string, inputSchema map[string]interface{}, toolArgs map[string]interface{}, validationErrs []mcputil.ValidationError, policyOwnerID string) map[string]interface{} {
	if len(inputSchema) == 0 || strings.TrimSpace(toolName) == "" {
		return nil
	}
	missing := make([]string, 0, len(validationErrs))
	fieldErrors := map[string]string{}
	formErrors := make([]string, 0, len(validationErrs))
	for _, validationErr := range validationErrs {
		param := strings.TrimSpace(validationErr.Param)
		if param != "" {
			fieldErrors[param] = validationErr.Message
			if normalizeAgentViewValidationCodeKind(validationErr.Code).IsMissingRequired() {
				missing = append(missing, param)
			}
		}
		if strings.TrimSpace(validationErr.Message) != "" {
			formErrors = append(formErrors, validationErr.Message)
		}
	}
	tool := RegisteredTool{
		Name:        toolName,
		Description: avTr("Fill MCP tool parameters before running.", "请填写 MCP 工具参数后运行。"),
		InputSchema: inputSchema,
	}
	callContext := map[string]interface{}{
		"server_id":   firstNonEmptyMISAgentView(serverRef, resolvedID),
		"tool_name":   toolName,
		"arguments":   cloneMISInterfaceMap(toolArgs),
		"resolved_id": resolvedID,
	}
	if ownerID := strings.TrimSpace(policyOwnerID); ownerID != "" {
		callContext[registeredToolPolicyOwnerIDField] = ownerID
	}
	if view := registeredToolSpecializedAgentView(tool, toolArgs, registeredToolSpecializedAgentViewOptions{
		ViewID:      "mcp:call",
		TitlePrefix: avTr("Run MCP tool: ", "运行 MCP 工具："),
		Description: avTr("Server: ", "服务器：") + firstNonEmptyMISAgentView(serverRef, resolvedID),
		SubmitLabel: avTr("Run MCP tool", "运行 MCP 工具"),
		HiddenData: map[string]interface{}{
			mcpAgentViewCallArgsField: callContext,
		},
		Meta: map[string]interface{}{
			"source": "mcp.adapter",
			"server": firstNonEmptyMISAgentView(serverRef, resolvedID),
			"tool":   toolName,
		},
	}); view != nil {
		return attachAgentViewSchemaVersion(view, "mcp.adapter", firstNonEmptyMISAgentView(serverRef, resolvedID)+":"+toolName, inputSchema)
	}
	fields := registeredToolAgentViewFields(tool, toolArgs, missing)
	for _, field := range fields {
		name := strings.TrimSpace(fmt.Sprint(field["name"]))
		if msg := fieldErrors[name]; msg != "" {
			field["error"] = msg
		}
	}
	fields = append(fields, map[string]interface{}{
		"name":  mcpAgentViewCallArgsField,
		"label": mcpAgentViewCallArgsField,
		"type":  "hidden",
		"value": callContext,
	})
	if len(formErrors) == 0 {
		formErrors = []string{avTr("MCP tool parameters need correction before execution.", "MCP 工具参数需要修正后才能执行。")}
	}
	return attachAgentViewSchemaVersion(map[string]interface{}{
		"type":        "form",
		"id":          "mcp:call",
		"title":       avTr("Run MCP tool: ", "运行 MCP 工具：") + toolName,
		"description": avTr("Server: ", "服务器：") + firstNonEmptyMISAgentView(serverRef, resolvedID),
		"fields":      fields,
		"formErrors":  formErrors,
		"submitLabel": avTr("Run MCP tool", "运行 MCP 工具"),
		"meta": map[string]interface{}{
			"source": "mcp.adapter",
			"server": firstNonEmptyMISAgentView(serverRef, resolvedID),
			"tool":   toolName,
		},
	}, "mcp.adapter", firstNonEmptyMISAgentView(serverRef, resolvedID)+":"+toolName, inputSchema)
}

func buildMCPToolResultAgentView(toolName, result string) map[string]interface{} {
	title := strings.TrimSpace(toolName)
	if title == "" {
		title = avTr("MCP tool", "MCP 工具")
	}
	return map[string]interface{}{
		"type":        "result_browser",
		"id":          "mcp:result:" + title,
		"title":       title + avTr(" result", " 结果"),
		"description": avTr("MCP tool execution completed.", "MCP 工具执行完成。"),
		"results": []map[string]interface{}{{
			"id":     "result",
			"title":  avTr("Output", "输出"),
			"status": avTr("done", "完成"),
			"data": map[string]interface{}{
				"output": result,
			},
		}},
	}
}

func (h *IMMessageHandler) lookupMCPInputSchema(resolvedID, toolName string, isLocal bool) map[string]interface{} {
	if h == nil || strings.TrimSpace(resolvedID) == "" || strings.TrimSpace(toolName) == "" {
		return nil
	}
	if isLocal {
		if mgr := h.getLocalMCPManager(); mgr != nil {
			for _, ts := range mgr.GetAllTools() {
				if ts.ServerID == resolvedID {
					for _, t := range ts.Tools {
						if t.Name == toolName {
							return t.InputSchema
						}
					}
				}
			}
		}
		return nil
	}
	if registry := h.getMCPRegistry(); registry != nil {
		for _, t := range registry.GetServerTools(resolvedID) {
			if t.Name == toolName {
				return t.InputSchema
			}
		}
	}
	return nil
}
