package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSetMaclawLLMCurrentModelUpdatesProviderAndLegacyField(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Key: "k", Model: "deepseek-chat"},
			{Name: "Custom1", URL: "https://x.example/v1", Key: "k2", Model: "old-model", IsCustom: true},
		},
		MaclawLLMCurrentProvider: "Custom1",
		MaclawLLMModel:           "old-model",
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	if err := app.SetMaclawLLMCurrentModel("new-model"); err != nil {
		t.Fatalf("SetMaclawLLMCurrentModel: %v", err)
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.MaclawLLMModel != "new-model" {
		t.Fatalf("MaclawLLMModel = %q, want new-model", cfg.MaclawLLMModel)
	}
	found := false
	for _, p := range cfg.MaclawLLMProviders {
		if p.Name == "Custom1" {
			found = true
			if p.Model != "new-model" {
				t.Fatalf("Custom1.Model = %q, want new-model", p.Model)
			}
			has := false
			for _, m := range p.Models {
				if m == "new-model" {
					has = true
					break
				}
			}
			if !has {
				t.Fatalf("Custom1.Models missing new-model: %#v", p.Models)
			}
		}
		if p.Name == "DeepSeek" && p.Model != "deepseek-chat" {
			t.Fatalf("DeepSeek model should be unchanged, got %q", p.Model)
		}
	}
	if !found {
		t.Fatal("Custom1 provider not found after model switch")
	}
	// Sticky MoA should not be cleared by model-only switch — no direct assert; method contract is not calling clearMoASticky.
}

func TestSetMaclawLLMCurrentModelRejectsEmpty(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SetMaclawLLMCurrentModel("  "); err == nil {
		t.Fatal("expected error for empty model")
	}
}
