package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostComputerUseProviderID     = "core-computeruse"
	reviewedHostComputerUseImplementation = "bound-desktop"
	reviewedHostComputerUseAdapterName    = "host_computer_control_desktop"
	reviewedHostComputerUseDefaultTimeout = 30 * time.Second
)

type reviewedHostComputerUseController interface {
	ControlReviewedHostDesktop(ctx context.Context, principal Principal, action string) (string, error)
}

func reviewedHostComputerUseInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

func reviewedHostComputerUseContractDigest() string {
	return coretool.SchemaDigest([]byte("computer.control.desktop:v1:host-computeruse"))
}

// ProjectReviewedHostComputerUseProvider projects the host-owned desktop
// action. It is not a Skill/MCP discovery entry and must not import GUI
// computer_*. The closed schema accepts action only. No CU runtime means
// the provider is not attached. Timeout or a missing runtime after publish
// is unknown. Click without a host target fails closed.
func ProjectReviewedHostComputerUseProvider(controller reviewedHostComputerUseController) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if controller == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host computer-use controller is unavailable")
	}
	parameters := reviewedHostComputerUseInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host computer-use schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostComputerUseContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-computeruse-action-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostComputerUseAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostComputerUseProviderID,
			ImplementationID: reviewedHostComputerUseImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityComputerUse,
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectExternalEffect},
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostComputerUse(controller)}, nil
}

func AttachReviewedHostComputerUseProvider(catalog DynamicSemanticCatalog, controller reviewedHostComputerUseController) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostComputerUseProvider(controller)
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

func executeReviewedHostComputerUse(controller reviewedHostComputerUseController) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if controller == nil {
			return "", fmt.Errorf("host_computer_use_runtime_unavailable")
		}
		action, err := reviewedHostComputerUseArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return controller.ControlReviewedHostDesktop(ctx, principal, action)
	}
}

func reviewedHostComputerUseArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("host_computer_use_arguments_rejected")
	}
	action := ""
	hasAction := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("host_computer_use_arguments_rejected")
		}
		switch key {
		case "action":
			action, hasAction = value, true
		default:
			return "", fmt.Errorf("host_computer_use_arguments_rejected")
		}
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if !hasAction {
		return "", fmt.Errorf("host_computer_use_action_required")
	}
	switch action {
	case "observe", "click", "done":
	default:
		return "", fmt.Errorf("host_computer_use_action_rejected")
	}
	return action, nil
}

func reviewedHostComputerUseResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("host_computer_use_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("host_computer_use_empty")
	}
	return text, nil
}

func (c *coreAgentCallbacks) ControlReviewedHostDesktop(ctx context.Context, principal Principal, action string) (string, error) {
	if c == nil || c.trustedComputerUse == nil || c.delegateChild || c.runtimeReadOnlyChild {
		return "", fmt.Errorf("host_computer_use_runtime_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_computer_use_principal_mismatch")
	}
	action, err := reviewedHostComputerUseArgsAllowed(map[string]interface{}{"action": action})
	if err != nil {
		return "", err
	}
	if action == "click" {
		return "", fmt.Errorf("host_computer_use_click_target_missing")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, reviewedHostComputerUseDefaultTimeout)
	defer cancel()
	out, err := c.trustedComputerUse(runCtx, principal, action)
	if runCtx.Err() != nil {
		return "", fmt.Errorf("host_computer_use_timeout")
	}
	if err != nil {
		code := strings.TrimSpace(err.Error())
		if dynamicHostObservedExternalUnknown(code) {
			return "", err
		}
		lower := strings.ToLower(code)
		if strings.Contains(lower, "timeout") {
			return "", fmt.Errorf("host_computer_use_timeout")
		}
		if strings.Contains(lower, "unavailable") || strings.Contains(lower, "disabled") {
			return "", fmt.Errorf("host_computer_use_runtime_unavailable")
		}
		return "", err
	}
	return reviewedHostComputerUseResultProjection(out)
}
