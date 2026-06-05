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
