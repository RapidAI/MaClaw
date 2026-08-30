package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
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

func TestSaveWebSearchProvidersIgnoresRequestBaseURLAndUnknownProvider(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	providers := []corelib.WebSearchProvider{
		{Type: "brave", Key: "new-key", BaseURL: "https://attacker.example/collect"},
		{Type: "unknown", Key: "secret", BaseURL: "https://attacker.example/unknown"},
	}
	if err := app.SaveWebSearchProviders(providers, "brave"); err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range saved.WebSearchProviders {
		if provider.Type == "unknown" {
			t.Fatalf("unknown provider was persisted: %#v", provider)
		}
		if provider.Type == "brave" && provider.BaseURL != defaultWebSearchProviders()[0].BaseURL {
			t.Fatalf("request changed server-owned BaseURL: %#v", provider)
		}
	}
}

func TestTestWebSearchProvider_RequiresAPIKey(t *testing.T) {
	app := &App{}

	err := app.TestWebSearchProvider(corelib.WebSearchProvider{Type: " brave ", Key: "   "})
	if err == nil {
		t.Fatal("TestWebSearchProvider() error = nil, want missing key failure")
	}
}

func TestTestWebSearchProviderIgnoresRequestBaseURL(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	persistedEndpointReached := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		persistedEndpointReached = true
		http.Error(w, "persisted endpoint reached", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "brave" {
			strategy.Engines[i].BaseURL = server.URL
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}

	err := app.TestWebSearchProvider(corelib.WebSearchProvider{
		Type: "brave", Key: "candidate-key", BaseURL: "http://127.0.0.1:1/attacker",
	})
	if err == nil || !persistedEndpointReached {
		t.Fatalf("error = %v, persisted endpoint reached = %t", err, persistedEndpointReached)
	}
	if strings.Contains(err.Error(), "persisted endpoint reached") {
		t.Fatalf("provider body leaked through legacy test endpoint: %v", err)
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

func TestLegacyWebSearchProvidersMaskAndPreserveKeys(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchProviders: []corelib.WebSearchProvider{{Name: "Brave", Type: "brave", Key: "secret"}}, WebSearchCurrentProvider: "brave"}); err != nil {
		t.Fatal(err)
	}
	view := app.GetWebSearchProviders()
	if view.Providers[0].Key != "******" {
		t.Fatalf("legacy provider key was exposed: %q", view.Providers[0].Key)
	}
	if err := app.SaveWebSearchProviders(view.Providers, view.Current); err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if saved.WebSearchProviders[0].Key != "secret" {
		t.Fatalf("masked key was not preserved: %q", saved.WebSearchProviders[0].Key)
	}
}

func TestGetWebSearchStrategyMigratesLegacyAndMasksKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchProviders: []corelib.WebSearchProvider{{Type: "brave", Key: "secret"}}, WebSearchCurrentProvider: "brave"}); err != nil {
		t.Fatal(err)
	}
	view := app.GetWebSearchStrategy()
	if len(view.Engines) == 0 || view.Engines[0].ID != "brave" || !view.Engines[0].HasKey || view.Engines[0].APIKey != "" {
		t.Fatalf("strategy view = %#v", view)
	}
}

func TestResetWebSearchStrategyKeepsSavedKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetInternational)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "brave" {
			strategy.Engines[i].APIKey = "secret"
			strategy.Engines[i].Enabled = true
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.ResetWebSearchStrategy(corelib.WebSearchPresetMainland); err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range saved.WebSearchStrategy.Engines {
		if engine.ID == "brave" && engine.APIKey != "secret" {
			t.Fatalf("reset lost key: %#v", engine)
		}
	}
}

func TestResetWebSearchStrategyRestoresPresetEnabledStates(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetInternational)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "brave" {
			strategy.Engines[i].APIKey = "secret"
			strategy.Engines[i].Enabled = true
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}
	view, err := app.ResetWebSearchStrategy(corelib.WebSearchPresetMainland)
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range view.Engines {
		if engine.ID == "brave" && (engine.Enabled || !engine.HasKey) {
			t.Fatalf("reset did not restore preset enabled state while preserving key: %#v", engine)
		}
	}
}

func TestSaveWebSearchStrategyRejectsInvalidRequestWithoutChangingConfig(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	original := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: original}); err != nil {
		t.Fatal(err)
	}
	err := app.SaveWebSearchStrategy(SaveWebSearchStrategyRequest{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                 []corelib.WebSearchEngineConfig{{ID: "unknown", Enabled: true, Priority: 1}},
		BrowserFallbackEngineID: "bing_cn",
	})
	if err == nil {
		t.Fatal("expected invalid strategy error")
	}
	saved, loadErr := app.LoadConfig()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if saved.WebSearchStrategy.Preset != original.Preset || saved.WebSearchStrategy.Engines[0].ID != original.Engines[0].ID {
		t.Fatalf("invalid save changed strategy: %#v", saved.WebSearchStrategy)
	}
}

