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
func tuiWebFetchProvider(cfg corelib.AppConfig) corelib.WebSearchProvider {
	strategy := tuiWebSearchStrategy(cfg)
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
