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
// the native request shape of the selected provider. Auto leaves ordinary
// provider defaults intact, except always-on thinking models (GLM-5.3): those
// reject disabled and only return thinking blocks when the request states
// thinking.type=enabled.
//
// An explicit user choice always wins over a caller-supplied reasoning field.
// This is important for forwarded OpenAI-compatible requests: otherwise a
// DeepSeek default or a stale pass-through field can silently undo “Off”.
func ApplyReasoningControls(cfg MaclawLLMConfig, body map[string]interface{}, api ReasoningAPIKind) {
	if body == nil {
		return
	}

	mode := normalizeReasoningMode(cfg.ThinkingMode)
	if IsAlwaysOnThinkingModel(cfg) {
		// GLM-5.3 rejects thinking.type=disabled, including leftover pass-through
		// bodies in auto mode. Anthropic-compatible gateways also omit
		// thinking_delta unless the thinking block is present, so auto must
		// still request enabled.
		if mode == "disabled" || mode == "" || thinkingTypeDisabled(body) {
			mode = "enabled"
		}
	}
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
		effort, omit := openAIStyleReasoningEffort(cfg, mode)
		if api == ReasoningAPIResponses {
			// Responses streams expose the user-displayable reasoning through
			// response.reasoning_summary_text.delta only when a summary is
			// requested. The internal chain of thought is never requested. Do
			// not ask for a summary in disabled mode: minimal is the lowest
			// supported effort, but it is not equivalent to showing thinking.
			if omit {
				effort = "minimal"
			}
			reasoning := map[string]interface{}{"effort": effort}
			if mode == "enabled" {
				reasoning["summary"] = "auto"
			}
			body["reasoning"] = reasoning
		} else if !omit {
			body["reasoning_effort"] = effort
		}
		return
	}

	if api == ReasoningAPIChat && IsAMDRadeonPublicChatEndpoint(cfg) {
		// This host's chat schema wins over model-name rules. A Qwen model id
		// on AMD must not be sent enable_thinking: the gateway drops that
		// field and rejects thinking.
		body["reasoning_effort"] = amdRadeonChatEffort(cfg, mode)
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
	// DeepSeek keeps thinking.type as the switch and also needs
	// reasoning_effort. Doing it here covers retarget, which would otherwise
	// delete a caller-supplied low/max and leave a later stamp to invent high.
	StampDeepSeekReasoningEffort(cfg, body)
}

// DeepSeekReasoningEffort maps a configured effort onto the values DeepSeek
// V4 accepts while thinking is enabled: low, high, or max. Empty and the
// OpenAI medium/xhigh aliases become high, which is DeepSeek's default.
// WorkBuddy ignores thinking.type by itself and returns an empty
// reasoning_content unless this field is present.
func DeepSeekReasoningEffort(configured string) string {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "minimal", "low", "none", "off", "false", "0":
		return "low"
	case "max", "ultra":
		return "max"
	default:
		return "high"
	}
}

// StampDeepSeekReasoningEffort finishes a DeepSeek chat body so gateways that
// only honor reasoning_effort still return reasoning_content. thinking.type
// stays the on/off switch. budget_tokens is Anthropic-only and is removed.
// A disabled thinking block must not also carry reasoning_effort.
func StampDeepSeekReasoningEffort(cfg MaclawLLMConfig, body map[string]interface{}) {
	if body == nil || !IsDeepSeekThinkingModeModel(cfg) {
		return
	}
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking == nil {
		return
	}
	delete(thinking, "budget_tokens")
	typ, _ := thinking["type"].(string)
	if !strings.EqualFold(strings.TrimSpace(typ), "enabled") {
		delete(body, "reasoning_effort")
		return
	}
	configured := cfg.ReasoningEffort
	if strings.TrimSpace(configured) == "" {
		if existing, _ := body["reasoning_effort"].(string); strings.TrimSpace(existing) != "" {
			configured = existing
		}
	}
	body["reasoning_effort"] = DeepSeekReasoningEffort(configured)
}

func thinkingTypeDisabled(body map[string]interface{}) bool {
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking == nil {
		return false
	}
	typ, _ := thinking["type"].(string)
	return strings.EqualFold(strings.TrimSpace(typ), "disabled")
}

