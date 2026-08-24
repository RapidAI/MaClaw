package main

// cost_route.go — TUI cost-route model selection (MACLAW_COST_ROUTE) aligned with GUI.

import (
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// buildModelRouter maps AppConfig.ModelRoutes into llm.ModelRouter.
func (app *TUIApp) buildModelRouter() *llm.ModelRouter {
	if app == nil || len(app.appConfig.ModelRoutes) == 0 {
		return llm.NewModelRouter(nil)
	}
	routes := make(map[string]llm.ModelRoute, len(app.appConfig.ModelRoutes))
	for k, v := range app.appConfig.ModelRoutes {
		routes[k] = llm.ModelRoute{
			Model:         v.Model,
			URL:           v.URL,
			Key:           v.Key,
			Protocol:      v.Protocol,
			Provider:      v.Provider,
			ContextLength: v.ContextLength,
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
	if app.isHubManagedTurnConfig(primary) {
		d := agent.RouteDecision{
			TaskType:         string(classified.Task),
			Model:            primary.Model,
			Provider:         primary.ProviderName,
			Source:           "primary",
			Reason:           classified.Reason + "; hub-managed skip desktop cost-route",
			Applied:          true,
			CostTier:         string(cost.Tier),
			CostRouteMode:    string(cost.Mode),
			CostRouteApplied: false,
			ThinkingPolicy:   string(cost.Thinking),
		}
		if llm.CostRouteSurfaces(cost.Mode) {
			log.Printf("[cost-route] tui mode=%s tier=%s task=%s model=%s applied=false reason=hub-managed",
				cost.Mode, cost.Tier, classified.Task, primary.Model)
		}
		workflowType, phaseKind := app.hubWorkflowHints()
		return primary.WithHubWorkloadHints(string(classified.Task), workflowType, phaseKind), d, true
	}

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
	cfg                   corelib.MaclawLLMConfig
	ok                    bool
	light                 bool
	surfaceRefreshPending bool
}

func (a *tuiActiveLLM) setRoute(cfg corelib.MaclawLLMConfig, decision agent.RouteDecision) {
	if a == nil {
		return
	}
	a.set(cfg)
	switch strings.ToLower(strings.TrimSpace(decision.TaskType)) {
	case string(llm.TaskFast), string(llm.TaskSummary), string(llm.TaskIntent):
		a.light = true
	default:
		a.light = strings.EqualFold(strings.TrimSpace(decision.Source), "aux")
	}
	a.surfaceRefreshPending = false
}

func (a *tuiActiveLLM) set(cfg corelib.MaclawLLMConfig) {
	if a == nil {
		return
	}
	a.cfg = cfg
	a.ok = strings.TrimSpace(cfg.Model) != "" && strings.TrimSpace(cfg.URL) != ""
}

// consumeSurfaceRefresh reports whether an escalation changed a light turn
// into a reasoning turn. It deliberately does not infer state from endpoint
// equality: a reasoning route can use the same model while only raising the
// context length.
func (a *tuiActiveLLM) consumeSurfaceRefresh() bool {
	if a == nil || !a.surfaceRefreshPending {
		return false
	}
	a.surfaceRefreshPending = false
	return true
}

func (a *tuiActiveLLM) get(fallback corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	if a != nil && a.ok {
		return a.cfg
	}
	return fallback
}

// escalateTUIActiveLLMAfterTool moves a light/cost route to the reasoning
// route after the model has requested a tool. The next core agent-loop round
// reads activeLLM, so the stronger route's context budget applies immediately
// to document reads and model-facing tool result projection.
func escalateTUIActiveLLMAfterTool(app *TUIApp, active *tuiActiveLLM) {
	if app == nil || active == nil || !active.light {
		return
	}
	primary := app.llmConfig
	if strings.TrimSpace(primary.URL) == "" || strings.TrimSpace(primary.Model) == "" {
		return
	}
	if app.isHubManagedTurnConfig(primary) {
		return
	}
	current := active.get(primary)
	next := app.buildModelRouter().RouteWithAux(llm.TaskReasoning, primary, app.appConfig.AuxiliaryLLM)
	if !sameTUIRoutedLLMConfig(current, next) {
		active.set(next)
		log.Printf("[model-route] tui tool escalation %s→%s context=%d→%d", current.Model, next.Model, current.ContextLength, next.ContextLength)
	}
	active.light = false
	active.surfaceRefreshPending = true
}

func sameTUIRoutedLLMConfig(a, b corelib.MaclawLLMConfig) bool {
	return a.Model == b.Model && a.URL == b.URL && a.Key == b.Key &&
		a.Protocol == b.Protocol && a.ProviderName == b.ProviderName &&
		a.ContextLength == b.ContextLength
}

func (app *TUIApp) isHubManagedTurnConfig(cfg corelib.MaclawLLMConfig) bool {
	if corelib.IsHubManagedLLMEndpoint(cfg.URL, cfg.Model) {
		return true
	}
	if app == nil {
		return false
	}
	want := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	for _, provider := range app.appConfig.MaclawLLMProviders {
		if !provider.IsHubService {
			continue
		}
		if strings.EqualFold(strings.TrimRight(strings.TrimSpace(provider.URL), "/"), want) {
			return true
		}
	}
	return false
}

func (app *TUIApp) hubWorkflowHints() (workflowType, phaseKind string) {
	if app == nil {
		return "", ""
	}
	return strings.TrimSpace(app.hubHintWorkflowType), strings.TrimSpace(app.hubHintPhaseKind)
}

func (app *TUIApp) setHubWorkflowHints(state *v2.WorkflowState) {
	if app == nil || state == nil {
		return
	}
	app.hubHintWorkflowType = strings.TrimSpace(state.Type)
	if phase := state.ActivePhase(); phase != nil {
		if phase.Kind != "" {
			app.hubHintPhaseKind = string(phase.Kind)
			return
		}
		app.hubHintPhaseKind = strings.TrimSpace(phase.ID)
		return
	}
	app.hubHintPhaseKind = ""
}

func (app *TUIApp) clearHubWorkflowHints() {
	if app == nil {
		return
	}
	app.hubHintWorkflowType = ""
	app.hubHintPhaseKind = ""
}
