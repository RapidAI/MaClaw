package corelib

import (
	"net/url"
	"strings"
)

// ReasoningAPIKind identifies the request schema that receives a reasoning
// control. Providers use several incompatible spellings for the same user
// intent, so callers must select the schema rather than forwarding one generic
// field to every endpoint.
type ReasoningAPIKind string

const (
	ReasoningAPIChat      ReasoningAPIKind = "chat"
	ReasoningAPIResponses ReasoningAPIKind = "responses"
	ReasoningAPIAnthropic ReasoningAPIKind = "anthropic"
)

// ApplyReasoningControls translates an explicit global thinking setting into
// the native request shape of the selected provider. It deliberately does
// nothing in auto mode, leaving the provider/model default intact.
//
// An explicit user choice always wins over a caller-supplied reasoning field.
// This is important for forwarded OpenAI-compatible requests: otherwise a
// DeepSeek default or a stale pass-through field can silently undo “Off”.
func ApplyReasoningControls(cfg MaclawLLMConfig, body map[string]interface{}, api ReasoningAPIKind) {
	if body == nil {
		return
	}

	mode := normalizeReasoningMode(cfg.ThinkingMode)
	if mode == "" {
		return
	}

	// Clear all alternate spellings before writing the one supported by the
	// selected provider. Mixing them causes 400 responses on several compatible
	// gateways and can make the setting appear to be ignored.
	delete(body, "thinking")
	delete(body, "reasoning")
	delete(body, "reasoning_effort")
	delete(body, "enable_thinking")

	if api != ReasoningAPIAnthropic && usesOpenAIStyleReasoning(cfg) {
		effort := reasoningEffortForMode(mode, cfg.ReasoningEffort)
		if api == ReasoningAPIResponses {
			// Responses streams expose the user-displayable reasoning through
			// response.reasoning_summary_text.delta only when a summary is
			// requested. The internal chain of thought is never requested. Do
			// not ask for a summary in disabled mode: minimal is the lowest
			// supported effort, but it is not equivalent to showing thinking.
			reasoning := map[string]interface{}{"effort": effort}
			if mode == "enabled" {
				reasoning["summary"] = "auto"
			}
			body["reasoning"] = reasoning
		} else {
			body["reasoning_effort"] = effort
		}
		return
	}

	if api == ReasoningAPIChat && usesQwenThinkingControl(cfg) {
		body["enable_thinking"] = mode == "enabled"
		return
	}

	if api == ReasoningAPIAnthropic {
		// Anthropic disables extended thinking by omitting the thinking block;
		// unlike the OpenAI-compatible APIs, it does not accept type=disabled.
		if mode == "enabled" {
			body["thinking"] = map[string]interface{}{
				"type":          "enabled",
				"budget_tokens": anthropicThinkingBudget(cfg.ReasoningEffort),
			}
		}
		return
	}

	// DeepSeek V4, GLM, Kimi, Ark, and most current OpenAI-compatible
	// reasoning gateways use this object. In particular, this preserves an
	// explicit disabled state for DeepSeek instead of later re-enabling it.
	body["thinking"] = map[string]interface{}{"type": mode}
}

func normalizeReasoningMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "enabled", "on", "1", "true":
		return "enabled"
	case "disabled", "off", "0", "false", "none":
		return "disabled"
	default:
		return ""
	}
}

// IsAutoThinkingMode reports whether a configuration leaves the provider's
// default reasoning behavior untouched. Keep this beside the normalizer so all
// request paths agree that whitespace and unknown legacy values mean auto.
func IsAutoThinkingMode(raw string) bool {
	return normalizeReasoningMode(raw) == ""
}

func reasoningEffortForMode(mode, configured string) string {
	if mode == "disabled" {
		// No universal literal “off” exists for OpenAI/xAI reasoning models;
		// minimal is their documented lowest-cost, lowest-reasoning setting.
		return "minimal"
	}
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "minimal", "low", "medium", "high", "xhigh":
		return strings.ToLower(strings.TrimSpace(configured))
	default:
		return "medium"
	}
}

func anthropicThinkingBudget(configured string) int {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "minimal", "low":
		return 1024
	case "high", "xhigh":
		return 8192
	default:
		return 4096
	}
}

func usesOpenAIStyleReasoning(cfg MaclawLLMConfig) bool {
	// Provider names and model IDs are user-editable and gateway URLs often
	// contain upstream vendor names in their path. Only a real endpoint host is
	// safe evidence that the OpenAI/xAI wire-specific fields are accepted.
	endpoint, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(endpoint.Hostname()))
	switch host {
	case "api.openai.com", "chatgpt.com", "api.x.ai", "x.ai":
		return true
	default:
		return false
	}
}

func usesQwenThinkingControl(cfg MaclawLLMConfig) bool {
	return IsQwenOpenAICompat(cfg)
}
