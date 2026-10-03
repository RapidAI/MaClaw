package corelib

import "testing"

func TestApplyReasoningControlsUsesProviderNativeShape(t *testing.T) {
	tests := []struct {
		name    string
		cfg     MaclawLLMConfig
		api     ReasoningAPIKind
		wantKey string
		want    interface{}
	}{
		{
			name:    "DeepSeek uses thinking object",
			cfg:     MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-reasoner", ThinkingMode: "disabled"},
			api:     ReasoningAPIChat,
			wantKey: "thinking",
			want:    "disabled",
		},
		{
			name:    "Grok uses reasoning effort",
			cfg:     MaclawLLMConfig{URL: "https://api.x.ai/v1", Model: "grok-4.5", ThinkingMode: "enabled"},
			api:     ReasoningAPIChat,
			wantKey: "reasoning_effort",
			want:    "medium",
		},
		{
			name:    "OpenAI Responses uses reasoning object",
			cfg:     MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-5", ThinkingMode: "enabled", ReasoningEffort: "high"},
			api:     ReasoningAPIResponses,
			wantKey: "reasoning",
			want:    "high",
		},
		{
			name:    "Qwen uses enable thinking",
			cfg:     MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3", ThinkingMode: "disabled"},
			api:     ReasoningAPIChat,
			wantKey: "enable_thinking",
			want:    false,
		},
		{
			name:    "Anthropic uses budgeted thinking",
			cfg:     MaclawLLMConfig{URL: "https://api.anthropic.com", Model: "claude-sonnet", ThinkingMode: "enabled", ReasoningEffort: "high"},
			api:     ReasoningAPIAnthropic,
			wantKey: "thinking",
			want:    8192,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]interface{}{
				"thinking":         map[string]interface{}{"type": "enabled"},
				"reasoning":        map[string]interface{}{"effort": "high"},
				"reasoning_effort": "high",
			}
			ApplyReasoningControls(tt.cfg, body, tt.api)
			got, ok := body[tt.wantKey]
			if !ok {
				t.Fatalf("missing %q in %#v", tt.wantKey, body)
			}
			switch want := tt.want.(type) {
			case string:
				if tt.wantKey == "thinking" {
					if actual := got.(map[string]interface{})["type"]; actual != want {
						t.Fatalf("thinking.type = %#v, want %q", actual, want)
					}
				} else if tt.wantKey == "reasoning" {
					if actual := got.(map[string]interface{})["effort"]; actual != want {
						t.Fatalf("reasoning.effort = %#v, want %q", actual, want)
					}
					if actual := got.(map[string]interface{})["summary"]; actual != "auto" {
						t.Fatalf("reasoning.summary = %#v, want auto", actual)
					}
				} else if got != want {
					t.Fatalf("%s = %#v, want %q", tt.wantKey, got, want)
				}
			case int:
				if actual := got.(map[string]interface{})["budget_tokens"]; actual != want {
					t.Fatalf("thinking.budget_tokens = %#v, want %d", actual, want)
				}
			default:
				if got != want {
					t.Fatalf("%s = %#v, want %#v", tt.wantKey, got, want)
				}
			}
			for _, key := range []string{"thinking", "reasoning", "reasoning_effort", "enable_thinking"} {
				if key != tt.wantKey {
					if _, present := body[key]; present {
						t.Fatalf("unexpected incompatible control %q in %#v", key, body)
					}
				}
			}
		})
	}
}

func TestStampDeepSeekReasoningEffortRequiresEffortAndDropsBudget(t *testing.T) {
	body := map[string]interface{}{
		"thinking": map[string]interface{}{"type": "enabled", "budget_tokens": 4096},
	}
	StampDeepSeekReasoningEffort(MaclawLLMConfig{Model: "deepseek-v4.1-flash", ThinkingMode: "enabled"}, body)
	thinking := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking = %#v", thinking)
	}
	if _, ok := thinking["budget_tokens"]; ok {
		t.Fatalf("budget_tokens remained: %#v", thinking)
	}
	if body["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", body["reasoning_effort"])
	}
	offEffort := map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}
	StampDeepSeekReasoningEffort(MaclawLLMConfig{Model: "deepseek-v4.1-flash", ThinkingMode: "enabled", ReasoningEffort: "none"}, offEffort)
	if offEffort["reasoning_effort"] != "low" {
		t.Fatalf("none effort = %#v, want low", offEffort["reasoning_effort"])
	}

	off := map[string]interface{}{
		"thinking":         map[string]interface{}{"type": "disabled", "budget_tokens": 1024},
		"reasoning_effort": "high",
	}
	StampDeepSeekReasoningEffort(MaclawLLMConfig{Model: "deepseek-v4.1-flash", ThinkingMode: "disabled"}, off)
	if _, ok := off["reasoning_effort"]; ok {
		t.Fatalf("disabled request kept reasoning_effort: %#v", off)
	}
}

