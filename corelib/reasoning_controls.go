package corelib

import (
	"net/url"
	"strings"
	"sync"
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

	absorbConfiguredThinkingLevel(&cfg)
	mode := normalizeReasoningMode(cfg.ThinkingMode)
	if floor := rememberedThinkingFloor(cfg); floor != "" && (mode == "disabled" || thinkingTypeDisabled(body)) {
		// A previous response told us this checkpoint rejects thinking off.
		mode = "enabled"
		if !explicitThinkingEffort(cfg.ReasoningEffort) {
			cfg.ReasoningEffort = floor
		}
	}
	if IsAlwaysOnThinkingModel(cfg) {
		// GLM-5.3 and its flash checkpoints reject thinking.type=disabled,
		// including leftover pass-through bodies. A request that tried to
		// turn thinking off uses the cheapest legal effort. Auto enables the
		// thinking block only on wires that otherwise omit it. Groq,
		// OpenRouter, and Together encode "on" as an effort, and inventing
		// one would replace the provider default.
		fromOff := mode == "disabled" || thinkingTypeDisabled(body)
		if fromOff || (mode == "" && alwaysOnAutoEnablesThinking(cfg, api)) {
			if fromOff && !explicitThinkingEffort(cfg.ReasoningEffort) {
				cfg.ReasoningEffort = "low"
			}
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
			// requested. The internal chain of thought is never requested.
			// omit means the provider's fieldless default already expresses
			// off, so writing minimal here would put back a level it rejects.
			if omit {
				return
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

	// Kimi, MiniMax, Gemini, Groq, OpenRouter, Together, and Doubao each reject
	// at least one of the generic spellings. A Qwen model id on those hosts
	// must not be sent enable_thinking. This also covers the Anthropic wire for
	// Kimi and MiniMax, which do not use Claude budget_tokens.
	if applyNativeVendorReasoning(cfg, body, mode, api) {
		return
	}

	if api == ReasoningAPIChat && usesQwenThinkingControl(cfg) {
		body["enable_thinking"] = mode == "enabled"
		return
	}

	if api == ReasoningAPIAnthropic {
		// Anthropic Claude disables extended thinking by omitting the thinking
		// block; it rejects type=disabled. GLM effort models use reasoning_effort
		// instead of budget_tokens: a budget on a short probe is spent inside
		// the trace before the answer, and GLM-5.3-Flash rejects disabled.
		if GLMSupportsReasoningEffort(cfg) {
			if mode == "disabled" {
				body["thinking"] = map[string]interface{}{"type": "disabled"}
				return
			}
			body["thinking"] = map[string]interface{}{"type": "enabled"}
			stampGLMReasoningEffort(cfg, body)
			return
		}
		if mode == "enabled" {
			body["thinking"] = map[string]interface{}{
				"type":          "enabled",
				"budget_tokens": anthropicThinkingBudget(cfg.ReasoningEffort),
			}
		}
		// Claude rejects thinking.type=disabled. Off is the omitted block.
		return
	}

	// DeepSeek V4, GLM, Ark-hosted GLM, TokenHub, and most current
	// OpenAI-compatible reasoning gateways use this object. In particular,
	// this preserves an explicit disabled state for DeepSeek instead of later
	// re-enabling it. Providers without an effort vocabulary treat low, high,
	// and max as thinking on, and none as thinking off.
	body["thinking"] = map[string]interface{}{"type": mode}
	// DeepSeek keeps thinking.type as the switch and also needs
	// reasoning_effort. Doing it here covers retarget, which would otherwise
	// delete a caller-supplied low/max and leave a later stamp to invent high.
	StampDeepSeekReasoningEffort(cfg, body)
	stampGLMReasoningEffort(cfg, body)
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

// CoerceAlwaysOnThinkingMode upgrades an off request to the cheapest legal
// effort when the selected model rejects thinking.type=disabled.
func CoerceAlwaysOnThinkingMode(cfg MaclawLLMConfig) MaclawLLMConfig {
	absorbConfiguredThinkingLevel(&cfg)
	if floor := rememberedThinkingFloor(cfg); floor != "" && normalizeReasoningMode(cfg.ThinkingMode) == "disabled" {
		cfg.ThinkingMode = "enabled"
		if !explicitThinkingEffort(cfg.ReasoningEffort) {
			cfg.ReasoningEffort = floor
		}
	}
	if IsAlwaysOnThinkingModel(cfg) && normalizeReasoningMode(cfg.ThinkingMode) == "disabled" {
		cfg.ThinkingMode = "enabled"
		if !explicitThinkingEffort(cfg.ReasoningEffort) {
			cfg.ReasoningEffort = "low"
		}
	}
	return cfg
}

// ParseGlobalThinkingMode normalizes the persisted application-level setting
// shared by GUI, MaClawSrv and other hosts. The bool distinguishes a known
// level from an unknown value so reviewed config surfaces can reject typos
// while request builders can safely fall back to auto.
//
// Canonical levels are auto, low, high, max, and none. Legacy enabled/on
// maps to high. Legacy disabled/off maps to none.
func ParseGlobalThinkingMode(raw string) (mode string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		return "auto", true
	case "low", "high", "max":
		return strings.ToLower(strings.TrimSpace(raw)), true
	case "none", "disabled", "disable", "off", "0", "false":
		return "none", true
	case "enabled", "enable", "on", "1", "true":
		return "high", true
	default:
		return "", false
	}
}

// GlobalThinkingModeOptions is the closed set of canonical thinking levels the
// config capability advertises in its invocation schema. Legacy truthy/falsy
// aliases are accepted by ParseGlobalThinkingMode but never advertised.
func GlobalThinkingModeOptions() []string {
	return []string{"auto", "low", "high", "max", "none"}
}

// GlobalThinkingModeHint renders the canonical vocabulary for a rejection
// message, so a model that ignored the enum still learns the valid names from
// its first failure and can retry inside the turn.
func GlobalThinkingModeHint() string {
	return "allowed: " + strings.Join(GlobalThinkingModeOptions(), ", ")
}

// NormalizeGlobalThinkingMode returns the canonical persisted spelling.
// Auto and unknown input persist as unset so the JSON field stays omitted.
func NormalizeGlobalThinkingMode(raw string) string {
	mode, ok := ParseGlobalThinkingMode(raw)
	if !ok || mode == "auto" {
		return ""
	}
	return mode
}

// CanonicalThinkingLevel is the value the settings UI shows: auto, low, high,
// max, or none. Unset and unknown input are auto.
func CanonicalThinkingLevel(raw string) string {
	mode, ok := ParseGlobalThinkingMode(raw)
	if !ok || mode == "" {
		return "auto"
	}
	return mode
}

// EffectiveGlobalThinkingMode is the level applied when a host reads the
// persisted setting. Unset and unknown values are auto: the system leaves
// ordinary providers alone and only rewrites models that reject thinking off.
func EffectiveGlobalThinkingMode(raw string) string {
	return CanonicalThinkingLevel(raw)
}

// ApplyGlobalThinkingLevel copies a persisted level onto a request config.
// The wire switch stays enabled/disabled; the depth is ReasoningEffort.
func ApplyGlobalThinkingLevel(cfg MaclawLLMConfig, raw string) MaclawLLMConfig {
	switch CanonicalThinkingLevel(raw) {
	case "low", "high", "max":
		cfg.ThinkingMode = "enabled"
		cfg.ReasoningEffort = CanonicalThinkingLevel(raw)
	case "none":
		cfg.ThinkingMode = "disabled"
		cfg.ReasoningEffort = ""
	default:
		cfg.ThinkingMode = ""
		cfg.ReasoningEffort = ""
	}
	return CoerceAlwaysOnThinkingMode(cfg)
}

func normalizeReasoningMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "enabled", "enable", "on", "1", "true", "low", "high", "max":
		return "enabled"
	case "disabled", "disable", "off", "0", "false", "none":
		return "disabled"
	default:
		return ""
	}
}

// absorbConfiguredThinkingLevel turns a level stored in ThinkingMode into the
// enabled/disabled switch plus ReasoningEffort. Request builders that copy
// the persisted setting straight onto ThinkingMode still send a legal body.
func absorbConfiguredThinkingLevel(cfg *MaclawLLMConfig) {
	if cfg == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ThinkingMode)) {
	case "low", "high", "max":
		if strings.TrimSpace(cfg.ReasoningEffort) == "" {
			cfg.ReasoningEffort = strings.ToLower(strings.TrimSpace(cfg.ThinkingMode))
		}
		cfg.ThinkingMode = "enabled"
	case "none":
		cfg.ThinkingMode = "disabled"
	}
}

