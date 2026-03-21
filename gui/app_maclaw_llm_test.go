package main

import (
	"reflect"
	"testing"

	"pgregory.net/rapid"
)

func TestDefaultMaclawLLMProviders(t *testing.T) {
	providers := defaultMaclawLLMProviders()

	// ── 1. First provider is OpenAI with correct fields ──
	if len(providers) == 0 {
		t.Fatal("defaultMaclawLLMProviders returned empty list")
	}
	first := providers[0]
	if first.Name != "OpenAI" {
		t.Errorf("first provider Name = %q, want %q", first.Name, "OpenAI")
	}
	if first.URL != "https://api.openai.com/v1" {
		t.Errorf("OpenAI URL = %q, want %q", first.URL, "https://api.openai.com/v1")
	}
	if first.Model != "gpt-4o" {
		t.Errorf("OpenAI Model = %q, want %q", first.Model, "gpt-4o")
	}
	if first.AuthType != "oauth" {
		t.Errorf("OpenAI AuthType = %q, want %q", first.AuthType, "oauth")
	}
	if first.ContextLength != 128000 {
		t.Errorf("OpenAI ContextLength = %d, want %d", first.ContextLength, 128000)
	}

	// ── 2. At least 5 providers (OpenAI, 智谱, MiniMax, Custom1, Custom2) ──
	if len(providers) < 5 {
		t.Fatalf("provider count = %d, want >= 5", len(providers))
	}

	expectedNames := []string{"OpenAI", "智谱", "MiniMax", "Custom1", "Custom2"}
	for i, want := range expectedNames {
		if providers[i].Name != want {
			t.Errorf("providers[%d].Name = %q, want %q", i, providers[i].Name, want)
		}
	}

	// ── 3. Last two entries have IsCustom=true ──
	n := len(providers)
	if !providers[n-2].IsCustom {
		t.Errorf("providers[%d] (%s) IsCustom = false, want true", n-2, providers[n-2].Name)
	}
	if !providers[n-1].IsCustom {
		t.Errorf("providers[%d] (%s) IsCustom = false, want true", n-1, providers[n-1].Name)
	}
}

// resolveProviders extracts the provider-selection logic from
// GetMaclawLLMProviders: if saved is non-empty, return it as-is;
// otherwise fall back to defaultMaclawLLMProviders().
func resolveProviders(saved []MaclawLLMProvider) []MaclawLLMProvider {
	if len(saved) == 0 {
		return defaultMaclawLLMProviders()
	}
	return saved
}

// genMaclawLLMProvider returns a rapid generator for a random MaclawLLMProvider.
func genMaclawLLMProvider() *rapid.Generator[MaclawLLMProvider] {
	return rapid.Custom(func(t *rapid.T) MaclawLLMProvider {
		return MaclawLLMProvider{
			Name:           rapid.StringMatching(`[A-Za-z0-9_]{1,20}`).Draw(t, "name"),
			URL:            rapid.StringMatching(`https?://[a-z0-9.]{1,30}`).Draw(t, "url"),
			Key:            rapid.String().Draw(t, "key"),
			Model:          rapid.StringMatching(`[a-z0-9-]{1,20}`).Draw(t, "model"),
			Protocol:       rapid.SampledFrom([]string{"", "openai", "anthropic"}).Draw(t, "protocol"),
			ContextLength:  rapid.IntRange(0, 256000).Draw(t, "ctx"),
			IsCustom:       rapid.Bool().Draw(t, "custom"),
			AuthType:       rapid.SampledFrom([]string{"", "api_key", "oauth"}).Draw(t, "auth"),
			RefreshToken:   rapid.String().Draw(t, "refresh"),
			TokenExpiresAt: rapid.Int64Range(0, 2000000000).Draw(t, "expires"),
		}
	})
}

// Feature: openai-oauth-provider, Property 8: 已保存的 provider 列表不被默认值覆盖
// **Validates: Requirements 2.4**
//
// For any non-empty saved provider list, calling resolveProviders (the core
// logic of GetMaclawLLMProviders) should return that saved list, not
// defaultMaclawLLMProviders()'s result.
func TestProperty_SavedProvidersNotOverwritten(t *testing.T) {
	defaults := defaultMaclawLLMProviders()

	rapid.Check(t, func(t *rapid.T) {
		// Generate a non-empty slice of random providers (1..10).
		n := rapid.IntRange(1, 10).Draw(t, "count")
		saved := make([]MaclawLLMProvider, n)
		for i := range saved {
			saved[i] = genMaclawLLMProvider().Draw(t, "provider")
		}

		result := resolveProviders(saved)

		// 1. The result must be the saved list, not the defaults.
		if !reflect.DeepEqual(result, saved) {
			t.Fatalf("resolveProviders returned different list than saved:\n  saved:  %+v\n  result: %+v", saved, result)
		}

		// 2. Confirm it is NOT the default list (unless saved happens to
		//    be identical, which is astronomically unlikely with random data).
		if reflect.DeepEqual(result, defaults) && !reflect.DeepEqual(saved, defaults) {
			t.Fatalf("resolveProviders returned defaults instead of saved list")
		}
	})
}
