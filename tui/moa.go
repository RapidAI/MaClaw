package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
)

// tuiMoAState holds one-shot / sticky MoA for the TUI process.
type tuiMoAState struct {
	mu      sync.Mutex
	oneShot *moa.ResolvedPreset
	sticky  *moa.ResolvedPreset
}

func (s *tuiMoAState) armOneShot(p moa.ResolvedPreset) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := p
	s.oneShot = &cp
}

func (s *tuiMoAState) armSticky(p moa.ResolvedPreset) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := p
	s.sticky = &cp
	s.oneShot = nil
}

func (s *tuiMoAState) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.oneShot = nil
	s.sticky = nil
}

// takeForLoop returns the preset for this agent loop and consumes one-shot.
func (s *tuiMoAState) takeForLoop() *moa.ResolvedPreset {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oneShot != nil {
		p := *s.oneShot
		s.oneShot = nil
		return &p
	}
	if s.sticky != nil {
		cp := *s.sticky
		return &cp
	}
	return nil
}

func (app *TUIApp) moaState() *tuiMoAState {
	if app == nil {
		return nil
	}
	if app.moa == nil {
		app.moa = &tuiMoAState{}
	}
	return app.moa
}

func (app *TUIApp) materializeProvider(name string) (corelib.MaclawLLMConfig, error) {
	name = strings.TrimSpace(name)
	if app == nil || name == "" {
		return corelib.MaclawLLMConfig{}, fmt.Errorf("provider required")
	}
	for _, p := range app.appConfig.MaclawLLMProviders {
		if corelib.MaclawLLMProviderNameEqual(p.Name, name) {
			return corelib.MaclawLLMConfig{
				URL:             p.URL,
				Key:             p.Key,
				Model:           corelib.MigrateZhipuCodingModel(p.Name, p.Model),
				Protocol:        p.Protocol,
				ContextLength:   p.ContextLength,
				TimeoutSec:      p.TimeoutSec,
				MaxOutputTokens: p.MaxOutputTokens,
				SupportsVision:  p.SupportsVision,
				AgentType:       p.AgentType,
				WireAPI:         p.WireAPI,
				ProviderName:    p.Name,
				AuthType:        p.AuthType,
				ThinkingMode:    app.appConfig.MaclawLLMThinkingMode,
			}, nil
		}
	}
	return corelib.MaclawLLMConfig{}, fmt.Errorf("provider %q not found", name)
}

func (app *TUIApp) resolveMoADefaultPreset() (moa.ResolvedPreset, error) {
	return app.resolveMoAPresetNamed("")
}

func (app *TUIApp) resolveMoAPresetNamed(presetName string) (moa.ResolvedPreset, error) {
	if app == nil {
		return moa.ResolvedPreset{}, fmt.Errorf("app not ready")
	}
	if !moa.EnvAllows() {
		return moa.ResolvedPreset{}, fmt.Errorf("MACLAW_MOA=off")
	}
	moaCfg := corelib.NormalizeMoAConfig(app.appConfig.MoA)
	if !moa.EffectiveEnabled(moaCfg.Enabled) {
		return moa.ResolvedPreset{}, fmt.Errorf("moa disabled in config")
	}
	primary := app.llmConfig
	if strings.TrimSpace(primary.URL) == "" || strings.TrimSpace(primary.Model) == "" {
		return moa.ResolvedPreset{}, fmt.Errorf("llm not configured")
	}
	name := moa.PickPresetName(moaCfg, presetName)
	if name == "" {
		return moa.ResolvedPreset{}, fmt.Errorf("no moa presets configured")
	}
	router := llm.NewModelRouter(nil)
	if len(app.appConfig.ModelRoutes) > 0 {
		routes := make(map[string]llm.ModelRoute, len(app.appConfig.ModelRoutes))
		for k, v := range app.appConfig.ModelRoutes {
			routes[k] = llm.ModelRoute{Model: v.Model, URL: v.URL, Key: v.Key, Protocol: v.Protocol, Provider: v.Provider, ContextLength: v.ContextLength}
		}
		router = llm.NewModelRouter(routes)
	}
	resolved, err := moa.ResolvePreset(moa.ResolveInput{
		AppMoA:     moaCfg,
		Primary:    primary,
		Aux:        app.appConfig.AuxiliaryLLM,
		Router:     router,
		Lookup:     app.materializeProvider,
		PresetName: name,
	})
	if err != nil {
		return moa.ResolvedPreset{}, err
	}
	if moa.CountUsableRefs(resolved.References) == 0 {
		return moa.ResolvedPreset{}, fmt.Errorf("no usable reference models")
	}
	return resolved, nil
}

// AllowMoAFanOut implements agent.MoABudgetGate: skip advisors when daily budget cannot cover the wave.
func (c *tuiCallbacks) AllowMoAFanOut(nRefs int) (ok bool, reason string) {
	if c == nil || c.app == nil {
		return true, ""
	}
	ct := c.app.ensureCostTracker()
	if ct == nil || ct.BudgetLimit() <= 0 {
		return true, ""
	}
	need := moa.EstimateWaveMinUSD(nRefs)
	if ct.CanAfford(need) {
		return true, ""
	}
	return false, fmt.Sprintf("moa advisors skipped (daily budget low; need ~$%.4f, %s)", need, ct.DailySummary())
}

// PrepareMoA implements agent.MoAHost on tuiCallbacks.
func (c *tuiCallbacks) PrepareMoA(iteration int, toolsSeen bool, fanoutsRan int) (active bool, preset moa.ResolvedPreset, progress string) {
	if c == nil || c.app == nil {
		return false, moa.ResolvedPreset{}, ""
	}
	// K9: env kill switch wins over sticky/one-shot armed earlier.
	if !moa.EnvAllows() {
		return false, moa.ResolvedPreset{}, ""
	}
	if c.moaPreset == nil {
		if p := c.app.moaState().takeForLoop(); p != nil {
			c.moaPreset = p
		} else {
			moaCfg := corelib.NormalizeMoAConfig(c.app.appConfig.MoA)
			if moa.EffectiveEnabled(moaCfg.Enabled) && moaCfg.AllowAuto {
				cr := llm.ClassifyTurn(c.lastUserText, llm.ClassifyHints{})
				tier := c.lastRoute.CostTier
				if moa.ShouldActivateAuto(true, cr.Task, tier) {
					if resolved, err := c.app.resolveMoADefaultPreset(); err == nil {
						cp := resolved
						c.moaPreset = &cp
						c.moaAuto = true
					}
				}
			}
		}
	}
	if c.moaPreset == nil || !c.moaPreset.Enabled {
		return false, moa.ResolvedPreset{}, ""
	}
	if c.CurrentPromptProfile().IsLight() {
		c.UpgradeLightPromptToFull("moa council")
	}
	n := len(c.moaPreset.References)
	progress = fmt.Sprintf("consulting %d models…", n)
	if c.moaAuto && iteration == 0 && fanoutsRan == 0 {
		progress = "auto multi-model: " + progress
	}
	_ = toolsSeen
	return true, *c.moaPreset, progress
}
