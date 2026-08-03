package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestTUIWebSearchStrategyUsesSavedOrder(t *testing.T) {
	cfg := corelib.AppConfig{WebSearchStrategy: corelib.WebSearchStrategy{
		Version: corelib.WebSearchStrategyVersion,
		Preset:  corelib.WebSearchPresetCustom,
		Mode:    corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{
			{ID: "google", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportBrowser},
			{ID: "bing_cn", Enabled: true, Priority: 2, Transport: corelib.WebSearchTransportHTTPHTML},
		},
		BrowserFallbackEngineID: "bing_cn",
		MinResultsBeforeHedge:   3,
	}}

	strategy := tuiWebSearchStrategy(cfg)
	if len(strategy.Engines) < 2 || strategy.Engines[0].ID != "google" || strategy.Engines[1].ID != "bing_cn" {
		t.Fatalf("engine order = %#v", strategy.Engines)
	}
}

func TestTUIWebSearchStrategyMigratesLegacyCurrentProviderByType(t *testing.T) {
	cfg := corelib.AppConfig{
		WebSearchProviders:       []corelib.WebSearchProvider{{Name: "Private Brave", Type: "brave", Key: "secret"}},
		WebSearchCurrentProvider: "brave",
	}

	strategy := tuiWebSearchStrategy(cfg)
	if len(strategy.Engines) == 0 || strategy.Engines[0].ID != "brave" || !strategy.Engines[0].Enabled {
		t.Fatalf("legacy strategy = %#v", strategy)
	}
}

func TestTUIWebFetchProviderFollowsHighestPriorityEngine(t *testing.T) {
	cfg := corelib.AppConfig{WebSearchStrategy: corelib.WebSearchStrategy{
		Version: corelib.WebSearchStrategyVersion,
		Preset:  corelib.WebSearchPresetCustom,
		Mode:    corelib.WebSearchModePriority,
		Engines: []corelib.WebSearchEngineConfig{
			{ID: "tinyfish", Enabled: true, Priority: 1, Transport: corelib.WebSearchTransportAPI, APIKey: "secret"},
			{ID: "bing_cn", Enabled: true, Priority: 2, Transport: corelib.WebSearchTransportHTTPHTML},
		},
		BrowserFallbackEngineID: "bing_cn",
		MinResultsBeforeHedge:   3,
	}}

	provider := tuiWebFetchProvider(cfg)
	if provider.Type != "tinyfish" || provider.Key != "secret" {
		t.Fatalf("provider = %#v", provider)
	}

	cfg.WebSearchStrategy.Engines[0], cfg.WebSearchStrategy.Engines[1] = cfg.WebSearchStrategy.Engines[1], cfg.WebSearchStrategy.Engines[0]
	cfg.WebSearchStrategy.Engines[0].Priority = 1
	cfg.WebSearchStrategy.Engines[1].Priority = 2
	if provider := tuiWebFetchProvider(cfg); provider.Type != "" {
		t.Fatalf("provider = %#v, want standard fetch", provider)
	}
}
