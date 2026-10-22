package im

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestLLMProviderRegistryRoundTripUpstreamOverrides(t *testing.T) {
	repo := &testSystemSettingsRepo{}
	ctx := context.Background()
	if err := SaveLLMProviderRegistry(ctx, repo, &LLMProviderRegistry{
		Providers: []LLMProvider{{
			ID:                "p",
			Name:              "P",
			APIURL:            "https://example.com/v1",
			APIKey:            "secret",
			Model:             "m",
			ExtraBody:         map[string]any{"reasoning_effort": "none"},
			ExtraBodyPriority: "MEMBER",
			DropFields:        []string{"thinking", "thinking"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadLLMProviderRegistry(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Providers[0]
	if got.ExtraBody["reasoning_effort"] != "none" || got.ExtraBodyPriority != "" || len(got.DropFields) != 1 || got.DropFields[0] != "thinking" {
		t.Fatalf("loaded extra %#v priority %q drop %#v", got.ExtraBody, got.ExtraBodyPriority, got.DropFields)
	}

	err = SaveLLMProviderRegistry(ctx, repo, &LLMProviderRegistry{
		Providers: []LLMProvider{{
			ID:        "p",
			ExtraBody: map[string]any{"model": "hijack"},
		}},
	})
	var overrideErr *llmpool.MemberUpstreamOverrideError
	if !errors.As(err, &overrideErr) {
		t.Fatalf("err = %v", err)
	}
	loaded, err = LoadLLMProviderRegistry(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Providers[0].ExtraBody["reasoning_effort"] != "none" {
		t.Fatalf("invalid save replaced the registry: %#v", loaded.Providers[0].ExtraBody)
	}
}
