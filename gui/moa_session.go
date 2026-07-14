package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
)

// moaSession holds per-user MoA arming state.
type moaSession struct {
	// OneShot: next agent loop only, then clear.
	OneShot bool
	// Sticky: every chat loop until cleared (session model moa:preset).
	Sticky bool
	// Resolved council preset.
	Resolved moa.ResolvedPreset
}

type moaSessionStore struct {
	mu   sync.Mutex
	byID map[string]*moaSession
}

func (s *moaSessionStore) armOneShot(userID string, preset moa.ResolvedPreset) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		s.byID = make(map[string]*moaSession)
	}
	s.byID[userID] = &moaSession{OneShot: true, Resolved: preset}
}

func (s *moaSessionStore) armSticky(userID string, preset moa.ResolvedPreset) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		s.byID = make(map[string]*moaSession)
	}
	s.byID[userID] = &moaSession{Sticky: true, Resolved: preset}
}

func (s *moaSessionStore) peek(userID string) *moaSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byID == nil {
		return nil
	}
	return s.byID[userID]
}

func (s *moaSessionStore) clear(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, userID)
}

// clearOneShot removes one-shot arm only; sticky sessions remain.
func (s *moaSessionStore) clearOneShot(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.byID[userID]
	if sess == nil {
		return
	}
	if sess.Sticky {
		sess.OneShot = false
		return
	}
	delete(s.byID, userID)
}

// resolveDefaultMoAPreset loads config and resolves the default MoA preset.
func (h *IMMessageHandler) resolveDefaultMoAPreset() (moa.ResolvedPreset, string, bool) {
	return h.resolveMoAPreset("")
}

// resolveMoAPreset resolves a named MoA preset (empty name → default).
// ok=false with detail when MoA cannot run.
func (h *IMMessageHandler) resolveMoAPreset(presetName string) (moa.ResolvedPreset, string, bool) {
	if h == nil || h.app == nil {
		return moa.ResolvedPreset{}, "app not ready", false
	}
	if !moa.EnvAllows() {
		return moa.ResolvedPreset{}, "MACLAW_MOA=off (kill switch)", false
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return moa.ResolvedPreset{}, "load config failed", false
	}
	moaCfg := corelib.NormalizeMoAConfig(cfg.MoA)
	if !moa.EffectiveEnabled(moaCfg.Enabled) {
		return moa.ResolvedPreset{}, "enable multi-model in LLM settings", false
	}
	if len(moaCfg.Presets) == 0 {
		return moa.ResolvedPreset{}, "configure other models in multi-model settings", false
	}
	primary := h.app.GetMaclawLLMConfig()
	if strings.TrimSpace(primary.URL) == "" || strings.TrimSpace(primary.Model) == "" {
		return moa.ResolvedPreset{}, "configure a primary LLM first", false
	}
	name := moa.PickPresetName(moaCfg, presetName)
	if name == "" {
		return moa.ResolvedPreset{}, "configure other models in multi-model settings", false
	}
	if _, ok := moaCfg.Presets[name]; !ok {
		return moa.ResolvedPreset{}, fmt.Sprintf("preset %q not found", name), false
	}
	router := llm.NewModelRouter(nil)
	if len(cfg.ModelRoutes) > 0 {
		routes := make(map[string]llm.ModelRoute, len(cfg.ModelRoutes))
		for k, v := range cfg.ModelRoutes {
			routes[k] = llm.ModelRoute{Model: v.Model, URL: v.URL, Key: v.Key, Protocol: v.Protocol, Provider: v.Provider}
		}
		router = llm.NewModelRouter(routes)
	}
	resolved, err := moa.ResolvePreset(moa.ResolveInput{
		AppMoA:     moaCfg,
		Primary:    primary,
		Aux:        cfg.AuxiliaryLLM,
		Router:     router,
		Lookup:     h.app.MaterializeProviderByName,
		PresetName: name,
	})
	if err != nil {
		return moa.ResolvedPreset{}, err.Error(), false
	}
	if moa.CountUsableRefs(resolved.References) == 0 {
		return moa.ResolvedPreset{}, "no usable other models (OAuth responses-ws cannot be advisors)", false
	}
	return resolved, "", true
}

// tryArmMoAOneShot resolves the default preset and arms one-shot for userID.
// Returns empty string on success, or a user-facing error message.
func (h *IMMessageHandler) tryArmMoAOneShot(userID, lang string) string {
	return h.tryArmMoAOneShotPreset(userID, lang, "")
}

// tryArmMoAOneShotPreset arms one-shot MoA with an optional preset name (empty = default).
func (h *IMMessageHandler) tryArmMoAOneShotPreset(userID, lang, presetName string) string {
	resolved, detail, ok := h.resolveMoAPreset(presetName)
	if !ok {
		return localizedIMMoAUnavailable(lang, detail)
	}
	if h.moaSessions == nil {
		h.moaSessions = &moaSessionStore{}
	}
	h.moaSessions.armOneShot(userID, resolved)
	return ""
}

// tryPrepareMoAAuto arms a loop-local auto MoA when allow_auto matches the turn.
// Returns resolved preset when auto should apply; ok=false otherwise (silent).
func (h *IMMessageHandler) tryPrepareMoAAuto(userText string, route modelRouteDecision) (moa.ResolvedPreset, bool) {
	if h == nil || h.app == nil {
		return moa.ResolvedPreset{}, false
	}
	if !moa.EnvAllows() {
		return moa.ResolvedPreset{}, false
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return moa.ResolvedPreset{}, false
	}
	moaCfg := corelib.NormalizeMoAConfig(cfg.MoA)
	if !moa.EffectiveEnabled(moaCfg.Enabled) || !moaCfg.AllowAuto {
		return moa.ResolvedPreset{}, false
	}
	task := llm.TaskType(strings.ToLower(strings.TrimSpace(route.Task)))
	if task == "" {
		// Fallback classify when route has no task.
		cr := llm.ClassifyTurn(userText, llm.ClassifyHints{})
		task = cr.Task
	}
	if !moa.ShouldActivateAuto(true, task, route.CostTier) {
		return moa.ResolvedPreset{}, false
	}
	resolved, _, ok := h.resolveDefaultMoAPreset()
	return resolved, ok
}

func localizedIMMoAUnavailable(lang, detail string) string {
	detail = strings.TrimSpace(detail)
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return fmt.Sprintf("Multi-model council is not available: %s", detail)
	case appLanguageZhHant:
		return fmt.Sprintf("多模型會診不可用：%s", detail)
	default:
		return fmt.Sprintf("多模型会诊不可用：%s", detail)
	}
}