func explicitThinkingEffort(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low", "medium", "high", "xhigh", "max":
		return true
	default:
		return false
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
	case "max", "ultra":
		// OpenAI and xAI have no max literal. xhigh is their deepest effort.
		return "xhigh"
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
	if isXAIReasoningEndpoint(cfg) {
		// grok-4.5 and later cannot turn reasoning off, and they do not
		// accept minimal. The cheapest legal effort is low. max is already
		// xhigh, which older Grok models treat as high.
		switch effort {
		case "minimal", "none":
			return "low", false
		}
	}
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
	case "max", "ultra":
		return 16384
	default:
		return 4096
	}
}

// stampGLMReasoningEffort writes reasoning_effort for GLM-5.2 and newer.
// Empty effort keeps the provider default (max on GLM-5.3). Disabled GLM-5.2
// keeps thinking.type=disabled and does not also send an effort. Always-on
// checkpoints never reach this function with type=disabled.
func stampGLMReasoningEffort(cfg MaclawLLMConfig, body map[string]interface{}) {
	if body == nil || !GLMSupportsReasoningEffort(cfg) {
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
	effort, ok := glmReasoningEffortValue(cfg.ReasoningEffort)
	if !ok {
		delete(body, "reasoning_effort")
		return
	}
	body["reasoning_effort"] = effort
}

func glmReasoningEffortValue(configured string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "low", "minimal":
		return "low", true
	case "high", "medium":
		return "high", true
	case "max", "xhigh", "ultra":
		return "max", true
	default:
		return "", false
	}
}

