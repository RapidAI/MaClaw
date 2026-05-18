package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// emitWorkflowPhaseForm builds an AgentView form from the phase's InputSchema
// and emits it to the frontend via the standard AG UI lifecycle protocol.
// The form appears in the right-side task panel (AgentTaskPanel).
func (h *IMMessageHandler) emitWorkflowPhaseForm(userID string, schema *workflow.PhaseInputSchema, phaseID string) {
	if h == nil || h.app == nil || schema == nil || len(schema.Fields) == 0 {
		return
	}

	fields := make([]map[string]interface{}, 0, len(schema.Fields)+1)
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

	// Hidden field carrying the phase ID for submit routing.
	fields = append(fields, map[string]interface{}{
		"name":  "_workflow_phase",
		"type":  "hidden",
		"value": phaseID,
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
	h.app.emitAgentView(view)
	log.Printf("[workflow-form] emitted AG UI form: phase=%s fields=%d", phaseID, len(schema.Fields))
}

// handleWorkflowFormAgentViewSubmit processes the user's form submission from
// the AG UI task panel. It delegates to the workflow engine's SubmitPhaseForm
// and triggers the agent loop with the form data injected into the phase prompt.
func (a *App) handleWorkflowFormAgentViewSubmit(phaseID string, data map[string]interface{}) *IMAgentResponse {
	hubClient := a.ensureHubClient()
	if hubClient == nil {
		return &IMAgentResponse{
			Text:           "AI assistant not initialized.",
			Error:          "missing hub client",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	handler := hubClient.ensureIMHandler()
	engine := handler.getWorkflowEngine()
	if engine == nil {
		return &IMAgentResponse{
			Text:           "Workflow engine is not available.",
			Error:          "no workflow engine",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	userID := handler.lastUserID
	ws := engine.GetActiveWorkflow(userID)
	if ws == nil {
		return &IMAgentResponse{
			Text:           "No active workflow is available.",
			Error:          "no active workflow",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	if ws.CurrentPhase != phaseID {
		return &IMAgentResponse{
			Text:           "The workflow phase has changed. Please refresh and submit the current form.",
			Error:          fmt.Sprintf("phase mismatch: expected %s, got %s", ws.CurrentPhase, phaseID),
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	// Remove hidden/internal fields from form data before passing to engine.
	cleanData := make(map[string]interface{}, len(data))
	for k, v := range data {
		if !strings.HasPrefix(k, "_") {
			cleanData[k] = v
		}
	}

	resp, err := engine.SubmitPhaseForm(userID, cleanData)
	if err != nil {
		return &IMAgentResponse{
			Text:           "Form submission failed.",
			Error:          err.Error(),
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	a.clearAgentView("workflow:form:" + phaseID)

	if resp.RunAgentLoop && resp.PhasePrompt != "" {
		log.Printf("[workflow-form] form submitted: user=%s phase=%s fields=%d; triggering agent loop via synthetic message",
			userID, phaseID, len(cleanData))

		handler.stashedPhasePrompt.Store(userID, resp.PhasePrompt)
		handler.workflowAgentLoopMarker.Store(userID, true)

		summaryText := buildFormSubmissionSummary(cleanData)
		go func() {
			_, _ = a.SendAIAssistantMessage(AIAssistantSendRequest{Text: summaryText})
		}()

		return &IMAgentResponse{
			Text:           "Information submitted. Generating the workflow output now...",
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}

	if resp.Text != "" {
		return &IMAgentResponse{
			Text:           resp.Text,
			ResponseSource: imResponseSourceAgentViewSubmit.String(),
		}
	}
	return &IMAgentResponse{
		Text:           "Form submitted.",
		ResponseSource: imResponseSourceAgentViewSubmit.String(),
	}
}

// buildIMFormGuidanceText generates a structured text prompt for IM channels
// (WeChat/Feishu/QQ) that cannot render AG UI forms. The text guides the user
// to provide information in a numbered format.
func buildIMFormGuidanceText(schema *workflow.PhaseInputSchema) string {
	if schema == nil || len(schema.Fields) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s\n\n", schema.Title))
	if schema.Description != "" {
		sb.WriteString(schema.Description + "\n\n")
	}
	sb.WriteString("Please provide the following information in order. Fields marked with * are required.\n\n")
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
			sb.WriteString(" (choose: " + strings.Join(labels, " / ") + ")")
		}
		if f.Placeholder != "" {
			sb.WriteString(" - " + f.Placeholder)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nReply by number, for example:\n1. My project\n2. Go\n3. Windows\n...")
	return sb.String()
}

// buildFormSubmissionSummary creates a brief text summary of the form data
// to use as the synthetic user message that triggers the agent loop.
func buildFormSubmissionSummary(data map[string]interface{}) string {
	if len(data) == 0 {
		return "The user submitted the workflow form. Generate the phase output from the persisted form data."
	}
	var parts []string
	for k, v := range data {
		valStr := fmt.Sprintf("%v", v)
		if valStr != "" && valStr != "[]" && valStr != "<nil>" {
			parts = append(parts, fmt.Sprintf("%s: %s", k, valStr))
		}
	}
	if len(parts) == 0 {
		return "The user submitted the workflow form. Generate the phase output from the persisted form data."
	}
	summary := strings.Join(parts, "; ")
	if len([]rune(summary)) > 200 {
		summary = string([]rune(summary)[:200]) + "..."
	}
	return "The user submitted workflow form data: " + summary
}
