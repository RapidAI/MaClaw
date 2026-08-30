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

	// The reverse direction: an OpenAI-style effort forwarded to DeepSeek must
	// become the thinking object, preserving the requested mode.
	body = map[string]interface{}{"reasoning_effort": "high"}
	RetargetReasoningControlsForUpstream(
		MaclawLLMConfig{URL: "https://api.deepseek.com/v1", Model: "deepseek-reasoner"},
		body,
		ReasoningAPIChat,
	)
	thinking, _ := body["thinking"].(map[string]interface{})
	if thinking["type"] != "enabled" {
		t.Fatalf("retargeted deepseek thinking = %#v, want type=enabled", body["thinking"])
	}
	if _, exists := body["reasoning_effort"]; exists {
		t.Fatalf("retargeted body must not keep reasoning_effort: %#v", body)
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