var thinkingFloors sync.Map

func thinkingFloorKey(cfg MaclawLLMConfig) string {
	return requestHost(cfg) + "|" + glmModelID(cfg)
}

// RememberThinkingFloor records that this checkpoint rejected thinking off
// and named a legal effort. Later requests that still ask for off use it.
func RememberThinkingFloor(cfg MaclawLLMConfig, effort string) {
	effort = strings.ToLower(strings.TrimSpace(effort))
	switch effort {
	case "low", "high", "max":
	default:
		return
	}
	thinkingFloors.Store(thinkingFloorKey(cfg), effort)
}

func rememberedThinkingFloor(cfg MaclawLLMConfig) string {
	value, ok := thinkingFloors.Load(thinkingFloorKey(cfg))
	if !ok {
		return ""
	}
	effort, _ := value.(string)
	return effort
}

// ThinkingOffRejectedEffort reads a provider error that refuses to turn
// thinking off. The returned effort is the cheapest level the message allows,
// which is low when it says to use low, high, or max.
func ThinkingOffRejectedEffort(text string) (string, bool) {
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	lower := strings.ToLower(text)
	rejected := strings.Contains(text, "不支持关闭思考") ||
		strings.Contains(text, "始终思考") ||
		strings.Contains(lower, "does not support disabling thinking") ||
		strings.Contains(lower, "cannot disable thinking") ||
		strings.Contains(lower, "thinking cannot be disabled") ||
		strings.Contains(lower, "does not support turning off thinking")
	if !rejected && strings.Contains(lower, "1210") && hasEffortWord(lower, "low") && hasEffortWord(lower, "max") {
		rejected = true
	}
	if !rejected {
		return "", false
	}
	switch {
	case hasEffortWord(lower, "low"):
		return "low", true
	case hasEffortWord(lower, "high"):
		return "high", true
	case hasEffortWord(lower, "max"):
		return "max", true
	default:
		return "low", true
	}
}

