package main

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"strings"
)

func defaultWebSearchProviders() []corelib.WebSearchProvider {
	return []corelib.WebSearchProvider{
		{Name: "Brave", Type: "brave", BaseURL: "https://api.search.brave.com/res/v1/web/search"},
		{Name: "Serper", Type: "serper", BaseURL: "https://google.serper.dev/search"},
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
		if provider.Type == "" || seen[provider.Type] {
			continue
		}
		if def, ok := defaultByType[provider.Type]; ok {
			if provider.Name == "" {
				provider.Name = def.Name
			}
			if provider.BaseURL == "" {
				provider.BaseURL = def.BaseURL
			}
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

func (a *App) GetWebSearchProviders() struct {
	Providers []corelib.WebSearchProvider `json:"providers"`
	Current   string              `json:"current"`
} {
	cfg, err := a.LoadConfig()
	if err != nil {
		providers := defaultWebSearchProviders()
		return struct {
			Providers []corelib.WebSearchProvider `json:"providers"`
			Current   string              `json:"current"`
		}{Providers: providers, Current: resolveWebSearchCurrent(providers, "duckduckgo")}
	}
	providers := mergeDefaultWebSearchProviders(cfg.WebSearchProviders)
	current := resolveWebSearchCurrent(providers, cfg.WebSearchCurrentProvider)
	return struct {
		Providers []corelib.WebSearchProvider `json:"providers"`
		Current   string              `json:"current"`
	}{Providers: providers, Current: current}
}

func (a *App) SaveWebSearchProviders(providers []corelib.WebSearchProvider, current string) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	providers = mergeDefaultWebSearchProviders(providers)
	cfg.WebSearchProviders = providers
	cfg.WebSearchCurrentProvider = resolveWebSearchCurrent(providers, current)
	return a.SaveConfig(cfg)
}
