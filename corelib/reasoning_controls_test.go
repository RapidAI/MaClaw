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
		{raw: "", mode: "auto", ok: true},
		{raw: " auto ", mode: "auto", ok: true},
		{raw: "low", mode: "low", ok: true},
		{raw: "HIGH", mode: "high", ok: true},
		{raw: "max", mode: "max", ok: true},
		{raw: "ENABLE", mode: "high", ok: true},
		{raw: "true", mode: "high", ok: true},
		{raw: "DISABLE", mode: "none", ok: true},
		{raw: "none", mode: "none", ok: true},
		{raw: "unexpected", mode: "", ok: false},
	}
	for _, tt := range tests {
		mode, ok := ParseGlobalThinkingMode(tt.raw)
		if mode != tt.mode || ok != tt.ok {
			t.Errorf("ParseGlobalThinkingMode(%q) = (%q, %v), want (%q, %v)", tt.raw, mode, ok, tt.mode, tt.ok)
		}
	}
	if got := EffectiveGlobalThinkingMode("auto"); got != "auto" {
		t.Fatalf("auto effective mode = %q, want auto", got)
	}
	if got := EffectiveGlobalThinkingMode("invalid"); got != "auto" {
		t.Fatalf("invalid effective mode = %q, want auto", got)
	}
	if got := NormalizeGlobalThinkingMode("auto"); got != "" {
		t.Fatalf("persisted auto = %q, want unset", got)
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
	if reasoning["effort"] != "low" {
		t.Fatalf("reasoning.effort = %#v, want low", reasoning["effort"])
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

func TestApplyReasoningControlsGLMFlashAnthropicUsesLowEffort(t *testing.T) {
	for _, model := range []string{"glm-5.3-flash", "GLM-5.3-FlashX", "vendor/glm-5.3-flash[1m]"} {
		body := map[string]interface{}{}
		ApplyReasoningControls(
			MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: model, ThinkingMode: "disabled"},
			body,
			ReasoningAPIAnthropic,
		)
		thinking, _ := body["thinking"].(map[string]interface{})
		if thinking["type"] != "enabled" {
			t.Fatalf("model %s thinking = %#v, want type=enabled", model, body["thinking"])
		}
		if _, ok := thinking["budget_tokens"]; ok {
			t.Fatalf("model %s kept a budget: %#v", model, thinking)
		}
		if body["reasoning_effort"] != "low" {
			t.Fatalf("model %s reasoning_effort = %#v, want low", model, body["reasoning_effort"])
		}
	}

	body := map[string]interface{}{}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://api.anthropic.com", Model: "claude-sonnet", ThinkingMode: "disabled"},
		body,
		ReasoningAPIAnthropic,
	)
	if _, ok := body["thinking"]; ok {
		t.Fatalf("claude disabled thinking = %#v, want the block omitted", body["thinking"])
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
	if thinking["type"] != "enabled" {
		t.Fatalf("auto glm-5.3 empty Anthropic body = %#v, want thinking.enabled", body)
	}
	if _, ok := thinking["budget_tokens"]; ok {
		t.Fatalf("auto glm-5.3 must not send a budget: %#v", thinking)
	}
	if _, ok := body["reasoning_effort"]; ok {
		t.Fatalf("auto glm-5.3 must keep the provider default effort: %#v", body)
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
	if got.ThinkingMode != "enabled" || got.ReasoningEffort != "low" {
		t.Fatalf("off = mode %q effort %q, want enabled/low", got.ThinkingMode, got.ReasoningEffort)
	}
	got = CoerceAlwaysOnThinkingMode(MaclawLLMConfig{Model: "glm-5.3-flash", ThinkingMode: "none"})
	if got.ThinkingMode != "enabled" || got.ReasoningEffort != "low" {
		t.Fatalf("flash off = mode %q effort %q, want enabled/low", got.ThinkingMode, got.ReasoningEffort)
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

func TestApplyGlobalThinkingLevelMapsProviders(t *testing.T) {
	levels := []struct {
		level string
		check func(t *testing.T, body map[string]interface{})
		cfg   MaclawLLMConfig
		api   ReasoningAPIKind
	}{
		{
			level: "max",
			cfg:   MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3-flash"},
			api:   ReasoningAPIAnthropic,
			check: func(t *testing.T, body map[string]interface{}) {
				thinking, _ := body["thinking"].(map[string]interface{})
				if thinking["type"] != "enabled" || body["reasoning_effort"] != "max" {
					t.Fatalf("glm max = %#v", body)
				}
			},
		},
		{
			level: "low",
			cfg:   MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				thinking, _ := body["thinking"].(map[string]interface{})
				if thinking["type"] != "enabled" || body["reasoning_effort"] != "low" {
					t.Fatalf("deepseek low = %#v", body)
				}
			},
		},
		{
			level: "max",
			cfg:   MaclawLLMConfig{URL: "https://api.openai.com/v1", Model: "gpt-5"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["reasoning_effort"] != "xhigh" {
					t.Fatalf("openai max = %#v, want xhigh", body["reasoning_effort"])
				}
			},
		},
		{
			level: "high",
			cfg:   MaclawLLMConfig{URL: "https://api.x.ai/v1", Model: "grok-4.5"},
			api:   ReasoningAPIResponses,
			check: func(t *testing.T, body map[string]interface{}) {
				reasoning, _ := body["reasoning"].(map[string]interface{})
				if reasoning["effort"] != "high" {
					t.Fatalf("grok high = %#v", body["reasoning"])
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["enable_thinking"] != false {
					t.Fatalf("qwen none = %#v", body)
				}
			},
		},
		{
			level: "low",
			cfg:   MaclawLLMConfig{URL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Model: "qwen3.8-flash"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["enable_thinking"] != true {
					t.Fatalf("qwen low = %#v", body)
				}
			},
		},
		{
			level: "max",
			cfg:   MaclawLLMConfig{URL: "https://api.anthropic.com", Model: "claude-sonnet"},
			api:   ReasoningAPIAnthropic,
			check: func(t *testing.T, body map[string]interface{}) {
				thinking, _ := body["thinking"].(map[string]interface{})
				if thinking["budget_tokens"] != 16384 {
					t.Fatalf("claude max budget = %#v", thinking)
				}
			},
		},
		{
			level: "max",
			cfg:   MaclawLLMConfig{URL: "https://api.agnes-ai.cn/v1", Model: "agnes-2.5-flash"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["reasoning_effort"] != "high" {
					t.Fatalf("agnes max = %#v, want high", body["reasoning_effort"])
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://api.kimi.com/coding/v1", Model: "kimi-k2-thinking"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				thinking, _ := body["thinking"].(map[string]interface{})
				if thinking["type"] != "disabled" {
					t.Fatalf("kimi none = %#v", body["thinking"])
				}
			},
		},
		{
			level: "high",
			cfg:   MaclawLLMConfig{URL: "https://api.minimaxi.com/v1", Model: "MiniMax-M2.7"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if _, exists := body["thinking"]; exists {
					t.Fatalf("minimax m2 must not send thinking: %#v", body)
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				thinking, _ := body["thinking"].(map[string]interface{})
				if thinking["type"] != "disabled" {
					t.Fatalf("minimax m3 none = %#v", body["thinking"])
				}
			},
		},
		{
			level: "high",
			cfg:   MaclawLLMConfig{URL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				thinking, _ := body["thinking"].(map[string]interface{})
				if thinking["type"] != "adaptive" {
					t.Fatalf("minimax m3 high = %#v", body["thinking"])
				}
			},
		},
		{
			level: "high",
			cfg:   MaclawLLMConfig{URL: "https://api.kimi.com/coding/v1", Model: "kimi-k3"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if _, exists := body["thinking"]; exists {
					t.Fatalf("kimi k3 kept thinking: %#v", body)
				}
				if body["reasoning_effort"] != "high" {
					t.Fatalf("kimi k3 high = %#v", body["reasoning_effort"])
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://api.moonshot.cn/v1", Model: "kimi-k3.1-pro"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["reasoning_effort"] != "low" {
					t.Fatalf("kimi k3 none = %#v", body)
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://api.kimi.com/coding/v1", Model: "kimi-k2.7-code"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				thinking, _ := body["thinking"].(map[string]interface{})
				if thinking["type"] != "enabled" || thinking["keep"] != "all" {
					t.Fatalf("kimi k2.7-code none = %#v", body["thinking"])
				}
				if _, exists := body["reasoning_effort"]; exists {
					t.Fatalf("kimi k2.7-code gained reasoning_effort: %#v", body)
				}
			},
		},
		{
			level: "max",
			cfg:   MaclawLLMConfig{URL: "https://api.groq.com/openai/v1", Model: "qwen/qwen3-32b"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if _, exists := body["thinking"]; exists {
					t.Fatalf("groq kept thinking: %#v", body)
				}
				if body["reasoning_effort"] != "xhigh" {
					t.Fatalf("groq max = %#v", body["reasoning_effort"])
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://api.groq.com/openai/v1", Model: "qwen/qwen3-32b"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["reasoning_effort"] != "none" {
					t.Fatalf("groq none = %#v", body["reasoning_effort"])
				}
			},
		},
		{
			level: "high",
			cfg:   MaclawLLMConfig{URL: "https://openrouter.ai/api/v1", Model: "openai/gpt-4o"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				reasoning, _ := body["reasoning"].(map[string]interface{})
				if reasoning["effort"] != "high" {
					t.Fatalf("openrouter high = %#v", body["reasoning"])
				}
				if _, exists := body["thinking"]; exists {
					t.Fatalf("openrouter kept thinking: %#v", body)
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://openrouter.ai/api/v1", Model: "openai/gpt-4o"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				reasoning, _ := body["reasoning"].(map[string]interface{})
				if reasoning["effort"] != "none" {
					t.Fatalf("openrouter none = %#v", body["reasoning"])
				}
			},
		},
		{
			level: "max",
			cfg:   MaclawLLMConfig{URL: "https://api.together.xyz/v1", Model: "openai/gpt-oss-120b"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if _, exists := body["reasoning"]; exists || body["reasoning_effort"] != "high" {
					t.Fatalf("together max = %#v, want reasoning_effort high alone", body)
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://api.together.xyz/v1", Model: "MiniMaxAI/MiniMax-M3"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				reasoning, _ := body["reasoning"].(map[string]interface{})
				if reasoning["enabled"] != false {
					t.Fatalf("together none = %#v", body)
				}
				if _, exists := body["reasoning_effort"]; exists {
					t.Fatalf("together none kept reasoning_effort: %#v", body)
				}
			},
		},
		{
			level: "low",
			cfg:   MaclawLLMConfig{URL: "https://ark.cn-beijing.volces.com/api/coding", Model: "doubao-seed-code-preview-latest"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				thinking, _ := body["thinking"].(map[string]interface{})
				if thinking["type"] != "enabled" || body["reasoning_effort"] != "low" {
					t.Fatalf("doubao low = %#v", body)
				}
			},
		},
		{
			level: "max",
			cfg:   MaclawLLMConfig{URL: "https://ark.cn-beijing.volces.com/api/coding", Model: "doubao-seed-2-0-pro"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["reasoning_effort"] != "high" {
					t.Fatalf("doubao max = %#v, want high", body["reasoning_effort"])
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://api.x.ai/v1", Model: "grok-4.5"},
			api:   ReasoningAPIResponses,
			check: func(t *testing.T, body map[string]interface{}) {
				reasoning, _ := body["reasoning"].(map[string]interface{})
				if reasoning["effort"] != "low" {
					t.Fatalf("grok none = %#v, want low", body["reasoning"])
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-3.8-flash"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if _, exists := body["thinking"]; exists {
					t.Fatalf("gemini kept thinking: %#v", body)
				}
				if body["reasoning_effort"] != "low" {
					t.Fatalf("gemini 3 none = %#v, want low", body["reasoning_effort"])
				}
			},
		},
		{
			level: "max",
			cfg:   MaclawLLMConfig{URL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-4-argon"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["reasoning_effort"] != "high" {
					t.Fatalf("gemini 4 max = %#v, want high", body["reasoning_effort"])
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-2.5-flash"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["reasoning_effort"] != "none" {
					t.Fatalf("gemini 2.5 flash none = %#v, want none", body["reasoning_effort"])
				}
			},
		},
		{
			level: "none",
			cfg:   MaclawLLMConfig{URL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-2.5-pro"},
			api:   ReasoningAPIChat,
			check: func(t *testing.T, body map[string]interface{}) {
				if body["reasoning_effort"] != "low" {
					t.Fatalf("gemini 2.5 pro none = %#v, want low", body["reasoning_effort"])
				}
			},
		},
		{
			level: "low",
			cfg:   MaclawLLMConfig{URL: "https://ark.cn-beijing.volces.com/api/plan/v3", Model: "glm-5.2"},
			api:   ReasoningAPIResponses,
			check: func(t *testing.T, body map[string]interface{}) {
				thinking, _ := body["thinking"].(map[string]interface{})
				if thinking["type"] != "enabled" || body["reasoning_effort"] != "low" {
					t.Fatalf("ark glm-5.2 low = %#v", body)
				}
			},
		},
	}
	for _, tt := range levels {
		t.Run(tt.cfg.Model+"/"+tt.level, func(t *testing.T) {
			cfg := ApplyGlobalThinkingLevel(tt.cfg, tt.level)
			body := map[string]interface{}{}
			ApplyReasoningControls(cfg, body, tt.api)
			tt.check(t, body)
		})
	}
}

func TestThinkingOffRejectedEffort(t *testing.T) {
	effort, ok := ThinkingOffRejectedEffort(`[1210][该模型始终思考，不支持关闭思考；请使用 low、high 或 max。]`)
	if !ok || effort != "low" {
		t.Fatalf("effort = %q ok=%v, want low", effort, ok)
	}
	if _, ok := ThinkingOffRejectedEffort("HTTP 400: invalid api key"); ok {
		t.Fatal("unrelated 400 looked like a thinking refusal")
	}
	effort, ok = ThinkingOffRejectedEffort("thinking cannot be disabled; the trace was slower")
	if !ok || effort != "low" {
		t.Fatalf("slower = %q ok=%v, want low", effort, ok)
	}
	effort, ok = ThinkingOffRejectedEffort("thinking cannot be disabled; see the highlight")
	if !ok || effort != "low" {
		t.Fatalf("highlight = %q ok=%v, want low", effort, ok)
	}
	effort, ok = ThinkingOffRejectedEffort("thinking cannot be disabled; maximal context")
	if !ok || effort != "low" {
		t.Fatalf("maximal = %q ok=%v, want low", effort, ok)
	}
	effort, ok = ThinkingOffRejectedEffort("does not support disabling thinking; use high or max")
	if !ok || effort != "high" {
		t.Fatalf("named high = %q ok=%v, want high", effort, ok)
	}
	effort, ok = ThinkingOffRejectedEffort(`thinking cannot be disabled {"max_tokens":2048}`)
	if !ok || effort != "low" {
		t.Fatalf("max_tokens = %q ok=%v, want low", effort, ok)
	}
	effort, ok = ThinkingOffRejectedEffort("cannot disable thinking; max-tokens was 256")
	if !ok || effort != "low" {
		t.Fatalf("max-tokens = %q ok=%v, want low", effort, ok)
	}
	effort, ok = ThinkingOffRejectedEffort("does not support turning off thinking; use max")
	if !ok || effort != "max" {
		t.Fatalf("named max = %q ok=%v, want max", effort, ok)
	}
	if _, ok := ThinkingOffRejectedEffort("1210 invalid low max_tokens"); ok {
		t.Fatal("1210 plus max_tokens looked like a named max level")
	}
}

func TestApplyReasoningControlsAutoDoesNotInventVendorEffort(t *testing.T) {
	if IsAlwaysOnThinkingModel(MaclawLLMConfig{Model: "glm-5.3-flash-free"}) {
		t.Fatal("OpenCode Zen glm-5.3-flash-free is not a Zhipu always-on checkpoint")
	}
	free := map[string]interface{}{"model": "glm-5.3-flash-free"}
	ApplyReasoningControls(
		MaclawLLMConfig{URL: "https://opencode.ai/zen/go/v1", Model: "glm-5.3-flash-free"},
		free,
		ReasoningAPIChat,
	)
	assertNoReasoningFields(t, free)

	for _, cfg := range []MaclawLLMConfig{
		{URL: "https://api.groq.com/openai/v1", Model: "glm-5.3-flash"},
		{URL: "https://openrouter.ai/api/v1", Model: "glm-5.3-flash"},
		{URL: "https://api.together.xyz/v1", Model: "glm-5.3-flash"},
		{URL: "https://api.kimi.com/coding/v1", Model: "kimi-k3"},
		{URL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-3.8-flash"},
	} {
		body := map[string]interface{}{"model": cfg.Model}
		ApplyReasoningControls(cfg, body, ReasoningAPIChat)
		assertNoReasoningFields(t, body)
	}

	kept := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:             "https://open.bigmodel.cn/api/anthropic",
		Model:           "glm-5.3",
		ThinkingMode:    "disabled",
		ReasoningEffort: "high",
	}, kept, ReasoningAPIChat)
	if kept["reasoning_effort"] != "high" {
		t.Fatalf("explicit high was downgraded: %#v", kept)
	}

	togetherOff := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:          "https://api.together.xyz/v1",
		Model:        "glm-5.3-flash",
		ThinkingMode: "disabled",
	}, togetherOff, ReasoningAPIChat)
	if _, exists := togetherOff["reasoning"]; exists || togetherOff["reasoning_effort"] != "low" {
		t.Fatalf("together always-on off = %#v, want reasoning_effort low alone", togetherOff)
	}

	kimiOn := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:          "https://api.kimi.com/coding/v1",
		Model:        "kimi-k3",
		ThinkingMode: "enabled",
	}, kimiOn, ReasoningAPIChat)
	assertNoReasoningFields(t, kimiOn)

	togetherOn := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:          "https://api.together.xyz/v1",
		Model:        "openai/gpt-oss-120b",
		ThinkingMode: "enabled",
	}, togetherOn, ReasoningAPIChat)
	reasoning, _ := togetherOn["reasoning"].(map[string]interface{})
	if reasoning["enabled"] != true {
		t.Fatalf("together enabled = %#v", togetherOn)
	}
	if _, exists := togetherOn["reasoning_effort"]; exists {
		t.Fatalf("together empty effort invented a depth: %#v", togetherOn)
	}

	agnes := map[string]interface{}{"thinking": map[string]interface{}{"type": "enabled"}}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:          "https://api.agnes-ai.cn/v1",
		Model:        "agnes-2.5-flash",
		ThinkingMode: "disabled",
	}, agnes, ReasoningAPIResponses)
	assertNoReasoningFields(t, agnes)

	ark := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:   "https://ark.cn-beijing.volces.com/api/coding",
		Model: "glm-5.3",
	}, ark, ReasoningAPIChat)
	arkThinking, _ := ark["thinking"].(map[string]interface{})
	if arkThinking["type"] != "enabled" {
		t.Fatalf("ark glm auto = %#v, want thinking.enabled", ark)
	}
	if _, exists := ark["reasoning_effort"]; exists {
		t.Fatalf("ark glm auto invented an effort: %#v", ark)
	}
}