func TestApplyReasoningControlsAutoPreservesCallerBody(t *testing.T) {
	body := map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}
	ApplyReasoningControls(MaclawLLMConfig{Model: "deepseek-reasoner"}, body, ReasoningAPIChat)
	if got := body["thinking"].(map[string]interface{})["type"]; got != "enabled" {
		t.Fatalf("auto changed caller body to %#v", body)
	}
}

func TestApplyReasoningControlsAnthropicDoesNotUseOpenAIShapeForCompatibleModelName(t *testing.T) {
	body := map[string]interface{}{}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-5", ThinkingMode: "enabled"},
		body,
		ReasoningAPIAnthropic,
	)
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 4096 {
		t.Fatalf("Anthropic request shape = %#v, want native thinking block", body)
	}
	if _, exists := body["reasoning_effort"]; exists {
		t.Fatalf("Anthropic request must not receive reasoning_effort: %#v", body)
	}
}

func TestIsAutoThinkingModeNormalizesWhitespaceAndUnknownValues(t *testing.T) {
	for _, value := range []string{"", "  ", "auto", "unknown"} {
		if !IsAutoThinkingMode(value) {
			t.Fatalf("IsAutoThinkingMode(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"enabled", "disabled", " on ", " off "} {
		if IsAutoThinkingMode(value) {
			t.Fatalf("IsAutoThinkingMode(%q) = true, want false", value)
		}
	}
}

func TestParseGlobalThinkingModeSharesHostAliases(t *testing.T) {
	tests := []struct {
		raw  string
		mode string
		ok   bool
	}{
		{raw: "", mode: "", ok: true},
		{raw: " auto ", mode: "", ok: true},
		{raw: "ENABLE", mode: "enabled", ok: true},
		{raw: "true", mode: "enabled", ok: true},
		{raw: "DISABLE", mode: "disabled", ok: true},
		{raw: "none", mode: "disabled", ok: true},
		{raw: "unexpected", mode: "", ok: false},
	}
	for _, tt := range tests {
		mode, ok := ParseGlobalThinkingMode(tt.raw)
		if mode != tt.mode || ok != tt.ok {
			t.Errorf("ParseGlobalThinkingMode(%q) = (%q, %v), want (%q, %v)", tt.raw, mode, ok, tt.mode, tt.ok)
		}
	}
	if got := EffectiveGlobalThinkingMode("auto"); got != "enabled" {
		t.Fatalf("auto effective mode = %q, want enabled", got)
	}
	if got := EffectiveGlobalThinkingMode("invalid"); got != "enabled" {
		t.Fatalf("invalid effective mode = %q, want enabled", got)
	}
}

func TestApplyReasoningControlsResponsesUsesThinkingForQwen(t *testing.T) {
	body := map[string]interface{}{}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3", ThinkingMode: "enabled"},
		body,
		ReasoningAPIResponses,
	)
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("Qwen Responses control = %#v, want thinking.type=enabled", body)
	}
	if _, exists := body["enable_thinking"]; exists {
		t.Fatalf("Qwen Responses must not receive chat-only enable_thinking: %#v", body)
	}
}

func TestApplyReasoningControlsDoesNotClassifyArbitraryGrokModelAsXAI(t *testing.T) {
	body := map[string]interface{}{}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://gateway.example/v1", ProviderName: "Custom", Model: "grok-compatible", ThinkingMode: "enabled"},
		body,
		ReasoningAPIChat,
	)
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("custom gateway control = %#v, want generic thinking object", body)
	}
	if _, exists := body["reasoning_effort"]; exists {
		t.Fatalf("custom gateway must not be classified as xAI: %#v", body)
	}
}

