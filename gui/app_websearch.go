package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

type WebSearchEngineView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Priority  int    `json:"priority"`
	Transport string `json:"transport"`
	NeedsKey  bool   `json:"needs_api_key"`
	HasKey    bool   `json:"has_api_key"`
	APIKey    string `json:"api_key,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
}

type WebSearchStrategyView struct {
	Version                   int                   `json:"version"`
	Preset                    string                `json:"preset"`
	Mode                      string                `json:"mode"`
	Engines                   []WebSearchEngineView `json:"engines"`
	BrowserFallbackEnabled    bool                  `json:"browser_fallback_enabled"`
	BrowserFallbackEngineID   string                `json:"browser_fallback_engine_id"`
	BrowserHumanAssistEnabled bool                  `json:"browser_human_assist_enabled"`
	HedgingDelayMS            int                   `json:"hedging_delay_ms"`
	MinResultsBeforeHedge     int                   `json:"min_results_before_hedge"`
}

type SaveWebSearchStrategyRequest struct {
	Version                   int                             `json:"version"`
	Preset                    string                          `json:"preset"`
	Mode                      string                          `json:"mode"`
	Engines                   []corelib.WebSearchEngineConfig `json:"engines"`
	ClearAPIKeyEngineIDs      []string                        `json:"clear_api_key_engine_ids,omitempty"`
	BrowserFallbackEnabled    bool                            `json:"browser_fallback_enabled"`
	BrowserFallbackEngineID   string                          `json:"browser_fallback_engine_id"`
	BrowserHumanAssistEnabled bool                            `json:"browser_human_assist_enabled"`
	HedgingDelayMS            int                             `json:"hedging_delay_ms"`
	MinResultsBeforeHedge     int                             `json:"min_results_before_hedge"`
}

var allowedWebSearchAPIKeyClearIDs = map[string]bool{
	"brave": true, "serper": true, "tinyfish": true, "tavily": true,
}

var retiredWebSearchEngineIDs = map[string]bool{"mojeek": true}

type WebSearchEngineTestResult struct {
	EngineID       string `json:"engine_id"`
	Transport      string `json:"transport"`
	DurationMS     int64  `json:"duration_ms"`
	ResultCount    int    `json:"result_count"`
	RetryCount     int    `json:"retry_count,omitempty"`
	Message        string `json:"message"`
	PreviewTitle   string `json:"preview_title,omitempty"`
	PreviewURL     string `json:"preview_url,omitempty"`
	PreviewSnippet string `json:"preview_snippet,omitempty"`
}

type TestWebSearchEngineRequest struct {
	Engine             corelib.WebSearchEngineConfig `json:"engine"`
	Query              string                        `json:"query,omitempty"`
	UseSavedKey        bool                          `json:"use_saved_key"`
	HumanAssistEnabled bool                          `json:"human_assist_enabled"`
}

func defaultWebSearchProviders() []corelib.WebSearchProvider {
	return []corelib.WebSearchProvider{
		{Name: "Brave", Type: "brave", BaseURL: "https://api.search.brave.com/res/v1/web/search"},
		{Name: "Serper", Type: "serper", BaseURL: "https://google.serper.dev/search"},
		{Name: "TinyFish", Type: "tinyfish", BaseURL: "https://api.search.tinyfish.ai"},
		{Name: "Tavily", Type: "tavily", BaseURL: "https://api.tavily.com/search"},
		{Name: "DuckDuckGo", Type: "duckduckgo"},
	}
}

func normalizeWebSearchProvider(provider corelib.WebSearchProvider) corelib.WebSearchProvider {
	provider.Name = strings.TrimSpace(provider.Name)
	provider.Type = strings.ToLower(strings.TrimSpace(provider.Type))
	provider.Key = strings.TrimSpace(provider.Key)
	provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
	return provider
}

func mergeDefaultWebSearchProviders(providers []corelib.WebSearchProvider) []corelib.WebSearchProvider {
	defaults := defaultWebSearchProviders()
	defaultByType := make(map[string]corelib.WebSearchProvider, len(defaults))
	for _, provider := range defaults {
		defaultByType[provider.Type] = provider
	}
	merged := make([]corelib.WebSearchProvider, 0, len(defaults))
	seen := make(map[string]bool, len(defaults))
	for _, provider := range providers {
		provider = normalizeWebSearchProvider(provider)
		def, supported := defaultByType[provider.Type]
		if !supported || seen[provider.Type] {
			continue
		}
		if provider.Name == "" {
			provider.Name = def.Name
		}
		if provider.BaseURL == "" {
			provider.BaseURL = def.BaseURL
		}
		merged = append(merged, provider)
		seen[provider.Type] = true
	}
	for _, def := range defaults {
		if !seen[def.Type] {
			merged = append(merged, def)
		}
	}
	return merged
}

func resolveWebSearchCurrent(providers []corelib.WebSearchProvider, current string) string {
	current = strings.ToLower(strings.TrimSpace(current))
	if current != "" {
		for _, provider := range providers {
			if provider.Type == current {
				return current
			}
		}
	}
	for _, provider := range providers {
		if provider.Type == "duckduckgo" {
			return provider.Type
		}
	}
	if len(providers) > 0 {
		return providers[0].Type
	}
	return "duckduckgo"
}

func (a *App) effectiveWebSearchStrategy() corelib.WebSearchStrategy {
	cfg, err := a.LoadConfig()
	if err != nil {
		return websearch.DefaultWebSearchStrategy(corelib.WebSearchPresetMainland)
	}
	return websearch.MigrateLegacyWebSearchStrategy(cfg.WebSearchStrategy, cfg.WebSearchProviders, cfg.WebSearchCurrentProvider)
}

func webSearchEngineName(id string) string {
	switch id {
	case "bing_cn":
		return "Bing"
	case "baidu":
		return "百度"
	case "google":
		return "Google"
	case "duckduckgo":
		return "DuckDuckGo"
	case "brave":
		return "Brave Search API"
	case "serper":
		return "Serper（Google API）"
	case "tinyfish":
		return "TinyFish"
	case "tavily":
		return "Tavily"
	case websearch.WebSearchEngineMaclawHub:
		return "MaClaw Hub / RapidSearch"
	default:
		return id
	}
}

func webSearchEngineTestQuery(engineID, query string) string {
	query = strings.TrimSpace(query)
	if query != "" {
		return query
	}
	if engineID == websearch.WebSearchEngineMaclawHub {
		return "golang http server"
	}
	return "MaClaw web search configuration test"
}

func webSearchEngineNeedsKey(id string) bool {
	switch id {
	case "brave", "serper", "tinyfish", "tavily":
		return true
	}
	return false
}

func webSearchStrategyView(strategy corelib.WebSearchStrategy) WebSearchStrategyView {
	view := WebSearchStrategyView{Version: strategy.Version, Preset: strategy.Preset, Mode: strategy.Mode,
		BrowserFallbackEnabled: strategy.BrowserFallbackEnabled, BrowserFallbackEngineID: strategy.BrowserFallbackEngineID,
		BrowserHumanAssistEnabled: strategy.BrowserHumanAssistEnabled,
		HedgingDelayMS:            strategy.HedgingDelayMS, MinResultsBeforeHedge: strategy.MinResultsBeforeHedge}
	for _, engine := range strategy.Engines {
		view.Engines = append(view.Engines, WebSearchEngineView{ID: engine.ID, Name: webSearchEngineName(engine.ID), Enabled: engine.Enabled,
			Priority: engine.Priority, Transport: engine.Transport, NeedsKey: webSearchEngineNeedsKey(engine.ID), HasKey: strings.TrimSpace(engine.APIKey) != "", BaseURL: engine.BaseURL})
	}
	return view
}

func (a *App) GetWebSearchStrategy() WebSearchStrategyView {
	return webSearchStrategyView(a.effectiveWebSearchStrategy())
}

func strategyFromRequest(req SaveWebSearchStrategyRequest) corelib.WebSearchStrategy {
	return corelib.WebSearchStrategy{Version: req.Version, Preset: req.Preset, Mode: req.Mode, Engines: req.Engines,
		BrowserFallbackEnabled: req.BrowserFallbackEnabled, BrowserFallbackEngineID: req.BrowserFallbackEngineID,
		BrowserHumanAssistEnabled: req.BrowserHumanAssistEnabled,
		HedgingDelayMS:            req.HedgingDelayMS, MinResultsBeforeHedge: req.MinResultsBeforeHedge}
}

func preserveWebSearchStrategyServerFields(current *corelib.WebSearchStrategy, next *corelib.WebSearchStrategy, clearAPIKeyEngineIDs []string) {
	byID := make(map[string]corelib.WebSearchEngineConfig, len(current.Engines))
	for _, engine := range current.Engines {
		byID[strings.ToLower(strings.TrimSpace(engine.ID))] = engine
	}
	clearKeys := make(map[string]bool, len(clearAPIKeyEngineIDs))
	for _, id := range clearAPIKeyEngineIDs {
		clearKeys[strings.ToLower(strings.TrimSpace(id))] = true
	}
	seen := make(map[string]bool, len(next.Engines))
	for i := range next.Engines {
		id := strings.ToLower(strings.TrimSpace(next.Engines[i].ID))
		next.Engines[i].ID = id
		seen[id] = true
		if old, ok := byID[id]; ok {
			if clearKeys[id] {
				next.Engines[i].APIKey = ""
			} else if strings.TrimSpace(next.Engines[i].APIKey) == "" || strings.Contains(next.Engines[i].APIKey, "••••") {
				next.Engines[i].APIKey = old.APIKey
			}
			// Base URLs are not editable in the settings UI. Treat the persisted
			// value as server-owned so a compromised frontend cannot redirect a
			// provider request (and its API key) to an arbitrary endpoint.
			next.Engines[i].BaseURL = old.BaseURL
		}
	}
	// Partial or older clients may omit catalog entries. Retain their
	// server-owned credentials and endpoints while disabling them, so omission
	// cannot silently erase secrets or reset a custom proxy endpoint.
	for _, old := range current.Engines {
		id := strings.ToLower(strings.TrimSpace(old.ID))
		if seen[id] || retiredWebSearchEngineIDs[id] {
			continue
		}
		old.ID = id
		old.Enabled = false
		if clearKeys[id] {
			old.APIKey = ""
		}
		next.Engines = append(next.Engines, old)
	}
}

func resolveWebSearchEngineForTest(current corelib.WebSearchStrategy, engine corelib.WebSearchEngineConfig, useSavedKey bool) corelib.WebSearchEngineConfig {
	engine.ID = strings.ToLower(strings.TrimSpace(engine.ID))
	for _, existing := range current.Engines {
		if strings.ToLower(strings.TrimSpace(existing.ID)) != engine.ID {
			continue
		}
		if useSavedKey && strings.TrimSpace(engine.APIKey) == "" {
			engine.APIKey = existing.APIKey
		}
		// Engine transport is catalog-owned. Otherwise a crafted bridge request
		// could test a known engine through an unrelated execution path and produce
		// misleading results that the settings UI can never persist.
		engine.Transport = existing.Transport
		// Base URLs are persisted configuration, not ad-hoc test input. Keeping
		// this server-authoritative prevents a compromised frontend from using
		// the connectivity test as an arbitrary HTTP request primitive.
		engine.BaseURL = existing.BaseURL
		break
	}
	return engine
}

func (a *App) SaveWebSearchStrategy(req SaveWebSearchStrategyRequest) error {
	for _, rawID := range req.ClearAPIKeyEngineIDs {
		id := strings.ToLower(strings.TrimSpace(rawID))
		if !allowedWebSearchAPIKeyClearIDs[id] {
			return fmt.Errorf("cannot clear API key for web search engine %q", id)
		}
	}
	var validationErr error
	_, err := a.PatchConfigIfChanged(func(cfg *corelib.AppConfig) bool {
		current := websearch.MigrateLegacyWebSearchStrategy(cfg.WebSearchStrategy, cfg.WebSearchProviders, cfg.WebSearchCurrentProvider)
		next := strategyFromRequest(req)
		preserveWebSearchStrategyServerFields(&current, &next, req.ClearAPIKeyEngineIDs)
		normalized, normalizeErr := websearch.NormalizeWebSearchStrategy(next)
		if normalizeErr != nil {
			validationErr = normalizeErr
			return false
		}
		cfg.WebSearchStrategy = normalized
		return true
	})
	if validationErr != nil {
		return validationErr
	}
	if err != nil {
		return err
	}
	return nil
}

func (a *App) ResetWebSearchStrategy(preset string) (WebSearchStrategyView, error) {
	var strategy corelib.WebSearchStrategy
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		current := websearch.MigrateLegacyWebSearchStrategy(cfg.WebSearchStrategy, cfg.WebSearchProviders, cfg.WebSearchCurrentProvider)
		strategy = websearch.ResetWebSearchStrategy(current, preset)
		cfg.WebSearchStrategy = strategy
	}); err != nil {
		return WebSearchStrategyView{}, err
	}
	return webSearchStrategyView(strategy), nil
}

func (a *App) TestWebSearchEngine(req TestWebSearchEngineRequest) (WebSearchEngineTestResult, error) {
	current := a.effectiveWebSearchStrategy()
	engine := resolveWebSearchEngineForTest(current, req.Engine, req.UseSavedKey)
	engine.Enabled = true
	engine.Priority = 1
	if engine.ID == websearch.WebSearchEngineMaclawHub {
		// Hub search must use the GUI-registered token, never a request-supplied
		// or persisted engine key. The settings UI has no RapidSearch API-key field.
		engine.APIKey = ""
		if cfg, cfgErr := a.LoadConfig(); cfgErr == nil {
			engine.APIKey = websearch.HubAuthTokenFromConfig(cfg)
		}
	}
	// The probe may retry one transient HTTP/API failure. Leave enough outer
	// budget for two 12-second cold-start attempts plus the short retry delay.
	// RapidSearch often needs 10-20s and is given a 180s client budget.
	testTimeout := 30 * time.Second
	if engine.ID == websearch.WebSearchEngineMaclawHub {
		testTimeout = websearch.WebSearchMaclawHubTimeout
	} else if engine.Transport == corelib.WebSearchTransportBrowser && req.HumanAssistEnabled {
		testTimeout = 2 * time.Minute
	} else if engine.Transport == corelib.WebSearchTransportBrowser {
		testTimeout = 35 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	started := time.Now()
	response, err := websearch.ProbeWebSearchEngineCtx(ctx, webSearchEngineTestQuery(engine.ID, req.Query), 3, engine, req.HumanAssistEnabled)
	result := WebSearchEngineTestResult{EngineID: engine.ID, Transport: engine.Transport, DurationMS: time.Since(started).Milliseconds(), ResultCount: len(response.Results)}
	if len(response.Results) > 0 {
		result.PreviewTitle = strings.TrimSpace(response.Results[0].Title)
		result.PreviewURL = strings.TrimSpace(response.Results[0].URL)
		result.PreviewSnippet = strings.TrimSpace(response.Results[0].Snippet)
	}
	if len(response.Diagnostics) > 0 {
		result.RetryCount = response.Diagnostics[0].RetryCount
	}
	if err != nil {
		// Provider responses and proxy errors can echo credentials, request data,
		// or private gateway details. The settings bridge must only expose the
		// stable, redacted diagnostic produced by the strategy layer. Prefer the
		// structured attempt because the aggregate error is intentionally generic.
		detail := websearch.SafeSearchErrorDetail(err)
		if len(response.Diagnostics) > 0 && response.Diagnostics[0].Detail != "" {
			detail = response.Diagnostics[0].Detail
		}
		if result.RetryCount > 0 {
			return result, fmt.Errorf("%s test failed after one retry: %s", webSearchEngineName(engine.ID), detail)
		}
		return result, fmt.Errorf("%s test failed: %s", webSearchEngineName(engine.ID), detail)
	}
	result.Message = fmt.Sprintf("%s test passed with %d results", webSearchEngineName(engine.ID), result.ResultCount)
	return result, nil
}

// Legacy Wails APIs remain for one compatibility cycle.
func (a *App) GetWebSearchProviders() struct {
	Providers []corelib.WebSearchProvider `json:"providers"`
	Current   string                      `json:"current"`
} {
	cfg, err := a.LoadConfig()
	providers := defaultWebSearchProviders()
	current := "duckduckgo"
	if err == nil {
		providers = mergeDefaultWebSearchProviders(cfg.WebSearchProviders)
		current = resolveWebSearchCurrent(providers, cfg.WebSearchCurrentProvider)
	}
	// Compatibility callers only need to know whether a provider is configured;
	// never return stored credentials to frontend JavaScript.
	for i := range providers {
		if strings.TrimSpace(providers[i].Key) != "" {
			providers[i].Key = "******"
		}
	}
	return struct {
		Providers []corelib.WebSearchProvider `json:"providers"`
		Current   string                      `json:"current"`
	}{providers, current}
}

func (a *App) SaveWebSearchProviders(providers []corelib.WebSearchProvider, current string) error {
	return a.PatchConfig(func(cfg *corelib.AppConfig) {
		persistedByType := make(map[string]corelib.WebSearchProvider, len(defaultWebSearchProviders()))
		for _, provider := range mergeDefaultWebSearchProviders(cfg.WebSearchProviders) {
			persistedByType[provider.Type] = provider
		}
		next := append([]corelib.WebSearchProvider(nil), providers...)
		for i := range next {
			next[i] = normalizeWebSearchProvider(next[i])
			persisted, ok := persistedByType[next[i].Type]
			if !ok {
				continue
			}
			if strings.TrimSpace(next[i].Key) == "" || strings.Contains(next[i].Key, "******") {
				next[i].Key = persisted.Key
			}
			// This compatibility endpoint never exposed BaseURL editing. Preserve the
			// server-side endpoint so it cannot be used to redirect stored keys.
			next[i].BaseURL = persisted.BaseURL
		}
		next = mergeDefaultWebSearchProviders(next)
		cfg.WebSearchProviders = next
		cfg.WebSearchCurrentProvider = resolveWebSearchCurrent(next, current)
	})
}

func (a *App) TestWebSearchProvider(provider corelib.WebSearchProvider) error {
	provider = normalizeWebSearchProvider(provider)
	if provider.Type == "brave" || provider.Type == "serper" || provider.Type == "tinyfish" || provider.Type == "tavily" {
		if strings.TrimSpace(provider.Key) == "" || strings.Contains(provider.Key, "******") {
			return fmt.Errorf("%s API key is not configured", webSearchEngineName(provider.Type))
		}
	}
	for _, engine := range a.effectiveWebSearchStrategy().Engines {
		if engine.ID != provider.Type {
			continue
		}
		provider.BaseURL = engine.BaseURL
		break
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := websearch.TestProvider(ctx, provider); err != nil {
		return fmt.Errorf("%s test failed: %s", webSearchEngineName(provider.Type), websearch.SafeSearchErrorDetail(err))
	}
	return nil
}
