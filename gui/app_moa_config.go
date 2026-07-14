package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm/moa"
)

// GetMoAConfig returns the current MoA (multi-model council) configuration.
func (a *App) GetMoAConfig() (corelib.MoAConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.MoAConfig{}, err
	}
	return corelib.NormalizeMoAConfig(cfg.MoA), nil
}

// SaveMoAConfig validates and persists MoA configuration.
func (a *App) SaveMoAConfig(moaCfg corelib.MoAConfig) error {
	moaCfg = corelib.NormalizeMoAConfig(moaCfg)
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	known := make(map[string]struct{}, len(cfg.MaclawLLMProviders))
	for _, p := range cfg.MaclawLLMProviders {
		name := strings.TrimSpace(p.Name)
		if name != "" {
			known[name] = struct{}{}
		}
	}
	if err := corelib.ValidateMoAConfig(moaCfg, known); err != nil {
		return err
	}
	// If enabled and default empty, pick first preset deterministically.
	if moaCfg.Enabled && moaCfg.DefaultPreset == "" && len(moaCfg.Presets) > 0 {
		// Prefer "review" or "default" if present; else first key in sorted order.
		if _, ok := moaCfg.Presets["review"]; ok {
			moaCfg.DefaultPreset = "review"
		} else if _, ok := moaCfg.Presets["default"]; ok {
			moaCfg.DefaultPreset = "default"
		} else {
			for name := range moaCfg.Presets {
				moaCfg.DefaultPreset = name
				break
			}
		}
	}
	cfg.MoA = moaCfg
	if err := a.SaveConfig(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	log.Printf("[moa] SaveMoAConfig enabled=%v presets=%d default=%q", moaCfg.Enabled, len(moaCfg.Presets), moaCfg.DefaultPreset)
	return nil
}

// MoAPresetSummary is a lightweight preset row for pickers.
type MoAPresetSummary struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	RefCount    int    `json:"ref_count"`
	Enabled     bool   `json:"enabled"`
}

// MoASessionState is the desktop session arming state for multi-model council.
type MoASessionState struct {
	Sticky      bool   `json:"sticky"`
	OneShot     bool   `json:"one_shot"`
	Preset      string `json:"preset,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Env         string `json:"env,omitempty"`
	Enabled     bool   `json:"enabled"`
	// Available is true when MoA can be armed (config+env+has default preset with refs).
	Available bool `json:"available"`
	// RefCount is the number of reference models on the active/default preset.
	RefCount int `json:"ref_count,omitempty"`
	// Presets lists configured councils for sticky picker UIs.
	Presets []MoAPresetSummary `json:"presets,omitempty"`
}

// GetMoASessionState returns whether sticky/one-shot MoA is armed for desktop-user.
func (a *App) GetMoASessionState() MoASessionState {
	st := MoASessionState{Env: moa.EnvStatusLabel()}
	if a == nil {
		return st
	}
	if cfg, err := a.LoadConfig(); err == nil {
		m := corelib.NormalizeMoAConfig(cfg.MoA)
		st.Enabled = moa.EffectiveEnabled(m.Enabled)
		// Build preset list (sorted by id for stable UI).
		ids := make([]string, 0, len(m.Presets))
		for id := range m.Presets {
			ids = append(ids, id)
		}
		// simple insertion sort (few presets)
		for i := 1; i < len(ids); i++ {
			j := i
			for j > 0 && ids[j] < ids[j-1] {
				ids[j], ids[j-1] = ids[j-1], ids[j]
				j--
			}
		}
		st.Presets = make([]MoAPresetSummary, 0, len(ids))
		for _, id := range ids {
			p := m.Presets[id]
			st.Presets = append(st.Presets, MoAPresetSummary{
				ID:          id,
				DisplayName: p.DisplayName,
				RefCount:    len(p.ReferenceModels),
				Enabled:     p.Enabled,
			})
		}
		name := m.DefaultPreset
		if name == "" && len(ids) > 0 {
			name = ids[0]
		}
		if p, ok := m.Presets[name]; ok {
			st.RefCount = len(p.ReferenceModels)
			if st.DisplayName == "" {
				st.DisplayName = p.DisplayName
			}
			if st.Preset == "" {
				st.Preset = name
			}
		}
		st.Available = st.Enabled && st.RefCount > 0 && moa.EnvAllows()
	}
	h := a.imHandler
	if h == nil || h.moaSessions == nil {
		return st
	}
	// Desktop local tab user id.
	sess := h.moaSessions.peek("desktop-user")
	if sess == nil {
		return st
	}
	st.Sticky = sess.Sticky
	st.OneShot = sess.OneShot
	st.Preset = sess.Resolved.Name
	st.DisplayName = sess.Resolved.DisplayName
	if st.DisplayName == "" {
		st.DisplayName = sess.Resolved.Name
	}
	return st
}

// SetMoASticky enables or disables sticky multi-model council for this app session
// (desktop-user). sticky=true resolves the default preset and arms it until cleared.
func (a *App) SetMoASticky(sticky bool) error {
	if !sticky {
		return a.SetMoAStickyPreset("")
	}
	return a.SetMoAStickyPreset("_default_")
}

// SetMoAStickyPreset arms sticky multi-model council with a named preset.
// Empty presetName clears sticky. "_default_" or unknown empty → default preset.
func (a *App) SetMoAStickyPreset(presetName string) error {
	if a == nil || a.imHandler == nil {
		return fmt.Errorf("agent not ready")
	}
	h := a.imHandler
	name := strings.TrimSpace(presetName)
	if name == "" {
		if h.moaSessions != nil {
			h.moaSessions.clear("desktop-user")
		}
		log.Printf("[moa] sticky cleared")
		if a.ctx != nil {
			a.emitEvent("moa-session-changed", nil)
		}
		return nil
	}
	if name == "_default_" {
		name = ""
	}
	resolved, detail, ok := h.resolveMoAPreset(name)
	if !ok {
		return fmt.Errorf("%s", detail)
	}
	if h.moaSessions == nil {
		h.moaSessions = &moaSessionStore{}
	}
	h.moaSessions.armSticky("desktop-user", resolved)
	log.Printf("[moa] sticky armed preset=%s", resolved.Name)
	if a.ctx != nil {
		a.emitEvent("moa-session-changed", nil)
	}
	return nil
}

// GetMoAStats returns durable MoA fan-out counters for diagnostics UI.
func (a *App) GetMoAStats() moa.Stats {
	return moa.LoadStats()
}

// clearMoAStickyForAllUsers is called when the primary LLM provider switches.
func (a *App) clearMoAStickyForAllUsers() {
	if a == nil || a.imHandler == nil || a.imHandler.moaSessions == nil {
		return
	}
	// Clear known desktop keys; full map wipe is safer for provider switch.
	a.imHandler.moaSessions.mu.Lock()
	a.imHandler.moaSessions.byID = make(map[string]*moaSession)
	a.imHandler.moaSessions.mu.Unlock()
	log.Printf("[moa] cleared all session arms (provider switch)")
}