func TestProbeNeedsReasoningRoom(t *testing.T) {
	needs := []MaclawLLMConfig{
		{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3-flash"},
		{URL: "https://api.x.ai/v1", Model: "grok-4.5"},
		{URL: "https://api.kimi.com/coding/v1", Model: "kimi-k3"},
		{URL: "https://api.moonshot.cn/v1", Model: "kimi-k2.7-code"},
		{URL: "https://api.minimaxi.com/v1", Model: "MiniMax-M2.7"},
		{URL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-3.8-flash"},
		{URL: "generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-4-argon"},
		{Protocol: "google", URL: "https://generativelanguage.googleapis.com/v1beta", Model: "gemini-3.8-flash"},
		{Protocol: "google", URL: "https://generativelanguage.googleapis.com/v1beta", Model: "gemini-2.5-pro"},
	}
	for _, cfg := range needs {
		if !ProbeNeedsReasoningRoom(cfg) {
			t.Fatalf("%s/%s should keep room for a trace", cfg.URL, cfg.Model)
		}
	}
	quiet := []MaclawLLMConfig{
		{Model: "glm-5.3-flash-free", URL: "https://opencode.ai/zen/go/v1"},
		{Model: "claude-sonnet", URL: "https://api.anthropic.com"},
		{Model: "kimi-k2-thinking", URL: "https://api.kimi.com/coding/v1"},
		{Model: "MiniMax-M3", URL: "https://api.minimaxi.com/v1"},
		{Model: "kimi-k3", URL: "https://api.openai.com/v1"},
		{Model: "gemini-2.5-flash", URL: "https://generativelanguage.googleapis.com/v1beta/openai"},
		{Model: "gemini-3.8-flash", URL: "https://api.openai.com/v1"},
		{Protocol: "google", Model: "gemini-2.5-flash", URL: "https://generativelanguage.googleapis.com/v1beta"},
	}
	for _, cfg := range quiet {
		if ProbeNeedsReasoningRoom(cfg) {
			t.Fatalf("%s/%s should use the short probe cap", cfg.URL, cfg.Model)
		}
	}
	xai := UseCheapLegalThinking(MaclawLLMConfig{URL: "https://api.x.ai/v1", Model: "grok-4.5"})
	if xai.ThinkingMode != "disabled" || xai.ReasoningEffort != "low" {
		t.Fatalf("xai short answer = mode %q effort %q", xai.ThinkingMode, xai.ReasoningEffort)
	}
	glm := UseCheapLegalThinking(MaclawLLMConfig{URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3-flash"})
	if glm.ThinkingMode != "enabled" || glm.ReasoningEffort != "low" {
		t.Fatalf("glm short answer = mode %q effort %q", glm.ThinkingMode, glm.ReasoningEffort)
	}
}

func TestApplyReasoningControlsSchemeLessHostUsesNativeWire(t *testing.T) {
	kimi := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:             "api.kimi.com/coding/v1",
		Model:           "kimi-k3",
		ThinkingMode:    "enabled",
		ReasoningEffort: "high",
	}, kimi, ReasoningAPIChat)
	if _, exists := kimi["thinking"]; exists {
		t.Fatalf("scheme-less kimi kept thinking: %#v", kimi)
	}
	if kimi["reasoning_effort"] != "high" {
		t.Fatalf("scheme-less kimi = %#v", kimi)
	}

	xai := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:          "api.x.ai/v1",
		Model:        "grok-4.5",
		ThinkingMode: "disabled",
	}, xai, ReasoningAPIChat)
	if _, exists := xai["thinking"]; exists {
		t.Fatalf("scheme-less xai kept thinking: %#v", xai)
	}
	if xai["reasoning_effort"] != "low" {
		t.Fatalf("scheme-less xai = %#v", xai)
	}

	together := map[string]interface{}{}
	ApplyReasoningControls(MaclawLLMConfig{
		URL:          "api.together.xyz/v1",
		Model:        "openai/gpt-oss-120b",
		ThinkingMode: "disabled",
	}, together, ReasoningAPIChat)
	reasoning, _ := together["reasoning"].(map[string]interface{})
	if reasoning["enabled"] != false {
		t.Fatalf("scheme-less together = %#v", together)
	}
	if _, exists := together["thinking"]; exists {
		t.Fatalf("scheme-less together kept thinking: %#v", together)
	}
}

func assertNoReasoningFields(t *testing.T, body map[string]interface{}) {
	t.Helper()
	for _, key := range []string{"thinking", "reasoning", "reasoning_effort", "enable_thinking"} {
		if _, exists := body[key]; exists {
			t.Fatalf("body kept %s: %#v", key, body)
		}
	}
}