// CoerceAlwaysOnThinkingMode upgrades ThinkingMode=disabled to enabled when
// the selected model rejects thinking.type=disabled.
func CoerceAlwaysOnThinkingMode(cfg MaclawLLMConfig) MaclawLLMConfig {
	if IsAlwaysOnThinkingModel(cfg) && normalizeReasoningMode(cfg.ThinkingMode) == "disabled" {
		cfg.ThinkingMode = "enabled"
	}
	return cfg
}

// ParseGlobalThinkingMode normalizes the persisted application-level setting
// shared by GUI, MaClawSrv and other hosts. The bool distinguishes the
// explicit auto/unset value from an unknown value so reviewed config surfaces
// can reject typos while request builders can safely fall back to auto.
func ParseGlobalThinkingMode(raw string) (mode string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		return "", true
	case "enabled", "enable", "on", "1", "true":
		return "enabled", true
	case "disabled", "disable", "off", "0", "false", "none":
		return "disabled", true
	default:
		return "", false
	}
}

// NormalizeGlobalThinkingMode returns the canonical persisted spelling, or
// the legacy auto/unset value for unknown input.
func NormalizeGlobalThinkingMode(raw string) string {
	mode, _ := ParseGlobalThinkingMode(raw)
	return mode
}

// EffectiveGlobalThinkingMode resolves auto/unset and invalid legacy values to
// the product default used by the desktop UI when materializing a request.
func EffectiveGlobalThinkingMode(raw string) string {
	mode, ok := ParseGlobalThinkingMode(raw)
	if ok && mode != "" {
		return mode
	}
	return "enabled"
}

func normalizeReasoningMode(raw string) string {
	return NormalizeGlobalThinkingMode(raw)
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

// openAIStyleReasoningEffort resolves the wire effort for an OpenAI-style
// reasoning endpoint, adjusting for providers whose accepted vocabulary is
// narrower than OpenAI's. omit reports that no control should be sent at all:
// Agnes rejects reasoning_effort=minimal with HTTP 400, and its default
// (no field) is already the no-reasoning behavior, so “off” maps to omission.
func openAIStyleReasoningEffort(cfg MaclawLLMConfig, mode string) (effort string, omit bool) {
	effort = reasoningEffortForMode(mode, cfg.ReasoningEffort)
	if isAgnesReasoningEndpoint(cfg) {
		switch effort {
		case "minimal":
			// Agnes rejects reasoning_effort=minimal with HTTP 400. “Off” maps
			// to omission (the fieldless default already skips reasoning); an
			// explicit minimal effort clamps up to the accepted floor.
			if mode == "disabled" {
				return "", true
			}
			return "low", false
		case "xhigh":
			return "high", false
		}
	}
	return effort, false
}

// RetargetReasoningControlsForUpstream rewrites caller-supplied reasoning
// controls into the upstream provider's native spelling. Forwarding proxies
// (for example the hub LLM proxy) receive any of the client-side spellings;
// upstreams accept only their own. Agnes only honors reasoning_effort, so a
// DeepSeek-style thinking object would otherwise be silently ignored. Bodies
// without any reasoning control keep the provider default (auto) untouched.
func RetargetReasoningControlsForUpstream(cfg MaclawLLMConfig, body map[string]interface{}, api ReasoningAPIKind) {
	if body == nil {
		return
	}
	mode, effort := reasoningControlsRequestedInBody(body)
	if mode == "" {
		return
	}
	next := cfg
	next.ThinkingMode = mode
	if effort != "" {
		next.ReasoningEffort = effort
	}
	ApplyReasoningControls(next, body, api)
}

// reasoningControlsRequestedInBody detects an explicit reasoning request in
// any of the client-side spellings. It returns the normalized mode and, when
// the caller stated an effort level, that level.
func reasoningControlsRequestedInBody(body map[string]interface{}) (mode, effort string) {
	normalizeMode := func(raw string) string {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "enabled", "on", "1", "true":
			return "enabled"
		case "disabled", "off", "0", "false", "none":
			return "disabled"
		default:
			return ""
		}
	}
	if thinking, _ := body["thinking"].(map[string]interface{}); thinking != nil {
		if typ, _ := thinking["type"].(string); typ != "" {
			mode = normalizeMode(typ)
		}
	}
	if v, ok := body["enable_thinking"].(bool); ok {
		if v {
			mode = "enabled"
		} else {
			mode = "disabled"
		}
	}
	if s, _ := body["reasoning_effort"].(string); strings.TrimSpace(s) != "" {
		if m := normalizeMode(s); m != "" {
			mode = m
		} else {
			mode = "enabled"
			effort = strings.TrimSpace(s)
		}
	}
	if reasoning, _ := body["reasoning"].(map[string]interface{}); reasoning != nil {
		if s, _ := reasoning["effort"].(string); strings.TrimSpace(s) != "" {
			if m := normalizeMode(s); m != "" {
				if mode == "" {
					mode = m
				}
			} else {
				if mode == "" {
					mode = "enabled"
				}
				if effort == "" {
					effort = strings.TrimSpace(s)
				}
			}
		}
	}
	return mode, effort
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
	case "api.agnes-ai.cn", "agnes-ai.cn":
		// Agnes accepts reasoning_effort (low/medium/high) but silently ignores
		// the DeepSeek-style thinking object and Qwen-style enable_thinking.
		return true
	default:
		return false
	}
}