// hasEffortWord reports an effort token, not a piece of a longer token.
// "slower" and "highlight" are not levels. Underscore and hyphen stay inside
// the token, so max_tokens and max-tokens are not the level max. Spaces,
// slashes, and punctuation remain boundaries, so "low/high/max" still matches.
func hasEffortWord(lower, word string) bool {
	for from := 0; from < len(lower); {
		i := strings.Index(lower[from:], word)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(word)
		beforeOK := i == 0 || !isEffortTokenByte(lower[i-1])
		afterOK := end == len(lower) || !isEffortTokenByte(lower[end])
		if beforeOK && afterOK {
			return true
		}
		from = i + 1
	}
	return false
}

func isEffortTokenByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-'
}

func usesOpenAIStyleReasoning(cfg MaclawLLMConfig) bool {
	// Provider names and model IDs are user-editable and gateway URLs often
	// contain upstream vendor names in their path. Only a real endpoint host is
	// safe evidence that the OpenAI/xAI wire-specific fields are accepted.
	switch requestHost(cfg) {
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
	switch requestHost(cfg) {
	case "api.agnes-ai.cn", "agnes-ai.cn":
		return true
	default:
		return false
	}
}

func isXAIReasoningEndpoint(cfg MaclawLLMConfig) bool {
	switch requestHost(cfg) {
	case "api.x.ai", "x.ai":
		return true
	default:
		return false
	}
}

func requestHost(cfg MaclawLLMConfig) string {
	// Same parser as the AMD check. url.Parse treats a scheme-less value as a
	// path, which would hide api.kimi.com and api.x.ai and let the generic
	// thinking object through.
	return llmEndpointHostname(cfg.URL)
}

func hostIs(host string, names ...string) bool {
	for _, name := range names {
		if host == name || strings.HasSuffix(host, "."+name) {
			return true
		}
	}
	return false
}

// nativeVendorReasoningKind names the host whose wire is not the generic
// thinking.type object. Anthropic Claude stays empty so budget_tokens still
// applies; Kimi and MiniMax keep their own object on that wire too. Volcengine
// GLM and DeepSeek stay empty so their effort stamps are not replaced by the
// Doubao vocabulary.
func nativeVendorReasoningKind(cfg MaclawLLMConfig, api ReasoningAPIKind) string {
	host := requestHost(cfg)
	switch {
	case hostIs(host, "kimi.com", "kimi.ai", "moonshot.cn", "moonshot.ai"):
		return "kimi"
	case hostIs(host, "minimaxi.com", "minimax.io", "minimax.chat"):
		return "minimax"
	}
	if api == ReasoningAPIAnthropic {
		return ""
	}
	switch {
	case hostIs(host, "generativelanguage.googleapis.com"):
		if _, ok := GoogleAIStudioOpenAIBase(cfg.URL); ok {
			return "gemini"
		}
		return ""
	case hostIs(host, "groq.com"):
		return "groq"
	case hostIs(host, "openrouter.ai"):
		return "openrouter"
	case hostIs(host, "together.xyz", "together.ai"):
		return "together"
	case hostIs(host, "volces.com", "volcengine.com") && !GLMSupportsReasoningEffort(cfg) && !IsDeepSeekThinkingModeModel(cfg):
		return "doubao"
	default:
		return ""
	}
}

// alwaysOnAutoEnablesThinking reports whether an unset thinking level must
// still send the on-block. Wires that encode "on" as an effort keep the
// provider default when the user left the level on auto.
func alwaysOnAutoEnablesThinking(cfg MaclawLLMConfig, api ReasoningAPIKind) bool {
	if api == ReasoningAPIAnthropic {
		return true
	}
	if usesOpenAIStyleReasoning(cfg) || IsAMDRadeonPublicChatEndpoint(cfg) {
		return false
	}
	if api == ReasoningAPIChat && usesQwenThinkingControl(cfg) {
		return false
	}
	return nativeVendorReasoningKind(cfg, api) == ""
}

func applyNativeVendorReasoning(cfg MaclawLLMConfig, body map[string]interface{}, mode string, api ReasoningAPIKind) bool {
	switch nativeVendorReasoningKind(cfg, api) {
	case "kimi":
		applyKimiReasoning(cfg, body, mode)
	case "minimax":
		applyMiniMaxReasoning(cfg, body, mode)
	case "gemini":
		applyGeminiReasoning(cfg, body, mode)
	case "groq":
		body["reasoning_effort"] = groqReasoningEffort(cfg, mode)
	case "openrouter":
		body["reasoning"] = map[string]interface{}{"effort": openRouterReasoningEffort(cfg, mode)}
	case "together":
		applyTogetherReasoning(cfg, body, mode)
	case "doubao":
		applyDoubaoReasoning(cfg, body, mode)
	default:
		return false
	}
	return true
}

func applyKimiReasoning(cfg MaclawLLMConfig, body map[string]interface{}, mode string) {
	switch kimiReasoningFamily(cfg.Model) {
	case "k3":
		// K3 rejects the K2 thinking object. It always reasons, and the only
		// depth control is reasoning_effort low, high, or max. An empty effort
		// stays omitted so the documented default (max) is not replaced.
		if effort := kimiK3Effort(cfg, mode); effort != "" {
			body["reasoning_effort"] = effort
		}
	case "k27code":
		// k2.7-code rejects every explicit thinking value except enabled
		// with keep=all. Omitting keep is accepted; sending disabled is not.
		body["thinking"] = map[string]interface{}{"type": "enabled", "keep": "all"}
	default:
		typ := "enabled"
		if mode == "disabled" {
			typ = "disabled"
		}
		body["thinking"] = map[string]interface{}{"type": typ}
	}
}

func kimiReasoningFamily(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	id = strings.ReplaceAll(id, "_", "-")
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	if i := strings.IndexByte(id, '['); i > 0 {
		id = strings.TrimSpace(id[:i])
	}
	if strings.Contains(id, "k2.7-code") || strings.Contains(id, "k2-7-code") {
		return "k27code"
	}
	if kimiFamilyToken(id, "kimi-k3") || kimiFamilyToken(id, "k3") {
		return "k3"
	}
	return "k2"
}

func kimiFamilyToken(id, family string) bool {
	for from := 0; from < len(id); {
		i := strings.Index(id[from:], family)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(family)
		if end == len(id) || id[end] == '.' || id[end] == '-' {
			if i == 0 || id[i-1] == '/' || id[i-1] == '-' || id[i-1] == ':' {
				return true
			}
		}
		from = i + 1
	}
	return false
}

func kimiK3Effort(cfg MaclawLLMConfig, mode string) string {
	if mode == "disabled" {
		return "low"
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort)) {
	case "low", "minimal", "none":
		return "low"
	case "high", "medium":
		return "high"
	case "max", "ultra", "xhigh":
		return "max"
	default:
		return ""
	}
}

