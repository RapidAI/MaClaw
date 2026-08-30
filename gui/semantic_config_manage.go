package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedConfigAdapter        = "semantic_administer_trusted_config"
	semanticTrustedConfigImplementation = "trusted-config-manage-v1"
)

func semanticUnpublishedLegacyConfigProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityConfigManageSelf {
			return true
		}
	}
	return false
}

func semanticTrustedConfigDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedConfigAdapter,
			"description": "Read or update the current principal's safe agent-self settings. Field presence decides get versus a single mutation of max_iterations or thinking_mode.",
			"parameters":  semanticTrustedConfigInvocationSchema(),
		},
	}
}

func semanticTrustedConfigInvocationSchema() map[string]interface{} {
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

func semanticTrustedConfigArgsAllowed(args map[string]interface{}) (maxIterations int, hasMax bool, thinkingMode string, hasThinking bool, err error) {
	if len(args) > 2 {
		return 0, false, "", false, fmt.Errorf("trusted_config_arguments_rejected")
	}
	for key, raw := range args {
		switch key {
		case "max_iterations":
			n, ok := semanticTrustedConfigInt(raw)
			if !ok {
				return 0, false, "", false, fmt.Errorf("trusted_config_arguments_rejected")
			}
			maxIterations, hasMax = n, true
		case "thinking_mode":
			value, ok := raw.(string)
			if !ok {
				return 0, false, "", false, fmt.Errorf("trusted_config_arguments_rejected")
			}
			thinkingMode, hasThinking = strings.TrimSpace(value), true
		default:
			return 0, false, "", false, fmt.Errorf("trusted_config_arguments_rejected")
		}
	}
	if _, ok := semanticTrustedConfigDispatch(hasMax, hasThinking); !ok {
		return 0, false, "", false, fmt.Errorf("trusted_config_field_presence_rejected")
	}
	return maxIterations, hasMax, thinkingMode, hasThinking, nil
}

func semanticTrustedConfigDispatch(hasMax, hasThinking bool) (string, bool) {
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

func semanticTrustedConfigThinkingMode(raw string) (string, bool) {
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

func semanticTrustedConfigInt(raw interface{}) (int, bool) {
	switch n := raw.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

func (h *IMMessageHandler) administerTrustedConfig(principalID string, maxIterations int, hasMax bool, thinkingMode string, hasThinking bool) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_config_unavailable")
	}
	if strings.TrimSpace(principalID) == "" {
		return "", fmt.Errorf("trusted_config_principal_required")
	}
	if h.semanticTrustedConfig != nil {
		return h.semanticTrustedConfig(principalID, maxIterations, hasMax, thinkingMode, hasThinking)
	}
	if h.app == nil {
		return "", fmt.Errorf("trusted_config_unavailable")
	}
	op, ok := semanticTrustedConfigDispatch(hasMax, hasThinking)
	if !ok {
		return "", fmt.Errorf("trusted_config_field_presence_rejected")
	}
	switch op {
	case "max_iterations":
		if maxIterations < config.MinAgentIterations || maxIterations > config.MaxAgentIterationsCap {
			return "", fmt.Errorf("trusted_config_max_iterations_rejected")
		}
		if err := h.app.SetMaclawAgentMaxIterations(maxIterations); err != nil {
			return "", err
		}
		return "配置已更新。\n" + semanticTrustedConfigProjection(h.app), nil
	case "thinking_mode":
		mode, modeOK := semanticTrustedConfigThinkingMode(thinkingMode)
		if !modeOK {
			return "", fmt.Errorf("trusted_config_thinking_mode_rejected")
		}
		if err := h.app.SetMaclawLLMThinkingMode(mode); err != nil {
			return "", err
		}
		return "配置已更新。\n" + semanticTrustedConfigProjection(h.app), nil
	default:
		return semanticTrustedConfigProjection(h.app), nil
	}
}

func semanticTrustedConfigProjection(app *App) string {
	maxIterations := config.MaxAgentIterationsCap
	// Unset thinking mode resolves to enabled (default-on), never "auto".
	mode := "enabled"
	if app != nil {
		maxIterations = config.EffectiveMaxIterations(app.GetMaclawAgentMaxIterations())
		if m := strings.TrimSpace(app.GetMaclawLLMThinkingMode()); m != "" {
			mode = m
		}
	}
	return fmt.Sprintf("当前配置:\n- max_iterations: %d\n- thinking_mode: %s\nLLM 服务商由宿主管理，不能在此切换。", maxIterations, mode)
}

func semanticTrustedConfigResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_config_delivery_token")
	}
	lower := strings.ToLower(text)
	for _, leaked := range []string{"sk-", "api_key", "llm_key", "llm_url", "http://", "https://"} {
		if strings.Contains(lower, leaked) {
			return "", fmt.Errorf("trusted_config_secret_leaked")
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_config_empty")
	}
	return text, nil
}
