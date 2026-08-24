package main

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedComputerUseAdapter        = "semantic_control_trusted_desktop"
	semanticTrustedComputerUseImplementation = "trusted-computer-control-v1"
)

func semanticUnpublishedLegacyComputerUseProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityComputerControlDesktop {
			return true
		}
	}
	return false
}

func semanticTrustedComputerUsePublished(h *IMMessageHandler) bool {
	return h != nil && (h.semanticTrustedComputerUse != nil || trustedComputerUseRuntimeAvailable(h))
}

func trustedComputerUseRuntimeAvailable(h *IMMessageHandler) bool {
	if h == nil || h.app == nil {
		return false
	}
	if cfg := h.app.PeekConfig(); cfg != nil {
		return computerUseEnabledFromConfig(cfg)
	}
	return true
}

func semanticTrustedComputerUseDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedComputerUseAdapter,
			"description": "Perform one host-observed desktop action. Group sessions and missing CU runtimes stay unmet.",
			"parameters":  semanticTrustedComputerUseInvocationSchema(),
		},
	}
}

func semanticTrustedComputerUseInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{"type": "string"},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
}

func semanticTrustedComputerUseArgsAllowed(args map[string]interface{}) (action string, err error) {
	if len(args) > 1 {
		return "", fmt.Errorf("trusted_computer_use_arguments_rejected")
	}
	hasAction := false
	for key, raw := range args {
		value, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("trusted_computer_use_arguments_rejected")
		}
		switch key {
		case "action":
			action, hasAction = value, true
		default:
			return "", fmt.Errorf("trusted_computer_use_arguments_rejected")
		}
	}
	action = strings.ToLower(strings.TrimSpace(action))
	if !hasAction {
		return "", fmt.Errorf("trusted_computer_use_action_required")
	}
	switch action {
	case "observe", "click", "done":
	default:
		return "", fmt.Errorf("trusted_computer_use_action_rejected")
	}
	return action, nil
}

func (h *IMMessageHandler) controlTrustedDesktop(principalID, action string) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_computer_use_runtime_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_computer_use_principal_required")
	}
	if h.semanticTrustedComputerUse != nil {
		return h.semanticTrustedComputerUse(principalID, action)
	}
	if !trustedComputerUseRuntimeAvailable(h) {
		return "", fmt.Errorf("trusted_computer_use_runtime_unavailable")
	}
	var text string
	switch action {
	case "observe":
		// The runtime already decided whether it saw the screen; reading that
		// verdict is the only way to keep a failed observation from arriving
		// as a successful one whose contents happen to be an error message.
		observed, ok := cuObserveResult(map[string]interface{}{})
		if !ok {
			return "", fmt.Errorf("trusted_computer_use_observe_failed")
		}
		text = observed
	case "click":
		return "", fmt.Errorf("trusted_computer_use_click_target_missing")
	case "done":
		// A refused completion changed nothing, so it is a definite failure
		// rather than an outcome nobody observed.
		completed, ok := cuDoneResult("")
		if !ok {
			return "", fmt.Errorf("trusted_computer_use_done_refused")
		}
		text = completed
	default:
		return "", fmt.Errorf("trusted_computer_use_action_rejected")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_computer_use_empty")
	}
	if strings.Contains(strings.ToLower(text), "disabled") {
		return "", fmt.Errorf("trusted_computer_use_runtime_unavailable")
	}
	return text, nil
}

func semanticTrustedComputerUseResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_computer_use_delivery_token")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_computer_use_empty")
	}
	return text, nil
}