func applyMiniMaxReasoning(cfg MaclawLLMConfig, body map[string]interface{}, mode string) {
	id := glmModelID(cfg)
	if strings.Contains(id, "minimax-m3") {
		// M3 thinks adaptively unless the request disables it. type=enabled
		// is not one of its accepted values.
		typ := "adaptive"
		if mode == "disabled" {
			typ = "disabled"
		}
		body["thinking"] = map[string]interface{}{"type": typ}
		return
	}
	// M2 always thinks. It has no effort enum, and type=disabled does not
	// turn thinking off. Leave the body without a thinking field.
}

// GoogleAIStudioOpenAIBase is the OpenAI-compatible root for an AI Studio
// API key, https://generativelanguage.googleapis.com/v1beta/openai.
// The native generateContent host without an /openai segment is not this API.
func GoogleAIStudioOpenAIBase(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !hostIs(llmEndpointHostname(raw), "generativelanguage.googleapis.com") {
		return "", false
	}
	normalized := raw
	switch {
	case strings.HasPrefix(normalized, "//"):
		normalized = "https:" + normalized
	case !strings.Contains(normalized, "://"):
		normalized = "https://" + normalized
	}
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Hostname() == "" {
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	openaiAt := -1
	for i, part := range parts {
		if strings.EqualFold(part, "openai") {
			openaiAt = i
			break
		}
	}
	if openaiAt < 0 {
		return "", false
	}
	parsed.Path = "/" + strings.Join(parts[:openaiAt+1], "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), true
}

func applyGeminiReasoning(cfg MaclawLLMConfig, body map[string]interface{}, mode string) {
	// The OpenAI compatibility layer accepts reasoning_effort and rejects the
	// generic thinking object. none is legal only on older Flash checkpoints.
	body["reasoning_effort"] = geminiReasoningEffort(cfg, mode)
}

func geminiReasoningEffort(cfg MaclawLLMConfig, mode string) string {
	if mode == "disabled" {
		if geminiThinkingCanDisable(cfg) {
			return "none"
		}
		return "low"
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort)) {
	case "none", "off", "false", "0":
		if geminiThinkingCanDisable(cfg) {
			return "none"
		}
		return "low"
	case "minimal", "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	case "max", "xhigh", "ultra":
		return "high"
	default:
		return "medium"
	}
}

// geminiThinkingCanDisable reports checkpoints whose OpenAI compatibility
// layer accepts reasoning_effort=none. Gemini 2.5 Pro, Gemini 3, and Gemini 4
// keep thinking on; none becomes the cheapest legal level instead.
func geminiThinkingCanDisable(cfg MaclawLLMConfig) bool {
	id := geminiModelID(cfg)
	if strings.Contains(id, "gemini-2.5-pro") {
		return false
	}
	return strings.HasPrefix(id, "gemini-2.5") ||
		strings.HasPrefix(id, "gemini-2.0") ||
		strings.HasPrefix(id, "gemini-1.5") ||
		strings.HasPrefix(id, "gemini-1.0")
}

func geminiModelID(cfg MaclawLLMConfig) string {
	id := strings.ToLower(strings.TrimSpace(cfg.Model))
	id = strings.ReplaceAll(id, "_", "-")
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	if i := strings.IndexByte(id, '['); i > 0 {
		id = strings.TrimSpace(id[:i])
	}
	return id
}

// ApplyGeminiGenerateContentThinking writes generationConfig.thinkingConfig
// for the native generateContent API. It does not write reasoning_effort or
// thinking.type. Auto asks for the thought trace without naming a level.
// Gemini 2.0 and earlier have no thinkingConfig and are left alone.
func ApplyGeminiGenerateContentThinking(cfg MaclawLLMConfig, generationConfig map[string]interface{}) {
	if generationConfig == nil {
		return
	}
	absorbConfiguredThinkingLevel(&cfg)
	mode := normalizeReasoningMode(cfg.ThinkingMode)
	if floor := rememberedThinkingFloor(cfg); floor != "" && mode == "disabled" {
		mode = "enabled"
		if !explicitThinkingEffort(cfg.ReasoningEffort) {
			cfg.ReasoningEffort = floor
		}
	}
	switch geminiNativeThinkingWire(geminiModelID(cfg)) {
	case "budget":
		applyGeminiThinkingBudget(cfg, generationConfig, mode)
	case "level":
		applyGeminiThinkingLevel(cfg, generationConfig, mode)
	}
}

func geminiNativeThinkingWire(id string) string {
	switch {
	case strings.Contains(id, "gemini-2.5"):
		return "budget"
	case strings.Contains(id, "gemini-3"), strings.Contains(id, "gemini-4"):
		return "level"
	default:
		return ""
	}
}

func applyGeminiThinkingBudget(cfg MaclawLLMConfig, generationConfig map[string]interface{}, mode string) {
	if mode == "" {
		generationConfig["thinkingConfig"] = map[string]interface{}{"includeThoughts": true}
		return
	}
	budget, ok := geminiNativeThinkingBudget(cfg, mode)
	if !ok {
		generationConfig["thinkingConfig"] = map[string]interface{}{"includeThoughts": true}
		return
	}
	thinking := map[string]interface{}{"thinkingBudget": budget}
	if budget != 0 {
		thinking["includeThoughts"] = true
	}
	generationConfig["thinkingConfig"] = thinking
}

func geminiNativeThinkingBudget(cfg MaclawLLMConfig, mode string) (int, bool) {
	effort := strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	if mode == "disabled" || effort == "none" || effort == "off" || effort == "false" || effort == "0" {
		if geminiThinkingCanDisable(cfg) {
			return 0, true
		}
		// Gemini 2.5 Pro rejects a zero budget. 128 is its minimum.
		return 128, true
	}
	switch effort {
	case "minimal":
		return 512, true
	case "low":
		return 1024, true
	case "medium":
		return 8192, true
	case "high", "max", "xhigh", "ultra":
		return 24576, true
	default:
		if mode == "enabled" {
			return 8192, true
		}
		return 0, false
	}
}

func applyGeminiThinkingLevel(cfg MaclawLLMConfig, generationConfig map[string]interface{}, mode string) {
	if mode == "" {
		generationConfig["thinkingConfig"] = map[string]interface{}{"includeThoughts": true}
		return
	}
	level := geminiReasoningEffort(cfg, mode)
	if level == "none" || level == "" {
		level = "low"
	}
	generationConfig["thinkingConfig"] = map[string]interface{}{
		"thinkingLevel":  level,
		"includeThoughts": true,
	}
}

func groqReasoningEffort(cfg MaclawLLMConfig, mode string) string {
	if mode == "disabled" {
		return "none"
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort)) {
	case "none":
		return "none"
	case "low", "medium", "high", "xhigh":
		return strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	case "minimal":
		return "low"
	case "max", "ultra":
		return "xhigh"
	default:
		return "medium"
	}
}

