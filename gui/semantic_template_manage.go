package main

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedTemplateAdapter        = "semantic_administer_trusted_template"
	semanticTrustedTemplateImplementation = "trusted-template-manage-v1"
	semanticTrustedTemplateNameMax        = 100
	semanticTrustedTemplateToolMax        = 64
	semanticTrustedTemplateTimeout        = 10 * time.Second
)

func semanticUnpublishedLegacyTemplateProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityTemplateManageSession {
			return true
		}
	}
	return false
}

func semanticTrustedTemplateDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedTemplateAdapter,
			"description": "Read or record a session template. Field presence decides create, get, or list. This does not launch a session.",
			"parameters":  semanticTrustedTemplateInvocationSchema(),
		},
	}
}

func semanticTrustedTemplateInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":        map[string]interface{}{"type": "string"},
			"coding_tool": map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func semanticTrustedTemplateArgsAllowed(args map[string]interface{}) (name, codingTool string, err error) {
	if len(args) > 2 {
		return "", "", fmt.Errorf("trusted_template_arguments_rejected")
	}
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("trusted_template_arguments_rejected")
		}
		switch key {
		case "name":
			name = strings.TrimSpace(value)
		case "coding_tool":
			codingTool = strings.TrimSpace(value)
		default:
			return "", "", fmt.Errorf("trusted_template_arguments_rejected")
		}
	}
	if _, ok := semanticTrustedTemplateDispatch(name, codingTool); !ok {
		return "", "", fmt.Errorf("trusted_template_field_presence_rejected")
	}
	return name, codingTool, nil
}

func semanticTrustedTemplateDispatch(name, codingTool string) (string, bool) {
	hasName := name != ""
	hasTool := codingTool != ""
	if hasName && hasTool {
		return "create", true
	}
	if hasName {
		return "get", true
	}
	if hasTool {
		return "", false
	}
	return "list", true
}

func semanticTrustedTemplateTokenOK(value string, max int) bool {
	if value == "" {
		return true
	}
	if utf8.RuneCountInString(value) > max {
		return false
	}
	return !strings.ContainsAny(value, "\x00\n\r")
}

func (h *IMMessageHandler) administerTrustedTemplate(principalID, name, codingTool string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_template_unavailable")
	}
	if strings.TrimSpace(principalID) == "" {
		return "", fmt.Errorf("trusted_template_principal_required")
	}
	if h.semanticTrustedTemplate != nil {
		return h.semanticTrustedTemplate(principalID, name, codingTool)
	}
	if h.templateManager == nil {
		return "", fmt.Errorf("trusted_template_unavailable")
	}
	name, codingTool = strings.TrimSpace(name), strings.TrimSpace(codingTool)
	op, ok := semanticTrustedTemplateDispatch(name, codingTool)
	if !ok {
		return "", fmt.Errorf("trusted_template_field_presence_rejected")
	}
	if !semanticTrustedTemplateTokenOK(name, semanticTrustedTemplateNameMax) {
		return "", fmt.Errorf("trusted_template_name_rejected")
	}
	if !semanticTrustedTemplateTokenOK(codingTool, semanticTrustedTemplateToolMax) {
		return "", fmt.Errorf("trusted_template_tool_rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), semanticTrustedTemplateTimeout)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	switch op {
	case "create":
		if err := h.templateManager.Create(remote.SessionTemplate{Name: name, Tool: codingTool}); err != nil {
			if strings.Contains(err.Error(), "already exists") {
				return "", fmt.Errorf("trusted_template_already_exists")
			}
			return "", err
		}
		created, err := h.templateManager.Get(name)
		if err != nil || created == nil {
			return "", fmt.Errorf("trusted_template_create_failed")
		}
		return semanticTrustedTemplateProjection("created", *created), nil
	case "get":
		current, err := h.templateManager.Get(name)
		if err != nil || current == nil {
			return "", fmt.Errorf("trusted_template_not_found")
		}
		return semanticTrustedTemplateProjection("current", *current), nil
	default:
		listed := h.templateManager.List()
		if len(listed) == 0 {
			return "当前没有会话模板。", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "共 %d 个模板:\n", len(listed))
		for _, item := range listed {
			fmt.Fprintf(&b, "- %s: 工具=%s\n", item.Name, item.Tool)
		}
		return strings.TrimRight(b.String(), "\n"), nil
	}
}

func semanticTrustedTemplateProjection(kind string, tpl remote.SessionTemplate) string {
	switch kind {
	case "created":
		return fmt.Sprintf("模板已创建: %s（工具=%s）", tpl.Name, tpl.Tool)
	default:
		return fmt.Sprintf("模板 [%s]: 工具=%s", tpl.Name, tpl.Tool)
	}
}

func semanticTrustedTemplateResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_template_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_template_empty")
	}
	return text, nil
}
