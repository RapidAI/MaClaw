package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostSessionProviderID     = "core-session"
	reviewedHostSessionImplementation = "local"
	reviewedHostSessionAdapterName    = "host_session_manage_coding"
)

type reviewedHostSessionInspector interface {
	InspectReviewedHostSessions(ctx context.Context, principal Principal, id string) (string, error)
}

func reviewedHostSessionInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostSessionContractDigest() string {
	return coretool.SchemaDigest([]byte("session.manage.coding:v1:host-session-inspect"))
}

func reviewedHostSessionDispatch(id string) (string, bool) {
	if id == "" {
		return "list", true
	}
	return "get", true
}

// ProjectReviewedHostSessionProvider projects the host-owned coding-session
// inspect surface. It is not a Skill/MCP discovery entry and must not import
// the GUI list_sessions / send_input / interrupt_session catalog. Field
// presence decides list versus get: empty object lists, id inspects. Drive,
// interrupt, kill, send, launch, provider, and project fields are rejected.
// This is not template.manage.session or agent.delegate.subtask. The host
// process observes the inspect result, so the handler result is the local
// completion receipt. PlanExecutionSucceeded does not mean a session was
// driven, interrupted, or launched.
func ProjectReviewedHostSessionProvider(inspector reviewedHostSessionInspector) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if inspector == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host session inspector is unavailable")
	}
	parameters := reviewedHostSessionInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host session schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostSessionContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-session-id-or-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostSessionAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostSessionProviderID,
			ImplementationID: reviewedHostSessionImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilitySessionManage,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostSession(inspector)}, nil
}

func AttachReviewedHostSessionProvider(catalog DynamicSemanticCatalog, inspector reviewedHostSessionInspector) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostSessionProvider(inspector)
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

func executeReviewedHostSession(inspector reviewedHostSessionInspector) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if inspector == nil {
			return "", fmt.Errorf("host_session_unavailable")
		}
		if len(args) > 1 {
			return "", fmt.Errorf("host_session_arguments_rejected")
		}
		id := ""
		for key, raw := range args {
			if key != "id" {
				return "", fmt.Errorf("host_session_arguments_rejected")
			}
			value, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("host_session_arguments_rejected")
			}
			id = strings.TrimSpace(value)
		}
		if _, ok := reviewedHostSessionDispatch(id); !ok {
			return "", fmt.Errorf("host_session_field_presence_rejected")
		}
		return inspector.InspectReviewedHostSessions(ctx, principal, id)
	}
}

func (c *coreAgentCallbacks) InspectReviewedHostSessions(ctx context.Context, principal Principal, id string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("host_session_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_session_principal_mismatch")
	}
	id = strings.TrimSpace(id)
	if _, ok := reviewedHostSessionDispatch(id); !ok {
		return "", fmt.Errorf("host_session_field_presence_rejected")
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
	if id == "" {
		return "当前没有编码会话。", nil
	}
	return "", fmt.Errorf("host_session_not_found")
}