func openRouterReasoningEffort(cfg MaclawLLMConfig, mode string) string {
	if mode == "disabled" {
		return "none"
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort)) {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		return strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	case "ultra":
		return "max"
	default:
		return "medium"
	}
}

func applyTogetherReasoning(cfg MaclawLLMConfig, body map[string]interface{}, mode string) {
	if mode == "disabled" {
		body["reasoning"] = map[string]interface{}{"enabled": false}
		return
	}
	// reasoning.enabled and reasoning_effort are alternative schemas. A named
	// level is the effort field alone. An empty effort is the on-switch alone.
	// Sending both is rejected by some checkpoints.
	switch strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort)) {
	case "low", "medium", "high":
		body["reasoning_effort"] = strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	case "max", "xhigh", "ultra":
		body["reasoning_effort"] = "high"
	default:
		body["reasoning"] = map[string]interface{}{"enabled": true}
	}
}

func applyDoubaoReasoning(cfg MaclawLLMConfig, body map[string]interface{}, mode string) {
	typ := "enabled"
	if mode == "disabled" {
		typ = "disabled"
	}
	body["thinking"] = map[string]interface{}{"type": typ}
	if mode != "enabled" {
		return
	}
	switch strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort)) {
	case "low", "medium", "high", "minimal":
		body["reasoning_effort"] = strings.ToLower(strings.TrimSpace(cfg.ReasoningEffort))
	case "max", "xhigh", "ultra":
		// Current Seed checkpoints map max and xhigh onto high.
		body["reasoning_effort"] = "high"
	}
}

