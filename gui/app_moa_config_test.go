package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestGetSaveMoAConfigRoundTrip(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "OpenAI", URL: "https://example.com/v1", Key: "k", Model: "gpt"},
		},
	}); err != nil {
		t.Fatalf("seed SaveConfig: %v", err)
	}

	moa := corelib.MoAConfig{
		Enabled:       true,
		DefaultPreset: "review",
		Presets: map[string]corelib.MoAPresetConfig{
			"review": {
				Enabled:            true,
				DisplayName:        "方案评审",
				Aggregator:         corelib.MoAModelRef{UsePrimary: true},
				ReferenceModels:    []corelib.MoAModelRef{{Provider: "OpenAI", Model: "gpt-4.1"}},
				ReferenceMaxTokens: 600,
			},
		},
	}
	if err := app.SaveMoAConfig(moa); err != nil {
		t.Fatalf("SaveMoAConfig: %v", err)
	}
	got, err := app.GetMoAConfig()
	if err != nil {
		t.Fatalf("GetMoAConfig: %v", err)
	}
	if !got.Enabled || got.DefaultPreset != "review" {
		t.Fatalf("got %#v", got)
	}
	p, ok := got.Presets["review"]
	if !ok || !p.Aggregator.UsePrimary || len(p.ReferenceModels) != 1 {
		t.Fatalf("preset %#v", p)
	}
	// Persist field on disk via LoadConfig
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !saved.MoA.Enabled || saved.MoA.Presets["review"].DisplayName != "方案评审" {
		t.Fatalf("persisted MoA %#v", saved.MoA)
	}
}

func TestSetMoAStickyRequiresHandler(t *testing.T) {
	app := &App{}
	if err := app.SetMoASticky(true); err == nil {
		t.Fatal("expected error without imHandler")
	}
	st := app.GetMoASessionState()
	if st.Sticky {
		t.Fatal("expected not sticky")
	}
}

func TestSaveMoAConfigRejectsUnknownProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: []corelib.MaclawLLMProvider{
			{Name: "OpenAI", URL: "https://example.com/v1", Key: "k", Model: "gpt"},
		},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	moa := corelib.MoAConfig{
		Enabled: true,
		Presets: map[string]corelib.MoAPresetConfig{
			"x": {
				Enabled:    true,
				Aggregator: corelib.MoAModelRef{UsePrimary: true},
				ReferenceModels: []corelib.MoAModelRef{
					{Provider: "DoesNotExist"},
				},
			},
		},
	}
	if err := app.SaveMoAConfig(moa); err == nil {
		t.Fatal("expected unknown provider error")
	}
}
