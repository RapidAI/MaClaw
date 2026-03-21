package corelib

import (
	"encoding/json"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

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
