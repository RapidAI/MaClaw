package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostTemplateProviderID     = "core-template"
	reviewedHostTemplateImplementation = "local"
	reviewedHostTemplateAdapterName    = "host_template_manage_session"
	reviewedHostTemplateNameMax        = 100
	reviewedHostTemplateToolMax        = 64
)

type reviewedHostTemplateManager interface {
	ManageReviewedHostTemplate(ctx context.Context, principal Principal, name, tool string) (string, error)
}

func reviewedHostTemplateInvocationSchema() map[string]interface{} {
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

func reviewedHostTemplateContractDigest() string {
	return coretool.SchemaDigest([]byte("template.manage.session:v1:host-template-manage"))
}

func reviewedHostTemplateDispatch(name, codingTool string) (string, bool) {
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

func reviewedHostTemplateTokenOK(value string, max int) bool {
	if value == "" {
		return true
	}
	if utf8.RuneCountInString(value) > max {
		return false
	}
	return !strings.ContainsAny(value, "\x00\n\r")
}

// ProjectReviewedHostTemplateProvider projects the host-owned session-template
// record. It is not a Skill/MCP discovery entry and must not import the GUI
// manage_template action catalog or launch a coding session. Field presence
// decides create/get/list. The model field is coding_tool because tool and
// tool_name are reserved invocation keys. action, launch, yolo_mode,
// model_config, env_vars, project_path, and template_name are rejected. This
// is not session.manage.coding or config.manage.self. The host process
// observes the template manager, so the handler result is the local
// completion receipt.
func ProjectReviewedHostTemplateProvider(manager reviewedHostTemplateManager) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if manager == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host template manager is unavailable")
	}
	parameters := reviewedHostTemplateInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host template schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostTemplateContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-template-name-and-coding-tool-or-name-or-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostTemplateAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostTemplateProviderID,
			ImplementationID: reviewedHostTemplateImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityTemplateManage,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostTemplate(manager)}, nil
}

func AttachReviewedHostTemplateProvider(catalog DynamicSemanticCatalog, manager reviewedHostTemplateManager) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostTemplateProvider(manager)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostTemplate(manager reviewedHostTemplateManager) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if manager == nil {
			return "", fmt.Errorf("host_template_unavailable")
		}
		if len(args) > 2 {
			return "", fmt.Errorf("host_template_arguments_rejected")
		}
		name, codingTool := "", ""
		for key, raw := range args {
			value, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("host_template_arguments_rejected")
			}
			switch key {
			case "name":
				name = value
			case "coding_tool":
				codingTool = value
			default:
				return "", fmt.Errorf("host_template_arguments_rejected")
			}
		}
		name, codingTool = strings.TrimSpace(name), strings.TrimSpace(codingTool)
		if _, ok := reviewedHostTemplateDispatch(name, codingTool); !ok {
			return "", fmt.Errorf("host_template_field_presence_rejected")
		}
		return manager.ManageReviewedHostTemplate(ctx, principal, name, codingTool)
	}
}

func (c *coreAgentCallbacks) ManageReviewedHostTemplate(ctx context.Context, principal Principal, name, codingTool string) (string, error) {
	if c == nil || c.templates == nil {
		return "", fmt.Errorf("host_template_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_template_principal_mismatch")
	}
	name, codingTool = strings.TrimSpace(name), strings.TrimSpace(codingTool)
	op, ok := reviewedHostTemplateDispatch(name, codingTool)
	if !ok {
		return "", fmt.Errorf("host_template_field_presence_rejected")
	}
	if !reviewedHostTemplateTokenOK(name, reviewedHostTemplateNameMax) {
		return "", fmt.Errorf("host_template_name_rejected")
	}
	if !reviewedHostTemplateTokenOK(codingTool, reviewedHostTemplateToolMax) {
		return "", fmt.Errorf("host_template_tool_rejected")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	switch op {
	case "create":
		if err := c.templates.Create(remote.SessionTemplate{Name: name, Tool: codingTool}); err != nil {
			if strings.Contains(err.Error(), "already exists") {
				return "", fmt.Errorf("host_template_already_exists")
			}
			return "", err
		}
		created, err := c.templates.Get(name)
		if err != nil || created == nil {
			return "", fmt.Errorf("host_template_create_failed")
		}
		return reviewedHostTemplateProjection("created", *created), nil
	case "get":
		current, err := c.templates.Get(name)
		if err != nil || current == nil {
			return "", fmt.Errorf("host_template_not_found")
		}
		return reviewedHostTemplateProjection("current", *current), nil
	default:
		listed := c.templates.List()
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

func reviewedHostTemplateProjection(kind string, tpl remote.SessionTemplate) string {
	switch kind {
	case "created":
		return fmt.Sprintf("模板已创建: %s（工具=%s）", tpl.Name, tpl.Tool)
	default:
		return fmt.Sprintf("模板 [%s]: 工具=%s", tpl.Name, tpl.Tool)
	}
}
