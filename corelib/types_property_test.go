package corelib

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestSetCodeGenClientNameHeaderIfNeeded(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://codegen.qianxin-inc.cn/api/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetCodeGenClientNameHeaderIfNeeded(req)
	if got := req.Header.Get(CodeGenClientNameHeader); got != CodeGenClientName {
		t.Fatalf("%s = %q, want %q", CodeGenClientNameHeader, got, CodeGenClientName)
	}

	custom, err := http.NewRequest(http.MethodGet, "https://api.codegen.qianxin-inc.cn/api/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetCodeGenClientNameHeaderIfNeededWithName(custom, "custom-agent")
	if got := custom.Header.Get(CodeGenClientNameHeader); got != "custom-agent" {
		t.Fatalf("custom %s = %q, want %q", CodeGenClientNameHeader, got, "custom-agent")
	}

	legacyDefault, err := http.NewRequest(http.MethodGet, "https://codegen.qianxin-inc.cn/api/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetCodeGenClientNameHeaderIfNeededWithName(legacyDefault, "openclaw")
	if got := legacyDefault.Header.Get(CodeGenClientNameHeader); got != CodeGenClientName {
		t.Fatalf("legacy default %s = %q, want %q", CodeGenClientNameHeader, got, CodeGenClientName)
	}

	other, err := http.NewRequest(http.MethodGet, "https://api.example.com/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetCodeGenClientNameHeaderIfNeeded(other)
	if got := other.Header.Get(CodeGenClientNameHeader); got != "" {
		t.Fatalf("non-CodeGen %s = %q, want empty", CodeGenClientNameHeader, got)
	}

	lookalike, err := http.NewRequest(http.MethodGet, "https://codegen.qianxin-inc.cn.evil.example/api/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	SetCodeGenClientNameHeaderIfNeeded(lookalike)
	if got := lookalike.Header.Get(CodeGenClientNameHeader); got != "" {
		t.Fatalf("lookalike %s = %q, want empty", CodeGenClientNameHeader, got)
	}

	if !IsCodeGenURL("wss://api.codegen.qianxin-inc.cn/api/v1/responses") {
		t.Fatal("CodeGen websocket URL was not recognized")
	}
	if IsCodeGenURL("wss://codegen.qianxin-inc.cn.evil.example/api/v1/responses") {
		t.Fatal("lookalike websocket URL was recognized as CodeGen")
	}
}

func TestNormalizeCodeGenSSOProvider(t *testing.T) {
	if got := NormalizeCodeGenModel(" auto "); got != CodeGenDefaultModelID {
		t.Fatalf("NormalizeCodeGenModel(auto) = %q, want %q", got, CodeGenDefaultModelID)
	}
	if got := NormalizeCodeGenModel("qax-codegen/Qwen-Flash"); got != "qax-codegen/Qwen-Flash" {
		t.Fatalf("NormalizeCodeGenModel(custom) = %q, want custom model", got)
	}

	provider := NormalizeCodeGenSSOProvider(MaclawLLMProvider{
		Name:      " codegen ",
		URL:       " https://codegen.qianxin-inc.cn/api/v1/anthropic/ ",
		Model:     " auto ",
		Protocol:  "anthropic",
		AgentType: "openclaw",
		AuthType:  " sso ",
	})
	if provider.Name != "CodeGen" {
		t.Fatalf("Name = %q, want CodeGen", provider.Name)
	}
	if provider.URL != "https://codegen.qianxin-inc.cn/api/v1" {
		t.Fatalf("URL = %q, want OpenAI base URL", provider.URL)
	}
	if provider.Model != CodeGenDefaultModelID {
		t.Fatalf("Model = %q, want %q", provider.Model, CodeGenDefaultModelID)
	}
	if provider.Protocol != "openai" {
		t.Fatalf("Protocol = %q, want openai", provider.Protocol)
	}
	if provider.AgentType != CodeGenClientName {
		t.Fatalf("AgentType = %q, want %q", provider.AgentType, CodeGenClientName)
	}

	hub := NormalizeCodeGenSSOProvider(MaclawLLMProvider{
		Name:     "hub",
		URL:      "https://hub.example.test/api/llm/v1",
		Model:    "auto",
		Protocol: "openai",
		AuthType: "sso",
	})
	if hub.Name != "hub" || hub.Model != "auto" {
		t.Fatalf("non-CodeGen SSO provider should be unchanged, got %#v", hub)
	}
}

func TestSanitizeCodeGenOpenAIChatToolsValue(t *testing.T) {
	tools := SanitizeCodeGenOpenAIChatToolsValue([]interface{}{map[string]interface{}{
		"type":                 "function",
		"x_execution_contract": "local-only",
		"function": map[string]interface{}{
			"name":   "strict_tool",
			"strict": true,
			"parameters": map[string]interface{}{
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"values": map[string]interface{}{
						"type":    "array",
						"oneOf":   []interface{}{map[string]interface{}{"type": "string"}},
						"default": []interface{}{"x"},
					},
					"metadata": map[string]interface{}{
						"type": "object",
						"additionalProperties": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		},
	}}).([]interface{})

	tool := tools[0].(map[string]interface{})
	if _, ok := tool["x_execution_contract"]; ok {
		t.Fatalf("local tool field leaked: %#v", tool)
	}
	fn := tool["function"].(map[string]interface{})
	if _, ok := fn["strict"]; ok {
		t.Fatalf("strict leaked: %#v", fn)
	}
	params := fn["parameters"].(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("additionalProperties=false leaked: %#v", params)
	}
	values := params["properties"].(map[string]interface{})["values"].(map[string]interface{})
	for _, bad := range []string{"oneOf", "default"} {
		if _, ok := values[bad]; ok {
			t.Fatalf("%s leaked: %#v", bad, values)
		}
	}
	metadata := params["properties"].(map[string]interface{})["metadata"].(map[string]interface{})
	if _, ok := metadata["additionalProperties"]; ok {
		t.Fatalf("additionalProperties schema leaked: %#v", metadata)
	}
	if got := values["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("array items type = %#v, want string", got)
	}

	functions := SanitizeCodeGenOpenAIFunctionsValue([]interface{}{map[string]interface{}{
		"name":   "legacy_function",
		"strict": true,
		"parameters": map[string]interface{}{
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"ids": map[string]interface{}{"type": "array"},
			},
		},
	}}).([]interface{})
	fn = functions[0].(map[string]interface{})
	if _, ok := fn["strict"]; ok {
		t.Fatalf("legacy strict leaked: %#v", fn)
	}
	params = fn["parameters"].(map[string]interface{})
	if _, ok := params["additionalProperties"]; ok {
		t.Fatalf("legacy additionalProperties=false leaked: %#v", params)
	}
	ids := params["properties"].(map[string]interface{})["ids"].(map[string]interface{})
	if got := ids["items"].(map[string]interface{})["type"]; got != "string" {
		t.Fatalf("legacy array items type = %#v, want string", got)
	}
}

func TestMaclawLLMUserAgentTrimsCustomValue(t *testing.T) {
	provider := MaclawLLMProvider{AgentType: "  custom-agent  "}
	if got := provider.UserAgent(); got != "custom-agent" {
		t.Fatalf("provider UserAgent() = %q, want %q", got, "custom-agent")
	}

	config := MaclawLLMConfig{AgentType: "  tigerclaw  "}
	if got := config.UserAgent(); got != "tigerclaw" {
		t.Fatalf("config UserAgent() = %q, want %q", got, "tigerclaw")
	}

	blank := MaclawLLMConfig{AgentType: "   "}
	if got := blank.UserAgent(); got != "openclaw" {
		t.Fatalf("blank UserAgent() = %q, want %q", got, "openclaw")
	}
}

// Feature: openai-oauth-provider, Property 1: MaclawLLMProvider JSON 序列化往返
// **Validates: Requirements 1.1, 1.2, 1.3, 1.5**
//
// For any MaclawLLMProvider instance, marshalling to JSON then unmarshalling
// back should produce a struct equal to the original. When AuthType,
// RefreshToken, TokenExpiresAt are zero values, they must NOT appear in the
// JSON output (omitempty behaviour).
func TestProperty_MaclawLLMProvider_JSONRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := MaclawLLMProvider{
			Name:           rapid.String().Draw(t, "name"),
			URL:            rapid.String().Draw(t, "url"),
			Key:            rapid.String().Draw(t, "key"),
			Model:          rapid.String().Draw(t, "model"),
			Protocol:       rapid.String().Draw(t, "protocol"),
			ContextLength:  rapid.Int().Draw(t, "context_length"),
			IsCustom:       rapid.Bool().Draw(t, "is_custom"),
			AuthType:       rapid.String().Draw(t, "auth_type"),
			RefreshToken:   rapid.String().Draw(t, "refresh_token"),
			TokenExpiresAt: rapid.Int64().Draw(t, "token_expires_at"),
		}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded MaclawLLMProvider
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if decoded != original {
			t.Fatalf("round-trip mismatch:\n  original: %+v\n  decoded:  %+v", original, decoded)
		}
	})
}

// TestProperty_MaclawLLMProvider_OmitEmpty verifies that when AuthType,
// RefreshToken, and TokenExpiresAt are zero values, they do NOT appear in the
// JSON output. Other omitempty fields (Protocol, ContextLength, IsCustom) are
// also checked.
// **Validates: Requirements 1.5**
func TestProperty_MaclawLLMProvider_OmitEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Build a provider where the three OAuth fields are always zero,
		// but the required fields (Name, URL, Key, Model) are random.
		p := MaclawLLMProvider{
			Name:  rapid.String().Draw(t, "name"),
			URL:   rapid.String().Draw(t, "url"),
			Key:   rapid.String().Draw(t, "key"),
			Model: rapid.String().Draw(t, "model"),
			// All omitempty fields left at zero values.
		}

		data, err := json.Marshal(p)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		raw := string(data)

		omitFields := []string{
			`"auth_type"`,
			`"refresh_token"`,
			`"token_expires_at"`,
			`"protocol"`,
			`"context_length"`,
			`"is_custom"`,
		}
		for _, field := range omitFields {
			if strings.Contains(raw, field) {
				t.Fatalf("zero-value field %s should be omitted from JSON, got: %s", field, raw)
			}
		}
	})
}

func TestNormalizeLLMTokenPricePerMTokensRMBAllowsZero(t *testing.T) {
	if got := NormalizeLLMTokenPricePerMTokensRMB(0, DefaultLLMInputPricePerMTokensRMB); got != 0 {
		t.Fatalf("zero price normalized to %v, want 0", got)
	}
	if got := NormalizeLLMTokenPricePerMTokensRMB(-1, DefaultLLMInputPricePerMTokensRMB); got != DefaultLLMInputPricePerMTokensRMB {
		t.Fatalf("negative price normalized to %v, want default", got)
	}
	_, _, total := CalculateLLMCostRMB(1_000_000, 1_000_000, 0, 0)
	if total != 0 {
		t.Fatalf("zero token prices produced total cost %v, want 0", total)
	}
}