func TestSaveWebSearchStrategyClearsSavedKeyAndPreservesBaseURL(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "brave" {
			strategy.Engines[i].APIKey = "secret"
			strategy.Engines[i].BaseURL = "https://search.example"
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}

	requestEngines := append([]corelib.WebSearchEngineConfig(nil), strategy.Engines...)
	for i := range requestEngines {
		if requestEngines[i].ID == "brave" {
			requestEngines[i].APIKey = ""
			requestEngines[i].BaseURL = "https://attacker.example/collect"
		}
	}
	err := app.SaveWebSearchStrategy(SaveWebSearchStrategyRequest{
		Version: 1, Preset: corelib.WebSearchPresetMainland, Mode: corelib.WebSearchModePriority,
		Engines: requestEngines, ClearAPIKeyEngineIDs: []string{"brave"}, BrowserFallbackEnabled: true,
		BrowserFallbackEngineID: "bing_cn", HedgingDelayMS: 500, MinResultsBeforeHedge: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range saved.WebSearchStrategy.Engines {
		if engine.ID == "brave" {
			if engine.APIKey != "" {
				t.Fatalf("API key was not cleared: %q", engine.APIKey)
			}
			if engine.BaseURL != "https://search.example" {
				t.Fatalf("frontend changed server-owned base URL: %q", engine.BaseURL)
			}
			return
		}
	}
	t.Fatal("brave engine missing after save")
}

func TestSaveWebSearchStrategyNormalizesIDBeforePreservingServerBaseURL(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "brave" {
			strategy.Engines[i].APIKey = "secret"
			strategy.Engines[i].BaseURL = "https://search.example"
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}

	err := app.SaveWebSearchStrategy(SaveWebSearchStrategyRequest{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{{ID: " BRAVE ", Enabled: true, Priority: 1,
			Transport: corelib.WebSearchTransportAPI, APIKey: "replacement", BaseURL: "https://attacker.example/collect"}},
		BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range saved.WebSearchStrategy.Engines {
		if engine.ID == "brave" {
			if engine.BaseURL != "https://search.example" || engine.APIKey != "replacement" {
				t.Fatalf("server fields not preserved safely: %#v", engine)
			}
			return
		}
	}
	t.Fatal("brave engine missing after save")
}

func TestSaveWebSearchStrategyPartialRequestRetainsOmittedServerFields(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "brave" {
			strategy.Engines[i].APIKey = "secret"
			strategy.Engines[i].BaseURL = "https://search.example"
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}

	err := app.SaveWebSearchStrategy(SaveWebSearchStrategyRequest{
		Version: 1, Preset: corelib.WebSearchPresetCustom, Mode: corelib.WebSearchModePriority,
		Engines:                 []corelib.WebSearchEngineConfig{{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser}},
		BrowserFallbackEngineID: "bing_cn", MinResultsBeforeHedge: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range saved.WebSearchStrategy.Engines {
		if engine.ID == "brave" {
			if engine.Enabled || engine.APIKey != "secret" || engine.BaseURL != "https://search.example" {
				t.Fatalf("omitted engine fields were not retained: %#v", engine)
			}
			return
		}
	}
	t.Fatal("omitted brave engine missing after save")
}

func TestPreserveWebSearchStrategyServerFieldsDoesNotReinsertRetiredEngine(t *testing.T) {
	current := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	current.Engines = append(current.Engines, corelib.WebSearchEngineConfig{
		ID: "mojeek", Enabled: true, Priority: len(current.Engines) + 1,
		Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: "https://www.mojeek.com/search",
	})
	next := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	preserveWebSearchStrategyServerFields(&current, &next, nil)
	for _, engine := range next.Engines {
		if engine.ID == "mojeek" {
			t.Fatalf("retired engine was reinserted: %#v", next.Engines)
		}
	}
}

func TestSaveWebSearchStrategyRejectsUnknownKeyClearID(t *testing.T) {
	app := &App{}
	err := app.SaveWebSearchStrategy(SaveWebSearchStrategyRequest{ClearAPIKeyEngineIDs: []string{"unknown"}})
	if err == nil || !strings.Contains(err.Error(), "cannot clear API key") {
		t.Fatalf("error = %v", err)
	}
}

func TestTestWebSearchEngineIgnoresRequestBaseURL(t *testing.T) {
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	request := corelib.WebSearchEngineConfig{
		ID: " DUCKDUCKGO ", Transport: corelib.WebSearchTransportHTTPHTML, BaseURL: "file:///unexpected",
	}
	resolved := resolveWebSearchEngineForTest(strategy, request, false)
	if resolved.ID != "duckduckgo" || resolved.BaseURL == request.BaseURL || strings.TrimSpace(resolved.BaseURL) == "" {
		t.Fatalf("request base URL was not replaced: %#v", resolved)
	}
}

func TestTestWebSearchEngineUsesSavedTransport(t *testing.T) {
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	request := corelib.WebSearchEngineConfig{
		ID: " GOOGLE ", Transport: corelib.WebSearchTransportHTTPHTML,
	}
	resolved := resolveWebSearchEngineForTest(strategy, request, false)
	if resolved.ID != "google" || resolved.Transport != corelib.WebSearchTransportBrowser {
		t.Fatalf("request transport was not replaced: %#v", resolved)
	}
}

func TestResolveWebSearchEngineForTestRequiresExplicitSavedKeyUse(t *testing.T) {
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "brave" {
			strategy.Engines[i].APIKey = "saved-secret"
		}
	}
	request := corelib.WebSearchEngineConfig{ID: "brave", Transport: corelib.WebSearchTransportAPI}
	if got := resolveWebSearchEngineForTest(strategy, request, false); got.APIKey != "" {
		t.Fatalf("saved key was used without consent: %#v", got)
	}
	if got := resolveWebSearchEngineForTest(strategy, request, true); got.APIKey != "saved-secret" {
		t.Fatalf("saved key was not used when requested: %#v", got)
	}
}

func TestTestWebSearchEngineUsesRequestedHumanAssistPreference(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	strategy.BrowserHumanAssistEnabled = true
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}

	var gotHumanAssist bool
	websearch.SetBrowserSearchProvider(func(_ context.Context, _ string, _ string, _ int, humanAssist bool) ([]websearch.BrowserSearchHit, error) {
		gotHumanAssist = humanAssist
		return []websearch.BrowserSearchHit{{Title: "Result", URL: "https://example.com"}}, nil
	})
	t.Cleanup(func() { websearch.SetBrowserSearchProvider(nil) })

	if _, err := app.TestWebSearchEngine(TestWebSearchEngineRequest{
		Engine:             corelib.WebSearchEngineConfig{ID: "google", Transport: corelib.WebSearchTransportBrowser},
		HumanAssistEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if !gotHumanAssist {
		t.Fatal("browser engine test did not use the requested human-assist preference")
	}
}

func TestTestWebSearchEngineRedactsProviderResponseBody(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gateway echoed secret-token and private diagnostics", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "brave" {
			strategy.Engines[i].APIKey = "secret-token"
			strategy.Engines[i].BaseURL = server.URL
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}

	_, err := app.TestWebSearchEngine(TestWebSearchEngineRequest{
		Engine:      corelib.WebSearchEngineConfig{ID: "brave", Transport: corelib.WebSearchTransportAPI},
		UseSavedKey: true,
	})
	if err == nil {
		t.Fatal("expected provider test failure")
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "private diagnostics") {
		t.Fatalf("provider response leaked through settings API: %v", err)
	}
	if !strings.Contains(err.Error(), "credentials were rejected") {
		t.Fatalf("error = %v, want redacted credential failure", err)
	}
}

func TestTestWebSearchEnginePropagatesRetryCount(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"organic":[{"title":"Recovered","link":"https://example.com/recovered","snippet":"ok"}]}`))
	}))
	t.Cleanup(server.Close)
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "serper" {
			strategy.Engines[i].APIKey = "saved-secret"
			strategy.Engines[i].BaseURL = server.URL
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}

	result, err := app.TestWebSearchEngine(TestWebSearchEngineRequest{
		Engine:      corelib.WebSearchEngineConfig{ID: "serper", Transport: corelib.WebSearchTransportAPI},
		UseSavedKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || result.RetryCount != 1 || result.ResultCount != 1 {
		t.Fatalf("calls=%d result=%#v, want recovered result with retry_count=1", calls, result)
	}
}

func TestTestWebSearchEngineReportsFailureAfterRetry(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == "serper" {
			strategy.Engines[i].APIKey = "saved-secret"
			strategy.Engines[i].BaseURL = server.URL
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{WebSearchStrategy: strategy}); err != nil {
		t.Fatal(err)
	}

	_, err := app.TestWebSearchEngine(TestWebSearchEngineRequest{
		Engine:      corelib.WebSearchEngineConfig{ID: "serper", Transport: corelib.WebSearchTransportAPI},
		UseSavedKey: true,
	})
	if err == nil || !strings.Contains(err.Error(), "failed after one retry") {
		t.Fatalf("error = %v, want retry-aware failure", err)
	}
	if calls != 2 {
		t.Fatalf("provider calls = %d, want initial attempt plus one retry", calls)
	}
}

func TestGetWebSearchStrategyIncludesMaclawHubUncheckedWithoutDedicatedKey(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatal(err)
	}

	view := app.GetWebSearchStrategy()
	var found *WebSearchEngineView
	for i := range view.Engines {
		if view.Engines[i].ID == websearch.WebSearchEngineMaclawHub {
			copy := view.Engines[i]
			found = &copy
		}
	}
	if found == nil {
		t.Fatal("MaClaw Hub / RapidSearch missing from settings strategy")
	}
	if found.Name != "MaClaw Hub / RapidSearch" {
		t.Fatalf("name = %q", found.Name)
	}
	if found.Enabled || found.NeedsKey || found.HasKey || found.APIKey != "" {
		t.Fatalf("settings view leaked hub credentials or enabled the engine: %#v", found)
	}
	if found.BaseURL != "https://hub.maclaw.top/searchproxy/search" {
		t.Fatalf("base URL = %q", found.BaseURL)
	}
}

func TestTestWebSearchEngineUsesRegisteredHubTokenForMaclawHub(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	var gotAuth, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotQuery = payload.Query
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Hub","url":"https://example.com/hub","snippet":"ok"}]}`))
	}))
	t.Cleanup(server.Close)

	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == websearch.WebSearchEngineMaclawHub {
			strategy.Engines[i].BaseURL = server.URL
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteViewerToken: "viewer-token",
		WebSearchStrategy: strategy,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := app.TestWebSearchEngine(TestWebSearchEngineRequest{
		Engine: corelib.WebSearchEngineConfig{ID: websearch.WebSearchEngineMaclawHub, Transport: corelib.WebSearchTransportAPI},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer viewer-token" {
		t.Fatalf("Authorization = %q, want registered hub token", gotAuth)
	}
	if gotQuery != "golang http server" {
		t.Fatalf("query = %q, want golang http server", gotQuery)
	}
	if result.ResultCount != 1 || result.PreviewTitle != "Hub" || result.PreviewURL != "https://example.com/hub" || result.PreviewSnippet != "ok" {
		t.Fatalf("result = %#v", result)
	}
}

func TestIMGetWebSearchStrategyAttachesHubAuthAtRuntime(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{RemoteViewerToken: "viewer-token"}); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{app: app}
	strategy := h.getWebSearchStrategy()
	for _, engine := range strategy.Engines {
		if engine.ID == websearch.WebSearchEngineMaclawHub {
			if engine.Enabled {
				t.Fatalf("runtime strategy enabled MaClaw Hub by default: %#v", engine)
			}
			if engine.APIKey != "viewer-token" {
				t.Fatalf("runtime APIKey = %q, want viewer-token", engine.APIKey)
			}
			return
		}
	}
	t.Fatal("MaClaw Hub missing from runtime strategy")
}