func TestApplyReasoningControlsDoesNotClassifyGatewayPathAsOfficialEndpoint(t *testing.T) {
	body := map[string]interface{}{}
	ApplyReasoningControls(
		MaclawLLMConfig{
			URL:          "https://gateway.example/openai/api.x.ai/v1",
			ProviderName: "OpenAI-compatible xAI gateway",
			Model:        "gpt-5",
			ThinkingMode: "enabled",
		},
		body,
		ReasoningAPIChat,
	)
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("gateway control = %#v, want generic thinking object", body)
	}
	if _, exists := body["reasoning_effort"]; exists {
		t.Fatalf("gateway path must not be classified as official OpenAI/xAI endpoint: %#v", body)
	}
}

func TestApplyReasoningControlsResponsesDoesNotRequestSummaryWhenDisabled(t *testing.T) {
	body := map[string]interface{}{}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://api.x.ai/v1", Model: "grok-4.5", ThinkingMode: "disabled"},
		body,
		ReasoningAPIResponses,
	)
	reasoning, _ := body["reasoning"].(map[string]interface{})
	if reasoning["effort"] != "minimal" {
		t.Fatalf("reasoning.effort = %#v, want minimal", reasoning["effort"])
	}
	if _, exists := reasoning["summary"]; exists {
		t.Fatalf("disabled Responses request must not ask for a summary: %#v", body)
	}
}

func TestApplyReasoningControlsGLM53DoesNotDisableThinking(t *testing.T) {
	for _, api := range []ReasoningAPIKind{ReasoningAPIChat, ReasoningAPIAnthropic} {
		body := map[string]interface{}{"thinking": map[string]interface{}{"type": "disabled"}}
		ApplyReasoningControls(
			MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3", ThinkingMode: "disabled"},
			body,
			api,
		)
		thinking, _ := body["thinking"].(map[string]interface{})
		if thinking["type"] != "enabled" {
			t.Fatalf("api=%s glm-5.3 thinking = %#v, want type=enabled", api, body["thinking"])
		}
	}

	body := map[string]interface{}{"thinking": map[string]interface{}{"type": "disabled"}}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3[1m]", ThinkingMode: "disabled"},
		body,
		ReasoningAPIChat,
	)
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("glm-5.3[1m] thinking = %#v, want type=enabled", body["thinking"])
	}

	body = map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "GLM-5.2", ThinkingMode: "disabled"},
		body,
		ReasoningAPIChat,
	)
	thinking, _ = body["thinking"].(map[string]interface{})
	if thinking["type"] != "disabled" {
		t.Fatalf("glm-5.2 thinking = %#v, want type=disabled", body["thinking"])
	}

	body = map[string]interface{}{"thinking": map[string]interface{}{"type": "disabled"}}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3", ThinkingMode: ""},
		body,
		ReasoningAPIChat,
	)
	thinking, _ = body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("auto glm-5.3 leftover disabled thinking = %#v, want type=enabled", body["thinking"])
	}
}

func TestApplyReasoningControlsAutoEnablesAlwaysOnThinking(t *testing.T) {
	body := map[string]interface{}{}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3"},
		body,
		ReasoningAPIAnthropic,
	)
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" || thinking["budget_tokens"] == nil {
		t.Fatalf("auto glm-5.3 empty Anthropic body = %#v, want thinking.enabled with budget", body)
	}
}

func TestApplyReasoningControlsAgnesUsesReasoningEffort(t *testing.T) {
	body := map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://api.agnes-ai.cn/v1", Model: "agnes-2.5-flash", ThinkingMode: "enabled"},
		body,
		ReasoningAPIChat,
	)
	if got := body["reasoning_effort"]; got != "medium" {
		t.Fatalf("agnes reasoning_effort = %#v, want medium", got)
	}
	if _, exists := body["thinking"]; exists {
		t.Fatalf("agnes request must not keep the thinking object: %#v", body)
	}
}

