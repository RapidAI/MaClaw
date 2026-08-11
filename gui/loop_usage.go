package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// accumulateLoopResultUsage folds RunLoop token accounting into the app-wide
// provider usage stats (sidebar / GetLLMTokenUsage) and process-local doctor
// last/process usage snapshots.
func accumulateLoopResultUsage(app *App, cfg corelib.MaclawLLMConfig, result agent.LoopResult) {
	u := result.Usage
	if u.Empty() {
		return
	}
	// Always record process-local last/process usage for System Doctor even
	// when provider labels are missing (still useful for token totals).
	recordLoopUsage(u)
	if app == nil {
		return
	}
	provider := maclawLLMUsageProviderName(app, cfg)
	if provider == "" {
		provider = strings.TrimSpace(u.Provider)
	}
	if provider == "" {
		provider = strings.TrimSpace(u.Model)
	}
	if provider == "" {
		return
	}
	app.AccumulateLLMTokenUsageWithCache(provider, u.InputTokens, u.OutputTokens, u.CachedTokens, u.CacheWriteTokens)
	// Preserve the legacy provider aggregate for existing surfaces, while new
	// requests also receive non-guessing profile/final-model attribution. The
	// loop's route decision is the source of truth after a coding reasoning or
	// vision route replaces the base profile model.
	app.AccumulateLLMProfileTokenUsageWithCache(finalLLMConfigForLoopUsage(cfg, result), u.InputTokens, u.OutputTokens, u.CachedTokens, u.CacheWriteTokens)
}

func finalLLMConfigForLoopUsage(base corelib.MaclawLLMConfig, result agent.LoopResult) corelib.MaclawLLMConfig {
	final := base
	if model := strings.TrimSpace(result.Route.Model); model != "" {
		final.Model = model
	}
	if provider := strings.TrimSpace(result.Route.Provider); provider != "" {
		final.ProviderName = provider
	}
	if source := strings.TrimSpace(result.Route.Source); source != "" {
		final.RouteSource = source
	}
	return final
}
