package main

import (
	"fmt"
	"log"
	"strings"

	workflow "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

const (
	workflowFormPhaseField       = "_workflow_phase"
	workflowFormUserIDField      = "_workflow_user_id"
	workflowFormWorkflowIDField  = "_workflow_id"
	workflowFormProjectPathField = "project_path"
)

// emitWorkflowPhaseForm builds an AgentView form from the phase's InputSchema
// and emits it to the frontend via the standard AG UI lifecycle protocol.
// The form appears in the right-side task panel (AgentTaskPanel).
func (h *IMMessageHandler) emitWorkflowPhaseForm(userID string, schema *workflow.PhaseInputSchemaSpec, phaseID string) {
	if h == nil || h.app == nil || schema == nil || len(schema.Fields) == 0 {
		return
	}
	schema = localizeWorkflowPhaseInputSchema(schema, h.getWorkflowLang())

	var ws *workflow.EngineState
	workflowID := ""
	if ws != nil {
		workflowID = ws.ID
	}

	view := buildWorkflowPhaseFormAgentView(userID, workflowID, phaseID, schema)
	h.app.emitAgentView(view)
	log.Printf("[workflow-form] emitted AG UI form: phase=%s fields=%d", phaseID, len(schema.Fields))
}

func buildWorkflowPhaseFormAgentView(userID, workflowID, phaseID string, schema *workflow.PhaseInputSchemaSpec) map[string]interface{} {
	if schema == nil {
		return nil
	}
	fields := make([]map[string]interface{}, 0, len(schema.Fields)+3)
	for _, f := range schema.Fields {
		field := map[string]interface{}{
			"name":  f.Name,
			"label": f.Label,
			"type":  f.Type,
		}
		if f.Required {
			field["required"] = true
		}
		if f.Description != "" {
			field["description"] = f.Description
		}
		if f.Placeholder != "" {
			field["placeholder"] = f.Placeholder
		}
		if len(f.Options) > 0 {
			opts := make([]map[string]string, len(f.Options))
			for i, o := range f.Options {
				opts[i] = map[string]string{"label": o.Label, "value": o.Value}
			}
			field["options"] = opts
		}
		if f.Default != nil {
			field["value"] = f.Default
		}
		if f.Min != nil {
			field["min"] = *f.Min
		}
		if f.Max != nil {
			field["max"] = *f.Max
		}
		if f.MinLength != nil {
			field["minLength"] = *f.MinLength
		}
		if f.MaxLength != nil {
			field["maxLength"] = *f.MaxLength
		}
		if f.Pattern != "" {
			field["pattern"] = f.Pattern
		}
		fields = append(fields, field)
	}

	// Hidden fields carrying stable workflow routing. The agent loop clears
	// lastUserID after it finishes, so form submit must not depend on that
	// transient field.
	fields = append(fields, map[string]interface{}{
		"name":  workflowFormPhaseField,
		"type":  "hidden",
		"value": phaseID,
	})
	fields = append(fields, map[string]interface{}{
		"name":  workflowFormUserIDField,
		"type":  "hidden",
		"value": userID,
	})
	fields = append(fields, map[string]interface{}{
		"name":  workflowFormWorkflowIDField,
		"type":  "hidden",
		"value": workflowID,
	})

	viewID := "workflow:form:" + phaseID
	view := map[string]interface{}{
		"type":        "form",
		"id":          viewID,
		"title":       schema.Title,
		"description": schema.Description,
		"fields":      fields,
		"submitLabel": avTr("Submit", "提交"),
		"meta": map[string]interface{}{
			"source":   "workflow.phase_form",
			"phase_id": phaseID,
		},
	}
	return view
}

// handleWorkflowFormAgentViewSubmit processes the user's form submission from
// the AG UI task panel. It stores the form data via the v2 workflow engine.
// The next user message will trigger the agent loop with form data in the prompt.
func (a *App) handleWorkflowFormAgentViewSubmit(phaseID string, data map[string]interface{}, requestID string) *IMAgentResponse {
	phaseID = strings.TrimSpace(phaseID)
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return &IMAgentResponse{
			Text:           avTr("AI assistant not initialized.", "AI 助手尚未初始化。"),
			Error:          "missing hub client",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	handler := hubClient.ensureIMHandler()

	// Resolve the user ID from form hidden fields
	userID := workflowFormStringField(data, workflowFormUserIDField)
	if userID == "" && handler != nil {
		userID = handler.lastUserID
	}

	// Route to v2 workflow engine
	if handler != nil && handler.isWorkflowV2Active(userID) {
		resp := handler.handleWorkflowV2FormSubmit(userID, phaseID, data)
		if resp != nil {
			resp.ResponseSource = imResponseSourceAgentViewSubmit.String()
			return resp
		}
		return &IMAgentResponse{
			Text:           "✅ 表单已提交，正在生成文档...",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	// Fallback: WorkflowEngine adapter (only instantiated in tests).
	if a.workflowEngine != nil && a.workflowEngine.HasActiveWorkflow(userID) {
		return a.handleWorkflowFormV1EngineSubmit(userID, phaseID, data)
	}

	return &IMAgentResponse{
		Text:           "当前没有活跃的工作流。",
		Error:          "no active workflow",
		ResponseSource: imResponseSourceAgentViewSubmit.String(),
	}
}

// handleWorkflowFormV1EngineSubmit handles form submission when V2 machine is
// not available but WorkflowEngine adapter has an active workflow. It validates project_path,
// stores form data via engine.SubmitPhaseForm, and returns success or error.
func (a *App) handleWorkflowFormV1EngineSubmit(userID, phaseID string, data map[string]interface{}) *IMAgentResponse {
	engine := a.workflowEngine
	if engine == nil {
		return &IMAgentResponse{
			Text:           "当前没有活跃的工作流。",
			Error:          "no active workflow",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	ws := engine.GetActiveWorkflow(userID)
	if ws == nil {
		return &IMAgentResponse{
			Text:           "当前没有活跃的工作流。",
			Error:          "no active workflow",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	// Phase mismatch guard: if the form has a _workflow_phase field and it
	// doesn't match the current phase, reject.
	if submittedPhase := workflowFormStringField(data, workflowFormPhaseField); submittedPhase != "" {
		if submittedPhase != ws.CurrentPhase {
			return &IMAgentResponse{
				Text:  fmt.Sprintf("表单阶段不匹配 (submitted=%s, current=%s)", submittedPhase, ws.CurrentPhase),
				Error: "phase mismatch: submitted " + submittedPhase + " != current " + ws.CurrentPhase,
				ResponseSource: imResponseSourceAgentViewSubmit.String(),
			}
		}
	}

	// Validate project_path before mutating state.
	if pp := workflowFormStringField(data, workflowFormProjectPathField); pp != "" {
		_, _, err := normalizeWorkflowProjectPath(pp)
		if err != nil {
			return &IMAgentResponse{
				Text:           fmt.Sprintf("项目路径无效: %v", err),
				Error:          err.Error(),
				ResponseSource: imResponseSourceAgentViewSubmit.String(),
			}
		}
	}

	// Strip hidden routing fields from form data before storing.
	cleanData := make(map[string]interface{}, len(data))
	for k, v := range data {
		if k == workflowFormUserIDField || k == workflowFormWorkflowIDField || k == workflowFormPhaseField {
			continue
		}
		cleanData[k] = v
	}

	// Submit form data through the WorkflowEngine adapter.
	_, err := engine.SubmitPhaseForm(userID, cleanData)
	if err != nil {
		return &IMAgentResponse{
			Text:           "表单提交失败: " + err.Error(),
			Error:          err.Error(),
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	log.Printf("[workflow-v1-form] form submitted: user=%s phase=%s fields=%d", userID, phaseID, len(cleanData))

	return &IMAgentResponse{
		Text:           "✅ 信息已收到！发送「继续」开始生成文档。",
		ResponseSource: imResponseSourceAgentViewSubmit.String(),
	}
}

func resolveWorkflowFormUserID(handler *IMMessageHandler, engine *workflow.WorkflowEngine, phaseID string, data map[string]interface{}) string {
	if userID := workflowFormStringField(data, workflowFormUserIDField); userID != "" {
		return userID
	}
	if engine != nil {
		if userID, ok := engine.ActiveWorkflowUserIDForPhase(phaseID); ok {
			return userID
		}
	}
	return ""
}

func workflowFormStringField(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func workflowFormLifecyclePayload(data map[string]interface{}) map[string]interface{} {
	return workflowFormLifecyclePayloadFor("", "", "", data)
}

func workflowFormLifecyclePayloadFor(workflowID, phaseID, userID string, data map[string]interface{}) map[string]interface{} {
	payload := map[string]interface{}{}
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		workflowID = workflowFormStringField(data, workflowFormWorkflowIDField)
	}
	phaseID = strings.TrimSpace(phaseID)
	if phaseID == "" {
		phaseID = workflowFormStringField(data, workflowFormPhaseField)
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = workflowFormStringField(data, workflowFormUserIDField)
	}
	if workflowID != "" {
		payload["workflow_id"] = workflowID
	}
	if phaseID != "" {
		payload["workflow_phase"] = phaseID
	}
	if userID != "" {
		payload["workflow_user_id"] = userID
	}
	return payload
}

func workflowFormLifecyclePayloadWithFallback(workflowID, phaseID, userID string, data map[string]interface{}) map[string]interface{} {
	payload := workflowFormLifecyclePayload(data)
	if _, ok := payload["workflow_id"]; !ok {
		if workflowID = strings.TrimSpace(workflowID); workflowID != "" {
			payload["workflow_id"] = workflowID
		}
	}
	if _, ok := payload["workflow_phase"]; !ok {
		if phaseID = strings.TrimSpace(phaseID); phaseID != "" {
			payload["workflow_phase"] = phaseID
		}
	}
	if _, ok := payload["workflow_user_id"]; !ok {
		if userID = strings.TrimSpace(userID); userID != "" {
			payload["workflow_user_id"] = userID
		}
	}
	return payload
}

func workflowFormMatchesActiveWorkflow(engine *workflow.WorkflowEngine, userID, phaseID string, data map[string]interface{}) bool {
	if engine == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(phaseID) == "" {
		return false
	}
	phaseID = strings.TrimSpace(phaseID)
	if submittedPhaseID := workflowFormStringField(data, workflowFormPhaseField); submittedPhaseID != "" && submittedPhaseID != phaseID {
		return false
	}
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil || ws.CurrentPhase != phaseID {
		return false
	}
	if submittedWorkflowID := workflowFormStringField(data, workflowFormWorkflowIDField); submittedWorkflowID != "" && ws.ID != submittedWorkflowID {
		return false
	}
	return true
}

// buildIMFormGuidanceText generates a structured text prompt for IM channels
// (WeChat/Feishu/QQ) that cannot render AG UI forms. The text guides the user
// to provide information in a numbered format.
func buildIMFormGuidanceText(schema *workflow.PhaseInputSchemaSpec) string {
	if schema == nil || len(schema.Fields) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n\n", schema.Title))
	if schema.Description != "" {
		sb.WriteString(schema.Description + "\n\n")
	}
	sb.WriteString(avTr("Please provide the following information in order. Fields marked with * are required.\n\n", "请按顺序提供以下信息。带 * 的字段为必填。\n\n"))
	for i, f := range schema.Fields {
		prefix := " "
		if f.Required {
			prefix = "*"
		}
		sb.WriteString(fmt.Sprintf("%s%d. %s", prefix, i+1, f.Label))
		if len(f.Options) > 0 {
			labels := make([]string, 0, len(f.Options))
			for _, o := range f.Options {
				labels = append(labels, o.Label)
			}
			sb.WriteString(avTr(" (choose: ", "（可选：") + strings.Join(labels, " / ") + avTr(")", "）"))
		}
		if f.Placeholder != "" {
			sb.WriteString(avTr(" - ", " - ") + f.Placeholder)
		}
		sb.WriteString("\n")
	}
	sb.WriteString(avTr("\nReply by number, for example:\n1. My project\n2. Go\n3. Windows\n...", "\n请按编号回复，例如：\n1. 我的项目\n2. Go\n3. Windows\n..."))
	return sb.String()
}

// buildFormSubmissionSummary creates a brief text summary of the form data
// to use as the synthetic user message that triggers the agent loop.
func buildFormSubmissionSummary(data map[string]interface{}) string {
	if len(data) == 0 {
		return avTr("The user submitted the workflow form. Generate the phase output from the persisted form data.", "\u7528\u6237\u5df2\u63d0\u4ea4\u5de5\u4f5c\u6d41\u8868\u5355\u3002\u8bf7\u57fa\u4e8e\u5df2\u4fdd\u5b58\u7684\u8868\u5355\u6570\u636e\u751f\u6210\u9636\u6bb5\u8f93\u51fa\u3002")
	}
	var parts []string
	for k, v := range data {
		valStr := fmt.Sprintf("%v", v)
		if valStr != "" && valStr != "[]" && valStr != "<nil>" {
			parts = append(parts, fmt.Sprintf("%s: %s", k, valStr))
		}
	}
	if len(parts) == 0 {
		return avTr("The user submitted the workflow form. Generate the phase output from the persisted form data.", "\u7528\u6237\u5df2\u63d0\u4ea4\u5de5\u4f5c\u6d41\u8868\u5355\u3002\u8bf7\u57fa\u4e8e\u5df2\u4fdd\u5b58\u7684\u8868\u5355\u6570\u636e\u751f\u6210\u9636\u6bb5\u8f93\u51fa\u3002")
	}
	summary := strings.Join(parts, "; ")
	if len([]rune(summary)) > 200 {
		summary = string([]rune(summary)[:200]) + "..."
	}
	return avTr("The user submitted workflow form data: ", "\u7528\u6237\u5df2\u63d0\u4ea4\u5de5\u4f5c\u6d41\u8868\u5355\u6570\u636e\uff1a") + summary
}