func TestApplyReasoningControlsAgnesDisabledOmitsControl(t *testing.T) {
	// Agnes rejects reasoning_effort=minimal with HTTP 400, and its fieldless
	// default already skips reasoning, so disabled must omit the control.
	body := map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://api.agnes-ai.cn/v1", Model: "agnes-2.5-flash", ThinkingMode: "disabled"},
		body,
		ReasoningAPIChat,
	)
	for _, key := range []string{"thinking", "reasoning", "reasoning_effort", "enable_thinking"} {
		if _, exists := body[key]; exists {
			t.Fatalf("agnes disabled request must omit %q: %#v", key, body)
		}
	}
}

func TestApplyReasoningControlsAgnesClampsUnsupportedEfforts(t *testing.T) {
	body := map[string]interface{}{}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://api.agnes-ai.cn/v1", Model: "agnes-2.5-flash", ThinkingMode: "enabled", ReasoningEffort: "minimal"},
		body,
		ReasoningAPIChat,
	)
	if got := body["reasoning_effort"]; got != "low" {
		t.Fatalf("agnes minimal clamp = %#v, want low", got)
	}

	body = map[string]interface{}{}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://api.agnes-ai.cn/v1", Model: "agnes-2.5-flash", ThinkingMode: "enabled", ReasoningEffort: "xhigh"},
		body,
		ReasoningAPIChat,
	)
	if got := body["reasoning_effort"]; got != "high" {
		t.Fatalf("agnes xhigh clamp = %#v, want high", got)
	}
}

func TestRetargetReasoningControlsForUpstream(t *testing.T) {
	// A DeepSeek-style thinking object forwarded to Agnes must become
	// reasoning_effort; otherwise Agnes silently skips reasoning.
	body := map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}
	RetargetReasoningControlsForUpstream(
		MaclawLLMConfig{URL: "https://api.agnes-ai.cn/v1", Model: "agnes-2.5-flash"},
		body,
		ReasoningAPIChat,
	)
	if got := body["reasoning_effort"]; got != "medium" {
		t.Fatalf("retargeted agnes reasoning_effort = %#v, want medium", got)
	}
	if _, exists := body["thinking"]; exists {
		t.Fatalf("retargeted body must not keep the thinking object: %#v", body)
	}

	// An OpenAI-style effort forwarded to DeepSeek becomes thinking.type and
	// keeps a DeepSeek effort. WorkBuddy returns an empty reasoning_content
	// when the effort field is missing. low must not be upgraded to high.
	body = map[string]interface{}{"reasoning_effort": "low"}
	RetargetReasoningControlsForUpstream(
		MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-reasoner"},
		body,
		ReasoningAPIChat,
	)
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("retargeted deepseek thinking = %#v, want type=enabled", body["thinking"])
	}
	if got := body["reasoning_effort"]; got != "low" {
		t.Fatalf("retargeted deepseek reasoning_effort = %#v, want low", got)
	}

	// enable_thinking=false is an explicit off and must survive the retarget.
	body = map[string]interface{}{"enable_thinking": false}
	RetargetReasoningControlsForUpstream(
		MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-reasoner"},
		body,
		ReasoningAPIChat,
	)
	thinking, _ = body["thinking"].(map[string]interface{})
	if thinking["type"] != "disabled" {
		t.Fatalf("retargeted disabled thinking = %#v, want type=disabled", body["thinking"])
	}
	if _, exists := body["reasoning_effort"]; exists {
		t.Fatalf("disabled retarget kept reasoning_effort: %#v", body)
	}
}

func TestRetargetReasoningControlsForUpstreamLeavesAutoUntouched(t *testing.T) {
	body := map[string]interface{}{"model": "auto", "stream": true}
	RetargetReasoningControlsForUpstream(
		MaclawLLMConfig{URL: "https://api.agnes-ai.cn/v1", Model: "agnes-2.5-flash"},
		body,
		ReasoningAPIChat,
	)
	if len(body) != 2 {
		t.Fatalf("auto body changed: %#v", body)
	}
}

