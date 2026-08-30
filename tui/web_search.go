package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// tuiWebSearchStrategy keeps interactive, pipe, and RPC modes aligned with
// the desktop search settings stored in the shared AppConfig.
func tuiWebSearchStrategy(cfg corelib.AppConfig) corelib.WebSearchStrategy {
	return websearch.ApplyConfigHubAuth(
		websearch.MigrateLegacyWebSearchStrategy(
			cfg.WebSearchStrategy,
			cfg.WebSearchProviders,
			cfg.WebSearchCurrentProvider,
		),
		cfg,
	)
}

// tuiWebFetchProvider enables provider-aware extraction only when TinyFish is
// the highest-priority enabled engine, matching the desktop behavior.
// An enabled MaClaw Hub / RapidSearch engine wins so downloads share that
// proxy channel even when another engine is first in search order.
func tuiWebFetchProvider(cfg corelib.AppConfig) corelib.WebSearchProvider {
	strategy := tuiWebSearchStrategy(cfg)
	for _, engine := range strategy.Engines {
		if engine.ID == websearch.WebSearchEngineMaclawHub && engine.Enabled {
			return corelib.WebSearchProvider{Type: engine.ID, Key: engine.APIKey, BaseURL: engine.BaseURL}
		}
	}
	for _, engine := range strategy.Engines {
		if !engine.Enabled {
			continue
		}
		if engine.ID == "tinyfish" && strings.TrimSpace(engine.APIKey) != "" {
			return corelib.WebSearchProvider{Type: engine.ID, Key: engine.APIKey, BaseURL: engine.BaseURL}
		}
		break
	}
	return corelib.WebSearchProvider{}
}
