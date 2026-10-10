package botmgmt

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSaveLLMSettingsKeepsKeyOutOfTheView(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	view, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "Claude", Protocol: "Anthropic",
		URL: "https://api.anthropic.com/", Key: "sk-ant-secret", Model: "claude-sonnet-4-5",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if view.LLM.DefaultServiceGroup != "system-free" || view.LLM.Current != "system-free" || len(view.LLM.Providers) != 1 {
		t.Fatalf("view = %#v", view.LLM)
	}
	got := view.LLM.Providers[0]
	if got.ID != "lp_abcdef0123456789" || got.Protocol != "anthropic" || got.URL != "https://api.anthropic.com" || !got.KeySet {
		t.Fatalf("provider = %#v", got)
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sk-ant-secret") {
		t.Fatalf("view leaked the key: %s", encoded)
	}

	again, err := svc.SaveLLMSettings(ctx, "tenant-a", got.ID, []LLMProviderInput{{
		ID: got.ID, Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Model: "claude-sonnet-4-5",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if again.LLM.Current != got.ID || !again.LLM.Providers[0].KeySet {
		t.Fatalf("blank key dropped the saved key: %#v", again.LLM)
	}
	raw, err := svc.System.Get(ctx, storageKey("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "sk-ant-secret") {
		t.Fatal("stored settings lost the key")
	}
}

func TestSaveLLMSettingsRejectsProtocolNameAndMissingKey(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	cases := []LLMProviderInput{
		{Name: "Other", Protocol: "azure", URL: "https://llm.example/v1", Key: "sk-1", Model: "m"},
		{Name: "hub-llm", Protocol: "openai", URL: "https://llm.example/v1", Key: "sk-1", Model: "m"},
		{Name: "system-free", Protocol: "openai", URL: "https://llm.example/v1", Key: "sk-1", Model: "m"},
		{Name: "Other", Protocol: "openai", URL: "ftp://llm.example/v1", Key: "sk-1", Model: "m"},
		{Name: "Other", Protocol: "openai", URL: "https://llm.example/v1", Model: "m"},
		{ID: "not-an-id", Name: "Other", Protocol: "openai", URL: "https://llm.example/v1", Model: "m"},
	}
	for _, in := range cases {
		if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{in}); err == nil {
			t.Fatalf("accepted %#v", in)
		}
	}
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "missing", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "Other", Protocol: "openai",
		URL: "https://llm.example/v1", Key: "sk-1", Model: "m",
	}}); err == nil {
		t.Fatal("accepted an unknown current provider")
	}
	if _, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{
		{Name: "智谱编程", Protocol: "anthropic", URL: "https://open.bigmodel.cn/api/anthropic", Key: "sk-1", Model: "glm"},
		{Name: "Zhipu GLM Coding", Protocol: "anthropic", URL: "https://open.bigmodel.cn/api/anthropic", Key: "sk-2", Model: "glm"},
	}); err == nil || !strings.Contains(err.Error(), "llm provider name is duplicated") {
		t.Fatalf("aliased names: %v", err)
	}
}

func TestSaveLLMSettingsKeepsTheConnection(t *testing.T) {
	svc := NewService(&memSettings{})
	ctx := context.Background()
	if _, err := svc.SaveConnection(ctx, "tenant-a", "https://maclaw.example", "token-1", true); err != nil {
		t.Fatal(err)
	}
	view, err := svc.SaveLLMSettings(ctx, "tenant-a", "system-free", []LLMProviderInput{{
		ID: "lp_abcdef0123456789", Name: "Claude", Protocol: "anthropic",
		URL: "https://api.anthropic.com", Key: "sk-ant-secret", Model: "claude-sonnet-4-5",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if view.BaseURL != "https://maclaw.example" || !view.TokenSet || len(view.LLM.Providers) != 1 {
		t.Fatalf("llm save changed the connection: %#v", view)
	}
	again, err := svc.SaveConnection(ctx, "tenant-a", "https://maclaw.example/other", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if again.BaseURL != "https://maclaw.example/other" || !again.TokenSet || len(again.LLM.Providers) != 1 || !again.LLM.Providers[0].KeySet {
		t.Fatalf("connection save changed the llm settings: %#v", again)
	}
	raw, err := svc.System.Get(ctx, storageKey("tenant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "token-1") || !strings.Contains(raw, "sk-ant-secret") {
		t.Fatalf("stored record dropped a secret: %s", raw)
	}
}