func usesQwenThinkingControl(cfg MaclawLLMConfig) bool {
	return IsQwenOpenAICompat(cfg)
}

// ProbeNeedsReasoningRoom reports a connectivity check or short JSON answer
// whose wire still spends the first tokens on a trace. A 256-token cap then
// comes back with an empty answer. The set is the wires that cannot express
// off: always-on GLM, xAI, Kimi K3 / k2.7-code, and MiniMax checkpoints other
// than M3. M3 accepts thinking.type=disabled.
func ProbeNeedsReasoningRoom(cfg MaclawLLMConfig) bool {
	if IsAlwaysOnThinkingModel(cfg) || isXAIReasoningEndpoint(cfg) {
		return true
	}
	// Native generateContent has no /openai segment, so the OpenAI host
	// check below does not see it. Gemini 3, Gemini 4, and 2.5 Pro still
	// spend the first tokens on a trace when thinking cannot be disabled.
	if cfg.IsGoogleProtocol() && !geminiThinkingCanDisable(cfg) {
		return true
	}
	switch nativeVendorReasoningKind(cfg, ReasoningAPIChat) {
	case "gemini":
		return !geminiThinkingCanDisable(cfg)
	case "kimi":
		switch kimiReasoningFamily(cfg.Model) {
		case "k3", "k27code":
			return true
		default:
			return false
		}
	case "minimax":
		return !strings.Contains(glmModelID(cfg), "minimax-m3")
	default:
		return false
	}
}

