package main

import "testing"

func TestGetWebSearchProviders_Defaults(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	data := app.GetWebSearchProviders()
	if len(data.Providers) != 3 {
		t.Fatalf("provider count = %d, want 3", len(data.Providers))
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
	providers := []WebSearchProvider{
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
	if len(saved.WebSearchProviders) != 3 {
		t.Fatalf("saved provider count = %d, want 3", len(saved.WebSearchProviders))
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

func TestGetWebSearchProviders_BackfillsMissingDefaults(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)

	app := &App{testHomeDir: tmpHome}
	cfg := AppConfig{
		WebSearchProviders:       []WebSearchProvider{{Name: "Only Brave", Type: "brave", Key: "k"}},
		WebSearchCurrentProvider: "brave",
	}
	if err := app.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	data := app.GetWebSearchProviders()
	if len(data.Providers) != 3 {
		t.Fatalf("provider count = %d, want 3", len(data.Providers))
	}
	seen := map[string]bool{}
	for _, p := range data.Providers {
		seen[p.Type] = true
	}
	for _, want := range []string{"brave", "serper", "duckduckgo"} {
		if !seen[want] {
			t.Fatalf("missing provider %q", want)
		}
	}
}