func TestAMDRadeonChatReasoningOmitsThinkingForDeepSeekFamily(t *testing.T) {
	amd := "https://developer.amd.com.cn/radeon/api/v1"
	for _, model := range []string{"DeepSeek-V4-Flash", "DeepSeek-V4.1-Flash", "DeepSeek-V4-Pro", "deepseek-reasoner"} {
		t.Run(model, func(t *testing.T) {
			body := map[string]interface{}{
				"thinking": map[string]interface{}{"type": "enabled", "budget_tokens": 4096},
			}
			RewriteAMDRadeonChatReasoning(MaclawLLMConfig{URL: amd, Model: model}, body)
			if _, exists := body["thinking"]; exists {
				t.Fatalf("AMD body kept thinking: %#v", body)
			}
			if got := body["reasoning_effort"]; got != "medium" {
				t.Fatalf("reasoning_effort = %#v, want medium", got)
			}
		})
	}

	// A plain availability probe states no reasoning control. AMD's DeepSeek
	// default is already a normal completion, so the body must stay plain.
	plain := map[string]interface{}{"model": "DeepSeek-V4-Flash"}
	RewriteAMDRadeonChatReasoning(MaclawLLMConfig{URL: amd, Model: "DeepSeek-V4-Flash"}, plain)
	if _, exists := plain["thinking"]; exists {
		t.Fatalf("plain AMD body gained thinking: %#v", plain)
	}
	if _, exists := plain["reasoning_effort"]; exists {
		t.Fatalf("plain AMD body gained reasoning_effort: %#v", plain)
	}

	kept := map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}
	RewriteAMDRadeonChatReasoning(MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "DeepSeek-V4-Flash"}, kept)
	if _, exists := kept["thinking"]; !exists {
		t.Fatalf("official DeepSeek lost thinking: %#v", kept)
	}

	forwarded := map[string]interface{}{
		"model":    "DeepSeek-V4.1-Flash",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		"thinking": map[string]interface{}{"type": "enabled"},
	}
	sanitizeOpenAICompatForwardBody(MaclawLLMConfig{URL: amd, Model: "DeepSeek-V4.1-Flash"}, forwarded)
	if _, exists := forwarded["thinking"]; exists {
		t.Fatalf("forwarded AMD body kept thinking: %#v", forwarded)
	}
	if got := forwarded["reasoning_effort"]; got != "medium" {
		t.Fatalf("forwarded reasoning_effort = %#v, want medium", got)
	}

	schemeLess := map[string]interface{}{"thinking": true}
	RewriteAMDRadeonChatReasoning(MaclawLLMConfig{URL: "developer.amd.com.cn/radeon/api/v1", Model: "DeepSeek-V4-Flash"}, schemeLess)
	if _, exists := schemeLess["thinking"]; exists {
		t.Fatalf("scheme-less AMD URL kept thinking: %#v", schemeLess)
	}
	if got := schemeLess["reasoning_effort"]; got != "medium" {
		t.Fatalf("scheme-less reasoning_effort = %#v, want medium", got)
	}

	asString := map[string]interface{}{"thinking": "enabled"}
	RewriteAMDRadeonChatReasoning(MaclawLLMConfig{URL: amd, Model: "DeepSeek-V4-Pro"}, asString)
	if _, exists := asString["thinking"]; exists {
		t.Fatalf("string thinking kept: %#v", asString)
	}
	if got := asString["reasoning_effort"]; got != "medium" {
		t.Fatalf("string thinking effort = %#v, want medium", got)
	}
}

func TestApplyReasoningControlsAMDBeatsQwenModelName(t *testing.T) {
	body := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:          "https://developer.amd.com.cn/radeon/api/v1",
		Model:        "Qwen3.8-Flash-Next",
		ThinkingMode: "enabled",
	}, body, ReasoningAPIChat)
	if _, exists := body["thinking"]; exists {
		t.Fatalf("AMD Qwen kept thinking: %#v", body)
	}
	if _, exists := body["enable_thinking"]; exists {
		t.Fatalf("AMD Qwen used enable_thinking: %#v", body)
	}
	if got := body["reasoning_effort"]; got != "medium" {
		t.Fatalf("AMD Qwen reasoning_effort = %#v, want medium", got)
	}

	onWithNone := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:             "https://developer.amd.com.cn/radeon/api/v1",
		Model:           "DeepSeek-V4-Flash",
		ThinkingMode:    "enabled",
		ReasoningEffort: "none",
	}, onWithNone, ReasoningAPIChat)
	if got := onWithNone["reasoning_effort"]; got != "low" {
		t.Fatalf("enabled none effort = %#v, want low", got)
	}
}