// UseCheapLegalThinking selects the cheapest trace a short answer can keep.
// xAI stays disabled: that state still writes effort low and does not request
// a Responses summary. Other families need an explicit enabled block, or an
// auto request keeps the provider's deepest default.
func UseCheapLegalThinking(cfg MaclawLLMConfig) MaclawLLMConfig {
	cfg.ReasoningEffort = "low"
	if isXAIReasoningEndpoint(cfg) {
		cfg.ThinkingMode = "disabled"
		return cfg
	}
	cfg.ThinkingMode = "enabled"
	return cfg
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
	// TokenHub chat accepts thinking.type only. budget_tokens is the Anthropic
	// spelling and is added for tool calls on other OpenAI-compatible hosts.
	if IsTencentTokenHubEndpoint(cfg) {
		if thinking, _ := body["thinking"].(map[string]interface{}); thinking != nil {
			delete(thinking, "budget_tokens")
		}
	}
	StampDeepSeekReasoningEffort(cfg, body)
}

// IsTencentTokenHubEndpoint reports the Token Plan chat hosts. Guangzhou and
// Singapore, on both the mainland and international domains, share this schema.
func IsTencentTokenHubEndpoint(cfg MaclawLLMConfig) bool {
	switch requestHost(cfg) {
	case "tokenhub.tencentmaas.com", "tokenhub-intl.tencentmaas.com",
		"tokenhub.tencentcloudmaas.com", "tokenhub-intl.tencentcloudmaas.com":
		return true
	default:
		return false
	}
}

// OpenAIChatThinkingUsesBudget reports whether a chat-completions thinking
// object is the Claude-style block that may carry budget_tokens. Kimi,
// MiniMax, and Doubao objects are not that block. DeepSeek, GLM, and TokenHub
// reject the field as well.
func OpenAIChatThinkingUsesBudget(cfg MaclawLLMConfig) bool {
	if IsDeepSeekThinkingModeModel(cfg) || GLMSupportsReasoningEffort(cfg) || IsTencentTokenHubEndpoint(cfg) {
		return false
	}
	return nativeVendorReasoningKind(cfg, ReasoningAPIChat) == ""
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
