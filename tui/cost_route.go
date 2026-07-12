package main

// cost_route.go — TUI cost-route model selection (MACLAW_COST_ROUTE) aligned with GUI.

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// buildModelRouter maps AppConfig.ModelRoutes into llm.ModelRouter.
func (app *TUIApp) buildModelRouter() *llm.ModelRouter {
	if app == nil || len(app.appConfig.ModelRoutes) == 0 {
		return llm.NewModelRouter(nil)
	}
	routes := make(map[string]llm.ModelRoute, len(app.appConfig.ModelRoutes))
	for k, v := range app.appConfig.ModelRoutes {
		routes[k] = llm.ModelRoute{
			Model:    v.Model,
			URL:      v.URL,
			Key:      v.Key,
			Protocol: v.Protocol,
			Provider: v.Provider,
		}
	}
	return llm.NewModelRouter(routes)
}

// routeTurn classifies the user message and applies cost-route / classic DecideTurn.
// Returns applied=true when a usable primary config exists (caller should adopt cfg).
func (app *TUIApp) routeTurn(userText string, hints llm.ClassifyHints) (corelib.MaclawLLMConfig, agent.RouteDecision, bool) {
	if app == nil {
		return corelib.MaclawLLMConfig{}, agent.RouteDecision{}, false
	}
	primary := app.llmConfig
	if strings.TrimSpace(primary.URL) == "" || strings.TrimSpace(primary.Model) == "" {
		return primary, agent.RouteDecision{}, false
	}
	router := app.buildModelRouter()
	aux := app.appConfig.AuxiliaryLLM
	classified := llm.ClassifyTurn(userText, hints)
	cost := llm.DecideCostRoute(classified.Task, hints, classified.Reason)

	var cfg corelib.MaclawLLMConfig
	var source, reason string
	if cost.Mode == llm.CostRouteOn {
		var detail string
		cfg, cost.Applied, source, detail = llm.ApplyCostTierConfig(router, primary, aux, cost.Tier, cost.Mode)
		cfg = llm.ApplyThinkingPolicy(cfg, cost.Thinking)
		cost.Reason = detail + "; think=" + string(cost.Thinking)
		reason = classified.Reason + "; " + cost.Reason
	} else {
		cfg, _, source, reason = llm.DecideTurn(router, primary, aux, userText, hints)
		if llm.CostRouteSurfaces(cost.Mode) {
			reason = reason + "; " + cost.Reason
		}
	}

	d := agent.RouteDecision{
		TaskType:         string(classified.Task),
		Model:            cfg.Model,
		Provider:         cfg.ProviderName,
		Source:           source,
		Reason:           reason,
		Applied:          true,
		CostTier:         string(cost.Tier),
		CostRouteMode:    string(cost.Mode),
		CostRouteApplied: cost.Applied,
		ThinkingPolicy:   string(cost.Thinking),
	}
	if llm.CostRouteSurfaces(cost.Mode) {
		log.Printf("[cost-route] tui mode=%s tier=%s think=%s task=%s model=%s→%s applied=%v",
			cost.Mode, cost.Tier, cost.Thinking, classified.Task, primary.Model, cfg.Model, cost.Applied)
	} else if cfg.Model != primary.Model {
		log.Printf("[model-route] tui task=%s source=%s model=%s→%s reason=%q",
			classified.Task, source, primary.Model, cfg.Model, reason)
	}
	return cfg, d, true
}

// tuiActiveLLM holds a RouteTurn result so GetLLMConfig does not snap back to primary.
type tuiActiveLLM struct {
	cfg corelib.MaclawLLMConfig
	ok  bool
}

func (a *tuiActiveLLM) set(cfg corelib.MaclawLLMConfig) {
	if a == nil {
		return
	}
	a.cfg = cfg
	a.ok = strings.TrimSpace(cfg.Model) != "" && strings.TrimSpace(cfg.URL) != ""
}

func (a *tuiActiveLLM) get(fallback corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	if a != nil && a.ok {
		return a.cfg
	}
	return fallback
}
