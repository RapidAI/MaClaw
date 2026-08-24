package agentservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostConfigProviderID     = "core-config"
	reviewedHostConfigImplementation = "local"
	reviewedHostConfigAdapterName    = "host_config_manage_self"
)

type reviewedHostConfigManager interface {
	AdministerReviewedHostConfig(ctx context.Context, principal Principal, maxIterations int, hasMax bool, thinkingMode string, hasThinking bool) (string, error)
}

func reviewedHostConfigInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"max_iterations": map[string]interface{}{"type": "integer"},
			"thinking_mode":  map[string]interface{}{"type": "string"},
		},
		"required":             []string{},
		"additionalProperties": false,
	}
}

func reviewedHostConfigContractDigest() string {
	return coretool.SchemaDigest([]byte("config.manage.self:v1:host-config-manage"))
}

func reviewedHostConfigDispatch(hasMax, hasThinking bool) (string, bool) {
	if hasMax && hasThinking {
		return "", false
	}
	if hasMax {
		return "max_iterations", true
	}
	if hasThinking {
		return "thinking_mode", true
	}
	return "get", true
}

func reviewedHostConfigThinkingMode(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "enabled", "enable", "on":
		return "enabled", true
	case "disabled", "disable", "off":
		return "disabled", true
	case "auto", "":
		return "", true
	default:
		return "", false
	}
}

// ProjectReviewedHostConfigProvider projects the host-owned agent-self
// configuration surface. It is not a Skill/MCP discovery entry and must not
// import the GUI manage_config / switch_llm_provider catalog. Field presence
// decides get versus a single safe mutation: empty object reads the redacted
// projection, max_iterations alone updates the loop cap, thinking_mode alone
// updates thinking. Provider, URL, key, model, action, export/import, and
// user-profile fields are rejected. This is not session.manage.coding.
// The host process observes the config store, so the handler result is the
// local completion receipt. PlanExecutionSucceeded does not mean the LLM
// provider was switched.
func ProjectReviewedHostConfigProvider(manager reviewedHostConfigManager) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if manager == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host config manager is unavailable")
	}
	parameters := reviewedHostConfigInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host config schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostConfigContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-config-max-iterations-xor-thinking-or-empty-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostConfigAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostConfigProviderID,
			ImplementationID: reviewedHostConfigImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityConfigManage,
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
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostConfig(manager)}, nil
}

func AttachReviewedHostConfigProvider(catalog DynamicSemanticCatalog, manager reviewedHostConfigManager) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostConfigProvider(manager)
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

func executeReviewedHostConfig(manager reviewedHostConfigManager) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if manager == nil {
			return "", fmt.Errorf("host_config_unavailable")
		}
		if len(args) > 2 {
			return "", fmt.Errorf("host_config_arguments_rejected")
		}
		maxIterations, hasMax := 0, false
		thinkingMode, hasThinking := "", false
		for key, raw := range args {
			switch key {
			case "max_iterations":
				n, ok := reviewedHostScheduleInt(raw)
				if !ok {
					return "", fmt.Errorf("host_config_arguments_rejected")
				}
				maxIterations, hasMax = n, true
			case "thinking_mode":
				value, ok := raw.(string)
				if !ok {
					return "", fmt.Errorf("host_config_arguments_rejected")
				}
				thinkingMode, hasThinking = strings.TrimSpace(value), true
			default:
				return "", fmt.Errorf("host_config_arguments_rejected")
			}
		}
		if _, ok := reviewedHostConfigDispatch(hasMax, hasThinking); !ok {
			return "", fmt.Errorf("host_config_field_presence_rejected")
		}
		return manager.AdministerReviewedHostConfig(ctx, principal, maxIterations, hasMax, thinkingMode, hasThinking)
	}
}

func (e *CoreAgentExecutor) SetReviewedHostConfigManager(manager reviewedHostConfigManager) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.configManager = manager
	e.mu.Unlock()
}

func (e *CoreAgentExecutor) getReviewedHostConfigManager() reviewedHostConfigManager {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.configManager
}

func (s *Service) AdministerReviewedHostConfig(ctx context.Context, principal Principal, maxIterations int, hasMax bool, thinkingMode string, hasThinking bool) (string, error) {
	if s == nil {
		return "", fmt.Errorf("host_config_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) == "" || strings.TrimSpace(principal.UserID) == "" {
		return "", fmt.Errorf("host_config_principal_required")
	}
	op, ok := reviewedHostConfigDispatch(hasMax, hasThinking)
	if !ok {
		return "", fmt.Errorf("host_config_field_presence_rejected")
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
	if op == "get" {
		cfg, err := s.GetUserConfig(ctx, principal)
		if err != nil {
			return "", err
		}
		return reviewedHostConfigProjection(cfg.AppConfig), nil
	}
	raw, err := s.GetRawUserConfig(ctx, principal)
	if err != nil {
		return "", err
	}
	next := raw.AppConfig
	switch op {
	case "max_iterations":
		if maxIterations < config.MinAgentIterations || maxIterations > config.MaxAgentIterationsCap {
			return "", fmt.Errorf("host_config_max_iterations_rejected")
		}
		next.MaclawAgentMaxIterations = maxIterations
	default:
		mode, modeOK := reviewedHostConfigThinkingMode(thinkingMode)
		if !modeOK {
			return "", fmt.Errorf("host_config_thinking_mode_rejected")
		}
		next.MaclawLLMThinkingMode = mode
	}
	updated, err := s.UpdateUserConfig(ctx, principal, next)
	if err != nil {
		return "", err
	}
	return "配置已更新。\n" + reviewedHostConfigProjection(updated.AppConfig), nil
}

func reviewedHostConfigProjection(cfg corelib.AppConfig) string {
	mode := strings.TrimSpace(cfg.MaclawLLMThinkingMode)
	if mode == "" {
		mode = "auto"
	}
	return fmt.Sprintf("当前配置:\n- max_iterations: %d\n- thinking_mode: %s\nLLM 服务商由宿主管理，不能在此切换。", config.EffectiveMaxIterations(cfg.MaclawAgentMaxIterations), mode)
}