func isAgnesReasoningEndpoint(cfg MaclawLLMConfig) bool {
	endpoint, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(endpoint.Hostname())) {
	case "api.agnes-ai.cn", "agnes-ai.cn":
		return true
	default:
		return false
	}
}

func usesQwenThinkingControl(cfg MaclawLLMConfig) bool {
	return IsQwenOpenAICompat(cfg)
}

// IsAMDRadeonPublicChatEndpoint reports AMD's public free-model gateway
// (https://developer.amd.com.cn/radeon/api/v1). That gateway returns HTTP 400
// when a chat completion carries `thinking`, and only honors reasoning_effort
// (or reasoning.effort). Dedicated instance hosts are a different gateway and
// are not matched here.
func IsAMDRadeonPublicChatEndpoint(cfg MaclawLLMConfig) bool {
	switch llmEndpointHostname(cfg.URL) {
	case "developer.amd.com.cn", "developer.amd.com":
		return true
	default:
		return false
	}
}

// llmEndpointHostname returns the lowercase host, accepting a URL that was
// saved without a scheme. url.Parse treats a scheme-less value as a path, which
// would hide developer.amd.com.cn and let a thinking object through.
func llmEndpointHostname(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(raw, "//"):
		raw = "https:" + raw
	case !strings.Contains(raw, "://"):
		raw = "https://" + raw
	}
	endpoint, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(endpoint.Hostname()))
}

func amdDeepSeekFamily(cfg MaclawLLMConfig) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(cfg.Model)), "deepseek")
}

// amdRadeonChatEffort maps a thinking mode onto a tier this gateway accepts.
// DeepSeek models on AMD accept the OpenAI set plus none/max. Other models on
// the same host only share low and medium, so those are the portable values.
func amdRadeonChatEffort(cfg MaclawLLMConfig, mode string) string {
	if mode == "disabled" {
		if amdDeepSeekFamily(cfg) {
			return "none"
		}
		return "low"
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort)) {
	case "low", "medium":
		return strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	case "minimal":
		if amdDeepSeekFamily(cfg) {
			return "minimal"
		}
		return "low"
	case "high":
		if amdDeepSeekFamily(cfg) {
			return "high"
		}
		return "medium"
	case "xhigh":
		if amdDeepSeekFamily(cfg) {
			return "xhigh"
		}
		return "medium"
	case "max", "ultra":
		if amdDeepSeekFamily(cfg) {
			return "max"
		}
		return "medium"
	case "none", "off", "false", "0":
		// Enabled mode only. Disabled already returned "none" or "low" above.
		// DeepSeek's own stamp maps these aliases to low while thinking stays on.
		return "low"
	default:
		return "medium"
	}
}