func TestTestWebSearchEngineHonorsExplicitMaclawHubQuery(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotQuery = payload.Query
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Weather","url":"https://example.com/weather","snippet":"ok"}]}`))
	}))
	t.Cleanup(server.Close)

	strategy := websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	for i := range strategy.Engines {
		if strategy.Engines[i].ID == websearch.WebSearchEngineMaclawHub {
			strategy.Engines[i].BaseURL = server.URL
		}
	}
	if err := app.SaveConfig(corelib.AppConfig{
		RemoteViewerToken: "viewer-token",
		WebSearchStrategy: strategy,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := app.TestWebSearchEngine(TestWebSearchEngineRequest{
		Engine: corelib.WebSearchEngineConfig{ID: websearch.WebSearchEngineMaclawHub, Transport: corelib.WebSearchTransportAPI},
		Query:  "北京天气",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "北京天气" {
		t.Fatalf("query = %q, want 北京天气", gotQuery)
	}
	if result.PreviewTitle != "Weather" {
		t.Fatalf("result = %#v", result)
	}
}

func TestWebSearchEngineTestQueryDefaults(t *testing.T) {
	if got := webSearchEngineTestQuery(websearch.WebSearchEngineMaclawHub, "  北京天气  "); got != "北京天气" {
		t.Fatalf("explicit query = %q", got)
	}
	if got := webSearchEngineTestQuery(websearch.WebSearchEngineMaclawHub, ""); got != "golang http server" {
		t.Fatalf("hub default = %q", got)
	}
	if got := webSearchEngineTestQuery("brave", ""); got != "MaClaw web search configuration test" {
		t.Fatalf("other default = %q", got)
	}
}
