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