func TestRewriteAMDRadeonChatReasoningClampsEffortAndEnabledFlag(t *testing.T) {
	amd := "https://developer.amd.com.cn/radeon/api/v1"

	enabledFlag := map[string]interface{}{"reasoning": map[string]interface{}{"enabled": true}}
	RewriteAMDRadeonChatReasoning(MaclawLLMConfig{URL: amd, Model: "DeepSeek-V4-Flash"}, enabledFlag)
	if _, exists := enabledFlag["reasoning"]; exists {
		t.Fatalf("reasoning.enabled kept: %#v", enabledFlag)
	}
	if got := enabledFlag["reasoning_effort"]; got != "medium" {
		t.Fatalf("reasoning.enabled effort = %#v, want medium", got)
	}

	qwen := map[string]interface{}{"reasoning_effort": "xhigh"}
	RewriteAMDRadeonChatReasoning(MaclawLLMConfig{URL: amd, Model: "Qwen3.8-27B"}, qwen)
	if got := qwen["reasoning_effort"]; got != "medium" {
		t.Fatalf("Qwen xhigh = %#v, want medium", got)
	}

	deepseek := map[string]interface{}{"reasoning_effort": "xhigh"}
	RewriteAMDRadeonChatReasoning(MaclawLLMConfig{URL: amd, Model: "DeepSeek-V4.1-Flash"}, deepseek)
	if got := deepseek["reasoning_effort"]; got != "xhigh" {
		t.Fatalf("DeepSeek xhigh = %#v, want xhigh", got)
	}

	blank := map[string]interface{}{"reasoning_effort": "  ", "thinking": 1}
	RewriteAMDRadeonChatReasoning(MaclawLLMConfig{URL: amd, Model: "DeepSeek-V4-Flash"}, blank)
	if _, exists := blank["thinking"]; exists {
		t.Fatalf("numeric thinking kept: %#v", blank)
	}
	if _, exists := blank["reasoning_effort"]; exists {
		t.Fatalf("blank reasoning_effort kept: %#v", blank)
	}
}

func TestApplyReasoningControlsAMDUsesReasoningEffort(t *testing.T) {
	body := map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:             "https://developer.amd.com.cn/radeon/api/v1",
		Model:           "DeepSeek-V4.1-Flash",
		ThinkingMode:    "enabled",
		ReasoningEffort: "high",
	}, body, ReasoningAPIChat)
	if _, exists := body["thinking"]; exists {
		t.Fatalf("AMD enabled body kept thinking: %#v", body)
	}
	if got := body["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got)
	}

	off := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:          "https://developer.amd.com.cn/radeon/api/v1",
		Model:        "DeepSeek-V4-Flash",
		ThinkingMode: "disabled",
	}, off, ReasoningAPIChat)
	if _, exists := off["thinking"]; exists {
		t.Fatalf("AMD disabled body kept thinking: %#v", off)
	}
	if got := off["reasoning_effort"]; got != "none" {
		t.Fatalf("disabled reasoning_effort = %#v, want none", got)
	}
}

func TestCoerceAlwaysOnThinkingMode(t *testing.T) {
	got := CoerceAlwaysOnThinkingMode(MaclawLLMConfig{Model: "glm-5.3", ThinkingMode: "off"})
	if got.ThinkingMode != "enabled" {
		t.Fatalf("off = %q, want enabled", got.ThinkingMode)
	}
	got = CoerceAlwaysOnThinkingMode(MaclawLLMConfig{Model: "glm-5.3", ThinkingMode: ""})
	if got.ThinkingMode != "" {
		t.Fatalf("auto overwritten: %q", got.ThinkingMode)
	}
	got = CoerceAlwaysOnThinkingMode(MaclawLLMConfig{Model: "claude-sonnet", ThinkingMode: "disabled"})
	if got.ThinkingMode != "disabled" {
		t.Fatalf("unrelated model coerced: %q", got.ThinkingMode)
	}
}
