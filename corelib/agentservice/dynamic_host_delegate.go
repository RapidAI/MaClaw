package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostDelegateProviderID     = "core-delegate"
	reviewedHostDelegateImplementation = "local"
	reviewedHostDelegateAdapterName    = "host_agent_delegate_subtask"
	reviewedHostDelegateDefaultTimeout = 2 * time.Minute
	reviewedHostDelegateChildKey       = "delegate_child"
)

type reviewedHostDelegateRunner interface {
	RunReviewedHostDelegate(ctx context.Context, principal Principal, task string) (string, error)
}

func reviewedHostDelegateChild(metas ...map[string]string) bool {
	for _, meta := range metas {
		if strings.EqualFold(strings.TrimSpace(meta[reviewedHostDelegateChildKey]), "1") ||
			strings.EqualFold(strings.TrimSpace(meta[reviewedHostDelegateChildKey]), "true") {
			return true
		}
	}
	return false
}

func reviewedHostDelegateInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"task"},
		"additionalProperties": false,
	}
}

func reviewedHostDelegateContractDigest() string {
	return coretool.SchemaDigest([]byte("agent.delegate.subtask:v1:host-delegate"))
}

// ProjectReviewedHostDelegateProvider projects the host-owned child wait.
// It is not a Skill/MCP discovery entry and must not import GUI
// delegate_task. The closed schema accepts task only. delegate_to, role,
// and channel are rejected. No runner means the provider is not attached.
// Started is not completed; timeout is unknown.
func ProjectReviewedHostDelegateProvider(runner reviewedHostDelegateRunner) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if runner == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host delegate runner is unavailable")
	}
	parameters := reviewedHostDelegateInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host delegate schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostDelegateContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-delegate-task-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostDelegateAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostDelegateProviderID,
			ImplementationID: reviewedHostDelegateImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityDelegateSubtask,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostDelegate(runner)}, nil
}

func AttachReviewedHostDelegateProvider(catalog DynamicSemanticCatalog, runner reviewedHostDelegateRunner) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostDelegateProvider(runner)
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

func executeReviewedHostDelegate(runner reviewedHostDelegateRunner) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if runner == nil {
			return "", fmt.Errorf("host_delegate_runner_unavailable")
		}
		task, err := reviewedHostDelegateArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return runner.RunReviewedHostDelegate(ctx, principal, task)
	}
}

func reviewedHostDelegateArgsAllowed(args map[string]interface{}) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("host_delegate_arguments_rejected")
	}
	task := ""
	hasTask := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("host_delegate_arguments_rejected")
		}
		switch key {
		case "task":
			task, hasTask = value, true
		default:
			return "", fmt.Errorf("host_delegate_arguments_rejected")
		}
	}
	task = strings.TrimSpace(task)
	if !hasTask || task == "" {
		return "", fmt.Errorf("host_delegate_task_required")
	}
	return task, nil
}

func reviewedHostDelegateStartedIsNotComplete(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(lower, "started") && !strings.Contains(lower, "completed")
}

func (c *coreAgentCallbacks) RunReviewedHostDelegate(ctx context.Context, principal Principal, task string) (string, error) {
	if c == nil || c.delegateSubtask == nil || c.delegateChild || c.runtimeReadOnlyChild {
		return "", fmt.Errorf("host_delegate_runner_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_delegate_principal_mismatch")
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return "", fmt.Errorf("host_delegate_task_required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, reviewedHostDelegateDefaultTimeout)
	defer cancel()
	out, err := c.delegateSubtask(runCtx, principal, task)
	if runCtx.Err() != nil {
		return "", fmt.Errorf("host_delegate_timeout")
	}
	if err != nil {
		code := strings.TrimSpace(err.Error())
		if code == "host_delegate_timeout" || code == "host_delegate_started_is_not_complete" {
			return "", err
		}
		if reviewedHostDelegateStartedIsNotComplete(code) {
			return "", fmt.Errorf("host_delegate_started_is_not_complete")
		}
		return "", err
	}
	if reviewedHostDelegateStartedIsNotComplete(out) {
		return "", fmt.Errorf("host_delegate_started_is_not_complete")
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", fmt.Errorf("host_delegate_empty")
	}
	if strings.Contains(out, "delegate_to") || strings.Contains(out, "delegate_task") {
		return "", fmt.Errorf("host_delegate_legacy_name")
	}
	return out, nil
}