// ShouldAutoStampDeepSeekThinking reports whether a chat body should gain a
// DeepSeek thinking object when the caller left thinking on auto. AMD's public
// gateway rejects that object for every DeepSeek model, so the stamp stays off
// that host. An explicit thinking request is rewritten separately.
func ShouldAutoStampDeepSeekThinking(cfg MaclawLLMConfig) bool {
	return IsAutoThinkingMode(cfg.ThinkingMode) && IsDeepSeekThinkingModeModel(cfg) && !IsAMDRadeonPublicChatEndpoint(cfg)
}

// AddAutoDeepSeekThinkingObject stamps thinking.type=enabled for hosts that
// still require the DeepSeek object. AMD is excluded by
// ShouldAutoStampDeepSeekThinking. A body that already has thinking is kept.
func AddAutoDeepSeekThinkingObject(cfg MaclawLLMConfig, body map[string]interface{}) {
	if body == nil || !ShouldAutoStampDeepSeekThinking(cfg) {
		return
	}
	if _, hasThinking := body["thinking"]; hasThinking {
		return
	}
	body["thinking"] = map[string]interface{}{"type": "enabled"}
}

// FinishOpenAIChatReasoningControls is the last reasoning edit on an OpenAI
// chat body. Official DeepSeek and WorkBuddy still get reasoning_effort beside
// thinking.type. AMD never gets that stamp: it would leave the thinking object
// in place, and AMD rejects the object on /v1/chat/completions.
func FinishOpenAIChatReasoningControls(cfg MaclawLLMConfig, body map[string]interface{}) {
	if body == nil {
		return
	}
	if IsAMDRadeonPublicChatEndpoint(cfg) {
		RewriteAMDRadeonChatReasoning(cfg, body)
		return
	}
	StampDeepSeekReasoningEffort(cfg, body)
}

// RewriteAMDRadeonChatReasoning removes thinking from an AMD public chat body.
// An explicit thinking request is kept as reasoning_effort. A body that stated
// no reasoning control does not gain one. Non-AMD hosts are ignored.
func RewriteAMDRadeonChatReasoning(cfg MaclawLLMConfig, body map[string]interface{}) {
	if body == nil || !IsAMDRadeonPublicChatEndpoint(cfg) {
		return
	}
	mode, effort := amdReasoningRequestedInBody(body)
	// thinking, enable_thinking, and reasoning are removed even when the
	// shape was not recognized. AMD returns HTTP 400 for thinking and for
	// reasoning.enabled; a plain body must not keep either key.
	delete(body, "thinking")
	delete(body, "enable_thinking")
	delete(body, "reasoning")
	if !amdReasoningEffortValueOK(body["reasoning_effort"]) {
		delete(body, "reasoning_effort")
	}
	if mode == "" {
		return
	}
	next := cfg
	next.ThinkingMode = mode
	if strings.TrimSpace(effort) != "" {
		next.ReasoningEffort = effort
	}
	body["reasoning_effort"] = amdRadeonChatEffort(next, mode)
}

// amdReasoningRequestedInBody reads the shared client spellings, then the
// bool and string forms of thinking that the shared parser ignores. Those
// forms still make AMD return HTTP 400.
func amdReasoningRequestedInBody(body map[string]interface{}) (mode, effort string) {
	mode, effort = reasoningControlsRequestedInBody(body)
	if mode != "" || body == nil {
		return mode, effort
	}
	// reasoning.enabled is rejected with HTTP 400 on this gateway. The shared
	// parser only reads reasoning.effort, so an enabled flag would otherwise
	// be deleted and the request would silently stop thinking.
	if reasoning, _ := body["reasoning"].(map[string]interface{}); reasoning != nil {
		if enabled, ok := reasoning["enabled"].(bool); ok {
			if enabled {
				return "enabled", ""
			}
			return "disabled", ""
		}
	}
	switch v := body["thinking"].(type) {
	case bool:
		if v {
			return "enabled", ""
		}
		return "disabled", ""
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "enabled", "on", "true", "1":
			return "enabled", ""
		case "disabled", "off", "false", "0", "none":
			return "disabled", ""
		}
	}
	return "", ""
}

func amdReasoningEffortValueOK(value interface{}) bool {
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) != ""
}
