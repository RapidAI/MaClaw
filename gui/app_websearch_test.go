package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestGetWebSearchProviders_Defaults(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	data := app.GetWebSearchProviders()
	wantCount := len(defaultWebSearchProviders())
	if len(data.Providers) != wantCount {
		t.Fatalf("provider count = %d, want %d", len(data.Providers), wantCount)
	}
	if data.Current != "duckduckgo" {
		t.Fatalf("current = %q, want duckduckgo", data.Current)
	}
}

func TestSaveWebSearchProviders_NormalizesAndPersistsCurrent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() seed error = %v", err)
	}
	cfg.RemoteEmail = "owner@example.com"
	cfg.LogDetailEnabled = true
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() seed error = %v", err)
	}

	providers := []corelib.WebSearchProvider{
		{Name: " Brave ", Type: " BRAVE ", Key: "  brave-key  ", BaseURL: "https://api.search.brave.com/res/v1/web/search/"},
	}
	if err := app.SaveWebSearchProviders(providers, " BRAVE "); err != nil {
		t.Fatalf("SaveWebSearchProviders() error = %v", err)
	}

	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if saved.WebSearchCurrentProvider != "brave" {
		t.Fatalf("WebSearchCurrentProvider = %q, want brave", saved.WebSearchCurrentProvider)
	}
	if saved.RemoteEmail != "owner@example.com" || !saved.LogDetailEnabled {
		t.Fatalf("unrelated fields overwritten by web search save: %#v", saved)
	}
	wantCount := len(defaultWebSearchProviders())
	if len(saved.WebSearchProviders) != wantCount {
		t.Fatalf("saved provider count = %d, want %d", len(saved.WebSearchProviders), wantCount)
	}
	if saved.WebSearchProviders[0].Type != "brave" {
		t.Fatalf("provider[0].Type = %q, want brave", saved.WebSearchProviders[0].Type)
	}
	if saved.WebSearchProviders[0].Key != "brave-key" {
		t.Fatalf("provider[0].Key = %q, want brave-key", saved.WebSearchProviders[0].Key)
	}
	if saved.WebSearchProviders[0].BaseURL != "https://api.search.brave.com/res/v1/web/search" {
		t.Fatalf("provider[0].BaseURL = %q", saved.WebSearchProviders[0].BaseURL)
	}
}

func TestTestWebSearchProvider_RequiresAPIKey(t *testing.T) {
	app := &App{}

	err := app.TestWebSearchProvider(corelib.WebSearchProvider{Type: " brave ", Key: "   "})
	if err == nil {
		t.Fatal("TestWebSearchProvider() error = nil, want missing key failure")
	}
}

func TestGetWebSearchProviders_BackfillsMissingDefaults(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := corelib.AppConfig{
		WebSearchProviders:       []corelib.WebSearchProvider{{Name: "Only Brave", Type: "brave", Key: "k"}},
		WebSearchCurrentProvider: "brave",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetWebSearchProviders()
	wantCount := len(defaultWebSearchProviders())
	if len(data.Providers) != wantCount {
		t.Fatalf("provider count = %d, want %d", len(data.Providers), wantCount)
	}
	seen := map[string]bool{}
	for _, p := range data.Providers {
		seen[p.Type] = true
	}
	for _, want := range []string{"brave", "serper", "tinyfish", "duckduckgo"} {
		if !seen[want] {
			t.Fatalf("missing provider %q", want)
		}
	}
}
